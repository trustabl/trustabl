// Package review implements the Diff Renderer + Exporter of architecture §2.
//
// Note on scope: the doc envisions a web-based Approval UX with per-artifact
// accept/edit/reject. In a CLI the equivalent is `--apply --yes` (accept all)
// or interactive prompts (reject individually). Per-finding edit-in-place is
// out of scope for v0.1.
package review

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/trustabl/karenctl/internal/models"
)

// Renderer produces the human-readable scan summary printed to stdout.
type Renderer struct {
	NoColor bool
}

func (r *Renderer) styles() (high, med, low, ok_, dim, header lipgloss.Style) {
	renderer := lipgloss.DefaultRenderer()
	if r.NoColor {
		renderer = lipgloss.NewRenderer(io.Discard, termenv.WithColorCache(true))
		renderer.SetColorProfile(termenv.Ascii)
	}
	high = renderer.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	med = renderer.NewStyle().Foreground(lipgloss.Color("208"))
	low = renderer.NewStyle().Foreground(lipgloss.Color("220"))
	ok_ = renderer.NewStyle().Foreground(lipgloss.Color("82")).Bold(true)
	dim = renderer.NewStyle().Foreground(lipgloss.Color("245"))
	header = renderer.NewStyle().Bold(true).Underline(true)
	return
}

func (r *Renderer) Render(result models.ScanResult) string {
	styleHigh, styleMed, styleLow, styleOK, styleDim, styleHeader := r.styles()
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n", styleHeader.Render("Scan summary"))
	fmt.Fprintf(&b, "  Repo:           %s\n", result.Repo)
	fmt.Fprintf(&b, "  Tools found:    %d\n", len(result.Tools))
	fmt.Fprintf(&b, "  Findings:       %d\n", len(result.Findings))
	sevTag := func(s models.Severity) string {
		switch s {
		case models.SeverityCritical:
			return styleHigh.Render("CRIT")
		case models.SeverityHigh:
			return styleHigh.Render("HIGH")
		case models.SeverityMedium:
			return styleMed.Render(" MED")
		case models.SeverityLow:
			return styleLow.Render(" LOW")
		default:
			return styleDim.Render("INFO")
		}
	}
	scoreCell := func(score float64) string {
		pct := fmt.Sprintf("%3.0f%%", score*100)
		switch {
		case score >= 0.85:
			return styleOK.Render(pct)
		case score >= 0.6:
			return styleMed.Render(pct)
		default:
			return styleHigh.Render(pct)
		}
	}

	fmt.Fprintf(&b, "  Overall score:  %s\n\n", scoreCell(result.OverallScore))

	if len(result.Findings) == 0 {
		b.WriteString(styleOK.Render("No findings. Nothing to commit.") + "\n")
		return b.String()
	}

	// Per-tool readiness table.
	b.WriteString(styleHeader.Render("Per-tool readiness") + "\n")
	for _, rd := range result.Readiness {
		fmt.Fprintf(&b, "  %-32s %s  (%d findings)\n",
			rd.ToolName, scoreCell(rd.Score), rd.FindingCount)
	}
	b.WriteString("\n")

	// Findings list, grouped by tool.
	b.WriteString(styleHeader.Render("Findings") + "\n")
	byTool := map[string][]models.Finding{}
	for _, f := range result.Findings {
		byTool[f.ToolName] = append(byTool[f.ToolName], f)
	}
	for _, ready := range result.Readiness {
		fs := byTool[ready.ToolName]
		if len(fs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n  %s\n", styleHeader.Render(ready.ToolName))
		for _, f := range fs {
			fmt.Fprintf(&b, "    [%s] %s %s  (%s:%d)\n",
				f.RuleID, sevTag(f.Severity), f.Title,
				f.FilePath, f.Line)
			fmt.Fprintf(&b, "        %s\n", styleDim.Render(wrapAt(f.Explanation, 86)))
			fmt.Fprintf(&b, "        %s %s\n", styleDim.Render("fix:"), f.SuggestedFix)
		}
	}

	// Generated artifacts (so the user knows what would be written).
	if len(result.GeneratedArtifacts) > 0 {
		b.WriteString("\n" + styleHeader.Render("Generated artifacts") + "\n")
		for _, a := range result.GeneratedArtifacts {
			fmt.Fprintf(&b, "  %s  (%s)\n", a.RelativePath, a.Category)
			fmt.Fprintf(&b, "    %s\n", styleDim.Render(a.Rationale))
		}
	}
	return b.String()
}

// wrapAt is a deliberately dumb word-wrapper. The output is for humans on a
// terminal; a perfect wrap isn't worth a dependency.
func wrapAt(s string, n int) string {
	words := strings.Fields(s)
	var b strings.Builder
	col := 0
	for i, w := range words {
		if col > 0 && col+1+len(w) > n {
			b.WriteString("\n        ")
			col = 0
		} else if i > 0 {
			b.WriteString(" ")
			col++
		}
		b.WriteString(w)
		col += len(w)
	}
	return b.String()
}

// ApplyArtifacts writes generated artifacts into the repo root.
//
// Discipline:
//   - Refuse to overwrite existing files unless `overwrite` is true.
//   - Create parent directories as needed.
//   - Never delete files; this function only writes.
func ApplyArtifacts(repoRoot string, artifacts []models.GeneratedArtifact, overwrite bool) error {
	for _, a := range artifacts {
		dest := filepath.Join(repoRoot, a.RelativePath)
		if !overwrite {
			if _, err := os.Stat(dest); err == nil {
				return fmt.Errorf("refusing to overwrite %s (re-run with --overwrite)", a.RelativePath)
			}
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, []byte(a.Contents), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
	}
	return nil
}
