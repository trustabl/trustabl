package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/trustabl/trustabl/internal/cyclonedx"
	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/progress"
	"github.com/trustabl/trustabl/internal/review"
	"github.com/trustabl/trustabl/internal/rules"
	"github.com/trustabl/trustabl/internal/rulesign"
	"github.com/trustabl/trustabl/internal/rulesource"
	"github.com/trustabl/trustabl/internal/sarif"
	"github.com/trustabl/trustabl/internal/scanner"
	"github.com/trustabl/trustabl/internal/telemetry"
)

type scanFlags struct {
	detectors     string
	format        string
	output        string
	strict        bool
	noColor       bool
	rulesRepo     string
	rulesRef      string
	rulesSource   string
	channel       string
	requireSigned bool
	noRulesUpdate bool
	noProgress    bool
	jsonOut       string
	sarifOut      string
	bomOut        string
	vulnScan      bool
	attest        bool
	attestKey     string
	attestBundle  string
	attestNoTLog  bool
	flagsUsed     []string
}

func newScanCommand(tel *telemetry.Client) *cobra.Command {
	var f scanFlags
	cmd := &cobra.Command{
		Use:   "scan <target>",
		Short: "Scan a local repo or GitHub URL",
		Long: `Scan a repository for agent reliability and safety weaknesses.

<target> is either a local path (a directory or a single file) or a GitHub URL
(https://github.com/owner/repo, optionally .../tree/<ref>). A remote target is
cloned into a temporary directory for the scan and removed afterward.

The scan discovers the tools, agents, subagents, and MCP servers in the repo,
loads the rule packs for the SDKs it actually finds, and reports the findings.
Detection rules are resolved from the trustabl-rules repository and cached
locally; pass --no-rules-update to run fully offline from that cache, or
--rules-ref to pin a branch or tag.

The report is written to stdout in the chosen --format (human, json, or sarif).
Use --output/-o to write it to a file instead, or --json-out / --sarif-out to
persist those formats alongside the stdout report. All progress, diagnostics, and
warnings go to stderr, so stdout stays byte-stable for machine consumers.

Exit codes:
  0  no findings of medium severity or higher
  1  one or more findings >= medium (or >= low with --strict; info/META
     signals never raise the exit code)
  2  scanner / I/O error, or no usable rules`,
		Example: `  # Human-readable scan of the current directory
  trustabl scan .

  # Scan a GitHub repo at a specific branch or tag
  trustabl scan https://github.com/owner/repo/tree/main

  # JSON to stdout, or to a file
  trustabl scan . --format json
  trustabl scan . --format json -o report.json

  # SARIF for a GitHub code-scanning upload
  trustabl scan . --format sarif -o trustabl.sarif

  # Print the human panel but also save machine artifacts
  trustabl scan . --json-out report.json --sarif-out trustabl.sarif

  # Strict CI gate: any finding (low and above) fails the run
  trustabl scan . --strict

  # Limit to specific detector categories
  trustabl scan . --detectors claude_sdk,mcp

  # Offline: use the cached rules without fetching
  trustabl scan . --no-rules-update`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var flagsUsed []string
			cmd.Flags().Visit(func(fl *pflag.Flag) {
				flagsUsed = append(flagsUsed, fl.Name)
			})
			f.flagsUsed = flagsUsed
			return runScan(args[0], f, logLevelFor(cmd), tel)
		},
	}
	cmd.Flags().StringVar(&f.detectors, "detectors", "",
		"comma-separated detector categories to run (default: all): "+categoryList())
	cmd.Flags().StringVar(&f.format, "format", "human",
		"output format: human|json|sarif")
	cmd.Flags().StringVarP(&f.output, "output", "o", "",
		"write the report to a file instead of stdout (use with --format sarif to feed code-scanning upload)")
	cmd.Flags().BoolVar(&f.strict, "strict", false,
		"exit 1 on any finding of low severity or higher (info/META signals never fail)")
	cmd.Flags().BoolVar(&f.noColor, "no-color", false,
		"disable colored output")
	cmd.Flags().StringVar(&f.rulesRepo, "rules-repo", "",
		"rules repository URL (default: official trustabl-rules; or TRUSTABL_RULES_REPO)")
	cmd.Flags().StringVar(&f.rulesRef, "rules-ref", "",
		"rules branch or tag to use (default: the repo's default branch)")
	cmd.Flags().StringVar(&f.rulesSource, "rules-source", "",
		"rules source: 'git' for the unsigned git path, or a signed channel name (e.g. production, "+
			"staging). Default: git. A signed channel is signature-verified; a pre-release channel is "+
			"watermarked in the report. --rules-repo/--rules-ref imply git.")
	cmd.Flags().StringVar(&f.channel, "channel", "",
		"deprecated alias for --rules-source <channel> (a signed release channel)")
	cmd.Flags().BoolVar(&f.requireSigned, "require-signed", false,
		"refuse to scan unless rules resolve from a signed channel (also via TRUSTABL_REQUIRE_SIGNED=1); "+
			"a hard CI gate against silently scanning with unsigned git rules")
	cmd.Flags().BoolVar(&f.noRulesUpdate, "no-rules-update", false,
		"do not fetch rules; use the local cache only")
	cmd.Flags().BoolVar(&f.noProgress, "no-progress", false,
		"disable real-time progress output")
	cmd.Flags().StringVar(&f.jsonOut, "json-out", "",
		"also write the JSON ScanResult to this file (independent of --format)")
	cmd.Flags().StringVar(&f.sarifOut, "sarif-out", "",
		"also write the SARIF report to this file (independent of --format)")
	cmd.Flags().StringVar(&f.bomOut, "bom-out", "",
		"also write a CycloneDX BOM of the repo's declared dependencies to this file")
	cmd.Flags().BoolVar(&f.vulnScan, "vuln-scan", false,
		"match dependencies against a pinned OSV snapshot and report known CVEs (off by default; fetches the OSV database on first use, then caches it)")
	cmd.Flags().BoolVar(&f.attest, "attest", false,
		"after the scan, sign the JSON report into a cosign attestation (requires cosign on PATH; keyless by default — see 'trustabl attest')")
	cmd.Flags().StringVar(&f.attestKey, "attest-key", "",
		"cosign private-key reference for --attest; omit for keyless signing")
	cmd.Flags().StringVar(&f.attestBundle, "attest-bundle", defaultAttestBundle,
		"output path for the --attest signed bundle")
	cmd.Flags().BoolVar(&f.attestNoTLog, "attest-no-tlog", false,
		"with --attest, do not upload the signature to the public Rekor transparency log")
	return cmd
}

