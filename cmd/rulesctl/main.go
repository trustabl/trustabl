// Command rulesctl is the Trustabl rules PUBLISHER tool. It is a separate binary
// from the scanner (cmd/trustabl) and is built only by CI / maintainers — it is
// NOT shipped to users and is excluded from the goreleaser build. Keeping the
// signing and key-generation code here, out of the scanner binary, preserves the
// rulesign package's verify-only guarantee: no private-key material links into
// the tool end users run.
//
// Subcommands:
//
//	rulesctl keygen   — generate an Ed25519 signing keypair (seed + public key)
//	rulesctl bundle   — pack a rule-pack dir into the canonical bundle.tar.gz + print its digest
//	rulesctl sign     — sign a channel statement the engine will verify
//
// The producer/verifier contract (digest serialization, signing payload) lives
// in internal/rulepub + internal/rulesign and is shared by both sides.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/rulepub"
	"github.com/trustabl/trustabl/internal/rulesign"
)

func main() {
	root := &cobra.Command{
		Use:           "rulesctl",
		Short:         "Trustabl rules publisher (keygen, bundle, sign) — CI/maintainer tool, not shipped to users",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newKeygenCommand(), newBundleCommand(), newSignCommand(), newVerifyCommand())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "rulesctl:", err)
		os.Exit(1)
	}
}

// --- keygen ------------------------------------------------------------------

func newKeygenCommand() *cobra.Command {
	var keyID, pubOut, seedOut, notAfter string
	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Generate an Ed25519 signing keypair",
		Long: `Generate a fresh Ed25519 signing keypair.

The 32-byte SEED is the signing secret: store it in the trustabl-rules CI signing
secret (RULES_SIGNING_KEY_ED25519). The PUBLIC KEY is non-secret: paste the
emitted keyring entry into the engine's internal/rulesign/keyring.json.

Run this ONCE, locally, to bootstrap a key — never in CI.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if keyID == "" {
				return fmt.Errorf("--key-id is required (e.g. trustabl-rules-2026-06)")
			}
			seed, pub, err := rulepub.GenerateKeypair()
			if err != nil {
				return err
			}
			seedB64 := base64.StdEncoding.EncodeToString(seed)
			ent := map[string]string{
				"id":         keyID,
				"public_key": base64.StdEncoding.EncodeToString(pub),
				"not_before": time.Now().UTC().Format(time.RFC3339),
			}
			if notAfter != "" {
				if _, perr := time.Parse(time.RFC3339, notAfter); perr != nil {
					return fmt.Errorf("--not-after must be RFC3339 (e.g. 2027-01-01T00:00:00Z): %w", perr)
				}
				ent["not_after"] = notAfter
			}
			eb, err := json.MarshalIndent(ent, "", "  ")
			if err != nil {
				return err
			}
			entry := string(eb) + "\n"

			if pubOut != "" {
				if err := os.WriteFile(pubOut, []byte(entry), 0o644); err != nil {
					return fmt.Errorf("write public entry: %w", err)
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "# keyring entry (paste into internal/rulesign/keyring.json \"keys\":[...]):")
				fmt.Fprint(cmd.OutOrStdout(), entry)
			}
			if seedOut != "" {
				// O_EXCL: refuse to overwrite an existing path (incl. a symlink)
				// rather than truncating it and inheriting a possibly-loose prior mode —
				// the seed is the trust-root signing secret, so its 0600 must hold.
				sf, err := os.OpenFile(seedOut, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL, 0o600)
				if err != nil {
					return fmt.Errorf("create seed file (refusing to overwrite an existing path): %w", err)
				}
				_, werr := sf.WriteString(seedB64 + "\n")
				if cerr := sf.Close(); werr == nil {
					werr = cerr
				}
				if werr != nil {
					return fmt.Errorf("write seed: %w", werr)
				}
				fmt.Fprintf(os.Stderr, "wrote private seed to %s (mode 0600) — store it in the CI secret, do NOT commit it\n", seedOut)
			} else {
				fmt.Fprintln(os.Stderr, "# PRIVATE SEED (store in CI secret RULES_SIGNING_KEY_ED25519, do NOT commit):")
				fmt.Fprintln(os.Stderr, seedB64)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&keyID, "key-id", "", "stable key id for the keyring entry (e.g. trustabl-rules-2026-06)")
	cmd.Flags().StringVar(&pubOut, "pub-out", "", "write the public keyring entry to this file (default: stdout)")
	cmd.Flags().StringVar(&seedOut, "seed-out", "", "write the private seed to this file (default: stderr)")
	cmd.Flags().StringVar(&notAfter, "not-after", "", "optional RFC3339 expiry for the key (sets not_after; use for a rotation's outgoing key)")
	return cmd
}

// --- bundle ------------------------------------------------------------------

func newBundleCommand() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "bundle <dir>",
		Short: "Pack a rule-pack directory into the canonical bundle.tar.gz and print its digest",
		Long: `Pack the rule-pack directory <dir> into a deterministic, canonical bundle.tar.gz
and print the bundle's content digest to stdout.

The digest is computed with the engine's own canonical serialization, so it is
exactly the digest the engine will recompute after downloading the bundle — sign
a channel statement over this digest. The directory MUST contain manifest.yaml at
its root with a positive schema_version, or bundling is refused.

<dir> must be a clean export: only manifest.yaml + the rule-pack subdirectories.
Any extra file (.git, editor cruft) changes the digest.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if out == "" {
				return fmt.Errorf("--out is required (path for bundle.tar.gz)")
			}
			f, err := os.Create(out)
			if err != nil {
				return fmt.Errorf("create %s: %w", out, err)
			}
			digest, err := rulepub.Bundle(args[0], f)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				_ = os.Remove(out)
				return err
			}
			// Digest to stdout (capture in CI: DIGEST=$(rulesctl bundle ...)).
			fmt.Fprintln(cmd.OutOrStdout(), digest)
			fmt.Fprintf(os.Stderr, "wrote bundle %s (digest %s)\n", out, digest)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "path to write bundle.tar.gz")
	return cmd
}

