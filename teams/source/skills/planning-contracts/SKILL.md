---
name: planning-contracts
description: Define feature scope, executable stories and architecture acceptance criteria.
requires:
  - ../workflow/SKILL.md
---

Derive scope from the latest owner request and current code; prior briefs are
evidence, not permission to retain superseded requirements. Separate decisions,
assumptions, non-goals, risks and existing features to reuse. In headless runs,
resolve non-material gaps reasonably instead of asking questions nobody can answer.

Each US-<n>.md must contain the user story, independently observable acceptance
criteria, a task table with dependencies and implementation/test ownership, and
task-specific architecture hints. Include failure behavior and security boundaries.
Plan_brief.md (lowercase filename: plan_brief.md) summarizes shared decisions and
indexes every local story with its path and optional existing forge URL. Existing
URLs are provenance; do not invent successful publication or update remote issues
without authorization. Document incomplete prerequisites rather than coding them
into a fabricated capability gate.
