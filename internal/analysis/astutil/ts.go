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

// TSImportAliases walks a parsed TS file's top-level import_statement nodes
// and returns a map: local-binding-name -> canonical-export-name, for every
// import that targets `module`. Sentinel canonical values:
//   "*"        — namespace import: `import * as ns from "module"`
//   "default"  — default import:   `import x from "module"`
//
// Named imports map their local name to the original export name. A renamed
// import (`import { tool as t }`) maps "t" -> "tool"; a plain named import
// (`import { tool }`) maps "tool" -> "tool". Imports from any module other
// than `module` are ignored.
func TSImportAliases(root *sitter.Node, src []byte, module string) map[string]string {
	out := make(map[string]string)
	if root == nil {
		return out
	}
	Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "import_statement" {
			return true
		}
		// source field is a string literal node like "@anthropic-ai/...".
		source := n.ChildByFieldName("source")
		if source == nil {
			return true
		}
		raw := NodeText(source, src)
		if len(raw) < 2 {
			return true
		}
		// Strip surrounding quotes.
		mod := raw[1 : len(raw)-1]
		if mod != module {
			return true
		}
		// Walk children for import_clause(s): named_imports / namespace_import / identifier
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			collectImportSpec(c, src, out)
		}
		return true
	})
	return out
}

func collectImportSpec(n *sitter.Node, src []byte, out map[string]string) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "import_clause":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			collectImportSpec(n.NamedChild(i), src, out)
		}
	case "identifier":
		// Default import: `import defaultExport from "..."`.
		out[NodeText(n, src)] = "default"
	case "namespace_import":
		// `import * as ns from "..."` — find the trailing identifier.
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c.Type() == "identifier" {
				out[NodeText(c, src)] = "*"
			}
		}
	case "named_imports":
		// Iterate `import_specifier` children.
		for i := 0; i < int(n.NamedChildCount()); i++ {
			spec := n.NamedChild(i)
			if spec.Type() != "import_specifier" {
				continue
			}
			nameNode := spec.ChildByFieldName("name")
			aliasNode := spec.ChildByFieldName("alias")
			if nameNode == nil {
				continue
			}
			origName := NodeText(nameNode, src)
			localName := origName
			if aliasNode != nil {
				localName = NodeText(aliasNode, src)
			}
			out[localName] = origName
		}
	}
}
