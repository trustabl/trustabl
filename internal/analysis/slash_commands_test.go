package analysis_test

import (
	"reflect"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestSlashCommands_ParsesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/commands/deploy.md",
		"---\ndescription: Ship it\nallowed-tools: Bash(git push *), Read\nmodel: sonnet\nargument-hint: \"[env]\"\n---\n\nRun the deploy.\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSlashCommand, Path: ".claude/commands/deploy.md"},
		},
	}
	got := analysis.DiscoverSlashCommands(manifest)
	if len(got) != 1 {
		t.Fatalf("got %d commands, want 1", len(got))
	}
	c := got[0]
	if c.Name != "deploy" {
		t.Errorf("Name = %q, want deploy (from basename)", c.Name)
	}
	if !reflect.DeepEqual(c.AllowedTools, []string{"Bash(git push *)", "Read"}) {
		t.Errorf("AllowedTools = %v", c.AllowedTools)
	}
	if c.ToolGrants[0].Tool != "Bash" || c.ToolGrants[0].Pattern != "git push *" {
		t.Errorf("ToolGrants[0] = %+v", c.ToolGrants[0])
	}
	if c.Model != "sonnet" {
		t.Errorf("Model = %q", c.Model)
	}
	if c.ArgumentHint != "[env]" {
		t.Errorf("ArgumentHint = %q", c.ArgumentHint)
	}
}

func TestSlashCommands_NoFrontmatterStillNamed(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/commands/plain.md", "Just a prompt body, no frontmatter.\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSlashCommand, Path: ".claude/commands/plain.md"},
		},
	}
	got := analysis.DiscoverSlashCommands(manifest)
	if len(got) != 1 || got[0].Name != "plain" {
		t.Fatalf("got %+v, want one command named plain", got)
	}
	if got[0].Line != 1 {
		t.Errorf("Line = %d, want 1 for no-frontmatter command", got[0].Line)
	}
}

func TestSlashCommands_DeterministicOrderAndIgnoresOtherKinds(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/commands/z.md", "---\ndescription: Z\n---\n")
	writeFixture(t, dir, ".claude/commands/a.md", "---\ndescription: A\n---\n")
	manifest := models.ScanManifest{
		RepoRoot: dir,
		Components: []models.AgentComponent{
			{Kind: models.ComponentSlashCommand, Path: ".claude/commands/z.md"},
			{Kind: models.ComponentSubagent, Path: ".claude/agents/ignored.md"},
			{Kind: models.ComponentSlashCommand, Path: ".claude/commands/a.md"},
		},
	}
	got := analysis.DiscoverSlashCommands(manifest)
	if len(got) != 2 || got[0].FilePath > got[1].FilePath {
		t.Fatalf("expected 2 commands sorted by FilePath, got %+v", got)
	}
}
