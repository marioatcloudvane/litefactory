# Drift Report — parallelization-lf
# Branch: lf/parallelization | Date: 2026-08-31 (run 2)

Run 1 (2026-08-30) landed the plan's preparatory steps 1-9 and stopped at the
red-commit boundary. This report covers run 2, which crossed that boundary.

## Delivered as planned

- **T-PAR-5, 6, 7, 8, 9, 13, 15, 22** — the necessarily-red commit, landed as one
  unit exactly as the plan's Sequencing section required (`f565ba2`). `Tick`
  splits into discovery and dispatch; `processIssue` from `GetWatermark` onward
  moves onto a worker taking the daemon context; `TickResult` reshaped per RD-5
  with `pending` and its min-reduction deleted; the fixture settle primitive
  built over `Daemon.Quiesce` with no sleep and no polling; the existing suite
  reshaped onto it; the cursor made monotonic with the backlog union and a lazy
  `GetIssue` at selection; the in-flight set wired as the RD-3 unit of exclusion
  spanning execute *and* report; two-level round-robin as a second pass over a
  materialised eligible set.
- **T-PAR-35** — the backoff checked at two points, not one (`256a8d9`).
- **T-PAR-23** — the dispatch policy, its `⌈R/S⌉` guarantee and all four
  non-promises stated in `internal/daemon/doc.go` (`256a8d9`).
- **T-PAR-10, 14, 16, 17, 24, 25** — the concurrency acceptance criteria tests
  (`480bc47`).

## Added scope

- **A third clone-gate critical section** (`f0aa5a2`). Not in the plan, because
  the code it gates did not exist when the plan was written. The `origin/main`
  merge brought in `internal/turn/subissues.go`, whose `fileStoryIssues` called
  `Commit` and `Push` on the shared clone with no gate acquisition at all. The
  package's own structural test caught it. Extends T-PAR-28..31 rather than
  adding a task; `commitStoryIndex` is now CS3 and cannot fold into CS2 because
  the `CreateSubIssues` forge round-trip sits between them.
- **`TestCriticalSectionsAreSingleAcquisitions` generalised** from two hardcoded
  sections to a `sections` + `gated` registry with a package-wide acquisition
  count. Necessary consequence of the above; keeps its teeth, verified by
  mutation.
- **CR-A-003 taken rather than deferred** (`256a8d9`). The repo rotation fell back
  to index 0 when the last-dispatched repo dropped out of the eligible set. Taken
  because the same commit wrote the `⌈R/S⌉` bound into `doc.go`, and the fallback
  made that promise false under repository churn. The added sort by repo id also
  makes AC-PAR-2.3 structural rather than incidental.
- **`TestBackoffTouchesNoContext`** — a structural test that `backoff.go` never
  imports `context`, pinning AC-PAR-4.3 (the backoff cancels nothing already
  running). The plan asked for the property, not for a structural test of it.
- **`pool.try`'s doc comment corrected.** Its claim that a `true` return which
  never reaches a `go` leaks a slot stopped being true when `offer` gained its
  materialise-failure undo.

## Dropped or deferred

Not attempted this run, all still open:

- **T-PAR-21** — tests for AC-PAR-6.1..6.4, 1.8, 1.9 (the config surface and the
  high-water instrumentation).
- **T-PAR-33** (daemon-driven part) — the AC-PAR-3.x tests driven through the
  daemon rather than through `turn.Execute` directly.
- **T-PAR-36** — the secondary-rate-limit single-slot probe.
- **T-PAR-37, 38** — remaining AC-PAR-4.x tests, and shutdown with N in flight
  (AC-PAR-4.6). AC-PAR-4.2, 4.3 and 4.4 do now have tests; 4.1 is structural and
  confirmed by review; 4.5 is covered.
- **T-PAR-41..46** — the whole of User Story 7, the written record: widening
  `.aiteam/AC_AMENDMENTS.md` to admit decisions, the D-X ruling entry, the
  per-criterion amendments, the pointer lines, the remaining RD-11 corrections
  outside `internal/daemon`, and the deferral register. `internal/daemon/doc.go`
  and `internal/turn/doc.go` were corrected in place as their code changed, so
  part of T-PAR-44 is paid off; the ledger work is untouched.

## Architectural deviations

None. Every architectural ruling the plan made was followed: no `errgroup`, no
queue, no second `recover()`, no `runs` owner column, no worker component on the
instance id, no mutex on `TickResult`, workers on the daemon context, `d.now()`
never read below `Tick`, and the clone gate injected rather than owned by
`internal/turn`.

One micro-decision worth recording because it reads like a deviation and is not:
`Daemon.offer` calls `pool.done()` directly on the Tick goroutine when
`materialise` fails, rather than from a worker's defer. It is the exact undo of
`pool.try()`, sound because `Add` and `Done` both happen on the Tick goroutine
and `wait` runs only after the last `Tick` has returned, and it was reviewed
explicitly.

## Verdict

**CLEAN** — delivered matches the plan. The one substantial addition (CS3) was
forced by code that arrived from `origin/main` after the plan was written, and it
extends an existing task rather than inventing a new direction.
