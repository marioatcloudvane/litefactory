# Manifest
# Branch: main
# Session: 2026-08-26

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `IMPLEMENTATION_PLAN.md` *(not started)*
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

## Notes

Request: tools are currently blocked by default for Claude sessions in
litefactory. We want them enabled, but carefully — scoped per session, with the
permitted set declared from GitHub (labels/tags on the issue, or some other
GitHub-side signal) rather than hardcoded.