// --- sign --------------------------------------------------------------------

func newSignCommand() *cobra.Command {
	var p rulepub.StatementParams
	var out, seedB64, seedFile string
	var ttl time.Duration
	cmd := &cobra.Command{
		Use:   "sign",
		Short: "Sign a channel statement the engine will verify",
		Long: `Sign a channel statement binding a channel to a bundle (by digest) at a
monotonic version, within a freshness window. The output statement.json is the
asset to upload to the channel-<name> release.

The signing seed is read from --seed-b64, --seed-file, or the
RULES_SIGNING_KEY_ED25519 environment variable (in that order of precedence).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			seed, err := loadSeed(seedB64, seedFile, os.Getenv("RULES_SIGNING_KEY_ED25519"))
			if err != nil {
				return err
			}
			issued, expires, err := statementTimes(time.Now().UTC(), p.IssuedAt, p.Expires, ttl)
			if err != nil {
				return err
			}
			p.IssuedAt, p.Expires = issued, expires
			if p.Version <= 0 {
				// Default to epoch seconds: simple, monotonic per channel across
				// sequential publishes. Override with --version for an explicit counter.
				p.Version = time.Now().UTC().Unix()
			}
			raw, err := rulepub.SignStatement(seed, p)
			if err != nil {
				return err
			}
			if out == "" {
				fmt.Fprint(cmd.OutOrStdout(), string(raw))
				return nil
			}
			if err := os.WriteFile(out, raw, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", out, err)
			}
			fmt.Fprintf(os.Stderr, "wrote statement %s (channel=%s version=%d digest=%s)\n", out, p.Channel, p.Version, p.Digest)
			return nil
		},
	}
	cmd.Flags().StringVar(&p.Channel, "channel", "", "channel name (e.g. production, staging)")
	cmd.Flags().Int64Var(&p.Version, "version", 0, "monotonic statement version (default: current epoch seconds)")
	cmd.Flags().StringVar(&p.Digest, "digest", "", "bundle content digest (64-char lowercase hex from `rulesctl bundle`)")
	cmd.Flags().StringVar(&p.KeyID, "key-id", "", "signing key id (must match a keyring entry)")
	cmd.Flags().StringVar(&p.IssuedAt, "issued-at", "", "RFC3339 issue time (default: now)")
	cmd.Flags().StringVar(&p.Expires, "expires", "", "RFC3339 expiry (default: issued-at + --ttl)")
	cmd.Flags().DurationVar(&ttl, "ttl", 30*24*time.Hour, "freshness window when --expires is omitted")
	cmd.Flags().StringVar(&out, "out", "", "write statement.json here (default: stdout)")
	cmd.Flags().StringVar(&seedB64, "seed-b64", "", "base64 signing seed (overrides --seed-file and env)")
	cmd.Flags().StringVar(&seedFile, "seed-file", "", "file containing the base64 signing seed")
	return cmd
}

// --- verify ------------------------------------------------------------------

func newVerifyCommand() *cobra.Command {
	var statementPath, bundleDir, keyringPath, channel string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify a candidate channel statement against a keyring + its bundle (CI self-verify before promote)",
		Long: `Verify a freshly signed candidate the way the engine will at scan time: check
the Ed25519 signature against the trust keyring, the channel binding, freshness,
and that the statement's digest matches the canonical digest of the local bundle
directory. Run this on the candidate BEFORE promoting a channel, so a statement
the fleet would reject is never promoted.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if statementPath == "" || bundleDir == "" || keyringPath == "" || channel == "" {
				return fmt.Errorf("--statement, --bundle-dir, --keyring, and --channel are all required")
			}
			krRaw, err := os.ReadFile(keyringPath)
			if err != nil {
				return fmt.Errorf("read keyring: %w", err)
			}
			ring, err := rulesign.ParseKeyring(krRaw)
			if err != nil {
				return err
			}
			if ring.Empty() {
				return fmt.Errorf("keyring %s trusts no keys — publish the signing public key before self-verify", keyringPath)
			}
			stmt, err := os.ReadFile(statementPath)
			if err != nil {
				return fmt.Errorf("read statement: %w", err)
			}
			if err := rulepub.VerifyBundle(stmt, bundleDir, ring, channel, time.Now().UTC()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK: statement verifies for channel %q against %s\n", channel, bundleDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&statementPath, "statement", "", "path to the candidate statement.json")
	cmd.Flags().StringVar(&bundleDir, "bundle-dir", "", "path to the bundle source dir the statement commits to")
	cmd.Flags().StringVar(&keyringPath, "keyring", "", "path to the trust keyring JSON (the engine's internal/rulesign/keyring.json)")
	cmd.Flags().StringVar(&channel, "channel", "", "expected channel name")
	return cmd
}

// --- helpers (unit-tested) ---------------------------------------------------

// loadSeed resolves the 32-byte Ed25519 signing seed from, in precedence order,
// a base64 flag, a file holding base64, then an environment value. It refuses a
// seed of the wrong length so a truncated secret fails loudly at sign time.
func loadSeed(b64Flag, file, env string) ([]byte, error) {
	var b64 string
	switch {
	case b64Flag != "":
		b64 = b64Flag
	case file != "":
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read seed file: %w", err)
		}
		b64 = strings.TrimSpace(string(data))
	case env != "":
		b64 = strings.TrimSpace(env)
	default:
		return nil, fmt.Errorf("no signing seed: pass --seed-b64, --seed-file, or set RULES_SIGNING_KEY_ED25519")
	}
	seed, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode signing seed: %w", err)
	}
	if len(seed) != 32 {
		return nil, fmt.Errorf("signing seed is %d bytes, want 32", len(seed))
	}
	return seed, nil
}

// statementTimes resolves the issued_at/expires RFC3339 strings: an empty
// issued-at defaults to now; an empty expires defaults to issued-at + ttl. It
// validates that both parse and that expires does not precede issued-at.
func statementTimes(now time.Time, issuedAt, expires string, ttl time.Duration) (string, string, error) {
	var issued time.Time
	if issuedAt == "" {
		issued = now
		issuedAt = issued.Format(time.RFC3339)
	} else {
		t, err := time.Parse(time.RFC3339, issuedAt)
		if err != nil {
			return "", "", fmt.Errorf("issued-at: %w", err)
		}
		issued = t
	}
	if expires == "" {
		expires = issued.Add(ttl).Format(time.RFC3339)
	} else {
		exp, err := time.Parse(time.RFC3339, expires)
		if err != nil {
			return "", "", fmt.Errorf("expires: %w", err)
		}
		if exp.Before(issued) {
			return "", "", fmt.Errorf("expires %s precedes issued-at %s", expires, issuedAt)
		}
	}
	return issuedAt, expires, nil
}
