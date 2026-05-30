package models_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestHostedToolDef_JSONShape(t *testing.T) {
	h := models.HostedToolDef{
		Class: "WebSearchTool",
		SDK:   models.SDKOpenAIAgents,
		Location: models.Location{
			FilePath: "agents/search.py",
			Line:     16,
		},
		Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{}},
	}
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	for _, want := range []string{`"class":"WebSearchTool"`, `"sdk":"openai_agents"`, `"file_path":"agents/search.py"`, `"start_line":16`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestAgentDef_HostedToolRefsField(t *testing.T) {
	a := models.AgentDef{
		Language:       models.LanguagePython,
		HostedToolRefs: []models.HostedToolRef{{Class: "WebSearchTool"}},
	}
	if len(a.HostedToolRefs) != 1 || a.HostedToolRefs[0].Class != "WebSearchTool" {
		t.Errorf("HostedToolRefs not wired: %+v", a.HostedToolRefs)
	}
}

func TestAgentDef_Language_RoundTripsThroughJSON(t *testing.T) {
	a := models.AgentDef{
		SDK:      models.SDKClaudeAgentSDK,
		Class:    "AgentDefinition",
		Language: models.LanguageTypeScript,
		Location: models.Location{
			FilePath: "src/agent.ts",
			Line:     10,
		},
		Name: "reviewer",
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"language":"typescript"`) {
		t.Errorf("JSON missing language field: %s", data)
	}
	var got models.AgentDef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Language != models.LanguageTypeScript {
		t.Errorf("language: got %q, want %q", got.Language, models.LanguageTypeScript)
	}
}

func TestMCPServerDef_Language_RoundTripsThroughJSON(t *testing.T) {
	m := models.MCPServerDef{
		Class:     "McpStdioServerConfig",
		Transport: "stdio",
		SDK:       models.SDKClaudeAgentSDK,
		Language:  models.LanguageTypeScript,
		Location: models.Location{
			FilePath: "src/server.ts",
			Line:     5,
		},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"language":"typescript"`) {
		t.Errorf("JSON missing language: %s", data)
	}
	var got models.MCPServerDef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Language != models.LanguageTypeScript {
		t.Errorf("language: got %q, want %q", got.Language, models.LanguageTypeScript)
	}
}
