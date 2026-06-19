package attest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Sentinel errors so the CLI can map a cosign outcome to the right exit code and
// message.
var (
	// ErrCosignNotFound means the cosign binary is not on PATH. Attestation and
	// verification both require it; Trustabl ships no signing code of its own.
	ErrCosignNotFound = errors.New("attest: cosign not found on PATH")
	// ErrVerifyFailed means cosign ran and reported the attestation did not verify
	// — a bad signature, a wrong identity, tampered report bytes, or a missing
	// transparency-log entry. Distinct from an exec error so the CLI can return
	// exit 1 (verification failed) rather than exit 2 (could not run).
	ErrVerifyFailed = errors.New("attest: attestation verification failed")
)

// cosignBinary is the executable name; a package var so a test can point it at a
// guaranteed-absent name to exercise the not-found path hermetically. Production
// never changes it.
var cosignBinary = "cosign"

// AttestOptions configures a single `cosign attest-blob` invocation.
type AttestOptions struct {
	Blob      string // path to the subject blob (the canonical scan-report JSON)
	Predicate string // path to the predicate JSON file
	Bundle    string // output path for the signed sigstore bundle
	KeyRef    string // cosign private-key reference; empty selects keyless (ambient OIDC)
	NoTLog    bool   // do not upload the signature to the public Rekor transparency log
}

// attestArgs builds the cosign argv (without the binary name). Pure and
// table-tested, so the flag wiring is verified without invoking cosign.
func attestArgs(o AttestOptions) []string {
	args := []string{
		"attest-blob",
		"--predicate", o.Predicate,
		"--type", PredicateType,
		"--bundle", o.Bundle,
		"--yes", // non-interactive: the operator opted in by running attest
	}
	if o.KeyRef != "" {
		args = append(args, "--key", o.KeyRef)
	}
	if o.NoTLog {
		args = append(args, "--tlog-upload=false")
	}
	// The subject blob is the trailing positional argument.
	return append(args, o.Blob)
}

// Attest signs o.Blob with cosign, embedding o.Predicate, and writes o.Bundle.
// Keyless (empty KeyRef) mints an ephemeral cert from the ambient OIDC identity;
// with KeyRef it signs with that key. cosign owns all crypto. Returns
// ErrCosignNotFound when cosign is absent, or a wrapped error when cosign runs
// but fails (e.g. keyless with no OIDC identity available).
func Attest(ctx context.Context, o AttestOptions) error {
	err := run(ctx, attestArgs(o))
	if err == nil || errors.Is(err, ErrCosignNotFound) {
		return err
	}
	return fmt.Errorf("attest: cosign signing failed: %w", err)
}

// VerifyOptions configures a single `cosign verify-blob-attestation` invocation.
type VerifyOptions struct {
	Blob         string // the subject blob to check against the attestation
	Bundle       string // the signed bundle produced by Attest
	KeyRef       string // public key for key-mode verify; empty selects keyless
	CertIdentity string // keyless: the signer identity the cert must carry
	CertIssuer   string // keyless: the OIDC issuer the cert must come from
	NoTLog       bool   // do not require/verify a Rekor transparency-log entry
}

func verifyArgs(o VerifyOptions) []string {
	args := []string{
		"verify-blob-attestation",
		"--type", PredicateType,
		"--bundle", o.Bundle,
	}
	if o.KeyRef != "" {
		args = append(args, "--key", o.KeyRef)
	} else {
		// Keyless verification is meaningless without pinning who signed and where:
		// an unconstrained verify accepts any Fulcio cert. The CLI requires both.
		args = append(args,
			"--certificate-identity", o.CertIdentity,
			"--certificate-oidc-issuer", o.CertIssuer)
	}
	if o.NoTLog {
		args = append(args, "--insecure-ignore-tlog=true")
	}
	return append(args, o.Blob)
}

// Verify checks that o.Bundle is a valid Trustabl attestation for o.Blob. It
// returns nil when cosign confirms it, ErrVerifyFailed when cosign ran but
// rejected it, ErrCosignNotFound when cosign is absent, or a wrapped exec error
// otherwise.
func Verify(ctx context.Context, o VerifyOptions) error {
	err := run(ctx, verifyArgs(o))
	if err == nil || errors.Is(err, ErrCosignNotFound) {
		return err
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		// cosign ran and exited non-zero: the attestation did not verify.
		return ErrVerifyFailed
	}
	return fmt.Errorf("attest: running cosign: %w", err)
}

// run executes cosign with args, returning ErrCosignNotFound when the binary is
// missing and the raw error otherwise (callers interpret *exec.ExitError).
// cosign's human-readable progress goes to stderr — never our stdout — so the
// byte-stable scan report on stdout is never perturbed.
func run(ctx context.Context, args []string) error {
	if _, err := exec.LookPath(cosignBinary); err != nil {
		return ErrCosignNotFound
	}
	cmd := exec.CommandContext(ctx, cosignBinary, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
