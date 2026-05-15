// Package scanner is the orchestration layer. It wires
// ingestion → analysis → generation → review into one Run() call.
//
// Why split this out from cmd/karenctl: the CLI is one entry point. A future
// HTTP server (architecture §1, Public API) or a unit test calls the same
// Run() and treats it as a pure function over a Config.
package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/trustabl/karenctl/internal/analysis"
	"github.com/trustabl/karenctl/internal/generation"
	"github.com/trustabl/karenctl/internal/ingestion"
	"github.com/trustabl/karenctl/internal/models"
	"github.com/trustabl/karenctl/internal/rules"
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

	manifest, err := ingestion.Normalize(src)
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("normalize: %w", err)
	}

	tools, parsed, err := analysis.DiscoverTools(manifest)
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("discover: %w", err)
	}

	registry, err := rules.LoadRegistry(rules.DefaultFS())
	if err != nil {
		return models.ScanResult{}, fmt.Errorf("load rules: %w", err)
	}
	if len(cfg.Categories) > 0 {
		registry = registry.Subset(cfg.Categories...)
	}
	findings := registry.Run(tools, parsed)

	readiness, overall := analysis.Score(tools, findings)

	// Generation. We always run both generators — empty findings just produce
	// a defaults-only policy and an empty hook scaffolding, which is the
	// honest output for a clean repo.
	artifacts := append(
		generation.GenerateHooks(findings),
		generation.GeneratePolicy(findings, cfg.Version)...,
	)

	repoLabel := src.RemoteURL
	if repoLabel == "" {
		repoLabel = src.RootPath
	}

	return models.ScanResult{
		ScanID:             scanID(repoLabel, manifest),
		Repo:               repoLabel,
		Manifest:           manifest,
		Tools:              tools,
		Findings:           findings,
		Readiness:          readiness,
		OverallScore:       overall,
		GeneratedArtifacts: artifacts,
	}, nil
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
