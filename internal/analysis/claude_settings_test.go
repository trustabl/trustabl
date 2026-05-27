package analysis_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestClaudeSettings_ParsesPermissions(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/settings.json", `{
		"permissions": {
			"allow": ["Bash(npm run *)", "Read(./.env)"],
			"deny":  ["Bash(curl *)", "WebFetch"],
			"ask":   ["Edit(./src/**)"],
			"defaultMode": "acceptEdits",
			"additionalDirectories": ["../docs/"]
		},
		"env": {"FOO": "bar"},
		"hooks": {"PreSessionStart": {"type": "command", "command": "./hooks/pre.sh"}}
	}`)

	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentClaudeSettings, Path: ".claude/settings.json"},
		},
	}
	got := analysis.DiscoverClaudeSettings(manifest)
	if len(got) != 1 {
		t.Fatalf("expected 1 settings record, got %d", len(got))
	}
	s := got[0]
	if filepath.ToSlash(s.FilePath) != ".claude/settings.json" {
		t.Errorf("FilePath = %v", s.FilePath)
	}
	if s.DefaultMode != "acceptEdits" {
		t.Errorf("DefaultMode = %v", s.DefaultMode)
	}
	wantAllow := []models.PermissionRule{
		{Tool: "Bash", Pattern: "npm run *", Raw: "Bash(npm run *)", Line: 3},
		{Tool: "Read", Pattern: "./.env", Raw: "Read(./.env)", Line: 3},
	}
	if !reflect.DeepEqual(s.Permissions.Allow, wantAllow) {
		t.Errorf("Allow = %+v\nwant %+v", s.Permissions.Allow, wantAllow)
	}
	wantDeny := []models.PermissionRule{
		{Tool: "Bash", Pattern: "curl *", Raw: "Bash(curl *)", Line: 4},
		{Tool: "WebFetch", Raw: "WebFetch", Line: 4},
	}
	if !reflect.DeepEqual(s.Permissions.Deny, wantDeny) {
		t.Errorf("Deny = %+v\nwant %+v", s.Permissions.Deny, wantDeny)
	}
	if !s.HasEnvBlock || !s.HasHooks {
		t.Errorf("HasEnvBlock=%v HasHooks=%v", s.HasEnvBlock, s.HasHooks)
	}
	if !reflect.DeepEqual(s.AdditionalDirs, []string{"../docs/"}) {
		t.Errorf("AdditionalDirs = %v", s.AdditionalDirs)
	}
}

func TestParsePermissionRule_Grammar(t *testing.T) {
	cases := []struct {
		raw, tool, pattern string
	}{
		{"Bash", "Bash", ""},
		{"Bash(npm install)", "Bash", "npm install"},
		{"Read(./secrets/**)", "Read", "./secrets/**"},
		{"WebFetch(domain:example.com)", "WebFetch", "domain:example.com"},
		{"MCP(server:github)", "MCP", "server:github"},
		{"mcp__github__list_issues", "MCP", "github__list_issues"},
		{"Agent(researcher)", "Agent", "researcher"},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got := analysis.ParsePermissionRule(c.raw)
			if got.Tool != c.tool || got.Pattern != c.pattern || got.Raw != c.raw {
				t.Errorf("ParsePermissionRule(%q) = %+v, want tool=%q pattern=%q",
					c.raw, got, c.tool, c.pattern)
			}
		})
	}
}

func TestClaudeSettings_MalformedJSONSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/settings.json", `{not json`)
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentClaudeSettings, Path: ".claude/settings.json"},
		},
	}
	if got := analysis.DiscoverClaudeSettings(manifest); len(got) != 0 {
		t.Errorf("expected zero settings from malformed JSON, got %+v", got)
	}
}

func TestClaudeSettings_EnvNullIsNotPresent(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/settings.json", `{"env": null, "hooks": null}`)
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentClaudeSettings, Path: ".claude/settings.json"},
		},
	}
	got := analysis.DiscoverClaudeSettings(manifest)
	if len(got) != 1 {
		t.Fatalf("expected 1 settings record, got %d", len(got))
	}
	if got[0].HasEnvBlock {
		t.Errorf(`"env": null must yield HasEnvBlock=false`)
	}
	if got[0].HasHooks {
		t.Errorf(`"hooks": null must yield HasHooks=false`)
	}
}

func TestClaudeSettings_SandboxBlockDetected(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/settings.json", `{"sandbox": {"networkAccess": "localhost-only"}}`)
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentClaudeSettings, Path: ".claude/settings.json"},
		},
	}
	got := analysis.DiscoverClaudeSettings(manifest)
	if len(got) != 1 || !got[0].HasSandboxBlock {
		t.Fatalf("expected HasSandboxBlock=true, got %+v", got)
	}
}

func TestClaudeSettings_LocationLineRange(t *testing.T) {
	// Fixture: 5 lines, trailing newline.
	//   line 1: {
	//   line 2:   "permissions": {
	//   line 3:     "allow": ["Bash", "Read"]
	//   line 4:   }
	//   line 5: }
	const body = "{\n  \"permissions\": {\n    \"allow\": [\"Bash\", \"Read\"]\n  }\n}\n"
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/settings.json", body)
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentClaudeSettings, Path: ".claude/settings.json"},
		},
	}
	got := analysis.DiscoverClaudeSettings(manifest)
	if len(got) != 1 {
		t.Fatalf("expected 1 settings file, got %d", len(got))
	}
	if got[0].Line != 1 {
		t.Errorf("Line = %d, want 1", got[0].Line)
	}
	if got[0].EndLine != 5 {
		t.Errorf("EndLine = %d, want 5", got[0].EndLine)
	}
}

func TestDiscoverClaudeSettings_PerRuleLines(t *testing.T) {
	// Fixture lines:
	//   1: {
	//   2:   "permissions": {
	//   3:     "allow": [
	//   4:       "Bash",
	//   5:       "Read(./*)"
	//   6:     ],
	//   7:     "deny": ["Write"]
	//   8:   }
	//   9: }
	const body = `{
  "permissions": {
    "allow": [
      "Bash",
      "Read(./*)"
    ],
    "deny": ["Write"]
  }
}
`
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/settings.json", body)
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentClaudeSettings, Path: ".claude/settings.json"},
		},
	}
	got := analysis.DiscoverClaudeSettings(manifest)
	if len(got) != 1 {
		t.Fatalf("expected 1 settings file, got %d", len(got))
	}
	allow := got[0].Permissions.Allow
	if len(allow) != 2 {
		t.Fatalf("allow len = %d, want 2", len(allow))
	}
	if allow[0].Line != 4 {
		t.Errorf("allow[0] (Bash) Line = %d, want 4", allow[0].Line)
	}
	if allow[1].Line != 5 {
		t.Errorf("allow[1] (Read(./*)) Line = %d, want 5", allow[1].Line)
	}
	deny := got[0].Permissions.Deny
	if len(deny) != 1 {
		t.Fatalf("deny len = %d, want 1", len(deny))
	}
	if deny[0].Line != 7 {
		t.Errorf("deny[0] (Write) Line = %d, want 7", deny[0].Line)
	}
}

// TestDiscoverClaudeSettings_NoTrailingNewline covers the EndLine branch that
// adds 1 when the file does not end in '\n'. A 3-line file written without a
// final newline must still report EndLine == 3.
func TestDiscoverClaudeSettings_NoTrailingNewline(t *testing.T) {
	const body = "{\n  \"permissions\": {}\n}" // 3 lines, no trailing newline
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/settings.json", body)
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentClaudeSettings, Path: ".claude/settings.json"},
		},
	}
	got := analysis.DiscoverClaudeSettings(manifest)
	if len(got) != 1 {
		t.Fatalf("expected 1 settings file, got %d", len(got))
	}
	if got[0].Line != 1 {
		t.Errorf("Line = %d, want 1", got[0].Line)
	}
	if got[0].EndLine != 3 {
		t.Errorf("EndLine = %d, want 3 (no trailing newline must still count the last line)", got[0].EndLine)
	}
}

// TestDiscoverClaudeSettings_SameLineRules verifies that multiple rules packed
// onto one source line all receive that same line number through the full
// parse+zip path (not just the extractPermissionLines helper in isolation).
func TestDiscoverClaudeSettings_SameLineRules(t *testing.T) {
	// Layout:
	//   1: {
	//   2:   "permissions": {
	//   3:     "allow": ["Bash", "Read", "Grep"]
	//   4:   }
	//   5: }
	const body = `{
  "permissions": {
    "allow": ["Bash", "Read", "Grep"]
  }
}
`
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/settings.json", body)
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentClaudeSettings, Path: ".claude/settings.json"},
		},
	}
	got := analysis.DiscoverClaudeSettings(manifest)
	if len(got) != 1 {
		t.Fatalf("expected 1 settings file, got %d", len(got))
	}
	allow := got[0].Permissions.Allow
	if len(allow) != 3 {
		t.Fatalf("allow len = %d, want 3", len(allow))
	}
	for i, r := range allow {
		if r.Line != 3 {
			t.Errorf("allow[%d] (%s) Line = %d, want 3 (all three rules share line 3)", i, r.Raw, r.Line)
		}
	}
}

func TestParsePermissionRule_EdgeCases(t *testing.T) {
	cases := []struct {
		raw, tool, pattern string
	}{
		{"Bash()", "Bash", ""},       // empty parens — not distinguished from bare "Bash"
		{"mcp__", "", ""},            // bare mcp__ prefix, nothing after — unrecognized
		{"Bash(unclosed", "", ""},    // malformed — unrecognized, Raw preserved
		{"", "", ""},                 // empty string
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got := analysis.ParsePermissionRule(c.raw)
			if got.Tool != c.tool || got.Pattern != c.pattern {
				t.Errorf("ParsePermissionRule(%q) = %+v, want tool=%q pattern=%q", c.raw, got, c.tool, c.pattern)
			}
			if got.Raw != c.raw {
				t.Errorf("ParsePermissionRule(%q).Raw = %q, want %q", c.raw, got.Raw, c.raw)
			}
		})
	}
}
