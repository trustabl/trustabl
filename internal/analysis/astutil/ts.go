package astutil

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// NewTSParser returns a parser configured for the tree-sitter-typescript
// grammar (use for .ts / .mts / .cts). Parsers are not safe for concurrent
// use; create one per goroutine if you parallelize.
func NewTSParser() *sitter.Parser {
	p := sitter.NewParser()
	p.SetLanguage(typescript.GetLanguage())
	return p
}

// NewTSXParser returns a parser configured for the tree-sitter-tsx grammar
// (use for .tsx). Separate grammar from typescript — accepts JSX productions.
func NewTSXParser() *sitter.Parser {
	p := sitter.NewParser()
	p.SetLanguage(tsx.GetLanguage())
	return p
}

// ParserKindForExtension returns "typescript" for .ts/.mts/.cts, "tsx" for
// .tsx, "" otherwise. Callers dispatch on the result to pick a parser.
func ParserKindForExtension(path string) string {
	switch {
	case strings.HasSuffix(path, ".tsx"):
		return "tsx"
	case strings.HasSuffix(path, ".ts"),
		strings.HasSuffix(path, ".mts"),
		strings.HasSuffix(path, ".cts"):
		return "typescript"
	}
	return ""
}
