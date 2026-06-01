// Package review implements the Diff Renderer of architecture §2 — the
// human-readable scan summary printed to stdout.
package review

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/trustabl/trustabl/internal/models"
)

// Renderer produces the human-readable scan summary printed to stdout.
type Renderer struct {
	NoColor bool
}

func (r *Renderer) styles() (high, med, low, ok_, dim, header lipgloss.Style) {
	renderer := lipgloss.DefaultRenderer()
	if r.NoColor {
		renderer = lipgloss.NewRenderer(io.Discard, termenv.WithColorCache(true))
		renderer.SetColorProfile(termenv.Ascii)
	}
	high = renderer.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	med = renderer.NewStyle().Foreground(lipgloss.Color("208"))
	low = renderer.NewStyle().Foreground(lipgloss.Color("220"))
	ok_ = renderer.NewStyle().Foreground(lipgloss.Color("82")).Bold(true)
	dim = renderer.NewStyle().Foreground(lipgloss.Color("245"))
	header = renderer.NewStyle().Bold(true).Underline(true)
	return
}

func (r *Renderer) Render(result models.ScanResult) string {
	styleHigh, styleMed, styleLow, styleOK, styleDim, styleHeader := r.styles()
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n", styleHeader.Render("Scan summary"))
	fmt.Fprintf(&b, "  Repo:           %s\n", result.Repo)
	fmt.Fprintf(&b, "  Languages:      %s\n", csv(result.Languages))
	fmt.Fprintf(&b, "  SDKs:           %s\n", csv(result.SDKs))
	if result.HasShellInvocations {
		// Surface the risk honestly: report what we saw, where to look first,
		// the threat model, and a concrete fix. No openshell rule pack ships
		// today, so we must not imply an audit happened — but a one-line
		// "unaudited surface" label is useless to a user trying to act on it.
		shells := shellInvocationLocations(result.Tools)
		noun, verb := "functions", "call"
		if len(shells) == 1 {
			noun, verb = "function", "calls"
		}
		fmt.Fprintf(&b, "  Risk surfaces:  openshell %s\n",
			styleDim.Render(fmt.Sprintf("(%d %s %s subprocess.* / os.system / os.popen)", len(shells), noun, verb)))
		if len(shells) > 0 {
			const sample = 3
			end := len(shells)
			if end > sample {
				end = sample
			}
			parts := make([]string, end)
			for i := 0; i < end; i++ {
				parts[i] = fmt.Sprintf("%s:%d", shells[i].FilePath, shells[i].Line)
			}
			line := strings.Join(parts, ", ")
			if len(shells) > sample {
				line += fmt.Sprintf(" — %d more", len(shells)-sample)
			}
			fmt.Fprintf(&b, "      %s %s\n", styleDim.Render("e.g."), styleDim.Render(line))
		}
		fmt.Fprintf(&b, "      %s %s\n",
			styleDim.Render("why:"),
			styleDim.Render("an agent that exposes any of these as a callable tool can be prompt-injected into running arbitrary commands."))
		fmt.Fprintf(&b, "      %s %s\n",
			styleDim.Render("fix:"),
			styleDim.Render("sandbox the call (NVIDIA OpenShell or equivalent), validate args against a strict allowlist, avoid shell=True, and keep shell logic out of agent-callable code paths."))
	}
	// Tool surface, broken out by where each kind enters the rule pipeline.
	// Previously this was one conflated "Tools found: N" union which led users
	// to wonder why only some appeared in per-tool readiness. The honest
	// breakdown: custom tool definitions have function bodies and run through
	// tool-scope rules; agent grants and hosted instances do not (no body to
	// audit) but are inputs to agent-scope rules.
	fmt.Fprintf(&b, "  Tool definitions:   %d %s\n", len(result.Tools),
		styleDim.Render("(custom tools with function bodies — audited in Surface readiness below)"))
	if n := distinctAgentToolGrants(result); n > 0 {
		fmt.Fprintf(&b, "  Agent tool grants:  %d %s\n", n,
			styleDim.Render("(tool names the agent may call — audited by agent-scope rules)"))
	}
	if n := len(result.HostedTools); n > 0 {
		fmt.Fprintf(&b, "  Hosted tools:       %d %s\n", n,
			styleDim.Render("("+hostedToolClassList(result.HostedTools)+")"))
	}
	if n := len(result.MCPServers); n > 0 {
		fmt.Fprintf(&b, "  MCP servers:        %d %s\n", n,
			styleDim.Render("("+mcpServerClassList(result.MCPServers)+")"))
	}
	if n := len(result.Subagents); n > 0 {
		fmt.Fprintf(&b, "  Subagents:          %d %s\n", n,
			styleDim.Render("("+subagentNamesList(result.Subagents)+")"))
	}
	if n := len(result.Skills); n > 0 {
		fmt.Fprintf(&b, "  Skills:             %d %s\n", n,
			styleDim.Render("("+skillNamesList(result.Skills)+")"))
	}
	if n := len(result.SlashCommands); n > 0 {
		fmt.Fprintf(&b, "  Slash commands:     %d\n", n)
	}
	if n := len(result.PluginManifests); n > 0 {
		fmt.Fprintf(&b, "  Plugin manifests:   %d\n", n)
	}
	if n := len(result.ClaudeSettings); n > 0 {
		fmt.Fprintf(&b, "  Claude settings:    %d file(s)\n", n)
	}
	fmt.Fprintf(&b, "  Agents found:       %d\n", len(result.Agents))
	fmt.Fprintf(&b, "  Findings:           %d\n", len(result.Findings))
	sevTag := func(s models.Severity) string {
		switch s {
		case models.SeverityCritical:
			return styleHigh.Render("CRIT")
		case models.SeverityHigh:
			return styleHigh.Render("HIGH")
		case models.SeverityMedium:
			return styleMed.Render(" MED")
		case models.SeverityLow:
			return styleLow.Render(" LOW")
		default:
			return styleDim.Render("INFO")
		}
	}
	scoreCell := func(score float64) string {
		pct := fmt.Sprintf("%3.0f%%", score*100)
		switch {
		case score >= 0.85:
			return styleOK.Render(pct)
		case score >= 0.6:
			return styleMed.Render(pct)
		default:
			return styleHigh.Render(pct)
		}
	}

	fmt.Fprintf(&b, "  Overall score:      %s\n\n", scoreCell(result.OverallScore))

	if len(result.Agents) > 0 {
		b.WriteString(styleHeader.Render("Agents") + "\n")
		for _, a := range result.Agents {
			label := a.Class
			if a.Name != "" {
				label += " " + a.Name
			}
			loc := formatLocation(a.Location)
			if a.Opaque {
				loc += " " + styleDim.Render("(opaque — rules cannot evaluate)")
			}
			fmt.Fprintf(&b, "  %-32s %s  %s\n", label, styleDim.Render(string(a.SDK)), loc)
			if tools := toolRefNames(a.ToolRefs); tools != "" {
				fmt.Fprintf(&b, "      %s %s\n", styleDim.Render("tools:"), tools)
			}
			if hosted := hostedToolRefNames(a.HostedToolRefs); hosted != "" {
				fmt.Fprintf(&b, "      %s %s\n", styleDim.Render("hosted tools:"), hosted)
			}
			if mcp := mcpServerRefNames(a.MCPServerRefs); mcp != "" {
				fmt.Fprintf(&b, "      %s %s\n", styleDim.Render("mcp servers:"), mcp)
			}
		}
		b.WriteString("\n")
	}

	if len(result.Subagents) > 0 {
		b.WriteString(styleHeader.Render("Subagents") + "\n")
		for _, s := range result.Subagents {
			fmt.Fprintf(&b, "  %-32s %s\n", s.Name, styleDim.Render(formatLocation(s.Location)))
			meta := []string{}
			if len(s.Tools) > 0 {
				meta = append(meta, "tools: "+strings.Join(s.Tools, ", "))
			}
			if s.Model != "" {
				meta = append(meta, "model: "+s.Model)
			}
			if len(meta) > 0 {
				fmt.Fprintf(&b, "      %s\n", styleDim.Render(strings.Join(meta, "    ")))
			}
		}
		b.WriteString("\n")
	}

	if len(result.ClaudeSettings) > 0 {
		b.WriteString(styleHeader.Render("Claude settings") + "\n")
		for _, s := range result.ClaudeSettings {
			meta := []string{}
			if s.DefaultMode != "" {
				meta = append(meta, "defaultMode="+s.DefaultMode)
			}
			meta = append(meta,
				fmt.Sprintf("allow:%d", len(s.Permissions.Allow)),
				fmt.Sprintf("deny:%d", len(s.Permissions.Deny)),
				fmt.Sprintf("ask:%d", len(s.Permissions.Ask)),
			)
			fmt.Fprintf(&b, "  %-32s %s\n", formatLocation(s.Location), styleDim.Render(strings.Join(meta, "  ")))
		}
		b.WriteString("\n")
	}

	if len(result.Findings) == 0 {
		b.WriteString(styleOK.Render("No findings. Nothing to commit.") + "\n")
		return b.String()
	}

	// Surface readiness table: one row per discovered tool, agent, and subagent,
	// plus a repo row when repo-scoped findings exist. Each row is scored from the
	// findings attributed to that surface; rows are sorted worst-first.
	b.WriteString(styleHeader.Render("Surface readiness") + "\n")
	for _, rd := range result.Surfaces {
		label := string(rd.Kind)
		if rd.Name != "" {
			label += ":" + rd.Name
		}
		fmt.Fprintf(&b, "  %-32s %s  (%d findings)\n",
			label, scoreCell(rd.Score), rd.FindingCount)
	}
	b.WriteString("\n")

	// Findings list, grouped by attribution. Discovered surfaces first (in
	// Surfaces order — already sorted worst-first). Then anything left —
	// findings whose ToolName matches no surface and repo-wide / META findings
	// (ToolName = "") — under their own headers, sorted alphabetically for
	// determinism. The empty-ToolName bucket renders under "(repo-wide)" so it
	// doesn't look like a missing label.
	b.WriteString(styleHeader.Render("Findings") + "\n")
	byTool := map[string][]models.Finding{}
	for _, f := range result.Findings {
		byTool[f.ToolName] = append(byTool[f.ToolName], f)
	}
	rendered := map[string]bool{}
	emit := func(name string) {
		fs := byTool[name]
		if len(fs) == 0 || rendered[name] {
			return
		}
		rendered[name] = true
		header := name
		if name == "" {
			header = "(repo-wide)"
		}
		fmt.Fprintf(&b, "\n  %s\n", styleHeader.Render(header))
		for _, f := range fs {
			fmt.Fprintf(&b, "    [%s] %s %s  (%s:%d)\n",
				f.RuleID, sevTag(f.Severity), f.Title,
				f.FilePath, f.Line)
			fmt.Fprintf(&b, "        %s\n", styleDim.Render(wrapAt(f.Explanation, 86)))
			fmt.Fprintf(&b, "        %s %s\n", styleDim.Render("fix:"), f.SuggestedFix)
		}
	}
	for _, s := range result.Surfaces {
		emit(s.Name)
	}
	var rest []string
	for name := range byTool {
		if !rendered[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		emit(name)
	}

	return b.String()
}

// formatLocation renders a Location as "path:line" if EndLine == Line or
// EndLine == 0 (legacy/uninitialized data), else "path:line-endline".
func formatLocation(loc models.Location) string {
	if loc.EndLine == 0 || loc.EndLine == loc.Line {
		return fmt.Sprintf("%s:%d", loc.FilePath, loc.Line)
	}
	return fmt.Sprintf("%s:%d-%d", loc.FilePath, loc.Line, loc.EndLine)
}

// csv joins a list of string-like values for the summary header, or "(none)"
// when empty so the line never renders blank.
func csv[T ~string](items []T) string {
	if len(items) == 0 {
		return "(none)"
	}
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = string(it)
	}
	return strings.Join(parts, ", ")
}

