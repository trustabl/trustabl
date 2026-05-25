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
// repo whose only tools are hosted (e.g. examples/research_bot using
// WebSearchTool) used to show "Tools found: 0" in human mode despite the
// JSON listing the tool.
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
				Name:     "search",
				FilePath: "agents/search.py",
				Line:     12,
				HostedToolRefs: []models.HostedToolRef{
					{Class: "WebSearchTool"},
				},
			},
		},
		HostedTools: []models.HostedToolDef{
			{Class: "WebSearchTool", SDK: models.SDKOpenAIAgents, FilePath: "agents/search.py", Line: 12},
		},
	}

	out := (&review.Renderer{NoColor: true}).Render(result)

	for _, want := range []string{
		"Hosted tools:   1",
		"WebSearchTool",
		"hosted tools:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q\n---\n%s", want, out)
		}
	}

	// "Tools found" must include the hosted tool class.
	if !strings.Contains(out, "Tools found:    1") {
		t.Errorf("Tools found count should be 1 (the hosted WebSearchTool); got:\n%s", out)
	}
}

func TestRender_MCPServersVisibleInHumanFormat(t *testing.T) {
	stdio := models.MCPServerDef{
		Class: "MCPServerStdio", Transport: "stdio", SDK: models.SDKOpenAIAgents,
		Language: models.LanguagePython,
		FilePath: "main.py", Line: 10,
	}
	result := models.ScanResult{
		Agents: []models.AgentDef{{
			SDK: models.SDKOpenAIAgents, Class: "Agent", Language: models.LanguagePython, Name: "fs",
			FilePath: "main.py", Line: 10,
			MCPServerRefs: []models.MCPServerRef{{Class: "MCPServerStdio", Resolved: &stdio}},
		}},
		MCPServers: []models.MCPServerDef{stdio},
	}

	out := (&review.Renderer{NoColor: true}).Render(result)
	for _, want := range []string{
		"MCP servers:    1",
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
			{Name: "researcher", FilePath: ".claude/agents/researcher.md",
				Tools: []string{"Read", "Glob", "Grep"}, Model: "haiku"},
		},
		ClaudeSettings: []models.ClaudeSettings{{
			FilePath:    ".claude/settings.json",
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