func runScan(target string, f scanFlags, level logx.Level, tel *telemetry.Client) error {
	if err := validateOutputFlags(f); err != nil {
		return err
	}
	// Diagnostics are stderr-only. A LevelNormal logger emits nothing; color is
	// gated like the report (see diagColor).
	log := logx.New(os.Stderr, level, diagColor(f.noColor))
	log.Verbosef("scan: target %s", target)
	log.Debugf("scan: flags format=%s strict=%v no-color=%v no-progress=%v output=%q json-out=%q sarif-out=%q bom-out=%q detectors=%q",
		f.format, f.strict, f.noColor, f.noProgress, f.output, f.jsonOut, f.sarifOut, f.bomOut, f.detectors)

	startTime := time.Now()
	targetType := "local"
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		targetType = "remote"
	}

	cfg := scanner.Config{Target: target, Log: log}
	if f.detectors != "" {
		cats, err := parseCategories(f.detectors)
		if err != nil {
			return err
		}
		cfg.Categories = cats
	}

	if tel != nil {
		tel.Track("scan.started", map[string]any{
			"os":             runtime.GOOS,
			"arch":           runtime.GOARCH,
			"target_type":    targetType,
			"format":         f.format,
			"strict_mode":    f.strict,
			"flags_used":     f.flagsUsed,
			"ci_provider":    telemetry.DetectCIProvider(),
			"is_new_install": tel.IsNewInstall(),
		})
	}

	mode := pickScanMode(f, log)
	log.Debugf("scan: progress mode %s", modeName(mode))

	// Non-TTY paths run synchronously: resolve, scan, render. Verbose/debug always
	// land here — pickScanMode downgrades an animated TTY panel to plain lines so
	// it cannot fight interleaved diagnostic output on the same stderr.
	if mode != progress.ModeTTY {
		var rep progress.Reporter = progress.NewNop()
		if mode == progress.ModePlain {
			rep = progress.NewPlain(os.Stderr)
		}
		return runScanSync(f, cfg, rep, tel, startTime)
	}

	// TTY path: render on the main goroutine, do the job in a goroutine. The
	// outcome crosses back over a buffered channel — the receive below is the
	// explicit happens-before edge that publishes the goroutine's writes to this
	// goroutine (the buffer means the goroutine never blocks even if we bail on
	// an interrupt before receiving).
	rep := progress.NewTTY(os.Stderr)
	// Cancel the scan when the user interrupts the TTY. Without this the scan
	// goroutine would be abandoned and os.Exit (below) would skip its deferred
	// temp-dir cleanup, orphaning the trustabl-clone-* dir on every interrupted
	// remote scan.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg.Ctx = ctx
	type scanOutcome struct {
		result models.ScanResult
		err    error
	}
	done := make(chan scanOutcome, 1)
	go func() {
		// Convert a panic in the scan into a clean error on the channel. Without
		// this, an unrecovered panic in this goroutine tears down the whole
		// process with a raw stack trace; here it surfaces as a normal scan
		// failure (exit 2) and lets the TTY reporter shut down first.
		defer func() {
			if r := recover(); r != nil {
				rep.Done()
				done <- scanOutcome{err: fmt.Errorf("scan panicked: %v", r)}
			}
		}()
		result, err := resolveAndScan(&cfg, f, rep)
		rep.Done()
		done <- scanOutcome{result, err}
	}()
	if err := rep.Run(); err != nil {
		if errors.Is(err, progress.ErrInterrupted) {
			fmt.Fprintln(os.Stderr, "Scan interrupted.")
			// Signal the scan goroutine to stop, then give it a brief window to
			// return so its deferred src.Cleanup() removes the clone temp dir.
			// Bounded so a wedged scan can't hold the process open indefinitely.
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
			}
			return exitCodeError{2}
		}
		return err
	}
	out := <-done
	return finishScan(out.result, out.err, f, log, tel, startTime)
}

