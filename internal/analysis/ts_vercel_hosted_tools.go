package analysis

import "strings"

// Vercel AI SDK provider-defined ("hosted") tools.
//
// Unlike the OpenAI Agents SDK (camelCase factory functions) and Google ADK
// (PascalCase class instantiations), Vercel exposes provider tools as a
// member call on a provider object: `anthropic.tools.bash_20250124()`,
// `openai.tools.localShell()`, `google.tools.codeExecution()`. The callee
// text discovery reads is therefore the full member path
// `<provider>.tools.<name>` (TSCalleeText cannot resolve this — the object is
// a runtime value, not an import alias — so the Vercel agent walk reads the
// member_expression text directly).
//
// canonicalVercelHostedClass normalizes that callee text into a stable class
// string by stripping any trailing `_<date>` version suffix that Anthropic's
// computer-use tools carry (bash_20250124 -> bash, computer_20241022 ->
// computer). The canonical string is what HostedToolRef.Class stores and what
// the vercel_ai agent rules list in `agent_uses_hosted_tool_class:`.

// vercelHostedToolPrefixes is the closed set of recognized provider-tool
// canonical names (after date-suffix stripping). Source: the Vercel AI SDK
// provider packages — @ai-sdk/anthropic (anthropic.tools.*), @ai-sdk/openai
// (openai.tools.*), and @ai-sdk/google (google.tools.*).
var vercelHostedToolPrefixes = map[string]bool{
	// @ai-sdk/anthropic — computer-use + code-execution provider tools.
	"anthropic.tools.bash":          true,
	"anthropic.tools.textEditor":    true,
	"anthropic.tools.computer":      true,
	"anthropic.tools.codeExecution": true,
	"anthropic.tools.webSearch":     true,
	// @ai-sdk/openai — responses-API provider tools.
	"openai.tools.localShell":         true,
	"openai.tools.computerUsePreview": true,
	"openai.tools.codeInterpreter":    true,
	"openai.tools.webSearch":          true,
	"openai.tools.webSearchPreview":   true,
	"openai.tools.fileSearch":         true,
	// @ai-sdk/google — Gemini provider tools.
	"google.tools.codeExecution": true,
	"google.tools.googleSearch":  true,
	"google.tools.urlContext":    true,
}

// canonicalVercelHostedClass strips a trailing `_<digits>` version suffix from
// the last path segment of a `<provider>.tools.<name>` callee and returns the
// canonical class string. `anthropic.tools.bash_20250124` -> `anthropic.tools.bash`.
// A callee with no recognized shape is returned unchanged.
func canonicalVercelHostedClass(callee string) string {
	dot := strings.LastIndex(callee, ".")
	if dot < 0 {
		return callee
	}
	head, name := callee[:dot+1], callee[dot+1:]
	if us := strings.LastIndex(name, "_"); us > 0 {
		// Only strip when everything after the underscore is digits (a date /
		// version stamp); a name like "code_execution" must survive intact —
		// but the Vercel surface uses camelCase, so an underscore here is
		// always a version suffix. Guard on digits anyway for safety.
		suffix := name[us+1:]
		allDigits := suffix != ""
		for _, r := range suffix {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			name = name[:us]
		}
	}
	return head + name
}

// IsVercelHostedTool reports whether class (a canonical
// `<provider>.tools.<name>` string, already date-suffix-stripped) is a
// recognized Vercel provider tool.
func IsVercelHostedTool(class string) bool {
	return vercelHostedToolPrefixes[class]
}

// vercelDangerousHostedClasses is the subset of provider tools that give the
// model shell, computer-control, or code-execution reach — the surface the
// VAI-006 agent rule flags. Web-search / URL-context retrieval tools are
// deliberately excluded (they are an SSRF-class concern, not RCE).
var vercelDangerousHostedClasses = []string{
	"anthropic.tools.bash",
	"anthropic.tools.computer",
	"anthropic.tools.codeExecution",
	"openai.tools.localShell",
	"openai.tools.computerUsePreview",
	"openai.tools.codeInterpreter",
	"google.tools.codeExecution",
}
