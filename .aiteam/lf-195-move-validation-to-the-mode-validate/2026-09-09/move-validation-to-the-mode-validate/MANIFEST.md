# Manifest
# Branch: lf-195-move-validation-to-the-mode-validate
# Session: 2026-09-09

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(complete)*
- [x] **Plan** — `plan_brief.md` + managed JSON proposal *(complete; factory publication pending)*
- [ ] **Implement** — run 0004: US-001–US-007 implemented and verified; US-008 excluded; US-009 blocked pending Claude. Full feature instruction parity remains outstanding.
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-09 — session created for issue #195, research run 0001
- 2026-09-09 — Research completed for run 0001: documented lifecycle and evidence coupling, Validate admission/permission gaps, review instruction drift, owner exception/follow-up decisions, and parent delivery scope.
- 2026-09-09 — Research run 0002 completed: owner requested all open decisions; preserved the original findings and added 11 owner choices with recommended defaults, alternatives, and engineering decisions that need no owner answer.
- 2026-09-09 — Plan run 0003 completed: incorporated owner decisions, produced feature-spec.md and ARCH_REVIEW.md, and wrote shared plan_brief.md plus the ignored version-1 transport proposal for generation 1. Nine create stories carry 55 criterion-named evidence inventories; schema, required sections, dependency DAG, harness isolation and transport exclusion checks passed. Factory publication supplies issue identities and Story Index links. No per-story Markdown was created.

## Notes

Headless run: resolve ambiguity with documented assumptions; no confirmation required. Do not commit or push.

Run 0002 historical note: the owner's explicit request for open decisions authorized questions in the final report; recommendations were then unapproved.

Run 0003: the owner answered the decisions and authorized planning. Implement and Validate stay separate owner-triggered runs. Claude and Codex instruction changes are independent sibling stories requiring a manual harness switch; no story writes both protected trees. Later implementation changes are new issue work and do not automatically reopen acceptance. All attached children count toward normal readiness; explicit owner exceptions remain auditable. Implementation, GitHub publication and main integration have not occurred in this run.

- 2026-09-09 — Implement run 0004 finished feasible scope US-001–US-007 with independent per-story acceptance/review/tests and 36 atomic progress events (including invalidation before overlap and final renewal). US-008 explicitly excluded; US-009 requires Claude for `.codex/**`. Final `make check` passed with a temporary test-only 20-minute timeout wrapper; initial default 10-minute turn timeout retained in notes. Final pinned lint: 0 issues; full race suite passed; `go build ./...` passed. No commits, pushes, protected-tree edits or live forge actions.
