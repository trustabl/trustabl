package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func lcFindAgent(agents []models.AgentDef, class string) (models.AgentDef, bool) {
	for _, a := range agents {
		if a.Class == class {
			return a, true
		}
	}
	return models.AgentDef{}, false
}

func TestTSLangChainAgent_CreateReactAgent(t *testing.T) {
	src := `import { createReactAgent } from "@langchain/langgraph/prebuilt";
const agent = createReactAgent({ llm: model, tools: [searchTool], prompt: "Be helpful." });
`
	pf := parseTSForTest(t, "agent.ts", src)
	agents := analysis.DiscoverTSLangChainAgents([]analysis.ParsedFile{pf}, nil)
	a, ok := lcFindAgent(agents, "ReactAgent")
	if !ok {
		t.Fatalf("createReactAgent not discovered; got %+v", agents)
	}
	if a.SDK != models.SDKLangChain {
		t.Errorf("SDK: got %q", a.SDK)
	}
	if a.VarName != "agent" {
		t.Errorf("VarName: got %q, want agent", a.VarName)
	}
	if len(a.ToolRefs) != 1 || a.ToolRefs[0].Name != "searchTool" {
		t.Errorf("ToolRefs: got %+v, want [searchTool]", a.ToolRefs)
	}
}

func TestTSLangChainAgent_CreateAgent(t *testing.T) {
	src := `import { createAgent } from "langchain";
const agent = createAgent({ model: "openai:gpt-4o", tools: [searchTool], systemPrompt: "You help." });
`
	pf := parseTSForTest(t, "ca.ts", src)
	agents := analysis.DiscoverTSLangChainAgents([]analysis.ParsedFile{pf}, nil)
	a, ok := lcFindAgent(agents, "CreateAgent")
	if !ok {
		t.Fatalf("createAgent not discovered; got %+v", agents)
	}
	if a.Kwargs == nil || a.Kwargs.Children["systemPrompt"] == nil {
		t.Errorf("systemPrompt kwarg not captured: %+v", a.Kwargs)
	}
}

func TestTSLangChainAgent_AgentExecutor(t *testing.T) {
	src := `import { AgentExecutor } from "@langchain/classic/agents";
const ex = new AgentExecutor({ agent: a, tools: [searchTool], maxIterations: 5 });
`
	pf := parseTSForTest(t, "ex.ts", src)
	agents := analysis.DiscoverTSLangChainAgents([]analysis.ParsedFile{pf}, nil)
	a, ok := lcFindAgent(agents, "AgentExecutor")
	if !ok {
		t.Fatalf("new AgentExecutor not discovered; got %+v", agents)
	}
	if a.Kwargs == nil || a.Kwargs.Children["maxIterations"] == nil {
		t.Errorf("maxIterations kwarg not captured: %+v", a.Kwargs)
	}
}

func TestTSLangChainAgent_GateExcludesNonLangChain(t *testing.T) {
	src := `function createReactAgent(x) { return x; }
const a = createReactAgent({ llm: m, tools: [] });
`
	pf := parseTSForTest(t, "no_lc.ts", src)
	agents := analysis.DiscoverTSLangChainAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 0 {
		t.Errorf("non-langchain createReactAgent should not be discovered; got %+v", agents)
	}
}
