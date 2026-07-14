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
	// SigningConfig is the path to a no-Rekor signing config. It is set internally
	// by Attest on cosign v3+ (which removed --tlog-upload) to realize NoTLog; when
	// empty and NoTLog is set, the pre-v3 --tlog-upload=false flag is used instead.
	SigningConfig string
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
		if o.SigningConfig != "" {
			// cosign v3+: sign against a no-Rekor signing config (no transparency log).
			args = append(args, "--signing-config", o.SigningConfig)
		} else {
			// cosign v2: the direct flag (removed in v3).
			args = append(args, "--tlog-upload=false")
		}
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
	if o.NoTLog {
		// cosign v3 removed --tlog-upload (it defaults --use-signing-config=true, a
		// Rekor-bearing config, so the old flag conflicts). Realize NoTLog on v3+ via
		// an explicit no-Rekor signing config; pre-v3 keeps --tlog-upload=false. If the
		// version can't be determined, fall back to the pre-v3 flag (best effort).
		if major, verr := cosignMajor(ctx); verr == nil && major >= 3 {
			cfg, cleanup, cerr := writeNoTLogSigningConfig(ctx)
			if cerr != nil {
				return cerr
			}
			defer cleanup()
			o.SigningConfig = cfg
		}
	}
	err := run(ctx, attestArgs(o))
	if err == nil || errors.Is(err, ErrCosignNotFound) {
		return err
	}
	return fmt.Errorf("attest: cosign signing failed: %w", err)
}

// writeNoTLogSigningConfig generates a Sigstore signing config with no Rekor, TSA,
// OIDC, or Fulcio services — i.e. one that signs without contacting or recording
// to any transparency log. cosign v3 needs this for offline signing because it
// defaults --use-signing-config=true. Returns the temp-file path and a cleanup
// func the caller must defer.
func writeNoTLogSigningConfig(ctx context.Context) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "trustabl-signingconfig-*.json")
	if err != nil {
		return "", func() {}, err
	}
	name := f.Name()
	f.Close()
	cleanup = func() { os.Remove(name) }
	if e := run(ctx, []string{
		"signing-config", "create",
		"--no-default-rekor", "--no-default-tsa",
		"--no-default-oidc", "--no-default-fulcio",
		"--out", name,
	}); e != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("attest: building no-tlog signing config: %w", e)
	}
	return name, cleanup, nil
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
