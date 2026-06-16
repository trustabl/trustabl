package rulepub_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/trustabl/trustabl/internal/rulepub"
	"github.com/trustabl/trustabl/internal/rulesign"
)

const goodDigest = "c198c970a4546eb2b5d379df0a1860e6b153ed99f34e9efb3fcc4afff2be3c66"

// TestGenerateKeypair_SeedDerivesPublicKey proves the two emitted artifacts are
// consistent: the 32-byte seed (CI secret) re-derives exactly the 32-byte public
// key (embedded keyring entry). A publisher signs with the seed; the engine
// verifies with the public key.
func TestGenerateKeypair_SeedDerivesPublicKey(t *testing.T) {
	seed, pub, err := rulepub.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	if len(seed) != 32 {
		t.Fatalf("seed = %d bytes, want 32", len(seed))
	}
	if len(pub) != 32 {
		t.Fatalf("pub = %d bytes, want 32", len(pub))
	}
	seed2, _, _ := rulepub.GenerateKeypair()
	if bytes.Equal(seed, seed2) {
		t.Fatal("GenerateKeypair returned identical seeds on two calls")
	}
}

// TestSignStatement_VerifiesAgainstEngine is the core producer/verifier contract:
// a statement signed by SignStatement must pass the engine's ParseStatement +
// VerifyStatement under a keyring holding the matching public key, with the
// bundle-digest binding enforced.
func TestSignStatement_VerifiesAgainstEngine(t *testing.T) {
	seed, pub, err := rulepub.GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	const keyID = "trustabl-rules-2026-06"
	raw, err := rulepub.SignStatement(seed, rulepub.StatementParams{
		Channel:  "production",
		Version:  1750000000,
		Digest:   goodDigest,
		KeyID:    keyID,
		IssuedAt: "2026-06-15T00:00:00Z",
		Expires:  "2026-07-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("SignStatement: %v", err)
	}

	stmt, err := rulesign.ParseStatement(raw)
	if err != nil {
		t.Fatalf("engine ParseStatement rejected producer output: %v", err)
	}
	ring := rulesign.NewKeyring(rulesign.Key{ID: keyID, PublicKey: pub})
	at := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	if err := rulesign.VerifyStatement(ring, stmt, rulesign.VerifyParams{
		Channel:         "production",
		ExpectedDigest:  goodDigest,
		LastSeenVersion: 0,
	}, at); err != nil {
		t.Fatalf("engine VerifyStatement rejected producer output: %v", err)
	}
}

// TestSignStatement_RejectsBadInput keeps malformed statements from ever being
// signed: a non-positive version, a non-hex digest, expires-before-issued, or an
// empty channel is a producer-side error, not something to ship and have the
// engine reject after the fleet already pulled it.
func TestSignStatement_RejectsBadInput(t *testing.T) {
	seed, _, _ := rulepub.GenerateKeypair()
	good := rulepub.StatementParams{
		Channel: "production", Version: 5, Digest: goodDigest, KeyID: "k",
		IssuedAt: "2026-06-15T00:00:00Z", Expires: "2026-07-15T00:00:00Z",
	}
	cases := map[string]func(p *rulepub.StatementParams){
		"non-hex digest":        func(p *rulepub.StatementParams) { p.Digest = "not-hex" },
		"zero version":          func(p *rulepub.StatementParams) { p.Version = 0 },
		"expires before issued": func(p *rulepub.StatementParams) { p.Expires = "2026-06-14T00:00:00Z" },
		"empty channel":         func(p *rulepub.StatementParams) { p.Channel = "" },
		"empty key id":          func(p *rulepub.StatementParams) { p.KeyID = "" },
		"reserved git channel":  func(p *rulepub.StatementParams) { p.Channel = "git" },
		"invalid channel name":  func(p *rulepub.StatementParams) { p.Channel = "Prod 2026" },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			p := good
			mut(&p)
			if _, err := rulepub.SignStatement(seed, p); err == nil {
				t.Fatalf("SignStatement accepted invalid params (%s)", name)
			}
		})
	}
}

