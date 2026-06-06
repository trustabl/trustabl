// Package models holds the typed values that cross module boundaries.
//
// Discipline: anything passed from ingestion → analysis → review
// lives here, not as ad-hoc maps. JSON tags are present because ScanResult is
// emitted for CI piping (--format json).
package models

// Scope classifies a rule by the kind of entity it fires against.
type Scope string

const (
	ScopeTool     Scope = "tool"
	ScopeAgent    Scope = "agent"
	ScopeRepo     Scope = "repo"
	ScopeSubagent Scope = "subagent"
)

// ValidScope reports whether s is a known scope value.
func ValidScope(s Scope) bool {
	switch s {
	case ScopeTool, ScopeAgent, ScopeRepo, ScopeSubagent:
		return true
	}
	return false
}

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// SeverityWeight maps a severity to a numeric weight for scoring. Tweak in
// scoring.go, not here, if the curve needs adjustment.
func SeverityWeight(s Severity) float64 {
	switch s {
	case SeverityCritical:
		return 1.0
	case SeverityHigh:
		return 0.7
	case SeverityMedium:
		return 0.4
	case SeverityLow:
		return 0.15
	default:
		return 0.05
	}
}

// DetectorCategory maps to the two AutoFix categories in architecture §0.
type DetectorCategory string

const (
	CategoryClaudeSDK  DetectorCategory = "claude_sdk"
	CategoryOpenAISDK  DetectorCategory = "openai_sdk"
	CategoryOpenShell  DetectorCategory = "openshell"
	CategoryGoogleADK  DetectorCategory = "google_adk"
	CategoryMCP        DetectorCategory = "mcp"
	CategoryLangChain  DetectorCategory = "langchain"
	CategoryCrewAI     DetectorCategory = "crewai"
	CategoryPydanticAI DetectorCategory = "pydantic_ai"
	CategoryVercelAI   DetectorCategory = "vercel_ai"
	CategoryAutoGen    DetectorCategory = "autogen"
)

// ValidCategory reports whether c is a category this build recognizes. New SDK
// categories are added here as coverage lands. The rule loader skips packs with
// an unrecognized category leniently at runtime (forward-compat: a newer rules
// release must not block an older binary from scanning the SDKs it knows) and
// rejects them in strict (authoring/CI) mode so a typo'd category is caught.
func ValidCategory(c DetectorCategory) bool {
	switch c {
	case CategoryClaudeSDK, CategoryOpenAISDK, CategoryOpenShell, CategoryGoogleADK,
		CategoryMCP, CategoryLangChain, CategoryCrewAI, CategoryPydanticAI,
		CategoryVercelAI, CategoryAutoGen:
		return true
	}
	return false
}

// ToolKind drives detector applicability.
type ToolKind string

const (
	KindClaudeSDKTool   ToolKind = "claude_sdk_tool"
	KindOpenAITool      ToolKind = "openai_tool" // OpenAI Agents SDK @function_tool
	KindMCPTool         ToolKind = "mcp_tool"
	KindShellInvocation ToolKind = "shell_invocation"
	KindUnknown         ToolKind = "unknown"
	KindADKFunctionTool ToolKind = "adk_function_tool"
	// KindLangChainTool is a LangChain / LangGraph tool: the @tool decorator,
	// a StructuredTool/Tool factory or constructor (Python), or the tool()
	// factory / DynamicStructuredTool / DynamicTool (TypeScript). Discovery is
	// import-gated so it does not collide with the Claude-SDK @tool / tool()
	// shapes that share the same callee name.
	KindLangChainTool  ToolKind = "langchain_tool"
	KindCrewAITool     ToolKind = "crewai_tool"
	KindPydanticAITool ToolKind = "pydantic_ai_tool"
	KindVercelAITool   ToolKind = "vercel_ai_tool"
	KindAutoGenTool    ToolKind = "autogen_tool"
)

// Language identifies the source language of a discovered tool. Rules
// declare a language too, and a rule only applies to a tool of the matching
// language. Empty string is treated as "python" by the loader for
// backwards compatibility.
type Language string

const (
	LanguagePython     Language = "python"
	LanguageTypeScript Language = "typescript"
	LanguageJavaScript Language = "javascript"
	LanguageGo         Language = "go"
	LanguageCSharp     Language = "csharp"
)

