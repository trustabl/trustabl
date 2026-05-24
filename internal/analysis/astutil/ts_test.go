package astutil_test

import (
	"context"
	"strings"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

func TestNewTSParser_ParsesHelloWorld(t *testing.T) {
	p := astutil.NewTSParser()
	if p == nil {
		t.Fatal("NewTSParser returned nil")
	}
	tree, err := p.ParseCtx(context.Background(), nil, []byte(`const x: number = 1;`))
	if err != nil || tree.RootNode().HasError() {
		t.Fatalf("parse failed: err=%v hasError=%v", err, tree.RootNode().HasError())
	}
}

func TestNewTSXParser_ParsesJSX(t *testing.T) {
	p := astutil.NewTSXParser()
	if p == nil {
		t.Fatal("NewTSXParser returned nil")
	}
	tree, err := p.ParseCtx(context.Background(), nil, []byte(`const el = <div>{x}</div>;`))
	if err != nil || tree.RootNode().HasError() {
		t.Fatalf("parse failed: err=%v hasError=%v", err, tree.RootNode().HasError())
	}
}

func TestParserForExtension(t *testing.T) {
	cases := []struct {
		path string
		want string // "typescript" | "tsx" | ""
	}{
		{"src/agent.ts", "typescript"},
		{"src/agent.mts", "typescript"},
		{"src/agent.cts", "typescript"},
		{"src/agent.tsx", "tsx"},
		{"src/agent.py", ""},
	}
	for _, c := range cases {
		got := astutil.ParserKindForExtension(c.path)
		if !strings.EqualFold(got, c.want) {
			t.Errorf("ParserKindForExtension(%q): got %q want %q", c.path, got, c.want)
		}
	}
}

func TestTSImportAliases(t *testing.T) {
	src := []byte(`
import { tool, query as q } from "@anthropic-ai/claude-agent-sdk";
import { createSdkMcpServer as mcp } from "@anthropic-ai/claude-agent-sdk";
import * as sdk from "@anthropic-ai/claude-agent-sdk";
import defaultExport from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
`)
	p := astutil.NewTSParser()
	tree, _ := p.ParseCtx(context.Background(), nil, src)
	got := astutil.TSImportAliases(tree.RootNode(), src, "@anthropic-ai/claude-agent-sdk")
	want := map[string]string{
		"tool":          "tool",               // named, no rename
		"q":             "query",              // renamed
		"mcp":           "createSdkMcpServer", // renamed
		"sdk":           "*",                  // namespace import — sentinel "*"
		"defaultExport": "default",            // default import — sentinel "default"
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("alias[%q] = %q, want %q", k, got[k], v)
		}
	}
	if got["z"] != "" {
		t.Errorf("alias[z] should be empty (wrong module), got %q", got["z"])
	}
}
