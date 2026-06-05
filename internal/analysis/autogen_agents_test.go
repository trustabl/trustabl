package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

// A UserProxyAgent(...) in a file importing the AG2 line (`autogen`) is emitted
// as one AgentDef with SDK=SDKAutoGen, Class="UserProxyAgent", and its nested
// code_execution_config={"use_docker": False} captured as a nested KwargTree so
// the agent-scope rules (AG2-001/002/006) can read code_execution_config.use_docker.
func TestAutoGenAgent_ConversableAndUserProxyCaptured(t *testing.T) {
	src := `from autogen import ConversableAgent, UserProxyAgent

assistant = ConversableAgent(
    name="assistant",
    llm_config={"model": "gpt-4"},
)

user_proxy = UserProxyAgent(
    name="user_proxy",
    human_input_mode="NEVER",
    code_execution_config={"use_docker": False},
)
`
	pf := parsePyFile(t, "ag.py", src)
	agents := analysis.DiscoverAutoGenAgents([]analysis.ParsedFile{pf})
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2", len(agents))
	}

	byClass := map[string]models.AgentDef{}
	for _, a := range agents {
		if a.SDK != models.SDKAutoGen {
			t.Errorf("SDK: got %q, want %q", a.SDK, models.SDKAutoGen)
		}
		if a.Language != models.LanguagePython {
			t.Errorf("Language: got %q, want python", a.Language)
		}
		byClass[a.Class] = a
	}

	conv, ok := byClass["ConversableAgent"]
	if !ok {
		t.Fatalf("ConversableAgent not discovered; got classes %v", byClass)
	}
	if conv.Name != "assistant" || conv.VarName != "assistant" {
		t.Errorf("ConversableAgent Name/VarName: got %q/%q, want assistant/assistant", conv.Name, conv.VarName)
	}

	up, ok := byClass["UserProxyAgent"]
	if !ok {
		t.Fatalf("UserProxyAgent not discovered; got classes %v", byClass)
	}
	if up.Name != "user_proxy" || up.VarName != "user_proxy" {
		t.Errorf("UserProxyAgent Name/VarName: got %q/%q, want user_proxy/user_proxy", up.Name, up.VarName)
	}
	// The nested dict literal must be captured so the dotted-path predicate works.
	cec := up.Kwargs.Children["code_execution_config"]
	if cec == nil {
		t.Fatalf("code_execution_config not captured: %+v", up.Kwargs)
	}
	ud := cec.Children["use_docker"]
	if ud == nil || ud.Value == nil {
		t.Fatalf("code_execution_config.use_docker not captured: %+v", cec)
	}
	if ud.Value.Kind != models.ExprLiteralBool || ud.Value.Text != "False" {
		t.Errorf("use_docker leaf: got kind=%q text=%q, want bool/False", ud.Value.Kind, ud.Value.Text)
	}
	if hm := up.Kwargs.Children["human_input_mode"]; hm == nil || hm.Value == nil || hm.Value.Text != `"NEVER"` {
		t.Errorf("human_input_mode not captured as NEVER: %+v", up.Kwargs)
	}
}

// GroupChat and GroupChatManager (the AG2/0.2 orchestration classes) are both
// discovered with their exact class names so agentKindMatches keys the
// autogen_group_chat_manager token on either, and CodeExecutorAgent (the v0.4
// class) is discovered from the autogen_agentchat import root.
func TestAutoGenAgent_GroupChatAndV04Classes(t *testing.T) {
	src := `from autogen import GroupChat, GroupChatManager
from autogen_agentchat.agents import CodeExecutorAgent

gc = GroupChat(agents=[a, b], messages=[])
mgr = GroupChatManager(groupchat=gc)
executor = CodeExecutorAgent(name="exec")
`
	pf := parsePyFile(t, "gc.py", src)
	agents := analysis.DiscoverAutoGenAgents([]analysis.ParsedFile{pf})
	got := map[string]bool{}
	for _, a := range agents {
		if a.SDK != models.SDKAutoGen {
			t.Errorf("SDK: got %q, want autogen", a.SDK)
		}
		got[a.Class] = true
	}
	for _, want := range []string{"GroupChat", "GroupChatManager", "CodeExecutorAgent"} {
		if !got[want] {
			t.Errorf("class %q not discovered; got %v", want, got)
		}
	}
}

