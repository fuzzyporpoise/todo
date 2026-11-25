// Package git shells out to the git CLI for repo metadata.
package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RepoRoot resolves the root of the current git repo. When the current
// directory is a bare repository or the clone root of a worktree layout (for
// example gwt/r2d, where .git is a pointer file to a bare .bare), it returns an
// actionable error naming that case instead of the raw git plumbing failure.
func RepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", repoRootError(err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", errors.New("git repo root resolved to empty string")
	}
	return root, nil
}

// repoRootError explains a failed --show-toplevel. A bare repository or a clone
// root resolves as bare, so name the worktree remedy; any other failure keeps
// the raw plumbing error.
func repoRootError(revParseErr error) error {
	if !isBareRepository() {
		return fmt.Errorf("git rev-parse --show-toplevel: %w", revParseErr)
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return fmt.Errorf("not a git working tree: %s is a clone root (bare repository); run from a worktree (for example main/): %w", cwd, revParseErr)
}

// isBareRepository reports whether git resolves the current directory to a bare
// repository. It is false when git cannot resolve a repository at all.
func isBareRepository() bool {
	out, err := exec.Command("git", "rev-parse", "--is-bare-repository").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// RemoteURL returns the URL of the named remote with any trailing .git
// stripped, or "" when the remote is unavailable (optional metadata).
func RemoteURL(remote string) string {
	out, err := exec.Command("git", "remote", "get-url", remote).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSpace(string(out)), ".git")
}

// RemoteURLAt returns the URL of the named remote in dir, or "" when the
// remote is unavailable.
func RemoteURLAt(dir, remote string) string {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", remote).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSpace(string(out)), ".git")
}
