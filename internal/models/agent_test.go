package models_test

import (
	"encoding/json"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestHostedToolDef_JSONShape(t *testing.T) {
	h := models.HostedToolDef{
		Class:    "WebSearchTool",
		SDK:      models.SDKOpenAIAgents,
		FilePath: "agents/search.py",
		Line:     16,
		Kwargs:   &models.KwargTree{Children: map[string]*models.KwargTree{}},
	}
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	for _, want := range []string{`"class":"WebSearchTool"`, `"sdk":"openai_agents"`, `"file_path":"agents/search.py"`, `"line":16`} {
		if !contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestAgentDef_HostedToolRefsField(t *testing.T) {
	a := models.AgentDef{
		HostedToolRefs: []models.HostedToolRef{{Class: "WebSearchTool"}},
	}
	if len(a.HostedToolRefs) != 1 || a.HostedToolRefs[0].Class != "WebSearchTool" {
		t.Errorf("HostedToolRefs not wired: %+v", a.HostedToolRefs)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
