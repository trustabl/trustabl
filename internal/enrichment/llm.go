package enrichment

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// llmEnricher is the interface the pipeline uses to call the LLM.
// The real implementation is llmClient; tests inject a mock.
type llmEnricher interface {
	enrichFile(ctx context.Context, filePath string, issues []issueContext) ([]enrichResult, error)
}

type enrichResult struct {
	Explanation   string `json:"explanation"`
	Fix           string `json:"fix"`
	LineStart     int    `json:"line_start"`
	LineEnd       int    `json:"line_end"`
	Replacement   string `json:"replacement"`
	FalsePositive bool   `json:"false_positive"`
}

// issueContext is the per-finding context assembled by the pipeline and sent to the LLM.
type issueContext struct {
	ruleID      string
	title       string
	severity    string
	ruleScope   string // "tool" | "agent" | "subagent" | "repo"
	line        int
	explanation string
	fixTemplate string
	codeBlock   string // extracted enclosing function/class block with → marker
}

type llmClient struct {
	client anthropic.Client
	model  anthropic.Model
}

func newLLMClient(apiKey, modelName string) *llmClient {
	return &llmClient{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  anthropic.Model(modelName),
	}
}

func (c *llmClient) enrichFile(ctx context.Context, filePath string, issues []issueContext) ([]enrichResult, error) {
	issueList := buildIssueList(issues)

	system := `You are a security engineer reviewing agent code misconfigurations.
You will be given a list of detected issues. Each issue includes the exact flagged line number,
the rule scope (tool / agent / subagent / repo), and the enclosing code block.
Use the rule scope to understand what kind of construct is being flagged.
Respond with a JSON array only — one object per issue in the same order:
[{"explanation":"...","fix":"...","line_start":N,"line_end":N,"replacement":"...","false_positive":false},...]
- explanation: 2-3 sentences specific to the actual code at that line (not generic)
- fix: human-readable description of what was changed and why
- line_start / line_end: MUST include the flagged line number. Only expand the range if adjacent lines must also change.
- replacement: the exact new lines in the correct language (preserve original indentation, no trailing newline)
If no code change is needed set line_start, line_end to 0 and replacement to "".
Do not wrap in markdown code fences.`

	user := fmt.Sprintf("File: %s\n\nIssues:\n%s\nRespond with a JSON array of %d objects.",
		filePath, issueList, len(issues))

	msg, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: 2048,
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("claude api: %w", err)
	}
	if len(msg.Content) == 0 {
		return nil, fmt.Errorf("claude api: empty response")
	}

	raw := strings.TrimSpace(msg.Content[0].Text)
	raw = stripFence(raw)

	var results []enrichResult
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		results = salvagePartialJSON(raw, len(issues))
		if len(results) == 0 {
			return nil, fmt.Errorf("parse llm response: %w", err)
		}
	}
	return results, nil
}

// buildIssueList formats the issue list portion of the LLM prompt.
func buildIssueList(issues []issueContext) string {
	var sb strings.Builder
	for i, iss := range issues {
		fmt.Fprintf(&sb, "%d. FLAGGED LINE: %d - %s (%s)\n   Severity: %s\n   Rule scope: %s\n   Issue: %s\n   Fix template: %s\n   Code:\n%s\n",
			i+1, iss.line, iss.title, iss.ruleID, iss.severity, iss.ruleScope,
			iss.explanation, iss.fixTemplate, indentBlock(iss.codeBlock, "   "))
	}
	return sb.String()
}

// stripFence removes a leading ```[language]\n / trailing \n``` wrapper if present.
func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Strip leading ```[language]\n
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[idx+1:]
	} else {
		s = strings.TrimLeft(s, "`abcdefghijklmnopqrstuvwxyz")
	}
	// Strip trailing \n``` or ```
	s = strings.TrimRight(s, "\n")
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimRight(s, "\n")
}

func salvagePartialJSON(raw string, expected int) []enrichResult {
	results := make([]enrichResult, expected)
	depth, start, idx := 0, -1, 0

	for i, ch := range raw {
		if ch == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 && start != -1 {
				var r enrichResult
				if err := json.Unmarshal([]byte(raw[start:i+1]), &r); err == nil && idx < expected {
					results[idx] = r
					idx++
				}
				start = -1
			}
		}
	}
	return results
}

func indentBlock(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
