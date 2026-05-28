package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// TSOpenAIHostedToolFactories is the closed set of @openai/agents hosted-tool
// factory function names recognized by TS discovery. Source of truth:
// packages/agents-core/src/tool.ts and packages/agents-openai/src/tools.ts in
// openai/openai-agents-js. camelCase factory functions, NOT PascalCase classes
// (the Python SDK uses class instantiation; the JS SDK uses factory calls).
var TSOpenAIHostedToolFactories = map[string]bool{
	// from @openai/agents-core
	"computerTool":   true,
	"shellTool":      true,
	"applyPatchTool": true,
	"hostedMcpTool":  true,
	// from @openai/agents-openai
	"webSearchTool":       true,
	"fileSearchTool":      true,
	"codeInterpreterTool": true,
	"imageGenerationTool": true,
	"toolSearchTool":      true,
}

// IsTSOpenAIHostedToolFactory reports whether name is a recognized
// @openai/agents hosted-tool factory function.
func IsTSOpenAIHostedToolFactory(name string) bool {
	return TSOpenAIHostedToolFactories[name]
}

// classifyTSOpenAIHostedFactoryCall inspects a call_expression node inside
// an agent's tools: [...] list and returns a HostedToolDef + true if the
// callee resolves (via aliases) to one of the known factory names.
// HostedToolDef carries SDK=SDKOpenAIAgents and the call site's Location.
func classifyTSOpenAIHostedFactoryCall(
	call *sitter.Node,
	aliases map[string]string,
	src []byte,
	filePath string,
) (models.HostedToolDef, bool) {
	if call == nil || call.Type() != "call_expression" {
		return models.HostedToolDef{}, false
	}
	canonical := astutil.TSCalleeText(call, src, aliases)
	if !IsTSOpenAIHostedToolFactory(canonical) {
		return models.HostedToolDef{}, false
	}
	return models.HostedToolDef{
		Class: canonical,
		SDK:   models.SDKOpenAIAgents,
		Location: models.Location{
			FilePath: filePath,
			Line:     int(call.StartPoint().Row) + 1,
			EndLine:  int(call.EndPoint().Row) + 1,
		},
	}, true
}
