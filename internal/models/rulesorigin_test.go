package models_test

import (
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestRulesOrigin_Tag(t *testing.T) {
	cases := []struct {
		name   string
		origin models.RulesOrigin
		want   string
	}{
		{"signed production", models.RulesOrigin{Signed: true, Channel: "production"}, "signed:production"},
		{"signed staging", models.RulesOrigin{Signed: true, Channel: "staging"}, "signed:staging"},
		{"signed no channel", models.RulesOrigin{Signed: true}, "signed:unknown"},
		{"signed production from custom repo", models.RulesOrigin{Signed: true, Channel: "production", Custom: true}, "signed:production:custom"},
		{"unsigned custom", models.RulesOrigin{Custom: true}, "unsigned:custom"},
		{"unsigned default", models.RulesOrigin{}, "unsigned:default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.origin.Tag(); got != tc.want {
				t.Errorf("Tag() = %q, want %q", got, tc.want)
			}
		})
	}
	// Every distinct origin must have a distinct tag (the ScanID-fold contract).
	tags := map[string]bool{}
	for _, tc := range cases {
		if tags[tc.want] {
			t.Errorf("tag %q is not unique across origins", tc.want)
		}
		tags[tc.want] = true
	}
}

func TestRulesOrigin_Watermark(t *testing.T) {
	// Clean origins carry no watermark.
	for _, clean := range []models.RulesOrigin{
		{Signed: true, Channel: "production"},
		{}, // unsigned default — pre-cutover normal source
	} {
		if wm := clean.Watermark(); wm != "" {
			t.Errorf("%+v should be clean, got watermark %q", clean, wm)
		}
	}

	// Deviating origins are watermarked.
	if wm := (models.RulesOrigin{Signed: true, Channel: "staging"}).Watermark(); !strings.Contains(wm, "staging") {
		t.Errorf("staging channel must be watermarked, got %q", wm)
	}
	if wm := (models.RulesOrigin{Custom: true}).Watermark(); !strings.Contains(wm, "UNSIGNED") {
		t.Errorf("unsigned custom must be watermarked, got %q", wm)
	}
	// A signed channel served from a custom (non-official) repo is signature-safe
	// but not the blessed source, so it must be watermarked even for production —
	// otherwise a fork replaying an old validly-signed statement reads as official.
	if wm := (models.RulesOrigin{Signed: true, Channel: "production", Custom: true}).Watermark(); !strings.Contains(wm, "custom") {
		t.Errorf("signed custom-repo scan must be watermarked, got %q", wm)
	}
}
