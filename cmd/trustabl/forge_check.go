package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/forge"
	"github.com/trustabl/trustabl/internal/rules"
	"github.com/trustabl/trustabl/internal/rulesource"
)

func newForgeCheckCommand() *cobra.Command {
	var rulesRef string

	cmd := &cobra.Command{
		Use:   "check [file]",
		Short: "Check whether a forge-generated SKILL.md is current with the latest rules",
		Long: `Check whether a forge-generated SKILL.md is current with the latest rules.

Reads the provenance stamp embedded in the file by "trustabl forge", resolves
the current rules SHA, and reports whether they match.

Exit codes:
  0  — stamp found and SHA matches current rules (file is current)
  1  — stale (SHA mismatch), unstamped, or file not found (user action needed)
  2  — error resolving rules or reading the file (operator/environment fault)`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "SKILL.md"
			isDefault := len(args) == 0
			if len(args) == 1 {
				path = args[0]
			}
			return runForgeCheck(cmd, path, rulesRef, isDefault)
		},
	}

	cmd.Flags().StringVar(&rulesRef, "rules-ref", "",
		"pin the trustabl-rules branch or tag (default: latest cached)")

	return cmd
}

func runForgeCheck(cmd *cobra.Command, path, rulesRef string, isDefault bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if isDefault {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"trustabl forge check: no SKILL.md found in current directory; "+
						"pass an explicit path or run: trustabl forge --output SKILL.md\n")
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"trustabl forge check: %s: file not found\n", path)
			}
			return exitCodeError{code: 1}
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "trustabl forge check: read %s: %v\n", path, err)
		return exitCodeError{code: 2}
	}

	stamp, ok := forge.ParseStamp(string(raw))
	if !ok {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"trustabl forge check: no forge stamp found in %s; "+
				"regenerate with: trustabl forge --output %s\n", path, path)
		return exitCodeError{code: 1}
	}

	res, err := rulesource.Resolve(
		rulesource.Config{Ref: rulesRef},
		rules.SupportedSchemaVersion,
	)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "trustabl forge check: rules: %v\n", err)
		return exitCodeError{code: 2}
	}

	if stamp.SHA == res.SHA {
		fmt.Fprintf(cmd.OutOrStdout(),
			"up to date (rules: %s, generated: %s)\n",
			shortSHA(stamp.SHA), stamp.Date)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(),
		"outdated: generated from %s, current rules are %s\nregenerate: trustabl forge --output %s\n",
		shortSHA(stamp.SHA), shortSHA(res.SHA), path)
	return exitCodeError{code: 1}
}

// shortSHA returns the first 7 characters of a SHA for display, or the full
// string when it is shorter than 7 characters.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
