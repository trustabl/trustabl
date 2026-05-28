package rules

import (
	"io/fs"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/detectors"
	"github.com/trustabl/trustabl/internal/models"
)

// toolRuleDetector adapts a tool-scoped RuleDef into a ToolDetector.
type toolRuleDetector struct{ rule RuleDef }

func (d toolRuleDetector) RuleID() string                    { return d.rule.ID }
func (d toolRuleDetector) Category() models.DetectorCategory { return d.rule.Category }
func (d toolRuleDetector) Applies(t models.ToolDef) bool {
	if d.rule.Language != "" && d.rule.Language != t.Language {
		return false
	}
	for _, k := range d.rule.AppliesTo {
		if string(t.Kind) == k {
			return true
		}
	}
	return false
}
func (d toolRuleDetector) Detect(t models.ToolDef, pf analysis.ParsedFile, inv models.RepoInventory) []models.Finding {
	if !d.rule.Match.EvaluateTool(t, pf) {
		return nil
	}
	return []models.Finding{findingFromRule(d.rule, t.FilePath, t.Line, t.Name)}
}

// agentRuleDetector adapts an agent-scoped RuleDef into an AgentDetector.
type agentRuleDetector struct{ rule RuleDef }

func (d agentRuleDetector) RuleID() string                    { return d.rule.ID }
func (d agentRuleDetector) Category() models.DetectorCategory { return d.rule.Category }
func (d agentRuleDetector) Applies(a models.AgentDef) bool {
	if d.rule.Language != "" && d.rule.Language != a.Language {
		return false
	}
	for _, k := range d.rule.AppliesTo {
		if agentKindMatches(k, a) {
			return true
		}
	}
	return false
}
func (d agentRuleDetector) Detect(a models.AgentDef, inv models.RepoInventory) []models.Finding {
	if !d.rule.Match.EvaluateAgent(a, inv) {
		return nil
	}
	return []models.Finding{findingFromRule(d.rule, a.FilePath, a.Line, a.Name)}
}

// repoRuleDetector adapts a repo-scoped RuleDef into a RepoDetector.
type repoRuleDetector struct{ rule RuleDef }

func (d repoRuleDetector) RuleID() string                    { return d.rule.ID }
func (d repoRuleDetector) Category() models.DetectorCategory { return d.rule.Category }
func (d repoRuleDetector) Applies(p models.RepoProfile, inv models.RepoInventory) bool {
	// Language gate at repo scope: a language-typed rule fires only when the
	// inventory observes that language. Matches the gate enforced by
	// toolRuleDetector and agentRuleDetector. Without this, a rule with
	// `language: python` would silently fire on a TS-only repo where the
	// rule's predicates have no Python code to evaluate against (e.g.
	// OAI-201's repo_uses_default_tracing trivially holds on a Python-free
	// repo since the tracing-config call never appears).
	if d.rule.Language != "" {
		var hasLang bool
		for _, l := range p.Languages {
			if l == d.rule.Language {
				hasLang = true
				break
			}
		}
		if !hasLang {
			return false
		}
	}
	for _, k := range d.rule.AppliesTo {
		// "openshell" is a risk-surface label, not an SDK — route it to
		// HasShellInvocations instead of looking it up in SDKsDetected.
		if k == "openshell" {
			if inv.HasShellInvocations {
				return true
			}
			continue
		}
		for _, sdk := range inv.SDKsDetected {
			if string(sdk) == k {
				return true
			}
		}
	}
	return false
}
func (d repoRuleDetector) Detect(p models.RepoProfile, inv models.RepoInventory) []models.Finding {
	if !d.rule.Match.EvaluateRepo(p, inv) {
		return nil
	}
	return []models.Finding{findingFromRule(d.rule, "", 0, "")}
}

func agentKindMatches(kind string, a models.AgentDef) bool {
	switch kind {
	case "openai_agent":
		return a.SDK == models.SDKOpenAIAgents && a.Class == "Agent"
	case "openai_sandbox_agent":
		return a.SDK == models.SDKOpenAIAgents && a.Class == "SandboxAgent"
	case "claude_agent_definition":
		return a.SDK == models.SDKClaudeAgentSDK && a.Class == "AgentDefinition"
	case "claude_query_main":
		return a.SDK == models.SDKClaudeAgentSDK && a.Class == "QueryMainAgent"
	case "adk_llm_agent":
		return a.SDK == models.SDKGoogleADK && a.Class == "LlmAgent"
	case "adk_sequential_agent":
		return a.SDK == models.SDKGoogleADK && a.Class == "SequentialAgent"
	case "adk_parallel_agent":
		return a.SDK == models.SDKGoogleADK && a.Class == "ParallelAgent"
	case "adk_loop_agent":
		return a.SDK == models.SDKGoogleADK && a.Class == "LoopAgent"
	case "adk_langgraph_agent":
		return a.SDK == models.SDKGoogleADK && a.Class == "LanggraphAgent"
	}
	return false
}

