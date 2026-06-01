package ingestion

import (
	"strings"
	"testing"
)

func TestValidateRemoteScheme(t *testing.T) {
	cases := []struct {
		target  string
		wantErr bool
	}{
		{"https://github.com/owner/repo", false},
		{"http://example.com/x.git", false},
		{"ssh://git@github.com/owner/repo.git", false},
		{"git@github.com:owner/repo.git", false},
		{"git://example.com/repo.git", true}, // cleartext/unauthenticated
		{"ftp://example.com/repo", true},     // unsupported transport
	}
	for _, c := range cases {
		if err := validateRemoteScheme(c.target); (err != nil) != c.wantErr {
			t.Errorf("validateRemoteScheme(%q) err=%v, wantErr=%v", c.target, err, c.wantErr)
		}
	}
}

func TestResolve_RejectsUnsafeScheme(t *testing.T) {
	// git:// parses as remote (scheme+host) so it reaches the scheme gate, which
	// must reject it before any clone is attempted.
	_, err := Resolve("git://example.com/repo.git", nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported remote transport") {
		t.Fatalf("Resolve(git://...) = %v, want unsupported-transport error", err)
	}
}

func TestIsRemote(t *testing.T) {
	cases := []struct {
		target string
		want   bool
	}{
		{"https://github.com/owner/repo", true},
		{"http://example.com/x/y.git", true},
		{"git@github.com:owner/repo.git", true},
		{"./relative/path", false},
		{"/abs/unix/path", false},
		{`C:\Users\me\project`, false},
		{"C:/Users/me/project", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsRemote(c.target); got != c.want {
			t.Errorf("IsRemote(%q) = %v, want %v", c.target, got, c.want)
		}
	}
}
