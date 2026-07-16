---
name: no-desc-skill
allowed-tools: Read
---

# No Description Skill (synthetic test fixture)

This is a synthetic Trustabl test fixture used to exercise the
skill_has_description predicate. It intentionally omits the `description:`
frontmatter field. It is otherwise clean: no dynamic-context execution, no
external URLs, no bundled scripts, and no prompt-injection markers, so
CSKILL-070 is the only skill-scope rule expected to fire.
