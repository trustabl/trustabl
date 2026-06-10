package acac

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

// ErrNoAgents means the scan discovered no AgentDef — there is nothing to
// generate a manifest for (subagents and skills are never manifest roots).
var ErrNoAgents = errors.New("no agent declarations discovered: nothing to generate")

// AmbiguousAgentError is returned when more than one agent was discovered and
// no --agent was given.
type AmbiguousAgentError struct {
	Candidates []string
}

func (e *AmbiguousAgentError) Error() string {
	return fmt.Sprintf("more than one agent discovered; pass --agent with one of:\n  %s",
		strings.Join(e.Candidates, "\n  "))
}

// UnknownAgentError is returned when --agent matched no discovered agent (or
// matched more than one, which a name cannot disambiguate).
type UnknownAgentError struct {
	Name       string
	Matches    int
	Candidates []string
}

func (e *UnknownAgentError) Error() string {
	if e.Matches > 1 {
		return fmt.Sprintf("--agent %q matches %d agents and cannot disambiguate them; candidates:\n  %s",
			e.Name, e.Matches, strings.Join(e.Candidates, "\n  "))
	}
	return fmt.Sprintf("--agent %q matches no discovered agent; candidates:\n  %s",
		e.Name, strings.Join(e.Candidates, "\n  "))
}

// BuildOptions parameterizes Build.
type BuildOptions struct {
	// EngineVersion is stamped into x-trustabl.engine_version.
	EngineVersion string
	// IncludeOWASP controls whether findings carry owasp IDs from the pinned
	// engine map (--owasp, default on).
	IncludeOWASP bool
	// GeneratedAt is an RFC3339 timestamp emitted as x-trustabl.generated_at.
	// Empty (the default) omits the field — deterministic output is the
	// default; --timestamp opts in.
	GeneratedAt string
}

// agentLabel renders one selection candidate: the agent's best name plus its
// declaration site.
func agentLabel(a models.AgentDef) string {
	return fmt.Sprintf("%s (%s:%d)", displayName(a), a.FilePath, a.Line)
}

// displayName is the best human name for an agent: the name= kwarg, else the
// assignment-target identifier, else the constructor class.
func displayName(a models.AgentDef) string {
	if a.Name != "" {
		return a.Name
	}
	if a.VarName != "" {
		return a.VarName
	}
	return a.Class
}

