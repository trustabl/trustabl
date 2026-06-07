package rules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/rules"
)

// TestLoad_SkipsSymlinks guards against a hostile rules pack escaping the pack
// directory via a symlink. os.DirFS follows symlinks on Open, so a pack from an
// attacker-influenced --rules-repo could ship `evil.yaml -> <outside>`; the
// loader must not follow it. Here the symlink target is a *valid* policy with a
// recognizable rule ID — if the loader followed it, EVIL-001 would appear.
func TestLoad_SkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.yaml"), []byte(validYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	// A valid policy living OUTSIDE the pack, reachable only via the symlink.
	outside := filepath.Join(t.TempDir(), "secret.yaml")
	if err := os.WriteFile(outside, []byte(strings.ReplaceAll(validYAML, "TEST-001", "EVIL-001")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "evil.yaml")); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}

	policies, err := rules.Load(os.DirFS(dir))
	if err != nil {
		t.Fatalf("Load errored (did it follow the symlink?): %v", err)
	}
	ids := map[string]bool{}
	for _, p := range policies {
		for _, r := range p.Rules {
			ids[r.ID] = true
		}
	}
	if ids["EVIL-001"] {
		t.Error("loader followed a symlink escaping the pack directory (EVIL-001 loaded)")
	}
	if !ids["TEST-001"] {
		t.Error("real (non-symlink) rule TEST-001 was not loaded")
	}
}
