package analysis_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/models"
)

func TestHostedTools_WebSearchTool(t *testing.T) {
	src := `
from agents import Agent, WebSearchTool

agent = Agent(name="search", tools=[WebSearchTool()])
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{
		Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf}),
	}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})

	if len(inv.HostedTools) != 1 {
		t.Fatalf("expected 1 hosted tool, got %d", len(inv.HostedTools))
	}
	h := inv.HostedTools[0]
	if h.Class != "WebSearchTool" {
		t.Errorf("Class = %v, want WebSearchTool", h.Class)
	}
	if h.SDK != models.SDKOpenAIAgents {
		t.Errorf("SDK = %v, want openai_agents", h.SDK)
	}

	if len(inv.Agents) != 1 || len(inv.Agents[0].HostedToolRefs) != 1 {
		t.Fatalf("expected 1 hosted tool ref on agent, got %+v", inv.Agents)
	}
	ref := inv.Agents[0].HostedToolRefs[0]
	if ref.Resolved == nil || ref.Resolved.Class != "WebSearchTool" {
		t.Errorf("ref not resolved: %+v", ref)
	}
}

func TestHostedTools_AllKnownClasses(t *testing.T) {
	classes := []string{
		"WebSearchTool", "FileSearchTool", "ComputerTool", "HostedMCPTool",
		"CodeInterpreterTool", "ImageGenerationTool", "LocalShellTool",
		"ShellTool", "ApplyPatchTool", "CustomTool", "ToolSearchTool",
	}
	for _, c := range classes {
		t.Run(c, func(t *testing.T) {
			src := "from agents import Agent\nagent = Agent(name=\"x\", tools=[" + c + "()])"
			pf := parsePyFile(t, "main.py", src)
			inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
			analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})
			if len(inv.HostedTools) != 1 || inv.HostedTools[0].Class != c {
				t.Errorf("class %s: expected exactly one HostedTool with that class, got %+v", c, inv.HostedTools)
			}
		})
	}
}

func TestHostedTools_UnknownClassIgnored(t *testing.T) {
	src := `
from agents import Agent

agent = Agent(name="x", tools=[NotAHostedTool()])
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})
	if len(inv.HostedTools) != 0 {
		t.Errorf("expected zero hosted tools, got %+v", inv.HostedTools)
	}
}

func TestHostedTools_DeterministicOrder(t *testing.T) {
	src := `
from agents import Agent, WebSearchTool, FileSearchTool

a = Agent(name="a", tools=[WebSearchTool(), FileSearchTool(vector_store_ids=["v"])])
b = Agent(name="b", tools=[FileSearchTool(vector_store_ids=["v"]), WebSearchTool()])
`
	pf := parsePyFile(t, "main.py", src)

	inv1 := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv1, []analysis.ParsedFile{pf})

	inv2 := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv2, []analysis.ParsedFile{pf})

	if len(inv1.HostedTools) != 4 {
		t.Fatalf("expected 4 hosted tools, got %d", len(inv1.HostedTools))
	}

	// Sorted by (FilePath, Line, Class).
	for i := 1; i < len(inv1.HostedTools); i++ {
		prev, curr := inv1.HostedTools[i-1], inv1.HostedTools[i]
		if prev.FilePath > curr.FilePath ||
			(prev.FilePath == curr.FilePath && prev.Line > curr.Line) ||
			(prev.FilePath == curr.FilePath && prev.Line == curr.Line && prev.Class > curr.Class) {
			t.Errorf("HostedTools not sorted at index %d: %+v then %+v", i, prev, curr)
		}
	}

	// Stable across two independent runs over the same input.
	if len(inv1.HostedTools) != len(inv2.HostedTools) {
		t.Fatalf("non-deterministic length: %d vs %d", len(inv1.HostedTools), len(inv2.HostedTools))
	}
	for i := range inv1.HostedTools {
		if inv1.HostedTools[i] != inv2.HostedTools[i] {
			t.Errorf("non-deterministic at index %d: %+v vs %+v",
				i, inv1.HostedTools[i], inv2.HostedTools[i])
		}
	}
}

func TestHostedTools_DuplicateClassResolvesDistinctEntries(t *testing.T) {
	src := `
from agents import Agent, WebSearchTool

agent = Agent(name="x", tools=[WebSearchTool(), WebSearchTool()])
`
	pf := parsePyFile(t, "main.py", src)
	inv := &models.RepoInventory{Agents: analysis.DiscoverAgents([]analysis.ParsedFile{pf})}
	analysis.ResolveEdges(inv, []analysis.ParsedFile{pf})

	if len(inv.HostedTools) != 2 {
		t.Fatalf("expected 2 hosted tools, got %d", len(inv.HostedTools))
	}
	if len(inv.Agents) != 1 || len(inv.Agents[0].HostedToolRefs) != 2 {
		t.Fatalf("expected 2 hosted refs on agent, got %+v", inv.Agents)
	}
	r0, r1 := inv.Agents[0].HostedToolRefs[0], inv.Agents[0].HostedToolRefs[1]
	if r0.Resolved == nil || r1.Resolved == nil {
		t.Fatalf("refs not resolved: r0=%+v r1=%+v", r0, r1)
	}
	if r0.Resolved == r1.Resolved {
		t.Errorf("duplicate class refs should resolve to distinct HostedToolDef entries, both point at %p", r0.Resolved)
	}
}

func TestHostedTools_AllKnownClasses_CrossReferencesMap(t *testing.T) {
	expected := []string{
		"WebSearchTool", "FileSearchTool", "ComputerTool", "HostedMCPTool",
		"CodeInterpreterTool", "ImageGenerationTool", "LocalShellTool",
		"ShellTool", "ApplyPatchTool", "CustomTool", "ToolSearchTool",
	}
	if len(analysis.HostedToolClasses) != len(expected) {
		t.Fatalf("HostedToolClasses has %d entries, test expected %d (slice/map drift)",
			len(analysis.HostedToolClasses), len(expected))
	}
	for _, name := range expected {
		if !analysis.HostedToolClasses[name] {
			t.Errorf("class %q missing from HostedToolClasses", name)
		}
	}
}
