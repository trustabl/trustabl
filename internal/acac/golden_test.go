package acac

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

var update = flag.Bool("update", false, "regenerate the golden manifests")

// goldenCases pins the generate path end to end (scan → select → build →
// emit) for three corpus fixtures against the rules fixture. Regenerate with:
//
//	go test ./internal/acac/ -run TestGoldenManifests -update
//
// The goldens are coupled to the rules fixture by design: a rule change that
// alters findings legitimately changes the manifest.
var goldenCases = []struct {
	name    string // golden file basename and subtest name
	fixture string // testdata/corpus/<fixture>
	agent   string // --agent selection (these are multi-agent repos)
}{
	{"basic-openai-agent", "basic-openai-agent", "Hello world"},
	{"ts-claude-sdk-min", "ts-claude-sdk-min", "analyst"},
	{"financial_research_agent", "financial_research_agent", "FinancialSearchAgent"},
	// Single-agent fixture authored for the Stage 2 typed captures: static
	// HTTP hosts, static write paths, retry_present — all visible in the
	// golden's x-trustabl surface facts. No --agent needed.
	{"acac-static-capture", "acac-static-capture", ""},
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

// scanCorpus runs the real scanner over a corpus fixture with the rules
// fixture, with a fixed RulesVersion so ScanID and the manifest are stable.
// Results are cached per fixture: the golden, selection, and stability tests
// all share one scan.
var corpusScans = map[string]models.ScanResult{}

func scanCorpus(t *testing.T, fixture string) models.ScanResult {
	t.Helper()
	if r, ok := corpusScans[fixture]; ok {
		return r
	}
	root := repoRoot(t)
	result, err := scanner.Run(scanner.Config{
		Target:       filepath.Join(root, "testdata", "corpus", fixture),
		RulesFS:      os.DirFS(filepath.Join(root, "testdata", "rules-fixture")),
		RulesVersion: "test-fixture",
	})
	if err != nil {
		t.Fatalf("scan %s: %v", fixture, err)
	}
	corpusScans[fixture] = result
	return result
}

func generateGolden(t *testing.T, fixture, agentName string) []byte {
	t.Helper()
	result := scanCorpus(t, fixture)
	agent, err := SelectAgent(result, agentName)
	if err != nil {
		t.Fatalf("select agent %q: %v", agentName, err)
	}
	out, err := Emit(Build(result, agent, BuildOptions{EngineVersion: "test", IncludeOWASP: true}))
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return out
}

func TestGoldenManifests(t *testing.T) {
	for _, c := range goldenCases {
		t.Run(c.name, func(t *testing.T) {
			got := generateGolden(t, c.fixture, c.agent)
			goldenPath := filepath.Join("testdata", c.name+".agf.yaml.golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
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
				t.Fatalf("read golden (run with -update to regenerate): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("manifest differs from golden %s (run with -update if the change is intended)\n--- got ---\n%s", goldenPath, got)
			}

			// Byte-stability across full re-generation, same process: a second
			// scan + build + emit must be byte-identical (no map-iteration or
			// scheduling leakage anywhere in the path).
			delete(corpusScans, c.fixture)
			again := generateGolden(t, c.fixture, c.agent)
			if !bytes.Equal(got, again) {
				t.Error("second generation differs from first: the generate path is not deterministic")
			}
		})
	}
}

// TestGoldenManifestsValidateAgainstSchema validates every golden against the
// vendored published AgentFormat schema. This is the spec's hard requirement:
// a generated manifest MUST validate — enforced by a test, not by review.
func TestGoldenManifestsValidateAgainstSchema(t *testing.T) {
	schemaBytes, err := os.ReadFile(filepath.Join("schema", "agentformat-1.0.json"))
	if err != nil {
		t.Fatalf("read vendored schema: %v", err)
	}
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		t.Fatalf("parse vendored schema: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("agentformat-1.0.json", rawSchema); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile("agentformat-1.0.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	for _, c := range goldenCases {
		t.Run(c.name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("testdata", c.name+".agf.yaml.golden"))
			if err != nil {
				t.Fatalf("read golden (run with -update to regenerate): %v", err)
			}
			// YAML → generic value → JSON bytes → JSON-typed value, so the
			// validator sees exactly what a JSON consumer of the YAML sees.
			var v any
			if err := yaml.Unmarshal(b, &v); err != nil {
				t.Fatalf("golden is not valid YAML: %v", err)
			}
			jb, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("golden is not JSON-representable: %v", err)
			}
			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(jb))
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(inst); err != nil {
				t.Errorf("golden does not validate against the published schema:\n%v", err)
			}
		})
	}
}

// TestMultiAgentSelectionOnCorpus pins the spec §3 selection rules against a
// real multi-agent repo: no --agent is ambiguous; a name carried by several
// same-named example agents cannot disambiguate.
func TestMultiAgentSelectionOnCorpus(t *testing.T) {
	result := scanCorpus(t, "basic-openai-agent")
	if len(result.Agents) < 2 {
		t.Fatalf("fixture expectation broken: %d agents", len(result.Agents))
	}

	_, err := SelectAgent(result, "")
	var ambiguous *AmbiguousAgentError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("no --agent on a multi-agent repo: got %v, want AmbiguousAgentError", err)
	}

	_, err = SelectAgent(result, "Assistant")
	var unknown *UnknownAgentError
	if !errors.As(err, &unknown) || unknown.Matches < 2 {
		t.Fatalf("--agent Assistant matches many: got %v", err)
	}

	agent, err := SelectAgent(result, "Hello world")
	if err != nil || agent.Name != "Hello world" {
		t.Fatalf("unique-name selection failed: %v", err)
	}
}
