// Command trustabl is the CLI entry point.
//
// Subcommands:
//
//	trustabl scan <target> [flags]   primary command: scan a repo
//	trustabl attest <report.json>    sign a scan report into an attestation (cosign)
//	trustabl verify <report.json>    verify a scan attestation (consumer side)
//	trustabl enrich [flags]          enrich a scan result with AI fixes
//	trustabl mcp [flags]             run a stdio MCP server exposing the scan
//	trustabl rules pull [flags]      pre-fetch the detection rule packs
//	trustabl llm ...                 manage optional LLM provider config (BYOK)
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
// Each subcommand lives in its own file (scan.go, attest.go, verify.go,
// version.go, rules.go, mcp.go, llm.go). This file owns the root command wiring,
// the build metadata, and the shared exit-code error type.
package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/crash"
	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/telemetry"
)

// Build metadata, injected at release time via -ldflags -X (see .goreleaser.yaml).
// Defaults are for local `go build` — an unreleased binary truthfully reports "dev".
var (
	version       = "dev"
	commit        = "none"
	date          = "unknown"
	posthogAPIKey = "" // injected at release time via -ldflags -X main.posthogAPIKey=<key>
)

// exitCodeError carries a desired process exit code through the cobra error
// path so we can avoid calling os.Exit inside runScan (which would skip any
// deferred cleanup added in the future).
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return "" }

func main() {
	configPath, _ := telemetry.DefaultConfigPath()
	tel := telemetry.New(posthogAPIKey, version, configPath, os.Stderr, os.Stdin)
	defer tel.Flush()
	defer func() {
		if r := recover(); r != nil {
			crash.Handle(r, debug.Stack(), buildCrashMeta(), tel)
			tel.Flush()
			os.Exit(2)
		}
	}()

	rootCmd := &cobra.Command{
		Use:   "trustabl",
		Short: "Static analyzer for agent reliability",
		Long: `Trustabl is a static analyzer for AI-agent codebases.

It scans repositories that use agent SDKs — Claude Agent SDK, OpenAI Agents SDK,
Google ADK, LangChain, CrewAI, Pydantic AI, Vercel AI, and AutoGen — and Model
Context Protocol (MCP) servers, then reports reliability and safety weaknesses in
the tools, agents, and subagents it discovers. Python and TypeScript codebases
are analyzed in depth; JavaScript and Go are recognized during recon but not yet
AST-parsed.

Detection rules are not built into this binary: they are resolved from the
trustabl-rules repository at scan time and cached locally, with an offline
fallback. Run "trustabl rules pull" to pre-fetch them.

Exit codes: 0 = clean (no finding >= medium), 1 = findings >= medium (or >= low
with --strict), 2 = scanner error or no usable rules.`,
		Example: `  # Scan the current directory
  trustabl scan .

  # Scan a public GitHub repository
  trustabl scan https://github.com/owner/repo

  # Fail CI on any finding (low severity and above)
  trustabl scan . --strict

  # SARIF output for GitHub code scanning
  trustabl scan . --format sarif -o trustabl.sarif

  # Pre-download the rule packs for offline use
  trustabl rules pull`,
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
	rootCmd.AddCommand(newScanCommand(tel))
	rootCmd.AddCommand(newAttestCommand(tel))
	rootCmd.AddCommand(newVerifyCommand(tel))
	rootCmd.AddCommand(newVersionCommand(tel))
	rootCmd.AddCommand(newRulesCommand(tel))
	rootCmd.AddCommand(newVulnDBCommand(tel))
	rootCmd.AddCommand(newMCPCommand(tel))
	rootCmd.AddCommand(newLLMCommand())
	rootCmd.AddCommand(newEnrichCommand(tel))
	rootCmd.AddCommand(newCapabilitiesCommand(tel))
	rootCmd.AddCommand(newTelemetryCommand())

	if err := rootCmd.Execute(); err != nil {
		var ec exitCodeError
		if errors.As(err, &ec) {
			tel.Flush()
			os.Exit(ec.code) // findings-based exit; message already printed
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		tel.Flush()
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

// buildCrashMeta assembles build/runtime context for a crash report. RulesSHA is
// left empty here — it is not resolved until mid-scan.
func buildCrashMeta() crash.Meta {
	return crash.Meta{
		Version: version,
		Commit:  commit,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
}
