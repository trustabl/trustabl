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
//	2  scanner / I/O error
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/review"
	"github.com/trustabl/trustabl/internal/rules"
	"github.com/trustabl/trustabl/internal/rulesource"
	"github.com/trustabl/trustabl/internal/scanner"
)

var version = "0.1.0"

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
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("Trustabl", version)
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
		"comma-separated detector categories: claude_sdk, openai_sdk, openshell (default: all)")
	cmd.Flags().StringVar(&f.format, "format", "human",
		"output format: human|json")
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

	// Resolve detection rules from the external rules repository.
	res, err := rulesource.Resolve(rulesConfigFromScan(f), rules.SupportedSchemaVersion)
	if err != nil {
		if errors.Is(err, rulesource.ErrNoRules) || errors.Is(err, rulesource.ErrNoCompatibleRules) {
			fmt.Fprintln(os.Stderr,
				"No usable rules found locally and could not fetch from the rules repository.")
			fmt.Fprintln(os.Stderr,
				`Run "trustabl rules pull" to download the rule packs.`)
			return exitCodeError{2}
		}
		return fmt.Errorf("resolve rules: %w", err)
	}
	if res.FromCache {
		fmt.Fprintf(os.Stderr,
			"warning: using cached rules %s; could not fetch or use newer rules from %s\n",
			res.SHA, res.RepoURL)
	}
	cfg.RulesFS = res.FS
	cfg.RulesSource = res.RepoURL
	cfg.RulesVersion = res.SHA
	cfg.RulesFromCache = res.FromCache

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
		case models.CategoryClaudeSDK, models.CategoryOpenAISDK, models.CategoryOpenShell:
			out = append(out, c)
		default:
			return nil, fmt.Errorf("unknown detector category %q (allowed: claude_sdk, openai_sdk, openshell)", c)
		}
	}
	return out, nil
}
