package scanner_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
// testdata/corpus/ and asserts the scanner completes without error.
//
// This is NOT a correctness test — it does NOT assert specific findings,
// because the corpus repos are real-world agents (or close to it) and shouldn't
// reliably trigger every rule. The point is regression coverage: if
// discovery starts panicking on weird code shapes, this catches it before
// the scanner ships broken to a real user.
//
// Per-rule fire/silent correctness lives in
// internal/rules/policies_test.go, which uses focused snippets.
func TestScanExamples_NoCrash(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	corpusDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "corpus")

	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Skipf("no testdata/corpus/ dir at %s: %v", corpusDir, err)
	}

	scanned := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip entries that are not agent repos: large datasets that would
		// slow the test pointlessly (ToolBench) and the shared license-text
		// directory (LICENSES). Add to this list as needed.
		switch e.Name() {
		case "ToolBench", "LICENSES":
			continue
		}

		target := filepath.Join(corpusDir, e.Name())
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
		t.Skip("testdata/corpus/ has no scannable subdirectories")
	}
}

// recordingReporter captures the phase keys a scan starts, so tests can assert
// which phases fired.
type recordingReporter struct{ phases []string }

func (r *recordingReporter) StartPhase(key, _ string) { r.phases = append(r.phases, key) }
func (r *recordingReporter) SetTotal(int)             {}
func (r *recordingReporter) Advance(string)           {}
func (r *recordingReporter) SetDetail(string)         {}
func (r *recordingReporter) ResetPhase()              {}
func (r *recordingReporter) EndPhase(string)          {}
func (r *recordingReporter) Fatal(error)              {}

// A local target resolves instantly (no clone), so the scan must NOT emit a
// "clone" phase — otherwise local scans would show a spurious "Cloning" line.
func TestScanRun_LocalTargetEmitsNoClonePhase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.py"), []byte("x = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rep := &recordingReporter{}
	if _, err := scanner.Run(scanner.Config{Target: dir, RulesFS: rulesFixture(t), Progress: rep}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, p := range rep.phases {
		if p == "clone" {
			t.Errorf("local scan emitted a clone phase; phases=%v", rep.phases)
		}
	}
	// Sanity: the normal phases still fire.
	if len(rep.phases) == 0 {
		t.Error("expected phases to be reported")
	}
}

func TestScan_GoogleADKDemoFiresExpectedRules(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "corpus", "google-adk-demo")
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

	// End-to-end regression guard for the multi-scope score: this repo fires
	// agent/repo-scoped rules, so the overall score must reflect them and read
	// below 100%. This is the user-visible bug the surface scoring fixed — a repo
	// with real non-tool findings used to report 100%.
	if res.OverallScore >= 1.0 {
		t.Errorf("OverallScore = %v; want < 1.0 (non-tool rules fired: %v)", res.OverallScore, fired)
	}

	// projected_scores must be populated by the scanner, monotonic (resolving
	// more findings never lowers the score), and every tier must sit at or above
	// the unprojected overall. fix_all resolves everything, so it is the ceiling.
	p := res.ProjectedScores
	if !(p.FixCritical >= res.OverallScore && p.FixHigh >= p.FixCritical &&
		p.FixMedium >= p.FixHigh && p.FixLow >= p.FixMedium && p.FixAll >= p.FixLow) {
		t.Errorf("projected_scores not monotonic/above overall: overall=%v projected=%+v", res.OverallScore, p)
	}
	if p.FixAll < res.OverallScore {
		t.Errorf("projected_scores.fix_all (%v) must be >= overall (%v)", p.FixAll, res.OverallScore)
	}
}

func TestScan_SurfacesNewInventoryFields(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "corpus", "financial_research_agent")
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
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "corpus", "ts-claude-sdk-min")
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
	// CSDK-010/011/012 now ship as TS-language Claude SDK rules, so META-004
	// ("SDK detected but no rule applicable") should NOT fire for this corpus.
	for _, f := range res.Findings {
		if f.RuleID == "META-004" {
			t.Errorf("unexpected META-004 finding: TS Claude SDK rules (CSDK-010/011/012) now ship, so META-004 should not fire; finding: %+v", f)
		}
	}
}

