# Implement notes — parallelization-lf
# Branch: lf/parallelization | Date: 2026-08-30

This run implemented the plan's "land these first, green, to shrink the red
commit" sequence (steps 1-9) and stopped at the red-commit boundary, by the
owner's instruction, before T-PAR-9.

## Stop point and why

T-PAR-9 re-shapes ~50% of `internal/daemon/tick_test.go`. A live litefactory
daemon (pid 17972) has an in-flight `claude` run on issue #7, whose branch has
already modified `internal/daemon/tick_test.go`, `internal/turn/classify.go`,
`internal/turn/report.go`, `internal/naming/aiteam.go` and
`internal/naming/errors.go`. T-PAR-9 collides head-on with the first of those.
The owner asked to decide at that point rather than resolve a conflict blindly.

**No file in the #7 list was edited by this run.** Two tasks came close and were
steered around it rather than through it:

- **T-PAR-1** was designed so the fix stays inside `fixture_test.go`. The new
  `syncBuffer` keeps the field named `logs` and satisfies `fmt.Stringer`, so all
  20 existing call sites — 13 of them in `tick_test.go` — kept compiling
  untouched.
- **T-PAR-34** was the flagged risk: the plan warned it "alters two existing
  sequential assertions". Both turned out to live in `backoff_internal_test.go`.
  `tick_test.go`'s `TestRateLimitBacksOffAndResumes` still passes unmodified,
  because it rate-limits `ListIssues` — the listing path, which still runs on
  the Tick goroutine and still ends the tick.

## Code Review Advisories

`CODE_REVIEW.md` — verdict **ADVISORY_ONLY**, 0 BLOCKING, 2 ADVISORY.

- **CR-A-001 — `markBacklog`'s call site must move when dispatch splits from
  discovery.** `internal/daemon/tick.go:386`. Marking every *discovered* task is
  correct only because discovery and dispatch are the same event today. Once
  T-PAR-5/6/22 split them, the mark must also cover the "eligible but the pool
  was full" case, per RD-2's protocol. Forward-looking guidance for the red
  commit, not a defect in what is committed.
- **CR-A-002 — a standing backoff cannot self-clear with zero repos
  registered.** `tick.go` `poll` / `backoff.go` `clear`. `clear()` fires from
  inside the repo loop on the first 200 listing, so an empty loop gives it no
  evidence-of-success event; the hold lapses on its own `until` instead.
  Surfaced by the implementing engineer, assessed by the reviewer as benign —
  with no repos there is no traffic the backoff protects.

## Decisions worth remembering

- **`max_concurrent_runs` default 3, range 1..16.** The 3 is a conservative
  placeholder, **not a measurement** — sizing probe P5 was never run and
  `SPIKE.md` says so. Recorded as such in the struct doc comment, the shipped
  template and the commit message. Because it is a config key, revising it after
  a real-hardware measurement is an operator edit, not a code change.
- **A nil `CloneGate` is refused, not degraded to a no-op** (`Options.validate`).
  A silent un-serialisation of a path measured at 75% (`fetch` against moved
  refs) and 0.75% (`worktree add`) would look healthy for a long time and then
  post fatal git errors that read as flakes. One production wiring point, so the
  requirement costs one line. `internal/turn`'s fixture supplies a *real*
  serialising gate, so every existing turn test now runs the production shape.
- **`pool.wait()` is unconditional and must stay that way.** At the only moment
  it is called, the loop's context is already cancelled — that is *why* `loop` is
  returning — so a `select` on `ctx.Done()` would return instantly and abandon
  every worker while still reading like a join. Documented in place.
- **T-PAR-12's clear is a statement, not a `defer`.** A `defer` would clear on
  the error paths too, which is exactly the stale-*false* RD-2 forbids. This
  required turning the tail of `processIssue` into a branch plus one join.
- **T-PAR-40 landed ahead of its stated dependency (T-PAR-6), deliberately**, so
  the clock check is a guardrail for the Tick split rather than an audit after
  it. It also rejects `d.now` inside any function literal, including one written
  inside `Tick` — the `go func() { ... d.now() ... }()` shape T-PAR-5/6 invites.

## Scope added beyond the plan

- **T-PAR-26 checks `branch.<n>.merge` as well as `branch.<n>.remote`.**
  AC-PAR-3.4 names only `.remote`. Measurement showed `git worktree add -b
  <branch> origin/<base>` already writes `.remote = origin` via
  `branch.autoSetupMerge`, so a remote-only check would have been close to
  decorative: a push whose config write was lost would still pass it while the
  branch tracked the *base*. A strict superset of the criterion, justified in the
  doc comment.
- **`daemon -> repos`** is a new (cycle-free) dependency arrow, from T-PAR-32's
  startup collision check reusing the pure decision function in `internal/repos`.
- **`internal/cli/cmd_daemon.go`** gained a `concurrency` line in the boot
  report, on the grounds that `Startup` exists to be printed.

## Process note

Multiple engineers shared one working tree. One agent's whole-index commit swept
another's staged files in; it was caught, reset and recommitted cleanly, and
every later agent was instructed to stage by explicit pathspec and commit with
`--only` in a single atomic invocation. Agents verified their own work in
`git archive HEAD | tar -x` scratch exports whenever a sibling's in-flight edits
made the shared tree red. Worth keeping if parallel engineers are used again.

## Not done (all remaining work)

The necessarily-red commit **T-PAR-5, 6, 7, 8, 9, 13, 15, 22** and everything
downstream of it: T-PAR-10, 14, 16, 17, 21, 23, 24, 25, 33 (the daemon-driven
part), 35, 36, 37, 38, 41-46.
