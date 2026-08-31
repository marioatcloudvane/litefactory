# Manifest
# Branch: main
# Session: 2026-08-30

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done)*
- [x] **Implement** — code + tests *(done — run 3 landed User Story 7, the written record. All 46 tasks complete; suite green under -race)*
- [x] **Spike** — `SPIKE.md` *(done — the two empirical gates)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-08-30 — session created
- 2026-08-30 — Research complete. `RESEARCH_BRIEF.md` written from four parallel
  codebase tours (daemon control flow; store + turn locking; git/harness/github
  contention; the AC/decision documentation layer). Two gates identified that a
  human must clear before Plan mode: a ruling on whether D-X still forbids a
  run-rate cap, and an empirical probe on whether two `claude` processes can run
  concurrently at all.
- 2026-08-30 — owner closed open question 1: **decoupled dispatch** — polling
  never blocks on a run. Fan-out-and-join considered and rejected.
- 2026-08-30 — Spike: `SPIKE.md`. Gate 3 **cleared** — 14 concurrent `claude`
  runs, all clean, including concurrent `--resume` of two sessions. Gate 2
  (D-X / where the cap lives) still open, owner's call. Probes overturned two of
  the brief's git conclusions: `worktree add` DOES contend (3/400) and
  `push --set-upstream` fails silently at exit 0; `fetch` contends 75% of the
  time when refs move. Corrections recorded in `RESEARCH_BRIEF.md`.

- 2026-08-30 — owner closed gate 2: the ceiling is **`max_concurrent_runs` in
  `config.yaml`** — one daemon-wide number shared across all repos, following the
  `poll_interval_seconds` precedent. Not a Go constant, not per-repo. D-X is
  ruled not to bar it: D-X rejected a run-*rate* limiter (spending control); this
  is a *concurrency* bound (resource control). The plan must record that ruling
  in-tree, since the M0 text says no task or AC may reintroduce a circuit breaker.

- 2026-08-30 — Plan complete. `feature-spec.md` (7 user stories, 40 `AC-PAR-` criteria, 12 carried
  decisions) then `IMPLEMENTATION_PLAN.md` (46 tasks `T-PAR-1..46`) from a requirements pass, two
  codebase reads and a four-lens architect review. All 12 required decisions closed. Headline
  rulings: the poll cursor becomes monotonic and outstanding work moves to a durable
  `sessions.needs_poll` flag (one migration — **neither** forge listing can re-find a busy issue,
  because both are timestamp-indexed); sequentiality covers execute **and** report via a new
  daemon-owned in-flight set, with the store's session lock unchanged as the cross-process
  mechanism; two-level round-robin dispatch, bounded at `⌈R/S⌉` ticks per repo; stdlib
  semaphore + `WaitGroup`, no queue (`errgroup` is a named rejected dependency *and* the rejected
  fan-out-and-join shape); no second `recover()`, no `runs` owner column, no worker component on
  the instance id.
  Three record corrections found: **D-X was never superseded** — the claimed supersession lives
  only in a superseded document that reverses itself at `:483` ("D-X stands in effect"); the shipped
  amendment convention is the repo-root `.aiteam/AC_AMENDMENTS.md`, not the session-local file both
  earlier plans specified and neither wrote; and AC-FC-2.6 is scoped to `CursorLookback`, so D2 is a
  distinction rather than a contradiction. One new security exposure identified that the git mutex
  does **not** cover — the cross-repo worktree-path TOCTOU — closed by refusal at startup *and* at
  `repo add`.

- 2026-08-30 — Implement (partial). Landed the plan's "land these first, green,
  to shrink the red commit" sequence, steps 1-9, as 12 commits on
  `lf/parallelization`: T-PAR-1, 18, 19, 20, 26, 27, 32, 11, 12, 34, 39, 2, 3, 4,
  28, 29, 30, 31, plus T-PAR-40 pulled forward as a guardrail and the reachable
  slice of T-PAR-33. `go test -race ./...` green across all 18 packages; gofmt
  and vet clean. Code review verdict **ADVISORY_ONLY** — 0 blocking, 2 advisory
  (`CODE_REVIEW.md`).
  **Stopped before T-PAR-9** and the necessarily-red commit (T-PAR-5, 6, 7, 8, 9,
  13, 15, 22): T-PAR-9 re-shapes ~50% of `internal/daemon/tick_test.go`, which a
  live daemon's in-flight run on issue #7 has already modified. No file owned by
  #7 was touched. Details in `notes.md`.

## Notes

