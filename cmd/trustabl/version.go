package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/telemetry"
)

func newVersionCommand(tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Long: `Print the Trustabl version, the git commit it was built from, and the build
date. A binary built locally with "go build" reports "dev" / "none" / "unknown";
released binaries carry real values injected at build time.`,
		Example: "  trustabl version",
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			if tel != nil {
				tel.Track("command.run", map[string]any{"command": "version"})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Trustabl %s\ncommit: %s\nbuilt:  %s\n",
				version, commit, date)
		},
	}
}
