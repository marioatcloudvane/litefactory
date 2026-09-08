---
name: go-constraints
description: Apply the repository's Go architecture and operational constraints.
requires:
  - ../workflow/SKILL.md
resources:
  - references/checklist.md
---

Read .aiteam/APP_CONTEXT.md when present, then relevant sections of
.aiteam/APP_CONSTRAINTS.md. Check go.mod and neighboring code before adding code.
Use the existing owner of paths, SQL, git operations, secrets and configuration;
do not introduce competing abstractions or dependencies without approved scope.
Read [the checklist](references/checklist.md) for implementation or test changes.
Repository-specific rules take precedence over generic Go preferences.
