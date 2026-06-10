package acac

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func emitFixture(t *testing.T) []byte {
	t.Helper()
	result, agent := buildFixtureResult()
	m := Build(result, agent, BuildOptions{EngineVersion: "test", IncludeOWASP: true})
	out, err := Emit(m)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return out
}

func TestEmitIsDeterministic(t *testing.T) {
	first := emitFixture(t)
	for i := 0; i < 5; i++ {
		if next := emitFixture(t); !bytes.Equal(first, next) {
			t.Fatalf("emit run %d differs", i+2)
		}
	}
	if bytes.Contains(first, []byte("\r")) {
		t.Fatal("emitted manifest contains CR; must be LF-only")
	}
}

func TestEmitStructureAndMarkers(t *testing.T) {
	out := string(emitFixture(t))

	for _, want := range []string{
		"schema_version: 1.0.0",
		"version: 0.1.0 # " + MarkerNeedsHuman,
		"# " + MarkerNeedsHuman + " — the engine cannot derive the agent's I/O contract;",
		"approval: true # " + MarkerSuggested,
		"# " + MarkerReview + " — tool reference did not resolve inside this repo",
		"# " + MarkerReview + " — handoff target is defined in source code, not a manifest",
		"server_ref:", // no MCP servers in fixture? keep below
		"deployment_readiness: needs_work",
		"reliability_score: 61 # informational in v0.x",
		"owasp: [ASI02, ASI05]",
		"hosted_tools: [WebSearchTool]",
		"sdks_detected: [openai_agents",
	} {
		if want == "server_ref:" {
			continue // fixture has no MCP servers; covered by golden tests later
		}
		if !strings.Contains(out, want) {
			t.Errorf("emitted manifest missing %q\n---\n%s", want, out)
		}
	}

	// generated_at must be absent by default.
	if strings.Contains(out, "generated_at") {
		t.Error("generated_at emitted without --timestamp")
	}

	// The document must round-trip as YAML.
	var v any
	if err := yaml.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("emitted manifest is not valid YAML: %v", err)
	}
}

func TestEmitTimestampAddsExactlyOneLine(t *testing.T) {
	result, agent := buildFixtureResult()
	base, err := Emit(Build(result, agent, BuildOptions{EngineVersion: "test", IncludeOWASP: true}))
	if err != nil {
		t.Fatal(err)
	}
	stamped, err := Emit(Build(result, agent, BuildOptions{
		EngineVersion: "test", IncludeOWASP: true, GeneratedAt: "2026-06-10T12:00:00Z",
	}))
	if err != nil {
		t.Fatal(err)
	}
	baseLines := strings.Split(string(base), "\n")
	stampedLines := strings.Split(string(stamped), "\n")
	if len(stampedLines) != len(baseLines)+1 {
		t.Fatalf("--timestamp added %d lines, want exactly 1", len(stampedLines)-len(baseLines))
	}
	found := false
	for _, l := range stampedLines {
		if strings.Contains(l, "generated_at:") && strings.Contains(l, "2026-06-10T12:00:00Z") {
			found = true
		}
	}
	if !found {
		t.Error("generated_at line missing from stamped output")
	}
}
