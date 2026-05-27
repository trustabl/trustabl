package analysis

import (
	"reflect"
	"testing"
)

func TestLineForOffset(t *testing.T) {
	raw := []byte("abc\ndef\nghi")
	nls := computeNewlineOffsets(raw)
	cases := []struct {
		off  int64
		want int
	}{
		{0, 1}, {2, 1}, {3, 1}, {4, 2}, {7, 2}, {8, 3}, {10, 3},
	}
	for _, c := range cases {
		got := lineForOffset(c.off, nls)
		if got != c.want {
			t.Errorf("offset %d: got line %d, want %d", c.off, got, c.want)
		}
	}
}

func TestExtractPermissionLines(t *testing.T) {
	const body = `{
  "permissions": {
    "allow": [
      "Bash",
      "Read"
    ],
    "deny": ["Write"]
  }
}
`
	// Layout:
	//   1: {
	//   2:   "permissions": {
	//   3:     "allow": [
	//   4:       "Bash",
	//   5:       "Read"
	//   6:     ],
	//   7:     "deny": ["Write"]
	//   8:   }
	//   9: }
	allowLines, denyLines, askLines, err := extractPermissionLines([]byte(body))
	if err != nil {
		t.Fatalf("extractPermissionLines: %v", err)
	}
	if !reflect.DeepEqual(allowLines, []int{4, 5}) {
		t.Errorf("allow = %v, want [4 5]", allowLines)
	}
	if !reflect.DeepEqual(denyLines, []int{7}) {
		t.Errorf("deny = %v, want [7]", denyLines)
	}
	if len(askLines) != 0 {
		t.Errorf("ask = %v, want empty", askLines)
	}
}

func TestExtractPermissionLines_SameLineRules(t *testing.T) {
	const body = `{
  "permissions": {
    "allow": ["Bash", "Read", "Grep"]
  }
}
`
	// All three rules on line 3.
	allow, _, _, err := extractPermissionLines([]byte(body))
	if err != nil {
		t.Fatalf("extractPermissionLines: %v", err)
	}
	if !reflect.DeepEqual(allow, []int{3, 3, 3}) {
		t.Errorf("allow = %v, want [3 3 3]", allow)
	}
}

func TestExtractPermissionLines_EmptyAndMissing(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty allow", `{"permissions": {"allow": []}}`},
		{"no permissions block", `{"defaultMode": "ask"}`},
		{"empty object", `{}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			allow, deny, ask, err := extractPermissionLines([]byte(c.body))
			if err != nil {
				t.Fatalf("extractPermissionLines: %v", err)
			}
			if len(allow) != 0 || len(deny) != 0 || len(ask) != 0 {
				t.Errorf("expected all empty, got allow=%v deny=%v ask=%v", allow, deny, ask)
			}
		})
	}
}

func TestExtractPermissionLines_OtherFieldsSkipped(t *testing.T) {
	// Verify env/hooks blocks (which appear in real settings.json files)
	// don't confuse the walker. The walker should skip them and return
	// only the allow rules.
	const body = `{
  "env": {"FOO": "bar"},
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "./lint.sh"}]}]},
  "permissions": {
    "allow": [
      "Bash"
    ]
  }
}
`
	// Layout: permissions block opens line 4, "allow" line 5, "Bash" line 6.
	allow, _, _, err := extractPermissionLines([]byte(body))
	if err != nil {
		t.Fatalf("extractPermissionLines: %v", err)
	}
	if !reflect.DeepEqual(allow, []int{6}) {
		t.Errorf("allow = %v, want [6]", allow)
	}
}
