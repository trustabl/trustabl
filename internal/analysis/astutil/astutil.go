// Package astutil wraps tree-sitter with a tiny ergonomic surface so the
// detector code reads like AST queries, not raw C bindings.
//
// All offsets are tree-sitter's 0-indexed; conversion to 1-indexed line
// numbers happens at the boundary (NodeLine).
package astutil

import (
	"context"
	"strings"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

// NewPyParser returns a parser configured for the tree-sitter-python grammar.
// Parsers are reusable across files, and reuse avoids allocating a fresh C
// parser per file; they are not safe for concurrent use, so create one per
// goroutine if you parallelize.
func NewPyParser() *sitter.Parser {
	p := sitter.NewParser()
	p.SetLanguage(python.GetLanguage())
	return p
}

// Parse parses Python source into a tree. Returns the tree and the original
// source — detectors need both (the tree gives structure; the source gives
// the actual byte content of identifiers). Each call allocates a one-shot
// parser; hot per-file loops should reuse a NewPyParser instead.
func Parse(src []byte) (*sitter.Tree, error) {
	return ParseCtxTimeout(context.Background(), NewPyParser(), src)
}

// ParseTimeout bounds how long a single tree-sitter parse may run. A normal
// source file parses in milliseconds; a pathologically-nested file (thousands of
// nested brackets/parens) can make the C parser consume CPU/memory for far
// longer. Wrapping ParseCtx with this deadline arms tree-sitter's cancellation
// so such a file is abandoned instead of hanging the whole scan.
const ParseTimeout = 15 * time.Second

// ParseCtxTimeout parses src with parser, deriving a deadline from ctx so that
// BOTH an upstream cancellation (e.g. the CLI's Ctrl-C) and the ParseTimeout
// ceiling arm tree-sitter's C-level cancellation — which the binding only
// engages when ctx.Done() is non-nil (a bare context.Background() never
// interrupts the C parser). Pass context.Background() when there is no upstream
// context to honor; the timeout still applies.
func ParseCtxTimeout(ctx context.Context, parser *sitter.Parser, src []byte) (*sitter.Tree, error) {
	cctx, cancel := context.WithTimeout(ctx, ParseTimeout)
	defer cancel()
	return parser.ParseCtx(cctx, nil, src)
}

// NodeText returns the source bytes a node spans, as a string.
//
// NodeText is a shared primitive called on nodes parsed from src, but the
// signature cannot enforce that the node and the bytes actually came from the
// same parse. A node whose offsets exceed len(src) (mismatched or truncated
// source) would otherwise panic the entire scan on an out-of-range slice, so
// the offsets are clamped defensively: an inverted or wholly out-of-range span
// yields "", and an overlong end is clamped to len(src).
func NodeText(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	start, end := int(n.StartByte()), int(n.EndByte())
	if start < 0 || start > len(src) || end < start {
		return ""
	}
	if end > len(src) {
		end = len(src)
	}
	return string(src[start:end])
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

// maxWalkDepth bounds the recursion depth of a tree walk. Discovery parses
// untrusted source, and a pathologically nested input (thousands of nested
// brackets/parens) yields a correspondingly deep tree that would otherwise
// recurse until the goroutine stack is exhausted and the scanner crashes. A
// cap this high is never reached by real code but caps an adversarial file.
const maxWalkDepth = 4000

// Walk performs a pre-order traversal, calling fn on each named node. Return
// false from fn to stop descending into that subtree. Traversal stops descending
// past maxWalkDepth to bound stack growth on adversarially-nested input.
func Walk(root *sitter.Node, fn func(*sitter.Node) bool) {
	if root == nil {
		return
	}
	walk(root, fn, 0)
}

func walk(n *sitter.Node, fn func(*sitter.Node) bool, depth int) {
	if depth > maxWalkDepth {
		return
	}
	// NamedChild can return nil for a null C node (malformed/partial tree), so
	// the recursive descent below may hand us nil — guard before fn dereferences.
	if n == nil {
		return
	}
	if !fn(n) {
		return
	}
	count := int(n.NamedChildCount())
	for i := 0; i < count; i++ {
		walk(n.NamedChild(i), fn, depth+1)
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
		if c != nil && c.Type() == "decorator" {
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
			if c != nil && c.Type() == "function_definition" {
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
	if first == nil || first.Type() != "expression_statement" || int(first.NamedChildCount()) == 0 {
		return ""
	}
	str := first.NamedChild(0)
	if str == nil || str.Type() != "string" {
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
		if p == nil {
			continue
		}
		switch p.Type() {
		case "identifier":
			out = append(out, NodeText(p, src))
		case "typed_parameter", "default_parameter", "typed_default_parameter":
			// First named child is the identifier in all three.
			if int(p.NamedChildCount()) > 0 {
				name := p.NamedChild(0)
				if name != nil && name.Type() == "identifier" {
					out = append(out, NodeText(name, src))
				}
			}
		case "list_splat_pattern", "dictionary_splat_pattern":
			// *args / **kwargs. The bare name (without the * / ** prefix) is
			// the splat pattern's identifier child. Surfacing it lets rules
			// that key on a parameter named e.g. "kwargs" match a real
			// **kwargs signature, not only a plain param literally so named.
			if int(p.NamedChildCount()) > 0 {
				name := p.NamedChild(0)
				if name != nil && name.Type() == "identifier" {
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
	// Inspect only the call's DIRECT keyword arguments. A previous Walk descended
	// the whole argument subtree, so a kwarg nested inside an argument's own call
	// — e.g. requests.get(url, headers=build(timeout=5)) — was wrongly attributed
	// to the outer call, making "does this call set `timeout`?" answer true and
	// silently weakening timeout/retry findings. keyword_argument nodes are direct
	// children of the argument_list, so depth-1 is the correct scope.
	count := int(args.NamedChildCount())
	for i := 0; i < count; i++ {
		n := args.NamedChild(i)
		if n == nil || n.Type() != "keyword_argument" {
			continue
		}
		k := n.ChildByFieldName("name")
		if k != nil && NodeText(k, src) == name {
			if v := n.ChildByFieldName("value"); v != nil {
				value = NodeText(v, src)
			}
			return value, true
		}
	}
	return "", false
}

// FunctionHasTypedParams reports whether the function declares at least one
// type-annotated parameter (excluding self/cls).
func FunctionHasTypedParams(fn *sitter.Node, src []byte) bool {
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
		if p == nil {
			continue
		}
		if p.Type() == "typed_parameter" || p.Type() == "typed_default_parameter" {
			// First named child is the identifier in both types.
			if int(p.NamedChildCount()) > 0 {
				name := p.NamedChild(0)
				if name != nil && name.Type() == "identifier" {
					text := NodeText(name, src)
					if text == "self" || text == "cls" {
						continue
					}
				}
			}
			return true
		}
	}
	return false
}
