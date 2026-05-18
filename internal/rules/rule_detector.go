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
	for _, k := range d.rule.AppliesTo {
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
	}
	return false
}

// NewToolRuleDetector wraps a RuleDef as a ToolDetector. Exported for test packages.
func NewToolRuleDetector(r RuleDef) detectors.ToolDetector { return toolRuleDetector{r} }

// NewAgentRuleDetector wraps a RuleDef as an AgentDetector. Exported for test packages.
func NewAgentRuleDetector(r RuleDef) detectors.AgentDetector { return agentRuleDetector{r} }

// NewRepoRuleDetector wraps a RuleDef as a RepoDetector. Exported for test packages.
func NewRepoRuleDetector(r RuleDef) detectors.RepoDetector { return repoRuleDetector{r} }

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
		FixHints:     r.FixHints,
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
		}
	}
	all, err := Load(fsys)
	if err != nil {
		return nil, err
	}
	var tool []detectors.ToolDetector
	var agent []detectors.AgentDetector
	var repo []detectors.RepoDetector
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
			}
		}
	}
	return detectors.New(tool, agent, repo), nil
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
	for _, p := range policies {
		for _, r := range p.Rules {
			switch r.Scope {
			case models.ScopeTool:
				tool = append(tool, toolRuleDetector{r})
			case models.ScopeAgent:
				agent = append(agent, agentRuleDetector{r})
			case models.ScopeRepo:
				repo = append(repo, repoRuleDetector{r})
			}
		}
	}
	return detectors.New(tool, agent, repo), nil
}
