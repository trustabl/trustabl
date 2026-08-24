package rules_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestLC112HumanApprovalPrerequisites(t *testing.T) {
	middleware := func(items ...models.Expr) *models.KwargTree {
		return &models.KwargTree{
			Value: &models.Expr{
				Kind: models.ExprList,
				List: items,
			},
		}
	}
	checkpointer := &models.KwargTree{
		Value: &models.Expr{Kind: models.ExprCall, Text: "InMemorySaver()"},
	}

	cases := []struct {
		name      string
		agent     models.AgentDef
		wantFires bool
	}{
		{
			name: "fires with privileged tool and no approval prerequisites",
			agent: models.AgentDef{
				SDK: models.SDKLangChain, Class: "CreateAgent", Language: models.LanguagePython,
				HostedToolRefs: []models.HostedToolRef{{Class: "ShellTool"}},
			},
			wantFires: true,
		},
		{
			name: "fires with middleware but no checkpointer",
			agent: models.AgentDef{
				SDK: models.SDKLangChain, Class: "CreateAgent", Language: models.LanguagePython,
				HostedToolRefs: []models.HostedToolRef{{Class: "ShellTool"}},
				Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
					"middleware": middleware(models.Expr{Kind: models.ExprCall, Text: "HumanInTheLoopMiddleware(...)"}),
				}},
			},
			wantFires: true,
		},
		{
			name: "fires with empty middleware even when checkpointer is present",
			agent: models.AgentDef{
				SDK: models.SDKLangChain, Class: "CreateAgent", Language: models.LanguagePython,
				HostedToolRefs: []models.HostedToolRef{{Class: "PythonREPLTool"}},
				Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
					"middleware":   middleware(),
					"checkpointer": checkpointer,
				}},
			},
			wantFires: true,
		},
		{
			name: "silent with middleware and checkpointer",
			agent: models.AgentDef{
				SDK: models.SDKLangChain, Class: "CreateAgent", Language: models.LanguagePython,
				HostedToolRefs: []models.HostedToolRef{{Class: "ShellTool"}},
				Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
					"middleware":   middleware(models.Expr{Kind: models.ExprCall, Text: "HumanInTheLoopMiddleware(...)"}),
					"checkpointer": checkpointer,
				}},
			},
			wantFires: false,
		},
		{
			name: "silent without a privileged execution tool",
			agent: models.AgentDef{
				SDK: models.SDKLangChain, Class: "CreateAgent", Language: models.LanguagePython,
			},
			wantFires: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadAgentRule(t, "LC-112")
			if !d.Applies(tc.agent) {
				if tc.wantFires {
					t.Fatalf("LC-112 does not apply to %s/%s", tc.agent.SDK, tc.agent.Class)
				}
				return
			}

			fired := false
			for _, f := range d.Detect(tc.agent, models.RepoInventory{}) {
				if f.RuleID == "LC-112" {
					fired = true
					break
				}
			}
			if fired != tc.wantFires {
				t.Fatalf("LC-112 fired=%v, want %v", fired, tc.wantFires)
			}
		})
	}
}
