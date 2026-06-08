package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/scanner"
)

// rulesFixture returns the in-engine test mirror of the rule packs, so the scan
// tool can resolve rules with no network — the same source the scanner package
// tests use.
func rulesFixture(t *testing.T) fs.FS {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "rules-fixture")
	return os.DirFS(root)
}

// fixtureScan builds a ScanFunc that runs the real scanner core against the
// local rules fixture. This is exactly what the production wiring does, minus
// the network rule resolution — the point of the ScanFunc seam.
func fixtureScan(t *testing.T) ScanFunc {
	fsys := rulesFixture(t)
	return func(ctx context.Context, req ScanRequest) (models.ScanResult, error) {
		return scanner.Run(scanner.Config{
			Target:  req.Path,
			RulesFS: fsys,
			Ctx:     ctx,
		})
	}
}

// writeRepo lays down a minimal agent repo that reliably produces a finding: an
// OpenAI Agents SDK tool with no docstring (OAI-001) and untyped params
// (OAI-002). Returns the repo root.
func writeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pyproject.toml", "[project]\nname = \"f\"\ndependencies = [\"openai-agents\"]\n")
	// No docstring, untyped param -> OAI-001 + OAI-002 fire.
	write("tools.py", `from agents import function_tool

@function_tool
def lookup(city):
    return city
`)
	return dir
}

// roundTrip drives the server over in-memory pipes: it feeds the given request
// lines on stdin and returns the decoded responses from stdout. Serve returns
// on EOF, so the whole exchange runs synchronously.
func roundTrip(t *testing.T, srv *Server, requests ...string) []rpcResponse {
	t.Helper()
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resps []rpcResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var r rpcResponse
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decode response line %q: %v", line, err)
		}
		resps = append(resps, r)
	}
	return resps
}

// TestScanTool_ReturnsFindings is the core test: a tools/call for `scan`
// against a known repo returns a well-formed result whose JSON text block is a
// ScanResult carrying the expected findings.
func TestScanTool_ReturnsFindings(t *testing.T) {
	dir := writeRepo(t)
	srv := New(fixtureScan(t), VersionInfo{Version: "test"})

	call := mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "scan",
			"arguments": map[string]any{"path": dir},
		},
	})

	resps := roundTrip(t, srv, call)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	resp := resps[0]
	if resp.Error != nil {
		t.Fatalf("scan returned protocol error: %+v", resp.Error)
	}

	// The result is a tool-call result: { content: [{type, text}], isError }.
	var tr struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &tr); err != nil {
		t.Fatalf("decode tool result: %v", err)
	}
	if tr.IsError {
		t.Fatalf("scan tool reported isError=true: %s", firstText(tr.Content))
	}
	if len(tr.Content) != 1 || tr.Content[0].Type != "text" {
		t.Fatalf("expected one text content block, got %+v", tr.Content)
	}

	// The text block must be a parseable ScanResult with the expected findings.
	var sr models.ScanResult
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &sr); err != nil {
		t.Fatalf("scan result is not valid ScanResult JSON: %v\npayload: %s", err, tr.Content[0].Text)
	}
	if sr.ScanID == "" {
		t.Errorf("ScanResult.ScanID empty")
	}
	fired := map[string]bool{}
	for _, f := range sr.Findings {
		fired[f.RuleID] = true
	}
	if !fired["OAI-001"] {
		t.Errorf("expected OAI-001 (no docstring) to fire; fired set: %v", fired)
	}
}

// TestScanTool_VulnScanArg proves the optional vuln_scan tool argument is parsed
// from the call and threaded into the ScanRequest — the seam the production
// handler reads to set Config.VulnScan. It also locks the input schema and the
// ScanRequest struct in sync (the schema must advertise vuln_scan).
func TestScanTool_VulnScanArg(t *testing.T) {
	var got ScanRequest
	srv := New(func(_ context.Context, req ScanRequest) (models.ScanResult, error) {
		got = req
		return models.ScanResult{ScanID: "x"}, nil
	}, VersionInfo{Version: "test"})

	call := mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "scan",
			"arguments": map[string]any{"path": "/repo", "vuln_scan": true},
		},
	})
	resps := roundTrip(t, srv, call)
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("unexpected response: %+v", resps)
	}
	if got.Path != "/repo" {
		t.Errorf("path = %q, want /repo", got.Path)
	}
	if !got.VulnScan {
		t.Errorf("vuln_scan arg was not threaded into ScanRequest: %+v", got)
	}

	// The schema must advertise vuln_scan, or a client never knows to send it.
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(scanInputSchema), &schema); err != nil {
		t.Fatalf("scanInputSchema invalid: %v", err)
	}
	if _, ok := schema.Properties["vuln_scan"]; !ok {
		t.Errorf("scanInputSchema does not advertise vuln_scan: %v", schema.Properties)
	}
}

