package enrichment

import (
	"strings"
	"testing"
)

func TestExtractScope_PythonDef(t *testing.T) {
	content := `def other():
    pass

def run_agent(query: str):
    runner = Runner(agent)
    result = runner.run(query)
    return result
`
	got := extractScope(content, 5, 5) // line 5: runner = Runner(agent)
	if !strings.Contains(got, "→ ") {
		t.Fatal("expected flagged line marker → in output")
	}
	if !strings.Contains(got, "def run_agent") {
		t.Error("expected block header def run_agent in scope")
	}
	if strings.Contains(got, "def other") {
		t.Error("should not include unrelated function def other")
	}
}

func TestExtractScope_AsyncDef(t *testing.T) {
	content := `async def run_agent(query: str):
    runner = Runner(agent, app_name="demo")
    result = await runner.run_async(user_id="user")
    return result
`
	got := extractScope(content, 2, 5) // line 2: runner = Runner(...)
	if !strings.Contains(got, "async def run_agent") {
		t.Error("expected async def run_agent in scope")
	}
	if !strings.Contains(got, "→ ") {
		t.Fatal("expected flagged line marker")
	}
}

func TestExtractScope_PythonClass(t *testing.T) {
	content := `class MyAgent:
    def __init__(self):
        self.model = "claude"
    def run(self):
        pass
`
	got := extractScope(content, 3, 5) // line 3: self.model = "claude"
	if !strings.Contains(got, "class MyAgent") {
		t.Error("expected class header in scope")
	}
}

func TestExtractScope_TypeScriptFunction(t *testing.T) {
	content := `function runAgent(query: string) {
  const runner = new Runner();
  return runner.run(query);
}
`
	got := extractScope(content, 2, 5) // line 2: const runner = ...
	if !strings.Contains(got, "function runAgent") {
		t.Error("expected function header")
	}
	if !strings.Contains(got, "→ ") {
		t.Fatal("expected flagged line marker")
	}
}

func TestExtractScope_ArrowFunction(t *testing.T) {
	content := `const runAgent = async (query: string) => {
  const runner = new Runner();
  return runner.run(query);
};
`
	got := extractScope(content, 2, 5) // line 2: const runner = ...
	if !strings.Contains(got, "const runAgent") {
		t.Error("expected arrow function header")
	}
}

func TestExtractScope_Fallback(t *testing.T) {
	content := `line1
line2
line3
line4
line5
line6
line7
line8
line9
line10
`
	got := extractScope(content, 5, 3) // no block header → fallback ±3 lines
	if !strings.Contains(got, "→ ") {
		t.Fatal("expected flagged line marker in fallback")
	}
	if strings.Contains(got, "line1") {
		t.Error("fallback should not include line1 (too far)")
	}
	if !strings.Contains(got, "line5") {
		t.Error("fallback must include the flagged line5")
	}
}

func TestExtractScope_OutOfBounds(t *testing.T) {
	content := "line1\nline2\n"
	// line 0 and line 99 should not panic
	extractScope(content, 0, 5)
	extractScope(content, 99, 5)
}

func TestScopeFallback_ClampStart(t *testing.T) {
	content := "line1\nline2\nline3\n"
	got := scopeFallback(strings.Split(content, "\n"), 1, 5) // line 1, context 5 → start clamps to 0
	if !strings.Contains(got, "line1") {
		t.Error("should include line1 when clamped to start")
	}
}
