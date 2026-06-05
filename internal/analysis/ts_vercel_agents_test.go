package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func vaiHasHostedClass(a models.AgentDef, class string) bool {
	for _, ref := range a.HostedToolRefs {
		if ref.Class == class {
			return true
		}
	}
	return false
}

func vaiHasToolRef(a models.AgentDef, name string) bool {
	for _, ref := range a.ToolRefs {
		if ref.Name == name {
			return true
		}
	}
	return false
}

// The core new mechanic: tools is an OBJECT RECORD, not an array. The walk must
// resolve a bare-identifier value to a ToolRef and a provider hosted-tool call
// to a HostedToolRef whose canonical Class has the date suffix stripped.
func TestTSVercel_AgentObjectRecordTools(t *testing.T) {
	src := `import { generateText } from "ai";
import { anthropic } from "@ai-sdk/anthropic";
const result = await generateText({
  model: anthropic("claude-sonnet-4"),
  tools: {
    weather: weatherTool,
    bash: anthropic.tools.bash_20250124(),
  },
});
`
	pf := parseTSForTest(t, "agent.ts", src)
	agents := analysis.DiscoverTSVercelAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d (%+v)", len(agents), agents)
	}
	a := agents[0]
	if a.SDK != models.SDKVercelAI {
		t.Errorf("SDK: got %q, want vercel_ai", a.SDK)
	}
	if a.Class != "GenerateText" {
		t.Errorf("Class: got %q, want GenerateText", a.Class)
	}
	if a.Language != models.LanguageTypeScript {
		t.Errorf("Language: got %q, want typescript", a.Language)
	}
	if !vaiHasToolRef(a, "weatherTool") {
		t.Errorf("expected a ToolRef{Name: weatherTool} from the record value; got %+v", a.ToolRefs)
	}
	if !vaiHasHostedClass(a, "anthropic.tools.bash") {
		t.Errorf("expected HostedToolRef class anthropic.tools.bash (date suffix stripped); got %+v", a.HostedToolRefs)
	}
}

// A bare generateText({model, prompt}) with NO tools is a one-shot completion,
// not an agent — discovery must emit nothing.
func TestTSVercel_NoToolsNoAgent(t *testing.T) {
	src := `import { generateText } from "ai";
import { openai } from "@ai-sdk/openai";
const r = await generateText({ model: openai("gpt-5"), prompt: "hi" });
`
	pf := parseTSForTest(t, "oneshot.ts", src)
	agents := analysis.DiscoverTSVercelAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 0 {
		t.Errorf("a completion with no tools must not be an agent; got %d (%+v)", len(agents), agents)
	}
}

// Class-based agent: new Experimental_Agent({...}) imported `as Agent`
// normalizes to Class ToolLoopAgent, and `instructions` is its system slot.
func TestTSVercel_ClassAgentAliased(t *testing.T) {
	src := `import { Experimental_Agent as Agent } from "ai";
import { openai } from "@ai-sdk/openai";
const a = new Agent({
  model: openai("gpt-5"),
  instructions: "You help with billing.",
  tools: { ci: openai.tools.codeInterpreter() },
});
`
	pf := parseTSForTest(t, "classagent.ts", src)
	agents := analysis.DiscoverTSVercelAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d (%+v)", len(agents), agents)
	}
	a := agents[0]
	if a.Class != "ToolLoopAgent" {
		t.Errorf("Class: got %q, want ToolLoopAgent", a.Class)
	}
	if !vaiHasHostedClass(a, "openai.tools.codeInterpreter") {
		t.Errorf("expected HostedToolRef openai.tools.codeInterpreter; got %+v", a.HostedToolRefs)
	}
	if a.Kwargs == nil || a.Kwargs.Children["instructions"] == nil {
		t.Errorf("expected `instructions` captured in Kwargs; got %+v", a.Kwargs)
	}
}

// An inline tool({...}) value in the tools record cannot be wired to a symbol
// edge — the agent is marked Opaque (matching ts_openai_agents' inline-tool
// handling) so "agent has no tools" rules don't false-fire.
func TestTSVercel_InlineToolMarksOpaque(t *testing.T) {
	src := `import { generateText, tool } from "ai";
import { openai } from "@ai-sdk/openai";
import { z } from "zod";
const r = await generateText({
  model: openai("gpt-5"),
  tools: { weather: tool({ description: "w", inputSchema: z.object({ c: z.string() }), execute: async () => "" }) },
});
`
	pf := parseTSForTest(t, "inline.ts", src)
	agents := analysis.DiscoverTSVercelAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if !agents[0].Opaque {
		t.Errorf("an inline tool({...}) record value should mark the agent Opaque; got %+v", agents[0])
	}
}

// A spread in the tools record (...mcpTools) is unenumerable — mark Opaque.
func TestTSVercel_SpreadToolsMarksOpaque(t *testing.T) {
	src := `import { streamText } from "ai";
import { openai } from "@ai-sdk/openai";
const r = streamText({ model: openai("gpt-5"), tools: { ...mcpTools, weather: weatherTool } });
`
	pf := parseTSForTest(t, "spread.ts", src)
	agents := analysis.DiscoverTSVercelAgents([]analysis.ParsedFile{pf}, nil)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if !agents[0].Opaque {
		t.Errorf("a spread tools value should mark the agent Opaque; got %+v", agents[0])
	}
}

// The named ToolRef must resolve end-to-end through ResolveEdges to a Vercel
// ToolDef whose VarName matches (Vercel ToolDefs have empty Name; toolsByFileSym
// keys by both Name and VarName).
func TestTSVercel_ResolveEdgesWiresNamedTool(t *testing.T) {
	src := `import { generateText, tool } from "ai";
import { anthropic } from "@ai-sdk/anthropic";
import { z } from "zod";
const weatherTool = tool({ description: "w", inputSchema: z.object({ c: z.string() }), execute: async () => "" });
const r = await generateText({ model: anthropic("claude-sonnet-4"), tools: { weather: weatherTool, bash: anthropic.tools.bash_20250124() } });
`
	pf := parseTSForTest(t, "wire.ts", src)
	tools := analysis.DiscoverTSVercelTools([]analysis.ParsedFile{pf}, nil)
	agents := analysis.DiscoverTSVercelAgents([]analysis.ParsedFile{pf}, nil)
	inv := models.RepoInventory{Tools: tools, Agents: agents}
	analysis.ResolveEdges(&inv, []analysis.ParsedFile{pf})
	if len(inv.Agents) != 1 {
		t.Fatalf("expected 1 agent post-resolve, got %d", len(inv.Agents))
	}
	a := inv.Agents[0]
	var resolved bool
	for _, ref := range a.ToolRefs {
		if ref.Name == "weatherTool" && ref.Resolved != nil {
			resolved = true
			if ref.Resolved.Kind != models.KindVercelAITool {
				t.Errorf("resolved tool Kind: got %q, want vercel_ai_tool", ref.Resolved.Kind)
			}
		}
	}
	if !resolved {
		t.Errorf("named tool ref 'weatherTool' did not resolve to a ToolDef; refs=%+v", a.ToolRefs)
	}
	// The provider hosted tool should have been materialized into the inventory.
	var sawHosted bool
	for _, h := range inv.HostedTools {
		if h.Class == "anthropic.tools.bash" && h.SDK == models.SDKVercelAI {
			sawHosted = true
		}
	}
	if !sawHosted {
		t.Errorf("expected anthropic.tools.bash materialized with SDK vercel_ai; hosted=%+v", inv.HostedTools)
	}
}
