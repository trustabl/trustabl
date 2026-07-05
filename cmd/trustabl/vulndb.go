package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/telemetry"
	"github.com/trustabl/trustabl/internal/vulndb"
)

func newVulnDBCommand(tel *telemetry.Client) *cobra.Command {
	vulnCmd := &cobra.Command{
		Use:   "vulndb",
		Short: "Manage the OSV vulnerability database snapshot",
		Long: `Manage the pinned OSV vulnerability snapshot used by "scan --vuln-scan".

The snapshot is resolved automatically the first time you run --vuln-scan and
cached under your user cache directory, with an offline fallback. You normally
never run these commands — a --vuln-scan fetches what it needs — but "vulndb
pull" lets you pre-fetch the database so a later --vuln-scan can run offline
(e.g. in CI or an air-gapped environment).`,
		Example: `  # Pre-download the OSV database for offline --vuln-scan
  trustabl vulndb pull`,
	}

	var noUpdate bool
	pull := &cobra.Command{
		Use:   "pull",
		Short: "Download the OSV vulnerability database into the local cache",
		Long: `Download the OSV vulnerability database for every supported ecosystem (PyPI,
npm, Go, NuGet, Packagist, crates.io) into the local cache, so a later
"scan --vuln-scan" can run offline. Each ecosystem's published OSV export is
pulled from osv.dev — this can be a sizable download.`,
		Example: `  # Pre-download the OSV database for offline --vuln-scan
  trustabl vulndb pull`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tel != nil {
				tel.Track("command.run", map[string]any{"command": "vulndb.pull"})
			}
			log := logx.New(os.Stderr, logLevelFor(cmd), diagColor(false))
			log.Verbosef("vulndb pull: fetching OSV databases for all supported ecosystems")
			defer log.Timer("vulndb pull")()
			res, err := vulndb.Resolve(vulndb.ResolveConfig{
				Ecosystems:   vulndb.AllEcosystems(),
				NoUpdate:     noUpdate,
				ForceRefresh: !noUpdate, // pull refreshes the cached snapshot on demand
			})
			if err != nil {
				return fmt.Errorf("vulndb pull: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Cached OSV snapshot %s (%d advisories) for offline --vuln-scan.\n", res.Version, res.DB.Len())
			return nil
		},
	}
	pull.Flags().BoolVar(&noUpdate, "no-update", false, "use the cached database only; do not fetch")
	vulnCmd.AddCommand(pull)
	return vulnCmd
}
