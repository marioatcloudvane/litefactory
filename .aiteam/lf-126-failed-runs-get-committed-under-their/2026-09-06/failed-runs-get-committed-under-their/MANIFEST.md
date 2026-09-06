# Manifest
# Branch: lf/126-failed-runs-get-committed-under-their
# Session: 2026-09-06

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-06 — session created
- 2026-09-06 — Research mode finished: RESEARCH_BRIEF.md written, root cause traced to two
  compounding bugs (unconditional commit-on-failure in `internal/turn/harvest.go` /
  `internal/git/commit.go`, and outcome-blind conversation resume in
  `internal/turn/prepare.go`).

## Notes

