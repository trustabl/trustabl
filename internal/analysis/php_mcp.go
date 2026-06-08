package analysis

import (
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// The smacker tree-sitter-php grammar does not model PHP 8 attributes: a
// single-line `#[...]` is parsed as a `comment` node (since `#` begins a PHP
// line comment). So #[McpTool(...)] discovery reads the attribute out of the
// comment text immediately preceding the method, not from a structured attribute
// node. A multi-line `#[...]` attribute is a documented v1 gap.
var (
	phpAttrNameRe = regexp.MustCompile(`\bname\s*:\s*['"]([^'"]*)['"]`)
	phpAttrDescRe = regexp.MustCompile(`\bdescription\s*:\s*['"]([^'"]*)['"]`)
	// phpMcpToolNameRe matches the McpTool attribute name as a whole word, so a
	// different attribute such as #[McpToolbox] or #[McpToolFactory] is not
	// mistaken for #[McpTool].
	phpMcpToolNameRe = regexp.MustCompile(`\bMcpTool\b`)
)

// DiscoverPHPMCPTools walks each parsed PHP file and emits a ToolDef per
// #[McpTool]-attributed method — the PHP MCP SDK shape (the official mcp/sdk and
// the community php-mcp/server both use a `#[McpTool]` PHP 8 attribute):
//
//	use PhpMcp\Server\Attributes\McpTool;
//	class CalculatorTools {
//	    #[McpTool(name: 'add', description: 'Add two numbers')]
//	    public function add(int $a, int $b): int { return $a + $b; }
//	}
//
// Import-gated to files that `use` an Mcp namespace. Tools carry Kind=mcp_tool,
// Language=php, so deriveSDKsDetected stamps SDKMCP and the shared mcp/ pack's
// language:php rules audit them.
func DiscoverPHPMCPTools(files []ParsedFile, onFile func(string)) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverPHPMCPToolsInFile(pf)...)
	}
	return out
}

func discoverPHPMCPToolsInFile(pf ParsedFile) []models.ToolDef {
	if pf.Tree == nil {
		return nil
	}
	root := pf.Tree.RootNode()
	if !phpUsesMCP(root, pf.Source) {
		return nil
	}
	var out []models.ToolDef
	astutil.Walk(root, func(n *sitter.Node) bool {
		if n.Type() != "method_declaration" {
			return true
		}
		attr, ok := phpMcpToolAttrComment(n, pf.Source)
		if !ok {
			return true
		}
		td := models.ToolDef{
			Kind:     models.KindMCPTool,
			Language: models.LanguagePHP,
			Location: phpNodeLocation(n, pf),
		}
		// Name + description from the #[McpTool(name:..., description:...)] text;
		// name falls back to the method name.
		if m := phpAttrNameRe.FindStringSubmatch(attr); m != nil {
			td.Name = m[1]
		} else if nameNode := n.ChildByFieldName("name"); nameNode != nil {
			td.Name = astutil.NodeText(nameNode, pf.Source)
		}
		if m := phpAttrDescRe.FindStringSubmatch(attr); m != nil {
			td.Description = m[1]
		}
		// Params from the method signature; typed only when a type hint is present
		// (PHP type hints are optional).
		if params := n.ChildByFieldName("parameters"); params != nil {
			for i := 0; i < int(params.NamedChildCount()); i++ {
				p := params.NamedChild(i)
				if p.Type() != "simple_parameter" && p.Type() != "property_promotion_parameter" {
					continue
				}
				nameNode := p.ChildByFieldName("name")
				if nameNode == nil {
					continue
				}
				td.ParamNames = append(td.ParamNames, strings.TrimPrefix(astutil.NodeText(nameNode, pf.Source), "$"))
				if p.ChildByFieldName("type") != nil {
					td.HasTypedParams = true
				}
			}
		}
		out = append(out, td)
		return true
	})
	return out
}

// phpMcpToolAttrComment scans the comment nodes immediately preceding a method
// for a `#[McpTool(...)]` attribute (which the grammar parses as a comment) and
// returns its raw text.
func phpMcpToolAttrComment(method *sitter.Node, src []byte) (string, bool) {
	for cur := method.PrevNamedSibling(); cur != nil && cur.Type() == "comment"; cur = cur.PrevNamedSibling() {
		text := strings.TrimSpace(astutil.NodeText(cur, src))
		if strings.HasPrefix(text, "#[") && phpMcpToolNameRe.MatchString(text) {
			return text, true
		}
	}
	return "", false
}

// phpUsesMCP gates a file: true when a `use` statement references an Mcp
// namespace (covers the official `Mcp\...` and community `PhpMcp\...` roots).
func phpUsesMCP(root *sitter.Node, src []byte) bool {
	found := false
	astutil.Walk(root, func(n *sitter.Node) bool {
		if found {
			return false
		}
		if n.Type() == "namespace_use_declaration" && strings.Contains(astutil.NodeText(n, src), "Mcp\\") {
			found = true
			return false
		}
		return true
	})
	return found
}

func phpNodeLocation(n *sitter.Node, pf ParsedFile) models.Location {
	return models.Location{
		FilePath: pf.RelPath,
		Line:     astutil.NodeLine(n),
		EndLine:  astutil.NodeEndLine(n),
	}
}
