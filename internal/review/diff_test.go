package review_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/review"
)

// TestRender_HostedToolsVisibleInHumanFormat is the regression test for the
// bug where ScanResult.HostedTools (populated by hosted-tool discovery) was
// surfaced in the JSON output but never rendered in the human format. A
// repo whose only tools are hosted (e.g. testdata/corpus/research_bot using
// WebSearchTool) used to render no hosted-tool info despite the JSON
// listing the tool.
func TestRender_HostedToolsVisibleInHumanFormat(t *testing.T) {
	result := models.ScanResult{
		Repo:      "./fixture",
		Languages: []models.Language{models.LanguagePython},
		SDKs:      []models.SDK{models.SDKOpenAIAgents},
		Agents: []models.AgentDef{
			{
				SDK:      models.SDKOpenAIAgents,
				Class:    "Agent",
				Language: models.LanguagePython,
				Location: models.Location{
					FilePath: "agents/search.py",
					Line:     12,
				},
				Name: "search",
				HostedToolRefs: []models.HostedToolRef{
					{Class: "WebSearchTool"},
				},
			},
		},
		HostedTools: []models.HostedToolDef{
			{Class: "WebSearchTool", SDK: models.SDKOpenAIAgents, Location: models.Location{FilePath: "agents/search.py", Line: 12}},
		},
	}

	out := (&review.Renderer{NoColor: true}).Render(result)

	for _, want := range []string{
		"Hosted tools:       1",
		"WebSearchTool",
		"hosted tools:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q\n---\n%s", want, out)
		}
	}
}

// TestRender_HostedToolsCountedInGrantsBucketWhenAttachedToAgent is a
// secondary check on the broken-out tool surface: a hosted-tool ref on an
// agent is counted under "Hosted tools" (the dedicated bucket), and does
// not double-count into Agent grants.
//
// TestRender_ToolSurfaceBrokenOutClearly is the regression test for the
// "16 vs 2 tools" presentation confusion. Previously a single conflated
// "Tools found: 16" line led users to wonder why per-tool readiness only
// listed 2. The renderer now breaks the tool surface into honest categories
// so the difference between "we audit it" (defs) and "we know it's granted"
// (grants/hosted) is obvious.
func TestRender_ToolSurfaceBrokenOutClearly(t *testing.T) {
	result := models.ScanResult{
		Repo: "./fixture",
		Tools: []models.ToolDef{
			{Name: "search_inbox", Kind: models.KindClaudeSDKTool, Language: models.LanguageTypeScript},
			{Name: "read_emails", Kind: models.KindClaudeSDKTool, Language: models.LanguageTypeScript},
		},
		Agents: []models.AgentDef{
			{
				SDK: models.SDKClaudeAgentSDK, Class: "QueryMainAgent",
				Language: models.LanguageTypeScript, Name: "client.run",
				ToolRefs: []models.ToolRef{
					{Name: "Bash"}, {Name: "Read"}, {Name: "Edit"},
					{Name: "mcp__email__search_inbox"},
				},
			},
		},
		// One finding is required for the readiness section to render
		// (the renderer short-circuits with "No findings. Nothing to commit."
		// when result.Findings is empty).
		Findings: []models.Finding{
			{RuleID: "META-001", Severity: models.SeverityInfo, Confidence: 1.0},
		},
		Readiness: []models.ToolReadiness{
			{ToolName: "search_inbox", Score: 1.0},
			{ToolName: "read_emails", Score: 1.0},
		},
	}

	out := (&review.Renderer{NoColor: true}).Render(result)

	// New breakdown: defs always shown; grants only when > 0.
	for _, want := range []string{
		"Tool definitions:   2",
		"Agent tool grants:  4",
		"Per-tool readiness (custom tool definitions)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q\n---\n%s", want, out)
		}
	}
	// The old conflated label MUST NOT reappear.
	for _, gone := range []string{"Tools found:", "Tool defs:"} {
		if strings.Contains(out, gone) {
			t.Errorf("human output still has stale label %q\n---\n%s", gone, out)
		}
	}
}

