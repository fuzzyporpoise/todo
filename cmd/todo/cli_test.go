package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"go.fuzzyporpoise.dev/todo/internal/registry"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", dir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	t.Chdir(dir)
	t.Setenv("TODO_REGISTRY", filepath.Join(dir, "registry.json"))
	return dir
}

func runApp(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := newApp()
	app.Writer = &stdout
	app.ErrWriter = &stderr
	app.Reader = strings.NewReader("")
	// Prevent ExitCoder from calling os.Exit so tests can inspect the error.
	app.ExitErrHandler = func(context.Context, *cli.Command, error) {}
	err := app.Run(context.Background(), append([]string{"todo"}, args...))
	return stdout.String(), stderr.String(), err
}

func TestInitCreatesTodoFile(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := os.Stat(".todo/todo.md"); err != nil {
		t.Fatalf(".todo/todo.md not created: %v", err)
	}
}

func TestAddAndList(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	out, _, err := runApp(t, []string{"add", "test task"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.Contains(out, "Created TSK-001") {
		t.Errorf("add output = %q, want Created TSK-001", out)
	}

	out, _, err = runApp(t, []string{"list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "test task") {
		t.Errorf("list output = %q, want task summary", out)
	}
}

func TestAddWithoutSummaryErrors(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	_, _, err := runApp(t, []string{"add"})
	if err == nil {
		t.Fatal("add with no summary: expected error")
	}
}

func TestAddHelpDocumentsSummaryCap(t *testing.T) {
	setupGitRepo(t)

	out, _, err := runApp(t, []string{"add", "--help"})
	if err != nil {
		t.Fatalf("add --help: %v", err)
	}
	for _, want := range []string{"120 runes", "work-order note"} {
		if !strings.Contains(out, want) {
			t.Errorf("add --help missing %q:\n%s", want, out)
		}
	}
}

func TestListJSON(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "json task"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runApp(t, []string{"list", "--json"})
	if err != nil {
		t.Fatalf("list --json: %v", err)
	}

	var envelope struct {
		SchemaVersion int        `json:"schema_version"`
		Tasks         []jsonTask `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("parse list json: %v", err)
	}
	if envelope.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", envelope.SchemaVersion)
	}
	tasks := envelope.Tasks
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	task := tasks[0]
	if task.ID != "TSK-001" {
		t.Errorf("id = %q, want TSK-001", task.ID)
	}
	if task.Status != "open" {
		t.Errorf("status = %q, want open", task.Status)
	}
	if task.StatusSymbol != " " {
		t.Errorf("status_symbol = %q, want space", task.StatusSymbol)
	}
	if task.Summary != "json task" {
		t.Errorf("summary = %q, want json task", task.Summary)
	}
}

func TestListSort(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-p", "med", "medium task"}); err != nil {
		t.Fatalf("add med: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-p", "low", "low task"}); err != nil {
		t.Fatalf("add low: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-p", "high", "high task"}); err != nil {
		t.Fatalf("add high: %v", err)
	}

	out, _, err := runApp(t, []string{"list", "--sort", "priority", "--json"})
	if err != nil {
		t.Fatalf("list --sort priority: %v", err)
	}
	var envelope struct {
		SchemaVersion int        `json:"schema_version"`
		Tasks         []jsonTask `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if len(envelope.Tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(envelope.Tasks))
	}
	if got := envelope.Tasks[0].Priority; got != "low" {
		t.Errorf("first sorted priority = %q, want low", got)
	}
	if got := envelope.Tasks[2].Priority; got != "high" {
		t.Errorf("last sorted priority = %q, want high", got)
	}

	out, _, err = runApp(t, []string{"list", "--sort", "priority", "--reverse", "--json"})
	if err != nil {
		t.Fatalf("list --sort priority --reverse: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if got := envelope.Tasks[0].Priority; got != "high" {
		t.Errorf("first reverse sorted priority = %q, want high", got)
	}
	if got := envelope.Tasks[2].Priority; got != "low" {
		t.Errorf("last reverse sorted priority = %q, want low", got)
	}
}

func TestDetailJSON(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "detail task"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-001"}); err != nil {
		t.Fatalf("pickup: %v", err)
	}

	out, _, err := runApp(t, []string{"detail", "TSK-001", "--json"})
	if err != nil {
		t.Fatalf("detail --json: %v", err)
	}

	var detail jsonDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("parse detail json: %v", err)
	}
	if detail.ID != "TSK-001" {
		t.Errorf("id = %q, want TSK-001", detail.ID)
	}
	if detail.Status != "in_progress" {
		t.Errorf("status = %q, want in_progress", detail.Status)
	}
	if detail.StatusSymbol != "o" {
		t.Errorf("status_symbol = %q, want o", detail.StatusSymbol)
	}
	if detail.Summary != "detail task" {
		t.Errorf("summary = %q, want detail task", detail.Summary)
	}
}

func TestRemoveNoteFlag(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "--create-note", "note task"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	if _, err := os.Stat(".todo/notes/TSK-001.md"); err != nil {
		t.Fatalf("note not created: %v", err)
	}

	out, _, err := runApp(t, []string{"remove", "--note", "TSK-001"})
	if err != nil {
		t.Fatalf("remove --note: %v", err)
	}
	if !strings.Contains(out, "TSK-001") {
		t.Errorf("remove output = %q, want TSK-001", out)
	}
	if _, err := os.Stat(".todo/notes/TSK-001.md"); !os.IsNotExist(err) {
		t.Errorf("note still exists after remove --note: %v", err)
	}
}

func TestRemoveNoteFlagFollowsSymlink(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "--create-note", "note task"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Simulate lnk project-scope: replace the note file with a symlink whose
	// target lives outside .todo (the "store").
	notePath := filepath.Join(".todo", "notes", "TSK-001.md")
	target := filepath.Join(t.TempDir(), "TSK-001.md")
	if err := os.WriteFile(target, []byte("note"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Remove(notePath); err != nil {
		t.Fatalf("remove note: %v", err)
	}
	if err := os.Symlink(target, notePath); err != nil {
		t.Fatalf("symlink note: %v", err)
	}

	if _, _, err := runApp(t, []string{"remove", "--note", "TSK-001"}); err != nil {
		t.Fatalf("remove --note: %v", err)
	}

	// The store target must be gone, and no dangling link may remain.
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("store target still exists after remove --note: %v", err)
	}
	if _, err := os.Lstat(notePath); !os.IsNotExist(err) {
		t.Errorf("note link still exists after remove --note: %v", err)
	}
}

func TestReopen(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "reopen task"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-001"}); err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if _, _, err := runApp(t, []string{"complete", "TSK-001"}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	out, _, err := runApp(t, []string{"reopen", "TSK-001"})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !strings.Contains(out, "[ ]") {
		t.Errorf("reopen output = %q, want open checkbox", out)
	}

	out, _, err = runApp(t, []string{"list", "--json"})
	if err != nil {
		t.Fatalf("list --json: %v", err)
	}
	if strings.Contains(out, "complete") {
		t.Errorf("task still complete after reopen: %q", out)
	}
}

func TestBumpUp(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-p", "low", "bump test"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runApp(t, []string{"bump", "TSK-001"})
	if err != nil {
		t.Fatalf("bump: %v", err)
	}
	if !strings.Contains(out, "[priority:med]") {
		t.Errorf("bump output = %q, want priority:med", out)
	}

	out, _, err = runApp(t, []string{"bump", "TSK-001"})
	if err != nil {
		t.Fatalf("bump: %v", err)
	}
	if !strings.Contains(out, "[priority:high]") {
		t.Errorf("bump output = %q, want priority:high", out)
	}
}

func TestBumpDown(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-p", "high", "bump test"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runApp(t, []string{"bump", "--down", "TSK-001"})
	if err != nil {
		t.Fatalf("bump --down: %v", err)
	}
	if !strings.Contains(out, "[priority:med]") {
		t.Errorf("bump --down output = %q, want priority:med", out)
	}
}

func TestBumpNoOp(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-p", "high", "bump test"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runApp(t, []string{"bump", "TSK-001"})
	if err != nil {
		t.Fatalf("bump: %v", err)
	}
	if !strings.Contains(out, "already at the up boundary") {
		t.Errorf("bump output = %q, want boundary message", out)
	}
}

func TestBumpNotFound(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	_, _, err := runApp(t, []string{"bump", "TSK-999"})
	if err == nil {
		t.Fatal("bump of missing task: expected error")
	}
}

func TestSchema(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	out, _, err := runApp(t, []string{"schema"})
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	var result struct {
		SchemaVersion int      `json:"schema_version"`
		StatusEnum    []string `json:"status_enum"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse schema json: %v", err)
	}
	if result.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", result.SchemaVersion)
	}
	want := []string{"open", "in_progress", "complete"}
	if len(result.StatusEnum) != len(want) {
		t.Fatalf("status_enum = %v, want %v", result.StatusEnum, want)
	}
	for i, v := range result.StatusEnum {
		if v != want[i] {
			t.Errorf("status_enum[%d] = %q, want %q", i, v, want[i])
		}
	}
}

func TestAddNoteDispositionFlags(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Work-order note.
	if _, _, err := runApp(t, []string{"add", "-n", "--kind", "work-order", "--note-content", "body", "work order"}); err != nil {
		t.Fatalf("add work-order: %v", err)
	}
	data, err := os.ReadFile(".todo/notes/TSK-001.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := "---\nkind: work-order\n---\n\nbody"; string(data) != want {
		t.Errorf("work-order note = %q, want %q", string(data), want)
	}

	// Record note.
	if _, _, err := runApp(t, []string{"add", "-n", "--category", "areas", "--synopsis", "syn", "--source", "repo", "--note-content", "body", "record"}); err != nil {
		t.Fatalf("add record: %v", err)
	}
	data, err = os.ReadFile(".todo/notes/TSK-002.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "---\ncategory: areas\ncreated: ") {
		t.Errorf("record note prefix = %q", string(data))
	}
	if !strings.HasSuffix(string(data), "\nsource: repo\nsynopsis: syn\n---\n\nbody\n") {
		t.Errorf("record note suffix = %q", string(data))
	}
}

func TestDetailJSONDisposition(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-n", "--kind", "work-order", "--note-content", "body", "disposition task"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runApp(t, []string{"detail", "TSK-001", "--json"})
	if err != nil {
		t.Fatalf("detail --json: %v", err)
	}

	var detail jsonDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("parse detail json: %v", err)
	}
	if detail.Disposition != "work-order" {
		t.Errorf("disposition = %q, want work-order", detail.Disposition)
	}
}

func TestDetailFullJSON(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	longBody := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\nline 11\nline 12\nline 13\nline 14\nline 15\nline 16\nline 17\nline 18\nline 19\nline 20\nline 21"
	if _, _, err := runApp(t, []string{"add", "-n", "--kind", "work-order", "--note-content", longBody, "full task"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runApp(t, []string{"detail", "TSK-001", "--json"})
	if err != nil {
		t.Fatalf("detail --json: %v", err)
	}

	var detail jsonDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("parse detail json: %v", err)
	}
	if !detail.NotePreviewTruncated {
		t.Errorf("NotePreviewTruncated = false, want true")
	}
	if detail.NoteBody != "" {
		t.Errorf("NoteBody = %q, want empty without --full", detail.NoteBody)
	}

	out, _, err = runApp(t, []string{"detail", "TSK-001", "--json", "--full"})
	if err != nil {
		t.Fatalf("detail --json --full: %v", err)
	}

	var fullDetail jsonDetail
	if err := json.Unmarshal([]byte(out), &fullDetail); err != nil {
		t.Fatalf("parse detail json: %v", err)
	}
	wantBody := "---\nkind: work-order\n---\n\n" + longBody
	if fullDetail.NoteBody != wantBody {
		t.Errorf("NoteBody = %q, want %q", fullDetail.NoteBody, wantBody)
	}
	if fullDetail.NotePreview != "" {
		t.Errorf("NotePreview = %q, want empty with --full", fullDetail.NotePreview)
	}
	if fullDetail.NotePreviewTruncated {
		t.Errorf("NotePreviewTruncated = true, want false with --full")
	}
}

func TestDetailFullHuman(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-n", "--kind", "work-order", "--note-content", "line 1\nline 2\nline 3", "human full task"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runApp(t, []string{"detail", "TSK-001", "--full"})
	if err != nil {
		t.Fatalf("detail --full: %v", err)
	}
	if !strings.Contains(out, "line 3") {
		t.Errorf("human --full output missing line 3: %q", out)
	}
}

func TestDetailFullNoNote(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "no note task"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runApp(t, []string{"detail", "TSK-001", "--json", "--full"})
	if err != nil {
		t.Fatalf("detail --json --full: %v", err)
	}

	var detail jsonDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("parse detail json: %v", err)
	}
	if detail.NoteBody != "" {
		t.Errorf("NoteBody = %q, want empty for task with no note", detail.NoteBody)
	}
	if detail.NoteExists {
		t.Error("NoteExists = true, want false")
	}
}

func TestDetailFullNoNoteFlag(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-n", "--kind", "work-order", "--note-content", "secret", "flag task"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runApp(t, []string{"detail", "TSK-001", "--json", "--full", "--no-note"})
	if err != nil {
		t.Fatalf("detail --json --full --no-note: %v", err)
	}

	var detail jsonDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("parse detail json: %v", err)
	}
	if detail.NoteBody != "" {
		t.Errorf("NoteBody = %q, want empty with --no-note", detail.NoteBody)
	}
	if detail.NotePreview != "" {
		t.Errorf("NotePreview = %q, want empty with --no-note", detail.NotePreview)
	}
}

func TestDetailFullSymlink(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "symlink task"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	noteDir := t.TempDir()
	realNote := filepath.Join(noteDir, "TSK-001.md")
	if err := os.WriteFile(realNote, []byte("line 1\nline 2\nline 3"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(".todo", "notes", "TSK-001.md")
	if err := os.MkdirAll(filepath.Dir(symlink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realNote, symlink); err != nil {
		t.Fatal(err)
	}

	out, _, err := runApp(t, []string{"detail", "TSK-001", "--json", "--full"})
	if err != nil {
		t.Fatalf("detail --json --full: %v", err)
	}

	var detail jsonDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("parse detail json: %v", err)
	}
	if detail.NoteBody != "line 1\nline 2\nline 3" {
		t.Errorf("NoteBody = %q, want symlink target content", detail.NoteBody)
	}
}

func TestAddDispositionRequiresNote(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	_, _, err := runApp(t, []string{"add", "--category", "areas", "no note"})
	if err == nil {
		t.Fatal("disposition flag without --note: expected error")
	}
}

func TestAddContentFlagsImplyNote(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	// --note-content without --note creates the note.
	if _, _, err := runApp(t, []string{"add", "-s", "x", "--note-content", "y"}); err != nil {
		t.Fatalf("add --note-content: %v", err)
	}
	data, err := os.ReadFile(".todo/notes/TSK-001.md")
	if err != nil {
		t.Fatalf("note not created: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n\ny\n") {
		t.Errorf("note content = %q, want body y", string(data))
	}

	// --note-file without --note copies the file into the note.
	src := filepath.Join(t.TempDir(), "n.md")
	if err := os.WriteFile(src, []byte("file body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runApp(t, []string{"add", "-s", "z", "--note-file", src}); err != nil {
		t.Fatalf("add --note-file: %v", err)
	}
	data, err = os.ReadFile(".todo/notes/TSK-002.md")
	if err != nil {
		t.Fatalf("note not created: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n\nfile body\n") {
		t.Errorf("note content = %q, want copied body", string(data))
	}

	// --dry-run previews the would-be note path for content-flag adds.
	out, _, err := runApp(t, []string{"add", "--dry-run", "-s", "x", "--note-content", "y"})
	if err != nil {
		t.Fatalf("add --dry-run: %v", err)
	}
	if !strings.Contains(out, "would create note: ") {
		t.Errorf("dry-run output = %q, want note path", out)
	}
}

func TestAddNoteOwnsItsFrontmatter(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	docBlocks := func(t *testing.T, name string) int {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(".todo/notes", name))
		if err != nil {
			t.Fatalf("read note: %v", err)
		}
		count := 0
		for line := range strings.SplitSeq(string(data), "\n") {
			if strings.TrimSpace(line) == "---" {
				count++
			}
		}
		return count
	}

	// --note-file bridges a free-standing doc: its frontmatter is dropped.
	src := filepath.Join(t.TempDir(), "plan.md")
	doc := "---\ntitle: Phase 3\ncategory: areas\nsynopsis: doc synopsis\n---\n\nplan body\n"
	if err := os.WriteFile(src, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runApp(t, []string{"add", "-s", "from doc", "--note-file", src}); err != nil {
		t.Fatalf("add --note-file: %v", err)
	}
	if n := docBlocks(t, "TSK-001.md"); n != 2 {
		t.Errorf("note-file add frontmatter delimiters = %d, want 2", n)
	}
	data, err := os.ReadFile(".todo/notes/TSK-001.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "kind: record") || strings.Contains(string(data), "title: Phase 3") {
		t.Errorf("note = %q, want todo's record block over the doc body", string(data))
	}

	// A supplied body declaring a park record keeps that disposition.
	if _, _, err := runApp(t, []string{"add", "-s", "declared park", "--note-content", "---\ncategory: areas\n---\n\nprose"}); err != nil {
		t.Fatalf("add --note-content with a declared park block: %v", err)
	}
	if n := docBlocks(t, "TSK-002.md"); n != 2 {
		t.Errorf("declared-park add frontmatter delimiters = %d, want 2", n)
	}
	data, err = os.ReadFile(".todo/notes/TSK-002.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "---\ncategory: areas\n") {
		t.Errorf("note = %q, want the declared park block", string(data))
	}

	// A body contradicting the requested disposition is refused, not stacked.
	_, _, err = runApp(t, []string{"add", "-s", "conflict", "--kind", "work-order", "--note-content", "---\nkind: record\n---\n\nprose"})
	if err == nil {
		t.Fatal("conflicting disposition: expected error")
	}
	if !strings.Contains(err.Error(), "declares a record disposition") {
		t.Errorf("error = %q, want the conflict to be named", err.Error())
	}
	if _, err := os.Stat(".todo/notes/TSK-003.md"); !os.IsNotExist(err) {
		t.Errorf("conflicting add wrote a note file: %v", err)
	}
}

func TestPickupOmitsNoteCoordinate(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-s", "with a note", "-n", "--note-content", "body"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runApp(t, []string{"pickup", "TSK-001"})
	if err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if !strings.Contains(out, "- [o] [TSK-001]") {
		t.Errorf("pickup output = %q, want the claimed line", out)
	}
	if strings.Contains(out, "note:") || strings.Contains(out, ".todo/notes") {
		t.Errorf("pickup output = %q, want no note coordinate; the note comes from detail", out)
	}

	out, _, err = runApp(t, []string{"detail", "TSK-001", "--json"})
	if err != nil {
		t.Fatalf("detail --json: %v", err)
	}
	if !strings.Contains(out, `"note_path"`) || !strings.Contains(out, "TSK-001.md") {
		t.Errorf("detail --json = %q, want the note path", out)
	}
}

func TestHandoffAndResumeCLI(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-p", "high", "-s", "resume the work", "-n", "--note-content", "brief"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Only the holder can hand off, so an open task is refused.
	if _, _, err := runApp(t, []string{"handoff", "TSK-001", "--note-content", "state"}); err == nil {
		t.Fatal("handoff on an open task: expected a guard error")
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-001"}); err != nil {
		t.Fatalf("pickup: %v", err)
	}

	body := "## State\n\nhalf way\n\n### Verify\n\nmake test"
	out, _, err := runApp(t, []string{"handoff", "TSK-001", "--note-content", body})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if !strings.Contains(out, "TSK-001.md: handoff written (") {
		t.Errorf("handoff output = %q, want the note path and the write", out)
	}

	note, err := os.ReadFile(filepath.Join(".todo", "notes", "TSK-001.md"))
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if !strings.Contains(string(note), "## Handoff (") || !strings.Contains(string(note), "half way") {
		t.Errorf("note = %q, want the handoff section", string(note))
	}
	if !strings.Contains(string(note), "## Handoff (") || strings.Count(string(note), "kind: record") != 1 {
		t.Errorf("note = %q, want exactly one frontmatter block", string(note))
	}

	// resume <ref> hands the section back, verify commands included.
	out, _, err = runApp(t, []string{"resume", "TSK-001"})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	for _, want := range []string{"TSK-001 (priority: high)", "## Handoff (", "half way", "make test"} {
		if !strings.Contains(out, want) {
			t.Errorf("resume output = %q, want %q", out, want)
		}
	}

	out, _, err = runApp(t, []string{"resume", "TSK-001", "--json"})
	if err != nil {
		t.Fatalf("resume --json: %v", err)
	}
	if !strings.Contains(out, `"id": "TSK-001"`) || !strings.Contains(out, `"verify": "make test"`) {
		t.Errorf("resume --json = %q, want the handoff and its verify commands", out)
	}

	// The bare form is the repo-wide re-entry view.
	out, _, err = runApp(t, []string{"resume"})
	if err != nil {
		t.Fatalf("resume (bare): %v", err)
	}
	if !strings.Contains(out, "TSK-001") || !strings.Contains(out, "resume the work") {
		t.Errorf("resume output = %q, want the in-flight task listed", out)
	}

	out, _, err = runApp(t, []string{"resume", "--json"})
	if err != nil {
		t.Fatalf("resume --json (bare): %v", err)
	}
	if !strings.Contains(out, `"schema_version": 1`) || !strings.Contains(out, `"handoffs"`) {
		t.Errorf("resume --json = %q, want the versioned envelope", out)
	}

	// A handoff --json reports the write without the note text.
	out, _, err = runApp(t, []string{"handoff", "TSK-001", "--note-content", "again", "--json"})
	if err != nil {
		t.Fatalf("handoff --json: %v", err)
	}
	if !strings.Contains(out, `"replaced": true`) || !strings.Contains(out, "TSK-001.md") {
		t.Errorf("handoff --json = %q, want the replacement reported", out)
	}

	// Replacing keeps one section.
	note, err = os.ReadFile(filepath.Join(".todo", "notes", "TSK-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(note), "## Handoff"); n != 1 {
		t.Errorf("note = %q, want one handoff section, got %d", string(note), n)
	}
	if strings.Contains(string(note), "half way") {
		t.Errorf("note = %q, want the first handoff replaced", string(note))
	}

	// Guards.
	if _, _, err := runApp(t, []string{"handoff", "TSK-001", "--note-content", "x", "--note-file", "y"}); err == nil {
		t.Error("handoff with both sources: expected a mutual-exclusion error")
	}
	if _, _, err := runApp(t, []string{"handoff", "TSK-001"}); err == nil {
		t.Error("handoff with no text: expected an error")
	}
	if _, _, err := runApp(t, []string{"resume", "TSK-001", "--all"}); err == nil {
		t.Error("resume --all with a ref: expected an error")
	}
	if _, _, err := runApp(t, []string{"resume", "TSK-099"}); err == nil {
		t.Error("resume of an unknown ref: expected an error")
	}

	// An in-flight task with no handoff yet is a clear miss, not a silent empty.
	if _, _, err := runApp(t, []string{"add", "-s", "no handoff yet"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-002"}); err != nil {
		t.Fatalf("pickup TSK-002: %v", err)
	}
	_, _, err = runApp(t, []string{"resume", "TSK-002"})
	if err == nil {
		t.Fatal("resume of a task with no handoff: expected an error")
	}
	if !strings.Contains(err.Error(), "no handoff recorded for TSK-002") {
		t.Errorf("error = %q, want the missing handoff named", err.Error())
	}
}

func TestHandoffCreatesNoteForNotelessTask(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-s", "no note at all"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-001"}); err != nil {
		t.Fatalf("pickup: %v", err)
	}

	out, _, err := runApp(t, []string{"handoff", "TSK-001", "--note-content", "state"})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if !strings.Contains(out, "handoff written (new note)") {
		t.Errorf("handoff output = %q, want the new note reported", out)
	}

	out, _, err = runApp(t, []string{"detail", "TSK-001", "--json"})
	if err != nil {
		t.Fatalf("detail --json: %v", err)
	}
	if !strings.Contains(out, `"disposition": "record"`) || !strings.Contains(out, `"note_exists": true`) {
		t.Errorf("detail --json = %q, want a record note on disk", out)
	}
}

func TestInitRegistersRepo(t *testing.T) {
	dir := setupGitRepo(t)
	dir, _ = filepath.EvalSymlinks(dir)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	entries, err := registry.Load(os.Getenv("TODO_REGISTRY"))
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("registry entries = %d, want 1", len(entries))
	}
	if entries[0].Path != dir {
		t.Errorf("path = %q, want %q", entries[0].Path, dir)
	}
}

func TestListAll(t *testing.T) {
	repoA := setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init repo a: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "repo a task"}); err != nil {
		t.Fatalf("add repo a: %v", err)
	}

	repoB := t.TempDir()
	if err := exec.Command("git", "init", repoB).Run(); err != nil {
		t.Fatalf("git init repo b: %v", err)
	}
	t.Chdir(repoB)
	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init repo b: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "repo b task"}); err != nil {
		t.Fatalf("add repo b: %v", err)
	}

	t.Chdir(repoA)
	out, _, err := runApp(t, []string{"list", "--all", "--json"})
	if err != nil {
		t.Fatalf("list --all: %v", err)
	}

	var envelope struct {
		SchemaVersion int        `json:"schema_version"`
		Tasks         []jsonTask `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if len(envelope.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(envelope.Tasks))
	}

	sums := make(map[string]bool)
	for _, task := range envelope.Tasks {
		sums[task.Summary] = true
		if task.RepoPath == "" {
			t.Errorf("task %s missing repo_path", task.ID)
		}
		if task.Disposition == "" {
			t.Errorf("task %s missing disposition", task.ID)
		}
	}
	if !sums["repo a task"] || !sums["repo b task"] {
		t.Errorf("summaries = %v", sums)
	}
}

func TestClearLocal(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "--kind", "work-order", "-n", "wo task"}); err != nil {
		t.Fatalf("add work-order: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-n", "--category", "areas", "--synopsis", "park task", "park task"}); err != nil {
		t.Fatalf("add park: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "no note task"}); err != nil {
		t.Fatalf("add no-note: %v", err)
	}

	if _, _, err := runApp(t, []string{"pickup", "TSK-001"}); err != nil {
		t.Fatalf("pickup TSK-001: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-002"}); err != nil {
		t.Fatalf("pickup TSK-002: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-003"}); err != nil {
		t.Fatalf("pickup TSK-003: %v", err)
	}

	if _, _, err := runApp(t, []string{"complete", "TSK-001"}); err != nil {
		t.Fatalf("complete TSK-001: %v", err)
	}
	if _, _, err := runApp(t, []string{"complete", "TSK-002"}); err != nil {
		t.Fatalf("complete TSK-002: %v", err)
	}
	if _, _, err := runApp(t, []string{"complete", "TSK-003"}); err != nil {
		t.Fatalf("complete TSK-003: %v", err)
	}

	out, _, err := runApp(t, []string{"clear"})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !strings.Contains(out, "TSK-001") {
		t.Errorf("clear output missing TSK-001: %q", out)
	}
	if !strings.Contains(out, "TSK-002") {
		t.Errorf("clear output missing TSK-002: %q", out)
	}
	if !strings.Contains(out, "removed (no note)") || !strings.Contains(out, "TSK-003") {
		t.Errorf("clear output missing no-note removal for TSK-003: %q", out)
	}

	if _, err := os.Stat(".todo/notes/TSK-001.md"); !os.IsNotExist(err) {
		t.Errorf("work-order note not deleted: %v", err)
	}
	if _, err := os.Stat(".todo/notes/TSK-002.md"); err != nil {
		t.Errorf("park note deleted: %v", err)
	}

	out, _, err = runApp(t, []string{"list"})
	if err != nil {
		t.Fatalf("list after clear: %v", err)
	}
	if strings.Contains(out, "TSK-001") || strings.Contains(out, "TSK-002") || strings.Contains(out, "TSK-003") {
		t.Errorf("cleared tasks still listed: %q", out)
	}
}

func TestClearAll(t *testing.T) {
	repoA := setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init repo a: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "--kind", "work-order", "-n", "repo a done"}); err != nil {
		t.Fatalf("add repo a: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-001"}); err != nil {
		t.Fatalf("pickup repo a: %v", err)
	}
	if _, _, err := runApp(t, []string{"complete", "TSK-001"}); err != nil {
		t.Fatalf("complete repo a: %v", err)
	}

	repoB := t.TempDir()
	if err := exec.Command("git", "init", repoB).Run(); err != nil {
		t.Fatalf("git init repo b: %v", err)
	}
	t.Chdir(repoB)
	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init repo b: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "repo b no note"}); err != nil {
		t.Fatalf("add repo b: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-001"}); err != nil {
		t.Fatalf("pickup repo b: %v", err)
	}
	if _, _, err := runApp(t, []string{"complete", "TSK-001"}); err != nil {
		t.Fatalf("complete repo b: %v", err)
	}

	t.Chdir(repoA)
	out, _, err := runApp(t, []string{"clear", "--all"})
	if err != nil {
		t.Fatalf("clear --all: %v", err)
	}
	if !strings.Contains(out, "removed work-order") || !strings.Contains(out, "TSK-001") {
		t.Errorf("clear --all missing repo a work-order: %q", out)
	}
	if !strings.Contains(out, "removed (no note)") || !strings.Contains(out, "TSK-001") {
		t.Errorf("clear --all missing repo b no-note: %q", out)
	}
}

