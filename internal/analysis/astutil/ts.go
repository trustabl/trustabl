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

// ParserKindForExtension returns "typescript" for .ts/.mts/.cts, "tsx" for .tsx
// and for every JavaScript extension (.js/.jsx/.mjs/.cjs), "" otherwise. Callers
// dispatch on the result to pick a parser. JavaScript routes to the tsx grammar
// deliberately: tsx is a superset that parses plain JS and tolerates JSX inside
// a .js file, so one parser covers the whole JS family. Discovery stamps these
// defs LanguageTypeScript; the scanner re-tags JS-sourced defs to
// LanguageJavaScript after edge resolution (see scanner.retagJavaScriptDefs).
func ParserKindForExtension(path string) string {
	switch {
	case strings.HasSuffix(path, ".tsx"),
		strings.HasSuffix(path, ".jsx"),
		strings.HasSuffix(path, ".js"),
		strings.HasSuffix(path, ".mjs"),
		strings.HasSuffix(path, ".cjs"):
		return "tsx"
	case strings.HasSuffix(path, ".ts"),
		strings.HasSuffix(path, ".mts"),
		strings.HasSuffix(path, ".cts"):
		return "typescript"
	}
	return ""
}

// IsJavaScriptExtension reports whether path names a JavaScript source file
// (.js/.jsx/.mjs/.cjs): the set recon classifies into ScanManifest.JavaScriptFiles
// and the set ParserKindForExtension routes to the tsx grammar. The scanner uses
// it to re-tag JS-sourced defs (discovery stamps the TS-family LanguageTypeScript)
// to LanguageJavaScript after edge resolution. Case-sensitive, mirroring
// ParserKindForExtension.
func IsJavaScriptExtension(path string) bool {
	return strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".jsx") ||
		strings.HasSuffix(path, ".mjs") || strings.HasSuffix(path, ".cjs")
}

// TSImportAliases walks a parsed TS/JS file's ES import_statement nodes AND its
// CommonJS require() variable bindings, returning a map: local-binding-name ->
// canonical-export-name, for every import/require that targets `module`.
// Sentinel canonical values:
//
//	"*"        — namespace:      `import * as ns from "module"` or `const ns = require("module")`
//	"default"  — default import: `import x from "module"`
//
// Named bindings map their local name to the original export name, for both ES
// imports and CommonJS require destructuring:
//
//	import { tool as t } from "module"      "t" -> "tool"
//	import { tool } from "module"           "tool" -> "tool"
//	const { tool: t } = require("module")   "t" -> "tool"
//	const { tool } = require("module")      "tool" -> "tool"
//	const x = require("module").tool        "x" -> "tool"
//
// Imports from any module other than `module` are ignored.
func TSImportAliases(root *sitter.Node, src []byte, module string) map[string]string {
	return TSImportAliasesMatch(root, src, func(mod string) bool { return mod == module })
}