// TestScanTool_MissingPath returns an isError tool result, not a protocol
// error: the model should see a usable message.
func TestScanTool_MissingPath(t *testing.T) {
	srv := New(fixtureScan(t), VersionInfo{Version: "test"})
	call := mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params":  map[string]any{"name": "scan", "arguments": map[string]any{}},
	})
	resps := roundTrip(t, srv, call)
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("want one non-protocol-error response, got %+v", resps)
	}
	var tr struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resps[0].Result, &tr); err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("missing path should produce isError=true")
	}
}

// TestInitializeAndToolsList covers the handshake and tool catalog: initialize
// returns the protocol version and serverInfo; tools/list advertises scan and
// version with input schemas.
func TestInitializeAndToolsList(t *testing.T) {
	srv := New(fixtureScan(t), VersionInfo{Version: "9.9.9"})

	initReq := mustJSON(t, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": protocolVersion},
	})
	listReq := mustJSON(t, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list",
	})

	resps := roundTrip(t, srv, initReq, listReq)
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}

	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(resps[0].Result, &init); err != nil {
		t.Fatalf("decode initialize: %v", err)
	}
	if init.ProtocolVersion == "" {
		t.Error("initialize: empty protocolVersion")
	}
	if init.ServerInfo.Name != "trustabl" {
		t.Errorf("serverInfo.name = %q, want trustabl", init.ServerInfo.Name)
	}
	if init.ServerInfo.Version != "9.9.9" {
		t.Errorf("serverInfo.version = %q, want 9.9.9", init.ServerInfo.Version)
	}
	if _, ok := init.Capabilities["tools"]; !ok {
		t.Error("initialize: capabilities missing 'tools'")
	}

	var list struct {
		Tools []toolDescriptor `json:"tools"`
	}
	if err := json.Unmarshal(resps[1].Result, &list); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	names := map[string]bool{}
	for _, td := range list.Tools {
		names[td.Name] = true
		if len(td.InputSchema) == 0 {
			t.Errorf("tool %q has empty inputSchema", td.Name)
		}
		// inputSchema must be valid JSON.
		var js any
		if err := json.Unmarshal(td.InputSchema, &js); err != nil {
			t.Errorf("tool %q inputSchema is not valid JSON: %v", td.Name, err)
		}
	}
	if !names["scan"] {
		t.Error("tools/list missing 'scan'")
	}
	if !names["version"] {
		t.Error("tools/list missing 'version'")
	}
}

// TestNotificationGetsNoResponse: a notification (no id) must produce no
// response line, per JSON-RPC 2.0.
func TestNotificationGetsNoResponse(t *testing.T) {
	srv := New(fixtureScan(t), VersionInfo{Version: "test"})
	note := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	resps := roundTrip(t, srv, note)
	if len(resps) != 0 {
		t.Fatalf("notification should get no response, got %d: %+v", len(resps), resps)
	}
}

// TestUnknownMethod returns a -32601 method-not-found protocol error.
func TestUnknownMethod(t *testing.T) {
	srv := New(fixtureScan(t), VersionInfo{Version: "test"})
	req := `{"jsonrpc":"2.0","id":5,"method":"does/not/exist"}`
	resps := roundTrip(t, srv, req)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	if resps[0].Error == nil || resps[0].Error.Code != codeMethodNotFound {
		t.Fatalf("want method-not-found error, got %+v", resps[0])
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func firstText(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	if len(content) == 0 {
		return ""
	}
	return content[0].Text
}
