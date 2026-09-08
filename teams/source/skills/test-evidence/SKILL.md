---
name: test-evidence
description: Author and execute scope-matched Go tests and report actual verification.
requires:
  - ../go-constraints/SKILL.md
---

Map acceptance criteria to tests, including relevant failure paths. Use the
repository's frameworks, fixture owners and test invocation conventions. Prefer
deterministic channels and local fixtures to sleeps or external services. Do not
invent build tags, test naming conventions, infrastructure or credentials.

Use only explicitly configured non-production integration targets, or isolated
local fixtures where supported. Missing infrastructure is a documented skip, not
a pass. Do not provision external services or mutate production as incidental
test setup. Do not log environment values that may contain secrets.

Return exact commands, passed/failed/skipped coverage and remaining risks. Separate
pre-existing failures from new ones using reproducible evidence, not assumption.
Never commit tests that consume live paid CLI quota as part of the ordinary suite.
