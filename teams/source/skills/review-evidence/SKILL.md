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

For separated managed workflow, assess the factory's frozen identities/revisions,
retained implementation snapshots and executed checks, never substitute live issue
edits. Return exactly one structured outcome per criterion and code, architecture
and approved-plan review findings through the versioned runner output contract.
Classify blocking defects separately from advisory improvements; advisory findings
alone do not prevent acceptance. Include proof references and explicit unmet or
unverified outcomes. Never propose a waiver as authority. Missing evidence remains
unverified and failed evidence remains failed even after an owner exception.
Only the factory records decisions, ticks criteria, posts comments or closes
stories. Follow the workflow skill for owner exceptions, follow-up proposals,
all-attached-child readiness, historical acceptance and approved PR delivery.
Legacy scope retains its original workflow semantics; do not migrate it by prose.
