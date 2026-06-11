package analysis_test

import (
	"reflect"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// Stage 2 capture matrix — Python. Static literals are captured as canonical
// host:port / verbatim paths; anything dynamic captures nothing.

func discoverOnePyTool(t *testing.T, src string) models.ToolDef {
	t.Helper()
	pf := parsePyFile(t, "main.py", src)
	tools := analysis.DiscoverToolsFromParsed([]analysis.ParsedFile{pf})
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	return tools[0]
}

func TestPythonCapture_StaticHTTPSHost(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
import requests

@function_tool
def fetch_data():
    return requests.get("https://api.example.com/v1/data")
`)
	if want := []string{"api.example.com:443"}; !reflect.DeepEqual(td.HTTPHosts, want) {
		t.Errorf("HTTPHosts = %v, want %v", td.HTTPHosts, want)
	}
}

func TestPythonCapture_HTTPMethods(t *testing.T) {
	// Verb-named calls carry the method in the callee; requests.request takes it
	// as the first positional arg. Aggregate set, sorted + deduped.
	td := discoverOnePyTool(t, `from agents import function_tool
import requests

@function_tool
def touch():
    requests.get("https://api.example.com/a")
    requests.post("https://api.example.com/b")
    return requests.request("DELETE", "https://api.example.com/c")
`)
	if want := []string{"DELETE", "GET", "POST"}; !reflect.DeepEqual(td.HTTPMethods, want) {
		t.Errorf("HTTPMethods = %v, want %v", td.HTTPMethods, want)
	}
}

func TestPythonCapture_HTTPCalls(t *testing.T) {
	// Structured (host:port, method, path) records; query string dropped.
	td := discoverOnePyTool(t, `from agents import function_tool
import requests

@function_tool
def touch():
    requests.get("https://api.example.com/status")
    requests.post("https://api.example.com/ingest?x=1")
`)
	want := []models.HTTPCall{
		{HostPort: "api.example.com:443", Method: "GET", Path: "/status"},
		{HostPort: "api.example.com:443", Method: "POST", Path: "/ingest"},
	}
	if !reflect.DeepEqual(td.HTTPCalls, want) {
		t.Errorf("HTTPCalls = %+v, want %+v", td.HTTPCalls, want)
	}
}

func TestPythonCapture_RequestPositionalURL(t *testing.T) {
	// requests.request("DELETE", url): the URL is the 2nd positional. Host, path,
	// method, and the HTTPCall must ALL be captured together — regression for the
	// audit finding that read the 1st positional (the verb) as the URL.
	td := discoverOnePyTool(t, `from agents import function_tool
import requests

@function_tool
def wipe():
    return requests.request("DELETE", "https://api.example.com/items/5")
`)
	if want := []string{"api.example.com:443"}; !reflect.DeepEqual(td.HTTPHosts, want) {
		t.Errorf("HTTPHosts = %v, want %v", td.HTTPHosts, want)
	}
	if want := []string{"DELETE"}; !reflect.DeepEqual(td.HTTPMethods, want) {
		t.Errorf("HTTPMethods = %v, want %v", td.HTTPMethods, want)
	}
	want := []models.HTTPCall{{HostPort: "api.example.com:443", Method: "DELETE", Path: "/items/5"}}
	if !reflect.DeepEqual(td.HTTPCalls, want) {
		t.Errorf("HTTPCalls = %+v, want %+v", td.HTTPCalls, want)
	}
}

func TestPythonCapture_NoHTTPNoMethods(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool

@function_tool
def add(a: int, b: int) -> int:
    return a + b
`)
	if td.HTTPMethods != nil {
		t.Errorf("non-HTTP tool must capture no methods, got %v", td.HTTPMethods)
	}
}

func TestPythonCapture_HTTPWithExplicitPort(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
import httpx

@function_tool
def fetch_local():
    return httpx.get("http://localhost:8080/health")
`)
	if want := []string{"localhost:8080"}; !reflect.DeepEqual(td.HTTPHosts, want) {
		t.Errorf("HTTPHosts = %v, want %v", td.HTTPHosts, want)
	}
}

func TestPythonCapture_FStringCapturesNothing(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
import requests

@function_tool
def fetch_user(user_id: str):
    return requests.get(f"https://api.example.com/users/{user_id}")
`)
	if td.HTTPHosts != nil {
		t.Errorf("f-string URL must capture nothing, got %v", td.HTTPHosts)
	}
}

