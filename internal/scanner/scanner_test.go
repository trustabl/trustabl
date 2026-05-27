package scanner_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

func TestScanRun_DiscoversTSClaudeSDK(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("package.json", `{"dependencies": {"@anthropic-ai/claude-agent-sdk": "^1.0.0"}}`)
	mustWrite("src/agent.ts", `
import { tool, query, createSdkMcpServer, AgentDefinition } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";

export const searchTool = tool("search", "Search", { q: z.string() }, async () => ({ content: [] }));
export const srv = createSdkMcpServer({ name: "x" });
export const reviewer: AgentDefinition = { description: "r", prompt: "p" };
export const q = query({ options: { agents: { analyst: { description: "a", prompt: "p" } } } });
`)
	cfg := scanner.Config{
		Target:  dir,
		RulesFS: rulesFixture(t),
	}
	res, err := scanner.Run(cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Errorf("Tools empty")
	}
	if len(res.Agents) < 2 {
		t.Errorf("want >= 2 agents (analyst + reviewer), got %d", len(res.Agents))
	}
	if len(res.MCPServers) == 0 {
		t.Errorf("MCPServers empty")
	}
	var sawClaudeSDK bool
	for _, s := range res.SDKs {
		if s == models.SDKClaudeAgentSDK {
			sawClaudeSDK = true
		}
	}
	if !sawClaudeSDK {
		t.Errorf("SDKsDetected missing claude_agent_sdk: %+v", res.SDKs)
	}
}

// rulesFixture returns the Phase-1 interim rule packs for tests.
func rulesFixture(t *testing.T) fs.FS {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "rules-fixture")
	return os.DirFS(root)
}

// TestScanExamples_NoCrash sweeps every immediate subdirectory under
// examples/ and asserts the scanner completes without error.
//
// This is NOT a correctness test — it does NOT assert specific findings,
// because the examples are real-world agents (or close to it) and shouldn't
// reliably trigger every rule. The point is regression coverage: if
// discovery starts panicking on weird code shapes, this catches it before
// the scanner ships broken to a real user.
//
// Per-rule fire/silent correctness lives in
// internal/rules/policies_test.go, which uses focused snippets.
func TestScanExamples_NoCrash(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	examplesDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "examples")

	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Skipf("no examples/ dir at %s: %v", examplesDir, err)
	}

	scanned := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip very large dataset directories that are not real agents and
		// would slow the test pointlessly. Add to this list as needed.
		switch e.Name() {
		case "ToolBench":
			continue
		}

		target := filepath.Join(examplesDir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			result, err := scanner.Run(scanner.Config{Target: target, RulesFS: rulesFixture(t)})
			if err != nil {
				t.Fatalf("scan %s: %v", e.Name(), err)
			}
			// Manifest must always be populated — even an empty repo gets
			// a RepoRoot. A nil manifest is a sign of a wiring break.
			if result.Manifest.RepoRoot == "" {
				t.Errorf("scan %s: empty RepoRoot in manifest", e.Name())
			}
		})
		scanned++
	}
	if scanned == 0 {
		t.Skip("examples/ has no scannable subdirectories")
	}
}