// TestScanExamples_OpenAIAgentsJS_DiscoveryCounts asserts the full inventory
// shape produced by scanning the vendored openai-agents-js examples corpus.
// This is the integration counterpart to the per-discovery unit tests in
// internal/analysis/ts_openai_*_test.go — those test each discoverer in
// isolation; this exercises the whole pipeline end-to-end (parse → discover
// → resolve edges → score) and would have caught the T11 double-emit bug
// and the T-followup Opaque-spread bug at integration time rather than via
// smoke check.
func TestScanExamples_OpenAIAgentsJS_DiscoveryCounts(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "corpus", "openai-agents-js-examples")
	res, err := scanner.Run(scanner.Config{Target: target, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// SDK detection: openai_agents must be observed in code.
	var sawOpenAI bool
	for _, s := range res.SDKs {
		if s == "openai_agents" {
			sawOpenAI = true
		}
	}
	if !sawOpenAI {
		t.Errorf("expected openai_agents in SDKs, got %v", res.SDKs)
	}

	// Inventory counts — exact, so a regression in any of the 6 discoverers
	// (tools, agents, hosted tools, MCP servers, plus the guardrails/sessions
	// that don't appear in ScanResult JSON but are exercised at runtime) is
	// caught here.
	if got, want := len(res.Tools), 1; got != want {
		t.Errorf("Tools: got %d, want %d (computeSum from basic.ts)", got, want)
	}
	if got, want := len(res.Agents), 4; got != want {
		t.Errorf("Agents: got %d, want %d (calculator + researcher + integrated + safe)", got, want)
	}
	if got, want := len(res.HostedTools), 3; got != want {
		t.Errorf("HostedTools: got %d, want %d (webSearchTool + fileSearchTool + shellTool)", got, want)
	}
	if got, want := len(res.MCPServers), 2; got != want {
		t.Errorf("MCPServers: got %d, want %d (MCPServerStdio + MCPServerSSE)", got, want)
	}

	// Regression guard for the T11 double-emit bug: the researcher agent's
	// tools=[webSearchTool(...), fileSearchTool(), shellTool()] must produce
	// HostedToolRefs only — NO External ToolRefs with the raw call text as
	// Name. (Pre-fix the count was 3 External ToolRefs per call_expression
	// item; post-fix it's 0.)
	for _, a := range res.Agents {
		if a.Name != "researcher" {
			continue
		}
		if len(a.HostedToolRefs) != 3 {
			t.Errorf("researcher: expected 3 HostedToolRefs, got %d: %+v", len(a.HostedToolRefs), a.HostedToolRefs)
		}
		for _, ref := range a.ToolRefs {
			if ref.External {
				t.Errorf("researcher: ToolRef %q should not be External (hosted-tool call was double-emitted)", ref.Name)
			}
		}
	}

	// Verify the new var_name → MCP class resolution path. The integrated
	// agent declares `mcpServers: [fs, events]` where `fs` and `events` are
	// const-bound MCPServerStdio/SSE constructions. After ResolveEdges, the
	// agent's MCPServerRefs should be resolved (Class = canonical class
	// name, not the identifier text, and Resolved is non-nil).
	for _, a := range res.Agents {
		if a.Name != "integrated" {
			continue
		}
		if len(a.MCPServerRefs) != 2 {
			t.Errorf("integrated: expected 2 MCPServerRefs, got %d", len(a.MCPServerRefs))
		}
		for _, ref := range a.MCPServerRefs {
			if ref.External {
				t.Errorf("integrated: MCPServerRef should resolve by VarName, got External (class=%q)", ref.Class)
			}
			if ref.Class != "MCPServerStdio" && ref.Class != "MCPServerSSE" {
				t.Errorf("integrated: MCPServerRef.Class should be canonical class name, got %q", ref.Class)
			}
		}
	}

	// META-004 must NOT fire here anymore: TypeScript OpenAI rules now ship
	// (e.g. OAI-016/017/019, `language: typescript`), so at least one loaded
	// detector's Applies() returns true for the TS tools/agents in this repo —
	// the openai_agents pack IS applicable, even though this clean example
	// happens to trigger none of those rules. (Before TS rules existed, META-004
	// fired because only python OAI rules were loaded and none applied to TS.)
	// OAI-201 (`language: python`) must still NOT fire on a TS-only repo.
	var sawMETA004, sawOAI201 bool
	for _, f := range res.Findings {
		switch f.RuleID {
		case "META-004":
			sawMETA004 = true
		case "OAI-201":
			sawOAI201 = true
		}
	}
	if sawMETA004 {
		t.Errorf("META-004 must NOT fire now that TS OpenAI rules ship (pack is applicable to TS entities); got findings=%v", res.Findings)
	}
	if sawOAI201 {
		t.Errorf("OAI-201 declares language: python and must NOT fire on TS-only repo (regression in repoRuleDetector language gate); got findings=%v", res.Findings)
	}
}

func TestScanExamples_EmailAgent_SubagentDiscoveredAndAudited(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "corpus", "email-agent")
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
//	agents[].start_line, agents[].end_line, agents[].file_path
//	hosted_tools[].start_line, hosted_tools[].end_line
//	mcp_servers[].start_line, mcp_servers[].end_line
//	subagents[].start_line, subagents[].end_line
//	claude_settings[].start_line, claude_settings[].end_line
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

	// agents[].start_line, agents[].end_line, agents[].file_path
	agents := mustList(generic["agents"], "agents")
	a0 := mustObject(agents[0], "agents[0]")
	mustHaveKey(a0, "start_line", "agents[0]")
	mustHaveKey(a0, "end_line", "agents[0]")
	mustHaveKey(a0, "file_path", "agents[0]")

	// hosted_tools[].start_line, hosted_tools[].end_line
	if ht, ok := generic["hosted_tools"]; ok {
		hosted := mustList(ht, "hosted_tools")
		h0 := mustObject(hosted[0], "hosted_tools[0]")
		mustHaveKey(h0, "start_line", "hosted_tools[0]")
		mustHaveKey(h0, "end_line", "hosted_tools[0]")
	} else {
		t.Errorf("hosted_tools missing from JSON (fixture has WebSearchTool)")
	}

	// mcp_servers[].start_line, mcp_servers[].end_line
	if ms, ok := generic["mcp_servers"]; ok {
		mcps := mustList(ms, "mcp_servers")
		m0 := mustObject(mcps[0], "mcp_servers[0]")
		mustHaveKey(m0, "start_line", "mcp_servers[0]")
		mustHaveKey(m0, "end_line", "mcp_servers[0]")
	} else {
		t.Errorf("mcp_servers missing from JSON (fixture has MCPServerStdio)")
	}

	// subagents[].start_line, subagents[].end_line
	subs := mustList(generic["subagents"], "subagents")
	s0 := mustObject(subs[0], "subagents[0]")
	mustHaveKey(s0, "start_line", "subagents[0]")
	mustHaveKey(s0, "end_line", "subagents[0]")

	// claude_settings[].start_line, claude_settings[].end_line,
	// claude_settings[].permissions.allow[].line
	cs := mustList(generic["claude_settings"], "claude_settings")
	c0 := mustObject(cs[0], "claude_settings[0]")
	mustHaveKey(c0, "start_line", "claude_settings[0]")
	mustHaveKey(c0, "end_line", "claude_settings[0]")
	perms := mustObject(c0["permissions"], "claude_settings[0].permissions")
	allow := mustList(perms["allow"], "claude_settings[0].permissions.allow")
	rule0 := mustObject(allow[0], "claude_settings[0].permissions.allow[0]")
	mustHaveKey(rule0, "line", "claude_settings[0].permissions.allow[0]")
}

