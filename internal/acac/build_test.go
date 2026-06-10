package acac

import (
	"errors"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestSlugID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Main Agent", "main-agent"},
		{"research_bot", "research_bot"},
		{"Crème Brûlée!!", "cr-me-br-l-e"},
		{"--weird--", "weird"},
		{"_underscored", "underscored"},
		{"42nd Agent", "42nd-agent"},
		{"!!!", "agent"},
		{"", "agent"},
		{"A  B", "a-b"},
	}
	for _, c := range cases {
		if got := slugID(c.in); got != c.want {
			t.Errorf("slugID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAliasSetSanitizesAndDedupes(t *testing.T) {
	s := newAliasSet()
	if got := s.claim("search-web"); got != "search_web" {
		t.Errorf("claim = %q, want search_web", got)
	}
	if got := s.claim("search web"); got != "search_web_2" {
		t.Errorf("collision claim = %q, want search_web_2", got)
	}
	if got := s.claim("search.web"); got != "search_web_3" {
		t.Errorf("second collision claim = %q, want search_web_3", got)
	}
	if got := s.claim("9lives"); got != "_9lives" {
		t.Errorf("leading-digit claim = %q, want _9lives", got)
	}
	if got := s.claim(""); got != "_" {
		t.Errorf("empty claim = %q, want _", got)
	}
}

func agentNamed(name, file string, line int) models.AgentDef {
	a := models.AgentDef{Name: name}
	a.FilePath = file
	a.Line = line
	return a
}

func TestSelectAgent(t *testing.T) {
	one := models.ScanResult{Agents: []models.AgentDef{agentNamed("solo", "a.py", 1)}}
	got, err := SelectAgent(one, "")
	if err != nil || got.Name != "solo" {
		t.Fatalf("single-agent default selection failed: %v %v", got.Name, err)
	}

	none := models.ScanResult{}
	if _, err := SelectAgent(none, ""); !errors.Is(err, ErrNoAgents) {
		t.Fatalf("zero agents: got %v, want ErrNoAgents", err)
	}

	many := models.ScanResult{Agents: []models.AgentDef{
		agentNamed("alpha", "a.py", 1),
		agentNamed("beta", "b.py", 2),
	}}
	_, err = SelectAgent(many, "")
	var ambiguous *AmbiguousAgentError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("multi-agent without --agent: got %v, want AmbiguousAgentError", err)
	}
	if len(ambiguous.Candidates) != 2 || !strings.Contains(ambiguous.Candidates[0], "alpha (a.py:1)") {
		t.Errorf("candidates = %v", ambiguous.Candidates)
	}

	got, err = SelectAgent(many, "beta")
	if err != nil || got.Name != "beta" {
		t.Fatalf("named selection failed: %v %v", got.Name, err)
	}

	_, err = SelectAgent(many, "gamma")
	var unknown *UnknownAgentError
	if !errors.As(err, &unknown) || unknown.Matches != 0 {
		t.Fatalf("unknown name: got %v", err)
	}

	dup := models.ScanResult{Agents: []models.AgentDef{
		agentNamed("twin", "a.py", 1),
		agentNamed("twin", "b.py", 2),
	}}
	_, err = SelectAgent(dup, "twin")
	if !errors.As(err, &unknown) || unknown.Matches != 2 {
		t.Fatalf("same-named agents: got %v, want 2-match UnknownAgentError", err)
	}
}

// buildFixtureResult assembles a synthetic ScanResult exercising every
// derivation path Build has: resolved + external tools, approval suggestion
// facts, an MCP server, a subagent, a handoff, a skill, sessions, findings on
// graph and off graph, and surfaces.
func buildFixtureResult() (models.ScanResult, models.AgentDef) {
	shellTool := models.ToolDef{
		Name:        "run_shell",
		Kind:        models.KindOpenAITool,
		Description: "Runs a shell command.",
		ParamNames:  []string{"cmd"},
		Facts:       map[string]string{"shells_out": "true"},
	}
	shellTool.FilePath = "tools.py"
	shellTool.Line = 10

	cleanTool := models.ToolDef{
		Name:        "lookup",
		Kind:        models.KindOpenAITool,
		Description: "Looks something up.",
		ParamNames:  []string{"query", "max_results"},
	}
	cleanTool.FilePath = "tools.py"
	cleanTool.Line = 30

	otherAgent := agentNamed("researcher", "other.py", 5)

	agent := models.AgentDef{
		Name: "main",
		Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
			"instructions": {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"Do research.\nCarefully."`}},
			"model":        {Value: &models.Expr{Kind: models.ExprLiteralString, Text: `"gpt-4o"`}},
		}},
		ToolRefs: []models.ToolRef{
			{Name: "run_shell", Resolved: &shellTool},
			{Name: "lookup", Resolved: &cleanTool},
			{Name: "mystery", External: true},
		},
		HandoffRefs:    []models.AgentRef{{Name: "researcher", Resolved: &otherAgent}},
		HostedToolRefs: []models.HostedToolRef{{Class: "WebSearchTool"}},
	}
	agent.FilePath = "main.py"
	agent.Line = 1

	sub := models.SubagentDef{Name: "inbox-searcher", Description: "Searches the inbox."}
	sub.FilePath = ".claude/agents/inbox-searcher.md"
	skill := models.SkillDef{
		Name:       "pdf-tools",
		ToolGrants: []models.ToolGrant{{Tool: "Read", Raw: "Read"}, {Tool: "Bash", Raw: "Bash"}},
	}
	skill.FilePath = "skills/pdf-tools/SKILL.md"
	session := models.SessionUse{Class: "SQLiteSession"}
	session.FilePath = "main.py"

	findings := []models.Finding{
		{RuleID: "OAI-012", Scope: models.ScopeTool, ToolName: "run_shell", FilePath: "tools.py",
			Severity: models.SeverityHigh, Title: "Tool body spawns a subprocess", SuggestedFix: "Gate it.", Confidence: 0.9},
		{RuleID: "OAI-999", Scope: models.ScopeTool, ToolName: "off_graph", FilePath: "elsewhere.py",
			Severity: models.SeverityCritical, Title: "Off-graph finding", Confidence: 1},
		{RuleID: "CSDK-201", Scope: models.ScopeRepo,
			Severity: models.SeverityHigh, Title: "Repo-level permission bypass", SuggestedFix: "Fix settings.", Confidence: 1},
		{RuleID: "META-001", Severity: models.SeverityInfo, Title: "Unaudited SDK in use", Confidence: 1},
	}
	surfaces := []models.SurfaceReadiness{
		{Kind: models.ScopeTool, Name: "run_shell", FilePath: "tools.py", Score: 0.55},
		{Kind: models.ScopeTool, Name: "off_graph", FilePath: "elsewhere.py", Score: 0.1},
		{Kind: models.ScopeAgent, Name: "main", FilePath: "main.py", Score: 0.7},
		{Kind: models.ScopeAgent, Name: "researcher", FilePath: "other.py", Score: 0.9},
		{Kind: models.ScopeSubagent, Name: "inbox-searcher", FilePath: ".claude/agents/inbox-searcher.md", Score: 0.8},
		{Kind: models.ScopeSkill, Name: "pdf-tools", FilePath: "skills/pdf-tools/SKILL.md", Score: 0.9},
		{Kind: models.ScopeTool, Name: "lookup", FilePath: "tools.py", Score: 1.0},
	}

	result := models.ScanResult{
		ScanID:       "scan-123",
		RulesVersion: "rules-sha",
		Agents:       []models.AgentDef{agent, otherAgent},
		Tools:        []models.ToolDef{shellTool, cleanTool},
		Subagents:    []models.SubagentDef{sub},
		Skills:       []models.SkillDef{skill},
		Sessions:     []models.SessionUse{session},
		SDKs:         []models.SDK{models.SDKOpenAIAgents},
		Dependencies: []models.DepRef{{Name: "requests", Source: "requirements.txt"}, {Name: "openai", Source: "requirements.txt"}},
		Findings:     findings,
		Surfaces:     surfaces,
		OverallScore: 0.61,
	}
	return result, agent
}

func TestBuildDerivations(t *testing.T) {
	result, agent := buildFixtureResult()
	m := Build(result, agent, BuildOptions{EngineVersion: "test", IncludeOWASP: true})

	if m.Metadata.Name != "main" || m.Metadata.ID != "main" || m.Metadata.NameScaffolded {
		t.Errorf("metadata name/id wrong: %+v", m.Metadata)
	}
	if m.Metadata.Description != "Do research.\\nCarefully." && m.Metadata.Description != "Do research." {
		// The raw text holds a source-level escape; first-line split applies
		// to real newlines. Either way it must be derived, not scaffolded.
		if m.Metadata.DescScaffolded {
			t.Errorf("description should derive from instructions, got scaffold")
		}
	}
	if m.Memory == nil || !m.Memory.Required {
		t.Error("memory.required should be set from Sessions")
	}
	if m.ExecutionPolicy.Instructions == "" || m.ExecutionPolicy.InstructionsScaffolded {
		t.Errorf("instructions should derive: %+v", m.ExecutionPolicy)
	}
	if m.ExecutionPolicy.Model != "gpt-4o" || m.ExecutionPolicy.ModelScaffolded {
		t.Errorf("model should derive: %+v", m.ExecutionPolicy)
	}

	tools := m.ActionSpace.LocalTools
	if len(tools) != 3 {
		t.Fatalf("local_tools = %d entries, want 3", len(tools))
	}
	// Sorted by name: lookup, mystery (external), run_shell.
	if tools[0].Name != "lookup" || tools[1].Name != "mystery" || tools[2].Name != "run_shell" {
		t.Errorf("local_tools order: %v %v %v", tools[0].Name, tools[1].Name, tools[2].Name)
	}
	if !tools[1].External {
		t.Error("mystery should be external")
	}
	if !tools[2].ApprovalSuggested {
		t.Error("run_shell should carry an approval suggestion (shells_out)")
	}

	agents := m.ActionSpace.LocalAgents
	if len(agents) != 2 {
		t.Fatalf("local_agents = %d entries, want subagent + handoff", len(agents))
	}
	if agents[0].Alias != "inbox_searcher" || agents[0].Review {
		t.Errorf("subagent entry wrong: %+v", agents[0])
	}
	if agents[1].Alias != "researcher" || !agents[1].Review || agents[1].Source != "other.py" {
		t.Errorf("handoff entry wrong: %+v", agents[1])
	}

	x := m.XTrustabl
	if x.Agent != "main" || x.ScanID != "scan-123" || x.RulesVersion != "rules-sha" {
		t.Errorf("x-trustabl identity wrong: %+v", x)
	}
	if x.Score100 != 61 {
		t.Errorf("score100 = %d, want 61", x.Score100)
	}
	// 61 < 85 with a high finding → needs_work.
	if x.Readiness != ReadinessNeedsWork {
		t.Errorf("readiness = %s, want needs_work", x.Readiness)
	}

	// Off-graph tool surface and finding must be excluded; META excluded.
	for _, s := range x.Surfaces {
		if s.Ref == "off_graph" {
			t.Error("off-graph surface leaked into x-trustabl")
		}
	}
	if len(x.Surfaces) != 6 {
		t.Errorf("surfaces = %d, want 6 (root agent, handoff target, 2 tools, subagent, skill)", len(x.Surfaces))
	}
	var ids []string
	for _, f := range x.Findings {
		ids = append(ids, f.ID)
	}
	if len(ids) != 2 || ids[0] != "OAI-012" || ids[1] != "CSDK-201" {
		t.Errorf("findings = %v, want [OAI-012 CSDK-201]", ids)
	}
	if len(x.Findings[0].OWASP) == 0 {
		t.Error("OAI-012 should carry owasp IDs from the pinned map")
	}
	if x.Findings[1].Ref != "" {
		t.Error("repo-scope finding must have empty ref")
	}

	// Root agent surface ref must be the manifest id.
	foundRoot := false
	for _, s := range x.Surfaces {
		if s.Kind == "agent" && s.Ref == "main" {
			foundRoot = true
		}
	}
	if !foundRoot {
		t.Error("root agent surface missing or not cross-referenced by manifest id")
	}

	if len(x.HostedTools) != 1 || x.HostedTools[0] != "WebSearchTool" {
		t.Errorf("hosted_tools = %v", x.HostedTools)
	}
	if x.Coverage.DepCount != 2 || len(x.Coverage.DepManifests) != 1 {
		t.Errorf("coverage deps wrong: %+v", x.Coverage)
	}
	if len(x.Skills) != 1 || x.Skills[0].Name != "pdf-tools" || !x.Skills[0].ModelInvocable {
		t.Errorf("skills wrong: %+v", x.Skills)
	}
}

func TestBuildOWASPOmittedWhenDisabled(t *testing.T) {
	result, agent := buildFixtureResult()
	m := Build(result, agent, BuildOptions{EngineVersion: "test", IncludeOWASP: false})
	for _, f := range m.XTrustabl.Findings {
		if len(f.OWASP) != 0 {
			t.Errorf("finding %s carries owasp despite --owasp=false", f.ID)
		}
	}
}
