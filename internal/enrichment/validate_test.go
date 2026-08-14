package enrichment

import (
	"context"
	"testing"
)

func TestValidateSyntax_ValidGo(t *testing.T) {
	src := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	if err := validateSyntax(context.Background(), "main.go", src); err != nil {
		t.Errorf("validateSyntax() = %v, want nil for valid Go", err)
	}
}

func TestValidateSyntax_InvalidGo(t *testing.T) {
	src := "package main\n\nfunc main() {\n\tprintln(\"hi\"\n" // missing closing paren and brace
	if err := validateSyntax(context.Background(), "main.go", src); err == nil {
		t.Error("validateSyntax() = nil, want error for invalid Go")
	}
}

func TestValidateSyntax_ValidPython(t *testing.T) {
	src := "def run():\n    agent = Agent(input_guardrails=[g])\n    return agent\n"
	if err := validateSyntax(context.Background(), "agent.py", src); err != nil {
		t.Errorf("validateSyntax() = %v, want nil for valid Python", err)
	}
}

func TestValidateSyntax_InvalidPython(t *testing.T) {
	src := "def run(:\n    agent = Agent(\n    return agent\n" // unmatched parens
	if err := validateSyntax(context.Background(), "agent.py", src); err == nil {
		t.Error("validateSyntax() = nil, want error for invalid Python")
	}
}

// TestValidateSyntax_InvalidPython_DuplicateKeywordArgument reproduces a real
// enrichment PR: the LLM duplicated an entire call's keyword arguments
// instead of adding one new one. tree-sitter's grammar has no rule against
// repeating a keyword argument — that's a CPython compile-time check, not a
// parse-tree shape — so HasError() misses it. python3's own compile() does
// not: it raises "SyntaxError: keyword argument repeated: name".
func TestValidateSyntax_InvalidPython_DuplicateKeywordArgument(t *testing.T) {
	src := "agent = Agent(\n" +
		"    name=\"x\",\n" +
		"    name=\"x\",\n" +
		")\n"
	if err := validateSyntax(context.Background(), "agent.py", src); err == nil {
		t.Error("validateSyntax() = nil, want error for Python with a repeated keyword argument")
	}
}

func TestValidateSyntax_UnsupportedExtension(t *testing.T) {
	// No grammar for .rb — must skip (nil), not fail closed.
	src := "def run\n  agent = Agent.new(\nend\n" // deliberately broken Ruby
	if err := validateSyntax(context.Background(), "agent.rb", src); err != nil {
		t.Errorf("validateSyntax() = %v, want nil (unsupported extension skips validation)", err)
	}
}
