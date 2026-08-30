# Manifest
# Branch: main
# Session: 2026-08-30

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done)*
- [ ] **Implement** — code + tests *(not started)*
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

## Notes

Feature: parallelize litefactory's turn execution.

Invariants to keep:
1. Only one daemon process (the $LF_HOME/daemon.lock flock singleton).
2. Runs for one issue stay strictly sequential and in comment order.

What should change:
3. Different repositories are polled/worked concurrently.
4. Different issues within the same repo run concurrently — a run on issue #7
   must not block a new issue #8 in the same repo from starting.
