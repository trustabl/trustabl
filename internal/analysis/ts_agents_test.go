package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverTSAgents_InlineInQuery(t *testing.T) {
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";

const q = query({
  prompt: "Analyze",
  options: {
    agents: {
      analyst: {
        description: "Data analyst",
        prompt: "Analyze data"
      }
    }
  }
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1: %+v", len(agents), agents)
	}
	a := agents[0]
	if a.Name != "analyst" {
		t.Errorf("Name: got %q want %q", a.Name, "analyst")
	}
	if a.Class != "AgentDefinition" {
		t.Errorf("Class: got %q want %q", a.Class, "AgentDefinition")
	}
	if a.SDK != models.SDKClaudeAgentSDK {
		t.Errorf("SDK: got %q want %q", a.SDK, models.SDKClaudeAgentSDK)
	}
	if a.Language != models.LanguageTypeScript {
		t.Errorf("Language: got %q want %q", a.Language, models.LanguageTypeScript)
	}
	if a.Kwargs == nil || a.Kwargs.Children["description"] == nil {
		t.Errorf("Kwargs.description missing: %+v", a.Kwargs)
	}
}

func TestDiscoverTSAgents_TypedConst(t *testing.T) {
	src := `
import { AgentDefinition } from "@anthropic-ai/claude-agent-sdk";

const reviewer: AgentDefinition = {
  description: "Code review specialist",
  prompt: "You are a code reviewer..."
};

export const auditor: AgentDefinition = {
  description: "Auditor",
  prompt: "Audit"
};
`
	pf := parseTSForTest(t, "src/a.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2: %+v", len(agents), agents)
	}
	names := map[string]bool{agents[0].Name: true, agents[1].Name: true}
	for _, want := range []string{"reviewer", "auditor"} {
		if !names[want] {
			t.Errorf("missing agent %q in %+v", want, names)
		}
	}
	for _, a := range agents {
		if a.VarName != a.Name {
			t.Errorf("agent %q: VarName=%q, want same as Name", a.Name, a.VarName)
		}
		if a.Language != models.LanguageTypeScript {
			t.Errorf("Language: got %q", a.Language)
		}
	}
}

func TestDiscoverTSAgents_ToolRefsFromBuiltinStrings(t *testing.T) {
	src := `
import { AgentDefinition } from "@anthropic-ai/claude-agent-sdk";

const x: AgentDefinition = {
  description: "x",
  prompt: "y",
  tools: ["Read", "Bash"]
};
`
	pf := parseTSForTest(t, "src/a.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 || len(agents[0].ToolRefs) != 2 {
		t.Fatalf("got %+v, want one agent with 2 ToolRefs", agents)
	}
	got := []string{agents[0].ToolRefs[0].Name, agents[0].ToolRefs[1].Name}
	want := []string{"Read", "Bash"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("ToolRef[%d]: got %q want %q", i, got[i], w)
		}
	}
}

func TestDiscoverTSAgents_OpaqueWhenAgentValueIsCall(t *testing.T) {
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";

const q = query({
  options: {
    agents: {
      dynamic: makeAgent()
    }
  }
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 || !agents[0].Opaque {
		t.Errorf("expected one opaque agent, got %+v", agents)
	}
}

func TestDiscoverTSAgents_NoExtractionWhenOptionsIsCall(t *testing.T) {
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";
const q = query(getOptions());
`
	pf := parseTSForTest(t, "src/a.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 0 {
		t.Errorf("expected zero agents (options non-literal), got %+v", agents)
	}
}
