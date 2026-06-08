---
name: trustabl-enrich
description: Enriches source files flagged by a Trustabl scan — adds what is missing and corrects what is wrong, guided entirely by the scan's own explanation and fix text. Use after `trustabl scan` to apply findings directly to source files without manual editing.
license: Apache-2.0
metadata:
  author: trustabl
  version: "1.0.0"
  domain: security
  triggers: "trustabl enrich, trustabl scan results, fix agent misconfigurations, fix tool misconfigurations, apply trustabl findings, remediate scan findings, add missing guardrails, add missing docstring, add missing timeout, fix subagent frontmatter"
  role: expert
  scope: fix
  output-format: structured
  related-skills: trustabl-scan
---

# Trustabl Enrich

Applies Trustabl scan findings directly to source files. "Enrich" covers the full remediation loop: adding what is absent (guardrails, docstrings, timeouts, type annotations) and correcting what is wrong (unsafe patterns, misconfigurations). The scan result already contains the solution — this skill translates it into code.

## When to Use

- You have output from `trustabl scan` (JSON, SARIF, or plain text) and want findings applied to source files automatically
- You want to remediate agent misconfigurations without editing each file manually
- You are triaging findings to separate real issues from false positives before applying changes

This skill does **not** run `trustabl scan`. Run the scan first (or use the `trustabl-scan` skill), then invoke this skill with the results.

## Input Formats

All three formats produced by the Trustabl CLI are accepted. The skill detects the format automatically.

| Format | Command |
|--------|---------|
| JSON | `trustabl scan --format json` |
| SARIF 2.1.0 | `trustabl scan --format sarif` |
| Plain text | paste terminal output directly |

> When you run `trustabl scan` yourself (e.g. to produce input, or to re-verify at the end), resolve the binary the way the **trustabl-scan** skill does: prefer `"$TRUSTABL_BIN"`, then the plugin-managed path reported by the Trustabl `SessionStart` check this session, then `trustabl` on `PATH`.

If the input cannot be parsed or contains zero findings, report clearly and stop — do not proceed to enrichment.

**SARIF extraction path:**
- Findings: `runs[0].results[]`
- File + line: `locations[0].physicalLocation.artifactLocation.uri` + `locations[0].physicalLocation.region.startLine`
- Explanation: `message.text`
- Fix (primary): `result.fixes[0].description.text` — present on most results
- Fix (fallback): `runs[0].tool.driver.rules[]` — match on `id == ruleId`, read `help.text`; use when `fixes` array is absent

**JSON extraction path:**
- Findings: `findings[]`
- File + line range: `file_path` + `start_line` … `end_line` (1-indexed, inclusive; a single-line finding sets `end_line == start_line`; both are `0` for repo-level findings with no location). Note: Trustabl 0.1.4 renamed the former flat `line` field to `start_line` / `end_line` — read those, not `line`.
- Explanation: `explanation` · Fix: `suggested_fix`
- Also present per finding: `rule_id`, `scope`, `severity`, `confidence`. Dependency CVE findings (when the scan ran with `--vuln-scan` / `vuln_scan`) appear in the same `findings` array — the advisory id (CVE / GHSA / PYSEC) is the `rule_id`, and `start_line` points at the dependency's line in its manifest. The structured `vulnerabilities[]` array carries the same matches with `fixed_in` versions.

## Workflow

1. **Parse** — detect the format and normalize every finding into: `file`, `start_line`, `end_line`, `rule_id`, `scope`, `severity`, `confidence`, `explanation`, `suggested_fix`. (For SARIF, `end_line` may be absent for a single-line region — fall back to `start_line`.)

2. **Summarize** — render a Markdown table before touching any file:

   | # | File | Line | Rule | Scope | Severity | Confidence | Explanation |

3. **Enrich per file** — group findings by file. For each file:
   - Read the current file content
   - Pass all findings for that file plus the file content to the model
   - The model returns replacement ranges guided by the scan's `explanation` and `suggested_fix`

