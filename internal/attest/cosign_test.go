package attest

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestAttestArgs(t *testing.T) {
	t.Run("keyless", func(t *testing.T) {
		got := attestArgs(AttestOptions{Predicate: "p.json", Bundle: "b.json", Blob: "report.json"})
		want := []string{
			"attest-blob", "--predicate", "p.json", "--type", PredicateType,
			"--bundle", "b.json", "--yes", "report.json",
		}
		if !slices.Equal(got, want) {
			t.Errorf("attestArgs keyless = %v, want %v", got, want)
		}
	})
	t.Run("key + no-tlog", func(t *testing.T) {
		got := attestArgs(AttestOptions{Predicate: "p.json", Bundle: "b.json", Blob: "report.json", KeyRef: "k.key", NoTLog: true})
		want := []string{
			"attest-blob", "--predicate", "p.json", "--type", PredicateType,
			"--bundle", "b.json", "--yes", "--key", "k.key", "--tlog-upload=false", "report.json",
		}
		if !slices.Equal(got, want) {
			t.Errorf("attestArgs key = %v, want %v", got, want)
		}
	})
	t.Run("key + no-tlog on cosign v3 (signing-config)", func(t *testing.T) {
		got := attestArgs(AttestOptions{Predicate: "p.json", Bundle: "b.json", Blob: "report.json", KeyRef: "k.key", NoTLog: true, SigningConfig: "sc.json"})
		want := []string{
			"attest-blob", "--predicate", "p.json", "--type", PredicateType,
			"--bundle", "b.json", "--yes", "--key", "k.key", "--signing-config", "sc.json", "report.json",
		}
		if !slices.Equal(got, want) {
			t.Errorf("attestArgs v3 no-tlog = %v, want %v", got, want)
		}
	})
}

func TestVerifyArgs(t *testing.T) {
	t.Run("keyless", func(t *testing.T) {
		got := verifyArgs(VerifyOptions{Bundle: "b.json", Blob: "report.json", CertIdentity: "id", CertIssuer: "iss"})
		want := []string{
			"verify-blob-attestation", "--type", PredicateType, "--bundle", "b.json",
			"--certificate-identity", "id", "--certificate-oidc-issuer", "iss", "report.json",
		}
		if !slices.Equal(got, want) {
			t.Errorf("verifyArgs keyless = %v, want %v", got, want)
		}
	})
	t.Run("key + no-tlog", func(t *testing.T) {
		got := verifyArgs(VerifyOptions{Bundle: "b.json", Blob: "report.json", KeyRef: "k.pub", NoTLog: true})
		want := []string{
			"verify-blob-attestation", "--type", PredicateType, "--bundle", "b.json",
			"--key", "k.pub", "--insecure-ignore-tlog=true", "report.json",
		}
		if !slices.Equal(got, want) {
			t.Errorf("verifyArgs key = %v, want %v", got, want)
		}
	})
}

// withMissingCosign points the package at a binary that cannot exist on PATH, so
// the not-found path is exercised regardless of whether the test host has cosign
// installed.
func withMissingCosign(t *testing.T) {
	t.Helper()
	old := cosignBinary
	cosignBinary = "cosign-not-installed-9f3c1a"
	t.Cleanup(func() { cosignBinary = old })
}

func TestAttest_CosignMissing(t *testing.T) {
	withMissingCosign(t)
	err := Attest(context.Background(), AttestOptions{Blob: "report.json", Predicate: "p.json", Bundle: "b.json"})
	if !errors.Is(err, ErrCosignNotFound) {
		t.Fatalf("Attest err = %v, want ErrCosignNotFound", err)
	}
}

func TestVerify_CosignMissing(t *testing.T) {
	withMissingCosign(t)
	err := Verify(context.Background(), VerifyOptions{Blob: "report.json", Bundle: "b.json", KeyRef: "k.pub"})
	if !errors.Is(err, ErrCosignNotFound) {
		t.Fatalf("Verify err = %v, want ErrCosignNotFound", err)
	}
}

func TestCosignVersionRE(t *testing.T) {
	cases := map[string]string{
		"GitVersion:    v3.1.1\n": "3",
		"GitVersion: v2.4.3":      "2",
		"GitVersion:\t2.5.0":      "2",
	}
	for in, want := range cases {
		m := cosignVersionRE.FindStringSubmatch(in)
		if m == nil {
			t.Errorf("parse %q: no match", in)
			continue
		}
		if m[1] != want {
			t.Errorf("parse %q major = %s, want %s", in, m[1], want)
		}
	}
}
