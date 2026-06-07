// Command trustabl is the CLI entry point.
//
// Subcommands:
//
//	trustabl scan <target> [flags]   primary command: scan a repo
//	trustabl mcp [flags]             run a stdio MCP server exposing the scan
//	trustabl version                 print version
//
// Exit codes:
//
//	0  no findings ≥ medium
//	1  findings ≥ medium present (or findings ≥ low with --strict; info/META
//	   findings never raise the exit code)
//	2  scanner / I/O error, or no usable rules (none resolved, incompatible
//	   schema, or a resolved pack that contains zero rules)
//
// Each subcommand lives in its own file (scan.go, version.go, rules.go, mcp.go,
// llm.go). This file owns the root command wiring, the build metadata, and the
// shared exit-code error type.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/logx"
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
	// Persistent (global) diagnostics flags, inherited by every subcommand. All
	// diagnostics go to stderr only, so they never perturb the byte-stable report
	// on stdout. --debug implies --verbose (see logLevelFor).
	rootCmd.PersistentFlags().BoolP("verbose", "v", false,
		"verbose diagnostics on stderr: rule provenance, discovery counts, phase summaries")
	rootCmd.PersistentFlags().Bool("debug", false,
		"debug diagnostics on stderr: everything --verbose shows plus per-phase timing and per-entity/per-finding detail (implies --verbose)")
	rootCmd.AddCommand(newScanCommand())
	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(newRulesCommand())
	rootCmd.AddCommand(newMCPCommand())
	rootCmd.AddCommand(newLLMCommand())
	rootCmd.AddCommand(newCapabilitiesCommand())

	if err := rootCmd.Execute(); err != nil {
		var ec exitCodeError
		if errors.As(err, &ec) {
			os.Exit(ec.code) // findings-based exit; message already printed
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}
}

// logLevelFor reads the persistent --verbose / --debug flags off cmd (they are
// defined on the root command and inherited by every subcommand) and maps them
// to a logx.Level. --debug wins: it is strictly more output than --verbose, so
// it implies it.
func logLevelFor(cmd *cobra.Command) logx.Level {
	if debug, _ := cmd.Flags().GetBool("debug"); debug {
		return logx.LevelDebug
	}
	if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
		return logx.LevelVerbose
	}
	return logx.LevelNormal
}

// refOrDefault renders a rules ref for a diagnostic line, naming the empty ref
// (resolve the repo's default branch) explicitly so the line is not blank.
func refOrDefault(ref string) string {
	if ref == "" {
		return "default branch"
	}
	return ref
}
