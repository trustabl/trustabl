package analysis

import (
	"strings"

	"github.com/trustabl/trustabl/internal/models"
)

// Pydantic AI built-in (native) tool discovery.
//
// Pydantic AI ships provider-native tools — code execution, web fetch, URL
// context, web search — that the model can invoke directly. The dangerous
// subset (model-driven code execution and model-chosen URL fetching) is
// recognized so the agent-scope rules PYD-102 / PYD-103 can flag the
// capability. Benign provider tools are intentionally omitted so a match is
// always a meaningful security signal.
//
// Native tools are wired onto an agent in two shapes:
//
//	capabilities=[NativeTool(CodeExecutionTool())]   (modern wrapper form)
//	builtin_tools=[CodeExecutionTool()]              (legacy direct form)
//
// They live under the `capabilities=` and `builtin_tools=` kwargs, NOT the
// generic `tools=` list, so ResolveEdges scans those two kwargs for Pydantic
// agents. The modern form wraps the tool in a NativeTool(...) call;
// classifyPydanticAIHostedToolCall unwraps that wrapper and classifies the inner
// class.

// PydanticAIHostedToolClasses is the dangerous subset of Pydantic AI native
// tool classes: CodeExecutionTool runs model-generated code; WebFetchTool /
// UrlContextTool / WebSearchTool fetch model-chosen URLs (the SSRF and
// data-exfiltration surface). Benign native tools are deliberately excluded.
var PydanticAIHostedToolClasses = map[string]bool{
	"CodeExecutionTool": true,
	"WebFetchTool":      true,
	"UrlContextTool":    true,
	"WebSearchTool":     true,
}

// IsPydanticAIHostedToolClass reports whether className is a recognized
// high-risk Pydantic AI native tool class.
func IsPydanticAIHostedToolClass(className string) bool {
	return PydanticAIHostedToolClasses[className]
}

// nativeToolInnerClass unwraps a `NativeTool(<inner>())` wrapper and returns the
// inner tool class name. Pydantic's modern native-tool form wraps the tool class
// in a NativeTool(...) call (`NativeTool(CodeExecutionTool())`), so the list
// item's own callee is NativeTool, not the tool class — the classifier must
// descend one level. Returns ("", false) when text is not a NativeTool(...)
// wrapper.
func nativeToolInnerClass(text string) (string, bool) {
	const prefix = "NativeTool("
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, ")") {
		return "", false
	}
	inner := strings.TrimSpace(text[len(prefix) : len(text)-1])
	if inner == "" {
		return "", false
	}
	// inner is the wrapped call (e.g. "CodeExecutionTool()" or
	// "tools.CodeExecutionTool(timeout=30)"); calleeName strips the arg list and
	// any module/attribute qualifier down to the bare class name.
	return calleeName(inner), true
}

// classifyPydanticAIHostedToolCall inspects an ExprCall item from a Pydantic
// agent's capabilities=[...] or builtin_tools=[...] list and returns a
// HostedToolDef + true when the callee (or, for the NativeTool(...) wrapper, the
// inner call) names a recognized high-risk native tool class. Mirrors
// classifyCrewAIHostedToolCall; the HostedToolDef.SDK is stamped SDKPydanticAI
// so a Pydantic CodeExecutionTool and the OpenAI hosted tool of the same name
// are evaluated by different rules (PYD-102 only fires against pydantic_ai_agent).
func classifyPydanticAIHostedToolCall(callItem models.Expr, filePath string) (models.HostedToolDef, bool) {
	if callItem.Kind != models.ExprCall {
		return models.HostedToolDef{}, false
	}
	name := calleeName(callItem.Text)
	// Unwrap the modern NativeTool(...) wrapper to the inner tool class.
	if inner, ok := nativeToolInnerClass(callItem.Text); ok {
		name = inner
	}
	if !IsPydanticAIHostedToolClass(name) {
		return models.HostedToolDef{}, false
	}
	return models.HostedToolDef{
		Class: name,
		SDK:   models.SDKPydanticAI,
		Location: models.Location{
			FilePath: filePath,
			Line:     callItem.Line,
			EndLine:  callItem.EndLine,
		},
		Kwargs: hostedKwargTree(callItem),
	}, true
}