func TestRender_MCPServersVisibleInHumanFormat(t *testing.T) {
	stdio := models.MCPServerDef{
		Class: "MCPServerStdio", Transport: "stdio", SDK: models.SDKOpenAIAgents,
		Language: models.LanguagePython,
		Location: models.Location{FilePath: "main.py", Line: 10},
	}
	result := models.ScanResult{
		Agents: []models.AgentDef{{
			SDK: models.SDKOpenAIAgents, Class: "Agent", Language: models.LanguagePython,
			Location: models.Location{FilePath: "main.py", Line: 10},
			Name:          "fs",
			MCPServerRefs: []models.MCPServerRef{{Class: "MCPServerStdio", Resolved: &stdio}},
		}},
		MCPServers: []models.MCPServerDef{stdio},
	}

	out := (&review.Renderer{NoColor: true}).Render(result)
	for _, want := range []string{
		"MCP servers:        1",
		"MCPServerStdio (stdio)",
		"mcp servers:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRender_SubagentsAndClaudeSettingsSections(t *testing.T) {
	result := models.ScanResult{
		Subagents: []models.SubagentDef{
			{Name: "researcher", Location: models.Location{FilePath: ".claude/agents/researcher.md"},
				Tools: []string{"Read", "Glob", "Grep"}, Model: "haiku"},
		},
		ClaudeSettings: []models.ClaudeSettings{{
			Location:    models.Location{FilePath: ".claude/settings.json"},
			DefaultMode: "acceptEdits",
			Permissions: models.ClaudePermissions{
				Allow: []models.PermissionRule{{Tool: "Bash", Pattern: "npm test", Raw: "Bash(npm test)"}},
				Deny:  []models.PermissionRule{{Tool: "WebFetch", Raw: "WebFetch"}},
			},
		}},
	}

	out := (&review.Renderer{NoColor: true}).Render(result)
	for _, want := range []string{
		"Subagents",                            // section header
		"researcher",                           // subagent name
		".claude/agents/researcher.md",         // subagent file path
		"tools: Read, Glob, Grep",              // tools list
		"model: haiku",                         // model
		"Claude settings",                      // section header
		".claude/settings.json",                // settings file path
		"defaultMode=acceptEdits",              // settings metadata
		"allow:1",                              // permission counts
		"deny:1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRender_EmptyInventorySkipsNewLines(t *testing.T) {
	// Sanity: a repo with no hosted tools / MCP / subagents / settings must
	// not print the new summary lines. We don't want clutter on simple repos.
	result := models.ScanResult{
		Repo: "./fixture",
		Tools: []models.ToolDef{
			{Name: "do_thing", Kind: models.KindOpenAITool, Language: models.LanguagePython},
		},
	}
	out := (&review.Renderer{NoColor: true}).Render(result)
	for _, unwanted := range []string{
		"Hosted tools:",
		"MCP servers:",
		"Subagents:",
		"Claude settings:",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("empty-inventory render leaked %q\n---\n%s", unwanted, out)
		}
	}
}

// TestRender_NonToolFindingsAppearUnderRepoWide is the regression test for
// the cut-off bug: when ALL findings have empty ToolName (META findings,
// agent-scoped findings with no matching tool), the renderer printed the
// "Findings" header and stopped — the actual findings were silently
// dropped. Root cause was that the renderer iterated Readiness (tool-only)
// to find findings, so any finding without a matching tool was orphaned.
func TestRender_NonToolFindingsAppearUnderRepoWide(t *testing.T) {
	result := models.ScanResult{
		Repo:      "./fixture",
		Languages: []models.Language{models.LanguageTypeScript},
		SDKs:      []models.SDK{models.SDKClaudeAgentSDK},
		Tools: []models.ToolDef{
			{Name: "search", Kind: models.KindClaudeSDKTool, Language: models.LanguageTypeScript},
		},
		Readiness: []models.ToolReadiness{
			{ToolName: "search", Score: 1.0},
		},
		Findings: []models.Finding{
			{
				RuleID:       "META-004",
				Severity:     models.SeverityInfo,
				Title:        "SDK detected but no rule was applicable",
				Explanation:  "Trustabl detected the SDK in code but no rules applied.",
				SuggestedFix: "Treat as uncovered.",
				Confidence:   1.0,
			},
		},
		OverallScore: 1.0,
	}

	out := (&review.Renderer{NoColor: true}).Render(result)

	// The Findings header must be followed by the actual finding content.
	if !strings.Contains(out, "META-004") {
		t.Errorf("META-004 (non-tool finding) missing from human output\n---\n%s", out)
	}
	if !strings.Contains(out, "SDK detected but no rule was applicable") {
		t.Errorf("finding title missing from human output\n---\n%s", out)
	}
	if !strings.Contains(out, "(repo-wide)") {
		t.Errorf("expected a (repo-wide) group header for non-tool findings\n---\n%s", out)
	}
}

// TestRenderer_PrintsLineRange verifies that multi-line inventory entities
// (agents, subagents, ClaudeSettings) render as "file:start-end" in the human
// output, and that single-line entities (EndLine == Line) collapse to "file:N"
// without the redundant "-N" suffix.
func TestRenderer_PrintsLineRange(t *testing.T) {
	result := models.ScanResult{
		Agents: []models.AgentDef{
			{
				SDK:      models.SDKClaudeAgentSDK,
				Class:    "AgentDefinition",
				Language: models.LanguagePython,
				Name:     "multi",
				Location: models.Location{FilePath: "f.py", Line: 10, EndLine: 20},
			},
			{
				SDK:      models.SDKClaudeAgentSDK,
				Class:    "AgentDefinition",
				Language: models.LanguagePython,
				Name:     "single",
				Location: models.Location{FilePath: "g.py", Line: 5, EndLine: 5},
			},
		},
		Subagents: []models.SubagentDef{
			{
				Name:     "sub",
				Location: models.Location{FilePath: ".claude/agents/sub.md", Line: 1, EndLine: 7},
			},
		},
		ClaudeSettings: []models.ClaudeSettings{
			{
				Location: models.Location{FilePath: ".claude/settings.json", Line: 1, EndLine: 12},
			},
		},
	}

	out := (&review.Renderer{NoColor: true}).Render(result)

	if !strings.Contains(out, "f.py:10-20") {
		t.Errorf("multi-line agent: expected 'f.py:10-20' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "g.py:5") {
		t.Errorf("single-line agent: expected 'g.py:5' in output, got:\n%s", out)
	}
	if strings.Contains(out, "g.py:5-5") {
		t.Errorf("single-line agent: 'g.py:5-5' must NOT appear (Line==EndLine should collapse), got:\n%s", out)
	}
	if !strings.Contains(out, ".claude/agents/sub.md:1-7") {
		t.Errorf("subagent: expected '.claude/agents/sub.md:1-7' in output, got:\n%s", out)
	}
	if !strings.Contains(out, ".claude/settings.json:1-12") {
		t.Errorf("settings: expected '.claude/settings.json:1-12' in output, got:\n%s", out)
	}
}

// TestRender_AgentScopedFindingsAppearUnderAgentName covers the forward-
// compatible case: when SP2's TS rule pack ships, agent-scoped findings
// will carry ToolName = agent name (not blank, but not matching any tool
// either). The renderer must group those under the agent's name.
func TestRender_AgentScopedFindingsAppearUnderAgentName(t *testing.T) {
	result := models.ScanResult{
		Repo: "./fixture",
		Tools: []models.ToolDef{
			{Name: "search", Kind: models.KindClaudeSDKTool, Language: models.LanguageTypeScript},
		},
		Readiness: []models.ToolReadiness{
			{ToolName: "search", Score: 1.0},
		},
		Findings: []models.Finding{
			{
				RuleID:       "CSDK-201",
				ToolName:     "AIClient.queryStream",
				Severity:     models.SeverityHigh,
				Title:        "Main thread agent has unrestricted Bash",
				Explanation:  "...",
				SuggestedFix: "...",
				Confidence:   1.0,
				FilePath:     "ccsdk/ai-client.ts",
				Line:         86,
			},
		},
	}

	out := (&review.Renderer{NoColor: true}).Render(result)
	if !strings.Contains(out, "CSDK-201") {
		t.Errorf("agent-scoped finding missing from human output\n---\n%s", out)
	}
	if !strings.Contains(out, "AIClient.queryStream") {
		t.Errorf("agent name missing as finding group header\n---\n%s", out)
	}
}
