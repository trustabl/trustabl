// Command trustabl is the CLI entry point.
//
// Subcommands:
//
//	trustabl scan <target> [flags]   primary command: scan a repo
//	trustabl version                 print version
//
// Exit codes:
//
//	0  no findings ≥ medium
//	1  findings ≥ medium present (or scan completed with findings + --strict)
//	2  scanner / I/O error, or no usable rules (none resolved, incompatible
//	   schema, or a resolved pack that contains zero rules)
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/progress"
	"github.com/trustabl/trustabl/internal/review"
	"github.com/trustabl/trustabl/internal/rules"
	"github.com/trustabl/trustabl/internal/rulesource"
	"github.com/trustabl/trustabl/internal/sarif"
	"github.com/trustabl/trustabl/internal/scanner"
)

// Build metadata, injected at release time via -ldflags -X (see .goreleaser.yaml).
// Defaults are for local `go build` — an unreleased binary truthfully reports "dev".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// exitCodeError carries a desired process exit code through the cobra error
// path so we can avoid calling os.Exit inside runScan (which would skip any
// deferred cleanup added in the future).
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return "" }

func main() {
	rootCmd := &cobra.Command{
		Use:   "trustabl",
		Short: "Static analyzer for agent reliability",
		Long: "Trustabl scans agent SDK repos (Claude Agent SDK, OpenAI Agents SDK,\n" +
			"MCP) for reliability and safety weaknesses and reports the findings.",
		SilenceUsage:  true,
		SilenceErrors: true, // we handle error printing ourselves below
	}
	rootCmd.AddCommand(newScanCommand())
	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(newRulesCommand())

	if err := rootCmd.Execute(); err != nil {
		var ec exitCodeError
		if errors.As(err, &ec) {
			os.Exit(ec.code) // findings-based exit; message already printed
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "Trustabl %s\ncommit: %s\nbuilt:  %s\n",
				version, commit, date)
		},
	}
}

// ────────────────────────────────────────────────────────────────────────────
// scan
// ────────────────────────────────────────────────────────────────────────────

type scanFlags struct {
	detectors     string
	format        string
	strict        bool
	noColor       bool
	rulesRepo     string
	rulesRef      string
	noRulesUpdate bool
	noProgress    bool
	jsonOut       string
	sarifOut      string
}

func newScanCommand() *cobra.Command {
	var f scanFlags
	cmd := &cobra.Command{
		Use:   "scan <target>",
		Short: "Scan a local repo or GitHub URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(args[0], f)
		},
	}
	cmd.Flags().StringVar(&f.detectors, "detectors", "",
		"comma-separated detector categories: claude_sdk, openai_sdk, openshell, google_adk (default: all)")
	cmd.Flags().StringVar(&f.format, "format", "human",
		"output format: human|json|sarif")
	cmd.Flags().BoolVar(&f.strict, "strict", false,
		"exit 1 if any finding is present, regardless of severity")
	cmd.Flags().BoolVar(&f.noColor, "no-color", false,
		"disable colored output")
	cmd.Flags().StringVar(&f.rulesRepo, "rules-repo", "",
		"rules repository URL (default: official trustabl-rules; or TRUSTABL_RULES_REPO)")
	cmd.Flags().StringVar(&f.rulesRef, "rules-ref", "",
		"rules branch or tag to use (default: the repo's default branch)")
	cmd.Flags().BoolVar(&f.noRulesUpdate, "no-rules-update", false,
		"do not fetch rules; use the local cache only")
	cmd.Flags().BoolVar(&f.noProgress, "no-progress", false,
		"disable real-time progress output")
	cmd.Flags().StringVar(&f.jsonOut, "json-out", "",
		"also write the JSON ScanResult to this file (independent of --format)")
	cmd.Flags().StringVar(&f.sarifOut, "sarif-out", "",
		"also write the SARIF report to this file (independent of --format)")
	return cmd
}

func runScan(target string, f scanFlags) error {
	cfg := scanner.Config{Target: target}
	if f.detectors != "" {
		cats, err := parseCategories(f.detectors)
		if err != nil {
			return err
		}
		cfg.Categories = cats
	}

	mode := pickScanMode(f)

	// Non-TTY paths run synchronously: resolve, scan, render.
	if mode != progress.ModeTTY {
		var rep progress.Reporter = progress.NewNop()
		if mode == progress.ModePlain {
			rep = progress.NewPlain(os.Stderr)
		}
		return runScanSync(f, cfg, rep)
	}

	// TTY path: render on the main goroutine, do the job in a goroutine. The
	// outcome crosses back over a buffered channel — the receive below is the
	// explicit happens-before edge that publishes the goroutine's writes to this
	// goroutine (the buffer means the goroutine never blocks even if we bail on
	// an interrupt before receiving).
	rep := progress.NewTTY(os.Stderr)
	type scanOutcome struct {
		result models.ScanResult
		err    error
	}
	done := make(chan scanOutcome, 1)
	go func() {
		result, err := resolveAndScan(&cfg, f, rep)
		rep.Done()
		done <- scanOutcome{result, err}
	}()
	if err := rep.Run(); err != nil {
		if errors.Is(err, progress.ErrInterrupted) {
			fmt.Fprintln(os.Stderr, "Scan interrupted.")
			return exitCodeError{2}
		}
		return err
	}
	out := <-done
	return finishScan(out.result, out.err, f)
}

