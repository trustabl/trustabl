package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// TSADKHostedToolClasses is the closed set of @google/adk hosted-tool class
// names recognized by TS discovery. Source of truth: the core/src/tools/
// directory in google/adk-js, cross-checked against core/src/index.ts exports.
//
// PascalCase class instantiations (`new GoogleSearchTool()`), NOT camelCase
// factories like @openai/agents — ADK JS mirrors Python's class-based shape
// even though the user-defined FunctionTool moved to an options-object API.
//
// The 13 JS classes overlap partially with Python's ADKHostedToolClasses:
//   - Shared (7): AgentTool, ExitLoopTool, GoogleMapsGroundingTool,
//     GoogleSearchTool, LongRunningTool, UrlContextTool, VertexAiSearchTool.
//   - JS-only (6): LoadArtifactsTool, LoadMemoryTool, PreloadMemoryTool,
//     VertexRagRetrievalTool, RunSkillInlineScriptTool, RunSkillScriptTool.
//   - Python-only (6, deliberately absent here): BashTool, LangchainTool,
//     CrewaiTool, LoadWebPage, DiscoveryEngineSearchTool, EnterpriseSearchTool —
//     these have no JS factory in the @google/adk surface today.
var TSADKHostedToolClasses = map[string]bool{
	"AgentTool":                true,
	"ExitLoopTool":             true,
	"GoogleMapsGroundingTool":  true,
	"GoogleSearchTool":         true,
	"LoadArtifactsTool":        true,
	"LoadMemoryTool":           true,
	"LongRunningTool":          true,
	"PreloadMemoryTool":        true,
	"UrlContextTool":           true,
	"VertexAiSearchTool":       true,
	"VertexRagRetrievalTool":   true,
	"RunSkillInlineScriptTool": true,
	"RunSkillScriptTool":       true,
}

// IsTSADKHostedToolClass reports whether name is a recognized @google/adk
// hosted-tool class.
func IsTSADKHostedToolClass(name string) bool {
	return TSADKHostedToolClasses[name]
}

// classifyTSADKHostedNewExpression inspects a new_expression node inside an
// agent's tools: [...] list and returns a HostedToolDef + true if the
// constructor identifier resolves (via the alias map) to one of the known
// ADK hosted-tool classes. HostedToolDef carries SDK=SDKGoogleADK and the
// new-expression's Location.
//
// v1 limitation: only bare-identifier constructors are recognized.
// Namespace-import constructors like `new ns.GoogleSearchTool({...})`
// (a member_expression) are not handled — extend if that pattern becomes
// common in real-world @google/adk code.
func classifyTSADKHostedNewExpression(
	n *sitter.Node,
	aliases map[string]string,
	src []byte,
	filePath string,
) (models.HostedToolDef, bool) {
	if n == nil || n.Type() != "new_expression" {
		return models.HostedToolDef{}, false
	}
	ctor := n.ChildByFieldName("constructor")
	if ctor == nil || ctor.Type() != "identifier" {
		return models.HostedToolDef{}, false
	}
	canonical, ok := aliases[astutil.NodeText(ctor, src)]
	if !ok || !IsTSADKHostedToolClass(canonical) {
		return models.HostedToolDef{}, false
	}
	return models.HostedToolDef{
		Class: canonical,
		SDK:   models.SDKGoogleADK,
		Location: models.Location{
			FilePath: filePath,
			Line:     int(n.StartPoint().Row) + 1,
			EndLine:  int(n.EndPoint().Row) + 1,
		},
	}, true
}
