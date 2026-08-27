# Manifest
# Branch: main
# Session: 2026-08-26

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-08-26 — session created; Research mode entered
- 2026-08-26 — Research complete; `RESEARCH_BRIEF.md` written. Premise refuted as
  stated (tools are narrowed, not blocked); `--tools` identified as the only real
  boundary; recommendation is tiered labels selecting operator-authored profiles,
  with a comment directive as per-run override. Ten open questions logged, one of
  which (`Task` vs `Agent` tool name) should be settled empirically before Plan.

- 2026-08-27 — owner decision on review: **Option A only**. Options B, C and D
  rejected outright (D explored in detail and dropped: GitHub sends no structured
  comment data, and a directive parsed out of a comment body fails silently).
  Addendum appended to `RESEARCH_BRIEF.md`. Research remains closed; OQ 1 still
  unanswered.

- 2026-08-27 — Plan complete; `IMPLEMENTATION_PLAN.md` written. 4 user stories,
  12 tasks. OQ 1 settled empirically from run logs: `Task` IS a live tool name
  (not stale, not renamed to `Agent`) — but `TodoWrite` is requested and silently
  dropped (8 requested, 7 resolved, no error), which becomes the worked example
  for requested-vs-resolved reporting. OQ 10 also closed (the forbidden-flag test
  exists at `internal/config/purity_test.go:84-91`). C7 clear — session-resume has
  landed, no sequencing blocker. `feature-spec.md` written by requirements-engineer;
  go-architect performed a four-lens review. Eight planning decisions recorded as
  D-1..D-8, including two judgment calls flagged for owner reversal: kept
  per-profile `allowed_tools` (rejecting the architect's cut), and label-present-
  but-no-profiles now REFUSES. `--setting-sources`/`--bare`/`dontAsk`/sandbox kept
  as a clearly-marked OPTIONAL section Implement may skip wholesale.

## Notes

Request: tools are currently blocked by default for Claude sessions in
litefactory. We want them enabled, but carefully — scoped per session, with the
permitted set declared from GitHub (labels/tags on the issue, or some other
GitHub-side signal) rather than hardcoded.
