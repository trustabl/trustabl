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
	ScopeSkill    Scope = "skill"
)

// AllScopes is every rule scope this build recognizes, in a stable order. Single
// source of truth: ValidScope checks against it and the capability descriptor
// (trustabl capabilities) lists it. Mirrors AllLanguages / AllCategories.
var AllScopes = []Scope{ScopeTool, ScopeAgent, ScopeRepo, ScopeSubagent, ScopeSkill}

// ValidScope reports whether s is a known scope value.
func ValidScope(s Scope) bool {
	for _, k := range AllScopes {
		if s == k {
			return true
		}
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
	// CategoryClaudeSkill covers Claude Code Agent Skills (SKILL.md). Skills are
	// not an SDK import, so this category is loaded unconditionally (like
	// openshell) rather than gated on SDKsDetected.
	CategoryClaudeSkill DetectorCategory = "claude_skill"
)

// display order. It is the single source of truth for category membership:
// ValidCategory checks against it, the CLI's --detectors flag derives its help
// text and validation error from it, and the capability descriptor (trustabl
// capabilities) lists it (so none drift as coverage lands). New SDK categories
// are added here as coverage lands.
var AllCategories = []DetectorCategory{
	CategoryClaudeSDK, CategoryOpenAISDK, CategoryOpenShell, CategoryGoogleADK,
	CategoryMCP, CategoryLangChain, CategoryCrewAI, CategoryPydanticAI,
	CategoryVercelAI, CategoryAutoGen, CategoryClaudeSkill,
}

// ValidCategory reports whether c is a category this build recognizes. The rule
// loader skips packs with an unrecognized category leniently at runtime
// (forward-compat: a newer rules release must not block an older binary from
// scanning the SDKs it knows) and rejects them in strict (authoring/CI) mode so
// a typo'd category is caught.
func ValidCategory(c DetectorCategory) bool {
	for _, k := range AllCategories {
		if c == k {
			return true
		}
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
	LanguagePHP        Language = "php"
	LanguageRust       Language = "rust"
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

// AllLanguages is every source language this build recognizes, in a stable
// order. It is the single source of truth for language membership: ValidLanguage
// checks against it, the strict rule loader builds its allow-list error from it,
// and the lenient (runtime) loader uses it to decide whether a rule targets a
// language this build does not understand — a forward-incompatible rule it skips
// rather than hard-failing. New languages are added here as discovery lands.
var AllLanguages = []Language{
	LanguagePython, LanguageTypeScript, LanguageJavaScript, LanguageGo,
	LanguageCSharp, LanguagePHP, LanguageRust,
}

// ValidLanguage reports whether l is a source language this build recognizes.
// Empty is NOT valid here: the loader treats an empty rule language as the
// python default separately, so this answers only "is this an explicit, known
// language". Mirrors ValidScope / ValidCategory.
func ValidLanguage(l Language) bool {
	for _, k := range AllLanguages {
		if l == k {
			return true
		}
	}
	return false
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
	Config         map[string]string `json:"config,omitempty"`
	// HTTPHosts are the canonical host:port targets of recognized HTTP calls
	// in the tool body whose URL argument is a plain string literal. The
	// scheme's default port is applied when the literal names none (https →
	// 443, http → 80) so consumers never re-derive it from a scheme the
	// inventory no longer carries. Static literals only: any interpolation
	// (f-string, template substitution, concatenation) captures nothing —
	// the existing dynamic-URL signals are unchanged. Sorted + deduped at
	// capture; never DNS-resolved (determinism contract).
	HTTPHosts []string `json:"http_hosts,omitempty"`
	// FSWritePaths are path literals passed to recognized filesystem-write
	// shapes in the tool body (open with a write mode, pathlib write_*,
	// shutil copy/move targets; fs.writeFile*/createWriteStream). Recorded
	// verbatim as written in source. Static literals only; sorted + deduped.
	FSWritePaths []string `json:"fs_write_paths,omitempty"` // decorator kwargs
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
	RuleID   string           `json:"rule_id"`
	Category DetectorCategory `json:"category"`
	Scope    Scope            `json:"scope"` // tool|agent|subagent|repo; empty for META findings
	Severity Severity         `json:"severity"`
	ToolName string           `json:"tool_name"`
	FilePath string           `json:"file_path"`
	// StartLine/EndLine are the 1-indexed inclusive line range of the entity the
	// finding fired on (a tool, agent, skill, subagent, or a declared dependency).
	// A single-line entity sets EndLine == StartLine. Both are 0 for repo-scope
	// findings that have no source location. Mirrors models.Location.
	StartLine    int     `json:"start_line"`
	EndLine      int     `json:"end_line"`
	Title        string  `json:"title"`
	Explanation  string  `json:"explanation"` // "show your work" per doc §7
	SuggestedFix string  `json:"suggested_fix"`
	Confidence   float64 `json:"confidence"` // 0..1
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
	PHPFiles               []string         `json:"php_files,omitempty"`
	RustFiles              []string         `json:"rust_files,omitempty"`
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

// DepRef is one dependency declared in a repo manifest — the agent-path
// supply-chain BOM (Story TR-278; supersedes the skill-only BOM of TR-221). It
// is pure inventory: Trustabl records what the repo DECLARES, then either hands
// off to a real SCA tool (OSV-Scanner, Dependabot) or matches it against a
// pinned OSV snapshot under the opt-in --vuln-scan (TR-271).
//
//   - Version is the declared spec verbatim — a pin ("1.2.3"), a range
//     ("^1.0", ">=2,<3"), or empty when the manifest names a dep with no
//     constraint. It is NOT a resolved/locked version.
//   - Ecosystem is the package registry: pypi / npm / golang / nuget /
//     composer / cargo.
//   - Source is the repo-relative path of the manifest the dep was read from
//     (a skill's bundled manifest, or a project-level one).
//
// Dependencies is deliberately NOT folded into ScanID: it is an additive
// inventory field, so emitting it must not perturb the byte-stable scan
// identity of an otherwise-unchanged repo.
type DepRef struct {
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	Ecosystem string `json:"ecosystem"`
	Source    string `json:"source"`
	// StartLine/EndLine are the 1-indexed line of the dependency's declaration in
	// Source. A declaration is a single manifest line, so EndLine == StartLine.
	// Mirrors models.Location so a dependency attributes like any other entity.
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

// DepVuln is one known vulnerability matched against a declared dependency by the
// opt-in --vuln-scan layer (TR-271): a BOM DepRef checked against a pinned OSV
// snapshot. ID is the primary OSV identifier (GHSA-… / PYSEC-… / CVE-…), Aliases
// the cross-references (the CVE), Severity is bucketed from the record's CVSS
// score, and FixedIn the first patched version when known. Emitted both as
// ScanResult.Vulnerabilities and as findings (so it affects exit codes + SARIF).
type DepVuln struct {
	Dep      DepRef   `json:"dep"`
	ID       string   `json:"id"`
	Aliases  []string `json:"aliases,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Severity Severity `json:"severity"`
	FixedIn  string   `json:"fixed_in,omitempty"`
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
	Dependencies       []DepRef                `json:"dependencies"`
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

// RulesOrigin classifies where a scan's rules came from and how far they can be
// trusted. It drives two things: a one-line report watermark for any scan that
// did NOT use blessed, signature-verified production rules, and an origin tag
// folded into ScanID so two scans of the same code with rules of different
// provenance get distinct IDs.
type RulesOrigin struct {
	// Signed is true when the rules were resolved through a verified signed
	// channel (releaseSource). Unsigned scans use the git path.
	Signed bool `json:"signed"`
	// Channel is the signed channel name ("production", "staging") when Signed.
	Channel string `json:"channel,omitempty"`
	// Custom is true when the operator overrode the default rules source
	// (--rules-repo / TRUSTABL_RULES_REPO) on the unsigned git path.
	Custom bool `json:"custom,omitempty"`
}

// Tag is the stable origin string folded into ScanID. It is always non-empty so
// the ID is honest about provenance for every scan.
func (o RulesOrigin) Tag() string {
	if o.Signed {
		ch := o.Channel
		if ch == "" {
			ch = "unknown"
		}
		return "signed:" + ch
	}
	if o.Custom {
		return "unsigned:custom"
	}
	return "unsigned:default"
}

// Watermark returns the report banner for a scan that deviated from blessed
// production rules, or "" for a clean, trusted scan. Signed production is clean;
// a signed pre-release channel and any unsigned custom source are flagged. The
// plain unsigned default is not flagged — pre-cutover it is the normal source;
// after the signed-production cutover (ENG-6) the default becomes signed and the
// git path is reached only via --rules-repo, which is Custom and so flagged.
func (o RulesOrigin) Watermark() string {
	switch {
	case o.Signed && o.Channel != "" && o.Channel != "production":
		return "Rules channel: " + o.Channel + " — pre-release rules, not blessed for production."
	case !o.Signed && o.Custom:
		return "UNSIGNED rules from a custom source — these rules were not signature-verified."
	default:
		return ""
	}
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
	Guardrails          []GuardrailDef     `json:"guardrails"`                // discovered guardrail functions; feeds agent-edge cross-checks and ACaC
	Sessions            []SessionUse       `json:"sessions"`                  // session-construction sites; presence drives ACaC memory.required
	Dependencies        []DepRef           `json:"dependencies"`              // repo-wide declared-dependency BOM (TR-278); not folded into ScanID
	Vulnerabilities     []DepVuln          `json:"vulnerabilities,omitempty"` // --vuln-scan OSV matches (TR-271); absent on the default path
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
	RulesStale          bool               `json:"rules_stale,omitempty"`          // true if the cached signed bundle's channel statement has expired
	RulesSchemaVersion  int                `json:"rules_schema_version,omitempty"` // pack manifest's declared schema_version
	RulesSchemaNewer    bool               `json:"rules_schema_newer,omitempty"`   // pack targets a schema newer than this build supports
	RulesSkipped        []string           `json:"rules_skipped,omitempty"`        // rule IDs dropped as forward-incompatible (sorted, deduped)
	RulesOrigin         RulesOrigin        `json:"rules_origin"`                   // provenance of the rules (signed channel / unsigned / custom)
	Coverage            Coverage           `json:"coverage"`                       // how many source files parsed vs. were skipped
}

// EnrichedFinding extends Finding with AI-generated context and an optional
// in-place code replacement produced by trustabl enrich.
type EnrichedFinding struct {
	Finding
	AIExplanation string `json:"ai_explanation,omitempty"`
	AIFix         string `json:"ai_fix,omitempty"`
	CodeSnippet   string `json:"code_snippet,omitempty"`
	LineStart     int    `json:"line_start,omitempty"`
	LineEnd       int    `json:"line_end,omitempty"`
	Replacement   string `json:"replacement,omitempty"`
	Diff          string `json:"diff,omitempty"` // unified diff of the proposed replacement (populated when --diff is set)
	FalsePositive bool   `json:"false_positive,omitempty"`
	Enriched      bool   `json:"enriched"`
	Applied       bool   `json:"applied,omitempty"`
}

// EnrichmentResult is the top-level output of trustabl enrich.
type EnrichmentResult struct {
	ScanID       string            `json:"scan_id,omitempty"`
	Repo         string            `json:"repo,omitempty"`
	RulesVersion string            `json:"rules_version,omitempty"`
	Findings     []EnrichedFinding `json:"findings"`
	EnrichedAt   int64             `json:"enriched_at"` // Unix milliseconds
}
