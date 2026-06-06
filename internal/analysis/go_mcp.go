package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// goMCPModulePrefixes gates Go MCP discovery: a file is processed only if it
// imports one of these module paths. mark3labs/mcp-go is the most-adopted
// community SDK; modelcontextprotocol/go-sdk is the official one. (metoro-io/
// mcp-golang's reflection-based RegisterTool is a documented v1 gap.)
var goMCPModulePrefixes = []string{
	"github.com/mark3labs/mcp-go",
	"github.com/modelcontextprotocol/go-sdk",
}

// DiscoverGoMCPTools walks each parsed Go file and emits a ToolDef per MCP tool
// definition. Import-gated to the mcp-go SDKs. Two shapes are recognized, both
// keyed on a call into the package named "mcp" (the import gate resolves its
// local alias):
//
//	mark3labs: mcp.NewTool("name", mcp.WithDescription("d"), mcp.WithString("p", ...))
//	official:  mcp.AddTool(server, &mcp.Tool{Name: "name", Description: "d"}, handler)
//
// Tools carry Kind=mcp_tool, Language=go, so deriveSDKsDetected stamps SDKMCP
// and the shared mcp/ rule pack's language:go rules audit them.
func DiscoverGoMCPTools(files []ParsedFile, onFile func(string)) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverGoMCPToolsInFile(pf)...)
	}
	return out
}

func discoverGoMCPToolsInFile(pf ParsedFile) []models.ToolDef {
	if pf.Tree == nil {
		return nil
	}
	root := pf.Tree.RootNode()
	mcpAlias, gated := goMCPPackageAlias(goImports(root, pf.Source))
	if !gated {
		return nil
	}
	var out []models.ToolDef
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "call_expression" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || fn.Type() != "selector_expression" {
			return true
		}
		operand := fn.ChildByFieldName("operand")
		field := fn.ChildByFieldName("field")
		if operand == nil || field == nil || operand.Type() != "identifier" {
			return true
		}
		if astutil.NodeText(operand, pf.Source) != mcpAlias {
			return true
		}
		switch astutil.NodeText(field, pf.Source) {
		case "NewTool":
			if td, ok := extractGoMark3labsTool(n, pf); ok {
				out = append(out, td)
			}
		case "AddTool":
			if td, ok := extractGoOfficialTool(n, pf); ok {
				out = append(out, td)
			}
		}
		return true
	})
	return out
}

// extractGoMark3labsTool reads mcp.NewTool("name", mcp.WithDescription(...),
// mcp.WithString("p", ...), ...). Name is arg 0; WithDescription supplies the
// description; the typed param builders (WithString/WithNumber/WithBoolean/
// WithObject/WithArray) each contribute a named, typed parameter.
func extractGoMark3labsTool(call *sitter.Node, pf ParsedFile) (models.ToolDef, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() == 0 {
		return models.ToolDef{}, false
	}
	name, ok := goStringArg(args.NamedChild(0), pf.Source)
	if !ok {
		return models.ToolDef{}, false
	}
	td := models.ToolDef{
		Name:     name,
		Kind:     models.KindMCPTool,
		Language: models.LanguageGo,
		Location: goNodeLocation(call, pf),
	}
	for i := 1; i < int(args.NamedChildCount()); i++ {
		opt := args.NamedChild(i)
		if opt.Type() != "call_expression" {
			continue
		}
		ofn := opt.ChildByFieldName("function")
		if ofn == nil || ofn.Type() != "selector_expression" {
			continue
		}
		field := ofn.ChildByFieldName("field")
		if field == nil {
			continue
		}
		switch astutil.NodeText(field, pf.Source) {
		case "WithDescription":
			if s, ok := goFirstStringArg(opt, pf.Source); ok {
				td.Description = s
			}
		case "WithString", "WithNumber", "WithBoolean", "WithObject", "WithArray":
			if p, ok := goFirstStringArg(opt, pf.Source); ok {
				td.ParamNames = append(td.ParamNames, p)
				td.HasTypedParams = true
			}
		}
	}
	return td, true
}

// extractGoOfficialTool reads mcp.AddTool(server, &mcp.Tool{Name: "n",
// Description: "d"}, handler) — the tool definition is the &mcp.Tool{...}
// composite literal among the arguments. Parameter schema comes from the
// handler's input struct (a second hop) and is a documented v1 gap, so only
// Name and Description are extracted here.
func extractGoOfficialTool(call *sitter.Node, pf ParsedFile) (models.ToolDef, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return models.ToolDef{}, false
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		lit := goCompositeLiteral(args.NamedChild(i))
		if lit == nil {
			continue
		}
		name, desc, ok := goToolStructFields(lit, pf.Source)
		if !ok {
			continue
		}
		return models.ToolDef{
			Name:        name,
			Description: desc,
			Kind:        models.KindMCPTool,
			Language:    models.LanguageGo,
			Location:    goNodeLocation(call, pf),
		}, true
	}
	return models.ToolDef{}, false
}