4. **Apply bottom-up** — within each file, sort enrichments by `line_start` descending, then apply using the Edit tool. Bottom-up order prevents line-offset drift.

5. **Report** — after all edits: `<file>: N enrichment(s) applied, M false positive(s) skipped, K external action(s) required`. Then ask: *"Re-run `trustabl scan` to confirm findings are resolved?"* — never run it automatically.

## System Prompt

Use this prompt verbatim for each file's model call.

```text
You are a security engineer enriching AI agent source code based on findings from a Trustabl static analysis scan.

INPUTS
You will receive:
1. The current content of a source file
2. A list of findings for that file, each with:
   - start_line / end_line: the flagged line range (1-indexed, inclusive; equal for a single-line finding)
   - rule_id: Trustabl rule that fired
   - scope: tool | agent | subagent | repo
   - severity: info | low | medium | high | critical
   - confidence: 0.0–1.0
   - explanation: what is wrong and why it matters
   - suggested_fix: the exact change Trustabl recommends

Read the file content carefully before generating any replacement.

OUTPUT
Return a raw JSON array — one object per finding, in the same order as input. No prose. No markdown fences.

[
  {
    "rule_id": "<rule_id from input>",
    "line_start": <first line of replacement range — must include the flagged line>,
    "line_end": <last line of replacement range>,
    "replacement": "<exact replacement lines, original indentation preserved, no trailing newline>",
    "false_positive": false,
    "reason": "<populated only if false_positive is true>"
  }
]

SCOPE GUIDE
- tool: enrich the tool function — add missing docstrings, network timeout kwargs, type annotations, failure handlers, normalized paths
- agent: enrich the Agent / AgentDefinition constructor — add missing input_guardrails, output_guardrails, fix tool_use_behavior, correct MCP server wiring
- subagent: enrich the .claude/agents/*.md frontmatter — fix tools list, align description to tools, add missing name field
- repo: enrich project-level config — add tracing processor, add SandboxAgent, update pyproject.toml / package.json settings

RULES
- The scan's `explanation` and `suggested_fix` fields are the authoritative spec. Do not invent a different solution.
- line_start and line_end MUST include the flagged range (start_line..end_line). Expand the range only if adjacent lines must also change for the replacement to be syntactically valid.
- If the fix is a config or external action with no code edit, set line_start and line_end to 0 and replacement to "".
- Set false_positive: true only when the code is demonstrably correct despite the finding. Populate reason with the specific evidence.
- Preserve the file's indentation style (tabs vs spaces) and language idioms.
- Do not add comments to enriched code explaining the change.
- Do not add imports that are not required by the replacement code.
```

## Applying Enrichments

After the model returns the JSON array:

If the response cannot be parsed as a JSON array, halt immediately and report the raw model response to the user — do not attempt heuristic extraction.

1. Group enrichments by file.
2. Sort each file's enrichments by `line_start` descending.
3. Apply each enrichment with the Edit tool.
4. Skip and log entries where `line_start == 0` as: `[external action required] <rule_id>: <suggested_fix>`
5. Skip and log entries where `false_positive == true` as: `[false positive] <rule_id>: <reason>`

**Findings with no file location** (META findings and repo-scoped findings where `file_path` is empty): show them in the summary table with `File: (repo-level)` and `Line: —`, then log them as external actions — do not attempt an Edit for these.

## Constraints

### MUST DO
- Show the findings table before touching any file
- Read the current file content before generating enrichments for it
- Apply enrichments bottom-up within each file
- Echo `rule_id` in every enrichment object
- Mark false positives with an explicit reason
- Log `line_start == 0` entries as external actions required
- Ask before re-running `trustabl scan`

### MUST NOT DO
- Run `trustabl scan` — this skill enriches only
- Change lines outside `line_start`–`line_end` unless required for syntactic validity
- Invent findings not present in the scan output
- Deviate from the scan's `suggested_fix` field
- Add comments to enriched code
- Collapse multiple findings into one edit
