package astutil

import (
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
)

// TestWalk_DeeplyNestedDoesNotPanic feeds a pathologically nested expression
// (the adversarial-input case the depth cap exists for) and asserts the walk
// completes instead of exhausting the stack. Without maxWalkDepth a hostile
// repo could crash the scanner just by checking in a deeply nested file.
func TestWalk_DeeplyNestedDoesNotPanic(t *testing.T) {
	// 20k nested parens — far past maxWalkDepth, deep enough to blow an
	// unbounded recursive walk's stack.
	const n = 20000
	src := "x = " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n) + "\n"

	tree, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer tree.Close()

	// Must return rather than panic/overflow. We don't assert a node count —
	// only that the bounded traversal terminates safely.
	visited := 0
	Walk(tree.RootNode(), func(*sitter.Node) bool {
		visited++
		return true
	})
	if visited == 0 {
		t.Error("Walk visited no nodes")
	}
}
