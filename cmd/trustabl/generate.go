package main

import (
	"fmt"
	"os"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/acac"
	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/progress"
	"github.com/trustabl/trustabl/internal/scanner"
)

type generateFlags struct {
	agent           string
	base            string
	out             string
	openshellPolicy string
	owasp           bool
	timestamp       bool
	enrich          bool
	failOn          string
	rulesRepo       string
	rulesRef        string
	channel         string
	noRulesUpdate   bool
	noProgress      bool
	vulnScan        bool
}

func newGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate deployment artifacts from a scan",
		Long: `Generate turns a Trustabl scan into deployment artifacts.

Subcommands:
  agent-yaml   an Agent Format (.agf.yaml) manifest with the x-trustabl
               reliability/readiness extension`,
	}
	cmd.AddCommand(newGenerateAgentYAMLCommand())
	return cmd
}

func newGenerateAgentYAMLCommand() *cobra.Command {
	var f generateFlags
	cmd := &cobra.Command{
		Use:   "agent-yaml [PATH]",
		Short: "Generate an Agent Format manifest (.agf.yaml) for one agent",
		Long: `Scan a repository and generate an Agent Format manifest for one agent,
carrying a Trustabl x-trustabl extension with per-surface readiness scores,
findings, and coverage.

One manifest describes one agent system. When the repo declares exactly one
agent it is selected automatically; with more than one, pass --agent <name>.
Subagents and skills are never manifest roots — they ride along on the
selected agent.

Everything the scan can prove is auto-filled; human intent (versioning,
budgets, the I/O contract) is scaffolded and marked with 'trustabl:' comments,
never invented. The reliability_score in x-trustabl is informational in v0.x —
its thresholds are provisional pending corpus calibration.

Output is deterministic by default: the same repo and the same rules version
produce a byte-identical manifest. --timestamp opts in to a generated_at field.

Exit codes:
  0  manifest generated and the readiness gate passed
  1  manifest generated but deployment_readiness is at or below --fail-on
  2  operational error (no agent discovered, ambiguous --agent, unwritable
     output, no usable rules)`,
		Example: `  # Single-agent repo, default output ./<agent-id>.agf.yaml
  trustabl generate agent-yaml .

  # Multi-agent repo: name the manifest root
  trustabl generate agent-yaml . --agent "Research Agent"

  # Write to an explicit path and fail CI unless the agent is fully ready
  trustabl generate agent-yaml . --out deploy/agent.agf.yaml --fail-on needs_work

  # Include known-vulnerability data in x-trustabl
  trustabl generate agent-yaml . --vuln-scan`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			return runGenerateAgentYAML(target, f, logLevelFor(cmd))
		},
	}
	cmd.Flags().StringVar(&f.agent, "agent", "",
		"agent to use as the manifest root (required when more than one agent is discovered)")
	cmd.Flags().StringVar(&f.base, "base", "agent-format",
		"base manifest standard (agent-format is the default and only base in v0.x)")
	cmd.Flags().StringVar(&f.out, "out", "",
		"output file (default: ./<agent-id>.agf.yaml)")
	cmd.Flags().StringVar(&f.openshellPolicy, "openshell-policy", "",
		"also emit an NVIDIA OpenShell sandbox policy derived from the same scan to this file "+
			"(verified end-to-end against OpenShell 0.0.36: policy accepted, enforcement observed)")
	cmd.Flags().BoolVar(&f.owasp, "owasp", true,
		"annotate findings with OWASP ASI/AST IDs from the pinned engine map")
	cmd.Flags().BoolVar(&f.timestamp, "timestamp", false,
		"include a generated_at timestamp (off by default: output stays byte-stable)")
	cmd.Flags().BoolVar(&f.enrich, "enrich", false,
		"enrich scaffolded fields with an LLM (not yet wired in this build)")
	cmd.Flags().StringVar(&f.failOn, "fail-on", "not_ready",
		"readiness gate for exit code 1: not_ready|needs_work|never")
	cmd.Flags().StringVar(&f.rulesRepo, "rules-repo", "",
		"rules repository URL (default: official trustabl-rules; or TRUSTABL_RULES_REPO)")
	cmd.Flags().StringVar(&f.rulesRef, "rules-ref", "",
		"rules branch or tag to use (default: the repo's default branch)")
	cmd.Flags().StringVar(&f.channel, "channel", "",
		"resolve rules from a signed release channel instead of git")
	cmd.Flags().BoolVar(&f.noRulesUpdate, "no-rules-update", false,
		"do not fetch rules; use the local cache only")
	cmd.Flags().BoolVar(&f.noProgress, "no-progress", false,
		"disable real-time progress output")
	cmd.Flags().BoolVar(&f.vulnScan, "vuln-scan", false,
		"match dependencies against a pinned OSV snapshot and carry known CVEs in x-trustabl")
	return cmd
}