// validateOutputFlags rejects output destinations that collide. --output writes
// the --format report to a file; --json-out/--sarif-out write those formats to
// files independently. Pointing two of them at one path writes it twice and,
// across formats, silently clobbers — so require distinct paths.
func validateOutputFlags(f scanFlags) error {
	out := filepath.Clean(f.output)
	jsonOut := filepath.Clean(f.jsonOut)
	sarifOut := filepath.Clean(f.sarifOut)
	bomOut := filepath.Clean(f.bomOut)
	if f.output != "" && f.jsonOut != "" && out == jsonOut {
		return fmt.Errorf("--output and --json-out point at the same file (%s); use distinct paths", f.output)
	}
	if f.output != "" && f.sarifOut != "" && out == sarifOut {
		return fmt.Errorf("--output and --sarif-out point at the same file (%s); use distinct paths", f.output)
	}
	if f.jsonOut != "" && f.sarifOut != "" && jsonOut == sarifOut {
		return fmt.Errorf("--json-out and --sarif-out point at the same file (%s); use distinct paths", f.jsonOut)
	}
	if f.output != "" && f.bomOut != "" && out == bomOut {
		return fmt.Errorf("--output and --bom-out point at the same file (%s); use distinct paths", f.output)
	}
	if f.jsonOut != "" && f.bomOut != "" && jsonOut == bomOut {
		return fmt.Errorf("--json-out and --bom-out point at the same file (%s); use distinct paths", f.jsonOut)
	}
	if f.sarifOut != "" && f.bomOut != "" && sarifOut == bomOut {
		return fmt.Errorf("--sarif-out and --bom-out point at the same file (%s); use distinct paths", f.sarifOut)
	}
	return nil
}

// pickScanMode maps flags + stderr TTY state to a progress mode, then downgrades
// the animated panel when verbose/debug logging is on (see modeForLogs).
func pickScanMode(f scanFlags, log *logx.Logger) progress.Mode {
	isTTY := isatty.IsTerminal(os.Stderr.Fd())
	base := progress.PickMode(f.format, isTTY, f.noColor, f.noProgress)
	return modeForLogs(base, log.Verbose())
}

// modeForLogs forces an animated TTY panel down to plain lines when verbose is
// true: bubbletea repaints its panel in place, and interleaving diagnostic log
// lines into that region on the same stderr corrupts both. ModeOff (JSON/SARIF)
// and ModePlain are returned unchanged — the logger writes alongside them fine.
func modeForLogs(base progress.Mode, verbose bool) progress.Mode {
	if verbose && base == progress.ModeTTY {
		return progress.ModePlain
	}
	return base
}

// diagColor decides whether logx diagnostics get a dim ANSI tag. Mirroring the
// human report's color contract, color is suppressed by --no-color, by the
// NO_COLOR convention, and whenever stderr is not a terminal (piped / CI) — so
// diagnostics stay plain ASCII wherever a machine or a log file is reading them.
func diagColor(noColor bool) bool {
	return !noColor && os.Getenv("NO_COLOR") == "" && isatty.IsTerminal(os.Stderr.Fd())
}

// modeName renders a progress.Mode for a --debug line.
func modeName(m progress.Mode) string {
	switch m {
	case progress.ModeOff:
		return "off"
	case progress.ModePlain:
		return "plain"
	case progress.ModeTTY:
		return "tty"
	default:
		return "unknown"
	}
}

// runScanSync runs resolution + scan + render inline (plain/nop modes).
func runScanSync(f scanFlags, cfg scanner.Config, rep progress.Reporter, tel *telemetry.Client, startTime time.Time) error {
	result, err := resolveAndScan(&cfg, f, rep)
	return finishScan(result, err, f, cfg.Log, tel, startTime)
}

