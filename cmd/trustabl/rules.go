package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/rules"
	"github.com/trustabl/trustabl/internal/rulesource"
	"github.com/trustabl/trustabl/internal/telemetry"
)

func newRulesCommand(tel *telemetry.Client) *cobra.Command {
	rulesCmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage Trustabl's detection rules",
		Long: `Manage Trustabl's detection rules.

Rules live in the external trustabl-rules repository and are resolved
automatically the first time you scan, then cached under your user cache
directory (keyed by commit, with an offline fallback). You normally never run
these commands — a scan fetches what it needs — but "rules pull" lets you
pre-fetch the packs so a later scan can run offline.`,
		Example: `  # Pre-download the rule packs into the local cache
  trustabl rules pull`,
	}

	var repo, ref, rulesSource, channel string
	pull := &cobra.Command{
		Use:   "pull",
		Short: "Download the detection rule packs into the local cache",
		Long: `Download the detection rule packs into the local cache so later scans can run
offline. By default this fetches the official trustabl-rules repository's default
branch; override the source with --rules-repo / --rules-ref, or set the
TRUSTABL_RULES_REPO environment variable. Pass --rules-source <channel> to instead
pre-warm a signed channel's bundle cache, so a later signed scan can run offline.`,
		Example: `  # Pull the official rules (default branch)
  trustabl rules pull

  # Pull a specific tag or branch
  trustabl rules pull --rules-ref v1.2.0

  # Pre-warm a signed channel for offline signed scans
  trustabl rules pull --rules-source production

  # Pull from a fork or mirror
  trustabl rules pull --rules-repo https://github.com/me/my-rules`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			log := logx.New(os.Stderr, logLevelFor(cmd), diagColor(false))
			// Reuse the scan command's source-selection so pull and scan resolve the
			// SAME source for the same flags — a signed channel routes to the signed
			// release path, the git default stays git.
			cfg, _, err := effectiveRules(scanFlags{rulesRepo: repo, rulesRef: ref, rulesSource: rulesSource, channel: channel})
			if err != nil {
				return err
			}
			label := cfg.RepoURL
			if label == "" {
				label = rulesource.DefaultRepoURL
			}
			if cfg.Channel != "" {
				log.Verbosef("rules pull: signed channel %q from %s", cfg.Channel, label)
			} else {
				log.Verbosef("rules pull: fetching %s @ %s", label, refOrDefault(cfg.Ref))
			}
			defer log.Timer("rules pull")()
			res, err := rulesource.Pull(cfg, rules.SupportedSchemaVersion)
			if err != nil {
				if errors.Is(err, rulesource.ErrNoCompatibleRules) {
					return fmt.Errorf("the rules at the requested ref are newer than this "+
						"Trustabl build can evaluate (this engine supports rule schema "+
						"version up to %d); upgrade Trustabl or pull an older --rules-ref",
						rules.SupportedSchemaVersion)
				}
				return fmt.Errorf("rules pull: %w", err)
			}
			log.Verbosef("rules pull: resolved %s from %s", res.SHA, res.RepoURL)
			if cfg.Channel != "" {
				fmt.Printf("Pulled signed %q channel from %s at %s\n", cfg.Channel, res.RepoURL, res.SHA)
			} else {
				fmt.Printf("Pulled rules from %s at %s\n", res.RepoURL, res.SHA)
			}
			return nil
		},
	}
	pull.Flags().StringVar(&repo, "rules-repo", "",
		"rules repository URL (default: official trustabl-rules; or TRUSTABL_RULES_REPO)")
	pull.Flags().StringVar(&ref, "rules-ref", "",
		"rules branch or tag to pull (default: the repo's default branch)")
	pull.Flags().StringVar(&rulesSource, "rules-source", "",
		"rules source: 'git' or a signed channel name (e.g. production) to pre-warm the signed bundle cache")
	pull.Flags().StringVar(&channel, "channel", "",
		"deprecated alias for --rules-source <channel>")

	validate := &cobra.Command{
		Use:   "validate [dir]",
		Short: "Validate a local rule-pack directory against this build's schema",
		Long: `Strict-load every rule pack under [dir] (default ".") and fail on the first
schema, parse, duplicate-ID, missing-field, out-of-range-confidence, or
unknown-predicate error. Unlike a scan it fetches nothing — it validates the
rules already on disk against this Trustabl build's rule schema.

This is the CI gate for the trustabl-rules repository: build the engine at a
known ref and run "trustabl rules validate ." against a checkout of the rules.
Strict loading means a rule that targets a newer schema than this build (a new
predicate not yet in the engine) fails here, which enforces the right ordering —
the engine ships the predicate before the rules repo ships rules that use it.`,
		Example: `  # Validate the rule packs in the current directory
  trustabl rules validate

  # Validate a checkout elsewhere
  trustabl rules validate ./trustabl-rules`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			policies, err := rules.Load(os.DirFS(dir))
			if err != nil {
				return fmt.Errorf("rules validate %s: %w", dir, err)
			}
			nRules := 0
			for _, p := range policies {
				nRules += len(p.Rules)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK: %d rule pack(s), %d rule(s) valid under rule schema version %d\n",
				len(policies), nRules, rules.SupportedSchemaVersion)
			return nil
		},
	}

	rulesCmd.AddCommand(pull, validate)
	return rulesCmd
}
