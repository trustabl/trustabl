package analysis_test

import (
	"reflect"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestSkills_ParsesSkillMd(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".claude/skills/deploy/SKILL.md",
		"---\nname: deploy\ndescription: Deploy to prod\nallowed-tools: Read Bash(git push *)\ndisable-model-invocation: true\nargument-hint: \"[env]\"\n---\n\nDeploy steps\n")
	manifest := models.ScanManifest{
		RepoRoot:      dir,
		MarkdownFiles: []string{".claude/skills/deploy/SKILL.md"},
	}
	got := analysis.DiscoverSkills(manifest)
	if len(got) != 1 {
		t.Fatalf("got %d skills, want 1", len(got))
	}
	s := got[0]
	if s.Name != "deploy" || s.Description != "Deploy to prod" {
		t.Errorf("name/desc = %q / %q", s.Name, s.Description)
	}
	if !reflect.DeepEqual(s.AllowedTools, []string{"Read", "Bash(git push *)"}) {
		t.Errorf("AllowedTools = %v", s.AllowedTools)
	}
	if len(s.ToolGrants) != 2 || s.ToolGrants[1].Tool != "Bash" || s.ToolGrants[1].Pattern != "git push *" {
		t.Errorf("ToolGrants = %+v", s.ToolGrants)
	}
	if !s.DisableModelInvocation {
		t.Errorf("DisableModelInvocation = false, want true")
	}
	if s.ArgumentHint != "[env]" {
		t.Errorf("ArgumentHint = %q", s.ArgumentHint)
	}
	if s.FilePath != ".claude/skills/deploy/SKILL.md" {
		t.Errorf("FilePath = %q", s.FilePath)
	}
}

func TestSkills_NonSkillMarkdownIgnored(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "docs/readme.md", "---\nname: x\n---\n")
	manifest := models.ScanManifest{RepoRoot: dir, MarkdownFiles: []string{"docs/readme.md"}}
	if got := analysis.DiscoverSkills(manifest); len(got) != 0 {
		t.Fatalf("got %d skills, want 0", len(got))
	}
}

func TestSkills_NoFrontmatterOrNoNameSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a/SKILL.md", "No frontmatter here\n")
	writeFixture(t, dir, "b/SKILL.md", "---\ndescription: nameless\n---\n")
	manifest := models.ScanManifest{RepoRoot: dir, MarkdownFiles: []string{"a/SKILL.md", "b/SKILL.md"}}
	if got := analysis.DiscoverSkills(manifest); len(got) != 0 {
		t.Fatalf("got %d skills, want 0 (no frontmatter / no name)", len(got))
	}
}

func TestSkills_DeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "z/SKILL.md", "---\nname: z\n---\n")
	writeFixture(t, dir, "a/SKILL.md", "---\nname: a\n---\n")
	manifest := models.ScanManifest{RepoRoot: dir, MarkdownFiles: []string{"z/SKILL.md", "a/SKILL.md"}}
	got := analysis.DiscoverSkills(manifest)
	if len(got) != 2 || got[0].FilePath > got[1].FilePath {
		t.Fatalf("expected sorted by FilePath, got %+v", got)
	}
}
