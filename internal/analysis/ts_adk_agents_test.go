package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverTSADKAgents_LlmAgent(t *testing.T) {
	src := `
import { LlmAgent } from "@google/adk";
const r = new LlmAgent({
  name: "researcher",
  model: "gemini-2.0-flash",
  instruction: "Research thoroughly",
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1: %+v", len(got), got)
	}
	a := got[0]
	if a.Class != "LlmAgent" {
		t.Errorf("Class = %q, want %q", a.Class, "LlmAgent")
	}
	if a.SDK != models.SDKGoogleADK {
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

func TestDiscoverTSADKAgents_InlineToolMarksOpaque(t *testing.T) {
	// Regression (TR-158): an inline new FunctionTool({...}) in tools: cannot be
	// wired to a ToolDef edge by symbol, so the agent must be marked Opaque.
	src := `
import { LlmAgent, FunctionTool } from "@google/adk";
const r = new LlmAgent({
  name: "x",
  tools: [new FunctionTool({ name: "inline", description: "d", parameters: {}, execute: async () => "" })],
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if !got[0].Opaque {
		t.Error("agent with an inline new FunctionTool({...}) must be Opaque")
	}
}

func TestDiscoverTSADKAgents_AllFiveAgentClasses(t *testing.T) {
	src := `
import { LlmAgent, SequentialAgent, ParallelAgent, LoopAgent, RoutedAgent }
  from "@google/adk";

const a1 = new LlmAgent({ name: "a1" });
const a2 = new SequentialAgent({ name: "a2" });
const a3 = new ParallelAgent({ name: "a3" });
const a4 = new LoopAgent({ name: "a4" });
const a5 = new RoutedAgent({ name: "a5" });
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 5 {
		t.Fatalf("got %d agents, want 5", len(got))
	}
	classes := map[string]bool{}
	for _, a := range got {
		classes[a.Class] = true
	}
	for _, want := range []string{"LlmAgent", "SequentialAgent", "ParallelAgent", "LoopAgent", "RoutedAgent"} {
		if !classes[want] {
			t.Errorf("missing agent class %q in discovered set: %+v", want, classes)
		}
	}
}

func TestDiscoverTSADKAgents_OpaqueNonObject(t *testing.T) {
	src := `
import { LlmAgent } from "@google/adk";
const r = new LlmAgent(getConfig());
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	a := got[0]
	if !a.Opaque {
		t.Errorf("non-object arg should set Opaque=true")
	}
	// Contract: opaque-early-return must not populate any refs. Guards
	// against a future refactor that accidentally calls populateTSADKToolRefs
	// or populateTSADKIdentifierList before the type check.
	if len(a.ToolRefs) != 0 {
		t.Errorf("opaque non-object agent should have no ToolRefs, got %+v", a.ToolRefs)
	}
	if len(a.HostedToolRefs) != 0 {
		t.Errorf("opaque non-object agent should have no HostedToolRefs, got %+v", a.HostedToolRefs)
	}
	if len(a.HandoffRefs) != 0 {
		t.Errorf("opaque non-object agent should have no HandoffRefs, got %+v", a.HandoffRefs)
	}
}

func TestDiscoverTSADKAgents_OpaqueSpread_StillExtractsName(t *testing.T) {
	src := `
import { LlmAgent } from "@google/adk";
const r = new LlmAgent({
  ...baseConfig,
  name: "researcher",
  instruction: "Research",
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	a := got[0]
	if !a.Opaque {
		t.Errorf("spread inside opts should set Opaque=true")
	}
	if a.Name != "researcher" {
		t.Errorf("Name should still be extracted, got %q", a.Name)
	}
	if a.Kwargs == nil {
		t.Errorf("Kwargs should still be populated")
	}
}

func TestDiscoverTSADKAgents_HostedToolPreResolution(t *testing.T) {
	src := `
import { LlmAgent, GoogleSearchTool, AgentTool } from "@google/adk";

const r = new LlmAgent({
  name: "x",
  tools: [new GoogleSearchTool(), new AgentTool({ agent: helper })],
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	a := got[0]
	if len(a.HostedToolRefs) != 2 {
		t.Errorf("expected 2 HostedToolRefs, got %d: %+v", len(a.HostedToolRefs), a.HostedToolRefs)
	}
	hostedClasses := map[string]bool{}
	for _, h := range a.HostedToolRefs {
		hostedClasses[h.Class] = true
	}
	if !hostedClasses["GoogleSearchTool"] || !hostedClasses["AgentTool"] {
		t.Errorf("missing hosted-tool classes: %+v", hostedClasses)
	}
}

func TestDiscoverTSADKAgents_HostedToolAliasRename(t *testing.T) {
	src := `
import { LlmAgent, GoogleSearchTool as gst } from "@google/adk";
const r = new LlmAgent({
  name: "x",
  tools: [new gst()],
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 || len(got[0].HostedToolRefs) != 1 {
		t.Fatalf("expected 1 agent with 1 HostedToolRef, got %+v", got)
	}
	if got[0].HostedToolRefs[0].Class != "GoogleSearchTool" {
		t.Errorf("aliased hosted tool should resolve to canonical name, got %q",
			got[0].HostedToolRefs[0].Class)
	}
}

func TestDiscoverTSADKAgents_ToolAndSubAgentRefs(t *testing.T) {
	src := `
import { LlmAgent } from "@google/adk";
const r = new LlmAgent({
  name: "x",
  tools: [computeSum],
  subAgents: [writer],
});
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	a := got[0]
	if len(a.ToolRefs) != 1 || a.ToolRefs[0].Name != "computeSum" {
		t.Errorf("ToolRefs: %+v", a.ToolRefs)
	}
	if len(a.HandoffRefs) != 1 || a.HandoffRefs[0].Name != "writer" {
		t.Errorf("HandoffRefs (from subAgents): %+v", a.HandoffRefs)
	}
}

func TestDiscoverTSADKAgents_NoImportGate(t *testing.T) {
	src := `
class LlmAgent { constructor(opts) {} }
const r = new LlmAgent({ name: "fake" });
`
	pf := parseTSForTest(t, "src/a.ts", src)
	got := analysis.DiscoverTSADKAgents([]analysis.ParsedFile{pf}, nil)
	if len(got) != 0 {
		t.Errorf("no-SDK-import should yield zero, got %+v", got)
	}
}
