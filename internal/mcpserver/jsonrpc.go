// Package mcpserver implements a minimal Model Context Protocol (MCP) server
// over stdio that exposes Trustabl's scanner to MCP clients (Claude Code,
// Cursor, Claude Desktop). It is a frontend over the same scanner core the CLI
// uses: it owns no scanning logic, it only translates MCP tool calls into
// scanner.Run invocations and serializes the resulting ScanResult back onto the
// protocol stream.
//
// The transport is JSON-RPC 2.0 over newline-delimited stdio. The protocol uses
// stdout for the JSON-RPC stream, so nothing else may write to stdout while the
// server runs: progress and diagnostics stay on stderr, exactly as the rest of
// the engine's determinism contract requires. The server is deliberately
// hand-rolled (no third-party MCP SDK) so the module's Go version floor and its
// dependency set stay unchanged for what is, at heart, a thin adapter.
package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// JSON-RPC 2.0 standard error codes (subset we emit).
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// rpcRequest is an incoming JSON-RPC 2.0 request or notification. A request
// carries an id; a notification omits it (and gets no response).
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether the message is a notification (no id), which
// per JSON-RPC 2.0 must never receive a response.
func (r *rpcRequest) isNotification() bool { return len(r.ID) == 0 }

// rpcResponse is an outgoing JSON-RPC 2.0 response. Exactly one of Result or
// Error is set.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// conn frames JSON-RPC messages over a reader/writer pair (stdin/stdout for the
// real server). Each message is a single line of JSON terminated by '\n'. A
// mutex guards the writer so concurrent responses never interleave on the wire;
// the current dispatch loop is single-threaded, but the lock keeps the
// invariant honest if that changes.
type conn struct {
	r  *bufio.Scanner
	w  io.Writer
	mu sync.Mutex
}

func newConn(r io.Reader, w io.Writer) *conn {
	sc := bufio.NewScanner(r)
	// Allow large tool payloads (a scan request path is tiny, but keep generous
	// headroom so a fat client message is never silently truncated).
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return &conn{r: sc, w: w}
}

// read returns the next request line, or io.EOF when the stream closes.
func (c *conn) read() (*rpcRequest, error) {
	for {
		if !c.r.Scan() {
			if err := c.r.Err(); err != nil {
				return nil, err
			}
			return nil, io.EOF
		}
		line := c.r.Bytes()
		if len(line) == 0 {
			// Blank keep-alive line: skip it and read the next. Iterative (not
			// recursive) so an unbounded run of blank lines from the client cannot
			// exhaust the goroutine stack.
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// A malformed line is a parse error with a null id, per JSON-RPC 2.0.
			return &rpcRequest{Method: parseErrorSentinel}, nil
		}
		return &req, nil
	}
}

// parseErrorSentinel is a private method marker the read path uses to flag a
// line that failed to parse, so the dispatch loop can emit a -32700 with a null
// id without conflating it with a real method name.
const parseErrorSentinel = "\x00parse-error"

// writeResult sends a success response carrying result for the given id.
func (c *conn) writeResult(id json.RawMessage, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return c.writeError(id, codeInternalError, fmt.Sprintf("marshal result: %v", err))
	}
	return c.write(rpcResponse{JSONRPC: "2.0", ID: idOrNull(id), Result: raw})
}

// writeError sends an error response for the given id.
func (c *conn) writeError(id json.RawMessage, code int, msg string) error {
	return c.write(rpcResponse{
		JSONRPC: "2.0",
		ID:      idOrNull(id),
		Error:   &rpcError{Code: code, Message: msg},
	})
}

// write marshals and emits one response as a single newline-terminated line.
func (c *conn) write(resp rpcResponse) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.w.Write(b)
	return err
}

// idOrNull returns the id verbatim, or a JSON null when absent, so every
// response carries an explicit id field as the spec requires.
func idOrNull(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}
