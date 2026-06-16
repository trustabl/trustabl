package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// runCmd executes a rulesctl subcommand with args, capturing its stdout/stderr.
func runCmd(t *testing.T, c *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c.SetArgs(args)
	c.SetOut(&out)
	c.SetErr(&out)
	return func() (string, error) { err := c.Execute(); return out.String(), err }()
}

// TestRulesctl_RoundTrip exercises the full producer chain through the CLI wiring
// (keygen -> bundle -> sign -> verify), the exact path the publish workflow runs:
// keygen refuses to overwrite a seed (O_EXCL) and rejects a bad --not-after; a
// signed statement verifies against a keyring built from the emitted public key
// and its bundle, and fails on the wrong channel.
func TestRulesctl_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	seed := filepath.Join(dir, "seed.b64")
	pub := filepath.Join(dir, "pub.json")

	if _, err := runCmd(t, newKeygenCommand(), "--key-id", "k", "--pub-out", pub, "--seed-out", seed); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	if _, err := runCmd(t, newKeygenCommand(), "--key-id", "k", "--pub-out", filepath.Join(dir, "p2.json"), "--seed-out", seed); err == nil {
		t.Fatal("keygen must refuse to overwrite an existing seed file (O_EXCL)")
	}
	if _, err := runCmd(t, newKeygenCommand(), "--key-id", "k", "--not-after", "nope", "--pub-out", filepath.Join(dir, "p3.json"), "--seed-out", filepath.Join(dir, "s3.b64")); err == nil {
		t.Fatal("keygen --not-after must reject a non-RFC3339 value")
	}

	pubEntry, err := os.ReadFile(pub)
	if err != nil {
		t.Fatal(err)
	}
	keyring := filepath.Join(dir, "keyring.json")
	if err := os.WriteFile(keyring, []byte(`{"keys":[`+string(pubEntry)+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	pack := filepath.Join(dir, "pack")
	if err := os.MkdirAll(filepath.Join(pack, "claude_sdk"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(pack, "manifest.yaml"), "schema_version: 12\n")
	mustWrite(t, filepath.Join(pack, "claude_sdk", "t.yaml"), "id: CSDK-010\n")

	out, err := runCmd(t, newBundleCommand(), pack, "--out", filepath.Join(dir, "b.tgz"))
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	digest := strings.TrimSpace(out)
	if len(digest) != 64 {
		t.Fatalf("bundle digest = %q, want 64 hex chars", digest)
	}

	stmt := filepath.Join(dir, "statement.json")
	if _, err := runCmd(t, newSignCommand(), "--channel", "production", "--digest", digest, "--key-id", "k", "--seed-file", seed, "--out", stmt); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := runCmd(t, newVerifyCommand(), "--statement", stmt, "--bundle-dir", pack, "--keyring", keyring, "--channel", "production"); err != nil {
		t.Fatalf("verify (matching): %v", err)
	}
	if _, err := runCmd(t, newVerifyCommand(), "--statement", stmt, "--bundle-dir", pack, "--keyring", keyring, "--channel", "staging"); err == nil {
		t.Fatal("verify must fail for the wrong channel")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSeed_PrecedenceAndValidation(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	b64 := base64.StdEncoding.EncodeToString(seed)

	t.Run("flag base64 wins", func(t *testing.T) {
		got, err := loadSeed(b64, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if base64.StdEncoding.EncodeToString(got) != b64 {
			t.Fatal("seed mismatch")
		}
	})

	t.Run("file fallback", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "seed.b64")
		if err := os.WriteFile(f, []byte(b64+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := loadSeed("", f, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 32 {
			t.Fatalf("got %d bytes", len(got))
		}
	})

	t.Run("env fallback", func(t *testing.T) {
		got, err := loadSeed("", "", b64)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 32 {
			t.Fatalf("got %d bytes", len(got))
		}
	})

	t.Run("wrong length refused", func(t *testing.T) {
		short := base64.StdEncoding.EncodeToString([]byte("too-short"))
		if _, err := loadSeed(short, "", ""); err == nil {
			t.Fatal("accepted a non-32-byte seed")
		}
	})

	t.Run("none provided refused", func(t *testing.T) {
		if _, err := loadSeed("", "", ""); err == nil {
			t.Fatal("accepted with no seed source")
		}
	})
}

func TestStatementTimes(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	ttl := 30 * 24 * time.Hour

	t.Run("both empty uses now and now+ttl", func(t *testing.T) {
		issued, expires, err := statementTimes(now, "", "", ttl)
		if err != nil {
			t.Fatal(err)
		}
		if issued != "2026-06-15T12:00:00Z" {
			t.Fatalf("issued = %s", issued)
		}
		if expires != "2026-07-15T12:00:00Z" {
			t.Fatalf("expires = %s", expires)
		}
	})

	t.Run("explicit issued, derived expires", func(t *testing.T) {
		issued, expires, err := statementTimes(now, "2026-01-01T00:00:00Z", "", ttl)
		if err != nil {
			t.Fatal(err)
		}
		if issued != "2026-01-01T00:00:00Z" {
			t.Fatalf("issued = %s", issued)
		}
		if expires != "2026-01-31T00:00:00Z" {
			t.Fatalf("expires = %s", expires)
		}
	})

	t.Run("both explicit pass through", func(t *testing.T) {
		issued, expires, err := statementTimes(now, "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z", ttl)
		if err != nil {
			t.Fatal(err)
		}
		if issued != "2026-01-01T00:00:00Z" || expires != "2026-02-01T00:00:00Z" {
			t.Fatalf("issued=%s expires=%s", issued, expires)
		}
	})

	t.Run("expires before issued refused", func(t *testing.T) {
		if _, _, err := statementTimes(now, "2026-02-01T00:00:00Z", "2026-01-01T00:00:00Z", ttl); err == nil {
			t.Fatal("accepted expires before issued")
		}
	})

	t.Run("unparseable issued refused", func(t *testing.T) {
		if _, _, err := statementTimes(now, "not-a-time", "", ttl); err == nil {
			t.Fatal("accepted unparseable issued-at")
		}
	})
}