// IsTSOrJS reports whether l is in the TypeScript/JavaScript family. The two
// share the tree-sitter grammar (the tsx parser parses plain JS), every
// discovery pass, and the discovery-computed body facts — so JavaScript
// tools/agents are audited by the same rule packs and predicates as TypeScript.
// The rule language-gate and the TS-branch predicates treat the two
// interchangeably; JS-sourced defs are re-tagged LanguageJavaScript for honest
// output after edge resolution (see scanner.retagJavaScriptDefs).
func IsTSOrJS(l Language) bool {
	return l == LanguageTypeScript || l == LanguageJavaScript
}

// ToolDef is one discovered surface that an agent can invoke at runtime.
// Mirrors the Tool Discovery node in architecture §2.
type ToolDef struct {
	Name           string            `json:"name"`
	VarName        string            `json:"var_name,omitempty"` // const-binding name (TS); empty for Python where Name and binding name coincide
	Kind           ToolKind          `json:"kind"`
	Language       Language          `json:"language"`
	Location                         // file_path / start_line / end_line (flat in JSON via anonymous embed)
	Description    string            `json:"description,omitempty"`
	HasTypedParams bool              `json:"has_typed_params"`
	ParamNames     []string          `json:"param_names,omitempty"`
	Facts          map[string]string `json:"facts,omitempty"`
	Config         map[string]string `json:"config,omitempty"` // decorator kwargs
}

// ComponentKind labels the type of an agent component the normalizer found
// outside of the tool function itself. Components are surfaced for context
// and for future detection passes; today's rule engine only fires against
// ToolDef, not against AgentComponent.
type ComponentKind string

const (
	ComponentMCPConfig             ComponentKind = "mcp_config"
	ComponentClaudeMd              ComponentKind = "claude_md"
	ComponentAgentsMd              ComponentKind = "agents_md" // AGENTS.md — vendor-neutral agent-guidance doc
	ComponentClaudeSettings        ComponentKind = "claude_settings"
	ComponentSubagent              ComponentKind = "subagent"
	ComponentSlashCommand          ComponentKind = "slash_command"
	ComponentHookScript            ComponentKind = "hook_script"
	ComponentSandboxPolicy         ComponentKind = "sandbox_policy"
	ComponentSystemPrompt          ComponentKind = "system_prompt"
	ComponentDependencyManifest    ComponentKind = "dependency_manifest"
	ComponentClaudeAgentDefinition ComponentKind = "claude_agent_definition" // Python file using AgentDefinition
	ComponentSkill                 ComponentKind = "skill"
	ComponentPluginManifest        ComponentKind = "plugin_manifest"
)

// AgentComponent is a non-tool agent-related artifact discovered during
// normalization: an MCP config, CLAUDE.md, hook script, sandbox policy,
// dependency manifest, etc. These are surfaced in ScanManifest.Components
// so users see the full agent surface, even though detection rules currently
// only run against tools.
type AgentComponent struct {
	Kind     ComponentKind `json:"kind"`
	Path     string        `json:"path"`               // relative to repo root
	Language Language      `json:"language,omitempty"` // for code components only; empty for configs / prompts
	Note     string        `json:"note,omitempty"`     // human-readable hint, e.g. "3 tools registered"
}

// Finding is one detector hit on one surface (tool, agent, subagent, or repo).
type Finding struct {
	RuleID       string           `json:"rule_id"`
	Category     DetectorCategory `json:"category"`
	Scope        Scope            `json:"scope"` // tool|agent|subagent|repo; empty for META findings
	Severity     Severity         `json:"severity"`
	ToolName     string           `json:"tool_name"`
	FilePath     string           `json:"file_path"`
	Line         int              `json:"line"`
	Title        string           `json:"title"`
	Explanation  string           `json:"explanation"` // "show your work" per doc §7
	SuggestedFix string           `json:"suggested_fix"`
	Confidence   float64          `json:"confidence"` // 0..1
}

