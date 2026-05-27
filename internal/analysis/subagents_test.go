package analysis_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func writeFixture(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSubagents_ParsesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/agents/inbox-searcher.md", `---
name: inbox-searcher
description: "Email search specialist."
tools: Read, Bash, Glob, Grep, mcp__email__search_inbox
---

# Body content
`)
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSubagent, Path: ".claude/agents/inbox-searcher.md"},
		},
	}
	got := analysis.DiscoverSubagents(manifest)
	if len(got) != 1 {
		t.Fatalf("expected 1 subagent, got %d", len(got))
	}
	want := models.SubagentDef{
		Name:        "inbox-searcher",
		Description: "Email search specialist.",
		Tools:       []string{"Read", "Bash", "Glob", "Grep", "mcp__email__search_inbox"},
		ToolGrants: []models.ToolGrant{
			{Tool: "Read", Raw: "Read"},
			{Tool: "Bash", Raw: "Bash"},
			{Tool: "Glob", Raw: "Glob"},
			{Tool: "Grep", Raw: "Grep"},
			{Tool: "MCP", Pattern: "email__search_inbox", Raw: "mcp__email__search_inbox"},
		},
		Location: models.Location{FilePath: ".claude/agents/inbox-searcher.md", Line: 1, EndLine: 5},
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("got  %+v\nwant %+v", got[0], want)
	}
}

func TestSubagents_NoFrontmatterReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/agents/x.md", "Just a body, no frontmatter.\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSubagent, Path: ".claude/agents/x.md"},
		},
	}
	if got := analysis.DiscoverSubagents(manifest); len(got) != 0 {
		t.Errorf("expected zero subagents from body-only file, got %+v", got)
	}
}

func TestSubagents_DeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/agents/b.md", "---\nname: b\ndescription: B\n---\n")
	writeFixture(t, dir, ".claude/agents/a.md", "---\nname: a\ndescription: A\n---\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSubagent, Path: ".claude/agents/b.md"},
			{Kind: models.ComponentSubagent, Path: ".claude/agents/a.md"},
		},
	}
	got := analysis.DiscoverSubagents(manifest)
	if len(got) != 2 || got[0].FilePath > got[1].FilePath {
		t.Errorf("expected sorted by FilePath, got %+v", got)
	}
}

func TestSubagents_ModelField(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/agents/r.md", "---\nname: r\ndescription: R\ntools: Read\nmodel: haiku\n---\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSubagent, Path: ".claude/agents/r.md"},
		},
	}
	got := analysis.DiscoverSubagents(manifest)
	if len(got) != 1 || got[0].Model != "haiku" {
		t.Errorf("expected model=haiku, got %+v", got)
	}
}

func TestSubagents_ToolsAsYAMLList(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/agents/listy.md", "---\nname: listy\ndescription: D\ntools:\n  - Read\n  - Bash\n  - Grep\n---\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSubagent, Path: ".claude/agents/listy.md"},
		},
	}
	got := analysis.DiscoverSubagents(manifest)
	if len(got) != 1 {
		t.Fatalf("expected 1 subagent (YAML-list tools must not skip the file), got %d", len(got))
	}
	if !reflect.DeepEqual(got[0].Tools, []string{"Read", "Bash", "Grep"}) {
		t.Errorf("Tools = %v, want [Read Bash Grep]", got[0].Tools)
	}
}

func TestSubagents_FrontmatterWithoutNameSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/agents/noname.md", "---\ndescription: has a description but no name\ntools: Read\n---\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSubagent, Path: ".claude/agents/noname.md"},
		},
	}
	if got := analysis.DiscoverSubagents(manifest); len(got) != 0 {
		t.Errorf("expected zero subagents (frontmatter with no name must be skipped), got %+v", got)
	}
}

func TestSubagents_CapturesSecurityFields(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/agents/risky.md",
		"---\n"+
			"name: risky\n"+
			"description: D\n"+
			"tools: Read, Bash(npm run *), mcp__email__search_inbox\n"+
			"disallowedTools: Write, Edit\n"+
			"model: opus\n"+
			"permissionMode: bypassPermissions\n"+
			"mcpServers: slack, github\n"+
			"skills: deploy, summarize\n"+
			"isolation: worktree\n"+
			"hooks:\n  PreToolUse: ./x.sh\n"+
			"---\n\nBody\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSubagent, Path: ".claude/agents/risky.md"},
		},
	}
	got := analysis.DiscoverSubagents(manifest)
	if len(got) != 1 {
		t.Fatalf("got %d subagents, want 1", len(got))
	}
	s := got[0]
	if !reflect.DeepEqual(s.Tools, []string{"Read", "Bash(npm run *)", "mcp__email__search_inbox"}) {
		t.Errorf("Tools = %v", s.Tools)
	}
	if len(s.ToolGrants) != 3 || s.ToolGrants[1].Tool != "Bash" || s.ToolGrants[1].Pattern != "npm run *" {
		t.Errorf("ToolGrants = %+v", s.ToolGrants)
	}
	if s.ToolGrants[2].Tool != "MCP" {
		t.Errorf("mcp grant Tool = %q, want MCP", s.ToolGrants[2].Tool)
	}
	if !reflect.DeepEqual(s.DisallowedTools, []string{"Write", "Edit"}) {
		t.Errorf("DisallowedTools = %v", s.DisallowedTools)
	}
	if s.PermissionMode != "bypassPermissions" {
		t.Errorf("PermissionMode = %q", s.PermissionMode)
	}
	if !reflect.DeepEqual(s.MCPServers, []string{"slack", "github"}) {
		t.Errorf("MCPServers = %v", s.MCPServers)
	}
	if !reflect.DeepEqual(s.Skills, []string{"deploy", "summarize"}) {
		t.Errorf("Skills = %v", s.Skills)
	}
	if s.Isolation != "worktree" {
		t.Errorf("Isolation = %q", s.Isolation)
	}
	if !s.HasHooks {
		t.Errorf("HasHooks = false, want true")
	}
}

// TestSubagents_HasHooksOnlyForRealHandlers guards the HasHooks predicate: it
// must be true only when hooks: carries an actual mapping of handlers, and
// false when the key is absent, null, or an empty map. A bare yaml.Node.IsZero()
// check is wrong because an explicit null populates the node.
func TestSubagents_HasHooksOnlyForRealHandlers(t *testing.T) {
	cases := []struct {
		name string
		fm   string
		want bool
	}{
		{"absent", "---\nname: a\ndescription: D\n---\n", false},
		{"null", "---\nname: a\ndescription: D\nhooks:\n---\n", false},
		{"tilde null", "---\nname: a\ndescription: D\nhooks: ~\n---\n", false},
		{"empty map", "---\nname: a\ndescription: D\nhooks: {}\n---\n", false},
		{"real handler", "---\nname: a\ndescription: D\nhooks:\n  PreToolUse: ./x.sh\n---\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixture(t, dir, ".claude/agents/h.md", tc.fm)
			manifest := models.ScanManifest{
				RepoRoot: dir,
				Components: []models.AgentComponent{
					{Kind: models.ComponentSubagent, Path: ".claude/agents/h.md"},
				},
			}
			got := analysis.DiscoverSubagents(manifest)
			if len(got) != 1 {
				t.Fatalf("got %d subagents, want 1", len(got))
			}
			if got[0].HasHooks != tc.want {
				t.Errorf("HasHooks = %v, want %v", got[0].HasHooks, tc.want)
			}
		})
	}
}

func TestSubagents_CRLFLineEndings(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/agents/crlf.md", "---\r\nname: crlf\r\ndescription: D\r\ntools: Read, Bash\r\n---\r\n\r\nBody\r\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSubagent, Path: ".claude/agents/crlf.md"},
		},
	}
	got := analysis.DiscoverSubagents(manifest)
	if len(got) != 1 || got[0].Name != "crlf" {
		t.Fatalf("expected 1 subagent named crlf from a CRLF file, got %+v", got)
	}
	if !reflect.DeepEqual(got[0].Tools, []string{"Read", "Bash"}) {
		t.Errorf("Tools = %v, want [Read Bash]", got[0].Tools)
	}
}

func TestSubagents_FlatCollectionByShape(t *testing.T) {
	dir := t.TempDir()
	// VoltAgent layout: subagent markdown under categories/, NOT .claude/agents/.
	writeFixture(t, dir, "categories/01-core/api-designer.md",
		"---\nname: api-designer\ndescription: D\ntools: Read, Write, Bash\nmodel: sonnet\n---\n\nBody\n")
	// A generic doc with frontmatter but no subagent shape must NOT be picked up.
	writeFixture(t, dir, "docs/post.md",
		"---\ntitle: Hello\ndate: 2026-01-01\n---\n\n# Post\n")
	// A plain README (no frontmatter) must NOT be picked up.
	writeFixture(t, dir, "categories/01-core/README.md", "# Index\n")

	manifest := models.ScanManifest{
		RepoRoot: dir,
		MarkdownFiles: []string{
			"categories/01-core/api-designer.md",
			"docs/post.md",
			"categories/01-core/README.md",
		},
	}
	got := analysis.DiscoverSubagents(manifest)
	if len(got) != 1 {
		t.Fatalf("got %d subagents, want 1 (only api-designer)", len(got))
	}
	if got[0].Name != "api-designer" {
		t.Errorf("Name = %q, want api-designer", got[0].Name)
	}
}

func TestSubagents_NoDoubleCountCanonicalAndMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/agents/a.md", "---\nname: a\ndescription: D\ntools: Read\n---\n")
	// Same file appears in BOTH Components (canonical) and MarkdownFiles (walk).
	manifest := models.ScanManifest{
		RepoRoot:      dir,
		Components:    []models.AgentComponent{{Kind: models.ComponentSubagent, Path: ".claude/agents/a.md"}},
		MarkdownFiles: []string{".claude/agents/a.md"},
	}
	got := analysis.DiscoverSubagents(manifest)
	if len(got) != 1 {
		t.Fatalf("got %d subagents, want 1 (no double count)", len(got))
	}
}

func TestSubagents_CanonicalWithoutToolsStillDiscovered(t *testing.T) {
	dir := t.TempDir()
	// A canonical .claude/agents file with no tools/model (inherits all) is still
	// a real subagent — the shape gate must NOT apply to canonical-path files.
	writeFixture(t, dir, ".claude/agents/inheritor.md", "---\nname: inheritor\ndescription: D\n---\n")
	manifest := models.ScanManifest{
		RepoRoot:   dir,
		Components: []models.AgentComponent{{Kind: models.ComponentSubagent, Path: ".claude/agents/inheritor.md"}},
	}
	got := analysis.DiscoverSubagents(manifest)
	if len(got) != 1 || got[0].Name != "inheritor" {
		t.Fatalf("canonical tool-less subagent not discovered: %+v", got)
	}
}

func TestSubagentDef_LocationLineRange(t *testing.T) {
	// Fixture:
	//   line 1: ---
	//   line 2: name: foo
	//   line 3: description: a foo
	//   line 4: tools: Read, Grep
	//   line 5: ---
	//   line 6: (blank)
	//   line 7: Body content.
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/agents/foo.md", "---\nname: foo\ndescription: a foo\ntools: Read, Grep\n---\n\nBody content.\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSubagent, Path: ".claude/agents/foo.md"},
		},
	}
	got := analysis.DiscoverSubagents(manifest)
	if len(got) != 1 {
		t.Fatalf("expected 1 subagent, got %d", len(got))
	}
	if got[0].Line != 1 {
		t.Errorf("Line = %d, want 1", got[0].Line)
	}
	if got[0].EndLine != 5 {
		t.Errorf("EndLine = %d, want 5", got[0].EndLine)
	}
}
