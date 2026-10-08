package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "registry.json")
	entries, err := Load(path)
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0", len(entries))
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	want := []Entry{
		{Path: "/code/foo", Project: "foo", Repo: "git@example.com:org/foo", LastSeen: "2026-09-01T00:00:00Z"},
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if got[0].Path != want[0].Path || got[0].Project != want[0].Project {
		t.Errorf("entry = %+v, want %+v", got[0], want[0])
	}
}

func TestUpsertUpdatesExisting(t *testing.T) {
	entries := []Entry{{Path: "/code/foo", Project: "foo", Repo: "old", LastSeen: "old"}}
	got := Upsert(entries, "/code/foo", "new", "foo")
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if got[0].Repo != "new" {
		t.Errorf("repo = %q, want new", got[0].Repo)
	}
	if got[0].LastSeen == "old" {
		t.Error("last_seen not updated")
	}
}

func TestUpsertAppendsNew(t *testing.T) {
	entries := []Entry{{Path: "/code/foo"}}
	got := Upsert(entries, "/code/bar", "repo", "bar")
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	if got[1].Path != "/code/bar" {
		t.Errorf("path = %q, want /code/bar", got[1].Path)
	}
}

func TestDropMissing(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present")
	if err := os.MkdirAll(filepath.Join(present, ".todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries := []Entry{
		{Path: present},
		{Path: filepath.Join(dir, "missing")},
	}
	kept, stale, err := DropMissing(entries)
	if err != nil {
		t.Fatalf("DropMissing: %v", err)
	}
	if len(kept) != 1 || len(stale) != 1 {
		t.Fatalf("kept=%d stale=%d, want 1/1", len(kept), len(stale))
	}
	if kept[0].Path != present {
		t.Errorf("kept path = %q", kept[0].Path)
	}
}

func TestFindTodoReposAndUnregistered(t *testing.T) {
	dir := t.TempDir()
	registered := filepath.Join(dir, "registered")
	unregistered := filepath.Join(dir, "unregistered")
	for _, p := range []string{registered, unregistered} {
		if err := os.MkdirAll(filepath.Join(p, ".todo"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, ".todo", "todo.md"), []byte("---\n---\n\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}

	repos, err := FindTodoRepos([]string{dir}, 2)
	if err != nil {
		t.Fatalf("FindTodoRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("repos = %v, want the two .todo folders", repos)
	}

	found := Unregistered(repos, []Entry{{Path: registered}})
	if len(found) != 1 {
		t.Fatalf("found = %d, want 1", len(found))
	}
	if found[0] != unregistered {
		t.Errorf("found = %q, want %q", found[0], unregistered)
	}
}

func TestEnclosingRepo(t *testing.T) {
	repo := t.TempDir()
	writeTodoFile(t, repo)
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := EnclosingRepo(sub)
	if !ok {
		t.Fatal("EnclosingRepo from a subdirectory found no repo")
	}
	if got != repo {
		t.Errorf("enclosing repo = %q, want %q", got, repo)
	}

	if _, ok := EnclosingRepo(t.TempDir()); ok {
		t.Error("EnclosingRepo outside a todo repo reported one")
	}
}

func TestUnadopted(t *testing.T) {
	dir := t.TempDir()
	adopted := filepath.Join(dir, "adopted")
	unadopted := filepath.Join(dir, "unadopted")
	for _, p := range []string{adopted, unadopted} {
		if err := os.MkdirAll(filepath.Join(p, ".todo"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTodoFile(t, unadopted)

	store := filepath.Join(dir, "store-todo.md")
	if err := os.WriteFile(store, []byte("---\n---\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, filepath.Join(adopted, ".todo", "todo.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	found, err := Unadopted([]string{adopted, unadopted})
	if err != nil {
		t.Fatalf("Unadopted: %v", err)
	}
	if len(found) != 1 || found[0] != unadopted {
		t.Errorf("unadopted = %v, want [%s]", found, unadopted)
	}
}

// writeTodoFile creates a repo-local .todo/todo.md as a real file.
func writeTodoFile(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repo, ".todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".todo", "todo.md"), []byte("---\n---\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
