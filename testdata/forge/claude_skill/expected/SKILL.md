---
name: claude-skill-safety
description: >-
  High-risk patterns in Claude Code Agent Skills (SKILL.md): auto-approved shell, pre-model dynamic-context execution,…
allowed-tools: Read
disable-model-invocation: false
---

# Trustabl Pre-Coding: Claude Agent Skill safety

<!-- generated: 2026-01-01 | rules: 0000000000000000000000000000000000000000 | schema: 1 | sdks: claude_skill -->

Before writing any SKILL.md, you MUST apply every constraint below. Rules are
ordered by severity. A violation here will fire the corresponding finding
in post-build scan — prevent it now.

## Tool Grant Decision Tree

1. Does this skill need shell commands?
   YES → specify exact prefixes: Bash(git status *) — never bare Bash or Bash(*)
   NO  → omit Bash entirely

2. Does allowed-tools include any of: Bash / Write / Edit / WebFetch / NotebookEdit?
   YES → set disable-model-invocation: true
   NO  → disable-model-invocation may be false (read-only skills are safe to auto-invoke)

---

## [CSKILL-003] Dynamic-context command performs network egress or reads secrets
**Severity:** critical | **Confidence:** 0.85

**Directive:** Remove the network and credential access from dynamic-context commands entirely.

**Why:** A dynamic-context command in this skill performs network egress (curl / wget / nc / …) or reads credentials/secrets (gh auth, $AWS_*, ~/.aws, ~/.ssh, id_rsa, *_key, …).

**When this applies:** Any dynamic-context command performing network egress or accessing credentials.

---

## [CSKILL-011] Bundled skill script reads credentials or secrets
**Severity:** critical | **Confidence:** 0.80

**Directive:** Remove credential and secret access from bundled scripts.

**Why:** A script bundled with this skill reads credentials or secrets (gh auth, $AWS_*, ~/.aws, ~/.ssh, id_rsa, *_key, …).

**When this applies:** Any skill directory that bundles scripts reading credentials or secrets.

---

## [CSKILL-001] Skill auto-approves unrestricted shell
**Severity:** high | **Confidence:** 0.90

**Directive:** Replace the wildcard with the specific command prefixes the skill needs, e.g.

**Why:** This skill's allowed-tools pre-approves unrestricted shell — a bare `Bash` grant or `Bash(*)`.

**When this applies:** Every skill — always verify allowed-tools before emitting.

---

## [CSKILL-002] Skill runs shell during load (dynamic-context execution)
**Severity:** high | **Confidence:** 0.90

**Directive:** Move the work into the skill's instructions so Claude runs it as a normal, model-visible tool call, or into a reviewed bundled script invoked explicitly.

**Why:** This skill's body uses dynamic-context execution (the inline ! `cmd` form or a ```! fenced block).

**When this applies:** Any skill body that uses the backtick-exec or fenced-exec form.

---

## [CSKILL-010] Bundled skill script performs network egress
**Severity:** high | **Confidence:** 0.70

**Directive:** Remove network calls from bundled scripts, or gate them behind an explicit, reviewed, user-approved step.

**Why:** A script bundled in this skill's directory makes outbound network calls (curl / wget / nc / …).

**When this applies:** Any skill directory that bundles scripts making outbound network calls.

---

## [CSKILL-030] Bundled skill file contains a hardcoded secret
**Severity:** high | **Confidence:** 0.85

**Directive:** Remove the secret from the bundled file and rotate it immediately — assume it is compromised the moment it is committed.

**Why:** A file bundled with this skill contains a hardcoded secret — a recognizable credential format such as an AWS access key (AKIA…), a GitHub token (ghp_… / github_pat_…), a Slack or Google API token, an OpenAI-style key, or a private-key block.

**When this applies:** Any file bundled within the skill directory.

---

## [CSKILL-050] Model-invocable skill grants side-effecting tools
**Severity:** high | **Confidence:** 0.80

**Directive:** Add `disable-model-invocation: true` so only the user can invoke this skill, or narrow allowed-tools to read-only tools (Read, Grep, Glob).

**Why:** Claude can auto-invoke this skill (disable-model-invocation is not set) and it pre-approves a side-effecting or exfiltration-capable tool (Bash / Write / Edit / WebFetch / NotebookEdit).

**When this applies:** Any skill where disable-model-invocation is not set to true, and Any skill pre-approving Bash, Write, Edit, WebFetch, or NotebookEdit in allowed-tools.

---

## [CSKILL-020] Skill fetches untrusted external content
**Severity:** medium | **Confidence:** 0.70

**Directive:** Avoid fetching external content from a skill.

**Why:** This skill's body references an external http(s) URL.

**When this applies:** Any skill body that references an http(s) URL.

---

## [CSKILL-040] Skill body contains prompt-injection markers
**Severity:** medium | **Confidence:** 0.60

**Directive:** Remove the override phrasing, the invisible/smuggling characters, or the encoded blob.

**Why:** This skill's body carries a prompt-injection signal — instruction-override phrasing ("ignore-previous-instructions"), invisible Unicode used to smuggle hidden text (zero-width characters, the Unicode Tags block U+E0000–E007F, or bidirectional overrides), or a long base64 blob that may conceal instructions.

**When this applies:** Any skill containing instruction-override phrasing, invisible Unicode, or encoded blobs.

---

## [CSKILL-060] Skill description claims read-only but grants side-effecting tools
**Severity:** medium | **Confidence:** 0.50

**Directive:** Make the description match the grants: either narrow allowed-tools to the read-only set the description promises (Read, Grep, Glob), or correct the description to disclose the side-effecting tools the skill actually uses.

**Why:** This skill's description claims it is read-only or side-effect-free, yet its allowed-tools pre-approve a side-effecting or exfiltration-capable tool (Bash / Write / Edit / WebFetch / NotebookEdit, or unrestricted shell).

**When this applies:** Any skill whose description claims read-only but grants side-effecting tools.

---

## [CSKILL-061] Skill allowed-tools list has duplicate tool references
**Severity:** low | **Confidence:** 0.70

**Directive:** Remove the duplicate entry so each tool appears once in allowed-tools.

**Why:** This skill's allowed-tools list references the same tool more than once (case-insensitive, whitespace-trimmed).

**When this applies:** Any skill with duplicate entries in allowed-tools.

---

## [CSKILL-070] Skill is missing a description
**Severity:** low | **Confidence:** 0.90

**Directive:** Add a description: field to the skill's SKILL.md frontmatter that names what the skill does, what tools it uses, and any side effects it may have.

**Why:** This skill has no description.

**When this applies:** Every skill — description is required.

---

## [CSKILL-071] Skill is bound to a single agent, reducing portability
**Severity:** low | **Confidence:** 0.60

**Directive:** Only bind a skill to a specific agent when that coupling is intentional and documented in the skill's description.

**Why:** This skill's frontmatter binds it to one specific agent (context: fork / agent: <name>) rather than leaving it agent-agnostic.

**When this applies:** Only when frontmatter includes a context: or agent: binding field.

