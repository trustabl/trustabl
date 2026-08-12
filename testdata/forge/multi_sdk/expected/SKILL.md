---
name: trustabl-pre-coding
description: >-
  Pre-coding reliability constraints for: claude_sdk, openai_sdk
allowed-tools: Read
disable-model-invocation: false
---

# Trustabl Pre-Coding Reliability Constraints

<!-- generated: 2026-01-01 | rules: abc1234 | schema: 13 | sdks: claude_sdk, openai_sdk -->

Before writing any agent code, apply every constraint below. Rules are
ordered by severity. A violation here will fire the corresponding finding
in post-build scan — prevent it now.

---

## Claude Agent SDK

### Tool Rules

---

#### [CSDK-001] Claude subagent tool function has no docstring
**Severity:** medium | **Confidence:** 0.90

**Directive:** Add a docstring describing what the tool does, its parameters, and return value.

**Why:** Missing docstring means the model cannot read the tool's intent.

**When this applies:** When defining a tool.

### Agent Rules

---

#### [CSDK-101] Claude subagent is granted the Bash tool without restrictions
**Severity:** high | **Confidence:** 0.80

**Directive:** Add input guardrails or restrict tool grants to exact prefixes.

**Why:** An agent with unrestricted Bash access poses a high risk.

**When this applies:** When declaring an agent.

---

## OpenAI Agents SDK

### Tool Rules

---

#### [OAI-001] Tool function has no docstring
**Severity:** low | **Confidence:** 0.90

**Directive:** Add a docstring to every @function_tool decorated function.

**Why:** The docstring is the model-facing description; without it the model cannot use the tool correctly.

**When this applies:** When defining a tool.

