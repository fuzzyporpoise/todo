package git

import (
	"testing"
)

func TestParseRemote(t *testing.T) {
	tests := []struct {
		name      string
		remote    string
		wantHost  string
		wantOwner string
	}{
		{"ssh short", "git@github.com:polymorcodeus/park.git", "github.com", "polymorcodeus"},
		{"https", "https://github.com/polymorcodeus/park.git", "github.com", "polymorcodeus"},
		{"https no suffix", "https://github.com/polymorcodeus/todo", "github.com", "polymorcodeus"},
		{"scp-like", "github.com:owner/repo.git", "github.com", "owner"},
		{"empty", "", "", ""},
		{"local path", "/code/repo", "", ""},
		{"local path with colon", "/code/re:po", "", ""},
		{"windows drive path", `C:\code\repo`, "", ""},
		{"windows drive slash path", "C:/code/repo", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, owner := ParseRemote(tc.remote)
			if host != tc.wantHost || owner != tc.wantOwner {
				t.Errorf("ParseRemote(%q) = (%q, %q), want (%q, %q)",
					tc.remote, host, owner, tc.wantHost, tc.wantOwner)
			}
		})
	}
}
