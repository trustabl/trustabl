package rules

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestValidAppliesToForScope_ADKTokens(t *testing.T) {
	cases := []struct {
		scope models.Scope
		kind  string
		want  bool
	}{
		{models.ScopeTool, "adk_function_tool", true},
		{models.ScopeAgent, "adk_llm_agent", true},
		{models.ScopeAgent, "adk_sequential_agent", true},
		{models.ScopeAgent, "adk_parallel_agent", true},
		{models.ScopeAgent, "adk_loop_agent", true},
		{models.ScopeAgent, "adk_langgraph_agent", true},
		{models.ScopeRepo, "google_adk", true},
		{models.ScopeTool, "google_adk", false},         // wrong scope
		{models.ScopeAgent, "adk_function_tool", false}, // wrong scope
		// claude_query_main is handled by agentKindMatches (TS QueryMainAgent);
		// the loader's scope validator must accept it too, else an SP2 rule
		// declaring applies_to: [claude_query_main] is rejected at load time.
		{models.ScopeAgent, "claude_query_main", true},
		{models.ScopeSubagent, "claude_subagent", true},
		{models.ScopeSubagent, "claude_agent_definition", false}, // wrong scope
	}
	for _, c := range cases {
		got := validAppliesToForScope(c.scope, c.kind)
		if got != c.want {
			t.Errorf("validAppliesToForScope(%q, %q) = %v, want %v",
				c.scope, c.kind, got, c.want)
		}
	}
}
