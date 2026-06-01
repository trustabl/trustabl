package analysis

import (
	"sort"

	"github.com/trustabl/trustabl/internal/models"
)

// saturation is the weighted-severity value at which a surface's score bottoms
// out at 0. Magic number — calibrate against a real-repo corpus before trusting
// the absolute value.
const saturation = 3.0

// blendK controls how hard the overall score is pulled toward weak surfaces.
// overall is a badness-weighted mean: weight wᵢ = 1 + blendK*(1-sᵢ). k=0 is a
// plain arithmetic mean; larger k weights low-scoring surfaces more heavily,
// without the min-cliff or divide-by-zero of harmonic/geometric means. Magic
// number — calibrate against a real-repo corpus.
const blendK = 3.0

// surfaceKey identifies a scored surface: a single tool, agent, or subagent
// (kind + file + name, so same-named entities across files stay distinct), or
// the single repo bucket (kind=repo, empty file/name).
type surfaceKey struct {
	kind     models.Scope
	filePath string
	name     string
}

// Score returns per-surface readiness and the overall score.
//
// A surface is created for every discovered tool, agent, and subagent (seeded at
// 1.0), plus one repo surface IFF at least one repo-scoped finding exists (the
// repo is not a discovered entity, so it gets no row when clean). Findings route
// to their surface by (Scope, FilePath, ToolName); all repo-scoped findings pool
// into the single repo surface. Findings whose Scope is not one of the four real
// scopes — META findings carry an empty Scope — are ignored, so info-meta signals
// never move the score.
//
// Per-surface:
//
//	weighted = Σ severityWeight(finding) * finding.confidence
//	score    = max(0, 1 - weighted/saturation)
//
// Overall is a badness-weighted mean across all surfaces (see blendK), so it
// responds to both severity and breadth. An empty surface set scores 1.0.
//
// Takes the three discovered slices explicitly (not the whole RepoInventory) to
// stay honest about exactly what scoring depends on.
func Score(tools []models.ToolDef, agents []models.AgentDef, subagents []models.SubagentDef, findings []models.Finding) ([]models.SurfaceReadiness, float64) {
	bySurface := map[surfaceKey]*models.SurfaceReadiness{}

	seed := func(kind models.Scope, filePath, name string) {
		k := surfaceKey{kind, filePath, name}
		if _, ok := bySurface[k]; !ok {
			bySurface[k] = &models.SurfaceReadiness{Kind: kind, Name: name, FilePath: filePath, Score: 1.0}
		}
	}
	for _, t := range tools {
		seed(models.ScopeTool, t.FilePath, t.Name)
	}
	for _, a := range agents {
		seed(models.ScopeAgent, a.FilePath, a.Name)
	}
	for _, s := range subagents {
		seed(models.ScopeSubagent, s.FilePath, s.Name)
	}

	for _, f := range findings {
		var k surfaceKey
		switch f.Scope {
		case models.ScopeTool, models.ScopeAgent, models.ScopeSubagent:
			k = surfaceKey{f.Scope, f.FilePath, f.ToolName}
		case models.ScopeRepo:
			// All repo findings pool into one repo surface, created on demand.
			k = surfaceKey{kind: models.ScopeRepo}
			if _, ok := bySurface[k]; !ok {
				bySurface[k] = &models.SurfaceReadiness{Kind: models.ScopeRepo, Score: 1.0}
			}
		default:
			// Empty/unknown scope (META findings) — not a scored surface.
			continue
		}
		r, ok := bySurface[k]
		if !ok {
			// A tool/agent/subagent finding whose (kind, file, name) matches no
			// discovered surface. Its attribution lives in the findings list;
			// don't invent a surface row for it.
			continue
		}
		r.FindingCount++
		r.WeightedSeverity += models.SeverityWeight(f.Severity) * f.Confidence
	}

	surfaces := make([]models.SurfaceReadiness, 0, len(bySurface))
	for _, r := range bySurface {
		s := 1.0 - r.WeightedSeverity/saturation
		if s < 0 {
			s = 0
		}
		r.Score = s
		surfaces = append(surfaces, *r)
	}
	sort.Slice(surfaces, func(i, j int) bool {
		if surfaces[i].Score != surfaces[j].Score {
			return surfaces[i].Score < surfaces[j].Score // worst first
		}
		if surfaces[i].Kind != surfaces[j].Kind {
			return surfaces[i].Kind < surfaces[j].Kind
		}
		if surfaces[i].Name != surfaces[j].Name {
			return surfaces[i].Name < surfaces[j].Name
		}
		return surfaces[i].FilePath < surfaces[j].FilePath // stable across same-named surfaces
	})

	return surfaces, overallScore(surfaces)
}

// overallScore is the badness-weighted mean of surface scores: weak surfaces
// pull harder via weight 1+blendK*(1-score). Bounded [0,1], deterministic, no
// divide-by-zero, no single-surface cliff. Empty input scores 1.0.
func overallScore(surfaces []models.SurfaceReadiness) float64 {
	if len(surfaces) == 0 {
		return 1.0
	}
	var num, den float64
	for _, s := range surfaces {
		w := 1.0 + blendK*(1.0-s.Score)
		num += w * s.Score
		den += w
	}
	return num / den
}
