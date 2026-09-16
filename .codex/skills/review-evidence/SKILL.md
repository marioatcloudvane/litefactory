---
name: review-evidence
description: Review changed Go code without modifying files or executing test commands.
requires:
  - ../workflow/SKILL.md
---

> **Codex copy of `.claude/skills/shared/review-evidence.md`.** Both tools read the
> same instructions; only the enforcement differs, and the differences are these:
>
> - **Read `.codex/skills/workflow/SKILL.md` first.** This contract depends on it
>   for owner exceptions, follow-up proposals, all-attached-child readiness,
>   historical acceptance and approved PR delivery.
> - **The read-only boundary is yours to hold.** Claude runs the reviewer with a
>   `Read, Glob, Grep` tool list and the harness enforces it. Codex gives every
>   agent the same tools, so the absence of shell, build and test commands here
>   is a rule you keep, not a wall you will hit. Having `Bash` available is not
>   permission to use it in Validate.
> - **Report by returning your findings as your final message.** There is no
>   separate reply channel, and no artifact to write.

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
and approved-plan review findings using the persisted submission contract. For
contract 2 return complete reference-only {"contributions":[...]} terminal
findings or the supplied application result shape, using retained list-B evidence.
Already permitted live tools are optional; identical live/terminal contributions
reconcile without redundant submission, while conflicts reject atomically before
new terminal decisions. Contract 1 keeps versioned runner output. This does
not authorize shell commands, new checks or a wider specialist tool policy.
Classify blocking defects separately from advisory improvements; advisory findings
alone do not prevent acceptance. Include proof references and explicit unmet or
unverified outcomes. Never propose a waiver as authority. Missing evidence remains
unverified and failed evidence remains failed even after an owner exception.
Only the factory records decisions, ticks criteria, posts comments or closes
stories. Follow the workflow skill for owner exceptions, follow-up proposals,
all-attached-child readiness, historical acceptance and approved PR delivery.
Legacy scope retains its original workflow semantics; do not migrate it by prose.
