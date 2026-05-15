package analysis

import (
	"sort"

	"github.com/trustabl/karenctl/internal/models"
)

// Score computes per-tool readiness percentages and an overall score for the repo.
//
// Per-tool algorithm (deliberately simple — calibrate against a real corpus
// before declaring it "right"):
//
//	weighted = Σ severityWeight(finding) * finding.confidence  for this tool
//	score    = max(0, 1 - weighted / saturation)
//
// `saturation` is the weighted-severity value at which the score bottoms out at 0.
// It's a magic number; bump it after looking at real-repo distributions.
//
// Overall score: the MIN across per-tool scores (weakest-link aggregation).
// Mean was misleading — a repo with one terrible tool and one perfect tool
// reads 50%, identical to a uniformly-mediocre repo. For an agent-reliability
// score, the agent is as reliable as its weakest surface, so min is honest.
const saturation = 3.0

// Score returns per-tool readiness and the overall score.
func Score(tools []models.ToolDef, findings []models.Finding) ([]models.ToolReadiness, float64) {
	byTool := map[string]*models.ToolReadiness{}
	for _, t := range tools {
		byTool[t.Name] = &models.ToolReadiness{ToolName: t.Name, Score: 1.0}
	}
	for _, f := range findings {
		r, ok := byTool[f.ToolName]
		if !ok {
			// Findings against tools we didn't list — shouldn't happen, but be safe.
			r = &models.ToolReadiness{ToolName: f.ToolName, Score: 1.0}
			byTool[f.ToolName] = r
		}
		r.FindingCount++
		r.WeightedSeverity += models.SeverityWeight(f.Severity) * f.Confidence
	}

	readiness := make([]models.ToolReadiness, 0, len(byTool))
	for _, r := range byTool {
		s := 1.0 - r.WeightedSeverity/saturation
		if s < 0 {
			s = 0
		}
		r.Score = s
		readiness = append(readiness, *r)
	}
	sort.Slice(readiness, func(i, j int) bool {
		if readiness[i].Score != readiness[j].Score {
			return readiness[i].Score < readiness[j].Score // worst first
		}
		return readiness[i].ToolName < readiness[j].ToolName
	})

	if len(readiness) == 0 {
		return readiness, 1.0
	}
	min := readiness[0].Score
	for _, r := range readiness[1:] {
		if r.Score < min {
			min = r.Score
		}
	}
	return readiness, min
}
