package rules_test

import (
	"testing"

	"github.com/trustabl/trustabl/internal/models"
)

func TestLC103LangChainHumanReviewGate(t *testing.T) {
	d := loadAgentRule(t, "LC-103")

	cases := []struct {
		name      string
		agent     models.AgentDef
		wantFires bool
	}{
		{
			name: "fires when CreateAgent wires PythonREPLTool without HITL prerequisites",
			agent: models.AgentDef{
				SDK:            models.SDKLangChain,
				Class:          "CreateAgent",
				Language:       models.LanguagePython,
				HostedToolRefs: []models.HostedToolRef{{Class: "PythonREPLTool"}},
			},
			wantFires: true,
		},
		{
			name: "silent when middleware and checkpointer are configured",
			agent: models.AgentDef{
				SDK:            models.SDKLangChain,
				Class:          "CreateAgent",
				Language:       models.LanguagePython,
				HostedToolRefs: []models.HostedToolRef{{Class: "PythonREPLTool"}},
				Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
					"middleware": {
						Value: &models.Expr{
							Kind: models.ExprList,
							Text: "[HumanInTheLoopMiddleware(...)]",
							List: []models.Expr{{Kind: models.ExprCall, Text: "HumanInTheLoopMiddleware(...)"}},
						},
					},
					"checkpointer": {
						Value: &models.Expr{Kind: models.ExprCall, Text: "InMemorySaver()"},
					},
				}},
			},
			wantFires: false,
		},
		{
			name: "fires when middleware is empty even with a checkpointer",
			agent: models.AgentDef{
				SDK:            models.SDKLangChain,
				Class:          "CreateAgent",
				Language:       models.LanguagePython,
				HostedToolRefs: []models.HostedToolRef{{Class: "ShellTool"}},
				Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
					"middleware": {
						Value: &models.Expr{Kind: models.ExprList, Text: "[]", List: []models.Expr{}},
					},
					"checkpointer": {
						Value: &models.Expr{Kind: models.ExprCall, Text: "InMemorySaver()"},
					},
				}},
			},
			wantFires: true,
		},
		{
			name: "fires when checkpointer is missing despite middleware",
			agent: models.AgentDef{
				SDK:            models.SDKLangChain,
				Class:          "CreateAgent",
				Language:       models.LanguagePython,
				HostedToolRefs: []models.HostedToolRef{{Class: "PythonAstREPLTool"}},
				Kwargs: &models.KwargTree{Children: map[string]*models.KwargTree{
					"middleware": {
						Value: &models.Expr{
							Kind: models.ExprList,
							Text: "[HumanInTheLoopMiddleware(...)]",
							List: []models.Expr{{Kind: models.ExprCall, Text: "HumanInTheLoopMiddleware(...)"}},
						},
					},
				}},
			},
			wantFires: true,
		},
		{
			name: "silent when CreateAgent has no dangerous hosted tool",
			agent: models.AgentDef{
				SDK:      models.SDKLangChain,
				Class:    "CreateAgent",
				Language: models.LanguagePython,
			},
			wantFires: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !d.Applies(tc.agent) {
				if tc.wantFires {
					t.Fatalf("LC-103 does not apply to CreateAgent")
				}
				return
			}
			fired := false
			for _, finding := range d.Detect(tc.agent, models.RepoInventory{}) {
				if finding.RuleID == "LC-103" {
					fired = true
					break
				}
			}
			if fired != tc.wantFires {
				t.Fatalf("LC-103 fired=%v, want %v", fired, tc.wantFires)
			}
		})
	}
}