// subagentRuleDetector adapts a subagent-scoped RuleDef into a SubagentDetector.
type subagentRuleDetector struct{ rule RuleDef }

func (d subagentRuleDetector) RuleID() string                    { return d.rule.ID }
func (d subagentRuleDetector) Category() models.DetectorCategory { return d.rule.Category }
func (d subagentRuleDetector) Applies(s models.SubagentDef) bool {
	// No language gate: SubagentDef has no Language (markdown frontmatter is
	// not Python/TS). Subagents are inherently Claude Code artifacts.
	for _, k := range d.rule.AppliesTo {
		if k == "claude_subagent" {
			return true
		}
	}
	return false
}
func (d subagentRuleDetector) Detect(s models.SubagentDef, inv models.RepoInventory) []models.Finding {
	if !d.rule.Match.EvaluateSubagent(s, inv) {
		return nil
	}
	// SubagentDef embeds Location: Line = the opening "---" of the frontmatter
	// (usually 1), EndLine = the closing "---". Attribute to the opening so
	// the user lands at the start of the declaration when jumping from a
	// finding.
	return []models.Finding{findingFromRule(d.rule, s.FilePath, s.Line, s.Name)}
}

// NewToolRuleDetector wraps a RuleDef as a ToolDetector. Exported for test packages.
func NewToolRuleDetector(r RuleDef) detectors.ToolDetector { return toolRuleDetector{r} }

// NewAgentRuleDetector wraps a RuleDef as an AgentDetector. Exported for test packages.
func NewAgentRuleDetector(r RuleDef) detectors.AgentDetector { return agentRuleDetector{r} }

// NewRepoRuleDetector wraps a RuleDef as a RepoDetector. Exported for test packages.
func NewRepoRuleDetector(r RuleDef) detectors.RepoDetector { return repoRuleDetector{r} }

// NewSubagentRuleDetector wraps a RuleDef as a SubagentDetector. Exported for test packages.
func NewSubagentRuleDetector(r RuleDef) detectors.SubagentDetector { return subagentRuleDetector{r} }

func findingFromRule(r RuleDef, filePath string, line int, toolName string) models.Finding {
	return models.Finding{
		RuleID:       r.ID,
		Category:     r.Category,
		Severity:     r.Severity,
		ToolName:     toolName,
		FilePath:     filePath,
		Line:         line,
		Title:        r.Title,
		Explanation:  r.Explanation,
		SuggestedFix: r.Fix,
		Confidence:   r.Confidence,
	}
}

// LoadFor returns a Registry containing only policy packs whose category matches
// one of the observed SDKs. openshell rules are always loaded.
// If sdks is empty, only openshell rules are returned.
func LoadFor(fsys fs.FS, sdks []models.SDK) (*detectors.Registry, error) {
	wanted := map[string]bool{
		"openshell": true,
	}
	for _, sdk := range sdks {
		switch sdk {
		case models.SDKClaudeAgentSDK:
			wanted["claude_sdk"] = true
		case models.SDKOpenAIAgents:
			wanted["openai_sdk"] = true
		case models.SDKMCP:
			wanted["mcp"] = true
		case models.SDKGoogleADK:
			wanted["google_adk"] = true
		}
	}
	all, err := Load(fsys)
	if err != nil {
		return nil, err
	}
	var tool []detectors.ToolDetector
	var agent []detectors.AgentDetector
	var repo []detectors.RepoDetector
	var subagent []detectors.SubagentDetector
	for _, p := range all {
		if !wanted[string(p.Policy.Category)] {
			continue
		}
		for _, r := range p.Rules {
			switch r.Scope {
			case models.ScopeTool:
				tool = append(tool, toolRuleDetector{r})
			case models.ScopeAgent:
				agent = append(agent, agentRuleDetector{r})
			case models.ScopeRepo:
				repo = append(repo, repoRuleDetector{r})
			case models.ScopeSubagent:
				subagent = append(subagent, subagentRuleDetector{r})
			}
		}
	}
	return detectors.New(tool, agent, repo, subagent), nil
}

// LoadRegistry loads policies from fsys and returns a populated detector Registry.
func LoadRegistry(fsys fs.FS) (*detectors.Registry, error) {
	policies, err := Load(fsys)
	if err != nil {
		return nil, err
	}
	var tool []detectors.ToolDetector
	var agent []detectors.AgentDetector
	var repo []detectors.RepoDetector
	var subagent []detectors.SubagentDetector
	for _, p := range policies {
		for _, r := range p.Rules {
			switch r.Scope {
			case models.ScopeTool:
				tool = append(tool, toolRuleDetector{r})
			case models.ScopeAgent:
				agent = append(agent, agentRuleDetector{r})
			case models.ScopeRepo:
				repo = append(repo, repoRuleDetector{r})
			case models.ScopeSubagent:
				subagent = append(subagent, subagentRuleDetector{r})
			}
		}
	}
	return detectors.New(tool, agent, repo, subagent), nil
}
