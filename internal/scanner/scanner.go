// Package scanner is the orchestration layer. It wires
// ingestion → analysis → review into one Run() call.
//
// Why split this out from cmd/trustabl: the CLI is one entry point. A future
// HTTP server (architecture §1, Public API) or a unit test calls the same
// Run() and treats it as a pure function over a Config.
package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/ingestion"
	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/progress"
	"github.com/trustabl/trustabl/internal/rules"
)

// Config configures one scan.
type Config struct {
	Target     string                    // local path or GitHub URL
	Categories []models.DetectorCategory // empty means all categories

	// RulesFS is the filesystem the rule packs are loaded from, resolved by
	// the caller (cmd/trustabl) via the rulesource package. Required.
	RulesFS fs.FS
	// Rules provenance, recorded into ScanResult and folded into ScanID.
	RulesSource    string
	RulesVersion   string
	RulesFromCache bool
	// RulesSchemaVersion is the resolved pack manifest's schema_version, and
	// RulesSchemaNewer is true when it exceeds this build's support. Surfaced on
	// ScanResult and used by the CLI's "rules newer than this build" warning.
	RulesSchemaVersion int
	RulesSchemaNewer   bool

	// Progress receives real-time phase events. Nil means no progress output.
	Progress progress.Reporter

	// Log receives verbose/debug diagnostics on stderr. Nil disables them (a nil
	// *logx.Logger is a safe no-op). It is independent of Progress: Progress
	// renders the phase UI, Log narrates detail at -v/--debug. Both are
	// stderr-only and never feed ScanResult, so neither affects determinism.
	Log *logx.Logger

	// Ctx, when non-nil, cancels the scan. The CLI ties this to its interrupt
	// handler so a Ctrl-C aborts the run at the next phase boundary and lets the
	// deferred temp-dir cleanup fire before the process exits. Nil means an
	// uncancellable scan (context.Background()).
	Ctx context.Context
}