// resolveAndScan resolves rules (reporting a "rules" phase) and runs the scan
// with the reporter attached.
func resolveAndScan(cfg *scanner.Config, f scanFlags, rep progress.Reporter) (models.ScanResult, error) {
	log := cfg.Log
	rep.StartPhase("rules", "Resolving rules")
	// Resolve makes a network round-trip (and a full clone on a cold cache) with
	// no internal progress; without a detail line the pre-flight spinner reads as
	// a blank/frozen screen. Name what it's contacting so the wait is legible.
	rcfg, origin, rerr := effectiveRules(f)
	if rerr != nil {
		rep.Fatal(rerr)
		return models.ScanResult{}, rerr
	}
	rulesRepo := rcfg.RepoURL
	if rulesRepo == "" {
		rulesRepo = rulesource.DefaultRepoURL
	}
	noUpdateNote := ""
	if rcfg.NoUpdate {
		noUpdateNote = " (no-update: cache only)"
	}
	log.Verbosef("rules: resolving %s @ %s%s", rulesRepo, refOrDefault(rcfg.Ref), noUpdateNote)
	log.Debugf("rules: engine supports rule schema version <= %d", rules.SupportedSchemaVersion)
	if dir, err := os.UserCacheDir(); err == nil {
		log.Debugf("rules: cache root %s", filepath.Join(dir, "trustabl", "rules"))
	}
	rep.SetDetail("fetching " + rulesRepo)
	stop := log.Timer("rules resolution")
	res, err := rulesource.Resolve(rcfg, rules.SupportedSchemaVersion)
	stop()
	if err != nil {
		rep.Fatal(err)
		return models.ScanResult{}, err
	}
	summary := res.SHA
	if res.FromCache {
		summary = res.SHA + " (cached, offline)"
		log.Verbosef("rules: resolved %s from %s (cached, offline — could not fetch newer)", res.SHA, res.RepoURL)
	} else {
		log.Verbosef("rules: resolved %s from %s", res.SHA, res.RepoURL)
	}
	if res.SchemaNewer {
		summary += " (newer schema)"
	}
	if res.Stale {
		summary += " (stale)"
	}
	rep.EndPhase(summary)

	cfg.RulesFS = res.FS
	cfg.RulesSource = res.RepoURL
	cfg.RulesVersion = res.SHA
	cfg.RulesFromCache = res.FromCache
	cfg.RulesStale = res.Stale
	cfg.RulesSchemaVersion = res.SchemaVersion
	cfg.RulesSchemaNewer = res.SchemaNewer
	cfg.RulesOrigin = origin
	cfg.VulnScan = f.vulnScan
	cfg.VulnNoUpdate = f.noRulesUpdate // --no-rules-update is the offline switch for both rules and the OSV DB
	cfg.Progress = rep

	result, err := scanner.Run(*cfg)
	if err != nil {
		rep.Fatal(err)
		return models.ScanResult{}, err
	}
	return result, nil
}

