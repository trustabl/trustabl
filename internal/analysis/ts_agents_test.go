package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// findAgentByClass returns the first agent whose Class matches; nil if none.
func findAgentByClass(agents []models.AgentDef, class string) *models.AgentDef {
	for i := range agents {
		if agents[i].Class == class {
			return &agents[i]
		}
	}
	return nil
}

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
	// Expect 2: 1 QueryMainAgent (the query() call itself) + 1 AgentDefinition
	// sub-agent (analyst). The main agent is always emitted for query() calls.
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2 (main + analyst): %+v", len(agents), agents)
	}
	a := findAgentByClass(agents, "AgentDefinition")
	if a == nil {
		t.Fatalf("missing AgentDefinition sub-agent in %+v", agents)
	}
	if a.Name != "analyst" {
		t.Errorf("Name: got %q want %q", a.Name, "analyst")
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
	// Expect 2: 1 QueryMainAgent (not opaque — options is inline) + 1 opaque
	// sub-agent (dynamic).
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2 (main + opaque dynamic): %+v", len(agents), agents)
	}
	sub := findAgentByClass(agents, "AgentDefinition")
	if sub == nil || !sub.Opaque {
		t.Errorf("expected an opaque AgentDefinition sub-agent, got %+v", agents)
	}
}

func TestDiscoverTSAgents_OnlyMainAgentWhenOptionsIsCall(t *testing.T) {
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";
const q = query(getOptions());
`
	pf := parseTSForTest(t, "src/a.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	// Expect 1: just the QueryMainAgent, marked Opaque because arg 0 is a
	// function call rather than an object literal. Sub-agent extraction
	// correctly yields zero (we cannot see inside getOptions()).
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1 (opaque QueryMainAgent): %+v", len(agents), agents)
	}
	a := agents[0]
	if a.Class != "QueryMainAgent" || !a.Opaque {
		t.Errorf("expected QueryMainAgent with Opaque=true, got %+v", a)
	}
}

func TestDiscoverTSAgents_MCPServerRefsFromOptions(t *testing.T) {
	src := `
import { query, createSdkMcpServer } from "@anthropic-ai/claude-agent-sdk";

const srv = createSdkMcpServer({ name: "x" });

const q = query({
  options: {
    agents: {
      a: { description: "a", prompt: "p" }
    },
    mcpServers: {
      inline: { type: "stdio", command: "x" },
      byref:  srv
    }
  }
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	// Expect 2: 1 QueryMainAgent + 1 AgentDefinition sub-agent. Both carry
	// MCPServerRefs from options.mcpServers.
	if len(agents) != 2 {
		t.Fatalf("want 2 agents (main + a), got %d: %+v", len(agents), agents)
	}
	sub := findAgentByClass(agents, "AgentDefinition")
	if sub == nil {
		t.Fatalf("missing AgentDefinition sub-agent in %+v", agents)
	}
	refs := sub.MCPServerRefs
	if len(refs) != 2 {
		t.Fatalf("want 2 MCPServerRefs on sub-agent, got %d: %+v", len(refs), refs)
	}
	var sawInline, sawByref bool
	for _, r := range refs {
		switch r.Class {
		case "McpStdioServerConfig":
			sawInline = true
		case "createSdkMcpServer":
			sawByref = true
		}
	}
	if !sawInline || !sawByref {
		t.Errorf("missing one of the expected refs: %+v", refs)
	}
}

// === Framing-1 main-thread-agent tests (new in this fix) ===

func TestDiscoverTSAgents_QueryMainAgent_OpaqueWhenOptionsIsIdentifier(t *testing.T) {
	// Real-world shape from testdata/corpus/email-agent/ccsdk/ai-client.ts:
	// options is a computed variable, not an inline object.
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";

class AIClient {
  defaults: any = {};
  async *queryStream(prompt: string) {
    const merged = { ...this.defaults };
    for await (const m of query({ prompt, options: merged })) {
      yield m;
    }
  }
}
`
	pf := parseTSForTest(t, "src/ai-client.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 {
		t.Fatalf("got %d, want 1 QueryMainAgent: %+v", len(agents), agents)
	}
	a := agents[0]
	if a.Class != "QueryMainAgent" {
		t.Errorf("Class: got %q want QueryMainAgent", a.Class)
	}
	if !a.Opaque {
		t.Errorf("expected Opaque=true (options is computed identifier), got false")
	}
	if a.SDK != models.SDKClaudeAgentSDK {
		t.Errorf("SDK: got %q", a.SDK)
	}
	if a.Language != models.LanguageTypeScript {
		t.Errorf("Language: got %q", a.Language)
	}
	if a.FilePath != "src/ai-client.ts" {
		t.Errorf("FilePath: got %q", a.FilePath)
	}
}

func TestDiscoverTSAgents_QueryMainAgent_InlineOptionsCapturesKwargs(t *testing.T) {
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";

const q = query({
  prompt: "Hello",
  options: {
    model: "opus",
    allowedTools: ["Bash", "Read"],
    appendSystemPrompt: "Be helpful"
  }
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1 QueryMainAgent: %+v", len(agents), agents)
	}
	a := agents[0]
	if a.Class != "QueryMainAgent" {
		t.Errorf("Class: got %q want QueryMainAgent", a.Class)
	}
	if a.Opaque {
		t.Errorf("expected Opaque=false (options is inline), got true")
	}
	if a.Name != "q" {
		t.Errorf("Name: got %q want %q (assignment-target)", a.Name, "q")
	}
	if a.Kwargs == nil || a.Kwargs.Children["prompt"] == nil {
		t.Errorf("Kwargs.prompt missing: %+v", a.Kwargs)
	}
	if a.Kwargs.Children["options"] == nil ||
		a.Kwargs.Children["options"].Children["model"] == nil {
		t.Errorf("Kwargs.options.model missing: %+v", a.Kwargs)
	}
	// allowedTools strings → ToolRefs on the main agent.
	if len(a.ToolRefs) != 2 {
		t.Errorf("ToolRefs: got %d, want 2 (Bash+Read): %+v", len(a.ToolRefs), a.ToolRefs)
	}
}

func TestDiscoverTSAgents_QueryMainAgent_NameFromEnclosingFunction(t *testing.T) {
	// for-await-of binds `m`, not the query() call. When no const binding
	// exists, fall back to the enclosing function name.
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";

async function run() {
  for await (const m of query({ prompt: "x", options: {} })) {}
}
`
	pf := parseTSForTest(t, "src/a.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(agents), agents)
	}
	if agents[0].Name != "run" {
		t.Errorf("Name: got %q want \"run\" (enclosing function)", agents[0].Name)
	}
}

func TestDiscoverTSAgents_QueryMainAgent_NameFromClassMethod(t *testing.T) {
	// Real-world shape from testdata/corpus/email-agent/ccsdk/ai-client.ts:
	// query() is inside a method of a class. Name should be Class.method.
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";

class AIClient {
  async *queryStream(prompt: string) {
    for await (const m of query({ prompt, options: {} })) {
      yield m;
    }
  }
}
`
	pf := parseTSForTest(t, "src/ai-client.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(agents), agents)
	}
	if agents[0].Name != "AIClient.queryStream" {
		t.Errorf("Name: got %q want \"AIClient.queryStream\"", agents[0].Name)
	}
}

func TestDiscoverTSAgents_QueryMainAgent_AllowedToolsFallbackWhenOpaque(t *testing.T) {
	// The common real-world shape: allowedTools is defined in a class field
	// or constant elsewhere in the file, options is computed at the query()
	// call site. Even though options is opaque, the file-scoped fallback
	// surfaces the agent's tool permissions as ToolRefs.
	src := `
import { query } from "@anthropic-ai/claude-agent-sdk";

class AIClient {
  defaults = {
    allowedTools: ["Bash", "Read", "mcp__email__search_inbox"]
  };
  async *run(p: string) {
    const merged = { ...this.defaults };
    for await (const m of query({ prompt: p, options: merged })) { yield m; }
  }
}
`
	pf := parseTSForTest(t, "src/ai-client.ts", src)
	agents := analysis.DiscoverTSAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(agents), agents)
	}
	a := agents[0]
	if !a.Opaque {
		t.Errorf("expected Opaque=true (options is computed), got false")
	}
	if len(a.ToolRefs) != 3 {
		t.Fatalf("ToolRefs: got %d, want 3 from file-scoped allowedTools: %+v", len(a.ToolRefs), a.ToolRefs)
	}
	want := map[string]bool{"Bash": true, "Read": true, "mcp__email__search_inbox": true}
	for _, r := range a.ToolRefs {
		if !want[r.Name] {
			t.Errorf("unexpected ToolRef %q", r.Name)
		}
		delete(want, r.Name)
	}
	if len(want) > 0 {
		t.Errorf("missing ToolRefs: %v", want)
	}
}
