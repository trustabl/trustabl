package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverTSOpenAIAgents_BasicNewAgent(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
const r = new Agent({
  name: "researcher",
  instructions: "Do research",
  model: "gpt-4o",
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1: %+v", len(got), got)
	}
	a := got[0]
	if a.Class != "Agent" {
		t.Errorf("Class = %q, want %q", a.Class, "Agent")
	}
	if a.SDK != models.SDKOpenAIAgents {
		t.Errorf("SDK = %q", a.SDK)
	}
	if a.Language != models.LanguageTypeScript {
		t.Errorf("Language = %q", a.Language)
	}
	if a.Name != "researcher" {
		t.Errorf("Name = %q", a.Name)
	}
	if a.VarName != "r" {
		t.Errorf("VarName = %q", a.VarName)
	}
	if a.Opaque {
		t.Errorf("Opaque should be false for inline-object agent")
	}
}

func TestDiscoverTSOpenAIAgents_AgentCreateStatic(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
const r = Agent.create({ name: "x", instructions: "y" });
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].Name != "x" || got[0].VarName != "r" {
		t.Errorf("Agent.create: %+v", got[0])
	}
}

func TestDiscoverTSOpenAIAgents_OpaqueNonObject(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
const r = new Agent(getOptions());
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if !got[0].Opaque {
		t.Errorf("non-object arg should set Opaque=true")
	}
}

func TestDiscoverTSOpenAIAgents_HostedToolPreResolution(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
import { webSearchTool, fileSearchTool } from "@openai/agents-openai";

const r = new Agent({
  name: "x",
  tools: [webSearchTool({ maxResults: 5 }), fileSearchTool()],
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	a := got[0]
	if len(a.HostedToolRefs) != 2 {
		t.Errorf("expected 2 HostedToolRefs, got %d: %+v", len(a.HostedToolRefs), a.HostedToolRefs)
	}
	// HostedTools slice on the AgentDef itself — the inv.HostedTools side is
	// populated by ResolveEdges via DefIndex, but the per-agent refs carry the
	// Class for direct assertion.
	hostedClasses := map[string]bool{}
	for _, h := range a.HostedToolRefs {
		hostedClasses[h.Class] = true
	}
	if !hostedClasses["webSearchTool"] || !hostedClasses["fileSearchTool"] {
		t.Errorf("missing hosted-tool classes: %+v", hostedClasses)
	}
}

func TestDiscoverTSOpenAIAgents_HostedToolAliasRename(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
import { webSearchTool as wst } from "@openai/agents-openai";

const r = new Agent({
  name: "x",
  tools: [wst()],
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 || len(got[0].HostedToolRefs) != 1 {
		t.Fatalf("expected 1 agent with 1 HostedToolRef, got %+v", got)
	}
	if got[0].HostedToolRefs[0].Class != "webSearchTool" {
		t.Errorf("aliased hosted tool should resolve to canonical name, got %q",
			got[0].HostedToolRefs[0].Class)
	}
}

func TestDiscoverTSOpenAIAgents_InlineToolMarksOpaque(t *testing.T) {
	// Regression (TR-158): an inline tool({...}) in tools: cannot be wired to a
	// ToolDef edge by symbol, so the agent must be marked Opaque rather than
	// appearing to own zero tools.
	src := `
import { Agent, tool } from "@openai/agents";
const r = new Agent({
  name: "x",
  tools: [tool({ name: "inline", description: "d", parameters: {}, execute: async () => "" })],
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if !got[0].Opaque {
		t.Error("agent with an inline tool({...}) must be Opaque, got Opaque=false")
	}
}

func TestDiscoverTSOpenAIAgents_ToolHandoffGuardrailMCPRefs(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
const r = new Agent({
  name: "x",
  tools: [computeSum],
  handoffs: [reviewer],
  inputGuardrails: [blockPII],
  outputGuardrails: [sanitize],
  mcpServers: [fsServer],
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	a := got[0]
	if len(a.ToolRefs) != 1 || a.ToolRefs[0].Name != "computeSum" {
		t.Errorf("ToolRefs: %+v", a.ToolRefs)
	}
	if len(a.HandoffRefs) != 1 || a.HandoffRefs[0].Name != "reviewer" {
		t.Errorf("HandoffRefs: %+v", a.HandoffRefs)
	}
	if len(a.InputGuards) != 1 || a.InputGuards[0].Name != "blockPII" {
		t.Errorf("InputGuards: %+v", a.InputGuards)
	}
	if len(a.OutputGuards) != 1 || a.OutputGuards[0].Name != "sanitize" {
		t.Errorf("OutputGuards: %+v", a.OutputGuards)
	}
	if len(a.MCPServerRefs) != 1 || a.MCPServerRefs[0].Class != "fsServer" {
		// at discovery, MCPServerRef.Class holds the identifier text;
		// ResolveEdges will replace it with the canonical class on resolution.
		t.Errorf("MCPServerRefs: %+v", a.MCPServerRefs)
	}
}

func TestDiscoverTSOpenAIAgents_NoImportGate(t *testing.T) {
	src := `
class Agent { constructor(opts) {} }
const r = new Agent({ name: "fake" });
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 0 {
		t.Errorf("no-SDK-import should yield zero, got %+v", got)
	}
}

func TestDiscoverTSOpenAIAgents_OpaqueSpread_StillExtractsName(t *testing.T) {
	src := `
import { Agent } from "@openai/agents";
const r = new Agent({
  ...baseConfig,
  name: "researcher",
  instructions: "Do research",
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSOpenAIAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	a := got[0]
	if !a.Opaque {
		t.Errorf("spread inside opts should set Opaque=true, got false")
	}
	if a.Name != "researcher" {
		t.Errorf("Name should still be extracted (not gated on Opaque), got %q", a.Name)
	}
	if a.Kwargs == nil {
		t.Errorf("Kwargs should still be populated (TSObjectKwargs skips spread but extracts other pairs)")
	}
}
