# Manifest
# Branch: lf/187-a-finished-turn-can-stay-in-the-in
# Session: 2026-09-09

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + generation-1 managed GitHub stories #192–#194 *(factory-selected frozen revisions supplied in run 0005)*
- [x] **Implement** — code + tests + `CODE_REVIEW.md` + `DRIFT_REPORT.md` *(done; scoped and aggregate gates passed)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-09 — session created; Research requested for issue #187.
- 2026-09-09 — Research completed: audited all five admission sites; no missing release located in current source. Documented finish-message/count ambiguities, unsafe lock-only reconciliation, diagnostic gaps, and unresolved incident evidence in `RESEARCH_BRIEF.md`. No implementation or test execution.

- 2026-09-09 — Run 0002 Research completed: applied the owner’s withdrawal of the leak diagnosis; identified unconditional managed-parent registry discovery as the concrete source-compatible explanation for repeated no-run workers. Rewrote `RESEARCH_BRIEF.md` around polling cost, admission diagnostics and metric semantics; production parent identities and deployed revision equivalence remain unverified. No implementation or test execution.

- 2026-09-09 — Run 0003 Research completed: specified a minimal observability recommendation with deduplicated enrollment-source attribution, complete worker identity, cleanup-aligned DEBUG completion, additive worker-summary terminology and reuse of existing INFO run-preparation events. Distinguished zero execution requests from no useful action; retained current polling and the unresolved production identities/revision match. Updated `RESEARCH_BRIEF.md`; Plan and Implement remain unopened. No production code changes or test execution.

- 2026-09-09 — Run 0004 Plan completed: requirements and architect review produced three dependent observability stories; authored shared `plan_brief.md` and root version-1 managed proposal, with eight exact-identifier acceptance/evidence mappings. Preserved polling and refined terminal logging to precede final pool join. Factory publication will populate the link-only Story Index. No per-story Markdown, implementation, commits or pushes.

- 2026-09-09 — Run 0005 Implement completed US-1, US-2 and US-3 from the supplied frozen revisions. All eight acceptance criteria passed with independent scoped reviews; final aggregate review and drift verdict CLEAN. Published nine immutable progress events, including invalidation before shared-file edits and renewed verification; final verified events are US-1 `000007.json` (attempt 3), US-2 `000008.json` (attempt 2), US-3 `000009.json` (attempt 1). Final composed daemon race suite passed (26.194s, 50.4% coverage); `make check` passed format, vet, pinned lint (0 issues) and all repository race tests, with no remaining failures. See `notes.md` and `MAKE_CHECK.txt` for evidence bounds and summarized sandbox lint-cache warnings. No commits, pushes or runtime modifications.

## Notes

- Headless run: proceed without questions or confirmation; no commits or pushes.