// SurfaceReadiness is the readiness score for one analyzable surface — a single
// tool, agent, or subagent, or the repo as a whole. Kind names which; Name is
// empty for the repo surface. Identity is (Kind, FilePath, Name) so same-named
// surfaces across files stay distinct.
type SurfaceReadiness struct {
	Kind             Scope   `json:"kind"`      // tool|agent|subagent|repo
	Name             string  `json:"name"`      // "" for the repo surface
	FilePath         string  `json:"file_path"` // distinguishes same-named surfaces across files
	Score            float64 `json:"score"`     // 0..1
	FindingCount     int     `json:"finding_count"`
	WeightedSeverity float64 `json:"weighted_severity"`
}

// ProjectedScores are overall-score projections after resolving findings up to
// and including each severity tier, cumulatively. Each value is the real
// overall score (see analysis.Score) recomputed with the remaining findings —
// an ESTIMATE, not a re-scan: it assumes each resolved finding is removed
// cleanly and introduces nothing new. Values are in [0,1] and non-decreasing
// from FixCritical → FixAll. Single source of truth for the "headroom" ladder;
// consumers (e.g. the GitHub Action) must not recompute scoring.
type ProjectedScores struct {
	FixCritical float64 `json:"fix_critical"` // resolve all critical findings
	FixHigh     float64 `json:"fix_high"`     // + all high
	FixMedium   float64 `json:"fix_medium"`   // + all medium
	FixLow      float64 `json:"fix_low"`      // + all low
	FixAll      float64 `json:"fix_all"`      // + all info (everything resolved)
}

// ScanManifest is what the Normalizer produces.
type ScanManifest struct {
	RepoRoot               string           `json:"repo_root"`
	IsRemote               bool             `json:"is_remote"`
	RemoteURL              string           `json:"remote_url,omitempty"`
	PythonFiles            []string         `json:"python_files"`
	TypeScriptFiles        []string         `json:"typescript_files,omitempty"`
	JavaScriptFiles        []string         `json:"javascript_files,omitempty"`
	GoFiles                []string         `json:"go_files,omitempty"`
	CSharpFiles            []string         `json:"csharp_files,omitempty"`
	YAMLFiles              []string         `json:"yaml_files"`
	JSONFiles              []string         `json:"json_files,omitempty"`
	MarkdownFiles          []string         `json:"markdown_files,omitempty"`
	HasClaudeSDKDependency bool             `json:"has_claude_sdk_dependency"`
	HasOpenShellArtifact   bool             `json:"has_openshell_artifact"`
	Components             []AgentComponent `json:"components,omitempty"`
}

// SDK identifies a tool/agent SDK we know about.
type SDK string

const (
	SDKClaudeAgentSDK SDK = "claude_agent_sdk"
	SDKOpenAIAgents   SDK = "openai_agents"
	SDKMCP            SDK = "mcp"
	SDKGoogleADK      SDK = "google_adk"
	SDKLangChain      SDK = "langchain" // LangChain + LangGraph (one ecosystem, one SDK row)
	SDKCrewAI         SDK = "crewai"
	SDKPydanticAI     SDK = "pydantic_ai"
	SDKVercelAI       SDK = "vercel_ai"
	SDKAutoGen        SDK = "autogen"
)

// "openshell" is intentionally NOT in the SDK enum: it is not a library
// you import. It is a synthesized risk-surface label for Python functions
// that call subprocess.* / os.system / os.popen. The surface is carried
// on RepoInventory.HasShellInvocations and on ScanResult.HasShellInvocations.
// Rule YAML still references the literal string "openshell" in
// `applies_to:` for repo-scope rules; repoRuleDetector.Applies and
// PredRepoHasSDKInCode route that string to HasShellInvocations.

