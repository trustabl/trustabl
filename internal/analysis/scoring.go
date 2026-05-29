package analysis

import (
	"sort"

	"github.com/trustabl/trustabl/internal/models"
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

// toolKey identifies a tool by file AND name. Name alone collapses distinct
// tools that share a name across modules (real repos reuse names like
// "search") into one row — overwriting the first and piling both files'
// findings onto it. Tool-scoped findings carry the tool's FilePath, so they key
// the same way.
type toolKey struct{ filePath, name string }

// Score returns per-tool readiness and the overall score.
func Score(tools []models.ToolDef, findings []models.Finding) ([]models.ToolReadiness, float64) {
	byTool := map[toolKey]*models.ToolReadiness{}
	for _, t := range tools {
		byTool[toolKey{t.FilePath, t.Name}] = &models.ToolReadiness{ToolName: t.Name, FilePath: t.FilePath, Score: 1.0}
	}
	for _, f := range findings {
		// Only tool-scoped findings count toward per-tool readiness. Findings
		// without a matching (FilePath, ToolName), or with a ToolName that
		// doesn't match any discovered tool, are agent-scoped, repo-scoped, or
		// META — they have their own attribution in the findings list and don't
		// belong in per-tool buckets. Aggregating them under a blank-name "tool"
		// used to surface as a confusing empty row in the readiness table and
		// dragged the overall score for non-tool reasons.
		r, ok := byTool[toolKey{f.FilePath, f.ToolName}]
		if !ok {
			continue
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
		if readiness[i].ToolName != readiness[j].ToolName {
			return readiness[i].ToolName < readiness[j].ToolName
		}
		return readiness[i].FilePath < readiness[j].FilePath // stable across same-named tools
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
