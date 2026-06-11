package analysis_test

import (
	"reflect"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// Stage 2 capture matrix — TypeScript.

func discoverOneTSTool(t *testing.T, body string) models.ToolDef {
	t.Helper()
	src := `
import { tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";

const t = tool(
  "subject",
  "Test tool",
  { q: z.string() },
  async ({ q }) => { ` + body + ` }
);
`
	pf := parseTSForTest(t, "src/agent.ts", src)
	tools := analysis.DiscoverTSTools([]analysis.ParsedFile{pf}, nil)
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	return tools[0]
}

func TestTSCapture_StaticHTTPSHost(t *testing.T) {
	td := discoverOneTSTool(t, `const r = await fetch("https://api.example.com/v1"); return r;`)
	if want := []string{"api.example.com:443"}; !reflect.DeepEqual(td.HTTPHosts, want) {
		t.Errorf("HTTPHosts = %v, want %v", td.HTTPHosts, want)
	}
	if td.Facts["dynamic_url"] == "true" {
		t.Error("static literal must not set dynamic_url")
	}
}

func TestTSCapture_HTTPMethods(t *testing.T) {
	// Verb-named axios calls carry the method; fetch reads a `method:` option,
	// defaulting to GET. Aggregate set, sorted + deduped.
	td := discoverOneTSTool(t, `
		await fetch("https://api.example.com/a", { method: "POST" });
		await axios.delete("https://api.example.com/b");
		const r = await fetch("https://api.example.com/c");
		return r;`)
	if want := []string{"DELETE", "GET", "POST"}; !reflect.DeepEqual(td.HTTPMethods, want) {
		t.Errorf("HTTPMethods = %v, want %v", td.HTTPMethods, want)
	}
}

func TestTSCapture_HTTPCalls(t *testing.T) {
	td := discoverOneTSTool(t, `
		await axios.get("https://api.example.com/status");
		await fetch("https://api.example.com/ingest", { method: "POST" });
		return null;`)
	want := []models.HTTPCall{
		{HostPort: "api.example.com:443", Method: "GET", Path: "/status"},
		{HostPort: "api.example.com:443", Method: "POST", Path: "/ingest"},
	}
	if !reflect.DeepEqual(td.HTTPCalls, want) {
		t.Errorf("HTTPCalls = %+v, want %+v", td.HTTPCalls, want)
	}
}

func TestTSCapture_HTTPWithExplicitPort(t *testing.T) {
	td := discoverOneTSTool(t, `return axios.get("http://localhost:3000/x");`)
	if want := []string{"localhost:3000"}; !reflect.DeepEqual(td.HTTPHosts, want) {
		t.Errorf("HTTPHosts = %v, want %v", td.HTTPHosts, want)
	}
}

func TestTSCapture_TemplateWithSubstitutionCapturesNothing(t *testing.T) {
	td := discoverOneTSTool(t, "const r = await fetch(`https://api.example.com/${q}`); return r;")
	if td.HTTPHosts != nil {
		t.Errorf("template substitution must capture nothing, got %v", td.HTTPHosts)
	}
	if td.Facts["dynamic_url"] != "true" {
		t.Error("dynamic_url must still be set for template substitution")
	}
}

func TestTSCapture_PlainTemplateLiteralCaptured(t *testing.T) {
	td := discoverOneTSTool(t, "const r = await fetch(`https://static.example.com/v2`); return r;")
	if want := []string{"static.example.com:443"}; !reflect.DeepEqual(td.HTTPHosts, want) {
		t.Errorf("substitution-free template is a literal; HTTPHosts = %v, want %v", td.HTTPHosts, want)
	}
}

func TestTSCapture_RelativeURLNotCaptured(t *testing.T) {
	td := discoverOneTSTool(t, `const r = await fetch("/api/v1"); return r;`)
	if td.HTTPHosts != nil {
		t.Errorf("relative URL has no host; got %v", td.HTTPHosts)
	}
}

func TestTSCapture_WriteLiteralPath(t *testing.T) {
	td := discoverOneTSTool(t, `fs.writeFileSync("/tmp/out.txt", q); return q;`)
	if want := []string{"/tmp/out.txt"}; !reflect.DeepEqual(td.FSWritePaths, want) {
		t.Errorf("FSWritePaths = %v, want %v", td.FSWritePaths, want)
	}
	if td.Facts["writes_fs"] != "true" {
		t.Error("writes_fs fact must still be set")
	}
}

func TestTSCapture_JoinedWritePathNotCaptured(t *testing.T) {
	td := discoverOneTSTool(t, `fs.writeFileSync(path.join("/tmp", q), q); return q;`)
	if td.FSWritePaths != nil {
		t.Errorf("joined path must not capture, got %v", td.FSWritePaths)
	}
	if td.Facts["writes_fs"] != "true" {
		t.Error("writes_fs fact must still be set for a joined path")
	}
}

func TestTSCapture_PRetrySetsRetryPresent(t *testing.T) {
	td := discoverOneTSTool(t, `return pRetry(() => fetch("https://api.example.com/"), { retries: 3 });`)
	if td.Facts["retry_present"] != "true" {
		t.Errorf("pRetry must set retry_present, facts = %v", td.Facts)
	}
}

func TestTSCapture_GotRetryOptionSetsRetryPresent(t *testing.T) {
	td := discoverOneTSTool(t, `return got("https://api.example.com/", { retry: { limit: 2 } });`)
	if td.Facts["retry_present"] != "true" {
		t.Errorf("got retry option must set retry_present, facts = %v", td.Facts)
	}
}

func TestTSCapture_MultipleSortedDeduped(t *testing.T) {
	td := discoverOneTSTool(t, `
		await fetch("https://zeta.example.com/a");
		await fetch("https://alpha.example.com/b");
		await fetch("https://zeta.example.com/c");
		return q;`)
	want := []string{"alpha.example.com:443", "zeta.example.com:443"}
	if !reflect.DeepEqual(td.HTTPHosts, want) {
		t.Errorf("HTTPHosts = %v, want sorted+deduped %v", td.HTTPHosts, want)
	}
}