func runGenerateAgentYAML(target string, f generateFlags, level logx.Level) error {
	if f.enrich {
		return fmt.Errorf("--enrich is not yet wired in this build; generate without it (scaffolds carry 'trustabl:' markers for manual completion)")
	}
	if f.base != "agent-format" {
		return fmt.Errorf("unknown --base %q (agent-format is the only base in v0.x)", f.base)
	}
	switch f.failOn {
	case "not_ready", "needs_work", "never":
	default:
		return fmt.Errorf("unknown --fail-on %q (allowed: not_ready, needs_work, never)", f.failOn)
	}

	log := logx.New(os.Stderr, level, diagColor(false))
	log.Verbosef("generate: target %s", target)

	// Progress is stderr-only, plain lines (no animated panel — generate's
	// output is a file, not a stdout report, so the TTY machinery buys
	// nothing). --no-progress silences it.
	var rep progress.Reporter = progress.NewNop()
	if !f.noProgress && isatty.IsTerminal(os.Stderr.Fd()) {
		rep = progress.NewPlain(os.Stderr)
	}

	// Reuse the scan command's rules-resolution + scan path verbatim: generate
	// is a consumer of the same pipeline, not a second pipeline.
	cfg := scanner.Config{Target: target, Log: log}
	sf := scanFlags{
		rulesRepo:     f.rulesRepo,
		rulesRef:      f.rulesRef,
		channel:       f.channel,
		noRulesUpdate: f.noRulesUpdate,
		vulnScan:      f.vulnScan,
	}
	result, err := resolveAndScan(&cfg, sf, rep)
	if err != nil {
		if presented, ok := presentKnownScanError(err); ok {
			return presented
		}
		return err
	}

	agent, err := acac.SelectAgent(result, f.agent)
	if err != nil {
		return err
	}

	opts := acac.BuildOptions{
		EngineVersion: version,
		IncludeOWASP:  f.owasp,
	}
	if f.timestamp {
		opts.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}
	manifest := acac.Build(result, agent, opts)
	out, err := acac.Emit(manifest)
	if err != nil {
		return err
	}

	path := f.out
	if path == "" {
		path = manifest.Metadata.ID + ".agf.yaml"
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("writing manifest to %s: %w", path, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s · deployment_readiness %s · reliability_score %d (informational pending calibration)\n",
		path, manifest.XTrustabl.Readiness, manifest.XTrustabl.Score100)

	if f.openshellPolicy != "" {
		policy := acac.BuildOpenShellPolicy(result, agent)
		if err := acac.ValidateOpenShellPolicy(policy); err != nil {
			return err
		}
		policyOut, err := acac.EmitOpenShellPolicy(policy)
		if err != nil {
			return err
		}
		if err := os.WriteFile(f.openshellPolicy, policyOut, 0o644); err != nil {
			return fmt.Errorf("writing OpenShell policy to %s: %w", f.openshellPolicy, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s — review the trustabl: markers (interpreter path, endpoint methods) before applying\n",
			f.openshellPolicy)
	}

	if gateFails(manifest.XTrustabl.Readiness, f.failOn) {
		fmt.Fprintf(os.Stderr, "readiness gate failed: deployment_readiness is %s (--fail-on %s)\n",
			manifest.XTrustabl.Readiness, f.failOn)
		return exitCodeError{1}
	}
	return nil
}

// gateFails reports whether the readiness level is at or below the --fail-on
// threshold (ready > needs_work > not_ready; "never" disables the gate).
func gateFails(level acac.ReadinessLevel, failOn string) bool {
	switch failOn {
	case "never":
		return false
	case "needs_work":
		return level == acac.ReadinessNeedsWork || level == acac.ReadinessNotReady
	default: // not_ready
		return level == acac.ReadinessNotReady
	}
}
