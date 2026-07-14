package langsmith

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// newServer stands up a fake LangSmith API. sessions maps project name → id;
// runs is the response body for /runs/query. Counters record call volumes.
func newServer(t *testing.T, sessions map[string]string, runs []map[string]any, sessionCalls, queryCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			http.Error(w, "missing api key", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions":
			sessionCalls.Add(1)
			name := r.URL.Query().Get("name")
			var out []map[string]string
			if id, ok := sessions[name]; ok {
				out = append(out, map[string]string{"id": id})
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/runs/query":
			queryCalls.Add(1)
			var req struct {
				Session []string `json:"session"`
				RunType string   `json:"run_type"`
				Filter  string   `json:"filter"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.RunType != "tool" {
				http.Error(w, "expected run_type tool", http.StatusBadRequest)
				return
			}
			// Filter runs by the eq(name, "...") the client sent.
			var matched []map[string]any
			for _, run := range runs {
				if strings.Contains(req.Filter, `"`+run["name"].(string)+`"`) {
					matched = append(matched, run)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"runs": matched})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestToolStats_ComputesAggregates(t *testing.T) {
	var sc, qc atomic.Int32
	srv := newServer(t, map[string]string{"prod": "sess-1"}, []map[string]any{
		{"name": "fetch_data", "status": "success", "error": "", "start_time": "2026-07-01T00:00:00Z", "end_time": "2026-07-01T00:00:01Z"},
		{"name": "fetch_data", "status": "error", "error": "Timeout after 30s", "start_time": "2026-07-01T00:00:00Z", "end_time": "2026-07-01T00:00:03Z"},
		{"name": "fetch_data", "status": "error", "error": "Timeout after 30s", "start_time": "bad", "end_time": "also-bad"},
	}, &sc, &qc)
	defer srv.Close()

	c := New("test-key", "prod", srv.URL)
	stats, err := c.ToolStats(context.Background(), "fetch_data")
	if err != nil {
		t.Fatalf("ToolStats: %v", err)
	}
	if stats == nil {
		t.Fatal("stats = nil, want non-nil")
	}
	if stats.Runs != 3 || stats.Errors != 2 {
		t.Errorf("Runs/Errors = %d/%d, want 3/2", stats.Runs, stats.Errors)
	}
	// Duplicate error message deduped to one entry.
	if len(stats.RecentErrors) != 1 || stats.RecentErrors[0] != "Timeout after 30s" {
		t.Errorf("RecentErrors = %v, want [Timeout after 30s]", stats.RecentErrors)
	}
	// Latency mean over the two parseable pairs: (1s + 3s) / 2 = 2000ms.
	// The malformed third pair is skipped, not zero-counted.
	if stats.AvgLatencyMS != 2000 {
		t.Errorf("AvgLatencyMS = %d, want 2000", stats.AvgLatencyMS)
	}
	if stats.Project != "prod" || stats.ToolName != "fetch_data" {
		t.Errorf("Project/ToolName = %q/%q, want prod/fetch_data", stats.Project, stats.ToolName)
	}
}

func TestToolStats_NoTracesIsNilNil(t *testing.T) {
	var sc, qc atomic.Int32
	srv := newServer(t, map[string]string{"prod": "sess-1"}, nil, &sc, &qc)
	defer srv.Close()

	c := New("test-key", "prod", srv.URL)
	stats, err := c.ToolStats(context.Background(), "never_ran")
	if err != nil {
		t.Fatalf("ToolStats: %v", err)
	}
	if stats != nil {
		t.Errorf("stats = %+v, want nil (no traces)", stats)
	}
}

func TestToolStats_CachesPerTool(t *testing.T) {
	var sc, qc atomic.Int32
	srv := newServer(t, map[string]string{"prod": "sess-1"}, []map[string]any{
		{"name": "fetch_data", "status": "success", "error": "", "start_time": "", "end_time": ""},
	}, &sc, &qc)
	defer srv.Close()

	c := New("test-key", "prod", srv.URL)
	for i := 0; i < 3; i++ {
		if _, err := c.ToolStats(context.Background(), "fetch_data"); err != nil {
			t.Fatalf("ToolStats #%d: %v", i, err)
		}
	}
	// The no-traces outcome is cached too.
	for i := 0; i < 3; i++ {
		if _, err := c.ToolStats(context.Background(), "never_ran"); err != nil {
			t.Fatalf("ToolStats never_ran #%d: %v", i, err)
		}
	}
	if got := sc.Load(); got != 1 {
		t.Errorf("session lookups = %d, want 1 (cached)", got)
	}
	if got := qc.Load(); got != 2 {
		t.Errorf("runs queries = %d, want 2 (one per distinct tool)", got)
	}
}

func TestToolStats_UnknownProjectErrors(t *testing.T) {
	var sc, qc atomic.Int32
	srv := newServer(t, map[string]string{"prod": "sess-1"}, nil, &sc, &qc)
	defer srv.Close()

	c := New("test-key", "no-such-project", srv.URL)
	_, err := c.ToolStats(context.Background(), "fetch_data")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want project-not-found", err)
	}
	// Resolution failure is cached: a second tool fails fast, no new lookup.
	_, err2 := c.ToolStats(context.Background(), "other_tool")
	if err2 == nil {
		t.Fatal("second ToolStats err = nil, want cached resolution error")
	}
	if got := sc.Load(); got != 1 {
		t.Errorf("session lookups = %d, want 1 (failure cached)", got)
	}
}

func TestToolStats_HTTPErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server on fire", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New("test-key", "prod", srv.URL)
	_, err := c.ToolStats(context.Background(), "fetch_data")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want HTTP 500", err)
	}
}

func TestRunLatency(t *testing.T) {
	cases := []struct {
		start, end string
		wantMS     int64
		wantOK     bool
	}{
		{"2026-07-01T00:00:00Z", "2026-07-01T00:00:02Z", 2000, true},
		{"2026-07-01T00:00:00.500000", "2026-07-01T00:00:01.500000", 1000, true}, // zoneless LangSmith form
		{"2026-07-01T00:00:05Z", "2026-07-01T00:00:00Z", 0, false},               // negative → skipped
		{"", "2026-07-01T00:00:00Z", 0, false},
		{"garbage", "2026-07-01T00:00:00Z", 0, false},
	}
	for _, tc := range cases {
		d, ok := runLatency(tc.start, tc.end)
		if ok != tc.wantOK || (ok && d.Milliseconds() != tc.wantMS) {
			t.Errorf("runLatency(%q, %q) = %v/%v, want %dms/%v", tc.start, tc.end, d, ok, tc.wantMS, tc.wantOK)
		}
	}
}
