# Manifest
# Branch: lf/207-acceptance-criteria-should-be-structured
# Session: 2026-09-09

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Validate** — read-only retained-evidence findings *(not started; no artifact or manifest update from Validate)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-09 — session created; Research requested by run 0001 for issue #207.
- 2026-09-09 — Research completed for run 0001: structured acceptance criteria and factory-owned story-progress envelopes investigated; repository findings, incident provenance, compatibility risks, and Plan decisions recorded in `RESEARCH_BRIEF.md`. No confirmation pause per headless instruction.

- 2026-09-09 — Research completed for run 0002: retained run 0001 findings as carried forward; reverified serial first-error behavior, live ingestion versus missing author feedback, both runner channels and same-run correction semantics. Added preflight/receipt boundaries, aggregated diagnostics, lifecycle risks and provenance limits (two diagnosed rejections; run 14 pending). Updated `RESEARCH_BRIEF.md`; no implementation or tests, confirmation pause, commit or push.

- 2026-09-10 — Research completed for run 0003: reused this session; consolidated all open questions into 7 owner-facing choices and 7 Plan/architecture decisions with explicitly unapproved defaults and tradeoffs. Preserved earlier research as carried-forward evidence, distinguished settled asks from questions, and added current legacy/separated workflow and instruction-ownership constraints. Historical run 14 remains unknown and is not a planning prerequisite. No implementation, tests, operational access, confirmation wait, commit or push.

- 2026-09-10 — Research completed for run 0004: recorded owner decisions, expanded scope to all five story sections, preserved free GitHub editing and explicit CLI-only legacy upgrades, and saved an illustrative typed story/submission/receipt sketch with equivalent evidence alternatives, proposed IDs and reconciliation limits. No production changes, runtime edits, tests, approval wait, commit or push.

## Notes

- Headless run: never wait for answers or confirmation. Run 0004 requests a full structured contract sketch, evidence-format examples and suggested IDs before moving on. Recorded owner decisions supersede earlier defaults; the sketch remains proposed for owner validation. No automatic mode transition or confirmation wait.
