package forge

import (
	"context"
	"sort"

	"github.com/trustabl/trustabl/internal/ingestion"
	"github.com/trustabl/trustabl/internal/models"
)

// depCategoryMap maps dep-file package names (as returned by ingestion.Recon's
// SDKDeps slice) to the detector category that audits them.
// Mirrors scanner.depNameToSDK + scanner.sdkToCategory in one table.
var depCategoryMap = map[string]models.DetectorCategory{
	"claude-agent-sdk": models.CategoryClaudeSDK,
	"openai-agents":    models.CategoryOpenAISDK,
	"google-adk":       models.CategoryGoogleADK,
	"mcp":              models.CategoryMCP,
	"langchain":        models.CategoryLangChain,
	"crewai":           models.CategoryCrewAI,
	"pydantic-ai":      models.CategoryPydanticAI,
	"vercel-ai":        models.CategoryVercelAI,
	"autogen":          models.CategoryAutoGen,
}

// DetectCategories resolves target (local path) and returns the detector
// categories for SDKs found in dep manifests (pyproject.toml, package.json,
// go.mod, etc.). Returns a sorted, deduplicated slice; returns nil (not an
// error) when no known SDK is found.
func DetectCategories(ctx context.Context, target string) ([]models.DetectorCategory, error) {
	src, err := ingestion.Resolve(ctx, target, nil)
	if err != nil {
		return nil, err
	}
	defer src.Cleanup()

	profile, err := ingestion.Recon(src, nil)
	if err != nil {
		return nil, err
	}

	seen := make(map[models.DetectorCategory]bool)
	for _, dep := range profile.SDKDeps {
		if cat, ok := depCategoryMap[dep.Name]; ok {
			seen[cat] = true
		}
	}

	cats := make([]models.DetectorCategory, 0, len(seen))
	for cat := range seen {
		cats = append(cats, cat)
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i] < cats[j] })
	return cats, nil
}

// MergeCategories merges auto-detected and explicitly specified categories,
// returning a deduplicated, sorted slice.
func MergeCategories(detected, explicit []models.DetectorCategory) []models.DetectorCategory {
	seen := make(map[models.DetectorCategory]bool)
	for _, c := range detected {
		seen[c] = true
	}
	for _, c := range explicit {
		seen[c] = true
	}
	out := make([]models.DetectorCategory, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
