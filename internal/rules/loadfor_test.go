package rules_test

import (
	"errors"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
)

// TestLoadFor_ZeroRulePackErrors covers the "engine never runs rule-less"
// contract: a pack that decodes and is schema-compatible but carries zero
// rules (an empty/truncated checkout) must hard-fail with ErrNoRulesInPack,
// never silently yield an empty registry that produces a clean report.
func TestLoadFor_ZeroRulePackErrors(t *testing.T) {
	const emptyPolicy = `
policy:
  id: empty
  name: Empty
  category: claude_sdk
  description: A pack with no rules.
rules: []
`
	fsys := makeFS(map[string]string{"empty.yaml": emptyPolicy})
	if _, err := rules.LoadFor(fsys, []models.SDK{models.SDKClaudeAgentSDK}); !errors.Is(err, rules.ErrNoRulesInPack) {
		t.Fatalf("LoadFor on a zero-rule pack: want ErrNoRulesInPack, got %v", err)
	}
}

// TestLoadFor_SDKWithNoMatchingPackSucceeds guards against the zero-rules check
// over-firing. The pack is non-empty but its only rule targets claude_sdk while
// the repo uses Google ADK — a legitimate "repo's SDKs have no matching pack"
// situation. The unfiltered rule count is > 0, so LoadFor must succeed and
// return a (possibly detector-light) registry, not ErrNoRulesInPack.
func TestLoadFor_SDKWithNoMatchingPackSucceeds(t *testing.T) {
	fsys := makeFS(map[string]string{"test.yaml": validYAML})
	reg, err := rules.LoadFor(fsys, []models.SDK{models.SDKGoogleADK})
	if err != nil {
		t.Fatalf("unexpected error for non-empty pack with non-matching SDK: %v", err)
	}
	if reg == nil {
		t.Fatal("expected a non-nil registry")
	}
}
