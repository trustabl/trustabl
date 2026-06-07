package scanner

import (
	"fmt"
	"strings"

	"github.com/trustabl/trustabl/internal/logx"
	"github.com/trustabl/trustabl/internal/models"
)

// maxDebugEntities caps the per-entity and per-finding debug dumps so a
// pathologically large repo cannot flood stderr; the remainder is summarized as
// "... and N more". The cap is generous — debug output is opt-in.
const maxDebugEntities = 50

// sdkListLabel renders a slice of SDKs as a stable comma list, or "none".
func sdkListLabel(sdks []models.SDK) string {
	if len(sdks) == 0 {
		return "none"
	}
	parts := make([]string, len(sdks))
	for i, s := range sdks {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}

// categoryListLabel renders a slice of detector categories as a comma list.
func categoryListLabel(cats []models.DetectorCategory) string {
	parts := make([]string, len(cats))
	for i, c := range cats {
		parts[i] = string(c)
	}
	return strings.Join(parts, ", ")
}

// severitySummary renders a deterministic severity histogram for a finding set,
// e.g. "2 high, 1 medium, 3 info" — highest severity first, zero tiers omitted.
// "none" for an empty set.
func severitySummary(findings []models.Finding) string {
	counts := map[models.Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	order := []models.Severity{
		models.SeverityCritical, models.SeverityHigh, models.SeverityMedium,
		models.SeverityLow, models.SeverityInfo,
	}
	var parts []string
	for _, s := range order {
		if n := counts[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, s))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// unauditedSDKs returns the detected SDKs that ship no policy pack — the ones
// META-001 flags as unaudited. Order follows the input (already sorted by Run).
func unauditedSDKs(sdks []models.SDK) []models.SDK {
	var out []models.SDK
	for _, s := range sdks {
		if !shippedPolicySDKs[s] {
			out = append(out, s)
		}
	}
	return out
}

// logDiscoveredEntities dumps each discovered tool and agent at debug level,
// capped. Caller guards with log.Enabled(LevelDebug) to skip the work entirely
// on the normal/verbose paths.
func logDiscoveredEntities(log *logx.Logger, tools []models.ToolDef, agents []models.AgentDef) {
	for i, t := range tools {
		if i == maxDebugEntities {
			log.Debugf("inventory: ... and %d more tools", len(tools)-maxDebugEntities)
			break
		}
		log.Debugf("inventory: tool %q [%s] %s:%d", t.Name, t.Kind, t.FilePath, t.Line)
	}
	for i, a := range agents {
		if i == maxDebugEntities {
			log.Debugf("inventory: ... and %d more agents", len(agents)-maxDebugEntities)
			break
		}
		log.Debugf("inventory: agent %q [%s] %s:%d", a.Name, a.SDK, a.FilePath, a.Line)
	}
}

// logFindingsDetail dumps each finding at debug level, capped. The findings are
// already deterministically sorted by the detector registry. Caller guards with
// log.Enabled(LevelDebug).
func logFindingsDetail(log *logx.Logger, findings []models.Finding) {
	for i, f := range findings {
		if i == maxDebugEntities {
			log.Debugf("analysis: ... and %d more findings", len(findings)-maxDebugEntities)
			break
		}
		loc := f.FilePath
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.FilePath, f.Line)
		}
		log.Debugf("analysis: %s [%s] %s — %s", f.RuleID, f.Severity, loc, f.Title)
	}
}
