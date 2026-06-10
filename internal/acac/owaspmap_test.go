package acac

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var owaspIDShape = regexp.MustCompile(`^(ASI|AST)(0[1-9]|10)$`)
var ruleIDShape = regexp.MustCompile(`^[A-Z]+-\d+$`)

// TestOWASPMapShape pins the closed ID vocabulary: every mapped value is a
// well-formed ASI/AST ID, every key looks like a rule ID, and no entry is an
// empty list (omission is the only spelling of "unmapped").
func TestOWASPMapShape(t *testing.T) {
	for rule, ids := range owaspMap {
		if !ruleIDShape.MatchString(rule) {
			t.Errorf("owaspMap key %q is not a rule ID", rule)
		}
		if len(ids) == 0 {
			t.Errorf("owaspMap[%q] is an empty list; unmapped rules must be omitted entirely", rule)
		}
		for _, id := range ids {
			if !owaspIDShape.MatchString(id) {
				t.Errorf("owaspMap[%q] contains malformed OWASP ID %q", rule, id)
			}
		}
	}
}

// TestOWASPMapKeysExistInFixture guards against typo'd rule IDs: every mapped
// rule must exist in the rules fixture (the test mirror of the production
// pack).
func TestOWASPMapKeysExistInFixture(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "rules-fixture")
	idLine := regexp.MustCompile(`(?m)^\s*-\s+id:\s+([A-Z]+-\d+)\s*$`)
	known := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range idLine.FindAllStringSubmatch(string(b), -1) {
			known[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk rules fixture: %v", err)
	}
	if len(known) == 0 {
		t.Fatal("no rule IDs found in the fixture; harness assumption broken")
	}
	for rule := range owaspMap {
		if !known[rule] {
			t.Errorf("owaspMap maps %q, which does not exist in the rules fixture", rule)
		}
	}
}

func TestOWASPForUnmappedIsNil(t *testing.T) {
	if got := OWASPFor("NOPE-999"); got != nil {
		t.Errorf("OWASPFor(unmapped) = %v, want nil", got)
	}
	if got := OWASPFor("CSDK-110"); len(got) != 2 {
		t.Errorf("OWASPFor(CSDK-110) = %v, want the spec's two anchor IDs", got)
	}
	// Returned slice must be a copy: callers (and emission) must not be able
	// to mutate the pinned table.
	a := OWASPFor("CSDK-110")
	a[0] = "MUTATED"
	if b := OWASPFor("CSDK-110"); b[0] == "MUTATED" {
		t.Error("OWASPFor returns the underlying table slice; must return a copy")
	}
}
