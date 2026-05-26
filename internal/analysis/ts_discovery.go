package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

const tsClaudeSDKModule = "@anthropic-ai/claude-agent-sdk"

// DiscoverTSTools walks each parsed TS file and emits a ToolDef for every
// tool() factory call from @anthropic-ai/claude-agent-sdk. The optional
// onFile callback fires once per file visited (progress UI hook). Files
// that do not import from the SDK module are skipped (import gate).
func DiscoverTSTools(files []ParsedFile, onFile func(string)) []models.ToolDef {
	var out []models.ToolDef
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSToolsInFile(pf)...)
	}
	return out
}

func discoverTSToolsInFile(pf ParsedFile) []models.ToolDef {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliases(pf.Tree.RootNode(), pf.Source, tsClaudeSDKModule)
	if len(aliases) == 0 {
		return nil // import gate
	}
	var out []models.ToolDef
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		if n.Type() != "call_expression" {
			return true
		}
		if astutil.TSCalleeText(n, pf.Source, aliases) != "tool" {
			return true
		}
		tool, ok := extractTSToolFromCall(n, pf)
		if ok {
			out = append(out, tool)
		}
		return true
	})
	return out
}

func extractTSToolFromCall(call *sitter.Node, pf ParsedFile) (models.ToolDef, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return models.ToolDef{}, false
	}
	// arguments are positional: name (string), description (string),
	// schema (object), handler (async fn), extras (optional object).
	posArgs := positionalArgs(args)
	if len(posArgs) < 4 {
		return models.ToolDef{}, false
	}
	name := tsStringLiteralText(posArgs[0], pf.Source)
	desc := tsStringLiteralText(posArgs[1], pf.Source)
	tool := models.ToolDef{
		Name:        name,
		Description: desc,
		Kind:        models.KindClaudeSDKTool,
		Language:    models.LanguageTypeScript,
		Location: models.Location{
			FilePath: pf.RelPath,
			Line:     int(call.StartPoint().Row) + 1,
		},
	}
	// Param names from the Zod schema object.
	if posArgs[2].Type() == "object" {
		kt := astutil.TSObjectKwargs(posArgs[2], pf.Source)
		for k := range kt.Children {
			tool.ParamNames = append(tool.ParamNames, k)
		}
		tool.HasTypedParams = len(kt.Children) > 0
	}
	// Handler body facts (arg 3): shells_out, http_call.
	if len(posArgs) >= 4 {
		facts := tsHandlerFacts(posArgs[3], pf.Source)
		if len(facts) > 0 {
			if tool.Facts == nil {
				tool.Facts = map[string]string{}
			}
			for k, v := range facts {
				tool.Facts[k] = v
			}
		}
	}
	// Extras (arg 4) flattened into Config.
	if len(posArgs) >= 5 && posArgs[4].Type() == "object" {
		extras := astutil.TSObjectKwargs(posArgs[4], pf.Source)
		if tool.Config == nil {
			tool.Config = map[string]string{}
		}
		flattenKwargs("", extras, tool.Config)
	}
	return tool, true
}

// positionalArgs returns the named children of an "arguments" node, which
// in tree-sitter-typescript are the actual arguments (commas are anonymous).
func positionalArgs(args *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	for i := 0; i < int(args.NamedChildCount()); i++ {
		out = append(out, args.NamedChild(i))
	}
	return out
}

func tsStringLiteralText(n *sitter.Node, src []byte) string {
	if n == nil || n.Type() != "string" {
		return ""
	}
	raw := astutil.NodeText(n, src)
	if len(raw) < 2 {
		return ""
	}
	return raw[1 : len(raw)-1]
}

// tsHandlerFacts walks a handler node (arrow_function or function) and
// returns facts about its body. Mirrors the Python callsShell pattern but
// recognizes JS/TS shell + HTTP call shapes.
func tsHandlerFacts(handler *sitter.Node, src []byte) map[string]string {
	out := map[string]string{}
	if handler == nil {
		return out
	}
	astutil.Walk(handler, func(n *sitter.Node) bool {
		if n.Type() != "call_expression" {
			return true
		}
		fn := n.ChildByFieldName("function")
		if fn == nil {
			return true
		}
		text := astutil.NodeText(fn, src)
		switch text {
		case "fetch", "axios", "axios.get", "axios.post", "axios.put", "axios.delete",
			"axios.patch", "axios.request", "got", "got.get", "got.post",
			"undici.fetch", "undici.request":
			out["http_call"] = "true"
		case "execSync", "exec", "spawn", "spawnSync", "fork":
			out["shells_out"] = "true"
		}
		return true
	})
	return out
}

// flattenKwargs walks a KwargTree and writes leaf values into out using
// dot-joined keys (`annotations.readOnlyHint` etc.).
func flattenKwargs(prefix string, kt *models.KwargTree, out map[string]string) {
	if kt == nil {
		return
	}
	for k, child := range kt.Children {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if child.Value != nil {
			out[key] = child.Value.Text
			continue
		}
		flattenKwargs(key, child, out)
	}
}
