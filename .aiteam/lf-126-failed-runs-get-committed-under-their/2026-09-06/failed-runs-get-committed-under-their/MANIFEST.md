# Manifest
# Branch: lf/126-failed-runs-get-committed-under-their
# Session: 2026-09-06

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(done)*
- [x] **Implement** — code + tests *(done)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-06 — session created
- 2026-09-06 — Research mode finished: RESEARCH_BRIEF.md written, root cause traced to two
  compounding bugs (unconditional commit-on-failure in `internal/turn/harvest.go` /
  `internal/git/commit.go`, and outcome-blind conversation resume in
  `internal/turn/prepare.go`).
- 2026-09-06 — Plan mode finished (headless run; issue owner's answers to the
  brief's open questions were taken as product decisions, per the dispatch):
  `plan_brief.md` plus six per-story sub-issues (`US-1.md`–`US-6.md`) written to
  this session directory. Shipping order: US-6 independent; US-1+US-2 as one
  hard-coupled unit; US-3 after US-1/US-2; US-4 before US-5. Story Index rows
  are complete (ID/title/size/depends-on); the Sub-issue link column is filled
  in once litefactory creates each forge sub-issue from these bodies.
- 2026-09-07 — Implement mode finished (headless run, resumed once after a
  session rate-limit cutoff mid-Phase-1; verified worktree state via
  `git status`/`git log` before continuing rather than trusting the prior
  summary). All six stories (US-1–US-6) implemented, code-reviewed (verdict:
  CLEAN, no findings), and unit-tested. Test result: 709 passed / 3 failed on
  first pass (a report-rendering double-mention defect); bug loop ran 2
  cycles (BT-001, BT-002), both closed — final full-repo `go build ./...`,
  `go vet ./...`, `gofmt -l .`, `go test ./...` all green across every
  package. No integration tests needed (no story touches a real
  DB/HTTP/queue dependency). `DRIFT_REPORT.md` verdict: CLEAN.
  `APP_CONTEXT.md`/`APP_CONSTRAINTS.md` updated (update mode) with the new
  commit-gate invariant, the two distinct `Succeeded()` predicates, the
  resume corrective-note mechanism, and the report renderer's single-site
  worktree-naming convention. Work left uncommitted in the worktree per
  standing instructions — litefactory's own commit step handles publishing
  it.

## Notes

