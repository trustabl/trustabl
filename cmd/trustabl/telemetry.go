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

Trustabl collects anonymous data (CLI version, OS, SDKs detected, scan
duration) to improve the product. No source code, file paths, or repo
names are ever sent. See https://trustabl.dev/telemetry for the full list.`,
	}
	cmd.AddCommand(newTelemetryOnCommand())
	cmd.AddCommand(newTelemetryOffCommand())
	cmd.AddCommand(newTelemetryStatusCommand())
	return cmd
}

func newTelemetryOnCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "on",
		Short: "Enable anonymous telemetry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setTelemetry(cmd, true)
		},
	}
}

func newTelemetryOffCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Disable anonymous telemetry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setTelemetry(cmd, false)
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
			case "0":
				fmt.Fprintln(cmd.OutOrStdout(), "telemetry: disabled (TRUSTABL_TELEMETRY=0)")
				return nil
			case "1":
				fmt.Fprintln(cmd.OutOrStdout(), "telemetry: enabled (TRUSTABL_TELEMETRY=1)")
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
			if !existed {
				fmt.Fprintln(cmd.OutOrStdout(), "telemetry: enabled (default — no config file)")
				return nil
			}
			if cfg.Enabled {
				fmt.Fprintln(cmd.OutOrStdout(), "telemetry: enabled (config file)")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "telemetry: disabled (config file)")
			}
			return nil
		},
	}
}

func setTelemetry(cmd *cobra.Command, enabled bool) error {
	path, err := telemetry.DefaultConfigPath()
	if err != nil {
		return err
	}
	cfg, _, err := telemetry.LoadConfig(path)
	if err != nil {
		return err
	}
	cfg.Enabled = enabled
	if err := telemetry.SaveConfig(path, cfg); err != nil {
		return err
	}
	if enabled {
		fmt.Fprintln(cmd.OutOrStdout(), "Telemetry enabled.")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Telemetry disabled.")
	}
	return nil
}
