package models_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestValidScope_Subagent(t *testing.T) {
	if !models.ValidScope(models.ScopeSubagent) {
		t.Errorf("ScopeSubagent should be valid")
	}
	if models.ScopeSubagent != "subagent" {
		t.Errorf("ScopeSubagent: got %q, want \"subagent\"", models.ScopeSubagent)
	}
}

func TestScanResult_RulesProvenanceFieldsSerialize(t *testing.T) {
	r := models.ScanResult{
		RulesSource:    "https://example.com/rules",
		RulesVersion:   "abc123",
		RulesFromCache: true,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"rules_source":"https://example.com/rules"`,
		`"rules_version":"abc123"`,
		`"rules_from_cache":true`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("JSON missing %s\ngot: %s", want, b)
		}
	}
}

func TestToolDef_VarName_OmitEmpty(t *testing.T) {
	td := models.ToolDef{Name: "x", Kind: models.KindOpenAITool, Language: models.LanguagePython}
	b, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte(`"var_name"`)) {
		t.Errorf("var_name should be omitted when empty, got: %s", b)
	}
	td.VarName = "myTool"
	b, err = json.Marshal(td)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte(`"var_name":"myTool"`)) {
		t.Errorf("var_name should be present when set, got: %s", b)
	}
}

func TestMCPServerDef_VarName_OmitEmpty(t *testing.T) {
	m := models.MCPServerDef{Class: "MCPServerStdio", Transport: "stdio",
		SDK: models.SDKOpenAIAgents, Language: models.LanguageTypeScript}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte(`"var_name"`)) {
		t.Errorf("var_name should be omitted when empty, got: %s", b)
	}
	m.VarName = "fsServer"
	b, err = json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte(`"var_name":"fsServer"`)) {
		t.Errorf("var_name should be present when set, got: %s", b)
	}
}

func TestGuardrailDef_VarName_OmitEmpty(t *testing.T) {
	g := models.GuardrailDef{Name: "x", Kind: "input"}
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte(`"var_name"`)) {
		t.Errorf("var_name should be omitted when empty, got: %s", b)
	}
	g.VarName = "blockPII"
	b, err = json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte(`"var_name":"blockPII"`)) {
		t.Errorf("var_name should be present when set, got: %s", b)
	}
}

func TestScanManifest_SourceFileCount(t *testing.T) {
	// Empty manifest counts zero.
	if got := (models.ScanManifest{}).SourceFileCount(); got != 0 {
		t.Errorf("empty manifest: got %d, want 0", got)
	}

	// Every source-language bucket is summed; data files are excluded.
	m := models.ScanManifest{
		PythonFiles:     []string{"a.py"},
		TypeScriptFiles: []string{"a.ts", "b.ts"},
		JavaScriptFiles: []string{"a.js"},
		GoFiles:         []string{"a.go"},
		CSharpFiles:     []string{"a.cs"},
		PHPFiles:        []string{"a.php"},
		RustFiles:       []string{"a.rs"},
		// Data files must NOT be counted.
		YAMLFiles:     []string{"a.yaml", "b.yaml"},
		JSONFiles:     []string{"a.json"},
		MarkdownFiles: []string{"a.md"},
	}
	if got, want := m.SourceFileCount(), 8; got != want {
		t.Errorf("SourceFileCount: got %d, want %d", got, want)
	}

	// Regression: a non-Python repo must not report zero source files.
	tsOnly := models.ScanManifest{TypeScriptFiles: []string{"a.ts", "b.ts", "c.ts"}}
	if got, want := tsOnly.SourceFileCount(), 3; got != want {
		t.Errorf("TypeScript-only SourceFileCount: got %d, want %d", got, want)
	}
}
