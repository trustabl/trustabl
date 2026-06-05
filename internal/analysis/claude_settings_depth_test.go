package analysis

import (
	"strings"
	"testing"
)

// TestExtractPermissionLines_DeepNestingDoesNotOverflow guards the JSON-walk
// depth bound. A pathologically deep object under a non-permissions key would,
// before the bound, recurse skipValue→skipToCloser one frame per level until the
// goroutine stack overflows and crashes the scan. It must now return an error
// gracefully (the caller then emits rules without line numbers) instead.
func TestExtractPermissionLines_DeepNestingDoesNotOverflow(t *testing.T) {
	depth := maxJSONDepth + 500
	raw := []byte(`{"x":` + strings.Repeat(`{"a":`, depth) + `1` + strings.Repeat(`}`, depth) + `}`)
	if _, _, _, err := extractPermissionLines(raw); err == nil {
		t.Error("expected a depth-limit error on pathologically nested JSON, got nil")
	}
}
