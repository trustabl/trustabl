package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/trustabl/trustabl/internal/models"
)

// protocolVersion is the MCP protocol revision this server implements. Clients
// send their own version in initialize; we echo a version we support. This is
// the stable revision the stdio tool surface targets.
const protocolVersion = "2024-11-05"

// ScanRequest is the input schema for the `scan` tool. Path is required and
// names a local directory or repository to scan; RulesRef optionally pins the
// detection-rules branch or tag (mirrors the CLI's --rules-ref). VulnScan opts
// the call into OSV dependency-vulnerability matching (mirrors --vuln-scan): off
// by default, so a scan stays offline-capable and fast unless the client asks.
type ScanRequest struct {
	Path     string `json:"path"`
	RulesRef string `json:"rules_ref,omitempty"`
	VulnScan bool   `json:"vuln_scan,omitempty"`
}

// ScanFunc runs a scan for the `scan` tool and returns the deterministic
// ScanResult. It is the seam between the protocol layer and the scanner core:
// production wires rule resolution (rulesource.Resolve) plus scanner.Run; tests
// inject a local-fixture scan so no network is required. Keeping this a
// function value is what lets the server be unit-tested without cloning rules.
type ScanFunc func(ctx context.Context, req ScanRequest) (models.ScanResult, error)

// VersionInfo is the build metadata surfaced by the optional `version` tool and
// in the initialize handshake's serverInfo.
type VersionInfo struct {
	Version string
	Commit  string
	Date    string
}

// Server is a stdio MCP server exposing Trustabl's scan. It is a frontend over
// the scanner core: it holds no scanning state, only the injected ScanFunc and
// the build metadata to report.
type Server struct {
	scan ScanFunc
	info VersionInfo
}

// New builds a Server. scan must be non-nil; it is the only path by which the
// server runs a scan.
func New(scan ScanFunc, info VersionInfo) *Server {
	return &Server{scan: scan, info: info}
}

// Serve runs the request/response loop over r (stdin) and w (stdout) until the
// input stream closes (EOF) or the context is cancelled. It returns nil on a
// clean EOF shutdown. NOTHING in this path may write to w except framed
// JSON-RPC responses: w is the protocol stream. Diagnostics belong on a
// separate stderr writer owned by the caller.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	c := newConn(r, w)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		req, err := c.read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("mcp: read: %w", err)
		}
		if err := s.handle(ctx, c, req); err != nil {
			return err
		}
	}
}

// handle dispatches one parsed request. A write failure on the connection is
// fatal to the loop (the client is gone); a handler-level problem is reported
// as a JSON-RPC error response, not a loop error.
func (s *Server) handle(ctx context.Context, c *conn, req *rpcRequest) error {
	if req.Method == parseErrorSentinel {
		return c.writeError(nil, codeParseError, "parse error")
	}
	// Notifications (no id) get no response. The common ones in the MCP
	// lifecycle are notifications/initialized and notifications/cancelled; we
	// have no per-call state to tear down, so acknowledging by ignoring is
	// correct.
	if req.isNotification() {
		return nil
	}

	switch req.Method {
	case "initialize":
		return c.writeResult(req.ID, s.initializeResult())
	case "tools/list":
		return c.writeResult(req.ID, s.toolsListResult())
	case "tools/call":
		return s.handleToolCall(ctx, c, req)
	case "ping":
		// MCP ping: an empty result object is the spec-compliant reply.
		return c.writeResult(req.ID, struct{}{})
	default:
		return c.writeError(req.ID, codeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// initializeResult is the handshake reply: protocol version, the single
// capability we expose (tools), and serverInfo.
func (s *Server) initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "trustabl",
			"version": s.versionString(),
		},
	}
}

func (s *Server) versionString() string {
	if s.info.Version == "" {
		return "dev"
	}
	return s.info.Version
}