// TestScanExamples_ADKJS_DiscoveryCounts asserts the full inventory shape
// produced by scanning the vendored adk-js examples corpus. End-to-end
// integration: parse → discover → resolve edges → score. Catches any
// regression in the cross-cutting ResolveEdges extensions (subAgents
// language branch, HostedTool ADK class recognition with correct SDK).
func TestScanExamples_ADKJS_DiscoveryCounts(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	target := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "corpus", "adk-js-examples")
	res, err := scanner.Run(scanner.Config{Target: target, RulesFS: rulesFixture(t)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// SDK detection: google_adk must be observed in code (via discovered
	// agents). dep-needle alone is NOT what feeds SDKsDetected — it's the
	// agent SDK fields populated by discovery.
	var sawADK bool
	for _, s := range res.SDKs {
		if s == "google_adk" {
			sawADK = true
		}
	}
	if !sawADK {
		t.Errorf("expected google_adk in SDKs, got %v", res.SDKs)
	}

	// Exact inventory counts from the fixture: basic.ts (1 agent, 1 tool,
	// 1 hosted tool) + multi_agent.ts (3 agents, 1 hosted tool).
	if got, want := len(res.Tools), 1; got != want {
		t.Errorf("Tools: got %d, want %d (summarize from basic.ts)", got, want)
	}
	if got, want := len(res.Agents), 4; got != want {
		t.Errorf("Agents: got %d, want %d (researcher + drafter + reviewer + pipeline)", got, want)
	}
	if got, want := len(res.HostedTools), 2; got != want {
		t.Errorf("HostedTools: got %d, want %d (GoogleSearchTool + AgentTool)", got, want)
	}

	// Hosted tools must carry SDK=google_adk (regression guard: the
	// extended HostedTool block stamps the right SDK from the class set).
	for _, h := range res.HostedTools {
		if h.SDK != "google_adk" {
			t.Errorf("HostedTool %q: SDK = %q, want google_adk (regression in ADK class → SDK stamping)",
				h.Class, h.SDK)
		}
	}

	// subAgents edge resolution (regression guard: the camelCase branch in
	// the sub_agents block). The `pipeline` SequentialAgent has subAgents:
	// [drafter, reviewer] — both must resolve, neither External.
	for _, a := range res.Agents {
		if a.Name != "pipeline" {
			continue
		}
		if len(a.HandoffRefs) != 2 {
			t.Errorf("pipeline: expected 2 HandoffRefs from subAgents, got %d", len(a.HandoffRefs))
		}
		for _, ref := range a.HandoffRefs {
			if ref.External {
				t.Errorf("pipeline HandoffRef %q: should resolve to same-file agent, got External",
					ref.Name)
			}
		}
	}

	// META-004 must NOT fire: the TS ADK rule pack now ships agent-scope rules
	// (ADK-109, language: typescript) whose Applies() returns true for the TS
	// LlmAgents discovered here, so google_adk is an "applicable" category and
	// the coverage-gap signal is correctly suppressed. ADK-109 itself fires on
	// the corpus agents that declare no description — the end-to-end proof that
	// the TS ADK rules run against real discovered code, not just hand-built
	// test inputs.
	var sawMETA004 bool
	var firedADKRules []string
	for _, f := range res.Findings {
		switch {
		case f.RuleID == "META-004":
			sawMETA004 = true
		case strings.HasPrefix(f.RuleID, "ADK-"):
			firedADKRules = append(firedADKRules, f.RuleID)
		}
	}
	if sawMETA004 {
		t.Errorf("META-004 should NOT fire now that TS ADK rules apply to discovered TS agents; got findings=%v", res.Findings)
	}
	if len(firedADKRules) == 0 {
		t.Errorf("expected ADK-109 to fire on the description-less TS LlmAgents in this corpus; got findings=%v", res.Findings)
	}
}
