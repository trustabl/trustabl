package sarif

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

var updateGolden = flag.Bool("update", false, "regenerate the golden SARIF file")

// fixtureResult builds a stable, hand-constructed ScanResult that exercises
// every Trustabl-to-SARIF mapping path: a regular tool finding (with
// location + ToolName), a repo-scoped rule finding (no location), META-002
// (manifest-located result), META-001 (notification), and remote-VCS
// provenance.
func fixtureResult() models.ScanResult {
	return models.ScanResult{
		ScanID: "scan_fixture0001",
		Repo:   "https://github.com/example/agent-repo",
		Manifest: models.ScanManifest{
			RepoRoot:  "/tmp/trustabl-clone-fixture",
			IsRemote:  true,
			RemoteURL: "https://github.com/example/agent-repo",
		},
		RulesSource:    "https://github.com/trustabl/trustabl-rules",
		RulesVersion:   "cb28dfbfixture",
		RulesFromCache: false,
		Findings: []models.Finding{
			{
				RuleID: "OAI-005", Category: models.CategoryOpenAISDK,
				Severity: models.SeverityHigh,
				ToolName: "fetch_url", FilePath: "agents/web.py", Line: 42,
				Title:        "Network call has no timeout",
				Explanation:  "An HTTP call without timeout can hang the agent run.",
				SuggestedFix: "Pass timeout=5 to the request.",
				Confidence:   0.85,
			},
			{
				RuleID:      "OAI-201",
				Severity:    models.SeverityMedium,
				Title:       "OpenAI Agents SDK present but no custom tracing",
				Explanation: "Tracing runs with the default processor only.",
				Confidence:  0.7,
			},
			{
				RuleID:      "META-002",
				Severity:    models.SeverityInfo,
				FilePath:    "pyproject.toml",
				Title:       "Declared SDK dependency has no observed code use",
				Explanation: "Dependency \"google-adk\" is declared in pyproject.toml but no code uses it.",
				Confidence:  1.0,
			},
			{
				RuleID:      "META-001",
				Severity:    models.SeverityInfo,
				Title:       "Unaudited SDK in use",
				Explanation: "This repo uses SDK \"google_adk\", which Trustabl does not currently audit.",
				Confidence:  1.0,
			},
		},
	}
}

func TestGoldenSARIF(t *testing.T) {
	got := Render(fixtureResult(), "0.0.0-test")
	goldenPath := filepath.Join("testdata", "golden.sarif.json")

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("regenerated %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("missing golden file %s — run `go test ./internal/sarif/ -update` to create it; %v", goldenPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("SARIF output drift. Run `go test ./internal/sarif/ -update` to refresh if the change is intentional.")
		t.Logf("got:\n%s", got)
	}
}

func TestStructuralInvariants(t *testing.T) {
	out := Render(fixtureResult(), "0.0.0-test")
	var log Log
	if err := json.Unmarshal(out, &log); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if log.Version != "2.1.0" {
		t.Errorf("Version = %q", log.Version)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("Runs len = %d", len(log.Runs))
	}
	run := log.Runs[0]
	validLevels := map[string]bool{"": true, "none": true, "note": true, "warning": true, "error": true}

	// Every rule has a valid level.
	for _, r := range run.Tool.Driver.Rules {
		if r.DefaultConfiguration != nil && !validLevels[r.DefaultConfiguration.Level] {
			t.Errorf("rule %s: invalid level %q", r.ID, r.DefaultConfiguration.Level)
		}
	}

	// Every result's RuleIndex points at a real rule with the matching ID.
	for _, r := range run.Results {
		if r.RuleIndex == nil {
			t.Errorf("result %s: RuleIndex is nil", r.RuleID)
			continue
		}
		if *r.RuleIndex < 0 || *r.RuleIndex >= len(run.Tool.Driver.Rules) {
			t.Errorf("result %s: RuleIndex %d out of range", r.RuleID, *r.RuleIndex)
			continue
		}
		if run.Tool.Driver.Rules[*r.RuleIndex].ID != r.RuleID {
			t.Errorf("result %s: RuleIndex points at %s", r.RuleID, run.Tool.Driver.Rules[*r.RuleIndex].ID)
		}
	}

	// Every physicalLocation has a non-empty URI; every uriBaseId references
	// a known base.
	for _, r := range run.Results {
		for _, loc := range r.Locations {
			if loc.PhysicalLocation == nil {
				continue
			}
			if loc.PhysicalLocation.ArtifactLocation.URI == "" {
				t.Errorf("result %s: empty artifactLocation.uri", r.RuleID)
			}
			if base := loc.PhysicalLocation.ArtifactLocation.URIBaseID; base != "" {
				if _, ok := run.OriginalUriBaseIds[base]; !ok {
					t.Errorf("result %s: uriBaseId %q not in OriginalUriBaseIds", r.RuleID, base)
				}
			}
		}
	}

	// Every notification has a valid level and an in-range descriptor index.
	for _, inv := range run.Invocations {
		for _, n := range inv.ToolExecutionNotifications {
			if !validLevels[n.Level] {
				t.Errorf("notification: invalid level %q", n.Level)
			}
			if n.Descriptor != nil {
				if n.Descriptor.Index < 0 || n.Descriptor.Index >= len(run.Tool.Driver.Rules) {
					t.Errorf("notification descriptor.index %d out of range", n.Descriptor.Index)
				}
			}
		}
	}
}
