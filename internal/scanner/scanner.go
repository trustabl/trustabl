// Package scanner is the orchestration layer. It wires
// ingestion → analysis → review into one Run() call.
//
// Why split this out from cmd/trustabl: the CLI is one entry point. A future
// HTTP server (architecture §1, Public API) or a unit test calls the same
// Run() and treats it as a pure function over a Config.
package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/ingestion"
	"github.com/trustabl/trustabl/internal/models"
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

	// Step 1: recon (cheap, no AST)
	profile, err := ingestion.Recon(src)
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("recon: %w", err)
	}

	// Step 2: inventory (per-language AST; Python only for now)
	tools, parsed, err := analysis.DiscoverTools(profile.Manifest)
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("discover: %w", err)
	}
	agents := analysis.DiscoverAgents(parsed)
	guardrails := analysis.DiscoverGuardrails(parsed)
	sessions := analysis.DiscoverSessions(parsed)

	inventory := models.RepoInventory{
		Tools:              tools,
		Agents:             agents,
		Guardrails:         guardrails,
		Sessions:           sessions,
		Manifest:           profile.Manifest,
		SDKsDetected:       deriveSDKsDetected(tools, agents),
		UsesDefaultTracing: computeUsesDefaultTracing(parsed),
	}
	analysis.ResolveEdges(&inventory, parsed)
	inventory.Subagents = analysis.DiscoverSubagents(profile.Manifest)
	inventory.ClaudeSettings = analysis.DiscoverClaudeSettings(profile.Manifest)

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
	ruleFindings := registry.Run(profile, inventory, parsed)
	findings := append(metaFindings, ruleFindings...)

	// Step 5: scoring
	readiness, overall := analysis.Score(tools, findings)

	return models.ScanResult{
		ScanID:         scanID(repoLabel, profile.Manifest, cfg.RulesVersion),
		Repo:           repoLabel,
		Languages:      profile.Languages,
		SDKs:           inventory.SDKsDetected,
		Manifest:       profile.Manifest,
		Tools:          tools,
		Agents:         inventory.Agents,
		HostedTools:    inventory.HostedTools,
		MCPServers:     inventory.MCPServers,
		Subagents:      inventory.Subagents,
		ClaudeSettings: inventory.ClaudeSettings,
		Findings:       findings,
		Readiness:      readiness,
		OverallScore:   overall,
		RulesSource:    cfg.RulesSource,
		RulesVersion:   cfg.RulesVersion,
		RulesFromCache: cfg.RulesFromCache,
	}, nil
}

// deriveSDKsDetected scans the inventory for tool/agent kinds that imply
// a specific SDK is in use.
func deriveSDKsDetected(tools []models.ToolDef, agents []models.AgentDef) []models.SDK {
	seen := make(map[models.SDK]bool)
	for _, t := range tools {
		switch t.Kind {
		case models.KindClaudeSDKTool:
			seen[models.SDKClaudeAgentSDK] = true
		case models.KindOpenAITool:
			seen[models.SDKOpenAIAgents] = true
		case models.KindMCPTool:
			seen[models.SDKMCP] = true
		case models.KindShellInvocation:
			seen[models.SDKOpenShell] = true
		}
	}
	for _, a := range agents {
		if a.SDK != "" {
			seen[a.SDK] = true
		}
	}
	var out []models.SDK
	for s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
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

// scanID is derived from the repo label, the sorted set of Python files, and
// the rules version, so the same inputs always produce the same ID. Including
// the rules version means a different rule pack yields a distinct, honest ID.
func scanID(repoLabel string, manifest models.ScanManifest, rulesVersion string) string {
	files := make([]string, len(manifest.PythonFiles))
	copy(files, manifest.PythonFiles)
	sort.Strings(files)
	h := sha256.New()
	h.Write([]byte(repoLabel))
	h.Write([]byte(strings.Join(files, "\n")))
	h.Write([]byte(rulesVersion))
	return "scan_" + hex.EncodeToString(h.Sum(nil)[:8])
}
