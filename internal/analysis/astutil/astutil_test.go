package astutil

import (
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
)

func TestNodeText_OutOfRangeIsSafe(t *testing.T) {
	// Regression (TR-160): a node parsed from a longer source, then sliced
	// against a shorter/mismatched src, must clamp rather than panic the scan.
	tree, err := Parse([]byte("x = 12345"))
	if err != nil {
		t.Fatal(err)
	}
	root := tree.RootNode()
	short := []byte("x") // shorter than root.EndByte()
	if got := NodeText(root, short); len(got) > len(short) {
		t.Errorf("NodeText returned %q (len %d) > len(src) %d; not clamped", got, len(got), len(short))
	}
	if NodeText(root, nil) != "" {
		t.Error("NodeText over nil src should be empty")
	}
	if NodeText(nil, []byte("abc")) != "" {
		t.Error("nil node should return empty")
	}
}

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

func parseFirstFunc(t *testing.T, src string) *sitter.Node {
	t.Helper()
	tree, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var fn *sitter.Node
	Walk(tree.RootNode(), func(n *sitter.Node) bool {
		if fn != nil {
			return false
		}
		if n.Type() == "function_definition" {
			fn = n
			return false
		}
		return true
	})
	if fn == nil {
		t.Fatal("no function_definition found")
	}
	return fn
}

func TestFunctionParams(t *testing.T) {
	cases := []struct {
		name string
		code string
		want []string
	}{
		{"plain", "def f(a, b):\n    pass", []string{"a", "b"}},
		{"typed-and-default", "def f(a: int, b=3):\n    pass", []string{"a", "b"}},
		// *args / **kwargs splats: the bare name must be surfaced (sans * / **),
		// so a rule keying on a parameter named "kwargs" matches a real **kwargs.
		{"var-splat", "def f(*args):\n    pass", []string{"args"}},
		{"kw-splat", "def f(**kwargs):\n    pass", []string{"kwargs"}},
		{"mixed-splats", "def f(a, *args, **kwargs):\n    pass", []string{"a", "args", "kwargs"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fn := parseFirstFunc(t, c.code)
			got := FunctionParams(fn, []byte(c.code))
			if len(got) != len(c.want) {
				t.Fatalf("FunctionParams = %v, want %v", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("FunctionParams = %v, want %v", got, c.want)
				}
			}
		})
	}
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
