package scanner_test

import (
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
