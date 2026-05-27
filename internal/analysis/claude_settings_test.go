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
		{Tool: "Bash", Pattern: "npm run *", Raw: "Bash(npm run *)"},
		{Tool: "Read", Pattern: "./.env", Raw: "Read(./.env)"},
	}
	if !reflect.DeepEqual(s.Permissions.Allow, wantAllow) {
		t.Errorf("Allow = %+v\nwant %+v", s.Permissions.Allow, wantAllow)
	}
	wantDeny := []models.PermissionRule{
		{Tool: "Bash", Pattern: "curl *", Raw: "Bash(curl *)"},
		{Tool: "WebFetch", Raw: "WebFetch"},
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
