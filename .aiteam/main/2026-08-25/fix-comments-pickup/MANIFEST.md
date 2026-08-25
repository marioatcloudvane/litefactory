# Manifest
# Branch: main
# Session: 2026-08-25

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — promoted from a spike, not a fresh Research pass)*
- [ ] **Plan** — `IMPLEMENTATION_PLAN.md` *(in progress)*
- [ ] **Implement** — code + tests *(not started)*

## History

- 2026-08-25 — session created
- 2026-08-25 — Spike: diagnosed two owner comments on `marioatcloudvane/litefactory#3`
  (ids 5409831806, 5409849898) that were never picked up. Root cause found at
  `internal/daemon/tick.go:99` — the poll cursor advances to the tick's start time with no
  overlap margin, so one lagged GitHub issue listing puts an update permanently behind
  `since`. Evidence: live daemon log, SQLite state, GitHub REST replay.
- 2026-08-25 — Spike promoted to Plan at the owner's request. Findings recorded as
  `RESEARCH_BRIEF.md`; six open questions carried forward.

## Notes

Scope: fix the comment-pickup hole in the daemon poll loop. Three candidate tiers are in the
brief — (1) cursor lookback margin, (2) poll known tasks by watermark instead of by listing,
(3) make a near-miss audible. **Tier 2 is the open architectural decision** and is the reason
the owner asked for a plan rather than a patch.

Not in scope unless the plan says otherwise: recovering the two lost comments, which is a
one-off operational rewind of `poll_cursor` on live state rather than a code change.

Constraint: the daemon is running in production against a real repo right now. Any change
lands under a live loop that is currently polling every 30s.
