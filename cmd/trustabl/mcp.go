package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/mcpserver"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/progress"
	"github.com/trustabl/trustabl/internal/rules"
	"github.com/trustabl/trustabl/internal/rulesource"
	"github.com/trustabl/trustabl/internal/scanner"
	"github.com/trustabl/trustabl/internal/telemetry"
)

// mcpFlags configure the `trustabl mcp` server. They mirror the rules-source
// knobs of `scan`, since rule resolution is identical; the server has no output
// format (it speaks the MCP protocol on stdout) and no progress (which would
// corrupt that stream).
type mcpFlags struct {
	rulesRepo     string
	rulesRef      string
	rulesSource   string
	channel       string
	noRulesUpdate bool
}

// newMCPCommand wires the `mcp` subcommand: a stdio MCP server that exposes
// Trustabl's scan to MCP clients (Claude Code, Cursor, Claude Desktop). It is a
// frontend over the same scanner core as `scan` — it reuses rule resolution and
// scanner.Run, and serializes the deterministic ScanResult onto the protocol
// stream itself.
func newMCPCommand(tel *telemetry.Client) *cobra.Command {
	var f mcpFlags
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run a stdio MCP server exposing Trustabl's scan",
		Long: "Run a Model Context Protocol (MCP) server over stdio so an MCP client\n" +
			"(Claude Code, Cursor, Claude Desktop) can scan code with Trustabl. The\n" +
			"server exposes a 'scan' tool that runs the same analysis as 'trustabl scan'\n" +
			"and returns the structured result as JSON.\n\n" +
			"The MCP protocol uses stdout for its JSON-RPC stream, so this command\n" +
			"writes nothing else to stdout; diagnostics go to stderr.",
		Example: `  # Run the server (an MCP client normally launches this for you)
  trustabl mcp

  # Pin the rules ref the server resolves
  trustabl mcp --rules-ref v1.2.0

  # Example Claude Code / Cursor client config entry:
  #   "mcpServers": {
  #     "trustabl": { "command": "trustabl", "args": ["mcp"] }
  #   }`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCP(cmd.Context(), f, logLevelFor(cmd))
		},
	}
	cmd.Flags().StringVar(&f.rulesRepo, "rules-repo", "",
		"rules repository URL (default: official trustabl-rules; or TRUSTABL_RULES_REPO)")
	cmd.Flags().StringVar(&f.rulesRef, "rules-ref", "",
		"rules branch or tag to use (default: the repo's default branch)")
	cmd.Flags().StringVar(&f.rulesSource, "rules-source", "",
		"rules source: 'git' or a signed channel name (e.g. production) the server resolves")
	cmd.Flags().StringVar(&f.channel, "channel", "",
		"deprecated alias for --rules-source <channel>")
	cmd.Flags().BoolVar(&f.noRulesUpdate, "no-rules-update", false,
		"do not fetch rules; use the local cache only")
	return cmd
}

// runMCP starts the stdio MCP server. The scan handler resolves rules and runs
// scanner.Run exactly like the CLI scan path, but with progress disabled (the
// nop reporter) so nothing touches stdout. A per-call rules_ref overrides the
// command-level --rules-ref when the client supplies one.
func runMCP(ctx context.Context, f mcpFlags, level logx.Level) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Diagnostics go to stderr; stdout is the JSON-RPC stream. diagColor keeps the
	// output plain when stderr is captured into a client's server log (the common
	// case) and only dims the tag when an operator runs the server in a terminal.
	log := logx.New(os.Stderr, level, diagColor(false))
	repo := rulesRepoFromFlag(f.rulesRepo)
	if repo == "" {
		repo = rulesource.DefaultRepoURL
	}
	log.Verbosef("mcp: rules repo %s", repo)

	scan := func(ctx context.Context, req mcpserver.ScanRequest) (models.ScanResult, error) {
		ref := f.rulesRef
		if req.RulesRef != "" {
			ref = req.RulesRef
		}
		log.Debugf("mcp: scan request path=%s rules_ref=%q", req.Path, ref)
		// Resolve via the same source-selection as `scan` so the MCP frontend can
		// use a signed channel and so provenance is derived once and never drifts.
		rcfg, origin, err := effectiveRules(scanFlags{
			rulesRepo: f.rulesRepo, rulesRef: ref, rulesSource: f.rulesSource,
			channel: f.channel, noRulesUpdate: f.noRulesUpdate,
		})
		if err != nil {
			return models.ScanResult{}, err
		}
		res, err := rulesource.Resolve(rcfg, rules.SupportedSchemaVersion)
		if err != nil {
			return models.ScanResult{}, fmt.Errorf("resolve rules: %w", err)
		}
		// Progress is the nop reporter: the MCP transport owns stdout, and even
		// stderr progress would interleave noisily into a client's server log
		// for every tool call. Log is wired through, but it is silent unless the
		// operator started the server with --verbose/--debug — an explicit opt-in
		// to the per-call diagnostic stream (stderr only, never the RPC stream).
		cfg := scanner.Config{
			Target:         req.Path,
			RulesFS:        res.FS,
			RulesSource:    res.RepoURL,
			RulesVersion:   res.SHA,
			RulesFromCache: res.FromCache,
			// Carry the full provenance/freshness signals so an MCP client gets the
			// same rules_origin, stale flag, and schema-newer signal as the CLI —
			// these were previously dropped on the MCP path.
			RulesStale:         res.Stale,
			RulesSchemaVersion: res.SchemaVersion,
			RulesSchemaNewer:   res.SchemaNewer,
			RulesOrigin:        origin,
			Progress:           progress.NewNop(),
			Log:                log,
			Ctx:                ctx,
			// Opt-in OSV vulnerability matching (mirrors the CLI's --vuln-scan);
			// the OSV offline switch reuses --no-rules-update, as the CLI does.
			VulnScan:     req.VulnScan,
			VulnNoUpdate: f.noRulesUpdate,
		}
		return scanner.Run(cfg)
	}

	srv := mcpserver.New(scan, mcpserver.VersionInfo{Version: version, Commit: commit, Date: date})
	// Announce readiness on stderr (stdout is the protocol stream). Quietly
	// returning here would leave an operator staring at a blank terminal unsure
	// whether the server came up.
	fmt.Fprintln(os.Stderr, "Trustabl MCP server ready (stdio). Awaiting client on stdin.")
	return srv.Serve(ctx, os.Stdin, os.Stdout)
}

// rulesRepoFromFlag applies the TRUSTABL_RULES_REPO environment override when
// --rules-repo is unset, matching the scan command's precedence.
func rulesRepoFromFlag(flag string) string {
	if flag != "" {
		return flag
	}
	return os.Getenv("TRUSTABL_RULES_REPO")
}
