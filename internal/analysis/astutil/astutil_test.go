package astutil

import (
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
)

func parseFirstCall(t *testing.T, src string) *sitter.Node {
	t.Helper()
	tree, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var call *sitter.Node
	Walk(tree.RootNode(), func(n *sitter.Node) bool {
		if call != nil {
			return false
		}
		if n.Type() == "call" {
			call = n
			return false
		}
		return true
	})
	if call == nil {
		t.Fatal("no call node found")
	}
	return call
}

func TestKwargValue(t *testing.T) {
	cases := []struct {
		name        string
		code        string
		kwarg       string
		wantValue   string
		wantPresent bool
	}{
		{"absent", "requests.get(url)", "timeout", "", false},
		{"none", "requests.get(url, timeout=None)", "timeout", "None", true},
		{"int", "requests.get(url, timeout=10)", "timeout", "10", true},
		// A kwarg nested inside an argument's own call must NOT be attributed to
		// the outer call — otherwise "does this call set timeout?" answers true
		// for a timeout that belongs to build(...), a false negative for
		// timeout/retry findings. parseFirstCall returns the outer call here.
		{"nested-not-attributed", "requests.get(url, headers=build(timeout=5))", "timeout", "", false},
		{"direct-wins-over-nested", "requests.get(url, timeout=10, headers=build(timeout=5))", "timeout", "10", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			call := parseFirstCall(t, c.code)
			gotValue, gotPresent := KwargValue(call, []byte(c.code), c.kwarg)
			if gotValue != c.wantValue || gotPresent != c.wantPresent {
				t.Errorf("KwargValue = (%q, %v), want (%q, %v)",
					gotValue, gotPresent, c.wantValue, c.wantPresent)
			}
		})
	}
}
