package ingestion

import "testing"

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