// Run executes the full pipeline. The returned ScanResult is what gets
// JSON-serialized for CI output and what the Renderer prints for humans.
func Run(cfg Config) (models.ScanResult, error) {
	rep := cfg.Progress
	if rep == nil {
		rep = progress.NewNop()
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	log := cfg.Log // a nil *logx.Logger is a safe no-op
	defer log.Timer("scan total")()

	// Step 0: resolve the target. For a remote target this shallow-clones to a
	// temp dir — potentially the longest single wait of the whole scan, and the
	// one step with no files to tick — so report it as its own spinner phase.
	// Local targets resolve instantly and get no phase.
	remote := ingestion.IsRemote(cfg.Target)
	if remote {
		// Name the repo in the live label; the plumbing fetch then drives an
		// accurate "receiving objects N/M" bar under this phase (rep satisfies
		// ingestion.CloneProgress).
		rep.StartPhase("clone", "Cloning "+cfg.Target)
	}
	var prog ingestion.CloneProgress
	if remote {
		prog = rep
	}
	src, err := ingestion.Resolve(ctx, cfg.Target, prog)
	if err != nil {
		if remote {
			rep.Fatal(err)
		}
		return models.ScanResult{}, fmt.Errorf("ingest: %w", err)
	}
	defer src.Cleanup()
	if remote {
		rep.EndPhase(cfg.Target)
		log.Verbosef("clone: %s", cfg.Target)
		log.Debugf("clone: checked out to %s", src.RootPath)
	}
	// Cancellation checkpoint. The expensive phases below (recon walk, per-language
	// AST, analysis) have no internal cancellation, but checking here — right after
	// the clone, the one step that creates the temp dir src.Cleanup removes — means
	// an interrupt that lands during or just after the clone returns promptly and
	// runs the deferred cleanup instead of orphaning a trustabl-clone-* dir.
	if err := ctx.Err(); err != nil {
		return models.ScanResult{}, err
	}

	repoLabel := src.RemoteURL
	if repoLabel == "" {
		repoLabel = src.RootPath
	}

	// idLabel is the *stable* identity component folded into ScanID. For a
	// remote scan it is the canonical RemoteURL; for a local scan it is the
	// target's basename, NOT its absolute path — so the same repo content
	// checked out at two different paths (or cloned into a fresh trustabl-clone
	// temp dir) yields the same ScanID. The absolute path stays in the
	// display-only Repo field. Folding the mount point would make the ID
	// machine-dependent, breaking the "same inputs -> same ScanID" contract.
	idLabel := src.RemoteURL
	if idLabel == "" {
		idLabel = filepath.Base(src.RootPath)
	}

	// Step 1: recon (cheap, no AST)
	rep.StartPhase("recon", "Recon")
	reconStop := log.Timer("recon")
	profile, err := ingestion.Recon(src, func(path string) { rep.Advance(path) })
	if err != nil {
		rep.Fatal(err)
		return models.ScanResult{}, fmt.Errorf("recon: %w", err)
	}
	rep.EndPhase(fmt.Sprintf("%d files · %s", len(profile.Manifest.PythonFiles), languagesLabel(profile.Languages)))
	reconStop()
	log.Verbosef("recon: languages %s · %d SDK deps declared", languagesLabel(profile.Languages), len(profile.SDKDeps))
	if log.Enabled(logx.LevelDebug) {
		m := profile.Manifest
		log.Debugf("recon: files py=%d ts=%d js=%d yaml=%d json=%d md=%d",
			len(m.PythonFiles), len(m.TypeScriptFiles), len(m.JavaScriptFiles), len(m.YAMLFiles), len(m.JSONFiles), len(m.MarkdownFiles))
		for _, d := range profile.SDKDeps {
			log.Debugf("recon: dep %s (source=%s confidence=%.2f)", d.Name, d.Source, d.Confidence)
		}
	}

	// Step 2: inventory (per-language AST; Python only for now)
	rep.StartPhase("inventory", "Inventory")
	inventoryStop := log.Timer("inventory")
	rep.SetTotal(len(profile.Manifest.PythonFiles) + len(profile.Manifest.TypeScriptFiles) + len(profile.Manifest.JavaScriptFiles))
	tools, parsed, pySkipped, err := analysis.DiscoverTools(ctx, profile.Manifest, func(path string) {
		rep.Advance(path)
	})
	if err != nil {
		rep.Fatal(err)
		return models.ScanResult{}, fmt.Errorf("discover: %w", err)
	}
	agents := analysis.DiscoverAgents(parsed)
	agents = append(agents, analysis.DiscoverADKAgents(parsed)...)
	agents = append(agents, analysis.DiscoverLangChainAgents(parsed)...)
	agents = append(agents, analysis.DiscoverCrewAIAgents(parsed)...)
	agents = append(agents, analysis.DiscoverAutoGenAgents(parsed)...)
	agents = append(agents, analysis.DiscoverPydanticAIAgents(parsed)...)
	tools = append(tools, analysis.DiscoverADKTools(parsed)...)
	tools = append(tools, analysis.DiscoverLangChainTools(parsed)...)
	tools = append(tools, analysis.DiscoverAutoGenTools(parsed)...)
	tools = append(tools, analysis.DiscoverPydanticAITools(parsed)...)
	guardrails := analysis.DiscoverGuardrails(parsed)
	sessions := analysis.DiscoverSessions(parsed)

	// TS block: parse TypeScript AND JavaScript files, then run TS-family
	// discovery (Claude SDK + OpenAI Agents + Google ADK + LangChain + Vercel +
	// MCP). JavaScript shares the tree-sitter grammar (the tsx parser parses
	// plain JS) and every discovery/predicate path, so .js/.jsx/.mjs/.cjs flow
	// through the same passes; discovery stamps them LanguageTypeScript and
	// retagJavaScriptDefs (after ResolveEdges, below) corrects them to
	// LanguageJavaScript for honest output. Both ES-module imports and CommonJS
	// require() bindings are recognized (astutil.TSImportAliases handles both).
	tsAndJSPaths := make([]string, 0, len(profile.Manifest.TypeScriptFiles)+len(profile.Manifest.JavaScriptFiles))
	tsAndJSPaths = append(tsAndJSPaths, profile.Manifest.TypeScriptFiles...)
	tsAndJSPaths = append(tsAndJSPaths, profile.Manifest.JavaScriptFiles...)
	tsFiles, tsSkipped := parseTSFiles(ctx, tsAndJSPaths, profile.Manifest.RepoRoot, func(path string) {
		rep.Advance(path)
	})

	// Release every parsed tree's C-heap memory once the whole scan completes.
	// Trees are consumed by the discovery/edge-resolution/analysis steps below
	// and the returned ScanResult retains none of them (it carries only
	// extracted, JSON-serializable data), so closing at Run's exit is safe and
	// bounds peak memory on large repos. parsed and tsFiles are disjoint, so no
	// tree is closed twice.
	defer func() {
		for _, pf := range parsed {
			if pf.Tree != nil {
				pf.Tree.Close()
			}
		}
		for _, pf := range tsFiles {
			if pf.Tree != nil {
				pf.Tree.Close()
			}
		}
	}()
	// Claude TS
	tools = append(tools, analysis.DiscoverTSTools(tsFiles, nil)...)
	agents = append(agents, analysis.DiscoverTSAgents(tsFiles, nil)...)
	mcpServers := analysis.DiscoverTSMCPServers(tsFiles, nil)
	// OpenAI TS
	tools = append(tools, analysis.DiscoverTSOpenAITools(tsFiles, nil)...)
	agents = append(agents, analysis.DiscoverTSOpenAIAgents(tsFiles, nil)...)
	mcpServers = append(mcpServers, analysis.DiscoverTSOpenAIMCPServers(tsFiles, nil)...)
	guardrails = append(guardrails, analysis.DiscoverTSOpenAIGuardrails(tsFiles, nil)...)
	sessions = append(sessions, analysis.DiscoverTSOpenAISessions(tsFiles, nil)...)
	// Google ADK TS
	tools = append(tools, analysis.DiscoverTSADKTools(tsFiles, nil)...)
	agents = append(agents, analysis.DiscoverTSADKAgents(tsFiles, nil)...)
	// LangChain / LangGraph TS
	tools = append(tools, analysis.DiscoverTSLangChainTools(tsFiles, nil)...)
	agents = append(agents, analysis.DiscoverTSLangChainAgents(tsFiles, nil)...)
	// Vercel AI SDK TS — tool()/dynamicTool() and generateText/streamText/
	// generateObject/streamObject + ToolLoopAgent agents. Emits KindVercelAITool,
	// so deriveSDKsDetected stamps SDKVercelAI and the vercel_ai pack loads.
	tools = append(tools, analysis.DiscoverTSVercelTools(tsFiles, nil)...)
	agents = append(agents, analysis.DiscoverTSVercelAgents(tsFiles, nil)...)
	// MCP server authoring (@modelcontextprotocol/sdk) TS — registerTool/tool.
	// Emits KindMCPTool, so deriveSDKsDetected stamps SDKMCP and the mcp pack
	// loads automatically (same path as the Python MCP tools).
	tools = append(tools, analysis.DiscoverTSMCPProper(tsFiles, nil)...)

	// Canonicalize tool/agent order before anything reads them. Discovery appends
	// in walk order (Python -> ADK -> TS), which is deterministic only by accident
	// of single-threaded lexical traversal — the same assumption the rest of the
	// pipeline refuses to rely on (scanID re-sorts every file list; HostedTools,
	// MCPServers, Subagents, Skills are all explicitly sorted). These two slices
	// flow verbatim into ScanResult, so an unsorted order is a latent break of the
	// byte-stable-report contract. Sort by (FilePath, Line, Name). This MUST run
	// before ResolveEdges below: ResolveEdges stores *ToolDef/*AgentDef pointers
	// into these backing arrays, so reordering afterward would dangle them.
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].FilePath != tools[j].FilePath {
			return tools[i].FilePath < tools[j].FilePath
		}
		if tools[i].Line != tools[j].Line {
			return tools[i].Line < tools[j].Line
		}
		return tools[i].Name < tools[j].Name
	})
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].FilePath != agents[j].FilePath {
			return agents[i].FilePath < agents[j].FilePath
		}
		if agents[i].Line != agents[j].Line {
			return agents[i].Line < agents[j].Line
		}
		return agents[i].Name < agents[j].Name
	})

	inventory := models.RepoInventory{
		Tools:      tools,
		Agents:     agents,
		Guardrails: guardrails,
		Sessions:   sessions,
		MCPServers: mcpServers,
		Manifest:   profile.Manifest,
		// SDKsDetected is set once below, after subagent discovery, since
		// markdown subagent presence contributes to it.
		HasShellInvocations: deriveHasShellInvocations(tools),
		UsesDefaultTracing:  computeUsesDefaultTracing(parsed),
	}
	// Combine the Python and TS parsed files into one explicitly-allocated slice.
	// Using append(parsed, tsFiles...) inline would be unsafe if DiscoverTools ever
	// returned a slice with spare capacity: two separate appends from the same base
	// would alias the same backing array and could be mutated out from under each
	// other. Allocate once, reuse for both ResolveEdges and registry.Run.
	allParsed := make([]analysis.ParsedFile, 0, len(parsed)+len(tsFiles))
	allParsed = append(allParsed, parsed...)
	allParsed = append(allParsed, tsFiles...)
	analysis.ResolveEdges(&inventory, allParsed)
	inventory.Subagents = analysis.DiscoverSubagents(profile.Manifest)
	inventory.Skills = analysis.DiscoverSkills(profile.Manifest)
	inventory.SlashCommands = analysis.DiscoverSlashCommands(profile.Manifest)
	inventory.PluginManifests = analysis.DiscoverPlugins(profile.Manifest)
	inventory.ClaudeSettings = analysis.DiscoverClaudeSettings(profile.Manifest)
	inventory.ClaudeAgentOptions = analysis.DiscoverClaudeAgentOptions(parsed)
	// Markdown subagents are an independent Claude Agent SDK signal: a repo can
	// ship .claude/agents/*.md (or a flat collection) with no Claude SDK code.
	// Fold them into SDKsDetected so LoadFor loads the claude_sdk pack (CSDK-110).
	inventory.SDKsDetected = deriveSDKsDetected(tools, agents, inventory.Subagents, inventory.ClaudeSettings, inventory.ClaudeAgentOptions)
	rep.EndPhase(fmt.Sprintf("%d tools · %d agents", len(tools), len(agents)))
	inventoryStop()
	log.Verbosef("inventory: %d tools · %d agents · %d guardrails · %d sessions · %d subagents · %d mcp servers · %d skills",
		len(tools), len(agents), len(guardrails), len(sessions), len(inventory.Subagents), len(mcpServers), len(inventory.Skills))
	log.Verbosef("inventory: SDKs detected: %s", sdkListLabel(inventory.SDKsDetected))
	if log.Enabled(logx.LevelDebug) {
		logDiscoveredEntities(log, tools, agents)
	}

	// JavaScript files were parsed by the shared TS-family grammar and ran
	// through discovery + ResolveEdges tagged LanguageTypeScript (so edge
	// resolution, which special-cases TypeScript, treats them correctly). Re-tag
	// the JS-sourced tools/agents/MCP servers to LanguageJavaScript now — after
	// edges are resolved, before analysis — so the inventory honestly reports the
	// source language while the analysis gate/predicates (TS/JS-family-aware via
	// models.IsTSOrJS) still audit them with the TypeScript rule packs.
	retagJavaScriptDefs(&inventory)

	// Step 3: policy selection
	if cfg.RulesFS == nil {
		return models.ScanResult{}, fmt.Errorf("scan: no rules filesystem provided")
	}
	registry, rulesSkipped, err := rules.LoadFor(cfg.RulesFS, inventory.SDKsDetected)
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("load rules: %w", err)
	}
	log.Verbosef("policy: loaded %d detectors for SDKs: %s", registry.Count(), sdkListLabel(inventory.SDKsDetected))
	if un := unauditedSDKs(inventory.SDKsDetected); len(un) > 0 {
		log.Verbosef("policy: unaudited SDKs (no pack shipped): %s", sdkListLabel(un))
	}
	if len(cfg.Categories) > 0 {
		registry = registry.Subset(cfg.Categories...)
		log.Verbosef("policy: --detectors filter → %d detectors for categories: %s",
			registry.Count(), categoryListLabel(cfg.Categories))
	}
	metaFindings := SelectAndEmitMETA(profile, inventory)
	metaFindings = append(metaFindings,
		EmitCoverageMETA(registry.ApplicableCategories(profile, inventory), inventory)...)
	// Honest-coverage signal for forward-incompatible rules the lenient loader
	// dropped (scope/applies_to/predicate this build predates). META-005 fires
	// only when something was actually skipped, so an in-sync pack adds nothing.
	metaFindings = append(metaFindings, EmitSkippedRulesMETA(rulesSkipped)...)

	// Step 4: analysis
	rep.StartPhase("analysis", "Analysis")
	analysisStop := log.Timer("analysis")
	rep.SetTotal(len(inventory.Tools) + len(inventory.Agents))
	ruleFindings := registry.Run(profile, inventory, allParsed, func(label string) {
		rep.Advance(label)
	})
	findings := append(metaFindings, ruleFindings...)
	rep.EndPhase(fmt.Sprintf("%d findings", len(findings)))
	analysisStop()
	log.Verbosef("analysis: %d findings (%d META + %d rule) · %s",
		len(findings), len(metaFindings), len(ruleFindings), severitySummary(findings))
	if log.Enabled(logx.LevelDebug) {
		logFindingsDetail(log, findings)
	}

	// Step 5: scoring
	surfaces, overall := analysis.Score(tools, inventory.Agents, inventory.Subagents, findings)
	projected := analysis.Project(tools, inventory.Agents, inventory.Subagents, findings)

	// Coverage: how many AST-targeted source files we actually parsed vs. how
	// many we attempted. Discovery skips files it cannot read or parse (one bad
	// file must not abort the scan), but that skip has to be visible — a scan
	// that silently dropped half the repo must not look like a clean result.
	// `parsed` holds the successfully parsed Python files; `tsFiles` the
	// successfully parsed TypeScript AND JavaScript files (the JS family shares
	// the tsx grammar). All three inventoried source languages count as attempted.
	filesParsed := len(parsed) + len(tsFiles)
	filesAttempted := len(profile.Manifest.PythonFiles) +
		len(profile.Manifest.TypeScriptFiles) + len(profile.Manifest.JavaScriptFiles)
	// Name the skipped files (Python + TS/JS), not just count them, so the report
	// can say which inputs went unanalyzed. Sorted+deduped for determinism.
	skippedFiles := append(append([]string{}, pySkipped...), tsSkipped...)
	skippedFiles = sortedUnique(skippedFiles)
	coverage := models.Coverage{
		FilesParsed:  filesParsed,
		FilesSkipped: filesAttempted - filesParsed,
		SkippedFiles: skippedFiles,
	}

	return models.ScanResult{
		ScanID:              scanID(idLabel, profile.Manifest, cfg.RulesVersion, rules.SupportedSchemaVersion),
		Repo:                repoLabel,
		Languages:           profile.Languages,
		SDKs:                inventory.SDKsDetected,
		HasShellInvocations: inventory.HasShellInvocations,
		Manifest:            profile.Manifest,
		Tools:               tools,
		Agents:              inventory.Agents,
		HostedTools:         inventory.HostedTools,
		MCPServers:          inventory.MCPServers,
		Subagents:           inventory.Subagents,
		Skills:              inventory.Skills,
		SlashCommands:       inventory.SlashCommands,
		PluginManifests:     inventory.PluginManifests,
		ClaudeSettings:      inventory.ClaudeSettings,
		Findings:            findings,
		Surfaces:            surfaces,
		OverallScore:        overall,
		ProjectedScores:     projected,
		RulesSource:         cfg.RulesSource,
		RulesVersion:        cfg.RulesVersion,
		RulesFromCache:      cfg.RulesFromCache,
		RulesSchemaVersion:  cfg.RulesSchemaVersion,
		RulesSchemaNewer:    cfg.RulesSchemaNewer,
		RulesSkipped:        sortedUnique(rulesSkipped),
		Coverage:            coverage,
	}, nil
}