func TestDoctorReportsAndFixes(t *testing.T) {
	repoA := setupGitRepo(t)
	repoA, _ = filepath.EvalSymlinks(repoA)
	registryPath := os.Getenv("TODO_REGISTRY")

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init repo a: %v", err)
	}

	nested := filepath.Join(repoA, "nested")
	if err := exec.Command("git", "init", nested).Run(); err != nil {
		t.Fatalf("git init nested: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(nested, ".todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".todo", "todo.md"), []byte("---\n---\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested, _ = filepath.EvalSymlinks(nested)

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	entries := []registry.Entry{
		{Path: repoA},
		{Path: missing},
	}
	if err := registry.Save(registryPath, entries); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	out, _, err := runApp(t, []string{"doctor", "--all", "--adoption", "--depth", "2"})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(out, "stale\t"+missing) {
		t.Errorf("doctor output missing stale entry: %q", out)
	}
	if !strings.Contains(out, "unregistered\t"+nested) {
		t.Errorf("doctor output missing unregistered entry: %q", out)
	}
	// Both repos hold a real todo.md, so both trees are outside every backup.
	for _, want := range []string{repoA, nested} {
		if !strings.Contains(out, "unadopted\t"+want) {
			t.Errorf("doctor output missing unadopted repo %s: %q", want, out)
		}
	}
	if !strings.Contains(out, "lnk project init") {
		t.Errorf("doctor output missing the corrective step: %q", out)
	}

	_, _, err = runApp(t, []string{"doctor", "--all", "--fix", "--depth", "2"})
	if err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}

	fixed, err := registry.Load(registryPath)
	if err != nil {
		t.Fatalf("load fixed registry: %v", err)
	}
	if len(fixed) != 2 {
		t.Fatalf("fixed entries = %d, want 2", len(fixed))
	}
	paths := make(map[string]bool)
	for _, e := range fixed {
		paths[e.Path] = true
	}
	if !paths[repoA] || !paths[nested] || paths[missing] {
		t.Errorf("fixed paths = %v", paths)
	}
}

