package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/rules"
	"github.com/trustabl/trustabl/internal/telemetry"
)

// newCapabilitiesCommand emits this build's capability descriptor — the
// machine-readable vocabulary (schema version, scopes, languages, categories,
// applies_to values, predicates) that a rule pack is checked against. A release
// publishes this as an asset; the trustabl-rules CI gate compares proposed rules
// against each supported release's descriptor so a rules change can't silently
// break a deployed binary.
func newCapabilitiesCommand(tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "capabilities",
		Short: "Print this build's rule-evaluation vocabulary as JSON",
		Long: `Print the machine-readable capability descriptor for this Trustabl build: the
rule-schema version, whether it loads rules forward-compatibly, and every scope,
language, detector category, applies_to value, and match predicate it can
evaluate.

This is the contract a rule pack is checked against. The trustabl-rules CI gate
uses each supported release's descriptor to decide, before a rule change merges,
whether a proposed rule would run, be skipped (forward-compatible), or hard-break
that release — so a rules change can never silently break a deployed binary.

The output is deterministic (sorted), suitable for committing or publishing as a
release asset.`,
		Example: `  # Print this build's capabilities
  trustabl capabilities

  # Publish a release's descriptor (CI attaches this as a release asset)
  trustabl capabilities > capabilities.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tel != nil {
				tel.Track("command.run", map[string]any{"command": "capabilities"})
			}
			b, err := json.MarshalIndent(rules.Describe(), "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	}
}
