package astutil

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// NewGoParser returns a parser configured for the tree-sitter-go grammar (use
// for .go files). The grammar ships with the smacker/go-tree-sitter module
// Trustabl already depends on, so this adds no new dependency. Parsers are not
// safe for concurrent use; create one per goroutine if you parallelize.
func NewGoParser() *sitter.Parser {
	p := sitter.NewParser()
	p.SetLanguage(golang.GetLanguage())
	return p
}