// register_function(fn, ...) and the @x.register_for_llm / @x.register_for_execution
// stacked decorators both produce KindAutoGenTool ToolDefs, with description=
// overrides and docstring fallback honored. A function carrying both stacked
// decorators is emitted exactly once.
func TestAutoGenTool_RegisterFunctionAndDecorators(t *testing.T) {
	src := `from autogen import ConversableAgent, register_function

assistant = ConversableAgent(name="a")
user_proxy = ConversableAgent(name="u")

def get_weather(city: str) -> str:
    """Return the weather for a city."""
    return "sunny"

register_function(
    get_weather,
    caller=assistant,
    executor=user_proxy,
    name="weather",
    description="Look up the weather.",
)

@user_proxy.register_for_execution()
@assistant.register_for_llm(name="adder", description="Add two numbers.")
def add(a: int, b: int) -> int:
    return a + b

@assistant.register_for_llm()
def multiply(a: int, b: int) -> int:
    """Multiply two numbers."""
    return a * b
`
	pf := parsePyFile(t, "tools.py", src)
	tools := analysis.DiscoverAutoGenTools([]analysis.ParsedFile{pf})

	byName := map[string]models.ToolDef{}
	for _, tl := range tools {
		if tl.Kind != models.KindAutoGenTool {
			t.Errorf("tool %q Kind: got %q, want %q", tl.Name, tl.Kind, models.KindAutoGenTool)
		}
		if tl.Language != models.LanguagePython {
			t.Errorf("tool %q Language: got %q, want python", tl.Name, tl.Language)
		}
		if _, dup := byName[tl.Name]; dup {
			t.Errorf("tool %q emitted more than once (stacked decorators must not double-emit)", tl.Name)
		}
		byName[tl.Name] = tl
	}

	// register_function with name= override.
	weather, ok := byName["weather"]
	if !ok {
		t.Fatalf("register_function tool 'weather' not discovered; got %v", keysOf(byName))
	}
	if weather.Description != "Look up the weather." {
		t.Errorf("weather description: got %q, want the description= override", weather.Description)
	}

	// Stacked decorators, description= on register_for_llm wins; emitted once.
	adder, ok := byName["adder"]
	if !ok {
		t.Fatalf("decorator tool 'adder' not discovered; got %v", keysOf(byName))
	}
	if adder.Description != "Add two numbers." {
		t.Errorf("adder description: got %q, want the register_for_llm description=", adder.Description)
	}

	// register_for_llm() with no description= falls back to the docstring.
	mul, ok := byName["multiply"]
	if !ok {
		t.Fatalf("decorator tool 'multiply' not discovered; got %v", keysOf(byName))
	}
	if mul.Description != "Multiply two numbers." {
		t.Errorf("multiply description: got %q, want the docstring fallback", mul.Description)
	}
}

// The Location of a register_function tool points at the wrapped function body,
// so the body-scanning predicates (has_shell_call etc.) inspect the function and
// the shells_out structural fact is stamped.
func TestAutoGenTool_LocationPointsAtFunctionBody(t *testing.T) {
	src := `from autogen import register_function

def run_cmd(cmd: str) -> str:
    """Run a shell command."""
    import subprocess
    return subprocess.run(cmd, shell=True)

register_function(run_cmd, name="runner")
`
	pf := parsePyFile(t, "shell.py", src)
	tools := analysis.DiscoverAutoGenTools([]analysis.ParsedFile{pf})
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	tl := tools[0]
	if tl.Facts["shells_out"] != "true" {
		t.Errorf("shells_out fact not stamped (Location must point at the function body): %+v", tl.Facts)
	}
}

// Collision guard: ConversableAgent / register_function in a file that imports
// NEITHER AutoGen line must yield nothing. DiscoverAutoGenAgents and
// DiscoverAutoGenTools are both import-gated.
func TestAutoGen_GateExcludesUnimported(t *testing.T) {
	src := `def ConversableAgent(**kw):
    return kw

def register_function(fn, **kw):
    return fn

agent = ConversableAgent(name="x", code_execution_config={"use_docker": False})

def helper(q: str) -> str:
    """Help."""
    return q

register_function(helper, name="help")
`
	pf := parsePyFile(t, "plain.py", src)
	if agents := analysis.DiscoverAutoGenAgents([]analysis.ParsedFile{pf}); len(agents) != 0 {
		t.Errorf("unimported ConversableAgent must not be an AutoGen agent; got %+v", agents)
	}
	if tools := analysis.DiscoverAutoGenTools([]analysis.ParsedFile{pf}); len(tools) != 0 {
		t.Errorf("unimported register_function must not be an AutoGen tool; got %+v", tools)
	}
}

// Import-gate boundary: both upstream lines are recognized as SDKAutoGen, but
// the AG2 dot-boundary (`autogen` / `autogen.`) must NOT be satisfied by a file
// that imports ONLY the v0.4 root `autogen_agentchat` — that file is still
// recognized via the v0.4 arm, and the two arms are kept distinct so neither
// root aliases the other. Each file independently yields the agent.
func TestAutoGen_ImportGateBoundary(t *testing.T) {
	// AG2 line: bare `autogen`.
	ag2 := parsePyFile(t, "ag2.py", `from autogen import AssistantAgent
a = AssistantAgent(name="a")
`)
	// v0.4 line: underscore root only.
	v04 := parsePyFile(t, "v04.py", `from autogen_agentchat.agents import AssistantAgent
a = AssistantAgent(name="b")
`)

	ag2Agents := analysis.DiscoverAutoGenAgents([]analysis.ParsedFile{ag2})
	if len(ag2Agents) != 1 || ag2Agents[0].SDK != models.SDKAutoGen {
		t.Fatalf("AG2 file: got %d agents (%+v), want 1 SDKAutoGen", len(ag2Agents), ag2Agents)
	}
	v04Agents := analysis.DiscoverAutoGenAgents([]analysis.ParsedFile{v04})
	if len(v04Agents) != 1 || v04Agents[0].SDK != models.SDKAutoGen {
		t.Fatalf("v0.4 file: got %d agents (%+v), want 1 SDKAutoGen", len(v04Agents), v04Agents)
	}

	// A file importing a deceptively-similar package that merely shares the
	// `autogen` prefix text (no dot boundary) must NOT trip either gate.
	bogus := parsePyFile(t, "bogus.py", `from autogenx import AssistantAgent
a = AssistantAgent(name="c")
`)
	if got := analysis.DiscoverAutoGenAgents([]analysis.ParsedFile{bogus}); len(got) != 0 {
		t.Errorf("autogenx (prefix-only, no dot boundary) must not match the AutoGen gate; got %+v", got)
	}
}

func keysOf(m map[string]models.ToolDef) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