// finishScan turns a job outcome into output + the process exit code.
func finishScan(result models.ScanResult, jobErr error, f scanFlags, log *logx.Logger, tel *telemetry.Client, startTime time.Time) error {
	durationMs := time.Since(startTime).Milliseconds()
	// Compute exit code once; reused by the telemetry Track call and the return
	// path below. When jobErr != nil these paths return early and the value is
	// unused, but computing it unconditionally keeps the logic in one place.
	scanExitCode := exitCode(result, f.strict)

	if tel != nil && jobErr != nil {
		errCategory := categorizeScanError(jobErr)
		tel.Track("scan.failed", map[string]any{
			"error_category": errCategory,
			"phase":          failurePhase(errCategory),
			"duration_ms":    durationMs,
			"rules_sha":      result.RulesVersion,
			"schema_version": result.RulesSchemaVersion,
			"exit_code":      2,
		})
	} else if tel != nil {
		// Aggregate findings.
		bySeverity := map[string]int{}
		ruleIDsFired := map[string]int{}
		for _, finding := range result.Findings {
			bySeverity[string(finding.Severity)]++
			ruleIDsFired[finding.RuleID]++
		}

		// Convert SDK and language slices to []string.
		sdks := make([]string, len(result.SDKs))
		for i, s := range result.SDKs {
			sdks[i] = string(s)
		}
		langs := make([]string, len(result.Languages))
		for i, l := range result.Languages {
			langs[i] = string(l)
		}

		tel.Track("scan.completed", map[string]any{
			"duration_ms":          durationMs,
			"repo_size_bucket":     repoSizeBucket(result.Manifest),
			"sdks_detected":        sdks,
			"languages_detected":   langs,
			"tools_count":          len(result.Tools),
			"agents_count":         len(result.Agents),
			"findings_by_severity": bySeverity,
			"rule_ids_fired":       ruleIDsFired,
			"rules_sha":            result.RulesVersion,
			"schema_version":       result.RulesSchemaVersion,
			"exit_code":            scanExitCode,
			"features_used":        scanFeaturesUsed(f),
			"repo_id_hash":         telemetry.RepoIDHash(),
		})
	}

	if jobErr != nil {
		if errors.Is(jobErr, rules.ErrAllRulesIncompatible) {
			fmt.Fprintf(os.Stderr,
				"Every rule in the resolved pack requires a newer Trustabl than this "+
					"build (this engine supports rule schema version up to %d).\n",
				rules.SupportedSchemaVersion)
			fmt.Fprintln(os.Stderr, "Fix it one of two ways:")
			fmt.Fprintln(os.Stderr,
				"  - Upgrade Trustabl to a build that supports the newer schema, or")
			fmt.Fprintf(os.Stderr,
				"  - Pin an older rules branch or tag whose pack targets schema <=%d:\n"+
					"      trustabl scan <path> --rules-ref <branch-or-tag>\n"+
					"    (--rules-ref resolves branches and tags only, not commit SHAs,\n"+
					"     so a compatible branch or tag must exist in the rules repo).\n",
				rules.SupportedSchemaVersion)
			return exitCodeError{2}
		}
		if errors.Is(jobErr, rulesource.ErrNoCompatibleRules) {
			fmt.Fprintln(os.Stderr,
				"The resolved rule pack has no usable schema manifest (it may be "+
					"corrupt or truncated).")
			fmt.Fprintln(os.Stderr,
				`Run "trustabl rules pull" to refresh the rule packs.`)
			return exitCodeError{2}
		}
		if errors.Is(jobErr, rulesource.ErrNoRules) {
			fmt.Fprintln(os.Stderr,
				"No usable rules found: none cached locally and none could be fetched.")
			fmt.Fprintln(os.Stderr,
				`Run "trustabl rules pull" to pre-warm the signed channel cache for offline use, `+
					`or pass --rules-source git to resolve from the unsigned git source.`)
			return exitCodeError{2}
		}
		if errors.Is(jobErr, rules.ErrNoRulesInPack) {
			fmt.Fprintln(os.Stderr,
				"No usable rules: the resolved rule pack contains zero rules.")
			fmt.Fprintln(os.Stderr,
				`The rules repository may be empty or truncated. Run "trustabl rules pull" to refresh.`)
			return exitCodeError{2}
		}
		if errors.Is(jobErr, rulesource.ErrNoTrustKeys) {
			fmt.Fprintln(os.Stderr,
				"This build of Trustabl embeds no rule-signing keys, so the signed rules "+
					"channel cannot be verified. A released binary always embeds them, so an "+
					"empty keyring indicates a broken or custom build.")
			fmt.Fprintln(os.Stderr,
				"As a workaround, use --rules-source git (or pass --rules-repo / --rules-ref) "+
					"to resolve rules from the unsigned git source instead.")
			return exitCodeError{2}
		}
		if isRuleSignFailure(jobErr) {
			fmt.Fprintln(os.Stderr,
				"Refusing to scan: the signed rules channel failed verification.")
			fmt.Fprintf(os.Stderr, "  %v\n", jobErr)
			fmt.Fprintln(os.Stderr,
				"Trustabl will not run unofficial, tampered, stale, or rolled-back rules. "+
					"Check the --channel value, or omit it to use the default rules source.")
			return exitCodeError{2}
		}
		// A generic error was already surfaced to the user by the reporter's
		// Fatal() in plain/tty modes (the "[phase] failed: …" line / the ✗ row).
		// Returning it raw here would make main() print "Error: …" a second
		// time. Only in silent (off) mode did nothing present it, so let main
		// be the single presenter there.
		if pickScanMode(f, log) != progress.ModeOff {
			return exitCodeError{2}
		}
		return jobErr
	}

	// In silent mode (JSON or --no-progress) the "(cached, offline)" rules phase
	// line is suppressed, so surface a cache fallback as a stderr warning — stale
	// rules should never be used without a human-visible signal. A stale bundle
	// (its signed-channel statement has expired) gets a louder, distinct message
	// and takes precedence over the plain cache-fallback warning.
	switch {
	case result.RulesStale && pickScanMode(f, log) == progress.ModeOff:
		fmt.Fprintf(os.Stderr,
			"warning: using cached rules %s whose signed channel statement has expired; the rules may be out of date. Run 'trustabl rules pull' when back online.\n",
			result.RulesVersion)
	case result.RulesFromCache && pickScanMode(f, log) == progress.ModeOff:
		fmt.Fprintf(os.Stderr,
			"warning: using cached rules %s; could not fetch or use newer rules\n",
			result.RulesVersion)
	}

	// A rules pack newer than this build — or any forward-incompatible rules it
	// carried — means some rules were skipped. Surface it on stderr so a degraded
	// scan never reads as a complete one; stdout stays machine-clean.
	if result.RulesSchemaNewer || len(result.RulesSkipped) > 0 {
		if result.RulesSchemaNewer {
			fmt.Fprintf(os.Stderr,
				"warning: the rules target schema version %d but this Trustabl build supports up to %d; %d rule(s) newer than this build were skipped. Upgrade Trustabl to evaluate them.\n",
				result.RulesSchemaVersion, rules.SupportedSchemaVersion, len(result.RulesSkipped))
		} else {
			fmt.Fprintf(os.Stderr,
				"warning: %d rule(s) were skipped because they use a scope, applies_to value, or predicate this Trustabl build does not understand.\n",
				len(result.RulesSkipped))
		}
		const maxShownRules = 10
		for i, id := range result.RulesSkipped {
			if i == maxShownRules {
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(result.RulesSkipped)-maxShownRules)
				break
			}
			fmt.Fprintf(os.Stderr, "  skipped rule: %s\n", id)
		}
	}

	// Incomplete parse coverage must never masquerade as a clean result. If any
	// AST-targeted file was skipped (unreadable or unparseable), say so on
	// stderr — stdout stays machine-clean for JSON/SARIF consumers.
	if skipped := result.Coverage.FilesSkipped; skipped > 0 {
		total := result.Coverage.FilesParsed + skipped
		fmt.Fprintf(os.Stderr,
			"warning: %d of %d source files could not be parsed and were skipped; findings may be incomplete\n",
			skipped, total)
		// Name the skipped files (stderr only) so the warning is actionable, not
		// just a count. Cap the list so a pathological repo can't flood stderr.
		const maxShown = 10
		names := result.Coverage.SkippedFiles
		for i, n := range names {
			if i == maxShown {
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(names)-maxShown)
				break
			}
			fmt.Fprintf(os.Stderr, "  skipped: %s\n", n)
		}
	}

	report, err := renderReport(result, f)
	if err != nil {
		return err
	}
	if err := writeReport(report, f.output); err != nil {
		return err
	}
	if f.output != "" {
		log.Verbosef("output: wrote %s report to %s", f.format, f.output)
	}

	// --json-out / --sarif-out persist the respective format to a file
	// independent of --format, so one scan can print the human panel to stdout
	// while writing machine artifacts. The bytes are identical to the matching
	// --format stdout output (shared renderers).
	if err := writeSideOutputs(result, f); err != nil {
		return err
	}
	if f.jsonOut != "" {
		log.Verbosef("output: wrote JSON to %s", f.jsonOut)
	}
	if f.sarifOut != "" {
		log.Verbosef("output: wrote SARIF to %s", f.sarifOut)
	}

	// --attest signs the JSON report into a cosign attestation. The subject must be
	// a persisted file (cosign signs bytes on disk and a verifier re-supplies the
	// same file), so reuse --json-out when set, else write the canonical report to a
	// default path with the identical bytes (jsonBytes). A signing failure here is
	// its own exit path — it must not masquerade as a findings exit, so we return it
	// before computing the findings exit code below.
	if f.attest {
		reportPath := f.jsonOut
		if reportPath == "" {
			reportPath = "trustabl-report.json"
			b, err := jsonBytes(result)
			if err != nil {
				return err
			}
			if err := os.WriteFile(reportPath, b, 0o644); err != nil {
				return fmt.Errorf("write %s for --attest: %w", reportPath, err)
			}
		}
		if err := doAttest(result, reportPath, attestFlags{
			keyRef:       f.attestKey,
			bundleOut:    f.attestBundle,
			predicateOut: defaultAttestPredicate,
			noTLog:       f.attestNoTLog,
		}, log); err != nil {
			return err
		}
	}

	code := scanExitCode
	log.Verbosef("result: scan_id %s · overall %.0f%% · %d findings (%s) · exit %d",
		result.ScanID, result.OverallScore*100, len(result.Findings), severitySummary(result.Findings), code)
	if code != 0 {
		return exitCodeError{code}
	}
	return nil
}

