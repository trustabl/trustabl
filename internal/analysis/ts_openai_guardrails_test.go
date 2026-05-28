package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverTSOpenAIGuardrails_AllFourKinds(t *testing.T) {
	src := `
import {
  defineInputGuardrail, defineOutputGuardrail,
  defineToolInputGuardrail, defineToolOutputGuardrail,
} from "@openai/agents";

const blockPII = defineInputGuardrail({
  name: "block_pii",
  execute: async () => ({ tripwireTriggered: false }),
});
const sanitize = defineOutputGuardrail({
  name: "sanitize",
  execute: async () => ({ tripwireTriggered: false }),
});
const toolIn = defineToolInputGuardrail({
  name: "tool_in",
  execute: async () => ({ tripwireTriggered: false }),
});
const toolOut = defineToolOutputGuardrail({
  name: "tool_out",
  execute: async () => ({ tripwireTriggered: false }),
});
`
	pf := parseTSForTest(t, "src/g.ts", src)
	got := analysis.DiscoverTSOpenAIGuardrails([]analysis.ParsedFile{pf}, nil)
	if len(got) != 4 {
		t.Fatalf("got %d guardrails, want 4: %+v", len(got), got)
	}
	byKind := map[models.GuardrailKind]models.GuardrailDef{}
	for _, g := range got {
		byKind[g.Kind] = g
	}
	cases := []struct {
		kind    models.GuardrailKind
		varName string
		name    string
	}{
		{models.GuardrailInput, "blockPII", "block_pii"},
		{models.GuardrailOutput, "sanitize", "sanitize"},
		{models.GuardrailToolInput, "toolIn", "tool_in"},
		{models.GuardrailToolOutput, "toolOut", "tool_out"},
	}
	for _, c := range cases {
		g, ok := byKind[c.kind]
		if !ok {
			t.Errorf("missing kind %q", c.kind)
			continue
		}
		if g.VarName != c.varName {
			t.Errorf("kind=%q VarName = %q, want %q", c.kind, g.VarName, c.varName)
		}
		if g.Name != c.name {
			t.Errorf("kind=%q Name = %q, want %q", c.kind, g.Name, c.name)
		}
	}
}

func TestDiscoverTSOpenAIGuardrails_NoImportGate(t *testing.T) {
	src := `
const defineInputGuardrail = (opts) => opts;
const x = defineInputGuardrail({ name: "fake" });
`
	pf := parseTSForTest(t, "src/g.ts", src)
	got := analysis.DiscoverTSOpenAIGuardrails([]analysis.ParsedFile{pf}, nil)
	if len(got) != 0 {
		t.Errorf("no-SDK-import should yield zero, got %+v", got)
	}
}

func TestDiscoverTSOpenAIGuardrails_NoBinding_StillEmits(t *testing.T) {
	src := `
import { defineInputGuardrail } from "@openai/agents";
defineInputGuardrail({ name: "orphan", execute: async () => ({ tripwireTriggered: false }) });
`
	pf := parseTSForTest(t, "src/g.ts", src)
	got := analysis.DiscoverTSOpenAIGuardrails([]analysis.ParsedFile{pf}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].VarName != "" {
		t.Errorf("no-binding case should have empty VarName, got %q", got[0].VarName)
	}
	if got[0].Name != "orphan" {
		t.Errorf("Name = %q", got[0].Name)
	}
}