// SelectAgent applies the spec §3 selection rules: exactly one agent → it is
// the root; more than one → name required; zero → ErrNoAgents. A name matches
// the agent's name= kwarg first, falling back to its assignment-target
// identifier.
func SelectAgent(result models.ScanResult, name string) (models.AgentDef, error) {
	agents := result.Agents
	if len(agents) == 0 {
		return models.AgentDef{}, ErrNoAgents
	}
	if name == "" {
		if len(agents) == 1 {
			return agents[0], nil
		}
		labels := make([]string, len(agents))
		for i, a := range agents {
			labels[i] = agentLabel(a)
		}
		return models.AgentDef{}, &AmbiguousAgentError{Candidates: labels}
	}
	var matches []models.AgentDef
	for _, a := range agents {
		if a.Name == name || (a.Name == "" && a.VarName == name) {
			matches = append(matches, a)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	labels := make([]string, len(agents))
	for i, a := range agents {
		labels[i] = agentLabel(a)
	}
	return models.AgentDef{}, &UnknownAgentError{Name: name, Matches: len(matches), Candidates: labels}
}

// Build derives the manifest tree from a completed ScanResult and the
// selected agent. It is a pure, deterministic transform: same ScanResult +
// same options → identical Manifest.
func Build(result models.ScanResult, agent models.AgentDef, opts BuildOptions) Manifest {
	var m Manifest

	// metadata — name/id auto where provable, scaffolds otherwise.
	name := agent.Name
	nameScaffolded := false
	if name == "" {
		name = agent.VarName
	}
	if name == "" {
		name = agent.Class
		nameScaffolded = true
	}
	desc, descDerived := agentDescription(agent)
	if !descDerived {
		desc = "Describe what this agent does and its intended deployment context."
	}
	m.Metadata = Metadata{
		Name:           name,
		NameScaffolded: nameScaffolded,
		ID:             slugID(name),
		Description:    desc,
		DescScaffolded: !descDerived,
		Version:        "0.1.0",
	}

	// action_space — built before interface so param hints can reference the
	// resolved tools.
	localTools, resolvedTools := buildLocalTools(agent)
	m.ActionSpace = ActionSpace{
		LocalTools:  localTools,
		MCPServers:  buildMCPServers(agent, result),
		LocalAgents: buildLocalAgents(agent, result),
	}

	// interface — never derivable today (the engine captures tool-level
	// param names only); emit hints from the resolved tools' signatures.
	var hints []string
	for _, t := range resolvedTools {
		if len(t.ParamNames) > 0 {
			hints = append(hints, fmt.Sprintf("%s(%s)", t.Name, strings.Join(t.ParamNames, ", ")))
		}
	}
	sort.Strings(hints)
	m.Interface = Interface{ParamHints: dedupeSorted(hints)}

	// memory — presence of any session-construction site in the repo.
	if len(result.Sessions) > 0 {
		m.Memory = &Memory{Required: true}
	}

	m.Constraints = Constraints{TightenOnlyInvariant: true}

	// execution_policy — agf.react; instructions/model from captured kwargs.
	instructions, instrDerived := kwargStringFirst(agent.Kwargs, instructionKwargs...)
	if !instrDerived {
		instructions = "Provide the agent's system prompt."
	}
	model, modelDerived := kwargStringFirst(agent.Kwargs, "model")
	if !modelDerived {
		model = "set-the-model-id"
	}
	m.ExecutionPolicy = ExecutionPolicy{
		ID:                     "agf.react",
		Instructions:           instructions,
		InstructionsScaffolded: !instrDerived,
		Model:                  model,
		ModelScaffolded:        !modelDerived,
	}

	m.XTrustabl = buildXTrustabl(result, agent, m.Metadata.ID, opts)
	return m
}

// surfaceKey identifies one scored surface for graph membership checks.
type surfaceKey struct {
	kind models.Scope
	file string
	name string
}

// graphSurfaces returns the surface keys of the selected agent's graph: the
// agent itself, its edge-resolved tools, its resolved handoff targets, and
// every subagent and skill (which ride along on the selected agent — they are
// never manifest roots).
func graphSurfaces(result models.ScanResult, agent models.AgentDef) map[surfaceKey]bool {
	in := map[surfaceKey]bool{
		{models.ScopeAgent, agent.FilePath, agent.Name}: true,
	}
	for _, ref := range agent.ToolRefs {
		if ref.Resolved != nil {
			in[surfaceKey{models.ScopeTool, ref.Resolved.FilePath, ref.Resolved.Name}] = true
		}
	}
	for _, ref := range agent.HandoffRefs {
		if ref.Resolved != nil {
			in[surfaceKey{models.ScopeAgent, ref.Resolved.FilePath, ref.Resolved.Name}] = true
		}
	}
	for _, s := range result.Subagents {
		in[surfaceKey{models.ScopeSubagent, s.FilePath, s.Name}] = true
	}
	for _, s := range result.Skills {
		in[surfaceKey{models.ScopeSkill, s.FilePath, s.Name}] = true
	}
	return in
}

func buildXTrustabl(result models.ScanResult, agent models.AgentDef, rootID string, opts BuildOptions) XTrustabl {
	included := graphSurfaces(result, agent)

	// Tool facts, keyed for surface enrichment.
	factsByTool := map[surfaceKey]map[string]string{}
	for i := range result.Tools {
		t := &result.Tools[i]
		if len(t.Facts) > 0 {
			factsByTool[surfaceKey{models.ScopeTool, t.FilePath, t.Name}] = t.Facts
		}
	}

	// Surfaces: filter the report's surface list (already deterministically
	// sorted worst-first) down to the graph. The root agent's ref is the
	// manifest id so the block cross-references itself.
	var surfaces []Surface
	for _, s := range result.Surfaces {
		k := surfaceKey{s.Kind, s.FilePath, s.Name}
		if !included[k] {
			continue
		}
		ref := s.Name
		if s.Kind == models.ScopeAgent && s.FilePath == agent.FilePath && s.Name == agent.Name {
			ref = rootID
		}
		surfaces = append(surfaces, Surface{
			Kind:  string(s.Kind),
			Ref:   ref,
			Score: Score100(s.Score),
			Facts: factsByTool[k],
		})
	}

	// Findings: those attributed to a graph surface, plus repo-scope findings
	// (they describe the deployment as a whole). Scope-less findings (META,
	// vulnerability synthetics) are excluded — coverage and vulnerabilities
	// carry that information in structured form below.
	var findings []FindingRecord
	for _, f := range result.Findings {
		switch f.Scope {
		case models.ScopeRepo:
			// included, ref omitted
		case models.ScopeTool, models.ScopeAgent, models.ScopeSubagent, models.ScopeSkill:
			if !included[surfaceKey{f.Scope, f.FilePath, f.ToolName}] {
				continue
			}
		default:
			continue
		}
		rec := FindingRecord{
			ID:       f.RuleID,
			Scope:    string(f.Scope),
			Ref:      f.ToolName,
			Severity: string(f.Severity),
			Message:  f.Title,
			Fix:      f.SuggestedFix,
		}
		if opts.IncludeOWASP {
			rec.OWASP = OWASPFor(f.RuleID)
		}
		findings = append(findings, rec)
	}

	// Skills inventory (no base-schema home).
	var skills []SkillRecord
	for _, s := range result.Skills {
		grants := make([]string, 0, len(s.ToolGrants))
		for _, g := range s.ToolGrants {
			grants = append(grants, g.Raw)
		}
		if len(grants) == 0 {
			grants = append(grants, s.AllowedTools...)
		}
		skills = append(skills, SkillRecord{
			Name:           s.Name,
			ToolGrants:     grants,
			ModelInvocable: !s.DisableModelInvocation,
		})
	}

	// Hosted tools wired to the selected agent (x-trustabl placement per
	// spec §4: they are not runtime-bound functions, so not local_tools).
	var hosted []string
	for _, ref := range agent.HostedToolRefs {
		hosted = append(hosted, ref.Class)
	}
	sort.Strings(hosted)
	hosted = dedupeSorted(hosted)

	// Coverage: detected SDKs, the observed-but-unaudited subset (never
	// silent), and the dependency BOM summary.
	sdks := make([]string, 0, len(result.SDKs))
	for _, s := range result.SDKs {
		sdks = append(sdks, string(s))
	}
	unaudited := make([]string, 0)
	for _, s := range scanner.UnauditedSDKs(result.SDKs) {
		unaudited = append(unaudited, string(s))
	}
	var manifests []string
	for _, d := range result.Dependencies {
		manifests = append(manifests, d.Source)
	}
	sort.Strings(manifests)
	manifests = dedupeSorted(manifests)

	// Vulnerabilities: present only when the scan ran --vuln-scan.
	var vulns []VulnRecord
	for _, v := range result.Vulnerabilities {
		vulns = append(vulns, VulnRecord{
			ID:           v.ID,
			Package:      v.Dep.Name,
			Severity:     string(v.Severity),
			FixedVersion: v.FixedIn,
		})
	}

	score := Score100(result.OverallScore)
	return XTrustabl{
		SpecVersion:   SpecVersion,
		EngineVersion: opts.EngineVersion,
		RulesVersion:  result.RulesVersion,
		ScanID:        result.ScanID,
		GeneratedAt:   opts.GeneratedAt,
		Agent:         rootID,
		Readiness:     ReadinessFor(score, findings),
		Score100:      score,
		Surfaces:      surfaces,
		Findings:      findings,
		Skills:        skills,
		HostedTools:   hosted,
		Coverage: Coverage{
			SDKsDetected: sdks,
			Unaudited:    unaudited,
			DepCount:     len(result.Dependencies),
			DepManifests: manifests,
		},
		Vulnerabilities: vulns,
	}
}

// buildLocalTools maps the agent's tool edges to local_tools entries and also
// returns the resolved ToolDefs (for interface param hints). Entries are
// sorted by tool name; aliases are sanitized and deduped in that order so the
// same input always yields the same aliases.
func buildLocalTools(agent models.AgentDef) ([]LocalTool, []models.ToolDef) {
	type entry struct {
		name     string
		desc     string
		suggest  bool
		external bool
	}
	var entries []entry
	var resolved []models.ToolDef
	seen := map[string]bool{}
	for _, ref := range agent.ToolRefs {
		if ref.Resolved != nil {
			t := *ref.Resolved
			key := "r\x00" + t.FilePath + "\x00" + t.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			resolved = append(resolved, t)
			entries = append(entries, entry{
				name:    t.Name,
				desc:    t.Description,
				suggest: t.Facts["shells_out"] == "true" || t.Facts["writes_fs"] == "true" || t.Facts["code_exec"] == "true",
			})
			continue
		}
		key := "x\x00" + ref.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, entry{name: ref.Name, external: true})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].name != entries[j].name {
			return entries[i].name < entries[j].name
		}
		return !entries[i].external && entries[j].external
	})
	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].Name != resolved[j].Name {
			return resolved[i].Name < resolved[j].Name
		}
		return resolved[i].FilePath < resolved[j].FilePath
	})

	aliases := newAliasSet()
	out := make([]LocalTool, 0, len(entries))
	for _, e := range entries {
		out = append(out, LocalTool{
			Alias:             aliases.claim(e.name),
			Name:              e.name,
			Description:       e.desc,
			ApprovalSuggested: e.suggest,
			External:          e.external,
		})
	}
	return out, resolved
}

