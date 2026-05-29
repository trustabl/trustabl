package analysis

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// DiscoverClaudeAgentOptions finds every ClaudeAgentOptions(...) construction in
// the parsed files and captures its constructor kwargs. ClaudeAgentOptions is the
// claude-agent-sdk session-configuration object (passed to ClaudeSDKClient /
// query); its permission_mode kwarg is the session-wide analogue of
// .claude/settings.json's defaultMode and the place most apps actually set a
// permission bypass. It is not an agent, so it is collected separately from
// DiscoverAgents into its own repo-scope inventory slice.
//
// Matching is by callee short-name: a bare ClaudeAgentOptions(...) or a dotted
// access like sdk.ClaudeAgentOptions(...) (matched on the final segment),
// mirroring how discoverAgentsInFile resolves agent constructors.
func DiscoverClaudeAgentOptions(files []ParsedFile) []models.ClaudeAgentOptionsDef {
	var out []models.ClaudeAgentOptionsDef
	for _, pf := range files {
		astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
			if n.Type() != "call" {
				return true
			}
			name := astutil.NodeText(n.ChildByFieldName("function"), pf.Source)
			if i := strings.LastIndex(name, "."); i >= 0 {
				name = name[i+1:]
			}
			if name != "ClaudeAgentOptions" {
				return true
			}
			kwargs, opaque := extractCallKwargs(n, pf.Source)
			out = append(out, models.ClaudeAgentOptionsDef{
				Location: models.Location{
					FilePath: pf.RelPath,
					Line:     int(n.StartPoint().Row) + 1,
					EndLine:  int(n.EndPoint().Row) + 1,
				},
				Kwargs: kwargs,
				Opaque: opaque,
			})
			return true
		})
	}
	return out
}
