package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
)

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