// pickScanMode maps flags + stderr TTY state to a progress mode.
func pickScanMode(f scanFlags) progress.Mode {
	isTTY := isatty.IsTerminal(os.Stderr.Fd())
	return progress.PickMode(f.format, isTTY, f.noColor, f.noProgress)
}

// runScanSync runs resolution + scan + render inline (plain/nop modes).
func runScanSync(f scanFlags, cfg scanner.Config, rep progress.Reporter) error {
	result, err := resolveAndScan(&cfg, f, rep)
	return finishScan(result, err, f)
}

// resolveAndScan resolves rules (reporting a "rules" phase) and runs the scan
// with the reporter attached.
func resolveAndScan(cfg *scanner.Config, f scanFlags, rep progress.Reporter) (models.ScanResult, error) {
	rep.StartPhase("rules", "Resolving rules")
	// Resolve makes a network round-trip (and a full clone on a cold cache) with
	// no internal progress; without a detail line the pre-flight spinner reads as
	// a blank/frozen screen. Name what it's contacting so the wait is legible.
	rcfg := rulesConfigFromScan(f)
	rulesRepo := rcfg.RepoURL
	if rulesRepo == "" {
		rulesRepo = rulesource.DefaultRepoURL
	}
	rep.SetDetail("fetching " + rulesRepo)
	res, err := rulesource.Resolve(rcfg, rules.SupportedSchemaVersion)
	if err != nil {
		rep.Fatal(err)
		return models.ScanResult{}, err
	}
	summary := res.SHA
	if res.FromCache {
		summary = res.SHA + " (cached, offline)"
	}
	rep.EndPhase(summary)

	cfg.RulesFS = res.FS
	cfg.RulesSource = res.RepoURL
	cfg.RulesVersion = res.SHA
	cfg.RulesFromCache = res.FromCache
	cfg.Progress = rep

	result, err := scanner.Run(*cfg)
	if err != nil {
		rep.Fatal(err)
		return models.ScanResult{}, err
	}
	return result, nil
}

// finishScan turns a job outcome into output + the process exit code.
func finishScan(result models.ScanResult, jobErr error, f scanFlags) error {
	if jobErr != nil {
		if errors.Is(jobErr, rulesource.ErrNoCompatibleRules) {
			fmt.Fprintf(os.Stderr,
				"The rules are newer than this Trustabl build can evaluate "+
					"(this engine supports rule schema version up to %d).\n",
				rules.SupportedSchemaVersion)
			fmt.Fprintln(os.Stderr,
				"Upgrade Trustabl, or use --rules-ref to pin an older compatible rules version.")
			return exitCodeError{2}
		}
		if errors.Is(jobErr, rulesource.ErrNoRules) {
			fmt.Fprintln(os.Stderr,
				"No usable rules found: none cached locally and none could be fetched.")
			fmt.Fprintln(os.Stderr,
				`Run "trustabl rules pull" to download the rule packs.`)
			return exitCodeError{2}
		}
		if errors.Is(jobErr, rules.ErrNoRulesInPack) {
			fmt.Fprintln(os.Stderr,
				"No usable rules: the resolved rule pack contains zero rules.")
			fmt.Fprintln(os.Stderr,
				`The rules repository may be empty or truncated. Run "trustabl rules pull" to refresh.`)
			return exitCodeError{2}
		}
		// A generic error was already surfaced to the user by the reporter's
		// Fatal() in plain/tty modes (the "[phase] failed: …" line / the ✗ row).
		// Returning it raw here would make main() print "Error: …" a second
		// time. Only in silent (off) mode did nothing present it, so let main
		// be the single presenter there.
		if pickScanMode(f) != progress.ModeOff {
			return exitCodeError{2}
		}
		return jobErr
	}

	// In silent mode (JSON or --no-progress) the "(cached, offline)" rules
	// phase line is suppressed, so surface the cache-fallback as a stderr
	// warning — stale rules should never be used without a human-visible signal.
	if result.RulesFromCache && pickScanMode(f) == progress.ModeOff {
		fmt.Fprintf(os.Stderr,
			"warning: using cached rules %s; could not fetch or use newer rules\n",
			result.RulesVersion)
	}

	// Incomplete parse coverage must never masquerade as a clean result. If any
	// AST-targeted file was skipped (unreadable or unparseable), say so on
	// stderr — stdout stays machine-clean for JSON/SARIF consumers.
	if skipped := result.Coverage.FilesSkipped; skipped > 0 {
		total := result.Coverage.FilesParsed + skipped
		fmt.Fprintf(os.Stderr,
			"warning: %d of %d source files could not be parsed and were skipped; findings may be incomplete\n",
			skipped, total)
	}

	switch f.format {
	case "json":
		if err := emitJSON(result); err != nil {
			return err
		}
	case "sarif":
		if err := emitSARIF(result); err != nil {
			return err
		}
	case "human", "":
		r := &review.Renderer{NoColor: f.noColor}
		fmt.Print(r.Render(result))
	default:
		return fmt.Errorf("unknown --format %q", f.format)
	}

	// --json-out / --sarif-out persist the respective format to a file
	// independent of --format, so one scan can print the human panel to stdout
	// while writing machine artifacts. The bytes are identical to the matching
	// --format stdout output (shared renderers).
	if err := writeSideOutputs(result, f); err != nil {
		return err
	}

	if code := exitCode(result, f.strict); code != 0 {
		return exitCodeError{code}
	}
	return nil
}

