# Manifest
# Branch: main
# Session: 2026-08-25

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — promoted from a spike, not a fresh Research pass)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done — six user stories; two of them gated on owner decisions)*
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
- 2026-08-25 — Plan written: `IMPLEMENTATION_PLAN.md`. Six user stories. Tier 1 (S2) and tier 2
  (S3) are both planned in full and the plan **does not pick between them** — that is OD-1, left
  to the owner. S1 (the regression test that fails today), S4 (tier 3, the near-miss WARN) and S5
  (the amended AC-2.8/AC-2.9 wording) ship whichever way OD-1 goes. S6 (recovery of the two lost
  comments) is planned but gated on OD-5 and deliberately not performed.
  Citation re-verification against `acbe751` corrected three of the brief's `file:line` references:
  `github/comments.go:60` → `:76`; `turn/classify.go:207` → `selfReason` at `:225` and `Admit` at
  `:268`; `daemon/tick.go:511` → `leave()` at `:470-485` (`:511` is `observe`). The claims
  themselves all held.
  OQ-6 is **answered and closable**: a survey found one further instance of the same knife-edge —
  `bootstrap` (`daemon/tick.go:225-251`) plants a watermark on the strength of a listing that could
  be stale, with no deferral escape hatch. Neither tier closes it; logged as a known open residual.
  No Go source or test was modified, and no live state was touched.

## Notes

Scope: fix the comment-pickup hole in the daemon poll loop. Three candidate tiers are in the
brief — (1) cursor lookback margin, (2) poll known tasks by watermark instead of by listing,
(3) make a near-miss audible. **Tier 2 is the open architectural decision** and is the reason
the owner asked for a plan rather than a patch.

Not in scope unless the plan says otherwise: recovering the two lost comments, which is a
one-off operational rewind of `poll_cursor` on live state rather than a code change.

Constraint: the daemon is running in production against a real repo right now. Any change
lands under a live loop that is currently polling every 30s.
