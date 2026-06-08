package astutil_test

import (
	"context"
	"testing"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

// TestTSImportAliases_CommonJSRequire verifies the CommonJS require() forms
// resolve to the same canonical alias values as the ES import forms, so the
// import gate and TSCalleeText work identically for require()-based JavaScript.
// Parsed with the tsx grammar (the grammar .js/.cjs route to).
func TestTSImportAliases_CommonJSRequire(t *testing.T) {
	src := []byte(`
const { tool } = require("@openai/agents");
const { Agent: MyAgent } = require("@openai/agents");
const oa = require("@openai/agents");
const t2 = require("@openai/agents").tool;
const { ignored } = require("some-other-pkg");
`)
	tree, err := astutil.NewTSXParser().ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := astutil.TSImportAliases(tree.RootNode(), src, "@openai/agents")
	want := map[string]string{
		"tool":    "tool",  // const { tool } = require(...)
		"MyAgent": "Agent", // const { Agent: MyAgent } = require(...)
		"oa":      "*",     // const oa = require(...)            (namespace)
		"t2":      "tool",  // const t2 = require(...).tool       (member)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("alias %q: got %q, want %q (full map: %v)", k, got[k], v, got)
		}
	}
	if _, ok := got["ignored"]; ok {
		t.Errorf("require() from an unmatched module must be ignored; got: %v", got)
	}
	if len(got) != len(want) {
		t.Errorf("got %d aliases, want %d: %v", len(got), len(want), got)
	}
}

// TestTSImportAliases_MixedImportAndRequire confirms ES imports and require()
// bindings coexist in one file and both land in the alias map.
func TestTSImportAliases_MixedImportAndRequire(t *testing.T) {
	src := []byte(`
import { query } from "@openai/agents";
const { tool } = require("@openai/agents");
`)
	tree, err := astutil.NewTSXParser().ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := astutil.TSImportAliases(tree.RootNode(), src, "@openai/agents")
	if got["query"] != "query" {
		t.Errorf("ES import alias missing: %v", got)
	}
	if got["tool"] != "tool" {
		t.Errorf("require alias missing: %v", got)
	}
}
