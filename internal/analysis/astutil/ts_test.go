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