// toolDescriptor is one entry in the tools/list catalog.
type toolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// toolsListResult advertises the exposed tools. The set is deliberately minimal
// and honest: `scan` (the real capability) and `version` (build metadata).
func (s *Server) toolsListResult() map[string]any {
	return map[string]any{
		"tools": []toolDescriptor{
			{
				Name: "scan",
				Description: "Scan a local directory or repository for AI-agent " +
					"security misconfigurations with Trustabl. Returns the structured " +
					"scan result (findings, scores, and discovered inventory) as JSON.",
				InputSchema: json.RawMessage(scanInputSchema),
			},
			{
				Name:        "version",
				Description: "Report the Trustabl build version, commit, and build date.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			},
		},
	}
}

// scanInputSchema is the JSON Schema for the `scan` tool input. path is
// required; rules_ref and vuln_scan are optional.
const scanInputSchema = `{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Local directory or repository path to scan."
    },
    "rules_ref": {
      "type": "string",
      "description": "Optional detection-rules branch or tag to use (default: the rules repo's default branch)."
    },
    "vuln_scan": {
      "type": "boolean",
      "description": "Match declared dependencies against a pinned OSV snapshot and report known CVEs in 'vulnerabilities' and as findings (default false; fetches the OSV database on first use, then reuses the cache)."
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`

// callParams is the params object of a tools/call request.
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// handleToolCall routes a tools/call to the named tool. A failure inside a tool
// is reported as a tool result with isError=true (the MCP convention for tool
// execution errors), NOT as a JSON-RPC protocol error, so the client can show
// the message to the model. Only a malformed request envelope is a protocol
// error.
func (s *Server) handleToolCall(ctx context.Context, c *conn, req *rpcRequest) error {
	var p callParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return c.writeError(req.ID, codeInvalidParams, fmt.Sprintf("invalid tools/call params: %v", err))
	}
	switch p.Name {
	case "scan":
		return s.callScan(ctx, c, req.ID, p.Arguments)
	case "version":
		return c.writeResult(req.ID, textResult(s.versionText(), false))
	default:
		return c.writeError(req.ID, codeInvalidParams, fmt.Sprintf("unknown tool: %s", p.Name))
	}
}

// callScan executes the `scan` tool: decode arguments, run the injected scan,
// and return the ScanResult as a JSON text content block. A scan error is
// surfaced as an isError tool result rather than a protocol error.
func (s *Server) callScan(ctx context.Context, c *conn, id json.RawMessage, args json.RawMessage) error {
	var sr ScanRequest
	if len(args) > 0 {
		if err := json.Unmarshal(args, &sr); err != nil {
			return c.writeResult(id, textResult(fmt.Sprintf("invalid scan arguments: %v", err), true))
		}
	}
	if sr.Path == "" {
		return c.writeResult(id, textResult("scan: 'path' is required", true))
	}

	result, err := s.scan(ctx, sr)
	if err != nil {
		return c.writeResult(id, textResult(fmt.Sprintf("scan failed: %v", err), true))
	}

	// Reuse the same serialization shape as the CLI's --format json path:
	// indented ScanResult JSON. Determinism is preserved because ScanResult is
	// already sorted/deduped by the scanner; we add no clocks or ordering here.
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return c.writeResult(id, textResult(fmt.Sprintf("scan: serialize result: %v", err), true))
	}
	return c.writeResult(id, textResult(string(payload), false))
}

func (s *Server) versionText() string {
	v, commit, date := s.info.Version, s.info.Commit, s.info.Date
	if v == "" {
		v = "dev"
	}
	if commit == "" {
		commit = "none"
	}
	if date == "" {
		date = "unknown"
	}
	return fmt.Sprintf("Trustabl %s\ncommit: %s\nbuilt:  %s", v, commit, date)
}

// textResult builds an MCP tool-call result with a single text content block.
// isError marks a tool-execution failure (the model sees the text and can
// react) as distinct from a transport/protocol error.
func textResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": isError,
	}
}
