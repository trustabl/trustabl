// Command karenctl is the CLI entry point.
//
// Subcommands:
//
//	karenctl scan <target> [flags]   primary command: scan a repo
//	karenctl version                 print version
//
// Exit codes:
//
//	0  no findings ≥ medium
//	1  findings ≥ medium present (or scan completed with findings + --strict)
//	2  scanner / I/O error
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trustabl/karenctl/internal/models"
	"github.com/trustabl/karenctl/internal/review"
	"github.com/trustabl/karenctl/internal/scanner"
)

var version = "0.1.0-skeleton"

// exitCodeError carries a desired process exit code through the cobra error
// path so we can avoid calling os.Exit inside runScan (which would skip any
// deferred cleanup added in the future).
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return "" }

func main() {
	rootCmd := &cobra.Command{
		Use:   "karenctl",
		Short: "Static analyzer for agent reliability",
		Long: "karenctl scans Claude Agent SDK repos for reliability weaknesses\n" +
			"and emits committable hook configs + OpenShell sandbox policies.\n" +
			"\n" +
			"NOTE: 'karenctl' is a temporary product name. Rename before this leaks\n" +
			"into commit history / package metadata.",
		SilenceUsage:  true,
		SilenceErrors: true, // we handle error printing ourselves below
	}
	rootCmd.AddCommand(newScanCommand())
	rootCmd.AddCommand(newVersionCommand())

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
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("karenctl", version)
		},
	}
}

// ────────────────────────────────────────────────────────────────────────────
// scan
// ────────────────────────────────────────────────────────────────────────────

type scanFlags struct {
	detectors string
	format    string
	apply     bool
	export    string
	yes       bool
	overwrite bool
	strict    bool
	noColor   bool
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
		"comma-separated detector categories: claude_sdk, openshell (default: all)")
	cmd.Flags().StringVar(&f.format, "format", "human",
		"output format: human|json")
	cmd.Flags().BoolVar(&f.apply, "apply", false,
		"write generated artifacts into the target repo")
	cmd.Flags().StringVar(&f.export, "export", "",
		"write generated artifacts to a ZIP at this path")
	cmd.Flags().BoolVar(&f.yes, "yes", false,
		"skip the apply-confirmation prompt")
	cmd.Flags().BoolVar(&f.overwrite, "overwrite", false,
		"allow --apply to overwrite existing files")
	cmd.Flags().BoolVar(&f.strict, "strict", false,
		"exit 1 if any finding is present, regardless of severity")
	cmd.Flags().BoolVar(&f.noColor, "no-color", false,
		"disable colored output")
	return cmd
}

func runScan(target string, f scanFlags) error {
	cfg := scanner.Config{Target: target, Version: version}
	if f.detectors != "" {
		cats, err := parseCategories(f.detectors)
		if err != nil {
			return err
		}
		cfg.Categories = cats
	}

	result, err := scanner.Run(cfg)
	if err != nil {
		return err
	}

	// Output.
	switch f.format {
	case "json":
		if err := emitJSON(result); err != nil {
			return err
		}
	case "human", "":
		r := &review.Renderer{NoColor: f.noColor}
		fmt.Print(r.Render(result))
	default:
		return fmt.Errorf("unknown --format %q", f.format)
	}

	// Apply side effects.
	if f.apply {
		if !f.yes && !confirm(
			fmt.Sprintf("Write %d artifact(s) to %s?", len(result.GeneratedArtifacts), target),
		) {
			fmt.Fprintln(os.Stderr, "Apply cancelled.")
			// Intentional fall-through: --export still runs, and exit code
			// is still based on findings severity.
		} else {
			// For remote scans, the temp dir is already gone; apply only
			// makes sense for local targets.
			if result.Manifest.IsRemote {
				return fmt.Errorf("--apply requires a local target (remote source is cleaned up)")
			}
			if err := review.ApplyArtifacts(result.Manifest.RepoRoot,
				result.GeneratedArtifacts, f.overwrite); err != nil {
				return fmt.Errorf("apply: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Wrote %d artifact(s).\n", len(result.GeneratedArtifacts))
		}
	}
	if f.export != "" {
		if err := review.ExportZIP(f.export, result.GeneratedArtifacts); err != nil {
			return fmt.Errorf("export: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Wrote bundle to %s\n", f.export)
	}

	if code := exitCode(result, f.strict); code != 0 {
		return exitCodeError{code}
	}
	return nil
}

func emitJSON(result models.ScanResult) error {
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(b, '\n'))
	return err
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

func parseCategories(s string) ([]models.DetectorCategory, error) {
	var out []models.DetectorCategory
	for _, raw := range strings.Split(s, ",") {
		c := models.DetectorCategory(strings.TrimSpace(raw))
		switch c {
		case models.CategoryClaudeSDK, models.CategoryOpenShell:
			out = append(out, c)
		default:
			return nil, fmt.Errorf("unknown detector category %q (allowed: claude_sdk, openshell)", c)
		}
	}
	return out, nil
}

func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", prompt)
	var resp string
	_, _ = fmt.Fscanln(os.Stdin, &resp)
	resp = strings.ToLower(strings.TrimSpace(resp))
	return resp == "y" || resp == "yes"
}
