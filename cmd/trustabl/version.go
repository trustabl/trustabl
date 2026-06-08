package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Long: `Print the Trustabl version, the git commit it was built from, and the build
date. A binary built locally with "go build" reports "dev" / "none" / "unknown";
released binaries carry real values injected at build time.`,
		Example: "  trustabl version",
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "Trustabl %s\ncommit: %s\nbuilt:  %s\n",
				version, commit, date)
		},
	}
}
