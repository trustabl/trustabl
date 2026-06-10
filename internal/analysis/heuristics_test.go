package analysis

import (
	"testing"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

func firstFunctionNode(t *testing.T, src string) (*sitter.Node, []byte) {
	t.Helper()
	b := []byte(src)
	tree, err := astutil.Parse(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var fn *sitter.Node
	astutil.Walk(tree.RootNode(), func(n *sitter.Node) bool {
		if fn != nil {
			return false
		}
		if n.Type() == "function_definition" {
			fn = n
			return false
		}
		return true
	})
	if fn == nil {
		t.Fatal("no function_definition found")
	}
	return fn, b
}

func TestResolveClientAliases(t *testing.T) {
	src := `
def tool():
    s = requests.Session()
    c = httpx.Client()
    sess = aiohttp.ClientSession()
    x = compute()
    with httpx.AsyncClient() as ac:
        ac.get("u")
    s.get("u")
`
	fn, b := firstFunctionNode(t, src)
	got := ResolveClientAliases(fn, b)
	want := map[string]string{
		"s":    "requests",
		"c":    "httpx",
		"sess": "aiohttp.ClientSession", // canonical receiver prefix, so sess.get -> aiohttp.ClientSession.get
		"ac":   "httpx",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d aliases %v, want %d %v", len(got), got, len(want), want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("alias %q = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["x"]; ok {
		t.Errorf("non-client assignment x should not be an alias")
	}
}

func TestResolveClientAliases_LastWriteWins(t *testing.T) {
	src := `
def tool():
    s = requests.Session()
    s = compute()
    s.get("u")
`
	fn, b := firstFunctionNode(t, src)
	got := ResolveClientAliases(fn, b)
	if _, ok := got["s"]; ok {
		t.Errorf("s rebound to non-client should not remain an alias; got %v", got)
	}
}

func firstCallNamed(t *testing.T, src, calleeText string) (*sitter.Node, []byte) {
	t.Helper()
	b := []byte(src)
	tree, err := astutil.Parse(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var found *sitter.Node
	astutil.Walk(tree.RootNode(), func(n *sitter.Node) bool {
		if found != nil {
			return false
		}
		if n.Type() == "call" {
			fn := n.ChildByFieldName("function")
			if fn != nil && astutil.NodeText(fn, b) == calleeText {
				found = n
				return false
			}
		}
		return true
	})
	if found == nil {
		t.Fatalf("call %q not found", calleeText)
	}
	return found, b
}

func TestIsHTTPCallNode(t *testing.T) {
	// direct call
	c1, b1 := firstCallNamed(t, `requests.get("u")`, "requests.get")
	if got, ok := IsHTTPCallNode(c1, b1, nil); !ok || got != "requests.get" {
		t.Errorf("direct: got (%q,%v), want (requests.get,true)", got, ok)
	}
	// aliased call
	c2, b2 := firstCallNamed(t, `s.get("u")`, "s.get")
	if got, ok := IsHTTPCallNode(c2, b2, map[string]string{"s": "requests"}); !ok || got != "requests.get" {
		t.Errorf("aliased: got (%q,%v), want (requests.get,true)", got, ok)
	}
	// unknown alias
	c3, b3 := firstCallNamed(t, `q.get("u")`, "q.get")
	if _, ok := IsHTTPCallNode(c3, b3, map[string]string{"s": "requests"}); ok {
		t.Errorf("unknown alias: ok=true, want false")
	}
	// non-HTTP
	c4, b4 := firstCallNamed(t, `json.dumps(x)`, "json.dumps")
	if _, ok := IsHTTPCallNode(c4, b4, nil); ok {
		t.Errorf("non-HTTP: ok=true, want false")
	}
}

// TestIsHTTPCallNode_AiohttpAliasIsRuleCallee guards that an aliased aiohttp
// session call canonicalizes to the SAME string rule callee lists use
// (aiohttp.ClientSession.get), not aiohttp.get — which is in no rule's callee
// set, so the resolution would "succeed" yet never match a rule.
func TestIsHTTPCallNode_AiohttpAliasIsRuleCallee(t *testing.T) {
	aliasMod := clientConstructorModule("aiohttp.ClientSession")
	c, b := firstCallNamed(t, `sess.get("u")`, "sess.get")
	got, ok := IsHTTPCallNode(c, b, map[string]string{"sess": aliasMod})
	if !ok {
		t.Fatal("aliased aiohttp call not recognized as HTTP")
	}
	if got != "aiohttp.ClientSession.get" {
		t.Errorf("canonical = %q, want aiohttp.ClientSession.get", got)
	}
	if !IsHTTPCall(got) {
		t.Errorf("canonical %q is not in the rule-callee set — unmatchable by any rule", got)
	}
}

func TestShellModuleAliases_Canonical(t *testing.T) {
	src := `
import subprocess as sp
import os as o
from subprocess import run as r
from os import system
import json as j
`
	tree, err := astutil.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := CollectShellModuleAliases(tree.RootNode(), []byte(src))
	cases := []struct {
		callee      string
		canonical   string
		wantIsShell bool
	}{
		{"sp.run", "subprocess.run", true},         // module alias
		{"sp.Popen", "subprocess.Popen", true},     // module alias, any attr
		{"o.system", "os.system", true},            // os module alias
		{"o.spawnl", "os.spawnl", true},            // os.spawn* family
		{"r", "subprocess.run", true},              // from-import alias
		{"system", "os.system", true},              // from-import bare symbol
		{"subprocess.run", "subprocess.run", true}, // literal, unchanged
		{"j.dumps", "j.dumps", false},              // benign module alias, unchanged
		{"open", "open", false},                    // unrelated bare callee
	}
	for _, c := range cases {
		got := a.Canonical(c.callee)
		if got != c.canonical {
			t.Errorf("Canonical(%q) = %q, want %q", c.callee, got, c.canonical)
		}
		if IsShellCallee(got) != c.wantIsShell {
			t.Errorf("IsShellCallee(Canonical(%q)=%q) = %v, want %v", c.callee, got, IsShellCallee(got), c.wantIsShell)
		}
	}
}

func TestCodeExecAliases_Canonical(t *testing.T) {
	src := `
import builtins
import builtins as b
from builtins import eval as ev
from builtins import exec
`
	tree, err := astutil.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := CollectCodeExecAliases(tree.RootNode(), []byte(src))
	cases := []struct {
		callee    string
		canonical string
		wantExec  bool
	}{
		{"eval", "eval", true},              // bare builtin
		{"ev", "eval", true},                // from-import alias
		{"exec", "exec", true},              // from-import (no alias)
		{"builtins.eval", "eval", true},     // module-qualified
		{"b.exec", "exec", true},            // module alias
		{"re.compile", "re.compile", false}, // non-builtins attribute, unchanged
		{"compile", "compile", true},        // bare builtin (always)
		{"upper", "upper", false},           // unrelated bare name
	}
	for _, c := range cases {
		got := a.Canonical(c.callee)
		if got != c.canonical {
			t.Errorf("Canonical(%q) = %q, want %q", c.callee, got, c.canonical)
		}
		if IsCodeExecCallee(got) != c.wantExec {
			t.Errorf("IsCodeExecCallee(Canonical(%q)=%q) = %v, want %v", c.callee, got, IsCodeExecCallee(got), c.wantExec)
		}
	}
}

func TestCollectHTTPModuleAliases(t *testing.T) {
	src := `
import requests as rq
import httpx as hx
import json as j
import aiohttp as ah
`
	tree, err := astutil.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := CollectHTTPModuleAliases(tree.RootNode(), []byte(src))
	want := map[string]string{"rq": "requests", "hx": "httpx"} // json/aiohttp not module-alias-resolved
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("alias %q = %q, want %q", k, got[k], v)
		}
	}
	// The aliased call must canonicalize to a rule callee via IsHTTPCallNode.
	c, b := firstCallNamed(t, `rq.get("u")`, "rq.get")
	if canon, ok := IsHTTPCallNode(c, b, got); !ok || canon != "requests.get" {
		t.Errorf("rq.get via module alias: got (%q,%v), want (requests.get,true)", canon, ok)
	}
}