func TestScan_GoogleADKDemoFiresExpectedRules(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "examples", "google-adk-demo")
	res, err := scanner.Run(scanner.Config{Target: target, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	var sawADK bool
	for _, s := range res.SDKs {
		if s == models.SDKGoogleADK {
			sawADK = true
		}
	}
	if !sawADK {
		t.Errorf("expected google_adk in ScanResult.SDKs, got %+v", res.SDKs)
	}

	fired := map[string]bool{}
	for _, f := range res.Findings {
		fired[f.RuleID] = true
	}
	for _, want := range []string{"ADK-001", "ADK-101", "ADK-102"} {
		if !fired[want] {
			t.Errorf("expected rule %s to fire on google-adk-demo; fired set: %v", want, fired)
		}
	}
}

func TestScan_SurfacesNewInventoryFields(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "examples", "financial_research_agent")
	res, err := scanner.Run(scanner.Config{Target: target, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	foundWebSearch := false
	for _, h := range res.HostedTools {
		if h.Class == "WebSearchTool" {
			foundWebSearch = true
		}
	}
	if !foundWebSearch {
		t.Errorf("expected WebSearchTool in ScanResult.HostedTools, got %+v", res.HostedTools)
	}
}

func TestScanExamples_TSClaudeSDKMin_DiscoveryCounts(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "examples", "ts-claude-sdk-min")
	res, err := scanner.Run(scanner.Config{Target: target, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Errorf("expected at least one TS tool, got 0")
	}
	if len(res.Agents) < 2 {
		t.Errorf("expected at least 2 agents (analyst + reviewer), got %d", len(res.Agents))
	}
	if len(res.MCPServers) < 2 {
		t.Errorf("expected at least 2 MCP servers (createSdkMcpServer + stdio config), got %d", len(res.MCPServers))
	}
	var meta004 int
	for _, f := range res.Findings {
		if f.RuleID == "META-004" {
			meta004++
		}
	}
	if meta004 == 0 {
		t.Errorf("expected META-004 (SDK detected but no rule applicable) since no TS-language rules ship yet; got 0 findings with that rule")
	}
}

func TestScanExamples_EmailAgent_SubagentDiscoveredAndAudited(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "examples", "email-agent")
	res, err := scanner.Run(scanner.Config{Target: target, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var sawInboxSearcher bool
	for _, s := range res.Subagents {
		if s.Name == "inbox-searcher" {
			sawInboxSearcher = true
		}
	}
	if !sawInboxSearcher {
		t.Errorf("expected inbox-searcher in Subagents, got %+v", res.Subagents)
	}
	var sawCSDK110 bool
	var ruleIDs []string
	for _, f := range res.Findings {
		ruleIDs = append(ruleIDs, f.RuleID)
		if f.RuleID == "CSDK-110" {
			sawCSDK110 = true
		}
	}
	if !sawCSDK110 {
		t.Errorf("expected CSDK-110 to fire on inbox-searcher (grants Bash); finding rule IDs: %v", ruleIDs)
	}
}

// TestScanResult_JSONLineRangeFields asserts that every new JSON field path
// added by the inventory line-attribution work is present in --format json
// output. This is a "the JSON shape is what we promised" contract test: it
// calls scanner.Run, marshals/unmarshals to a generic map, then walks the
// expected field paths.
//
// Field paths asserted:
//
//	agents[].line, agents[].end_line, agents[].file_path
//	hosted_tools[].line, hosted_tools[].end_line
//	mcp_servers[].line, mcp_servers[].end_line
//	subagents[].line, subagents[].end_line
//	claude_settings[].line, claude_settings[].end_line
//	claude_settings[].permissions.allow[].line
func TestScanResult_JSONLineRangeFields(t *testing.T) {
	// Build a tiny fixture that exercises every new JSON field path:
	//   - agent.py: Python AgentDef with a WebSearchTool (hosted tool) and
	//               MCPServerStdio — exercises agents, hosted_tools, mcp_servers.
	//   - .claude/agents/helper.md: subagent markdown — exercises subagents.
	//   - .claude/settings.json: permission rules — exercises claude_settings
	//     and permissions.allow[].line.
	dir := t.TempDir()
	writeFile := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeFile("pyproject.toml", "[project]\nname = \"f\"\ndependencies = [\"openai-agents\"]\n")
	writeFile("agent.py", `from agents import Agent, WebSearchTool
from agents.mcp import MCPServerStdio

agent = Agent(
    name="researcher",
    tools=[
        WebSearchTool(
            search_context_size="high",
        ),
    ],
    mcp_servers=[
        MCPServerStdio(
            params={"command": "uvx"},
        ),
    ],
)
`)
	writeFile(".claude/agents/helper.md", `---
name: helper
description: a helper
tools: Read, Bash
---

Body.
`)
	writeFile(".claude/settings.json", `{
  "permissions": {
    "allow": [
      "Bash",
      "Read(./*)"
    ]
  }
}
`)

	cfg := scanner.Config{Target: dir, RulesFS: rulesFixture(t)}
	res, err := scanner.Run(cfg)
	if err != nil {
		t.Fatalf("scanner.Run: %v", err)
	}

	js, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(js, &generic); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// mustHaveKey fails the test when key is absent from m.
	mustHaveKey := func(m map[string]any, key, path string) {
		t.Helper()
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON field %q at %s", key, path)
		}
	}
	// mustList asserts v is a non-empty JSON array and returns it.
	mustList := func(v any, path string) []any {
		t.Helper()
		l, ok := v.([]any)
		if !ok || len(l) == 0 {
			t.Fatalf("expected non-empty list at %s, got %T (%v)", path, v, v)
		}
		return l
	}
	// mustObject asserts v is a JSON object and returns it.
	mustObject := func(v any, path string) map[string]any {
		t.Helper()
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("expected object at %s, got %T (%v)", path, v, v)
		}
		return m
	}

	// agents[].line, agents[].end_line, agents[].file_path
	agents := mustList(generic["agents"], "agents")
	a0 := mustObject(agents[0], "agents[0]")
	mustHaveKey(a0, "line", "agents[0]")
	mustHaveKey(a0, "end_line", "agents[0]")
	mustHaveKey(a0, "file_path", "agents[0]")

	// hosted_tools[].line, hosted_tools[].end_line
	if ht, ok := generic["hosted_tools"]; ok {
		hosted := mustList(ht, "hosted_tools")
		h0 := mustObject(hosted[0], "hosted_tools[0]")
		mustHaveKey(h0, "line", "hosted_tools[0]")
		mustHaveKey(h0, "end_line", "hosted_tools[0]")
	} else {
		t.Errorf("hosted_tools missing from JSON (fixture has WebSearchTool)")
	}

	// mcp_servers[].line, mcp_servers[].end_line
	if ms, ok := generic["mcp_servers"]; ok {
		mcps := mustList(ms, "mcp_servers")
		m0 := mustObject(mcps[0], "mcp_servers[0]")
		mustHaveKey(m0, "line", "mcp_servers[0]")
		mustHaveKey(m0, "end_line", "mcp_servers[0]")
	} else {
		t.Errorf("mcp_servers missing from JSON (fixture has MCPServerStdio)")
	}

	// subagents[].line, subagents[].end_line
	subs := mustList(generic["subagents"], "subagents")
	s0 := mustObject(subs[0], "subagents[0]")
	mustHaveKey(s0, "line", "subagents[0]")
	mustHaveKey(s0, "end_line", "subagents[0]")

	// claude_settings[].line, claude_settings[].end_line,
	// claude_settings[].permissions.allow[].line
	cs := mustList(generic["claude_settings"], "claude_settings")
	c0 := mustObject(cs[0], "claude_settings[0]")
	mustHaveKey(c0, "line", "claude_settings[0]")
	mustHaveKey(c0, "end_line", "claude_settings[0]")
	perms := mustObject(c0["permissions"], "claude_settings[0].permissions")
	allow := mustList(perms["allow"], "claude_settings[0].permissions.allow")
	rule0 := mustObject(allow[0], "claude_settings[0].permissions.allow[0]")
	mustHaveKey(rule0, "line", "claude_settings[0].permissions.allow[0]")
}
