package analysis

import (
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// tree-sitter-rust models PHP-8-style attributes properly: `#[tool(...)]` is an
// (attribute_item) that is a *preceding sibling* of the (function_item) it
// decorates — not a child — and its arguments are a flat (token_tree) of tokens
// (`description = "..."` → identifier, string_literal). So discovery walks
// function_item nodes and looks back over preceding siblings for the #[tool]
// attribute. The official rmcp SDK derives a tool's description from EITHER the
// `description = "..."` attribute argument OR the `///` doc comment on the method,
// so a faithful "no description" check must consider both.
var (
	rustAttrNameRe = regexp.MustCompile(`\bname\s*=\s*"([^"]*)"`)
	rustAttrDescRe = regexp.MustCompile(`\bdescription\s*=\s*"([^"]*)"`)
)

// DiscoverRustMCPTools walks each parsed Rust file and emits a ToolDef per
// #[tool]-attributed method — the official rmcp SDK shape
// (modelcontextprotocol/rust-sdk):
//
//	use rmcp::{tool, tool_router};
//	#[tool_router]
//	impl Calculator {
//	    /// Add two numbers.
//	    #[tool(description = "Add two numbers")]
//	    fn add(&self, Parameters(p): Parameters<AddParams>) -> String { ... }
//	}
//
// Import-gated to files that `use` the rmcp crate. Tools carry Kind=mcp_tool,
// Language=rust, so deriveSDKsDetected stamps SDKMCP and the shared mcp/ pack's
// language:rust rules audit them.
func DiscoverRustMCPTools(files []ParsedFile, onFile func(string)) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverRustMCPToolsInFile(pf)...)
	}
	return out
}

func discoverRustMCPToolsInFile(pf ParsedFile) []models.ToolDef {
	if pf.Tree == nil {
		return nil
	}
	root := pf.Tree.RootNode()
	if !rustUsesMCP(root, pf.Source) {
		return nil
	}
	var out []models.ToolDef
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "function_item" {
			return true
		}
		attr := rustToolAttr(n, pf.Source)
		if attr == nil {
			return true
		}
		td := models.ToolDef{
			Kind:     models.KindMCPTool,
			Language: models.LanguageRust,
			Location: rustNodeLocation(n, pf),
		}
		attrText := astutil.NodeText(attr, pf.Source)
		// Name from the #[tool(name = "...")] arg; falls back to the method name.
		if m := rustAttrNameRe.FindStringSubmatch(attrText); m != nil {
			td.Name = m[1]
		} else if nameNode := n.ChildByFieldName("name"); nameNode != nil {
			td.Name = astutil.NodeText(nameNode, pf.Source)
		}
		// Description from the #[tool(description = "...")] arg, else the /// doc
		// comment on the method (rmcp accepts either). An empty arg falls through
		// to the doc comment.
		if m := rustAttrDescRe.FindStringSubmatch(attrText); m != nil && m[1] != "" {
			td.Description = m[1]
		} else if doc := rustDocComment(n, pf.Source); doc != "" {
			td.Description = doc
		}
		// Params from the signature, skipping the &self receiver. Rust is
		// statically typed, so any declared parameter is typed.
		if params := n.ChildByFieldName("parameters"); params != nil {
			for i := 0; i < int(params.NamedChildCount()); i++ {
				if params.NamedChild(i).Type() == "parameter" {
					td.HasTypedParams = true
				}
			}
		}
		out = append(out, td)
		return true
	})
	return out
}

// rustToolAttr returns the #[tool] attribute_item decorating fn, or nil. It walks
// fn's preceding siblings across intervening attributes/comments (e.g. a #[derive]
// or a doc comment between #[tool] and the fn) and stops at the previous real item.
func rustToolAttr(fn *sitter.Node, src []byte) *sitter.Node {
	for cur := fn.PrevNamedSibling(); cur != nil; cur = cur.PrevNamedSibling() {
		switch cur.Type() {
		case "attribute_item":
			if rustAttrMacroName(cur, src) == "tool" {
				return cur
			}
		case "line_comment", "block_comment":
			// doc/regular comment between the attribute and the fn — keep scanning.
		default:
			return nil
		}
	}
	return nil
}

// rustDocComment returns the trimmed text of the /// (or /** */) doc comment
// immediately preceding fn (skipping any attributes between), or "" if the
// nearest preceding comment is a plain // comment or there is none.
func rustDocComment(fn *sitter.Node, src []byte) string {
	for cur := fn.PrevNamedSibling(); cur != nil; cur = cur.PrevNamedSibling() {
		switch cur.Type() {
		case "attribute_item":
			continue
		case "line_comment":
			text := strings.TrimSpace(astutil.NodeText(cur, src))
			if strings.HasPrefix(text, "///") {
				return strings.TrimSpace(strings.TrimPrefix(text, "///"))
			}
			return ""
		case "block_comment":
			text := strings.TrimSpace(astutil.NodeText(cur, src))
			if strings.HasPrefix(text, "/**") {
				text = strings.TrimSuffix(strings.TrimPrefix(text, "/**"), "*/")
				return strings.TrimSpace(strings.Trim(text, "*"))
			}
			return ""
		default:
			return ""
		}
	}
	return ""
}

// rustAttrMacroName returns the macro identifier of an attribute_item — "tool"
// for #[tool(...)] and "rmcp::tool" is normalized to "tool" — so #[tool_router]
// (on the impl) and #[derive(...)] are not mistaken for the per-method #[tool].
func rustAttrMacroName(attrItem *sitter.Node, src []byte) string {
	attr := attrItem.NamedChild(0)
	if attr == nil {
		return ""
	}
	nameNode := attr.NamedChild(0)
	if nameNode == nil {
		return ""
	}
	name := astutil.NodeText(nameNode, src)
	if i := strings.LastIndex(name, "::"); i >= 0 {
		name = name[i+2:]
	}
	return name
}

// rustUsesMCP gates a file: true when a `use` statement references the rmcp crate.
func rustUsesMCP(root *sitter.Node, src []byte) bool {
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() == "use_declaration" && strings.Contains(astutil.NodeText(n, src), "rmcp::") {
			found = true
			return false
		}
		return true
	})
	return found
}

func rustNodeLocation(n *sitter.Node, pf ParsedFile) models.Location {
	return models.Location{
		FilePath: pf.RelPath,
		Line:     astutil.NodeLine(n),
		EndLine:  astutil.NodeEndLine(n),
	}
}