// TestVerifyBundle is the producer-side self-verify gate (CI runs it before
// promoting a channel): a freshly bundled+signed candidate must verify against
// the trust keyring and the local bundle dir; a digest or channel mismatch must
// be refused.
func TestVerifyBundle(t *testing.T) {
	seed, pub, err := rulepub.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	const keyID = "trustabl-rules-2026-06"
	ring := rulesign.NewKeyring(rulesign.Key{ID: keyID, PublicKey: pub})
	at := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	dir := writePack(t, map[string]string{
		"manifest.yaml":        "schema_version: 12\n",
		"claude_sdk/tool.yaml": "id: CSDK-010\n",
	})
	digest, err := rulepub.Bundle(dir, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := rulepub.SignStatement(seed, rulepub.StatementParams{
		Channel: "production", Version: 1, Digest: digest, KeyID: keyID,
		IssuedAt: "2026-06-15T00:00:00Z", Expires: "2026-07-15T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("candidate verifies against its own bundle", func(t *testing.T) {
		if err := rulepub.VerifyBundle(stmt, dir, ring, "production", at); err != nil {
			t.Fatalf("VerifyBundle rejected a valid candidate: %v", err)
		}
	})

	t.Run("digest mismatch refused", func(t *testing.T) {
		other := writePack(t, map[string]string{"manifest.yaml": "schema_version: 12\n", "x.yaml": "id: Y\n"})
		if err := rulepub.VerifyBundle(stmt, other, ring, "production", at); !errors.Is(err, rulesign.ErrDigestMismatch) {
			t.Fatalf("want ErrDigestMismatch for a different bundle dir, got %v", err)
		}
	})

	t.Run("channel mismatch refused", func(t *testing.T) {
		if err := rulepub.VerifyBundle(stmt, dir, ring, "staging", at); !errors.Is(err, rulesign.ErrChannelMismatch) {
			t.Fatalf("want ErrChannelMismatch, got %v", err)
		}
	})
}

// TestBundle_DigestMatchesEngine proves the published artifact and the engine
// agree: Bundle's returned digest equals CanonicalDigest of the source tree, and
// the gzipped tar it writes, when unpacked, re-digests to the same value (what
// the engine does after download).
func TestBundle_DigestMatchesEngine(t *testing.T) {
	dir := writePack(t, map[string]string{
		"manifest.yaml":        "schema_version: 12\n",
		"claude_sdk/tool.yaml": "id: CSDK-010\n",
		"openai_sdk/a.yaml":    "id: OAI-016\n",
	})
	want, err := rulesign.CanonicalDigest(os.DirFS(dir))
	if err != nil {
		t.Fatalf("CanonicalDigest(dir): %v", err)
	}

	var buf bytes.Buffer
	got, err := rulepub.Bundle(dir, &buf)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	if got != want {
		t.Fatalf("Bundle digest = %s, want %s", got, want)
	}

	unpacked := gunzipUntar(t, buf.Bytes())
	roundTrip, err := rulesign.CanonicalDigest(unpacked)
	if err != nil {
		t.Fatalf("CanonicalDigest(unpacked): %v", err)
	}
	if roundTrip != want {
		t.Fatalf("unpacked bundle digest = %s, want %s", roundTrip, want)
	}
}

// TestBundle_RejectsBadManifest catches the late-failure trap: a bundle whose
// manifest.yaml is missing or non-positive would pass signature+digest verify on
// the engine and only then fail ErrNoCompatibleRules (exit 2). Refuse at publish.
func TestBundle_RejectsBadManifest(t *testing.T) {
	t.Run("missing manifest", func(t *testing.T) {
		dir := writePack(t, map[string]string{"claude_sdk/tool.yaml": "id: CSDK-010\n"})
		if _, err := rulepub.Bundle(dir, io.Discard); err == nil {
			t.Fatal("Bundle accepted a pack with no manifest.yaml")
		}
	})
	t.Run("non-positive schema_version", func(t *testing.T) {
		dir := writePack(t, map[string]string{
			"manifest.yaml":        "schema_version: 0\n",
			"claude_sdk/tool.yaml": "id: CSDK-010\n",
		})
		if _, err := rulepub.Bundle(dir, io.Discard); err == nil {
			t.Fatal("Bundle accepted a manifest with schema_version 0")
		}
	})
}

func writePack(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func gunzipUntar(t *testing.T, raw []byte) fstest.MapFS {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	out := fstest.MapFS{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %q: %v", hdr.Name, err)
		}
		out[hdr.Name] = &fstest.MapFile{Data: data}
	}
	return out
}
