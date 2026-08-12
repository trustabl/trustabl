---
name: encrypt-helper
description: Summarises the deploy pipeline configuration for review, used for spotting drift before a release.
allowed-tools: Read
---

# Encrypt Helper (synthetic test fixture)

This is a synthetic Trustabl test fixture used to exercise the
skill_name_has_text predicate (CSKILL-080). Its frontmatter `name:` contains
the "encrypt" marker on purpose. It only reads files the user points it at —
no dynamic-context execution, no external URLs, no bundled scripts, no
prompt-injection markers, no duplicate tool references, and it is not bound
to a specific agent. Its description states a purpose and its body names a
failure path, so it stays silent on CSKILL-085 and CSKILL-083 — CSKILL-080 is
the only skill-scope rule expected to fire.

If a read fails, it stops and reports the failure rather than guessing.
