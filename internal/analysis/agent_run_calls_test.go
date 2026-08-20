package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestDiscoverAgentRunCalls_OpenAI_CapturesMaxTurns(t *testing.T) {
	src := `from agents import Agent, Runner

agent = Agent(name="x")

async def main():
    result = await Runner.run(agent, "hi", max_turns=5)
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 1 {
		t.Fatalf("expected 1 run call, got %d: %+v", len(calls), calls)
	}
	rc := calls[0]
	if rc.SDK != models.SDKOpenAIAgents {
		t.Errorf("SDK = %q, want %q", rc.SDK, models.SDKOpenAIAgents)
	}
	if rc.AgentVarName != "agent" {
		t.Errorf("AgentVarName = %q, want %q", rc.AgentVarName, "agent")
	}
	if rc.Kwargs == nil || rc.Kwargs.Children["max_turns"] == nil {
		t.Fatalf("max_turns kwarg not captured: %+v", rc)
	}
	if rc.FilePath != "main.py" || rc.Line == 0 {
		t.Errorf("unexpected location: %+v", rc.Location)
	}
}

func TestDiscoverAgentRunCalls_OpenAI_SilentWhenMaxTurnsAbsent(t *testing.T) {
	src := `from agents import Agent, Runner

agent = Agent(name="x")

async def main():
    result = await Runner.run(agent, "hi")
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 1 {
		t.Fatalf("expected 1 run call, got %d", len(calls))
	}
	if calls[0].Kwargs != nil && calls[0].Kwargs.Children["max_turns"] != nil {
		t.Errorf("expected no max_turns kwarg, got %+v", calls[0].Kwargs)
	}
}

func TestDiscoverAgentRunCalls_OpenAI_RunSyncAndRunStreamed(t *testing.T) {
	src := `from agents import Agent, Runner

agent = Agent(name="x")
result = Runner.run_sync(agent, "hi", max_turns=3)
streamed = Runner.run_streamed(agent, "hi")
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 2 {
		t.Fatalf("expected 2 run calls, got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_OpenAI_UnrelatedRunnerClassNotMatched(t *testing.T) {
	// TaskRunner.run(...) must not match: the object segment is "TaskRunner",
	// not "Runner" — a suffix check on the whole callee text would wrongly
	// match this (isRunnerObject guards against exactly this).
	src := `from agents import Agent

agent = Agent(name="x")

class TaskRunner:
    pass

TaskRunner.run(agent, "hi", max_turns=1)
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 0 {
		t.Fatalf("expected 0 run calls (TaskRunner is not Runner), got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_OpenAI_SilentWhenFirstArgNotIdentifier(t *testing.T) {
	src := `from agents import Agent, Runner

async def main():
    result = await Runner.run(get_agent(), "hi", max_turns=5)
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 0 {
		t.Fatalf("expected 0 run calls (non-identifier first arg), got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_PydanticAI_CapturesUsageLimits(t *testing.T) {
	src := `from pydantic_ai import Agent
from pydantic_ai.usage import UsageLimits

agent = Agent("openai:gpt-4o")

async def main():
    result = await agent.run("hi", usage_limits=UsageLimits(request_limit=5))
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 1 {
		t.Fatalf("expected 1 run call, got %d: %+v", len(calls), calls)
	}
	rc := calls[0]
	if rc.SDK != models.SDKPydanticAI {
		t.Errorf("SDK = %q, want %q", rc.SDK, models.SDKPydanticAI)
	}
	if rc.AgentVarName != "agent" {
		t.Errorf("AgentVarName = %q, want %q", rc.AgentVarName, "agent")
	}
	if rc.Kwargs == nil || rc.Kwargs.Children["usage_limits"] == nil {
		t.Fatalf("usage_limits kwarg not captured: %+v", rc)
	}
	ul := rc.Kwargs.Children["usage_limits"]
	if ul.Value == nil || ul.Value.Kind != models.ExprCall {
		t.Fatalf("usage_limits value not a call expr: %+v", ul.Value)
	}
}

func TestDiscoverAgentRunCalls_PydanticAI_SilentWhenUsageLimitsAbsent(t *testing.T) {
	src := `from pydantic_ai import Agent

agent = Agent("openai:gpt-4o")

async def main():
    result = await agent.run("hi")
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 1 {
		t.Fatalf("expected 1 run call, got %d", len(calls))
	}
	if calls[0].Kwargs != nil && calls[0].Kwargs.Children["usage_limits"] != nil {
		t.Errorf("expected no usage_limits kwarg, got %+v", calls[0].Kwargs)
	}
}

func TestDiscoverAgentRunCalls_PydanticAI_RunSyncAndRunStream(t *testing.T) {
	src := `from pydantic_ai import Agent

agent = Agent("openai:gpt-4o")
result = agent.run_sync("hi")
with agent.run_stream("hi") as stream:
    pass
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 2 {
		t.Fatalf("expected 2 run calls, got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_PydanticAI_SilentWhenReceiverNotIdentifier(t *testing.T) {
	src := `from pydantic_ai import Agent

class Service:
    def __init__(self):
        self.agent = Agent("openai:gpt-4o")

    async def go(self):
        return await self.agent.run("hi")
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 0 {
		t.Fatalf("expected 0 run calls (self.agent receiver is not a plain identifier), got %d: %+v", len(calls), calls)
	}
}

func TestDiscoverAgentRunCalls_SilentWhenNeitherSDKImported(t *testing.T) {
	src := `class Db:
    def run(self, query):
        return query

db = Db()
db.run("select 1")
`
	calls := analysis.DiscoverAgentRunCalls([]analysis.ParsedFile{parsePyFile(t, "main.py", src)})
	if len(calls) != 0 {
		t.Fatalf("expected 0 run calls (file imports neither SDK), got %d: %+v", len(calls), calls)
	}
}
