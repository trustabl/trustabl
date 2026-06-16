package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
