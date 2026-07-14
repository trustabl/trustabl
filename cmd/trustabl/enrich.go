package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/enrichment"
	"github.com/trustabl/trustabl/internal/langsmith"
	"github.com/trustabl/trustabl/internal/llm"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/telemetry"
)

type enrichFlags struct {
	inputFile        string
	repoRoot         string
	outputFile       string
	apply            bool
	onlyEnriched     bool
	diff             bool
	rules            []string
	langsmith        bool
	langsmithProject string
}

func newEnrichCommand(tel *telemetry.Client) *cobra.Command {
	var f enrichFlags
	cmd := &cobra.Command{
		Use:   "enrich",
		Short: "Enrich a scan result with AI-generated explanations and code fixes",
		Long: `Reads a ScanResult produced by "trustabl scan --format json" (from --input or
stdin), extracts the enclosing code block around each flagged line, and sends it
to the configured LLM provider to generate code-specific explanations and exact
line replacements.

Requires an LLM provider to be configured. Supported providers:

  anthropic   export ANTHROPIC_API_KEY=<key>   or   trustabl llm key set
  openai      export OPENAI_API_KEY=<key>       or   trustabl llm key set
  google      export GOOGLE_API_KEY=<key>       or   trustabl llm key set

Switch provider:   trustabl llm provider set openai
Optional model:    export TRUSTABL_LLM_MODEL=gpt-4.1   or   trustabl llm model set gpt-4.1

With --langsmith, tool-scope findings are additionally grounded in runtime trace
evidence: recent executions of each flagged tool are sampled from a LangSmith
project (error rate, latency, recent error messages) and fed to the LLM alongside
the static code snippet. Requires LANGSMITH_API_KEY; the project is taken from
--langsmith-project, then LANGSMITH_PROJECT, then "default" (self-hosted
deployments: set LANGSMITH_ENDPOINT). Tools with no trace history, or trace API
errors, degrade per finding to plain static enrichment; they never fail the run.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tel != nil {
				tel.Track("command.run", map[string]any{"command": "enrich"})
			}
			return runEnrich(cmd, f)
		},
	}
	cmd.Flags().StringVarP(&f.inputFile, "input", "i", "",
		"path to ScanResult JSON (default: stdin)")
	cmd.Flags().StringVarP(&f.repoRoot, "repo", "r", ".",
		"root directory of the scanned repository (for reading source files)")
	cmd.Flags().StringVarP(&f.outputFile, "output", "o", "",
		"output file path (default: stdout)")
	cmd.Flags().BoolVar(&f.apply, "apply", false,
		"write AI-generated fixes to source files on disk; combine with --diff to preview before writing")
	cmd.Flags().BoolVar(&f.onlyEnriched, "only-enriched", false,
		"omit findings that could not be enriched from the output")
	cmd.Flags().BoolVar(&f.diff, "diff", false,
		"print a unified diff of proposed replacements to stderr; combine with --apply to also write them")
	cmd.Flags().StringArrayVar(&f.rules, "rule", nil,
		"filter to a specific rule ID (repeatable, e.g. --rule CSDK-010)")
	cmd.Flags().BoolVar(&f.langsmith, "langsmith", false,
		"ground tool-scope findings in LangSmith runtime traces (requires LANGSMITH_API_KEY)")
	cmd.Flags().StringVar(&f.langsmithProject, "langsmith-project", "",
		"LangSmith project to sample traces from (default: $LANGSMITH_PROJECT, then \"default\")")
	return cmd
}

func runEnrich(cmd *cobra.Command, f enrichFlags) error {
	cfg, err := llm.Load()
	if err != nil {
		return fmt.Errorf("enrich: load llm config: %w", err)
	}
	key := cfg.ActiveProvider().Key
	if key == "" {
		return fmt.Errorf("enrich: no LLM key configured, run: trustabl llm key set")
	}
	model := cfg.ActiveProvider().Model

	result, err := readScanResult(f.inputFile)
	if err != nil {
		return fmt.Errorf("enrich: read input: %w", err)
	}

	pipeline := &enrichment.Pipeline{
		LLMProvider:  cfg.Active,
		LLMKey:       key,
		LLMModel:     model,
		RepoRoot:     f.repoRoot,
		RuleFilter:   f.rules,
		Apply:        f.apply,
		OnlyEnriched: f.onlyEnriched,
		Diff:         f.diff,
	}

	if f.langsmith {
		// Double gate: the flag is the opt-in, the key is BYOK. The flag is an
		// explicit request, so a missing key fails loudly here; silently
		// producing un-grounded output the user believes is trace-informed
		// would be worse. Per-tool trace failures past this point degrade
		// silently inside the pipeline instead.
		lsKey := os.Getenv("LANGSMITH_API_KEY")
		if lsKey == "" {
			return fmt.Errorf("enrich: --langsmith requires LANGSMITH_API_KEY to be set")
		}
		project := f.langsmithProject
		if project == "" {
			project = os.Getenv("LANGSMITH_PROJECT")
		}
		if project == "" {
			project = "default"
		}
		pipeline.Traces = langsmith.New(lsKey, project, os.Getenv("LANGSMITH_ENDPOINT"))
	}

	enriched, err := pipeline.Run(cmd.Context(), result)
	if err != nil {
		return fmt.Errorf("enrich: pipeline: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	if f.outputFile != "" {
		fh, err := os.Create(f.outputFile)
		if err != nil {
			return fmt.Errorf("enrich: create output: %w", err)
		}
		defer fh.Close()
		enc = json.NewEncoder(fh)
		enc.SetIndent("", "  ")
	}

	return enc.Encode(enriched)
}

func readScanResult(path string) (*models.ScanResult, error) {
	if path == "" {
		var r models.ScanResult
		if err := json.NewDecoder(os.Stdin).Decode(&r); err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return &r, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", path, err)
	}
	var r models.ScanResult
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return &r, nil
}
