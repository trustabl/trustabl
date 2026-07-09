// Package langsmith is a minimal read-only client for the LangSmith REST API,
// used by `trustabl enrich --langsmith` to sample recent runtime executions of
// a flagged tool. It is strictly opt-in and post-scan: nothing in the scan
// pipeline imports this package, so the scan's no-network and determinism
// contracts are untouched.
//
// BYOK: the API key comes from the LANGSMITH_API_KEY environment variable and
// is sent only to the configured LangSmith endpoint, never to any Trustabl
// service. Only tool names are sent out; trace content only flows in.
package langsmith

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/trustabl/trustabl/internal/models"
)

// DefaultBaseURL is the public LangSmith API endpoint. Self-hosted deployments
// override it via LANGSMITH_ENDPOINT.
const DefaultBaseURL = "https://api.smith.langchain.com"

// runSampleLimit caps how many recent runs are sampled per tool. Small on
// purpose: the summary is grounding evidence for an LLM prompt, not analytics.
const runSampleLimit = 25

// maxRecentErrors caps the distinct error messages carried on ToolTraceStats.
const maxRecentErrors = 3

// maxErrorLen truncates each carried error message. Trace errors can embed
// full stack traces; the prompt needs the headline, not the dump.
const maxErrorLen = 160

// Client queries one LangSmith project for per-tool run statistics. Safe for
// concurrent use; results are cached per tool name for the Client's lifetime
// (one enrich invocation), so repeat findings on the same tool cost one fetch.
type Client struct {
	apiKey  string
	project string
	baseURL string
	http    *http.Client

	mu          sync.Mutex
	statsByTool map[string]toolStatsEntry
	sessionID   string
	sessionErr  error
	sessionDone bool
}

type toolStatsEntry struct {
	stats *models.ToolTraceStats // nil = no traces for this tool
	err   error
}

// New returns a Client for project using apiKey. baseURL "" means the public
// LangSmith endpoint.
func New(apiKey, project, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		apiKey:      apiKey,
		project:     project,
		baseURL:     strings.TrimRight(baseURL, "/"),
		http:        &http.Client{Timeout: 15 * time.Second},
		statsByTool: make(map[string]toolStatsEntry),
	}
}

// ToolStats samples the most recent runs of toolName in the client's project
// and returns aggregate statistics. Returns (nil, nil) when the project holds
// no runs for that tool: "no evidence" is an expected outcome, not an error.
// Results (including the no-traces outcome and errors) are cached per tool.
func (c *Client) ToolStats(ctx context.Context, toolName string) (*models.ToolTraceStats, error) {
	c.mu.Lock()
	if e, ok := c.statsByTool[toolName]; ok {
		c.mu.Unlock()
		return e.stats, e.err
	}
	c.mu.Unlock()

	stats, err := c.fetchToolStats(ctx, toolName)

	c.mu.Lock()
	c.statsByTool[toolName] = toolStatsEntry{stats: stats, err: err}
	c.mu.Unlock()
	return stats, err
}

func (c *Client) fetchToolStats(ctx context.Context, toolName string) (*models.ToolTraceStats, error) {
	sessionID, err := c.resolveSession(ctx)
	if err != nil {
		return nil, err
	}

	// LangSmith filter DSL string equality. Tool names come from discovery
	// (identifiers), but escape quotes and backslashes defensively anyway.
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(toolName)
	reqBody := map[string]any{
		"session":  []string{sessionID},
		"run_type": "tool",
		"filter":   fmt.Sprintf(`eq(name, "%s")`, esc),
		"limit":    runSampleLimit,
		"select":   []string{"name", "status", "error", "start_time", "end_time"},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("langsmith: marshal runs query: %w", err)
	}

	var resp struct {
		Runs []struct {
			Status    string `json:"status"`
			Error     string `json:"error"`
			StartTime string `json:"start_time"`
			EndTime   string `json:"end_time"`
		} `json:"runs"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/runs/query", bytes.NewReader(body), &resp); err != nil {
		return nil, err
	}
	if len(resp.Runs) == 0 {
		return nil, nil // no traces for this tool, expected, not an error
	}

	stats := &models.ToolTraceStats{
		ToolName: toolName,
		Project:  c.project,
		Runs:     len(resp.Runs),
	}
	var latencySum time.Duration
	var latencyN int64
	seenErr := make(map[string]bool)
	for _, r := range resp.Runs {
		if r.Status == "error" || r.Error != "" {
			stats.Errors++
			msg := strings.TrimSpace(r.Error)
			if msg != "" {
				if len(msg) > maxErrorLen {
					msg = msg[:maxErrorLen] + "…"
				}
				if !seenErr[msg] && len(stats.RecentErrors) < maxRecentErrors {
					seenErr[msg] = true
					stats.RecentErrors = append(stats.RecentErrors, msg)
				}
			}
		}
		if d, ok := runLatency(r.StartTime, r.EndTime); ok {
			latencySum += d
			latencyN++
		}
	}
	if latencyN > 0 {
		stats.AvgLatencyMS = (latencySum / time.Duration(latencyN)).Milliseconds()
	}
	return stats, nil
}

// resolveSession maps the configured project name to its LangSmith session ID
// (the runs/query endpoint takes session UUIDs, not project names). Resolved
// once per Client; the outcome, success or failure, is cached so a bad
// project name fails fast instead of re-erroring on every tool.
func (c *Client) resolveSession(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.sessionDone {
		id, err := c.sessionID, c.sessionErr
		c.mu.Unlock()
		return id, err
	}
	c.mu.Unlock()

	var sessions []struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/sessions?name="+url.QueryEscape(c.project), nil, &sessions)
	var id string
	if err == nil {
		if len(sessions) == 0 {
			err = fmt.Errorf("langsmith: project %q not found", c.project)
		} else {
			id = sessions[0].ID
		}
	}

	c.mu.Lock()
	// First resolution wins; a concurrent duplicate resolution of the same
	// project reaches the same answer, so overwriting is harmless either way.
	c.sessionID, c.sessionErr, c.sessionDone = id, err, true
	c.mu.Unlock()
	return id, err
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("langsmith: build request: %w", err)
	}
	req.Header.Set("x-api-key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("langsmith: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("langsmith: %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("langsmith: decode %s response: %w", path, err)
	}
	return nil
}

// runLatency computes end−start from LangSmith timestamps. The API emits
// RFC3339-ish timestamps with and without a zone suffix; unparseable or
// nonsensical (negative) pairs report ok=false and are skipped.
func runLatency(start, end string) (time.Duration, bool) {
	st, ok1 := parseTraceTime(start)
	et, ok2 := parseTraceTime(end)
	if !ok1 || !ok2 {
		return 0, false
	}
	d := et.Sub(st)
	if d < 0 {
		return 0, false
	}
	return d, true
}

func parseTraceTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