// severitySummary renders a deterministic, human-legible severity histogram for
// a finding set, e.g. "2 high, 1 medium, 3 info" — highest severity first, zero
// tiers omitted. Used in the verbose result line. "none" for an empty set.
func severitySummary(findings []models.Finding) string {
	counts := map[models.Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	order := []models.Severity{
		models.SeverityCritical, models.SeverityHigh, models.SeverityMedium,
		models.SeverityLow, models.SeverityInfo,
	}
	var parts []string
	for _, s := range order {
		if n := counts[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, s))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// renderReport turns a ScanResult into the report bytes for the chosen format.
// Rendering is decoupled from the write destination so the same bytes go to
// stdout or to --output unchanged, keeping the JSON/SARIF byte-stability
// contract regardless of where the report lands.
func renderReport(result models.ScanResult, f scanFlags) ([]byte, error) {
	switch f.format {
	case "json":
		return jsonBytes(result)
	case "sarif":
		return sarif.Render(result, version), nil
	case "human", "":
		r := &review.Renderer{NoColor: f.noColor}
		return []byte(r.Render(result)), nil
	default:
		return nil, fmt.Errorf("unknown --format %q", f.format)
	}
}

// jsonBytes renders the ScanResult as the canonical pretty JSON document
// (trailing newline). Shared by renderReport (--format json) and writeSideOutputs
// (--json-out) so all JSON output is byte-identical.
func jsonBytes(result models.ScanResult) ([]byte, error) {
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// writeReport sends the rendered report to stdout, or to path when --output is
// set. The report is fully materialized before the file is opened, so a render
// error never leaves a half-written file behind.
func writeReport(report []byte, path string) error {
	if path == "" {
		_, err := os.Stdout.Write(report)
		return err
	}
	if err := os.WriteFile(path, report, 0o644); err != nil {
		return fmt.Errorf("writing report to %s: %w", path, err)
	}
	return nil
}

// writeSideOutputs honors --json-out / --sarif-out / --bom-out, writing each to
// its file when the flag is set. No-op when all are empty.
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
	if f.bomOut != "" {
		// result.Vulnerabilities is populated only under --vuln-scan; otherwise nil,
		// so the BOM stays a pure inventory. When present, they ride along as a
		// CycloneDX VEX vulnerabilities[] array linked to the affected components.
		if err := os.WriteFile(f.bomOut, cyclonedx.Render(result.Dependencies, result.Vulnerabilities, version), 0o644); err != nil {
			return fmt.Errorf("write --bom-out: %w", err)
		}
	}
	return nil
}

func exitCode(result models.ScanResult, strict bool) int {
	for _, f := range result.Findings {
		switch f.Severity {
		case models.SeverityMedium, models.SeverityHigh, models.SeverityCritical:
			return 1
		case models.SeverityLow:
			// --strict tightens the gate to any genuine finding, but still floors
			// at low. info/META signals (an opaque agent, an unused dep, an
			// unaudited SDK) are not defects, so they must not fail a --strict CI
			// run on an otherwise-clean repo.
			if strict {
				return 1
			}
		}
	}
	return 0
}

// defaultRulesSource is the rules source used when the operator selects none.
// The signed-production cutover (ENG-6) flipped it from "git" to "production": a
// plain `trustabl scan` now resolves the signature-verified production channel,
// and the unsigned git path is the explicit opt-out (--rules-source git, or any
// --rules-repo / --rules-ref / TRUSTABL_RULES_REPO, which effectiveRules already
// routes to git). The flip is safe only because the embedded keyring is populated
// and channel-production has a published, floor-pinned statement — the
// TestDefaultRulesSource_CutoverHasGenesisFloor guard fails the build otherwise.
const defaultRulesSource = "production"

// effectiveRules resolves the scan's rules source from flags into BOTH a
// rulesource.Config and a models.RulesOrigin. Deriving them from one decision is
// the point: the source that actually resolves and the provenance reported in the
// watermark and folded into ScanID can never disagree. Precedence:
//
//   - An explicit --rules-source (or its deprecated alias --channel) wins;
//     conflicting values are an error.
//   - With no explicit selection, a git-only override (--rules-repo / --rules-ref
//     / TRUSTABL_RULES_REPO) implies the git path; otherwise defaultRulesSource.
//   - "git" is the unsigned official/custom git path; any other token is a signed
//     channel name. A signed channel keeps --rules-repo (to test a signed fork)
//     but rejects --rules-ref (a git-only concept).
func effectiveRules(f scanFlags) (rulesource.Config, models.RulesOrigin, error) {
	repo := f.rulesRepo
	if repo == "" {
		repo = os.Getenv("TRUSTABL_RULES_REPO")
	}

	src := f.rulesSource
	if f.channel != "" {
		if src != "" && src != f.channel {
			return rulesource.Config{}, models.RulesOrigin{}, fmt.Errorf(
				"conflicting rules source: --rules-source %q vs --channel %q", src, f.channel)
		}
		src = f.channel
	}
	if src == "" {
		// No explicit selection: a git-only override implies git, else the default.
		if repo != "" || f.rulesRef != "" {
			src = "git"
		} else {
			src = defaultRulesSource
		}
	}

	// Hard signed-only gate: when --require-signed (or TRUSTABL_REQUIRE_SIGNED=1) is
	// set, refuse the unsigned git path entirely rather than silently scanning with
	// unverified rules — the fail-closed switch a security-conscious CI wants.
	if (f.requireSigned || os.Getenv("TRUSTABL_REQUIRE_SIGNED") == "1") && src == "git" {
		return rulesource.Config{}, models.RulesOrigin{}, fmt.Errorf(
			"signed rules required (--require-signed / TRUSTABL_REQUIRE_SIGNED=1) but the resolved source is the unsigned git path; pass --rules-source <channel>")
	}
	if src == "git" {
		return rulesource.Config{
			RepoURL:  repo,
			Ref:      f.rulesRef,
			NoUpdate: f.noRulesUpdate,
		}, models.RulesOrigin{Custom: repo != ""}, nil
	}

	// Signed channel path.
	if !isValidChannelName(src) {
		return rulesource.Config{}, models.RulesOrigin{}, fmt.Errorf(
			"invalid rules channel %q (allowed: lowercase letters, digits, '.', '_', '-')", src)
	}
	if f.rulesRef != "" {
		return rulesource.Config{}, models.RulesOrigin{}, fmt.Errorf(
			"--rules-ref has no effect on the signed %q channel; use --rules-source git to pin a git ref", src)
	}
	return rulesource.Config{
		RepoURL:  repo, // a non-empty repo here is a signed fork: --rules-source <chan> --rules-repo <fork>
		Channel:  src,
		NoUpdate: f.noRulesUpdate,
		// A signed channel from a non-default repo is signature-verified but not the
		// official source — mark it Custom so it is watermarked and gets a distinct
		// ScanID (a fork can replay an old validly-signed statement).
	}, models.RulesOrigin{Signed: true, Channel: src, Custom: repo != ""}, nil
}

// isValidChannelName mirrors the engine's channel-name rule (rulesign
// channelstate.validChannelName) so a bad channel fails early at the CLI with a
// clear message rather than late, deep in the resolver.
func isValidChannelName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// isRuleSignFailure reports whether err is a signed-channel verification failure
// — a bad signature, an untrusted/expired key, channel confusion, an expired or
// rolled-back statement, a digest mismatch, or a malformed statement. These are
// refusals, not transient errors: the engine must not fall back to running
// unverified rules.
func isRuleSignFailure(err error) bool {
	return errors.Is(err, rulesign.ErrBadSignature) ||
		errors.Is(err, rulesign.ErrUnknownKeyID) ||
		errors.Is(err, rulesign.ErrKeyExpired) ||
		errors.Is(err, rulesign.ErrKeyNotYetValid) ||
		errors.Is(err, rulesign.ErrChannelMismatch) ||
		errors.Is(err, rulesign.ErrStatementExpired) ||
		errors.Is(err, rulesign.ErrVersionRegression) ||
		errors.Is(err, rulesign.ErrDigestMismatch) ||
		errors.Is(err, rulesign.ErrStatementMalformed)
}

// repoSizeBucket classifies a repo's file count into a coarse bucket.
// Thresholds: small < 20, medium < 200, large >= 200.
func repoSizeBucket(m models.ScanManifest) string {
	total := len(m.PythonFiles) + len(m.TypeScriptFiles) + len(m.JavaScriptFiles) +
		len(m.GoFiles) + len(m.YAMLFiles) + len(m.JSONFiles) + len(m.MarkdownFiles) +
		len(m.CSharpFiles) + len(m.PHPFiles) + len(m.RustFiles)
	switch {
	case total < 20:
		return "small"
	case total < 200:
		return "medium"
	default:
		return "large"
	}
}

// scanFeaturesUsed returns a list of optional feature names that were activated
// by the given flags. Only features the user explicitly enabled are listed.
func scanFeaturesUsed(f scanFlags) []string {
	var features []string
	if f.attest {
		features = append(features, "attest")
	}
	if f.vulnScan {
		features = append(features, "vuln_scan")
	}
	if f.sarifOut != "" {
		features = append(features, "sarif_out")
	}
	if f.jsonOut != "" {
		features = append(features, "json_out")
	}
	if f.bomOut != "" {
		features = append(features, "bom_out")
	}
	if f.noRulesUpdate {
		features = append(features, "no_rules_update")
	}
	return features
}

// failurePhase maps an error_category to the pipeline phase where it occurred.
func failurePhase(category string) string {
	switch category {
	case "rules_fetch_failed", "no_rules":
		return "rules"
	case "clone_failed":
		return "clone"
	case "parse_error":
		return "inventory"
	default:
		return "unknown"
	}
}

// categorizeScanError maps a scan error to the closed error_category enum.
// err.Error() is used internally for pattern matching only — the raw string
// never reaches PostHog; only the closed label is forwarded.
func categorizeScanError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case isRulesFetchError(err, msg):
		return "rules_fetch_failed"
	case isCloneError(err, msg):
		return "clone_failed"
	case isNoRulesError(err, msg):
		return "no_rules"
	case isParseError(err, msg):
		return "parse_error"
	default:
		return "unknown"
	}
}

