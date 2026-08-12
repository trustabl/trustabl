package forge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestDetectCategories_PyprojectOpenAI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"),
		[]byte(`[project]\ndependencies = ["openai-agents>=0.1"]\n`), 0o644); err != nil {
		t.Fatal(err)
	}
	cats, err := DetectCategories(context.Background(), dir)
	if err != nil {
		t.Fatalf("DetectCategories: %v", err)
	}
	if len(cats) != 1 || cats[0] != models.CategoryOpenAISDK {
		t.Errorf("got %v, want [openai_sdk]", cats)
	}
}

func TestDetectCategories_PackageJSON_VercelAI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"dependencies":{"ai":"^3.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cats, err := DetectCategories(context.Background(), dir)
	if err != nil {
		t.Fatalf("DetectCategories: %v", err)
	}
	if len(cats) != 1 || cats[0] != models.CategoryVercelAI {
		t.Errorf("got %v, want [vercel_ai]", cats)
	}
}

func TestDetectCategories_NoManifest(t *testing.T) {
	dir := t.TempDir()
	cats, err := DetectCategories(context.Background(), dir)
	if err != nil {
		t.Fatalf("DetectCategories: %v", err)
	}
	if len(cats) != 0 {
		t.Errorf("expected empty, got %v", cats)
	}
}

func TestDetectCategories_MultipleManifests(t *testing.T) {
	dir := t.TempDir()
	// Python + TS in same repo
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"),
		[]byte(`[project]\ndependencies = ["crewai>=0.1"]\n`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"dependencies":{"@ai-sdk/openai":"^0.0.1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cats, err := DetectCategories(context.Background(), dir)
	if err != nil {
		t.Fatalf("DetectCategories: %v", err)
	}
	// crewai + vercel_ai, sorted
	if len(cats) != 2 {
		t.Fatalf("got %d categories, want 2: %v", len(cats), cats)
	}
	if cats[0] != models.CategoryCrewAI || cats[1] != models.CategoryVercelAI {
		t.Errorf("got %v, want [crewai vercel_ai]", cats)
	}
}

func TestMergeCategories_Dedup(t *testing.T) {
	detected := []models.DetectorCategory{models.CategoryOpenAISDK, models.CategoryMCP}
	explicit := []models.DetectorCategory{models.CategoryOpenAISDK, models.CategoryClaudeSkill}
	got := MergeCategories(detected, explicit)
	want := []models.DetectorCategory{models.CategoryClaudeSkill, models.CategoryMCP, models.CategoryOpenAISDK}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMergeCategories_EmptyDetected(t *testing.T) {
	got := MergeCategories(nil, []models.DetectorCategory{models.CategoryClaudeSDK})
	if len(got) != 1 || got[0] != models.CategoryClaudeSDK {
		t.Errorf("got %v, want [claude_sdk]", got)
	}
}