// deriveSDKsDetected scans the inventory for tool/agent kinds that imply
// a specific SDK is in use.
//
// KindShellInvocation is intentionally NOT mapped here. There is no SDK
// called "openshell" — it is a risk-surface label for Python functions
// that shell out, carried on RepoInventory.HasShellInvocations.
func deriveSDKsDetected(tools []models.ToolDef, agents []models.AgentDef, subagents []models.SubagentDef, claudeSettings []models.ClaudeSettings, claudeAgentOptions []models.ClaudeAgentOptionsDef) []models.SDK {
	seen := make(map[models.SDK]bool)
	for _, t := range tools {
		switch t.Kind {
		case models.KindClaudeSDKTool:
			seen[models.SDKClaudeAgentSDK] = true
		case models.KindOpenAITool:
			seen[models.SDKOpenAIAgents] = true
		case models.KindMCPTool:
			seen[models.SDKMCP] = true
		case models.KindADKFunctionTool:
			seen[models.SDKGoogleADK] = true
		case models.KindLangChainTool:
			seen[models.SDKLangChain] = true
		case models.KindCrewAITool:
			seen[models.SDKCrewAI] = true
		case models.KindPydanticAITool:
			seen[models.SDKPydanticAI] = true
		case models.KindVercelAITool:
			seen[models.SDKVercelAI] = true
		case models.KindAutoGenTool:
			seen[models.SDKAutoGen] = true
		}
	}
	for _, a := range agents {
		if a.SDK != "" {
			seen[a.SDK] = true
		}
	}
	// Markdown subagents (.claude/agents/*.md or a flat collection) are Claude
	// Code configuration — a Claude Agent SDK surface even when no SDK code is
	// present. Their presence is what makes the claude_sdk pack load.
	if len(subagents) > 0 {
		seen[models.SDKClaudeAgentSDK] = true
	}
	// A .claude/settings.json (or settings.local.json) is likewise a Claude
	// Agent SDK surface on its own — it configures permission modes, hooks, and
	// sandboxing for Claude even in a repo with no SDK code. Its presence loads
	// the claude_sdk pack so repo-scope settings rules (e.g. CSDK-201's
	// defaultMode: bypassPermissions check) can fire.
	if len(claudeSettings) > 0 {
		seen[models.SDKClaudeAgentSDK] = true
	}
	// A ClaudeAgentOptions(...) construction is claude-agent-sdk code — it
	// configures a session (permission_mode, allowed_tools, etc.) and is the
	// likeliest place an app sets a permission bypass. Its presence marks the
	// repo Claude even when no @tool/AgentDefinition/subagent/settings exists,
	// so the claude_sdk pack loads and CSDK-202 can fire.
	if len(claudeAgentOptions) > 0 {
		seen[models.SDKClaudeAgentSDK] = true
	}
	var out []models.SDK
	for s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// deriveHasShellInvocations is true when any discovered tool was a
// KindShellInvocation (a Python function whose body calls subprocess.*,
// os.system, or os.popen). This is the "openshell" risk surface — see
// the comment on deriveSDKsDetected for why it isn't an SDK.
func deriveHasShellInvocations(tools []models.ToolDef) bool {
	for _, t := range tools {
		if t.Kind == models.KindShellInvocation {
			return true
		}
	}
	return false
}

func computeUsesDefaultTracing(parsed []analysis.ParsedFile) bool {
	for _, pf := range parsed {
		if pf.Tree == nil {
			continue
		}
		if disablesDefaultTracing(pf) {
			return false
		}
	}
	return true
}

// tracingProcessorFuncs are the OpenAI Agents SDK calls that replace or augment
// the default trace processor. Either one means the repo is not on the pure
// default-tracing path.
var tracingProcessorFuncs = map[string]bool{
	"add_trace_processor":  true,
	"set_trace_processors": true,
}

// disablesDefaultTracing reports whether a parsed file installs a custom trace
// processor or references the tracing-disable env var. It inspects typed AST
// nodes — call-function names and string literals — rather than substring-
// scanning raw source, so a mention inside a comment or an unrelated identifier
// no longer produces a false signal (the inventory-owns-AST-facts contract).
func disablesDefaultTracing(pf analysis.ParsedFile) bool {
	found := false
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if found {
			return false
		}
		switch n.Type() {
		case "call":
			fn := n.ChildByFieldName("function")
			if fn == nil {
				return true
			}
			name := astutil.NodeText(fn, pf.Source)
			// For an attribute callee (e.g. trace.add_trace_processor) match the
			// final dotted component; for a bare identifier match it directly.
			if i := strings.LastIndex(name, "."); i >= 0 {
				name = name[i+1:]
			}
			if tracingProcessorFuncs[name] {
				found = true
				return false
			}
		case "string":
			// The disable switch is the env var name as a string literal, e.g.
			// os.environ["OPENAI_AGENTS_DISABLE_TRACING"]. Restricting the match
			// to string nodes excludes comments and unrelated code.
			if strings.Contains(astutil.NodeText(n, pf.Source), "OPENAI_AGENTS_DISABLE_TRACING") {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// languagesLabel renders a stable, comma-separated language list for progress.
// sortedUnique returns the input sorted with duplicates removed. Used to make
// the Coverage.SkippedFiles list deterministic.
func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	cp := append([]string{}, in...)
	sort.Strings(cp)
	out := cp[:0]
	for i, s := range cp {
		if i == 0 || s != cp[i-1] {
			out = append(out, s)
		}
	}
	return out
}

func languagesLabel(langs []models.Language) string {
	if len(langs) == 0 {
		return "no known languages"
	}
	parts := make([]string, len(langs))
	for i, l := range langs {
		parts[i] = string(l)
	}
	return strings.Join(parts, ", ")
}

// parseTSFiles reads and parses each path in paths (relative to root) using
// the appropriate tree-sitter grammar (typescript vs tsx). Files that cannot
// be read or parsed are silently skipped — one bad file should not abort the
// scan. The optional onFile callback fires once per file attempted (progress
// hook), mirroring the same callback convention used by analysis.DiscoverTools.
func parseTSFiles(ctx context.Context, paths []string, root string, onFile func(string)) ([]analysis.ParsedFile, []string) {
	var out []analysis.ParsedFile
	var skipped []string
	for _, rel := range paths {
		if onFile != nil {
			onFile(rel)
		}
		full := filepath.Join(root, rel)
		body, err := os.ReadFile(full)
		if err != nil {
			skipped = append(skipped, rel) // unreadable — not analyzed
			continue
		}
		// Fresh parser per file: ParseCtxTimeout's cancelable context arms
		// go-tree-sitter's per-parser cancellation flag, so a reused parser could
		// have a prior parse's timeout goroutine set that flag and abort the next
		// file's parse. Isolate by constructing per file (cheap vs. parsing).
		var parser *sitter.Parser
		switch astutil.ParserKindForExtension(rel) {
		case "typescript":
			parser = astutil.NewTSParser()
		case "tsx":
			parser = astutil.NewTSXParser()
		default:
			skipped = append(skipped, rel) // unknown extension — not analyzed
			continue
		}
		tree, err := astutil.ParseCtxTimeout(ctx, parser, body)
		if err != nil {
			skipped = append(skipped, rel) // unparseable — not analyzed
			continue
		}
		out = append(out, analysis.ParsedFile{RelPath: rel, Source: body, Tree: tree})
	}
	return out, skipped
}

// retagJavaScriptDefs corrects the Language of every discovered tool, agent, and
// MCP server sourced from a JavaScript file to LanguageJavaScript. Discovery
// runs JS through the shared TS-family passes and stamps LanguageTypeScript (the
// tsx grammar parses both, and ResolveEdges special-cases TypeScript), so this
// runs AFTER edge resolution and BEFORE analysis: the inventory then honestly
// names the source language, while the analysis gate and predicates (which treat
// TS and JS as one family via models.IsTSOrJS) still audit JS with the
// TypeScript rule packs. GuardrailDef and SessionUse carry no Language field, so
// they need no re-tag.
func retagJavaScriptDefs(inv *models.RepoInventory) {
	for i := range inv.Tools {
		if astutil.IsJavaScriptExtension(inv.Tools[i].FilePath) {
			inv.Tools[i].Language = models.LanguageJavaScript
		}
	}
	for i := range inv.Agents {
		if astutil.IsJavaScriptExtension(inv.Agents[i].FilePath) {
			inv.Agents[i].Language = models.LanguageJavaScript
		}
	}
	for i := range inv.MCPServers {
		if astutil.IsJavaScriptExtension(inv.MCPServers[i].FilePath) {
			inv.MCPServers[i].Language = models.LanguageJavaScript
		}
	}
}

// scanID is derived from a stable identity label (RemoteURL for remote scans,
// the target's basename for local scans — never the absolute mount point), the
// sorted set of inventoried files, and the rules version, so the same inputs
// always produce the same ID regardless of where the repo is checked out.
// Including the rules version means a different rule pack yields a distinct,
// honest ID; folding the engine's supported schema version likewise keeps the
// ID honest when forward-compatible loading makes two builds skip different
// rules from the same pack.
func scanID(idLabel string, manifest models.ScanManifest, rulesVersion string, engineSchema int) string {
	h := sha256.New()
	h.Write([]byte(idLabel))
	// Fold every inventoried file list so the ID is honest about all scanned
	// inputs, not just Python — the engine now does first-class TypeScript /
	// JavaScript discovery and markdown / JSON / YAML config scanning. Each list
	// is sorted independently so OS-walk order does not leak, and each is labeled
	// with a NUL-delimited tag so list membership is preserved in the digest.
	fileLists := []struct {
		label string
		files []string
	}{
		{"py", manifest.PythonFiles},
		{"ts", manifest.TypeScriptFiles},
		{"js", manifest.JavaScriptFiles},
		{"yaml", manifest.YAMLFiles},
		{"json", manifest.JSONFiles},
		{"md", manifest.MarkdownFiles},
	}
	for _, fl := range fileLists {
		files := make([]string, len(fl.files))
		copy(files, fl.files)
		sort.Strings(files)
		h.Write([]byte(fl.label))
		h.Write([]byte{0})
		h.Write([]byte(strings.Join(files, "\n")))
		h.Write([]byte{0})
	}
	h.Write([]byte(rulesVersion))
	// Fold the engine's supported schema version. With forward-compatible
	// loading, two builds supporting different schema versions can skip different
	// (forward-incompatible) rules from the SAME pack and thus emit different
	// findings; folding it keeps the ScanID honest about the effective ruleset,
	// not just the pack SHA.
	h.Write([]byte{0})
	h.Write([]byte(fmt.Sprintf("%d", engineSchema)))
	return "scan_" + hex.EncodeToString(h.Sum(nil)[:8])
}
