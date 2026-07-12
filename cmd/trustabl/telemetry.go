// cmd/trustabl/telemetry.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/telemetry"
)

func newTelemetryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Manage anonymous usage telemetry",
		Long: `Manage Trustabl's anonymous usage telemetry.

Trustabl optionally collects anonymous data (CLI version, scan outcome) to
improve the product. No source code, file paths, or repo names are ever sent.
See https://trustabl.ai/telemetry for the full list.`,
	}
	cmd.AddCommand(newTelemetryOffCommand())
	cmd.AddCommand(newTelemetryMinimalCommand())
	cmd.AddCommand(newTelemetryFullCommand())
	cmd.AddCommand(newTelemetryOnCommand())
	cmd.AddCommand(newTelemetryStatusCommand())
	return cmd
}

func newTelemetryOffCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Disable telemetry — no data sent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setTelemetryMode(cmd, "disabled")
		},
	}
}

func newTelemetryMinimalCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "minimal",
		Short: "Enable minimal telemetry — version and outcome only",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setTelemetryMode(cmd, "minimal")
		},
	}
}

func newTelemetryFullCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "full",
		Short: "Enable full anonymous usage telemetry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setTelemetryMode(cmd, "full")
		},
	}
}

func newTelemetryOnCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "on",
		Short: "Enable full telemetry (alias for 'full')",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setTelemetryMode(cmd, "full")
		},
	}
}

func newTelemetryStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current telemetry state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			envVal := os.Getenv("TRUSTABL_TELEMETRY")
			switch envVal {
			case "0", "disabled":
				fmt.Fprintf(cmd.OutOrStdout(), "telemetry: disabled (TRUSTABL_TELEMETRY=%s)\n", envVal)
				return nil
			case "1", "full":
				fmt.Fprintf(cmd.OutOrStdout(), "telemetry: full (TRUSTABL_TELEMETRY=%s)\n", envVal)
				return nil
			case "minimal":
				fmt.Fprintln(cmd.OutOrStdout(), "telemetry: minimal (TRUSTABL_TELEMETRY=minimal)")
				return nil
			}
			path, err := telemetry.DefaultConfigPath()
			if err != nil {
				return err
			}
			cfg, existed, err := telemetry.LoadConfig(path)
			if err != nil {
				return err
			}
			if !existed || cfg.Mode == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "telemetry: disabled (default — not yet configured)")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "telemetry: %s (config file)\n", cfg.Mode)
			return nil
		},
	}
}

func setTelemetryMode(cmd *cobra.Command, mode string) error {
	path, err := telemetry.DefaultConfigPath()
	if err != nil {
		return err
	}
	cfg, _, err := telemetry.LoadConfig(path)
	if err != nil {
		return err
	}
	cfg.Mode = mode
	if err := telemetry.SaveConfig(path, cfg); err != nil {
		return err
	}
	switch mode {
	case "disabled":
		fmt.Fprintln(cmd.OutOrStdout(), "Telemetry disabled.")
	case "minimal":
		fmt.Fprintln(cmd.OutOrStdout(), "Telemetry set to minimal (version and outcome only).")
	case "full":
		fmt.Fprintln(cmd.OutOrStdout(), "Telemetry set to full.")
	}
	return nil
}