// TSImportAliasesMatch generalizes TSImportAliases to an arbitrary module
// matcher. It exists for ecosystems whose imports use many subpaths — e.g.
// LangChain's "@langchain/core/tools", "@langchain/langgraph/prebuilt",
// "@langchain/community/tools/shell" — where an exact module list would be
// brittle. The matcher receives the unquoted module specifier.
func TSImportAliasesMatch(root *sitter.Node, src []byte, match func(mod string) bool) map[string]string {
	out := make(map[string]string)
	if root == nil {
		return out
	}
	Walk(root, func(n *sitter.Node) bool {
		switch n.Type() {
		case "import_statement":
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
			if !match(mod) {
				return true
			}
			// Walk children for import_clause(s): named_imports / namespace_import / identifier
			for i := 0; i < int(n.NamedChildCount()); i++ {
				c := n.NamedChild(i)
				collectImportSpec(c, src, out)
			}
		case "variable_declarator":
			// CommonJS require() bindings (const { tool } = require("mod"),
			// const ns = require("mod"), const x = require("mod").tool) so
			// JavaScript apps written against CommonJS instead of ES modules are
			// gated in and have their SDK symbols resolved the same way.
			collectRequireSpec(n, src, match, out)
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
			if c != nil && c.Type() == "identifier" {
				out[NodeText(c, src)] = "*"
			}
		}
	case "named_imports":
		// Iterate `import_specifier` children.
		for i := 0; i < int(n.NamedChildCount()); i++ {
			spec := n.NamedChild(i)
			if spec == nil || spec.Type() != "import_specifier" {
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

// collectRequireSpec recognizes CommonJS require() bindings on a
// variable_declarator and merges them into out using the SAME canonical values
// as ES imports, so downstream resolution (TSCalleeText) is identical:
//
//	const ns = require("mod")            ns   -> "*"      (namespace)
//	const x  = require("mod").tool       x    -> "tool"   (member)
//	const { tool } = require("mod")      tool -> "tool"   (named)
//	const { tool: t } = require("mod")   t    -> "tool"   (renamed)
//
// let/var declarators reach here too (they share the variable_declarator node).
// Only require() calls whose module specifier passes match are kept.
func collectRequireSpec(decl *sitter.Node, src []byte, match func(mod string) bool, out map[string]string) {
	value := decl.ChildByFieldName("value")
	if value == nil {
		return
	}
	// Resolve the require("mod") call, optionally behind a single .member access.
	var call *sitter.Node
	var member string
	switch value.Type() {
	case "call_expression":
		call = value
	case "member_expression":
		if obj := value.ChildByFieldName("object"); obj != nil && obj.Type() == "call_expression" {
			call = obj
			if prop := value.ChildByFieldName("property"); prop != nil {
				member = NodeText(prop, src)
			}
		}
	}
	if call == nil {
		return
	}
	mod, ok := requireModule(call, src)
	if !ok || !match(mod) {
		return
	}
	name := decl.ChildByFieldName("name")
	if name == nil {
		return
	}
	switch name.Type() {
	case "identifier":
		local := NodeText(name, src)
		if member != "" {
			out[local] = member // const x = require("mod").tool
		} else {
			out[local] = "*" // const ns = require("mod")
		}
	case "object_pattern":
		// Destructuring binds the module's named exports; a .member before the
		// destructure is not a meaningful shape, so only the plain-call form is
		// handled.
		if member != "" {
			return
		}
		for i := 0; i < int(name.NamedChildCount()); i++ {
			c := name.NamedChild(i)
			switch c.Type() {
			case "shorthand_property_identifier_pattern":
				// const { tool } = require(...)
				k := NodeText(c, src)
				out[k] = k
			case "pair_pattern":
				// const { tool: t } = require(...)
				keyNode := c.ChildByFieldName("key")
				valNode := c.ChildByFieldName("value")
				if keyNode == nil || valNode == nil || valNode.Type() != "identifier" {
					continue
				}
				out[NodeText(valNode, src)] = NodeText(keyNode, src)
			}
		}
	}
}

// requireModule returns the unquoted module specifier of a require("mod") call
// expression, or ("", false) when call is not a require() of a string literal.
func requireModule(call *sitter.Node, src []byte) (string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "identifier" || NodeText(fn, src) != "require" {
		return "", false
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		if a := args.NamedChild(i); a.Type() == "string" {
			raw := NodeText(a, src)
			if len(raw) < 2 {
				return "", false
			}
			return raw[1 : len(raw)-1], true
		}
	}
	return "", false
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
		if prop == nil || prop.Type() != "pair" {
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
		// tree-sitter lexes ints and floats as one "number" node, so the only
		// reliable non-integer marker is a decimal point. Hex (0xff), binary,
		// octal, and exponent-without-point (1e3) literals are integer-valued
		// and stay ExprLiteralInt; 1.5 / 1.5e3 are ExprLiteralFloat. Without
		// this, a rule branching on ExprLiteralInt to mean "an integer" would
		// mis-handle every float.
		kind := models.ExprLiteralInt
		if strings.ContainsRune(text, '.') {
			kind = models.ExprLiteralFloat
		}
		return &models.Expr{Kind: kind, Text: text}
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
//
//	tool(...)            — direct call, aliases["tool"] = "tool"
//	t(...)               — renamed import, aliases["t"] = "tool"
//	sdk.tool(...)        — namespace import, aliases["sdk"] = "*"
//	defaultExport.tool() — default import treated as a namespace, aliases["defaultExport"] = "default"
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
