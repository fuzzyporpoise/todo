// Package registry manages the machine-local cache of tracked todo repos.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry records a single tracked repository.
type Entry struct {
	Path     string `json:"path"`
	Project  string `json:"project,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Host     string `json:"host,omitempty"`
	Owner    string `json:"owner,omitempty"`
	LastSeen string `json:"last_seen"`
}

// DefaultPath returns the default registry file path:
// $XDG_CACHE_HOME/todo/registry.json, or ~/.cache/todo/registry.json.
// If the home directory cannot be determined, it falls back to the OS
// temporary directory so the registry is never written to an arbitrary
// working directory.
func DefaultPath() string {
	if cacheHome := os.Getenv("XDG_CACHE_HOME"); cacheHome != "" {
		return filepath.Join(cacheHome, "todo", "registry.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "todo-registry.json")
	}
	return filepath.Join(home, ".cache", "todo", "registry.json")
}

// Load reads the registry from path. A missing file is treated as an empty
// registry, not an error.
func Load(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return entries, nil
}

// Save writes entries to path atomically.
func Save(path string, entries []Entry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write registry temp: %w", err)
	}
	return os.Rename(tmp, path)
}

// Upsert adds or updates an entry keyed by absolute, cleaned path.
func Upsert(entries []Entry, path, repo, project string) []Entry {
	path = filepath.Clean(path)
	now := time.Now().UTC().Format(time.RFC3339)

	for i := range entries {
		if filepath.Clean(entries[i].Path) == path {
			entries[i].Project = project
			entries[i].Repo = repo
			entries[i].LastSeen = now
			return entries
		}
	}

	return append(entries, Entry{
		Path:     path,
		Project:  project,
		Repo:     repo,
		LastSeen: now,
	})
}

// DropMissing returns the kept entries and the stale entries whose directories
// no longer exist. A stat error other than "not exist" is returned so the
// caller can distinguish a missing repo from a permission problem.
func DropMissing(entries []Entry) (kept, stale []Entry, err error) {
	for _, e := range entries {
		if _, statErr := os.Stat(e.Path); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				stale = append(stale, e)
				continue
			}
			return nil, nil, fmt.Errorf("stat %s: %w", e.Path, statErr)
		}
		kept = append(kept, e)
	}
	return kept, stale, nil
}

// FindUnregistered walks roots up to maxDepth and returns the absolute,
// cleaned repo paths that contain .todo/todo.md but are not already in entries.
// A maxDepth <= 0 means no limit.
func FindUnregistered(roots []string, maxDepth int, entries []Entry) ([]string, error) {
	known := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		known[filepath.Clean(e.Path)] = struct{}{}
	}

	seen := make(map[string]struct{})
	var found []string

	for _, root := range roots {
		root = filepath.Clean(root)
		info, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", root, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("not a directory: %s", root)
		}

		if err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip paths we cannot read
			}
			if !d.IsDir() {
				return nil
			}

			base := filepath.Base(p)
			// Skip the .todo directory itself (the parent is the repo root) and
			// any hidden directories to avoid unreadable caches/dotfiles.
			if base == ".todo" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}

			rel, err := filepath.Rel(root, p)
			if err != nil {
				return nil
			}
			depth := strings.Count(rel, string(os.PathSeparator))
			if maxDepth > 0 && depth > maxDepth {
				return filepath.SkipDir
			}

			if !hasTodoFile(p) {
				return nil
			}

			clean := filepath.Clean(p)
			if _, ok := known[clean]; ok {
				return nil
			}
			if _, ok := seen[clean]; !ok {
				seen[clean] = struct{}{}
				found = append(found, clean)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk %s: %w", root, err)
		}
	}

	sort.Strings(found)
	return found, nil
}

func hasTodoFile(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".todo", "todo.md"))
	return err == nil
}
