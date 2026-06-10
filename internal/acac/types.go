// Package acac turns a completed ScanResult into two declarative artifacts,
// from the same read-only scan. They belong to two distinct artifact classes
// — a distinction this package is careful to honor:
//
//   - The Agent Format manifest (.agf.yaml), carrying the Trustabl
//     reliability/readiness extension (x-trustabl). This is the Agent
//     CONFIGURATION artifact (ACaC): it defines the agent's identity,
//     declared tools, permissions, and skills, it travels with the agent
//     across conforming runtimes, and it is model/runtime-interpreted —
//     a behavioral, not enforced, guarantee. This is the package's primary
//     output and the reason for its name.
//   - The optional OpenShell sandbox policy (openshell.go). This is an
//     agent INFRASTRUCTURE artifact (AIaC): it constrains the host, stays
//     with the OpenShell gateway rather than the agent, and is enforced
//     mechanically by seccomp/Landlock/an L7 egress proxy — a structural
//     guarantee verified live (see .superpowers/verification/).
//
// The two outputs have different review owners (the manifest → engineering;
// the policy → platform/security), which the docs make explicit. The package
// is a pure transform — it never re-parses source and never re-runs
// discovery; everything it emits is read off the typed ScanResult.
//
// The manifest flow is: SelectAgent (one manifest describes one agent system)
// → Build (ScanResult → Manifest tree) → Emit (Manifest → deterministic YAML
// with scaffold-marker comments). The policy flow mirrors it:
// BuildOpenShellPolicy → ValidateOpenShellPolicy → EmitOpenShellPolicy.
package acac

// SpecVersion is the x-trustabl extension spec version this package
// implements. The readiness thresholds in gate.go are versioned with it:
// changing them is a spec change, not a silent retune.
const SpecVersion = "0.2"

// SchemaVersion is the AgentFormat schema version generated manifests target.
// A copy of the published schema is vendored at schema/agentformat-1.0.json.
const SchemaVersion = "1.0.0"

// Scaffold markers, per the spec's marker convention. They are emitted as
// YAML comments; the values they annotate are checked for presence by humans,
// never invented by the generator.
const (
	// MarkerNeedsHuman flags a field required by the schema or by policy but
	// not derivable from code.
	MarkerNeedsHuman = "trustabl: needs-human-input"
	// MarkerSuggested flags a derived suggestion (safe default emitted, human
	// confirms).
	MarkerSuggested = "trustabl: suggested — confirm"
	// MarkerReview flags something detected whose safe value cannot be
	// derived (dynamic URL, code-defined handoff target, unresolved ref).
	MarkerReview = "trustabl: review"
)

// Manifest is the full generated document: the Agent Format base fields plus
// the x-trustabl extension block. Field order here mirrors emission order.
type Manifest struct {
	Metadata        Metadata
	Interface       Interface
	Memory          *Memory // nil → key omitted (schema: absent means not required)
	Constraints     Constraints
	ActionSpace     ActionSpace
	ExecutionPolicy ExecutionPolicy
	XTrustabl       XTrustabl
}

// Metadata mirrors the schema's required metadata block. The *Scaffolded
// flags drive needs-human-input markers in emission; the values themselves
// are always present (the schema requires non-empty strings).
type Metadata struct {
	Name           string
	NameScaffolded bool // true when the name fell back to the agent class
	ID             string
	Description    string
	DescScaffolded bool
	Version        string // always the "0.1.0" scaffold in v0.x (runtime intent)
}

// Interface is always scaffolded in v0.x: the engine captures tool-level
// param names only, not the agent's I/O contract. ParamHints carries
// "tool(param, param)" lines emitted as comment hints above the block.
type Interface struct {
	ParamHints []string
}

// Memory maps SessionUse presence to the schema's memory.required.
type Memory struct {
	Required bool
}

// Constraints carries only what v0.x derives; budget/limits/
// governance_policies are emitted as empty scaffolds with markers.
type Constraints struct {
	TightenOnlyInvariant bool
}

// LocalTool is one action_space.local_tools entry.
type LocalTool struct {
	Alias             string
	Name              string
	Description       string
	ApprovalSuggested bool // emit approval: true + suggested marker
	External          bool // unresolved tool ref; emit review marker
}

// MCPServerEntry is one action_space.mcp_servers entry. ServerRef is always
// a scaffold in v0.x (registry identity is org intent). AllowedTools is
// derived only when the captured config kwarg was a literal list.
type MCPServerEntry struct {
	Alias               string
	Description         string
	AllowedTools        []string
	AllowedToolsDerived bool
	External            bool
}

// LocalAgent is one action_space.local_agents entry: a markdown subagent
// (source_type file, source = the .md path) or an in-code handoff target
// (source = the defining source file, review marker — code is not a
// manifest).
type LocalAgent struct {
	Alias       string
	SourceType  string
	Source      string
	Description string
	Review      bool
}

// ActionSpace mirrors the schema's action_space block. remote_agents is
// never emitted in v0.x (A2A discovery is out of scope).
type ActionSpace struct {
	LocalTools  []LocalTool
	MCPServers  []MCPServerEntry
	LocalAgents []LocalAgent
}

// ExecutionPolicy is always agf.react in v0.x — orchestration shapes are not
// inferred. Instructions and Model are schema-required non-empty strings;
// when not derivable from captured kwargs they hold scaffold text and the
// *Scaffolded flag drives the marker.
type ExecutionPolicy struct {
	ID                     string
	Instructions           string
	InstructionsScaffolded bool
	Model                  string
	ModelScaffolded        bool
}

// XTrustabl is the vendor extension block (spec §5).
type XTrustabl struct {
	SpecVersion   string
	EngineVersion string
	RulesVersion  string
	ScanID        string
	GeneratedAt   string // RFC3339; non-empty only with --timestamp
	Agent         string // manifest root (metadata.id), for cross-checking
	Readiness     ReadinessLevel
	Score100      int

	Surfaces        []Surface
	Findings        []FindingRecord
	Skills          []SkillRecord
	HostedTools     []string
	Coverage        Coverage
	Vulnerabilities []VulnRecord // only when the scan ran --vuln-scan
}

// Surface is one scored surface of the selected agent's graph. Facts carries
// the tool's body facts verbatim (tool surfaces only).
type Surface struct {
	Kind  string
	Ref   string
	Score int
	Facts map[string]string
}

// FindingRecord is one finding attributed to the selected agent's graph.
// OWASP holds pinned-map IDs; empty means the key is omitted, never
// empty-listed.
type FindingRecord struct {
	ID       string
	Scope    string
	Ref      string // empty for repo-scope findings (key omitted)
	Severity string
	Message  string
	Fix      string
	OWASP    []string
}

// SkillRecord is one SKILL.md declaration (no base-schema home; rides in
// x-trustabl).
type SkillRecord struct {
	Name           string
	ToolGrants     []string
	ModelInvocable bool
}

// Coverage is the honest-coverage block: which SDKs the scan saw, which of
// those Trustabl does not audit, and the dependency BOM summary (TR-278).
type Coverage struct {
	SDKsDetected []string
	Unaudited    []string
	DepCount     int
	DepManifests []string
}

// VulnRecord is one OSV match from --vuln-scan (TR-271).
type VulnRecord struct {
	ID           string
	Package      string
	Severity     string
	FixedVersion string
}
