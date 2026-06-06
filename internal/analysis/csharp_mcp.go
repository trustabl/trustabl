package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// DiscoverCSharpMCPTools walks each parsed C# file and emits a ToolDef per
// [McpServerTool]-attributed method — the official ModelContextProtocol C# SDK
// shape:
//
//	[McpServerToolType]
//	public class WeatherTools {
//	    [McpServerTool, Description("Gets the weather for a city.")]
//	    public string GetWeather([Description("City name")] string city) { ... }
//	}
//
// Import-gated to files that `using` a ModelContextProtocol namespace. Tools
// carry Kind=mcp_tool, Language=csharp, so deriveSDKsDetected stamps SDKMCP and
// the shared mcp/ pack's language:csharp rules audit them.
func DiscoverCSharpMCPTools(files []ParsedFile, onFile func(string)) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverCSharpMCPToolsInFile(pf)...)
	}
	return out
}

func discoverCSharpMCPToolsInFile(pf ParsedFile) []models.ToolDef {
	if pf.Tree == nil {
		return nil
	}
	root := pf.Tree.RootNode()
	if !csharpUsesMCP(root, pf.Source) {
		return nil
	}
	var out []models.ToolDef
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "method_declaration" {
			return true
		}
		if csharpFindAttribute(n, pf.Source, "McpServerTool") == nil {
			return true
		}
		td := models.ToolDef{
			Kind:     models.KindMCPTool,
			Language: models.LanguageCSharp,
			Location: csharpNodeLocation(n, pf),
		}
		// Name: the method name. (The [McpServerTool(Name = "...")] override is a
		// documented v1 gap — the method name is the SDK default and common case.)
		if nameNode := n.ChildByFieldName("name"); nameNode != nil {
			td.Name = astutil.NodeText(nameNode, pf.Source)
		}
		// Description: the first string literal inside the co-located
		// [Description("...")] attribute (System.ComponentModel).
		if descAttr := csharpFindAttribute(n, pf.Source, "Description"); descAttr != nil {
			if d, ok := csharpFirstStringLiteral(descAttr, pf.Source); ok {
				td.Description = d
			}
		}
		// Params: method parameters. C# is statically typed, so any parameter is
		// a typed parameter.
		if params := n.ChildByFieldName("parameters"); params != nil {
			for i := 0; i < int(params.NamedChildCount()); i++ {
				p := params.NamedChild(i)
				if p.Type() != "parameter" {
					continue
				}
				if pn := p.ChildByFieldName("name"); pn != nil {
					td.ParamNames = append(td.ParamNames, astutil.NodeText(pn, pf.Source))
					td.HasTypedParams = true
				}
			}
		}
		out = append(out, td)
		return true
	})
	return out
}

// csharpUsesMCP gates a file: true when it has a `using` directive referencing a
// ModelContextProtocol namespace (the SDK's server attributes live under it).
func csharpUsesMCP(root *sitter.Node, src []byte) bool {
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() == "using_directive" && strings.Contains(astutil.NodeText(n, src), "ModelContextProtocol") {
			found = true
			return false
		}
		return true
	})
	return found
}

// csharpFindAttribute returns the first attribute on a declaration whose name
// matches want (namespace qualification and the "Attribute" suffix stripped),
// or nil. Handles both [A] and combined [A, B] attribute lists.
func csharpFindAttribute(decl *sitter.Node, src []byte, want string) *sitter.Node {
	for i := 0; i < int(decl.NamedChildCount()); i++ {
		al := decl.NamedChild(i)
		if al.Type() != "attribute_list" {
			continue
		}
		for j := 0; j < int(al.NamedChildCount()); j++ {
			attr := al.NamedChild(j)
			if attr.Type() != "attribute" {
				continue
			}
			if csharpAttrName(attr, src) == want {
				return attr
			}
		}
	}
	return nil
}

func csharpAttrName(attr *sitter.Node, src []byte) string {
	var nameNode *sitter.Node
	if nn := attr.ChildByFieldName("name"); nn != nil {
		nameNode = nn
	} else if attr.NamedChildCount() > 0 {
		nameNode = attr.NamedChild(0)
	}
	if nameNode == nil {
		return ""
	}
	name := astutil.NodeText(nameNode, src)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:] // strip namespace qualification
	}
	return strings.TrimSuffix(name, "Attribute")
}

// csharpFirstStringLiteral returns the unquoted value of the first string
// literal anywhere under n.
func csharpFirstStringLiteral(n *sitter.Node, src []byte) (string, bool) {
	var val string
	var ok bool
	astutil.Walk(n, func(c *sitter.Node) bool {
		if ok {
			return false
		}
		switch c.Type() {
		case "string_literal", "verbatim_string_literal":
			val = csharpUnquote(astutil.NodeText(c, src))
			ok = true
			return false
		}
		return true
	})
	return val, ok
}

func csharpUnquote(s string) string {
	s = strings.TrimPrefix(s, "@")
	s = strings.TrimPrefix(s, "$")
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func csharpNodeLocation(n *sitter.Node, pf ParsedFile) models.Location {
	return models.Location{
		FilePath: pf.RelPath,
		Line:     astutil.NodeLine(n),
		EndLine:  astutil.NodeEndLine(n),
	}
}
