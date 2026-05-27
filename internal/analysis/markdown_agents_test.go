package analysis

import (
	"reflect"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestSplitToolGrants(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"comma scalar", "Read, Bash, Glob", []string{"Read", "Bash", "Glob"}},
		{"space separated", "Read Grep", []string{"Read", "Grep"}},
		{"paren keeps inner comma", "Agent(worker, researcher), Read", []string{"Agent(worker, researcher)", "Read"}},
		{"paren keeps inner space", "Bash(npm run test), Read", []string{"Bash(npm run test)", "Read"}},
		{"mcp ref verbatim", "Read, mcp__email__search_inbox", []string{"Read", "mcp__email__search_inbox"}},
		{"empty", "", nil},
		{"trailing comma", "Read, ", []string{"Read"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitToolGrants(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitToolGrants(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseToolGrants(t *testing.T) {
	got := parseToolGrants([]string{"Read", "Bash(npm run *)", "mcp__email__search_inbox", "Agent(worker, researcher)"})
	want := []models.ToolGrant{
		{Tool: "Read", Pattern: "", Raw: "Read"},
		{Tool: "Bash", Pattern: "npm run *", Raw: "Bash(npm run *)"},
		{Tool: "MCP", Pattern: "email__search_inbox", Raw: "mcp__email__search_inbox"},
		{Tool: "Agent", Pattern: "worker, researcher", Raw: "Agent(worker, researcher)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseToolGrants =\n %+v\nwant\n %+v", got, want)
	}
}

// TestParseToolGrants_FallbackKeepsRaw covers the path where a token does not
// match the permission grammar (ParsePermissionRule returns Tool==""): the raw
// token must be kept as the Tool name so nothing is silently dropped.
func TestParseToolGrants_FallbackKeepsRaw(t *testing.T) {
	got := parseToolGrants([]string{"lowercaseunknown"})
	want := []models.ToolGrant{{Tool: "lowercaseunknown", Pattern: "", Raw: "lowercaseunknown"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseToolGrants fallback =\n %+v\nwant\n %+v", got, want)
	}
	if parseToolGrants(nil) != nil {
		t.Errorf("parseToolGrants(nil) should be nil")
	}
}
