package astutil

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/csharp"
)

// NewCSharpParser returns a parser configured for the tree-sitter-c-sharp
// grammar (use for .cs files). The grammar ships with the smacker/go-tree-sitter
// module Trustabl already depends on, so this adds no new dependency. Parsers
// are not safe for concurrent use; create one per goroutine if you parallelize.
func NewCSharpParser() *sitter.Parser {
	p := sitter.NewParser()
	p.SetLanguage(csharp.GetLanguage())
	return p
}