func TestDoctorFlagsUnadoptedRepo(t *testing.T) {
	repo := setupGitRepo(t)
	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// doctor reports the canonical path, as the registry stores it.
	wantRepo, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("resolve repo: %v", err)
	}

	// init writes a real todo.md, but adoption is a host-side convention: the
	// default doctor run says nothing about it.
	out, _, err := runApp(t, []string{"doctor"})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if strings.Contains(out, "unadopted\t") {
		t.Errorf("default doctor run reported adoption: %q", out)
	}
	if !strings.Contains(out, "summary: 1 ok, 0 stale, 0 unregistered\n") {
		t.Errorf("default doctor summary = %q, want the registry-only counts", out)
	}

	out, _, err = runApp(t, []string{"doctor", "--json"})
	if err != nil {
		t.Fatalf("doctor --json: %v", err)
	}
	if strings.Contains(out, `"adopted"`) {
		t.Errorf("default doctor --json reported adoption: %q", out)
	}

	// --adoption is the opt-in check.
	out, _, err = runApp(t, []string{"doctor", "--adoption"})
	if err != nil {
		t.Fatalf("doctor --adoption: %v", err)
	}
	if !strings.Contains(out, "unadopted\t"+wantRepo) {
		t.Errorf("doctor output missing unadopted repo: %q", out)
	}
	if !strings.Contains(out, "lnk project init") {
		t.Errorf("doctor output missing the corrective step: %q", out)
	}
	// The repo init registered is the repo doctor scans, not an unregistered
	// twin reached through a symlinked path.
	if strings.Contains(out, "unregistered\t") {
		t.Errorf("doctor reported the current repo as unregistered: %q", out)
	}

	out, _, err = runApp(t, []string{"doctor", "--adoption", "--json"})
	if err != nil {
		t.Fatalf("doctor --adoption --json: %v", err)
	}
	var got struct {
		SchemaVersion int `json:"schema_version"`
		Repos         []struct {
			Path       string `json:"path"`
			Registered bool   `json:"registered"`
			Adopted    *bool  `json:"adopted"`
			Action     string `json:"action"`
		} `json:"repos"`
		Summary struct {
			Unadopted *int `json:"unadopted"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode doctor --json: %v\n%s", err, out)
	}
	if len(got.Repos) != 1 || got.Repos[0].Path != wantRepo {
		t.Fatalf("doctor --json repos = %+v, want the current repo", got.Repos)
	}
	if !got.Repos[0].Registered {
		t.Error("doctor --json reported the registered repo as unregistered")
	}
	if got.Repos[0].Adopted == nil || *got.Repos[0].Adopted {
		t.Errorf("doctor --json adopted = %v, want false", got.Repos[0].Adopted)
	}
	if got.Repos[0].Action == "" || got.Summary.Unadopted == nil || *got.Summary.Unadopted != 1 {
		t.Errorf("doctor --json = %+v, want the corrective action and one unadopted repo", got)
	}

	// Adopt the tree: the store owns todo.md, the repo holds a link to it. The
	// check must go quiet on an adopted repo.
	store := filepath.Join(t.TempDir(), "todo.md")
	if err := os.Rename(filepath.Join(repo, ".todo", "todo.md"), store); err != nil {
		t.Fatalf("move todo.md into the store: %v", err)
	}
	if err := os.Symlink(store, filepath.Join(repo, ".todo", "todo.md")); err != nil {
		t.Fatalf("symlink todo.md: %v", err)
	}

	out, _, err = runApp(t, []string{"doctor", "--adoption"})
	if err != nil {
		t.Fatalf("doctor --adoption adopted: %v", err)
	}
	if strings.Contains(out, "unadopted\t") {
		t.Errorf("adopted repo reported as unadopted: %q", out)
	}
	// A checked zero is reported, so the caller can tell it from "not checked".
	if !strings.Contains(out, "0 unadopted") {
		t.Errorf("doctor summary missing zero unadopted: %q", out)
	}
}

func TestArchiveCommand(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-n", "--note-content", "the detail body", "retire this"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-001"}); err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if _, _, err := runApp(t, []string{"complete", "TSK-001"}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	out, _, err := runApp(t, []string{"archive", "TSK-001"})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !strings.Contains(out, "archived: ") || !strings.Contains(out, ".todo/archive/TSK-001.md") {
		t.Errorf("archive output = %q, want archived path", out)
	}

	data, err := os.ReadFile(".todo/archive/TSK-001.md")
	if err != nil {
		t.Fatalf("read archived note: %v", err)
	}
	for _, want := range []string{"kind: record", "synopsis: retire this", "the detail body"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("archived note missing %q:\n%s", want, string(data))
		}
	}

	// The line and the source note entry are gone.
	if _, err := os.Stat(".todo/notes/TSK-001.md"); !os.IsNotExist(err) {
		t.Errorf("source note still present: %v", err)
	}
	out, _, err = runApp(t, []string{"list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(out, "retire this") {
		t.Errorf("default list still shows the archived task: %q", out)
	}

	// The compact archived view surfaces it.
	out, _, err = runApp(t, []string{"list", "--archive"})
	if err != nil {
		t.Fatalf("list --archive: %v", err)
	}
	if !strings.Contains(out, "TSK-001.md") || !strings.Contains(out, "retire this") {
		t.Errorf("list --archive output = %q", out)
	}
}

func TestListArchiveJSON(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Nothing archived yet: an empty envelope, not an error.
	out, _, err := runApp(t, []string{"list", "--archive", "--json"})
	if err != nil {
		t.Fatalf("list --archive --json: %v", err)
	}
	var empty jsonArchiveEnvelope
	if err := json.Unmarshal([]byte(out), &empty); err != nil {
		t.Fatalf("parse empty archive json: %v", err)
	}
	if empty.SchemaVersion != 1 || len(empty.Archived) != 0 {
		t.Errorf("empty envelope = %+v", empty)
	}

	if _, _, err := runApp(t, []string{"add", "-n", "--note-content", "body", "archive me"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-001"}); err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if _, _, err := runApp(t, []string{"complete", "TSK-001"}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, _, err := runApp(t, []string{"archive", "TSK-001"}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	out, _, err = runApp(t, []string{"list", "--archive", "--json"})
	if err != nil {
		t.Fatalf("list --archive --json: %v", err)
	}
	var env jsonArchiveEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parse archive json: %v", err)
	}
	if env.SchemaVersion != 1 || len(env.Archived) != 1 {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Archived[0].Name != "TSK-001.md" || env.Archived[0].Synopsis != "archive me" {
		t.Errorf("archived entry = %+v", env.Archived[0])
	}
}

func TestArchiveCommandRequiresSynopsis(t *testing.T) {
	setupGitRepo(t)

	if _, _, err := runApp(t, []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := runApp(t, []string{"add", "-n", "--kind", "work-order", "--note-content", "body", "work order"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := runApp(t, []string{"pickup", "TSK-001"}); err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if _, _, err := runApp(t, []string{"complete", "TSK-001"}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if _, _, err := runApp(t, []string{"archive", "TSK-001"}); err == nil {
		t.Fatal("archive without a synopsis: expected error")
	}

	if _, _, err := runApp(t, []string{"archive", "TSK-001", "--synopsis", "why it matters", "--name", "retired-why"}); err != nil {
		t.Fatalf("archive --synopsis: %v", err)
	}
	data, err := os.ReadFile(".todo/archive/retired-why.md")
	if err != nil {
		t.Fatalf("read archived note: %v", err)
	}
	if !strings.Contains(string(data), "synopsis: why it matters") {
		t.Errorf("archived note missing asserted synopsis:\n%s", string(data))
	}
}
