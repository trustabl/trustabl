---
name: deploy-draft
description: Summarises the deploy pipeline configuration for review
allowed-tools: Read
---

# Deploy Draft (synthetic test fixture)

This is a synthetic Trustabl test fixture used to exercise the
skill_name_has_text predicate (CSKILL-063). Its frontmatter `name:` contains
the "draft" marker on purpose. It only reads files the user points it at —
no dynamic-context execution, no external URLs, no bundled scripts, no
prompt-injection markers, no duplicate tool references, and it is not bound
to a specific agent — so CSKILL-063 is the only skill-scope rule expected to
fire.
