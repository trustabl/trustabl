package analysis

import "github.com/trustabl/trustabl/internal/models"

// CrewAIHostedToolClasses is the set of CrewAI built-in / crewai_tools tool
// classes that represent high-risk capabilities: model-driven code execution
// (CodeInterpreterTool), unconstrained filesystem read/write (FileReadTool,
// FileWriterTool, DirectoryReadTool, DirectorySearchTool), and tools that fetch
// model-chosen URLs (ScrapeWebsiteTool, SeleniumScrapingTool, WebsiteSearchTool,
// SerperDevTool, JSONSearchTool, PDFSearchTool, CSVSearchTool). Benign built-ins
// are intentionally omitted so a match is always a meaningful security signal.
//
// They are recognized when they appear in a CrewAI agent's tools=[...] list and
// emitted as HostedToolDef edges, so the agent-scope rules CREW-103 / CREW-106 /
// CREW-107 can flag the capability. FileWriteTool is the older spelling of
// FileWriterTool; both are listed so a rename in the (archived) crewai_tools repo
// stays covered.
var CrewAIHostedToolClasses = map[string]bool{
	"CodeInterpreterTool":  true,
	"FileReadTool":         true,
	"FileWriterTool":       true,
	"FileWriteTool":        true,
	"DirectoryReadTool":    true,
	"DirectorySearchTool":  true,
	"ScrapeWebsiteTool":    true,
	"SeleniumScrapingTool": true,
	"WebsiteSearchTool":    true,
	"SerperDevTool":        true,
	"JSONSearchTool":       true,
	"PDFSearchTool":        true,
	"CSVSearchTool":        true,
}

// IsCrewAIHostedToolClass reports whether className is a recognized high-risk
// CrewAI built-in tool class.
func IsCrewAIHostedToolClass(className string) bool {
	return CrewAIHostedToolClasses[className]
}

// classifyCrewAIHostedToolCall inspects an ExprCall item from a CrewAI agent's
// tools=[...] list and returns a HostedToolDef + true when the callee names a
// recognized high-risk built-in tool class. Mirrors
// classifyLangChainHostedToolCall; the HostedToolDef.SDK is stamped SDKCrewAI so
// a CrewAI CodeInterpreterTool and the OpenAI hosted tool of the same name are
// evaluated by different rules (CREW-103 only fires against crewai_agent).
func classifyCrewAIHostedToolCall(callItem models.Expr, filePath string) (models.HostedToolDef, bool) {
	if callItem.Kind != models.ExprCall {
		return models.HostedToolDef{}, false
	}
	name := calleeName(callItem.Text)
	if !IsCrewAIHostedToolClass(name) {
		return models.HostedToolDef{}, false
	}
	return models.HostedToolDef{
		Class: name,
		SDK:   models.SDKCrewAI,
		Location: models.Location{
			FilePath: filePath,
			Line:     callItem.Line,
			EndLine:  callItem.EndLine,
		},
		Kwargs: hostedKwargTree(callItem),
	}, true
}