func isRulesFetchError(_ error, msg string) bool {
	// rulesource errors mention "fetch", "resolve", or "clone" in the context
	// of the rules repo — check for the rulesource package sentinel strings.
	return containsAny(msg, "fetch rules", "resolve rules", "rulesource")
}

func isCloneError(_ error, msg string) bool {
	return containsAny(msg, "clone", "git clone", "cloning")
}

func isNoRulesError(_ error, msg string) bool {
	return containsAny(msg, "no rules", "no usable rules", "no compatible rules",
		"no rules in pack", "all rules incompatible")
}

func isParseError(_ error, msg string) bool {
	return containsAny(msg, "parse error", "ast error", "tree-sitter", "syntax error", "failed to parse")
}

func containsAny(s string, needles ...string) bool {
	sl := strings.ToLower(s)
	for _, n := range needles {
		if strings.Contains(sl, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

func parseCategories(s string) ([]models.DetectorCategory, error) {
	var out []models.DetectorCategory
	for _, raw := range strings.Split(s, ",") {
		c := models.DetectorCategory(strings.TrimSpace(raw))
		if !models.ValidCategory(c) {
			return nil, fmt.Errorf("unknown detector category %q (allowed: %s)", c, categoryList())
		}
		out = append(out, c)
	}
	return out, nil
}

// categoryList renders the recognized detector categories as a comma-separated
// string, sourced from models.AllCategories so the --detectors help text and the
// validation error never drift from what the engine actually recognizes.
func categoryList() string {
	names := make([]string, len(models.AllCategories))
	for i, c := range models.AllCategories {
		names[i] = string(c)
	}
	return strings.Join(names, ", ")
}
