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
// Line and EndLine are read from callItem's own position, not the agent's line.
func classifyHostedToolCall(callItem models.Expr, filePath string) (models.HostedToolDef, bool) {
	if callItem.Kind != models.ExprCall {
		return models.HostedToolDef{}, false
	}
	name := calleeName(callItem.Text)
	if !IsHostedToolClass(name) {
		return models.HostedToolDef{}, false
	}
	return models.HostedToolDef{
		Class: name,
		SDK:   models.SDKOpenAIAgents,
		Location: models.Location{
			FilePath: filePath,
			Line:     callItem.Line,
			EndLine:  callItem.EndLine,
		},
		Kwargs: hostedKwargTree(callItem),
	}, true
}

// hostedKwargTree wraps a hosted-tool call's captured kwargs into a KwargTree,
// or returns nil when the call had no keyword arguments.
func hostedKwargTree(callItem models.Expr) *models.KwargTree {
	if len(callItem.CallKwargs) == 0 {
		return nil
	}
	return &models.KwargTree{Children: callItem.CallKwargs}
}

func calleeName(callText string) string {
	for i, r := range callText {
		if r == '(' {
			return callText[:i]
		}
	}
	return callText
}

// sortHostedTools sorts hs in-place by (FilePath, Line, Class) and returns a
// permutation slice where oldToNew[oldIndex] = newIndex. The caller uses this
// to remap pre-sort DefIndex values on HostedToolRef after the sort. Uses
// SliceStable so equal elements keep a deterministic relative order.
func sortHostedTools(hs []models.HostedToolDef) []int {
	type indexed struct {
		def models.HostedToolDef
		old int
	}
	tmp := make([]indexed, len(hs))
	for i, h := range hs {
		tmp[i] = indexed{def: h, old: i}
	}
	sort.SliceStable(tmp, func(i, j int) bool {
		a, b := tmp[i].def, tmp[j].def
		if a.FilePath != b.FilePath {
			return a.FilePath < b.FilePath
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Class < b.Class
	})
	oldToNew := make([]int, len(hs))
	for newIdx, it := range tmp {
		hs[newIdx] = it.def
		oldToNew[it.old] = newIdx
	}
	return oldToNew
}
