---
name: bound-skill
description: A skill bound to one specific agent
agent: some-agent
allowed-tools: Read
---

# Bound Skill (synthetic test fixture)

This is a synthetic Trustabl test fixture used to exercise the
skill_is_agent_specific predicate. It is intentionally coupled to a
specific agent via the `agent:` frontmatter field and is not meant to be
reused across agents. It only reads files the user points it at — no
dynamic-context execution, no external URLs, no bundled scripts, and no
prompt-injection markers, so CSKILL-071 is the only skill-scope rule
expected to fire.
