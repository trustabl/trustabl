// Package astutil wraps tree-sitter with a tiny ergonomic surface so the
// detector code reads like AST queries, not raw C bindings.
//
// All offsets are tree-sitter's 0-indexed; conversion to 1-indexed line
// numbers happens at the boundary (NodeLine).
package astutil

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

// Parse parses Python source into a tree. Returns the tree and the original
// source — detectors need both (the tree gives structure; the source gives
// the actual byte content of identifiers).
func Parse(src []byte) (*sitter.Tree, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	return parser.ParseCtx(context.Background(), nil, src)
}

// NodeText returns the source bytes a node spans, as a string.
func NodeText(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

// NodeLine returns the 1-indexed start line of a node.
func NodeLine(n *sitter.Node) int {
	if n == nil {
		return 0
	}
	return int(n.StartPoint().Row) + 1
}

// NodeEndLine returns the 1-indexed end line.
func NodeEndLine(n *sitter.Node) int {
	if n == nil {
		return 0
	}
	return int(n.EndPoint().Row) + 1
}

// Walk performs a pre-order traversal, calling fn on each named node. Return
// false from fn to stop descending into that subtree.
func Walk(root *sitter.Node, fn func(*sitter.Node) bool) {
	if root == nil {
		return
	}
	walk(root, fn)
}

func walk(n *sitter.Node, fn func(*sitter.Node) bool) {
	if !fn(n) {
		return
	}
	count := int(n.NamedChildCount())
	for i := 0; i < count; i++ {
		walk(n.NamedChild(i), fn)
	}
}

// FindAll collects every named node whose Type() matches one of the given types.
func FindAll(root *sitter.Node, types ...string) []*sitter.Node {
	want := make(map[string]struct{}, len(types))
	for _, t := range types {
		want[t] = struct{}{}
	}
	var out []*sitter.Node
	Walk(root, func(n *sitter.Node) bool {
		if _, ok := want[n.Type()]; ok {
			out = append(out, n)
		}
		return true
	})
	return out
}

// Decorators returns the decorator nodes attached to a `decorated_definition`,
// or nil if n is a bare function/class.
func Decorators(n *sitter.Node) []*sitter.Node {
	if n == nil || n.Type() != "decorated_definition" {
		return nil
	}
	var out []*sitter.Node
	count := int(n.NamedChildCount())
	for i := 0; i < count; i++ {
		c := n.NamedChild(i)
		if c.Type() == "decorator" {
			out = append(out, c)
		}
	}
	return out
}

// FunctionDef returns the function_definition inside a decorated_definition,
// or n itself if it's already a function_definition.
func FunctionDef(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Type() == "function_definition" {
		return n
	}
	if n.Type() == "decorated_definition" {
		count := int(n.NamedChildCount())
		for i := 0; i < count; i++ {
			c := n.NamedChild(i)
			if c.Type() == "function_definition" {
				return c
			}
		}
	}
	return nil
}

// FunctionName returns the identifier text for a function_definition node.
func FunctionName(fn *sitter.Node, src []byte) string {
	if fn == nil || fn.Type() != "function_definition" {
		return ""
	}
	name := fn.ChildByFieldName("name")
	return NodeText(name, src)
}

// FunctionDocstring returns the docstring CONTENT (between the quotes) of the
// leading string literal in a function body, if any. Returns "" if the body
// doesn't start with a string expression.
//
// The smacker tree-sitter-python binding does not surface `string_content`
// as a child node — the string's raw text is the only available source —
// so we strip the surrounding quotes and any Python string prefix
// (r/R/b/B/u/U/f/F and 2-char combinations like rb/br/fR/...) by hand.
func FunctionDocstring(fn *sitter.Node, src []byte) string {
	if fn == nil {
		return ""
	}
	body := fn.ChildByFieldName("body")
	if body == nil || int(body.NamedChildCount()) == 0 {
		return ""
	}
	first := body.NamedChild(0)
	if first.Type() != "expression_statement" || int(first.NamedChildCount()) == 0 {
		return ""
	}
	str := first.NamedChild(0)
	if str.Type() != "string" {
		return ""
	}
	return stripPythonStringLiteral(NodeText(str, src))
}

// stripPythonStringLiteral removes the prefix and quote markers from a Python
// string literal, returning the content (trimmed). Handles single, double,
// triple-single, triple-double quotes and the standard prefixes.
func stripPythonStringLiteral(s string) string {
	s = strings.TrimSpace(s)
	// Strip up to two prefix letters (the legal Python combinations are at
	// most 2 chars: rb, br, fr, rf, plus the singles r/b/u/f). Matching by
	// character set keeps casing simple.
	const prefixChars = "rRbBuUfF"
	for i := 0; i < 2 && len(s) > 0 && strings.ContainsRune(prefixChars, rune(s[0])); i++ {
		s = s[1:]
	}
	for _, q := range []string{`"""`, `'''`, `"`, `'`} {
		if strings.HasPrefix(s, q) && strings.HasSuffix(s, q) && len(s) >= 2*len(q) {
			return strings.TrimSpace(s[len(q) : len(s)-len(q)])
		}
	}
	return s
}

// FunctionParams returns the parameter names declared on a function_definition.
func FunctionParams(fn *sitter.Node, src []byte) []string {
	if fn == nil {
		return nil
	}
	params := fn.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	var out []string
	count := int(params.NamedChildCount())
	for i := 0; i < count; i++ {
		p := params.NamedChild(i)
		switch p.Type() {
		case "identifier":
			out = append(out, NodeText(p, src))
		case "typed_parameter", "default_parameter", "typed_default_parameter":
			// First named child is the identifier in all three.
			if int(p.NamedChildCount()) > 0 {
				name := p.NamedChild(0)
				if name.Type() == "identifier" {
					out = append(out, NodeText(name, src))
				}
			}
		}
	}
	return out
}

// KwargValue returns the value-node text of the named keyword argument in a
// call's argument list, and whether the kwarg is present at all.
//
//	present=false              -> kwarg absent
//	present=true, value="None" -> kwarg present with literal None
//	present=true, value="10"   -> kwarg present with value 10
func KwargValue(call *sitter.Node, src []byte, name string) (value string, present bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	Walk(args, func(n *sitter.Node) bool {
		if present {
			return false
		}
		if n.Type() != "keyword_argument" {
			return true
		}
		k := n.ChildByFieldName("name")
		if k != nil && NodeText(k, src) == name {
			present = true
			if v := n.ChildByFieldName("value"); v != nil {
				value = NodeText(v, src)
			}
			return false
		}
		return true
	})
	return value, present
}

// FunctionHasTypedParams reports whether the function declares at least one
// type-annotated parameter (excluding self/cls).
func FunctionHasTypedParams(fn *sitter.Node) bool {
	if fn == nil {
		return false
	}
	params := fn.ChildByFieldName("parameters")
	if params == nil {
		return false
	}
	count := int(params.NamedChildCount())
	for i := 0; i < count; i++ {
		p := params.NamedChild(i)
		if p.Type() == "typed_parameter" || p.Type() == "typed_default_parameter" {
			return true
		}
	}
	return false
}
