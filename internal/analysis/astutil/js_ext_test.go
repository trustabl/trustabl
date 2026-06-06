package astutil_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

// TestParserKindForExtension_JavaScript locks the JS-family routing: every
// JavaScript extension dispatches to the tsx grammar (a JS superset that also
// tolerates JSX inside a .js file), while the TypeScript and non-JS/TS cases are
// unchanged.
func TestParserKindForExtension_JavaScript(t *testing.T) {
	cases := []struct{ path, want string }{
		{"src/app.js", "tsx"},
		{"src/app.jsx", "tsx"},
		{"src/app.mjs", "tsx"},
		{"src/app.cjs", "tsx"},
		// TypeScript unchanged.
		{"src/app.ts", "typescript"},
		{"src/app.mts", "typescript"},
		{"src/app.cts", "typescript"},
		{"src/app.tsx", "tsx"},
		// Non-JS/TS unchanged.
		{"src/app.py", ""},
		{"src/app.go", ""},
	}
	for _, c := range cases {
		if got := astutil.ParserKindForExtension(c.path); got != c.want {
			t.Errorf("ParserKindForExtension(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestIsJavaScriptExtension covers the JS extension set the scanner uses to
// re-tag JS-sourced defs (it must match exactly the extensions
// ParserKindForExtension routes to the JS family, and exclude TypeScript).
func TestIsJavaScriptExtension(t *testing.T) {
	js := []string{"a.js", "a.jsx", "a.mjs", "a.cjs", "dir/nested/b.js"}
	for _, p := range js {
		if !astutil.IsJavaScriptExtension(p) {
			t.Errorf("IsJavaScriptExtension(%q) = false, want true", p)
		}
	}
	notJS := []string{"a.ts", "a.tsx", "a.mts", "a.cts", "a.py", "a.json", "a"}
	for _, p := range notJS {
		if astutil.IsJavaScriptExtension(p) {
			t.Errorf("IsJavaScriptExtension(%q) = true, want false", p)
		}
	}
}
