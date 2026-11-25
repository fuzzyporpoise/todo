package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newWorktreeLayout builds a gwt/r2d-style clone root: a bare .bare repository,
// a .git pointer file at the root aimed at it, and a main/ worktree. The root
// is not a working tree; main/ is.
func newWorktreeLayout(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	src := filepath.Join(base, "src")
	runGit(t, "", "init", "-q", src)
	runGit(t, src, "config", "user.email", "t@t")
	runGit(t, src, "config", "user.name", "t")
	runGit(t, src, "commit", "-q", "--allow-empty", "-m", "init")

	proj := filepath.Join(base, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	runGit(t, "", "clone", "--bare", "-q", src, filepath.Join(proj, ".bare"))
	if err := os.WriteFile(filepath.Join(proj, ".git"), []byte("gitdir: ./.bare\n"), 0o644); err != nil {
		t.Fatalf("write .git pointer: %v", err)
	}
	runGit(t, filepath.Join(proj, ".bare"), "worktree", "add", "-q", filepath.Join(proj, "main"))
	return proj
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func TestRepoRootCloneRootNamesCase(t *testing.T) {
	proj := newWorktreeLayout(t)
	t.Chdir(proj)

	root, err := RepoRoot()
	if err == nil {
		t.Fatalf("RepoRoot() = %q, want error at clone root", root)
	}
	msg := err.Error()
	for _, want := range []string{"not a git working tree", "clone root", "main/"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestRepoRootInsideWorktree(t *testing.T) {
	proj := newWorktreeLayout(t)
	main := filepath.Join(proj, "main")
	t.Chdir(main)

	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot() in worktree: %v", err)
	}
	want, err := filepath.EvalSymlinks(main)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	if root != want {
		t.Errorf("RepoRoot() = %q, want %q", root, want)
	}
}

func TestRepoRootNonRepoKeepsBehavior(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	root, err := RepoRoot()
	if err == nil {
		t.Fatalf("RepoRoot() = %q, want error outside a repo", root)
	}
	msg := err.Error()
	if !strings.Contains(msg, "git rev-parse --show-toplevel") {
		t.Errorf("error %q does not keep the raw plumbing error", msg)
	}
	if strings.Contains(msg, "clone root") {
		t.Errorf("error %q names the clone-root case outside a repo", msg)
	}
}
