package analysis

import (
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/trustabl/internal/models"
)

type skillFrontmatter struct {
	Name                   string       `yaml:"name"`
	Description            string       `yaml:"description"`
	AllowedTools           stringOrList `yaml:"allowed-tools"`
	ArgumentHint           string       `yaml:"argument-hint"`
	DisableModelInvocation bool         `yaml:"disable-model-invocation"`
}

// DiscoverSkills parses every SKILL.md in the manifest's markdown files. A skill
// is identified by basename (SKILL.md) at any depth — covering .claude/skills/,
// ~/.claude/skills/ layouts, plugin skills/ dirs, and nested monorepo skills.
// Files without frontmatter or without a name are skipped.
func DiscoverSkills(manifest models.ScanManifest) []models.SkillDef {
	var out []models.SkillDef
	for _, p := range manifest.MarkdownFiles {
		if filepath.Base(p) != "SKILL.md" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(manifest.RepoRoot, p))
		if err != nil {
			continue
		}
		fm, startLine, endLine, ok := extractFrontmatter(raw)
		if !ok {
			continue
		}
		var parsed skillFrontmatter
		if err := yaml.Unmarshal(fm, &parsed); err != nil {
			continue
		}
		if parsed.Name == "" {
			continue
		}
		tokens := splitToolsTokens([]string(parsed.AllowedTools))
		out = append(out, models.SkillDef{
			Name:                   parsed.Name,
			Description:            parsed.Description,
			AllowedTools:           tokens,
			ToolGrants:             parseToolGrants(tokens),
			ArgumentHint:           parsed.ArgumentHint,
			DisableModelInvocation: parsed.DisableModelInvocation,
			Location:               models.Location{FilePath: p, Line: startLine, EndLine: endLine},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	return out
}
