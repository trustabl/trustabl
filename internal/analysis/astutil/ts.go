package astutil

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/trustabl/trustabl/internal/models"
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

// TSImportAliasesAny returns the union of TSImportAliases across modules.
// Used when a single discovery pass needs to recognize imports from any of
// several related packages (e.g. @openai/agents + @openai/agents-core +
// @openai/agents-openai). On a same-local-name collision across modules,
// last-write wins — which never happens in practice because the meta package
// re-exports the others. An empty modules slice returns an empty map.
func TSImportAliasesAny(root *sitter.Node, src []byte, modules []string) map[string]string {
	out := make(map[string]string)
	for _, mod := range modules {
		for k, v := range TSImportAliases(root, src, mod) {
			out[k] = v
		}
	}
	return out
}

// TSObjectKwargs converts a tree-sitter "object" node (object literal) into
// a KwargTree. Each property becomes a child keyed by the property name.
// Leaf values are typed via classifyTSExpr (string/int/bool/null literals,
// list/array, call, identifier, or unknown). Spread properties and computed
// property names are skipped (the caller can detect this by an Opaque-style
// check — TSObjectKwargs does NOT itself flag opaqueness).
func TSObjectKwargs(obj *sitter.Node, src []byte) *models.KwargTree {
	out := &models.KwargTree{Children: map[string]*models.KwargTree{}}
	if obj == nil || obj.Type() != "object" {
		return out
	}
	for i := 0; i < int(obj.NamedChildCount()); i++ {
		prop := obj.NamedChild(i)
		if prop.Type() != "pair" {
			continue // skip spread_element and shorthand_property_identifier
		}
		key := prop.ChildByFieldName("key")
		val := prop.ChildByFieldName("value")
		if key == nil || val == nil {
			continue
		}
		// Only property_identifier and string keys are extractable; computed
		// keys (in [brackets]) are skipped — caller can detect via a separate
		// hasComputedKey() pass.
		var keyName string
		switch key.Type() {
		case "property_identifier":
			keyName = NodeText(key, src)
		case "string":
			raw := NodeText(key, src)
			if len(raw) >= 2 {
				keyName = raw[1 : len(raw)-1]
			}
		default:
			continue
		}
		child := &models.KwargTree{}
		if val.Type() == "object" {
			nested := TSObjectKwargs(val, src)
			child.Children = nested.Children
		} else {
			child.Value = classifyTSExpr(val, src)
		}
		out.Children[keyName] = child
	}
	return out
}

func classifyTSExpr(n *sitter.Node, src []byte) *models.Expr {
	if n == nil {
		return nil
	}
	text := NodeText(n, src)
	switch n.Type() {
	case "string":
		return &models.Expr{Kind: models.ExprLiteralString, Text: text}
	case "number":
		return &models.Expr{Kind: models.ExprLiteralInt, Text: text}
	case "true", "false":
		return &models.Expr{Kind: models.ExprLiteralBool, Text: text}
	case "null", "undefined":
		return &models.Expr{Kind: models.ExprLiteralNone, Text: text}
	case "identifier":
		return &models.Expr{Kind: models.ExprNameRef, Text: text}
	case "call_expression":
		return &models.Expr{Kind: models.ExprCall, Text: text}
	case "array":
		list := make([]models.Expr, 0, n.NamedChildCount())
		for i := 0; i < int(n.NamedChildCount()); i++ {
			if e := classifyTSExpr(n.NamedChild(i), src); e != nil {
				list = append(list, *e)
			}
		}
		return &models.Expr{Kind: models.ExprList, Text: text, List: list}
	}
	return &models.Expr{Kind: models.ExprUnknown, Text: text}
}

// TSCalleeText resolves a call_expression's callee against an alias map and
// returns the canonical export name (e.g. "tool", "query",
// "createSdkMcpServer") if the call targets a tracked export, or "" if it
// does not. The aliases map should come from TSImportAliases. Handles:
//   tool(...)            — direct call, aliases["tool"] = "tool"
//   t(...)               — renamed import, aliases["t"] = "tool"
//   sdk.tool(...)        — namespace import, aliases["sdk"] = "*"
//   defaultExport.tool() — default import treated as a namespace, aliases["defaultExport"] = "default"
func TSCalleeText(call *sitter.Node, src []byte, aliases map[string]string) string {
	if call == nil || aliases == nil {
		return ""
	}
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		name := NodeText(fn, src)
		if canon, ok := aliases[name]; ok && canon != "*" && canon != "default" {
			return canon
		}
	case "member_expression":
		obj := fn.ChildByFieldName("object")
		prop := fn.ChildByFieldName("property")
		if obj == nil || prop == nil || obj.Type() != "identifier" {
			return ""
		}
		objName := NodeText(obj, src)
		if canon, ok := aliases[objName]; ok && (canon == "*" || canon == "default") {
			return NodeText(prop, src)
		}
	}
	return ""
}