// distinctAgentToolGrants is the count of distinct tool names that agents
// in this repo are granted permission to call (across all agents, deduped,
// quote-stripped). These are NOT tool definitions — they're built-in or
// MCP-namespaced names referenced from `allowedTools` / `tools` arrays.
// They have no function body so tool-scope rules cannot audit them; they
// feed agent-scope rules instead ("agent X grants Bash without a callback").
func distinctAgentToolGrants(result models.ScanResult) int {
	seen := make(map[string]struct{})
	for _, a := range result.Agents {
		for _, r := range a.ToolRefs {
			seen[strings.Trim(r.Name, `"'`)] = struct{}{}
		}
	}
	return len(seen)
}

// toolRefNames renders an agent's granted tools (quote-stripped) for the
// Agents section. These are the tools the agent can invoke — built-in tool
// names for Claude AgentDefinition, resolved/external refs for others — and
// are distinct from discovered tool *definitions* counted in "Tool defs".
func toolRefNames(refs []models.ToolRef) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = strings.Trim(r.Name, `"'`)
	}
	return strings.Join(parts, ", ")
}

// hostedToolRefNames renders the agent's hosted-tool classes (WebSearchTool,
// FileSearchTool, ...). These are OpenAI Agents SDK SDK-managed runtimes
// instantiated inline in `tools=[...]` and never have a function body to
// analyse.
func hostedToolRefNames(refs []models.HostedToolRef) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = r.Class
	}
	return strings.Join(parts, ", ")
}