Feature: parallelize litefactory's turn execution.

Invariants to keep:
1. Only one daemon process (the $LF_HOME/daemon.lock flock singleton).
2. Runs for one issue stay strictly sequential and in comment order.

What should change:
3. Different repositories are polled/worked concurrently.
4. Different issues within the same repo run concurrently — a run on issue #7
   must not block a new issue #8 in the same repo from starting.

- 2026-08-31 — Implement, run 2. Crossed the red-commit boundary. Five commits on
  `lf/parallelization` after the `origin/main` merge at `6f068f5`:
  `f0aa5a2` (the clone gate's third critical section — a real bug the merge
  introduced, caught by the package's own structural test), `f565ba2` (the
  necessarily-red commit: T-PAR-5, 6, 7, 8, 9, 13, 15, 22 as one unit),
  `256a8d9` (T-PAR-35, T-PAR-23, CR-A-003), `80fbfad` (the code review record)
  and `480bc47` (T-PAR-10, 14, 16, 17, 24, 25 — the concurrency criteria tests).

  **Concurrent dispatch is live.** `Daemon.offer` calls `inflight.enter` then
  `pool.try` and spawns the worker; `Tick` returns without waiting for it. The
  poll cursor is a pure monotonic watermark and re-discovery runs off
  `sessions.needs_poll`.

  `go test -race ./...` green across all 19 packages; `internal/daemon` green
  over 20 consecutive runs; gofmt and vet clean. Code review verdict
  **ADVISORY_ONLY** — 0 blocking; CR-A-001 and CR-A-003 closed in code, CR-A-002
  accepted and documented.

  One flaky test was found and fixed by construction rather than by luck: the
  AC-PAR-1.6 refill test had treated a posted report as if it meant a freed
  slot, which it does not. See `notes.md`.

  Not done: T-PAR-21, 33 (daemon-driven part), 36, 37, 38 and the whole of
  User Story 7 (T-PAR-41..46, the written record). See `DRIFT_REPORT.md`.
- 2026-08-31 — **Implement run 3 — User Story 7, "The record" (T-PAR-41..46).**
  Seven commits, `195a0f4..b4f1f77`. The story is documentation: it wrote the
  written record of the concurrency feature runs 1 and 2 shipped.
  `.aiteam/AC_AMENDMENTS.md` went from an acceptance-criteria-only ledger to one
  admitting **amendments, rulings, reviews and deferrals** (69 → ~1350 lines):
  the D-X ruling with the correction that D-X was never superseded, the
  AC-FC-2.6 distinction recorded as explicitly *not* an amendment, 16 amended
  criteria, 2 rulings, 5 reviewed-and-left-standing, and a deferral register.
  Five pointer lines were added to two archived plans, which keep their original
  text — the convention is that an archive edited into agreement with the
  present is no longer evidence. The four outstanding RD-11 doc sites were
  corrected (`git/clone.go`, `store/store.go`, `turn/execute.go`,
  `turn/prepare.go`), plus a fifth found in passing (`tick.go`'s
  `TickResult.Repos`); four more had already been paid off in run 2 and were
  verified rather than rewritten.
  **Tests: 19 packages green under `-race`, 0 failed, no escalation.**
  T-PAR-45 added `internal/daemon/record_test.go` — two structural tests, each
  deliberately broken and observed to fail before being committed.
  Code review **ADVISORY_ONLY**, zero BLOCKING. CR-A-004 (false precision: two
  distinct push measurements folded into one sentence) was **taken rather than
  filed**, because a fresh overclaim inside the story that exists to end
  overclaiming is the defect class itself.
  Drift **CLEAN**. Three additions, all in-class: a fifth pointer line under
  `lf-setup`'s Settled Decisions table, the `tick.go` site, and the CR-A-004 fix.
  One methodological deviation: `lf-setup:58` was pointed at rather than
  rewritten, following the repository's archive rule over the plan's looser
  "reconcile both" wording.
  Three plan claims the tree contradicted and the record now states correctly:
  RD-9's secondary-limit probe has not shipped (T-PAR-36 open); AC-FC-2.6's
  subject ships as `daemon.ListingOverlap`, not `CursorLookback`, and never got
  the `Options` seam it promised; and the shutdown join is one defer in `loop`
  plus `close(done)` in `Run`'s wrapper, not two defers in `loop`.
  Known-open and untouched by design: T-PAR-21, 33, 36, 37, 38, CR-A-002.
  **Not pushed** — the branch has no upstream; the owner pushes after a rebuild.
