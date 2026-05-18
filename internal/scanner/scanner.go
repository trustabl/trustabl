// Package scanner is the orchestration layer. It wires
// ingestion → analysis → generation → review into one Run() call.
//
// Why split this out from cmd/trustabl: the CLI is one entry point. A future
// HTTP server (architecture §1, Public API) or a unit test calls the same
// Run() and treats it as a pure function over a Config.
package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/generation"
	"github.com/trustabl/trustabl/internal/ingestion"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// Config configures one scan. Zero-value is "scan everything, generate everything".
type Config struct {
	Target      string                    // local path or GitHub URL
	Categories  []models.DetectorCategory // empty means all categories
	Version     string                    // injected by the CLI for artifact metadata
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

	// Phase 1: reconnaissance (cheap, no AST)
	profile, err := ingestion.Recon(src)
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("recon: %w", err)
	}

	// Phase 2a: per-language inventory (Python only for now)
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

	// Phase 2b: policy selection
	registry, err := rules.LoadFor(rules.DefaultFS(), inventory.SDKsDetected)
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("load rules: %w", err)
	}
	if len(cfg.Categories) > 0 {
		registry = registry.Subset(cfg.Categories...)
	}
	metaFindings := SelectAndEmitMETA(profile, inventory)

	// Phase 2c: analysis
	ruleFindings := registry.Run(profile, inventory, parsed)
	findings := append(metaFindings, ruleFindings...)

	readiness, overall := analysis.Score(tools, findings)
	artifacts := append(
		generation.GenerateHooks(findings),
		generation.GeneratePolicy(findings, cfg.Version)...,
	)

	return models.ScanResult{
		ScanID:             scanID(repoLabel, profile.Manifest),
		Repo:               repoLabel,
		Manifest:           profile.Manifest,
		Tools:              tools,
		Findings:           findings,
		Readiness:          readiness,
		OverallScore:       overall,
		GeneratedArtifacts: artifacts,
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

// scanID is derived from the repo label and the sorted set of Python files so
// that the same input always produces the same ID. This keeps JSON output
// diff-comparable across identical runs in CI.
func scanID(repoLabel string, manifest models.ScanManifest) string {
	files := make([]string, len(manifest.PythonFiles))
	copy(files, manifest.PythonFiles)
	sort.Strings(files)
	h := sha256.New()
	h.Write([]byte(repoLabel))
	h.Write([]byte(strings.Join(files, "\n")))
	return "scan_" + hex.EncodeToString(h.Sum(nil)[:8])
}