type SDKDep struct {
	Name       string  `json:"name"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
}

// RepoProfile is the output of the recon step.
type RepoProfile struct {
	Languages []Language   `json:"languages"`
	SDKDeps   []SDKDep     `json:"sdk_deps"`
	Manifest  ScanManifest `json:"manifest"`
}

// RepoInventory is the output of the inventory step.
// AgentDef, GuardrailDef, SessionUse, HostedToolDef, MCPServerDef are in agent.go.
type RepoInventory struct {
	Tools              []ToolDef               `json:"tools"`
	Agents             []AgentDef              `json:"agents"`
	Guardrails         []GuardrailDef          `json:"guardrails"`
	Sessions           []SessionUse            `json:"sessions"`
	HostedTools        []HostedToolDef         `json:"hosted_tools"`
	MCPServers         []MCPServerDef          `json:"mcp_servers"`
	Subagents          []SubagentDef           `json:"subagents"`
	Skills             []SkillDef              `json:"skills"`
	SlashCommands      []SlashCommandDef       `json:"slash_commands"`
	PluginManifests    []PluginManifest        `json:"plugin_manifests"`
	ClaudeSettings     []ClaudeSettings        `json:"claude_settings"`
	ClaudeAgentOptions []ClaudeAgentOptionsDef `json:"claude_agent_options,omitempty"`
	SDKsDetected       []SDK                   `json:"sdks_detected"`
	// HasShellInvocations is true if any discovered ToolDef is a
	// KindShellInvocation (a Python function whose body calls
	// subprocess.* / os.system / os.popen). This is the "openshell" risk
	// surface; it is deliberately not modeled as an SDK because there is
	// no library to import.
	HasShellInvocations bool         `json:"has_shell_invocations"`
	Manifest            ScanManifest `json:"manifest"` // convenience copy for repo-scope predicates
	UsesDefaultTracing  bool         `json:"uses_default_tracing"`
}

// Coverage records how thoroughly the scan actually parsed the repo's source.
// A scanner that silently skips files it cannot read or parse can report a
// near-empty, low-risk result that is indistinguishable from a genuinely clean
// repo — the worst failure mode for a security tool. Surfacing the skip count
// makes incomplete coverage an explicit, machine-readable signal.
type Coverage struct {
	FilesParsed  int `json:"files_parsed"`  // source files successfully read AND parsed
	FilesSkipped int `json:"files_skipped"` // source files attempted but skipped (read or parse error)
	// SkippedFiles names the relative paths that were attempted but could not be
	// read or parsed (the identities behind FilesSkipped). Sorted for
	// determinism; omitted from JSON when empty. Lets the report say *which*
	// files went unanalyzed, not just how many.
	SkippedFiles []string `json:"skipped_files,omitempty"`
}

// ScanResult is the top-level output. JSON-serializable for CI.
type ScanResult struct {
	ScanID              string             `json:"scan_id"`
	Repo                string             `json:"repo"`
	Languages           []Language         `json:"languages"` // detected by file extension (recon)
	SDKs                []SDK              `json:"sdks"`      // observed in code (inventory)
	HasShellInvocations bool               `json:"has_shell_invocations"`
	Manifest            ScanManifest       `json:"manifest"`
	Tools               []ToolDef          `json:"tools"`
	Agents              []AgentDef         `json:"agents"`
	HostedTools         []HostedToolDef    `json:"hosted_tools"`
	MCPServers          []MCPServerDef     `json:"mcp_servers"`
	Subagents           []SubagentDef      `json:"subagents"`
	Skills              []SkillDef         `json:"skills"`
	SlashCommands       []SlashCommandDef  `json:"slash_commands"`
	PluginManifests     []PluginManifest   `json:"plugin_manifests"`
	ClaudeSettings      []ClaudeSettings   `json:"claude_settings"`
	Findings            []Finding          `json:"findings"`
	Surfaces            []SurfaceReadiness `json:"surfaces"`
	OverallScore        float64            `json:"overall_score"`
	ProjectedScores     ProjectedScores    `json:"projected_scores"`
	RulesSource         string             `json:"rules_source"`                   // repo the rule pack came from
	RulesVersion        string             `json:"rules_version"`                  // resolved rules commit SHA
	RulesFromCache      bool               `json:"rules_from_cache"`               // true if rules came from cache (network skipped/unreachable)
	RulesSchemaVersion  int                `json:"rules_schema_version,omitempty"` // pack manifest's declared schema_version
	RulesSchemaNewer    bool               `json:"rules_schema_newer,omitempty"`   // pack targets a schema newer than this build supports
	RulesSkipped        []string           `json:"rules_skipped,omitempty"`        // rule IDs dropped as forward-incompatible (sorted, deduped)
	Coverage            Coverage           `json:"coverage"`                       // how many source files parsed vs. were skipped
}
