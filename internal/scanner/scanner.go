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

	// Progress receives real-time phase events. Nil means no progress output.
	Progress progress.Reporter
}

// Run executes the full pipeline. The returned ScanResult is what gets
// JSON-serialized for CI output and what the Renderer prints for humans.
func Run(cfg Config) (models.ScanResult, error) {
	src, err := ingestion.Resolve(cfg.Target)
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("ingest: %w", err)
	}
	defer src.Cleanup()

	repoLabel := src.RemoteURL
	if repoLabel == "" {
		repoLabel = src.RootPath
	}

	rep := cfg.Progress
	if rep == nil {
		rep = progress.NewNop()
	}

	// Step 1: recon (cheap, no AST)
	rep.StartPhase("recon", "Recon")
	profile, err := ingestion.Recon(src)
	if err != nil {
		rep.Fatal(err)
		return models.ScanResult{}, fmt.Errorf("recon: %w", err)
	}
	rep.EndPhase(fmt.Sprintf("%d files · %s", len(profile.Manifest.PythonFiles), languagesLabel(profile.Languages)))

	// Step 2: inventory (per-language AST; Python only for now)
	rep.StartPhase("inventory", "Inventory")
	rep.SetTotal(len(profile.Manifest.PythonFiles) + len(profile.Manifest.TypeScriptFiles))
	tools, parsed, err := analysis.DiscoverTools(profile.Manifest, func(path string) {
		rep.Advance(path)
	})
	if err != nil {
		rep.Fatal(err)
		return models.ScanResult{}, fmt.Errorf("discover: %w", err)
	}
	agents := analysis.DiscoverAgents(parsed)
	agents = append(agents, analysis.DiscoverADKAgents(parsed)...)
	tools = append(tools, analysis.DiscoverADKTools(parsed)...)
	guardrails := analysis.DiscoverGuardrails(parsed)
	sessions := analysis.DiscoverSessions(parsed)

	// TS block: parse TypeScript files, then run TS-specific discovery
	// (Claude SDK + OpenAI Agents + Google ADK).
	tsFiles := parseTSFiles(profile.Manifest.TypeScriptFiles, profile.Manifest.RepoRoot, func(path string) {
		rep.Advance(path)
	})
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

	inventory := models.RepoInventory{
		Tools:               tools,
		Agents:              agents,
		Guardrails:          guardrails,
		Sessions:            sessions,
		MCPServers:          mcpServers,
		Manifest:            profile.Manifest,
		// SDKsDetected is set once below, after subagent discovery, since
		// markdown subagent presence contributes to it.
		HasShellInvocations: deriveHasShellInvocations(tools),
		UsesDefaultTracing:  computeUsesDefaultTracing(parsed),
	}
	analysis.ResolveEdges(&inventory, append(parsed, tsFiles...))
	inventory.Subagents = analysis.DiscoverSubagents(profile.Manifest)
	inventory.Skills = analysis.DiscoverSkills(profile.Manifest)
	inventory.SlashCommands = analysis.DiscoverSlashCommands(profile.Manifest)
	inventory.PluginManifests = analysis.DiscoverPlugins(profile.Manifest)
	inventory.ClaudeSettings = analysis.DiscoverClaudeSettings(profile.Manifest)
	// Markdown subagents are an independent Claude Agent SDK signal: a repo can
	// ship .claude/agents/*.md (or a flat collection) with no Claude SDK code.
	// Fold them into SDKsDetected so LoadFor loads the claude_sdk pack (CSDK-110).
	inventory.SDKsDetected = deriveSDKsDetected(tools, agents, inventory.Subagents)
	rep.EndPhase(fmt.Sprintf("%d tools · %d agents", len(tools), len(agents)))

	// Step 3: policy selection
	if cfg.RulesFS == nil {
		return models.ScanResult{}, fmt.Errorf("scan: no rules filesystem provided")
	}
	registry, err := rules.LoadFor(cfg.RulesFS, inventory.SDKsDetected)
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("load rules: %w", err)
	}
	if len(cfg.Categories) > 0 {
		registry = registry.Subset(cfg.Categories...)
	}
	metaFindings := SelectAndEmitMETA(profile, inventory)
	metaFindings = append(metaFindings,
		EmitCoverageMETA(registry.ApplicableCategories(profile, inventory), inventory)...)

	// Step 4: analysis
	rep.StartPhase("analysis", "Analysis")
	rep.SetTotal(len(inventory.Tools) + len(inventory.Agents))
	allParsed := append(parsed, tsFiles...)
	ruleFindings := registry.Run(profile, inventory, allParsed, func(label string) {
		rep.Advance(label)
	})
	findings := append(metaFindings, ruleFindings...)
	rep.EndPhase(fmt.Sprintf("%d findings", len(findings)))

	// Step 5: scoring
	readiness, overall := analysis.Score(tools, findings)

	return models.ScanResult{
		ScanID:              scanID(repoLabel, profile.Manifest, cfg.RulesVersion),
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
		Readiness:           readiness,
		OverallScore:        overall,
		RulesSource:         cfg.RulesSource,
		RulesVersion:        cfg.RulesVersion,
		RulesFromCache:      cfg.RulesFromCache,
	}, nil
}

// deriveSDKsDetected scans the inventory for tool/agent kinds that imply
// a specific SDK is in use.
//
// KindShellInvocation is intentionally NOT mapped here. There is no SDK
// called "openshell" — it is a risk-surface label for Python functions
// that shell out, carried on RepoInventory.HasShellInvocations.
func deriveSDKsDetected(tools []models.ToolDef, agents []models.AgentDef, subagents []models.SubagentDef) []models.SDK {
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
		src := string(pf.Source)
		if strings.Contains(src, "add_trace_processor") ||
			strings.Contains(src, "OPENAI_AGENTS_DISABLE_TRACING") {
			return false
		}
	}
	return true
}

// languagesLabel renders a stable, comma-separated language list for progress.
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
func parseTSFiles(paths []string, root string, onFile func(string)) []analysis.ParsedFile {
	tsParser := astutil.NewTSParser()
	tsxParser := astutil.NewTSXParser()
	var out []analysis.ParsedFile
	for _, rel := range paths {
		if onFile != nil {
			onFile(rel)
		}
		full := filepath.Join(root, rel)
		body, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		var parser *sitter.Parser
		switch astutil.ParserKindForExtension(rel) {
		case "typescript":
			parser = tsParser
		case "tsx":
			parser = tsxParser
		default:
			continue
		}
		tree, err := parser.ParseCtx(context.Background(), nil, body)
		if err != nil {
			continue
		}
		out = append(out, analysis.ParsedFile{RelPath: rel, Source: body, Tree: tree})
	}
	return out
}

// scanID is derived from the repo label, the sorted set of Python files, and
// the rules version, so the same inputs always produce the same ID. Including
// the rules version means a different rule pack yields a distinct, honest ID.
func scanID(repoLabel string, manifest models.ScanManifest, rulesVersion string) string {
	h := sha256.New()
	h.Write([]byte(repoLabel))
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
	return "scan_" + hex.EncodeToString(h.Sum(nil)[:8])
}