// buildMCPServers maps the agent's MCP server edges to mcp_servers entries.
func buildMCPServers(agent models.AgentDef, result models.ScanResult) []MCPServerEntry {
	type entry struct {
		base     string
		desc     string
		allowed  []string
		derived  bool
		external bool
	}
	var entries []entry
	for _, ref := range agent.MCPServerRefs {
		def := ref.Resolved
		if def == nil && ref.DefIndex >= 0 && ref.DefIndex < len(result.MCPServers) {
			def = &result.MCPServers[ref.DefIndex]
		}
		if def == nil {
			entries = append(entries, entry{base: ref.Class, desc: ref.Class, external: true})
			continue
		}
		base := def.VarName
		if base == "" {
			base = def.Class
		}
		desc := def.Class
		if def.Transport != "" {
			desc = fmt.Sprintf("%s (%s transport)", def.Class, def.Transport)
		}
		allowed, derived := kwargStringList(def.Kwargs, "allowed_tools", "allowedTools")
		entries = append(entries, entry{base: base, desc: desc, allowed: allowed, derived: derived})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].base != entries[j].base {
			return entries[i].base < entries[j].base
		}
		return entries[i].desc < entries[j].desc
	})
	aliases := newAliasSet()
	out := make([]MCPServerEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, MCPServerEntry{
			Alias:               aliases.claim(e.base),
			Description:         e.desc,
			AllowedTools:        e.allowed,
			AllowedToolsDerived: e.derived,
			External:            e.external,
		})
	}
	return out
}

