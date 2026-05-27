package analysis

import (
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// adkFunctionToolArg extracts the inner symbol from a FunctionTool(symbol)
// call's source text. Returns ("", false) unless the text is exactly a
// FunctionTool wrapper around a single bare identifier (e.g. nested calls,
// keyword args, or attribute accesses are not resolvable to a same-file
// ToolDef and so are left for External classification).
func adkFunctionToolArg(text string) (string, bool) {
	const prefix = "FunctionTool("
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, ")") {
		return "", false
	}
	inner := strings.TrimSpace(text[len(prefix) : len(text)-1])
	if inner == "" || strings.ContainsAny(inner, "(),=. ") {
		return "", false
	}
	return inner, true
}

// ADKHostedToolClasses is the closed set of Google ADK built-in tool classes
// recognized by discovery. Source of truth: google/adk-python's
// src/google/adk/tools/ directory.
var ADKHostedToolClasses = map[string]bool{
	"BashTool":                  true,
	"GoogleSearchTool":          true,
	"VertexAiSearchTool":        true,
	"LangchainTool":             true,
	"CrewaiTool":                true,
	"AgentTool":                 true,
	"LongRunningTool":           true,
	"LoadWebPage":               true,
	"ExitLoopTool":              true,
	"GoogleMapsGroundingTool":   true,
	"UrlContextTool":            true,
	"DiscoveryEngineSearchTool": true,
	"EnterpriseSearchTool":      true,
}

// IsADKHostedToolClass reports whether className is a recognized ADK
// built-in tool class.
func IsADKHostedToolClass(className string) bool { return ADKHostedToolClasses[className] }

// classifyADKHostedToolCall inspects an ExprCall item from an ADK agent's
// tools=[...] list and returns a HostedToolDef + true if the callee names an
// ADK built-in tool class.
// Line and EndLine are read from callItem's own position, not the agent's line.
func classifyADKHostedToolCall(callItem models.Expr, filePath string) (models.HostedToolDef, bool) {
	if callItem.Kind != models.ExprCall {
		return models.HostedToolDef{}, false
	}
	name := calleeName(callItem.Text)
	if !IsADKHostedToolClass(name) {
		return models.HostedToolDef{}, false
	}
	return models.HostedToolDef{
		Class: name,
		SDK:   models.SDKGoogleADK,
		Location: models.Location{
			FilePath: filePath,
			Line:     callItem.Line,
			EndLine:  callItem.EndLine,
		},
	}, true
}
