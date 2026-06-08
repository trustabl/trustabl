package analysis_test

import (
	"context"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis"
	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

func parseCSharpForTest(t *testing.T, src string) analysis.ParsedFile {
	t.Helper()
	tree, err := astutil.NewCSharpParser().ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return analysis.ParsedFile{RelPath: "Tools.cs", Tree: tree, Source: []byte(src)}
}

func TestDiscoverCSharpMCPTools(t *testing.T) {
	src := `
using System.ComponentModel;
using ModelContextProtocol.Server;

[McpServerToolType]
public class WeatherTools
{
    [McpServerTool, Description("Gets the current weather for a city.")]
    public string GetWeather([Description("City name")] string city)
    {
        return "sunny";
    }

    [McpServerTool]
    public static string Process(string input)
    {
        return input;
    }
}
`
	tools := analysis.DiscoverCSharpMCPTools([]analysis.ParsedFile{parseCSharpForTest(t, src)}, nil)
	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d: %+v", len(tools), tools)
	}
	byName := map[string]models.ToolDef{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	gw, ok := byName["GetWeather"]
	if !ok {
		t.Fatalf("GetWeather not discovered: %+v", tools)
	}
	if gw.Description != "Gets the current weather for a city." {
		t.Errorf("description = %q", gw.Description)
	}
	if gw.Language != models.LanguageCSharp {
		t.Errorf("language = %q, want csharp", gw.Language)
	}
	if gw.Kind != models.KindMCPTool {
		t.Errorf("kind = %q, want mcp_tool", gw.Kind)
	}
	if !gw.HasTypedParams {
		t.Error("want HasTypedParams=true")
	}
	if len(gw.ParamNames) != 1 || gw.ParamNames[0] != "city" {
		t.Errorf("ParamNames = %v, want [city]", gw.ParamNames)
	}
	proc, ok := byName["Process"]
	if !ok {
		t.Fatal("Process not discovered")
	}
	if proc.Description != "" {
		t.Errorf("Process description = %q, want empty (would fire MCP-017)", proc.Description)
	}
}

func TestDiscoverCSharpMCPTools_GateExcludesNonMCP(t *testing.T) {
	// No ModelContextProtocol using → not gated in, even with an [McpServerTool]
	// attribute (avoids matching a coincidentally-named attribute).
	src := `
using System;
using System.ComponentModel;

public class Tools
{
    [McpServerTool, Description("x")]
    public string Echo(string m) { return m; }
}
`
	tools := analysis.DiscoverCSharpMCPTools([]analysis.ParsedFile{parseCSharpForTest(t, src)}, nil)
	if len(tools) != 0 {
		t.Errorf("non-MCP file must gate out; got %d: %+v", len(tools), tools)
	}
}
