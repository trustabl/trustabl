package astutil

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
)

// NewPHPParser returns a parser configured for the tree-sitter-php grammar (use
// for .php files). The grammar ships with the smacker/go-tree-sitter module
// Trustabl already depends on, so this adds no new dependency. Parsers are not
// safe for concurrent use; create one per goroutine if you parallelize.
func NewPHPParser() *sitter.Parser {
	p := sitter.NewParser()
	p.SetLanguage(php.GetLanguage())
	return p
}