// buildLocalAgents maps markdown subagents (which ride along on any selected
// agent) and the agent's handoff targets to local_agents entries. Subagents
// come first (their .md file IS a manifest-shaped source); handoff targets
// point at source code, which is not a manifest, so they carry a review
// marker.
func buildLocalAgents(agent models.AgentDef, result models.ScanResult) []LocalAgent {
	type entry struct {
		base   string
		source string
		desc   string
		review bool
	}
	var subs []entry
	for _, s := range result.Subagents {
		subs = append(subs, entry{base: s.Name, source: s.FilePath, desc: s.Description})
	}
	sort.Slice(subs, func(i, j int) bool {
		if subs[i].base != subs[j].base {
			return subs[i].base < subs[j].base
		}
		return subs[i].source < subs[j].source
	})
	var handoffs []entry
	for _, ref := range agent.HandoffRefs {
		if ref.Resolved != nil {
			t := ref.Resolved
			handoffs = append(handoffs, entry{base: displayName(*t), source: t.FilePath, review: true})
			continue
		}
		// Unresolved handoff: the symbol is all we have. Source must be
		// non-empty per the schema; the review marker says it needs a human.
		handoffs = append(handoffs, entry{base: ref.Name, source: ref.Name, review: true})
	}
	sort.Slice(handoffs, func(i, j int) bool {
		if handoffs[i].base != handoffs[j].base {
			return handoffs[i].base < handoffs[j].base
		}
		return handoffs[i].source < handoffs[j].source
	})

	aliases := newAliasSet()
	out := make([]LocalAgent, 0, len(subs)+len(handoffs))
	for _, e := range append(subs, handoffs...) {
		out = append(out, LocalAgent{
			Alias:       aliases.claim(e.base),
			SourceType:  "file",
			Source:      e.source,
			Description: e.desc,
			Review:      e.review,
		})
	}
	return out
}