// goToolStructFields extracts the Name and Description string fields from an
// mcp.Tool composite literal. Returns ok only when the literal's type name ends
// in "Tool" (so an unrelated struct argument is not mistaken for a tool).
func goToolStructFields(lit *sitter.Node, src []byte) (name, desc string, ok bool) {
	typeNode := lit.ChildByFieldName("type")
	if typeNode == nil {
		return "", "", false
	}
	typeText := astutil.NodeText(typeNode, src)
	if !strings.HasSuffix(typeText, "Tool") {
		return "", "", false
	}
	body := lit.ChildByFieldName("body")
	if body == nil {
		return "", "", false
	}
	for i := 0; i < int(body.NamedChildCount()); i++ {
		el := body.NamedChild(i)
		if el.Type() != "keyed_element" {
			continue
		}
		if el.NamedChildCount() < 2 {
			continue
		}
		key := goUnwrapLiteralElement(el.NamedChild(0))
		val := goUnwrapLiteralElement(el.NamedChild(1))
		if key == nil || val == nil {
			continue
		}
		switch astutil.NodeText(key, src) {
		case "Name":
			if s, ok := goStringNode(val, src); ok {
				name = s
			}
		case "Description":
			if s, ok := goStringNode(val, src); ok {
				desc = s
			}
		}
	}
	return name, desc, name != "" || desc != ""
}

// goUnwrapLiteralElement returns the inner expression of a literal_element
// wrapper (tree-sitter-go wraps composite-literal keys/values in
// literal_element), or n unchanged when it is not such a wrapper.
func goUnwrapLiteralElement(n *sitter.Node) *sitter.Node {
	if n != nil && n.Type() == "literal_element" && n.NamedChildCount() == 1 {
		return n.NamedChild(0)
	}
	return n
}

// goCompositeLiteral unwraps `&T{...}` (a unary_expression over a
// composite_literal) and bare `T{...}` to the composite_literal, or nil.
func goCompositeLiteral(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	switch n.Type() {
	case "composite_literal":
		return n
	case "unary_expression":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			if c := n.NamedChild(i); c.Type() == "composite_literal" {
				return c
			}
		}
	}
	return nil
}

// goImports returns every (alias, path) import in a parsed Go file. The alias is
// the explicit rename when present, else the import path's last segment.
type goImport struct {
	Alias string
	Path  string
}

func goImports(root *sitter.Node, src []byte) []goImport {
	var out []goImport
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "import_spec" {
			return true
		}
		pathNode := n.ChildByFieldName("path")
		if pathNode == nil {
			return true
		}
		path := goUnquote(astutil.NodeText(pathNode, src))
		alias := path
		if i := strings.LastIndex(alias, "/"); i >= 0 {
			alias = alias[i+1:]
		}
		if nameNode := n.ChildByFieldName("name"); nameNode != nil {
			alias = astutil.NodeText(nameNode, src)
		}
		out = append(out, goImport{Alias: alias, Path: path})
		return true
	})
	return out
}

// goMCPPackageAlias gates the file (true only when an mcp-go module is imported)
// and resolves the local alias of its "mcp" package — the receiver of both
// mcp.NewTool (mark3labs) and mcp.AddTool (official), since both SDKs name that
// package "mcp".
func goMCPPackageAlias(imports []goImport) (string, bool) {
	for _, im := range imports {
		if !goIsMCPModule(im.Path) {
			continue
		}
		seg := im.Path
		if i := strings.LastIndex(seg, "/"); i >= 0 {
			seg = seg[i+1:]
		}
		if seg == "mcp" {
			return im.Alias, true
		}
	}
	return "", false
}

func goIsMCPModule(path string) bool {
	for _, p := range goMCPModulePrefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// goFirstStringArg returns the first string-literal argument of a call.
func goFirstStringArg(call *sitter.Node, src []byte) (string, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() == 0 {
		return "", false
	}
	return goStringArg(args.NamedChild(0), src)
}

// goStringArg returns the unquoted value when n is a Go string literal.
func goStringArg(n *sitter.Node, src []byte) (string, bool) {
	return goStringNode(n, src)
}

func goStringNode(n *sitter.Node, src []byte) (string, bool) {
	if n == nil {
		return "", false
	}
	switch n.Type() {
	case "interpreted_string_literal", "raw_string_literal":
		return goUnquote(astutil.NodeText(n, src)), true
	}
	return "", false
}

// goUnquote strips one pair of surrounding `"` or backtick quotes.
func goUnquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '`' && s[len(s)-1] == '`') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func goNodeLocation(n *sitter.Node, pf ParsedFile) models.Location {
	return models.Location{
		FilePath: pf.RelPath,
		Line:     astutil.NodeLine(n),
		EndLine:  astutil.NodeEndLine(n),
	}
}
