package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestMCPServers_InlineStdio(t *testing.T) {
	src := `
from agents import Agent
from agents.mcp import MCPServerStdio

agent = Agent(
    name="fs",
    mcp_servers=[MCPServerStdio(params={"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem"]})],
)
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})

	if len(inv.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(inv.MCPServers))
	}
	m := inv.MCPServers[0]
	if m.Class != "MCPServerStdio" {
		t.Errorf("Class = %v, want MCPServerStdio", m.Class)
	}
	if m.Transport != "stdio" {
		t.Errorf("Transport = %v, want stdio", m.Transport)
	}

	if len(inv.Agents[0].MCPServerRefs) != 1 || inv.Agents[0].MCPServerRefs[0].Resolved == nil {
		t.Errorf("expected 1 resolved MCP server ref, got %+v", inv.Agents[0].MCPServerRefs)
	}
}

func TestMCPServers_TransportDerivation(t *testing.T) {
	cases := []struct {
		class, transport string
	}{
		{"MCPServerStdio", "stdio"},
		{"MCPServerSse", "sse"},
		{"MCPServerStreamableHttp", "streamable_http"},
	}
	for _, tc := range cases {
		t.Run(tc.class, func(t *testing.T) {
			if got := analysis.MCPTransportFromClass(tc.class); got != tc.transport {
				t.Errorf("MCPTransportFromClass(%q) = %q, want %q", tc.class, got, tc.transport)
			}
		})
	}
}

func TestMCPServers_UnknownClassNotEmitted(t *testing.T) {
	src := `
from agents import Agent
agent = Agent(name="x", mcp_servers=[SomethingElse()])
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})
	if len(inv.MCPServers) != 0 {
		t.Errorf("expected zero MCP servers, got %+v", inv.MCPServers)
	}
	if len(inv.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(inv.Agents))
	}
	if len(inv.Agents[0].MCPServerRefs) != 1 {
		t.Fatalf("expected 1 MCPServerRef (count-preserving fallthrough), got %d", len(inv.Agents[0].MCPServerRefs))
	}
	if !inv.Agents[0].MCPServerRefs[0].External {
		t.Errorf("expected External=true for unrecognized class ref so Task 4 alias resolution can find it")
	}
}

func TestMCPServers_AsyncWithAlias(t *testing.T) {
	src := `
from agents import Agent
from agents.mcp import MCPServerStdio

async def main():
    async with MCPServerStdio(params={"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "."]}) as fs:
        agent = Agent(name="a", mcp_servers=[fs])
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})

	if len(inv.MCPServers) != 1 || inv.MCPServers[0].Class != "MCPServerStdio" {
		t.Fatalf("expected one MCPServerStdio via alias, got %+v", inv.MCPServers)
	}
	if inv.MCPServers[0].Transport != "stdio" {
		t.Errorf("Transport = %v, want stdio", inv.MCPServers[0].Transport)
	}
	if len(inv.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(inv.Agents))
	}
	refs := inv.Agents[0].MCPServerRefs
	if len(refs) != 1 || refs[0].Resolved == nil || refs[0].External {
		t.Errorf("expected one resolved (non-external) ref, got %+v", refs)
	}
}

func TestMCPServers_SyncWithAlias(t *testing.T) {
	src := `
from agents import Agent
from agents.mcp import MCPServerSse

with MCPServerSse(params={"url": "https://example.com/sse"}) as srv:
    agent = Agent(name="a", mcp_servers=[srv])
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})

	if len(inv.MCPServers) != 1 || inv.MCPServers[0].Class != "MCPServerSse" {
		t.Fatalf("expected one MCPServerSse via sync-with alias, got %+v", inv.MCPServers)
	}
	refs := inv.Agents[0].MCPServerRefs
	if len(refs) != 1 || refs[0].Resolved == nil || refs[0].External {
		t.Errorf("expected one resolved ref, got %+v", refs)
	}
}

func TestMCPServers_UnknownAliasStaysExternal(t *testing.T) {
	src := `
from agents import Agent
agent = Agent(name="a", mcp_servers=[not_an_alias])
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})

	if len(inv.MCPServers) != 0 {
		t.Errorf("expected zero MCP servers, got %+v", inv.MCPServers)
	}
	refs := inv.Agents[0].MCPServerRefs
	if len(refs) != 1 || !refs[0].External {
		t.Errorf("expected one External ref for an unresolvable name, got %+v", refs)
	}
}

func TestMCPServers_SharedAliasDuplicatesDef_V1(t *testing.T) {
	// v1 simplification: one `async with ... as fs:` referenced by two agents
	// produces TWO MCPServerDef entries (one per agent). Lock this in so a
	// future change is a deliberate decision, not an accident.
	src := `
from agents import Agent
from agents.mcp import MCPServerStdio

async def main():
    async with MCPServerStdio(params={"command": "npx"}) as fs:
        a = Agent(name="a", mcp_servers=[fs])
        b = Agent(name="b", mcp_servers=[fs])
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})

	if len(inv.MCPServers) != 2 {
		t.Fatalf("v1 expects 2 MCPServerDef entries (one per agent) for a shared alias, got %d", len(inv.MCPServers))
	}
	for _, a := range inv.Agents {
		if len(a.MCPServerRefs) != 1 || a.MCPServerRefs[0].Resolved == nil || a.MCPServerRefs[0].External {
			t.Errorf("agent %q: expected one resolved ref, got %+v", a.Name, a.MCPServerRefs)
		}
	}
}

func TestMCPServers_MultiItemWith(t *testing.T) {
	src := `
from agents import Agent
from agents.mcp import MCPServerStdio, MCPServerSse

async def main():
    async with MCPServerStdio(params={"command": "npx"}) as fs, MCPServerSse(params={"url": "https://example.com/sse"}) as sse:
        agent = Agent(name="a", mcp_servers=[fs, sse])
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})

	if len(inv.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP servers from a multi-item with, got %d: %+v", len(inv.MCPServers), inv.MCPServers)
	}
	classes := map[string]bool{}
	for _, m := range inv.MCPServers {
		classes[m.Class] = true
	}
	if !classes["MCPServerStdio"] || !classes["MCPServerSse"] {
		t.Errorf("expected both MCPServerStdio and MCPServerSse, got %+v", inv.MCPServers)
	}
	if len(inv.Agents) != 1 || len(inv.Agents[0].MCPServerRefs) != 2 {
		t.Fatalf("expected 2 resolved refs on the agent, got %+v", inv.Agents)
	}
	for _, ref := range inv.Agents[0].MCPServerRefs {
		if ref.Resolved == nil || ref.External {
			t.Errorf("ref not resolved: %+v", ref)
		}
	}
}

func TestMCPServers_NonMCPWithIgnored(t *testing.T) {
	src := `
from agents import Agent
from agents.mcp import MCPServerStdio

async def main():
    with open("data.txt") as f:
        async with MCPServerStdio(params={"command": "npx"}) as fs:
            agent = Agent(name="a", mcp_servers=[fs])
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})

	if len(inv.MCPServers) != 1 || inv.MCPServers[0].Class != "MCPServerStdio" {
		t.Fatalf("expected exactly one MCPServerStdio (the open() with must be ignored), got %+v", inv.MCPServers)
	}
}
