---
name: no-desc-skill
allowed-tools: Read
---

# No Description Skill (synthetic test fixture)

This is a synthetic Trustabl test fixture used to exercise the
skill_has_description predicate. It intentionally omits the `description:`
frontmatter field. It is otherwise clean: no dynamic-context execution, no
external URLs, no bundled scripts, and no prompt-injection markers, so
CSKILL-070 is the only skill-scope rule expected to fire — except CSKILL-085
(missing purpose language), which also legitimately fires here since the
omitted description carries no purpose phrase either. If a read fails, it
stops and reports the failure rather than guessing.
