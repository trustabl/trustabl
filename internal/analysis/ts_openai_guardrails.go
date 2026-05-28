package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// tsOpenAIGuardrailFactories maps each define*Guardrail factory function
// name to its GuardrailKind. Source of truth: packages/agents-core/src/
// {guardrail.ts, toolGuardrail.ts} in openai/openai-agents-js.
var tsOpenAIGuardrailFactories = map[string]models.GuardrailKind{
	"defineInputGuardrail":      models.GuardrailInput,
	"defineOutputGuardrail":     models.GuardrailOutput,
	"defineToolInputGuardrail":  models.GuardrailToolInput,
	"defineToolOutputGuardrail": models.GuardrailToolOutput,
}

// DiscoverTSOpenAIGuardrails walks each parsed TS file and emits a
// GuardrailDef per defineX guardrail factory call. Import-gated to the
// @openai/agents family.
func DiscoverTSOpenAIGuardrails(files []ParsedFile, onFile func(string)) []models.GuardrailDef {
	var out []models.GuardrailDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSOpenAIGuardrailsInFile(pf)...)
	}
	return out
}

func discoverTSOpenAIGuardrailsInFile(pf ParsedFile) []models.GuardrailDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesAny(pf.Tree.RootNode(), pf.Source, tsOpenAIAgentsModules)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.GuardrailDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call_expression" {
			return true
		}
		canon := astutil.TSCalleeText(n, pf.Source, aliases)
		kind, ok := tsOpenAIGuardrailFactories[canon]
		if !ok {
			return true
		}
		g := models.GuardrailDef{
			Kind: kind,
			Location: models.Location{
				FilePath: pf.RelPath,
				Line:     int(n.StartPoint().Row) + 1,
				EndLine:  int(n.EndPoint().Row) + 1,
			},
			VarName: directAssignmentName(n, pf.Source),
		}
		// Pull options.name when arg 0 is an object literal with a string `name`.
		if args := n.ChildByFieldName("arguments"); args != nil && args.NamedChildCount() > 0 {
			if arg0 := args.NamedChild(0); arg0.Type() == "object" {
				if nameNode := getObjectProperty(arg0, "name", pf.Source); nameNode != nil &&
					nameNode.Type() == "string" {
					g.Name = unquote(astutil.NodeText(nameNode, pf.Source))
				}
			}
		}
		out = append(out, g)
		return true
	})
	return out
}
