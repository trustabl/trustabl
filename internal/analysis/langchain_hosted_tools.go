package analysis

import "github.com/trustabl/trustabl/internal/models"

// LangChainHostedToolClasses is the set of LangChain / LangGraph built-in tool
// classes that represent high-risk capabilities: code execution (PythonREPLTool,
// PythonAstREPLTool from langchain_experimental), shell (ShellTool), and raw
// outbound HTTP (the Requests* family) from langchain_community. Benign built-ins
// are intentionally omitted so a match is always a meaningful security signal.
//
// They are recognized when they appear in an agent's resolved tools list and
// emitted as HostedToolDef edges, so agent-scope rules can flag the capability.
var LangChainHostedToolClasses = map[string]bool{
	"PythonREPLTool":     true,
	"PythonAstREPLTool":  true,
	"ShellTool":          true,
	"RequestsGetTool":    true,
	"RequestsPostTool":   true,
	"RequestsPatchTool":  true,
	"RequestsPutTool":    true,
	"RequestsDeleteTool": true,
}

// IsLangChainHostedToolClass reports whether className is a recognized
// high-risk LangChain built-in tool class.
func IsLangChainHostedToolClass(className string) bool {
	return LangChainHostedToolClasses[className]
}

// classifyLangChainHostedToolCall inspects an ExprCall item from a LangChain
// agent's tools=[...] list and returns a HostedToolDef + true when the callee
// names a recognized high-risk built-in tool class. Mirrors
// classifyADKHostedToolCall.
func classifyLangChainHostedToolCall(callItem models.Expr, filePath string) (models.HostedToolDef, bool) {
	if callItem.Kind != models.ExprCall {
		return models.HostedToolDef{}, false
	}
	name := calleeName(callItem.Text)
	if !IsLangChainHostedToolClass(name) {
		return models.HostedToolDef{}, false
	}
	return models.HostedToolDef{
		Class: name,
		SDK:   models.SDKLangChain,
		Location: models.Location{
			FilePath: filePath,
			Line:     callItem.Line,
			EndLine:  callItem.EndLine,
		},
		Kwargs: hostedKwargTree(callItem),
	}, true
}
