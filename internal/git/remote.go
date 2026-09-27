package git

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// ParseRemote extracts a host and owner from common git remote URL formats.
// It returns empty strings for local paths or unrecognised URLs.
func ParseRemote(raw string) (host, owner string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	// SSH short form: git@github.com:owner/repo.git
	if before, after, ok := strings.Cut(raw, "@"); ok && strings.HasPrefix(before, "git") {
		if hostPath, _, ok := strings.Cut(after, ":"); ok {
			parts := strings.SplitN(after, ":", 3)
			if len(parts) == 2 {
				host = hostPath
				owner = ownerFromPath(parts[1])
				return host, owner
			}
		}
	}

	// Standard URL form.
	u, err := url.Parse(raw)
	if err == nil && u.Host != "" {
		return u.Host, ownerFromPath(u.Path)
	}

	// SCP-like form without scheme: github.com:owner/repo.git
	if hostPath, repoPath, ok := strings.Cut(raw, ":"); ok && !isLocalPath(raw) {
		return hostPath, ownerFromPath(repoPath)
	}

	return "", ""
}

// isLocalPath reports whether raw is a local filesystem path rather than a
// remote URL. Windows drive-letter paths are matched explicitly so the check
// does not depend on the OS the binary runs on.
func isLocalPath(raw string) bool {
	if path.IsAbs(raw) || filepath.IsAbs(raw) {
		return true
	}
	if len(raw) >= 2 && raw[1] == ':' {
		c := raw[0]
		return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
	}
	return false
}

func ownerFromPath(p string) string {
	p = strings.TrimSuffix(p, ".git")
	p = strings.Trim(p, "/")
	parts := strings.Split(p, "/")
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return ""
	default:
		return parts[len(parts)-2]
	}
}
