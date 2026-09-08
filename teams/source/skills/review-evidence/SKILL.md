---
name: review-evidence
description: Review changed Go code without modifying files or executing test commands.
requires:
  - ../workflow/SKILL.md
---

Validate is read-only. Inspect current changes and relevant surrounding code,
approved acceptance criteria and existing verification reports. Do not edit source,
tests, context documents or session artifacts. Do not invoke shell/build/test
commands or widen the tool policy; use available read/search tools only.

Prioritize correctness, security, error paths, cancellation, resource ownership,
cross-story consistency and compatibility with repository constraints. Findings
need severity, file:line, concrete failure scenario and a suggested correction.
Distinguish observed test evidence from unverified behavior. Return findings inline
with BLOCKING, ADVISORY_ONLY or CLEAN and explicit verification limitations.
Do not hand off to implementation or perform fixes within this mode.
