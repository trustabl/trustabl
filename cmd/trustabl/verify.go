package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/attest"
	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/telemetry"
)

type verifyFlags struct {
	bundle       string
	keyRef       string
	certIdentity string
	certIssuer   string
	noTLog       bool
}

func newVerifyCommand(tel *telemetry.Client) *cobra.Command {
	var f verifyFlags
	cmd := &cobra.Command{
		Use:   "verify <report.json>",
		Short: "Verify a scan attestation against its report",
		Long: `Verify a Trustabl scan attestation with cosign.

This is the consumer side: run it where you CONSUME an attestation (a deploy gate,
an admission controller, a downstream pipeline), against the report file and the
bundle the producer published. It confirms the bundle is a valid Trustabl
attestation for exactly these report bytes, signed by the identity you pin.

<report.json> must be byte-identical to the report that was attested (it is the
signed subject). Verification requires the cosign CLI on PATH.

Keyless verify (the default) REQUIRES --certificate-identity and
--certificate-oidc-issuer: without pinning who signed and which OIDC issuer minted
the cert, verification would accept any Fulcio certificate. For a key-signed
attestation pass --key (the public key) instead.

Exit codes: 0 = verified, 1 = verification failed, 2 = usage error or cosign
missing.`,
		Example: `  # Keyless: pin the GitHub Actions workflow that signed it
  trustabl verify report.json \
    --certificate-identity https://github.com/OWNER/REPO/.github/workflows/scan.yml@refs/heads/main \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com

  # Key-mode, offline
  trustabl verify report.json --key cosign.pub --no-tlog`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if tel != nil {
				tel.Track("command.run", map[string]any{"command": "verify"})
			}
			return runVerify(args[0], f, logLevelFor(cmd))
		},
	}
	cmd.Flags().StringVar(&f.bundle, "bundle", defaultAttestBundle,
		"the attestation bundle to verify")
	cmd.Flags().StringVar(&f.keyRef, "key", "",
		"cosign public key for key-mode verify; omit for keyless")
	cmd.Flags().StringVar(&f.certIdentity, "certificate-identity", "",
		"keyless: required signer identity (e.g. the signing workflow URI)")
	cmd.Flags().StringVar(&f.certIssuer, "certificate-oidc-issuer", "",
		"keyless: required OIDC issuer (e.g. https://token.actions.githubusercontent.com)")
	cmd.Flags().BoolVar(&f.noTLog, "no-tlog", false,
		"do not require a Rekor transparency-log entry (offline/private verify)")
	return cmd
}

func runVerify(reportPath string, f verifyFlags, level logx.Level) error {
	log := logx.New(os.Stderr, level, diagColor(false))
	// Keyless verification without an identity + issuer accepts any Fulcio cert —
	// fail fast with a clear message rather than performing a meaningless check.
	if f.keyRef == "" && (f.certIdentity == "" || f.certIssuer == "") {
		fmt.Fprintln(os.Stderr,
			"keyless verify requires --certificate-identity and --certificate-oidc-issuer")
		fmt.Fprintln(os.Stderr,
			"(or pass --key <public-key> to verify a key-signed attestation).")
		return exitCodeError{2}
	}

	log.Verbosef("verify: report %s · bundle %s · keyless=%v", reportPath, f.bundle, f.keyRef == "")
	err := attest.Verify(context.Background(), attest.VerifyOptions{
		Blob:         reportPath,
		Bundle:       f.bundle,
		KeyRef:       f.keyRef,
		CertIdentity: f.certIdentity,
		CertIssuer:   f.certIssuer,
		NoTLog:       f.noTLog,
	})
	switch {
	case err == nil:
		fmt.Fprintf(os.Stderr, "Attestation verified: %s (bundle %s)\n", reportPath, f.bundle)
		return nil
	case errors.Is(err, attest.ErrCosignNotFound):
		return cosignMissingError()
	case errors.Is(err, attest.ErrVerifyFailed):
		fmt.Fprintln(os.Stderr, "Attestation verification FAILED.")
		return exitCodeError{1}
	default:
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitCodeError{2}
	}
}