// mcpServerRefNames renders the agent's MCP server classes with transport
// hint (stdio / sse / streamable_http). External refs (unresolvable alias
// or unknown class) render as the raw name with an "(external)" marker.
func mcpServerRefNames(refs []models.MCPServerRef) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, len(refs))
	for i, r := range refs {
		switch {
		case r.Resolved != nil:
			parts[i] = fmt.Sprintf("%s (%s)", r.Resolved.Class, r.Resolved.Transport)
		case r.External:
			parts[i] = r.Class + " (external)"
		default:
			parts[i] = r.Class
		}
	}
	return strings.Join(parts, ", ")
}

// hostedToolClassList renders distinct hosted-tool classes for the scan
// summary one-liner. The same class instantiated on N agents collapses to a
// single token.
func hostedToolClassList(hs []models.HostedToolDef) string {
	if len(hs) == 0 {
		return ""
	}
	seen := make(map[string]bool)
	var classes []string
	for _, h := range hs {
		if !seen[h.Class] {
			seen[h.Class] = true
			classes = append(classes, h.Class)
		}
	}
	return strings.Join(classes, ", ")
}

// mcpServerClassList renders MCP server classes with their multiplicity for
// the scan summary one-liner (e.g. "MCPServerStdio×2, MCPServerSse").
func mcpServerClassList(ms []models.MCPServerDef) string {
	if len(ms) == 0 {
		return ""
	}
	counts := map[string]int{}
	order := []string{}
	for _, m := range ms {
		if _, ok := counts[m.Class]; !ok {
			order = append(order, m.Class)
		}
		counts[m.Class]++
	}
	parts := make([]string, len(order))
	for i, c := range order {
		if counts[c] > 1 {
			parts[i] = fmt.Sprintf("%s×%d", c, counts[c])
		} else {
			parts[i] = c
		}
	}
	return strings.Join(parts, ", ")
}

