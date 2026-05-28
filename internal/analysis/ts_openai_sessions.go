package analysis

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/trustabl/trustabl/internal/analysis/astutil"
	"github.com/trustabl/trustabl/internal/models"
)

// tsOpenAISessionClasses is the set of OpenAI session class names recognized
// via `new X(...)`. tsOpenAISessionFactories is the set of factory function
// names recognized via plain call_expression.
var tsOpenAISessionClasses = map[string]bool{
	"MemorySession":                    true,
	"OpenAIConversationsSession":       true,
	"OpenAIResponsesCompactionSession": true,
}

var tsOpenAISessionFactories = map[string]bool{
	"startOpenAIConversationsSession": true,
}

// DiscoverTSOpenAISessions walks each parsed TS file and emits a SessionUse
// record for every session-class instantiation OR session-factory call.
func DiscoverTSOpenAISessions(files []ParsedFile, onFile func(string)) []models.SessionUse {
	var out []models.SessionUse
	for _, pf := range files {
		if onFile != nil {
			onFile(pf.RelPath)
		}
		out = append(out, discoverTSOpenAISessionsInFile(pf)...)
	}
	return out
}

func discoverTSOpenAISessionsInFile(pf ParsedFile) []models.SessionUse {
	if pf.Tree == nil {
		return nil
	}
	aliases := astutil.TSImportAliasesAny(pf.Tree.RootNode(), pf.Source, tsOpenAIAgentsModules)
	if len(aliases) == 0 {
		return nil
	}
	var out []models.SessionUse
	astutil.Walk(pf.Tree.RootNode(), func(n *sitter.Node) bool {
		switch n.Type() {
		case "new_expression":
			ctor := n.ChildByFieldName("constructor")
			if ctor == nil || ctor.Type() != "identifier" {
				return true
			}
			canon, ok := aliases[astutil.NodeText(ctor, pf.Source)]
			if !ok || !tsOpenAISessionClasses[canon] {
				return true
			}
			out = append(out, models.SessionUse{
				Class: canon,
				Location: models.Location{
					FilePath: pf.RelPath,
					Line:     int(n.StartPoint().Row) + 1,
					EndLine:  int(n.EndPoint().Row) + 1,
				},
			})
		case "call_expression":
			canon := astutil.TSCalleeText(n, pf.Source, aliases)
			if !tsOpenAISessionFactories[canon] {
				return true
			}
			out = append(out, models.SessionUse{
				Class: canon,
				Location: models.Location{
					FilePath: pf.RelPath,
					Line:     int(n.StartPoint().Row) + 1,
					EndLine:  int(n.EndPoint().Row) + 1,
				},
			})
		}
		return true
	})
	return out
}
