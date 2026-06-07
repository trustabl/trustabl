package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/rules"
	"github.com/trustabl/trustabl/internal/rulesource"
)

func newRulesCommand() *cobra.Command {
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

	var repo, ref string
	pull := &cobra.Command{
		Use:   "pull",
		Short: "Download the detection rule packs into the local cache",
		Long: `Download the detection rule packs into the local cache so later scans can run
offline. By default this fetches the official trustabl-rules repository's default
branch; override the source with --rules-repo / --rules-ref, or set the
TRUSTABL_RULES_REPO environment variable.`,
		Example: `  # Pull the official rules (default branch)
  trustabl rules pull

  # Pull a specific tag or branch
  trustabl rules pull --rules-ref v1.2.0

  # Pull from a fork or mirror
  trustabl rules pull --rules-repo https://github.com/me/my-rules`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			log := logx.New(os.Stderr, logLevelFor(cmd), diagColor(false))
			if repo == "" {
				repo = os.Getenv("TRUSTABL_RULES_REPO")
			}
			src := repo
			if src == "" {
				src = rulesource.DefaultRepoURL
			}
			log.Verbosef("rules pull: fetching %s @ %s", src, refOrDefault(ref))
			defer log.Timer("rules pull")()
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
			log.Verbosef("rules pull: resolved %s from %s", res.SHA, res.RepoURL)
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