// subagentNamesList renders the discovered subagent names for the scan
// summary one-liner.
func subagentNamesList(subs []models.SubagentDef) string {
	if len(subs) == 0 {
		return ""
	}
	parts := make([]string, len(subs))
	for i, s := range subs {
		parts[i] = s.Name
	}
	return strings.Join(parts, ", ")
}

// shellInvocationLocations returns the shell-invocation tools sorted
// deterministically by (FilePath, Line) so the "first few" examples on the
// risk-surfaces line are stable across runs.
func shellInvocationLocations(tools []models.ToolDef) []models.ToolDef {
	var out []models.ToolDef
	for _, t := range tools {
		if t.Kind == models.KindShellInvocation {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FilePath != out[j].FilePath {
			return out[i].FilePath < out[j].FilePath
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// skillNamesList renders the discovered skill names for the scan summary
// one-liner.
func skillNamesList(skills []models.SkillDef) string {
	if len(skills) == 0 {
		return ""
	}
	parts := make([]string, len(skills))
	for i, s := range skills {
		parts[i] = s.Name
	}
	return strings.Join(parts, ", ")
}

// wrapAt is a deliberately dumb word-wrapper. The output is for humans on a
// terminal; a perfect wrap isn't worth a dependency.
func wrapAt(s string, n int) string {
	words := strings.Fields(s)
	var b strings.Builder
	col := 0
	for i, w := range words {
		if col > 0 && col+1+len(w) > n {
			b.WriteString("\n        ")
			col = 0
		} else if i > 0 {
			b.WriteString(" ")
			col++
		}
		b.WriteString(w)
		col += len(w)
	}
	return b.String()
}