// jsonBytes renders the ScanResult as the canonical pretty JSON document
// (trailing newline). Shared by stdout (--format json) and file (--json-out) so
// the two are byte-identical.
func jsonBytes(result models.ScanResult) ([]byte, error) {
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func emitJSON(result models.ScanResult) error {
	b, err := jsonBytes(result)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(b)
	return err
}

func emitSARIF(result models.ScanResult) error {
	_, err := os.Stdout.Write(sarif.Render(result, version))
	return err
}

// writeSideOutputs honors --json-out / --sarif-out, writing each format to its
// file when the flag is set. No-op when both are empty.
func writeSideOutputs(result models.ScanResult, f scanFlags) error {
	if f.jsonOut != "" {
		b, err := jsonBytes(result)
		if err != nil {
			return err
		}
		if err := os.WriteFile(f.jsonOut, b, 0o644); err != nil {
			return fmt.Errorf("write --json-out: %w", err)
		}
	}
	if f.sarifOut != "" {
		if err := os.WriteFile(f.sarifOut, sarif.Render(result, version), 0o644); err != nil {
			return fmt.Errorf("write --sarif-out: %w", err)
		}
	}
	return nil
}

func exitCode(result models.ScanResult, strict bool) int {
	if strict && len(result.Findings) > 0 {
		return 1
	}
	for _, f := range result.Findings {
		switch f.Severity {
		case models.SeverityMedium, models.SeverityHigh, models.SeverityCritical:
			return 1
		}
	}
	return 0
}

// rulesConfigFromScan builds a rulesource.Config from scan flags, applying the
// TRUSTABL_RULES_REPO environment override when --rules-repo is not set.
func rulesConfigFromScan(f scanFlags) rulesource.Config {
	repo := f.rulesRepo
	if repo == "" {
		repo = os.Getenv("TRUSTABL_RULES_REPO")
	}
	return rulesource.Config{
		RepoURL:  repo,
		Ref:      f.rulesRef,
		NoUpdate: f.noRulesUpdate,
	}
}

func newRulesCommand() *cobra.Command {
	rulesCmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage Trustabl's detection rules",
	}

	var repo, ref string
	pull := &cobra.Command{
		Use:   "pull",
		Short: "Download the detection rule packs into the local cache",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if repo == "" {
				repo = os.Getenv("TRUSTABL_RULES_REPO")
			}
			res, err := rulesource.Pull(
				rulesource.Config{RepoURL: repo, Ref: ref},
				rules.SupportedSchemaVersion,
			)
			if err != nil {
				if errors.Is(err, rulesource.ErrNoCompatibleRules) {
					return fmt.Errorf("the rules at the requested ref are newer than this "+
						"Trustabl build can evaluate (this engine supports rule schema "+
						"version up to %d); upgrade Trustabl or pull an older --rules-ref",
						rules.SupportedSchemaVersion)
				}
				return fmt.Errorf("rules pull: %w", err)
			}
			fmt.Printf("Pulled rules from %s at %s\n", res.RepoURL, res.SHA)
			return nil
		},
	}
	pull.Flags().StringVar(&repo, "rules-repo", "",
		"rules repository URL (default: official trustabl-rules; or TRUSTABL_RULES_REPO)")
	pull.Flags().StringVar(&ref, "rules-ref", "",
		"rules branch or tag to pull (default: the repo's default branch)")

	rulesCmd.AddCommand(pull)
	return rulesCmd
}

func parseCategories(s string) ([]models.DetectorCategory, error) {
	var out []models.DetectorCategory
	for _, raw := range strings.Split(s, ",") {
		c := models.DetectorCategory(strings.TrimSpace(raw))
		switch c {
		case models.CategoryClaudeSDK, models.CategoryOpenAISDK,
			models.CategoryOpenShell, models.CategoryGoogleADK:
			out = append(out, c)
		default:
			return nil, fmt.Errorf("unknown detector category %q (allowed: claude_sdk, openai_sdk, openshell, google_adk)", c)
		}
	}
	return out, nil
}