// instructionKwargs are the system-prompt kwarg spellings across SDKs, in
// preference order: OpenAI Agents (instructions), Google ADK (instruction),
// Claude AgentDefinition (prompt), plus the snake/camel system-prompt forms.
var instructionKwargs = []string{"instructions", "instruction", "prompt", "system_prompt", "systemPrompt"}

// agentDescription derives metadata.description: the description kwarg where
// captured, else the first line of the instructions/prompt literal.
func agentDescription(agent models.AgentDef) (string, bool) {
	if d, ok := kwargStringFirst(agent.Kwargs, "description"); ok {
		return d, true
	}
	if instr, ok := kwargStringFirst(agent.Kwargs, instructionKwargs...); ok {
		line := instr
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line != "" {
			return line, true
		}
	}
	return "", false
}

// kwargStringFirst returns the first present string-literal kwarg among keys,
// unquoted and whitespace-trimmed.
func kwargStringFirst(kwargs *models.KwargTree, keys ...string) (string, bool) {
	if kwargs == nil {
		return "", false
	}
	for _, key := range keys {
		c := kwargs.Children[key]
		if c == nil || c.Value == nil || c.Value.Kind != models.ExprLiteralString {
			continue
		}
		v := strings.TrimSpace(strings.Trim(c.Value.Text, "\"'`"))
		if v != "" {
			return v, true
		}
	}
	return "", false
}

// kwargStringList returns the elements of the first present list-literal
// kwarg among keys whose elements are all string literals. A list with any
// non-literal element is not derivable.
func kwargStringList(kwargs *models.KwargTree, keys ...string) ([]string, bool) {
	if kwargs == nil {
		return nil, false
	}
	for _, key := range keys {
		c := kwargs.Children[key]
		if c == nil || c.Value == nil || c.Value.Kind != models.ExprList {
			continue
		}
		out := make([]string, 0, len(c.Value.List))
		ok := true
		for _, e := range c.Value.List {
			if e.Kind != models.ExprLiteralString {
				ok = false
				break
			}
			out = append(out, strings.Trim(e.Text, "\"'`"))
		}
		if ok {
			return out, true
		}
	}
	return nil, false
}

// slugID derives metadata.id from a name, deterministic and conforming to
// the schema pattern ^[a-z0-9][a-z0-9_\-]*$: lowercase; characters outside
// [a-z0-9_-] become '-'; runs of '-' collapse; leading non-alphanumerics and
// trailing dashes are trimmed. An empty result falls back to "agent".
func slugID(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s := b.String()
	s = strings.TrimLeft(s, "-_")
	s = strings.TrimRight(s, "-")
	if s == "" {
		return "agent"
	}
	return s
}

// aliasSet sanitizes names to the schema's alias grammar
// (^[a-zA-Z_][a-zA-Z0-9_]*$) and dedupes collisions with numeric suffixes in
// claim order (callers claim in sorted order, so suffixes are deterministic).
type aliasSet struct {
	taken map[string]bool
}

func newAliasSet() *aliasSet { return &aliasSet{taken: map[string]bool{}} }

func (s *aliasSet) claim(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	base := b.String()
	if base == "" {
		base = "_"
	}
	if c := base[0]; c >= '0' && c <= '9' {
		base = "_" + base
	}
	alias := base
	for n := 2; s.taken[alias]; n++ {
		alias = fmt.Sprintf("%s_%d", base, n)
	}
	s.taken[alias] = true
	return alias
}

// dedupeSorted removes adjacent duplicates from a sorted slice.
func dedupeSorted(in []string) []string {
	out := in[:0]
	for i, v := range in {
		if i == 0 || v != in[i-1] {
			out = append(out, v)
		}
	}
	return out
}
