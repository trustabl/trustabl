package analysis

import (
	"sort"

	"github.com/trustabl/trustabl/internal/models"
)

// HostedToolClasses is the closed set of OpenAI Agents SDK hosted tool classes
// recognized by discovery. Source of truth: openai-agents-python/src/agents/tool.py.
// Adding a new class here is the only place to extend hosted-tool detection.
var HostedToolClasses = map[string]bool{
	"WebSearchTool":       true,
	"FileSearchTool":      true,
	"ComputerTool":        true,
	"HostedMCPTool":       true,
	"CodeInterpreterTool": true,
	"ImageGenerationTool": true,
	"LocalShellTool":      true,
	"ShellTool":           true,
	"ApplyPatchTool":      true,
	"CustomTool":          true,
	"ToolSearchTool":      true,
}

// IsHostedToolClass reports whether className is a recognized OpenAI Agents
// SDK hosted-tool class.
func IsHostedToolClass(className string) bool { return HostedToolClasses[className] }

// classifyHostedToolCall inspects an ExprCall item from a tools=[...] list
// and returns a HostedToolDef + true if the callee names a hosted-tool class.
func classifyHostedToolCall(callItem models.Expr, filePath string, line int) (models.HostedToolDef, bool) {
	if callItem.Kind != models.ExprCall {
		return models.HostedToolDef{}, false
	}
	name := calleeName(callItem.Text)
	if !IsHostedToolClass(name) {
		return models.HostedToolDef{}, false
	}
	return models.HostedToolDef{
		Class:    name,
		SDK:      models.SDKOpenAIAgents,
		FilePath: filePath,
		Line:     line,
	}, true
}

func calleeName(callText string) string {
	for i, r := range callText {
		if r == '(' {
			return callText[:i]
		}
	}
	return callText
}

// sortHostedTools sorts by (FilePath, Line, Class) for deterministic output.
func sortHostedTools(hs []models.HostedToolDef) {
	sort.Slice(hs, func(i, j int) bool {
		if hs[i].FilePath != hs[j].FilePath {
			return hs[i].FilePath < hs[j].FilePath
		}
		if hs[i].Line != hs[j].Line {
			return hs[i].Line < hs[j].Line
		}
		return hs[i].Class < hs[j].Class
	})
}
