package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/attest"
	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/telemetry"
)

// Default output paths shared by the `attest` subcommand and the `scan --attest`
// convenience flag, so both produce the same file names.
const (
	defaultAttestBundle    = "trustabl-attestation.bundle.json"
	defaultAttestPredicate = "trustabl-predicate.json"
)

// attestFlags is the shared config for both attestation entry points (the
// subcommand and scan --attest). One struct, one core (doAttest), so the two
// paths can never drift in how they sign.
type attestFlags struct {
	keyRef       string
	bundleOut    string
	predicateOut string
	noTLog       bool
}

func newAttestCommand(tel *telemetry.Client) *cobra.Command {
	var f attestFlags
	cmd := &cobra.Command{
		Use:   "attest <report.json>",
		Short: "Sign a scan report into a verifiable attestation",
		Long: `Attest a Trustabl scan report with cosign.

<report.json> is a scan result written by "trustabl scan --format json" (or
--json-out). attest builds a deterministic predicate from it and signs the report
itself as the attestation subject, producing a sigstore bundle a consumer can
later verify with "trustabl verify".

Signing requires the cosign CLI on PATH (https://docs.sigstore.dev/cosign). By
default it signs KEYLESS: in CI it uses the runner's ambient OIDC identity (no
keys to manage) and records the signature in the public Rekor transparency log.
Pass --key to sign with a private key instead — for offline, air-gapped, or
private signing that must not write to a public log (combine with --no-tlog).

What gets attested is the SCANNED repo's result, not the Trustabl binary; the
signer is whoever runs this command, never trustabl.dev.`,
		Example: `  # Keyless (in CI): scan to JSON, then attest
  trustabl scan . --format json -o report.json
  trustabl attest report.json

  # Key-mode, fully offline (no transparency log)
  cosign generate-key-pair
  trustabl attest report.json --key cosign.key --no-tlog`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if tel != nil {
				tel.Track("command.run", map[string]any{"command": "attest"})
			}
			return runAttest(args[0], f, logLevelFor(cmd))
		},
	}
	cmd.Flags().StringVar(&f.keyRef, "key", "",
		"cosign private-key reference; omit for keyless signing (ambient CI OIDC identity)")
	cmd.Flags().StringVarP(&f.bundleOut, "bundle", "o", defaultAttestBundle,
		"write the signed attestation bundle to this file")
	cmd.Flags().StringVar(&f.predicateOut, "predicate-out", defaultAttestPredicate,
		"write the generated predicate to this file")
	cmd.Flags().BoolVar(&f.noTLog, "no-tlog", false,
		"do not upload the signature to the public Rekor transparency log (offline/private signing)")
	return cmd
}

// runAttest is the subcommand path: load a persisted report, then sign it.
func runAttest(reportPath string, f attestFlags, level logx.Level) error {
	log := logx.New(os.Stderr, level, diagColor(false))
	result, err := readScanResult(reportPath)
	if err != nil {
		return err
	}
	return doAttest(*result, reportPath, f, log)
}

// doAttest is the single signing core shared by the subcommand and scan --attest.
// It writes the predicate next to the report, then drives cosign to sign the
// report (the subject) and emit the bundle. reportPath must already exist on disk
// — cosign signs the bytes at that path, and a verifier re-supplies the same file.
func doAttest(result models.ScanResult, reportPath string, f attestFlags, log *logx.Logger) error {
	pred := attest.BuildPredicate(result, version)
	predBytes, err := pred.JSON()
	if err != nil {
		return fmt.Errorf("building attestation predicate: %w", err)
	}
	if err := os.WriteFile(f.predicateOut, predBytes, 0o644); err != nil {
		return fmt.Errorf("writing predicate to %s: %w", f.predicateOut, err)
	}
	log.Verbosef("attest: wrote predicate to %s", f.predicateOut)

	if err := attest.Attest(context.Background(), attest.AttestOptions{
		Blob:      reportPath,
		Predicate: f.predicateOut,
		Bundle:    f.bundleOut,
		KeyRef:    f.keyRef,
		NoTLog:    f.noTLog,
	}); err != nil {
		return attestExitError(err)
	}
	log.Verbosef("attest: wrote attestation bundle to %s", f.bundleOut)
	fmt.Fprintf(os.Stderr, "Attestation written: %s (predicate %s, subject %s)\n",
		f.bundleOut, f.predicateOut, reportPath)
	return nil
}

// attestExitError turns an attest.Attest failure into the right CLI exit. A
// missing cosign gets the actionable install message; anything else is a generic
// signing failure (exit 2). The reporter has already streamed cosign's own stderr.
func attestExitError(err error) error {
	if errors.Is(err, attest.ErrCosignNotFound) {
		return cosignMissingError()
	}
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	return exitCodeError{2}
}

// cosignMissingError prints how to install cosign and returns the scanner-error
// exit code. Shared by the attest and verify paths.
func cosignMissingError() error {
	fmt.Fprintln(os.Stderr, "cosign was not found on PATH.")
	fmt.Fprintln(os.Stderr,
		"Trustabl shells out to the cosign CLI for signing and verification; install it:")
	fmt.Fprintln(os.Stderr, "  https://docs.sigstore.dev/cosign/system_config/installation/")
	fmt.Fprintln(os.Stderr,
		"If cosign is already downloaded, add its folder to PATH — a binary sitting in")
	fmt.Fprintln(os.Stderr,
		"the current directory is not enough, and each new shell needs PATH set again.")
	return exitCodeError{2}
}
