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
		{"ssh short", "git@github.com:fuzzyporpoise/park.git", "github.com", "fuzzyporpoise"},
		{"https", "https://github.com/fuzzyporpoise/park.git", "github.com", "fuzzyporpoise"},
		{"https no suffix", "https://github.com/fuzzyporpoise/todo", "github.com", "fuzzyporpoise"},
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
