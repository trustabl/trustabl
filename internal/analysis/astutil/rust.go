package astutil

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/rust"
)

// NewRustParser returns a parser configured for the tree-sitter-rust grammar
// (use for .rs files). The grammar ships with the smacker/go-tree-sitter module
// Trustabl already depends on, so this adds no new dependency. Parsers are not
// safe for concurrent use; create one per goroutine if you parallelize.
func NewRustParser() *sitter.Parser {
	p := sitter.NewParser()
	p.SetLanguage(rust.GetLanguage())
	return p
}
