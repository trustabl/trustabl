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
		Surfaces: []models.SurfaceReadiness{
			{Kind: models.ScopeTool, Name: "search_inbox", Score: 1.0},
			{Kind: models.ScopeTool, Name: "read_emails", Score: 1.0},
		},
	}

	out := (&review.Renderer{NoColor: true}).Render(result)

	// New breakdown: defs always shown; grants only when > 0.
	for _, want := range []string{
		"Tool definitions:   2",
		"Agent tool grants:  4",
		"Surface readiness",
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
			Location:      models.Location{FilePath: "main.py", Line: 10},
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

func TestRender_SkillsCommandsPluginsVisible(t *testing.T) {
	result := models.ScanResult{
		Subagents:       []models.SubagentDef{{Name: "a", Location: models.Location{FilePath: ".claude/agents/a.md", Line: 1}}},
		Skills:          []models.SkillDef{{Name: "deploy", Location: models.Location{FilePath: ".claude/skills/deploy/SKILL.md", Line: 1}}},
		SlashCommands:   []models.SlashCommandDef{{Name: "ship", Location: models.Location{FilePath: ".claude/commands/ship.md", Line: 1}}},
		PluginManifests: []models.PluginManifest{{Kind: "marketplace", Name: "m", Location: models.Location{FilePath: ".claude-plugin/marketplace.json", Line: 1}}},
	}
	out := (&review.Renderer{NoColor: true}).Render(result)
	for _, want := range []string{
		"Skills:             1",
		"deploy",
		"Slash commands:     1",
		"Plugin manifests:   1",
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
		"Subagents",                    // section header
		"researcher",                   // subagent name
		".claude/agents/researcher.md", // subagent file path
		"tools: Read, Glob, Grep",      // tools list
		"model: haiku",                 // model
		"Claude settings",              // section header
		".claude/settings.json",        // settings file path
		"defaultMode=acceptEdits",      // settings metadata
		"allow:1",                      // permission counts
		"deny:1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRender_RiskSurfacesShowsWhyAndFix(t *testing.T) {
	// The risk-surfaces line must (a) report the count, (b) name the first few
	// offending file:line locations so the user can jump straight in, (c)
	// explain WHY this is a risk (the prompt-injection threat model), and
	// (d) prescribe a concrete FIX. It must NOT claim an audit happened (no
	// openshell rule pack ships today — see the fix in the previous commit).
	tools := []models.ToolDef{
		{Kind: models.KindShellInvocation, Name: "a", Language: models.LanguagePython, Location: models.Location{FilePath: "a/b.py", Line: 12}},
		{Kind: models.KindShellInvocation, Name: "b", Language: models.LanguagePython, Location: models.Location{FilePath: "c/d.py", Line: 34}},
		{Kind: models.KindShellInvocation, Name: "c", Language: models.LanguagePython, Location: models.Location{FilePath: "e/f.py", Line: 56}},
		{Kind: models.KindShellInvocation, Name: "d", Language: models.LanguagePython, Location: models.Location{FilePath: "g/h.py", Line: 78}},
		{Kind: models.KindShellInvocation, Name: "e", Language: models.LanguagePython, Location: models.Location{FilePath: "i/j.py", Line: 90}},
		// A non-shell tool must NOT be counted or shown as an example.
		{Kind: models.KindOpenAITool, Name: "ignored", Language: models.LanguagePython, Location: models.Location{FilePath: "z.py", Line: 1}},
	}
	result := models.ScanResult{Repo: "./x", HasShellInvocations: true, Tools: tools}
	out := (&review.Renderer{NoColor: true}).Render(result)

	for _, want := range []string{
		"Risk surfaces:  openshell",
		"5 functions call subprocess",
		"a/b.py:12", // first example, deterministically sorted
		"2 more",    // overflow indicator (5 total, 3 shown)
		"why:",
		"prompt-injected",
		"fix:",
		"sandbox",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	for _, gone := range []string{
		"audited by", // honesty guarantee — must never claim an audit
		"z.py",       // non-shell tool must not leak into examples
	} {
		if strings.Contains(out, gone) {
			t.Errorf("output unexpectedly contains %q\n---\n%s", gone, out)
		}
	}
}

func TestRender_RiskSurfacesSingularForOneFunction(t *testing.T) {
	result := models.ScanResult{
		Repo:                "./x",
		HasShellInvocations: true,
		Tools: []models.ToolDef{
			{Kind: models.KindShellInvocation, Name: "lone", Language: models.LanguagePython, Location: models.Location{FilePath: "x.py", Line: 1}},
		},
	}
	out := (&review.Renderer{NoColor: true}).Render(result)
	if !strings.Contains(out, "1 function calls") {
		t.Errorf("expected singular '1 function calls'; got\n---\n%s", out)
	}
	if strings.Contains(out, " more") {
		t.Errorf("no 'more' suffix expected for a single example; got\n---\n%s", out)
	}
}

func TestRender_RiskSurfacesOmittedWhenNoShellInvocations(t *testing.T) {
	result := models.ScanResult{Repo: "./x", HasShellInvocations: false}
	out := (&review.Renderer{NoColor: true}).Render(result)
	if strings.Contains(out, "Risk surfaces:") {
		t.Errorf("risk-surfaces line must be omitted when HasShellInvocations is false; got\n---\n%s", out)
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
		Surfaces: []models.SurfaceReadiness{
			{Kind: models.ScopeTool, Name: "search", Score: 1.0},
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
		Surfaces: []models.SurfaceReadiness{
			{Kind: models.ScopeTool, Name: "search", Score: 1.0},
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

// TestRender_RulesSkippedShownAsDegraded: a scan that dropped forward-incompatible
// rules must say so as a first-class summary line, so a partial scan never reads
// as a complete one. A clean scan must not show the line.
func TestRender_RulesSkippedShownAsDegraded(t *testing.T) {
	withSkips := (&review.Renderer{NoColor: true}).Render(models.ScanResult{
		RulesSkipped: []string{"FOO-001", "BAR-002"},
	})
	if !strings.Contains(withSkips, "Rules skipped:") {
		t.Errorf("summary missing the skipped-rules signal:\n%s", withSkips)
	}
	if !strings.Contains(withSkips, "degraded scan") {
		t.Errorf("skipped-rules line should mark the scan degraded:\n%s", withSkips)
	}

	newer := (&review.Renderer{NoColor: true}).Render(models.ScanResult{
		RulesSkipped:     []string{"FOO-001"},
		RulesSchemaNewer: true,
	})
	if !strings.Contains(newer, "newer schema") {
		t.Errorf("schema-newer skip reason missing:\n%s", newer)
	}

	clean := (&review.Renderer{NoColor: true}).Render(models.ScanResult{})
	if strings.Contains(clean, "Rules skipped:") {
		t.Errorf("a clean scan must not show a skipped-rules line:\n%s", clean)
	}
}
