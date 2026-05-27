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
		Location:    models.Location{FilePath: ".claude/agents/inbox-searcher.md", Line: 1, EndLine: 5},
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
