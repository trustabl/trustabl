package review_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/review"
)

// TestRender_RulesWatermark is the ENG-5 "golden reports show the banner"
// contract: a scan from a pre-release channel or an unsigned custom source
// carries a visible watermark in the human report, while signed production and
// the unsigned default render clean.
func TestRender_RulesWatermark(t *testing.T) {
	render := func(o models.RulesOrigin) string {
		return (&review.Renderer{NoColor: true}).Render(models.ScanResult{
			Repo:        "example/repo",
			RulesOrigin: o,
		})
	}

	t.Run("staging channel is watermarked", func(t *testing.T) {
		out := render(models.RulesOrigin{Signed: true, Channel: "staging"})
		if !strings.Contains(out, "staging") || !strings.Contains(out, "⚠") {
			t.Errorf("staging report missing watermark:\n%s", out)
		}
	})

	t.Run("unsigned custom is watermarked", func(t *testing.T) {
		out := render(models.RulesOrigin{Custom: true})
		if !strings.Contains(out, "UNSIGNED") {
			t.Errorf("unsigned-custom report missing watermark:\n%s", out)
		}
	})

	t.Run("signed production renders clean", func(t *testing.T) {
		out := render(models.RulesOrigin{Signed: true, Channel: "production"})
		if strings.Contains(out, "⚠") || strings.Contains(out, "UNSIGNED") {
			t.Errorf("production report should be clean:\n%s", out)
		}
	})

	t.Run("unsigned default renders clean", func(t *testing.T) {
		out := render(models.RulesOrigin{})
		if strings.Contains(out, "⚠") || strings.Contains(out, "UNSIGNED") {
			t.Errorf("default report should be clean:\n%s", out)
		}
	})
}

// TestRender_WatermarkByteStable guards the determinism contract: the same
// result renders identically across calls (the watermark is a pure function of
// the origin).
func TestRender_WatermarkByteStable(t *testing.T) {
	result := models.ScanResult{Repo: "example/repo", RulesOrigin: models.RulesOrigin{Custom: true}}
	a := (&review.Renderer{NoColor: true}).Render(result)
	b := (&review.Renderer{NoColor: true}).Render(result)
	if a != b {
		t.Error("render not byte-stable for a fixed result")
	}
}