func TestPythonCapture_MultipleCallsSortedDeduped(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
import requests

@function_tool
def multi():
    requests.get("https://zeta.example.com/a")
    requests.post("https://alpha.example.com/b")
    requests.get("https://zeta.example.com/c")
`)
	want := []string{"alpha.example.com:443", "zeta.example.com:443"}
	if !reflect.DeepEqual(td.HTTPHosts, want) {
		t.Errorf("HTTPHosts = %v, want sorted+deduped %v", td.HTTPHosts, want)
	}
}

func TestPythonCapture_RelativeURLNotCaptured(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
import requests

@function_tool
def rel():
    return requests.get("/api/v1/data")
`)
	if td.HTTPHosts != nil {
		t.Errorf("relative URL has no host; got %v", td.HTTPHosts)
	}
}

func TestPythonCapture_AliasedSessionClient(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
import requests

@function_tool
def via_session():
    s = requests.Session()
    return s.get("https://api.example.com/v2")
`)
	if want := []string{"api.example.com:443"}; !reflect.DeepEqual(td.HTTPHosts, want) {
		t.Errorf("HTTPHosts = %v, want %v (aliased client)", td.HTTPHosts, want)
	}
}

func TestPythonCapture_OpenWriteLiteralPath(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool

@function_tool
def save(data: str):
    with open("/tmp/out.txt", "w") as f:
        f.write(data)
`)
	if want := []string{"/tmp/out.txt"}; !reflect.DeepEqual(td.FSWritePaths, want) {
		t.Errorf("FSWritePaths = %v, want %v", td.FSWritePaths, want)
	}
}

func TestPythonCapture_OpenReadModeNotCaptured(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool

@function_tool
def load():
    with open("/tmp/in.txt") as f:
        return f.read()
`)
	if td.FSWritePaths != nil {
		t.Errorf("read-mode open must not capture, got %v", td.FSWritePaths)
	}
}

func TestPythonCapture_OpenJoinedPathNotCaptured(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
import os

@function_tool
def save(name: str, data: str):
    with open(os.path.join("/tmp", name), "w") as f:
        f.write(data)
`)
	if td.FSWritePaths != nil {
		t.Errorf("joined path must not capture, got %v", td.FSWritePaths)
	}
}

func TestPythonCapture_PathlibWriteText(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
from pathlib import Path

@function_tool
def save(data: str):
    Path("results.json").write_text(data)
`)
	if want := []string{"results.json"}; !reflect.DeepEqual(td.FSWritePaths, want) {
		t.Errorf("FSWritePaths = %v, want %v", td.FSWritePaths, want)
	}
}

func TestPythonCapture_ShutilMoveTarget(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
import shutil

@function_tool
def archive(src: str):
    shutil.move(src, "/archive/dest.bin")
`)
	if want := []string{"/archive/dest.bin"}; !reflect.DeepEqual(td.FSWritePaths, want) {
		t.Errorf("FSWritePaths = %v, want %v (move target only)", td.FSWritePaths, want)
	}
}

func TestPythonCapture_RetryDecorator(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
from tenacity import retry

@function_tool
@retry
def flaky():
    pass
`)
	if td.Facts["retry_present"] != "true" {
		t.Errorf("tenacity @retry must set retry_present, facts = %v", td.Facts)
	}
}

func TestPythonCapture_BackoffDecorator(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
import backoff

@function_tool
@backoff.on_exception(backoff.expo, Exception)
def flaky():
    pass
`)
	if td.Facts["retry_present"] != "true" {
		t.Errorf("@backoff.on_exception must set retry_present, facts = %v", td.Facts)
	}
}

func TestPythonCapture_NoRetrySignals(t *testing.T) {
	td := discoverOnePyTool(t, `from agents import function_tool
import requests

@function_tool
def plain():
    return requests.get("https://api.example.com/")
`)
	if _, ok := td.Facts["retry_present"]; ok {
		t.Errorf("no retry signal present; facts = %v", td.Facts)
	}
}
