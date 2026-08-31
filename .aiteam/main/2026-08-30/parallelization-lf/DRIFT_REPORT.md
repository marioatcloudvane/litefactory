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

---

# Drift Report — parallelization-lf (run 3)
# Branch: lf/parallelization | Date: 2026-08-31

Run 3 covers **User Story 7 — "The record"** (T-PAR-41..46) and nothing else.
Runs 1 and 2 landed Stories 1-6; this run landed the written record of them.

## Delivered as planned

- **T-PAR-41** — `.aiteam/AC_AMENDMENTS.md`'s header widened to admit
  acceptance criteria, decisions, rulings and deferrals; "the AC id" generalised
  to "the identifier (`AC`, `D`, `P`, `SD`, …)"; the `Status` vocabulary extended
  with *ruled*, *amended*, *reviewed, unchanged* and *deferred*; the ***Applies
  to*** line added for entries that settle rather than amend. The rejected
  sibling `DECISION_AMENDMENTS.md` is recorded with the reason, naming the two
  earlier session-local ledgers that were specified and never written.
- **T-PAR-42** — 17 entries. AC-PAR-8.1 (the D-X ruling, with the record
  correction that D-X was never superseded), AC-PAR-8.2 (the AC-FC-2.6
  distinction, recorded as *not* an amendment), RD-6's no-`recover()` ruling
  against AC-3.3, and RD-12's AC-2.8 payoff with AC-2.9 recorded as reviewed and
  unchanged. Of the 21 identifiers AC-PAR-8.3 names: **16 amended** (AC-2.1,
  2.8, 2.12, 2.13, 2.17, 2.21, 3.1, 3.5, 3.8, 3.9, 3.10, 3.20, 3.22, D-A, D-B,
  P-3), **2 ruled** (D-X, AC-3.3), **5 reviewed and left standing** with reasons
  (AC-2.9, AC-3.2, D-7, SD-1, AC-FC-2.6). No identifier was left unaccounted for.
- **T-PAR-43** — pointer lines under the AC-2 and AC-3 tables in
  `lf-setup/IMPLEMENTATION_PLAN.md`, verbatim in form from the `:237` precedent.
- **T-PAR-44** — the RD-11 corrections. Four sites were already paid off in
  run 2 (`daemon/doc.go`, `turn/doc.go`, `daemon.go`'s goroutine inventory and
  its mutable-state paragraph, `tick.go`'s log line) and were verified rather
  than rewritten. The four outstanding ones landed: `turn/execute.go`'s log line,
  `git/clone.go`'s concurrency claim with the measured contention figures,
  `store/store.go`'s now-load-bearing queueing sentence, and `turn/prepare.go`'s
  `previousRun` justification restated against N in-flight sessions.
- **T-PAR-45** — both tests, in `internal/daemon/record_test.go`. Each was
  deliberately broken and observed to fail before being reverted and committed.
- **T-PAR-46** — the deferral register: the per-repo bound (with the one-extra-
  counter seam described against the real `Daemon.offer`), the
  `naming.WorktreePath` repo component, the LF-11 view, and the `runs` owner
  column recorded as *rejected* rather than deferred. `Daemon.Quiesce` is
  documented as the sole exported API the feature adds, production-used and not
  a test seam, and named as the LF-11 attachment point.
- **The Sequencing constraint was honoured** — T-PAR-41, 42 and 43 landed as one
  commit (`5c48b4d`). The ledger was never left widened-without-entries or
  entries-without-pointers.

## Added scope

- **A fifth pointer line**, under `lf-setup`'s Settled Decisions table. The plan
  specified pointers under the AC-2 and AC-3 tables only, but D-A, D-B and D-7
  were amended and D-X ruled on — all of which live in that table. Leaving it
  unpointed would have left the record in exactly the state the ledger's own
  rule forbids, which is what the "land 41/42/43 together" note exists to
  prevent. In-spirit, not a new direction.
- **A fifth RD-11 site: `tick.go`'s `TickResult.Repos` doc comment**, which
  claimed one listing per repository was "the whole per-tick API cost (AC-2.8)".
  Not in the RD-11 table, but false, and false about a criterion this very story
  amends. Corrected in `0c29ff1` under AC-PAR-8.4's plain wording.
- **CR-A-004 taken rather than filed** (`cfe14fb`). The review found the new
  `clone.go` paragraph merged two distinct measurements (37% printed the
  config-lock error, 25% lost the upstream) into one sentence, over-claiming a
  co-occurrence `SPIKE.md` explicitly says was never chased. An advisory would
  normally be recorded and deferred; it was fixed because a fresh overclaim in
  the story that exists to end overclaiming is the defect class itself.

## Dropped or deferred

- Nothing in T-PAR-41..46 was dropped.
- **AC-FC-2.6's missing test seam was recorded, not fixed.** Its subject ships as
  `daemon.ListingOverlap`, a bare exported const with no `daemon.Options`
  override — so the criterion names a constant that does not exist under that
  name (`config.go:63` still says `CursorLookback`) *and* the seam it promised
  was never built. Both predate this work and neither is in the RD-11 table, so
  the ledger entry records them honestly and the code was left alone. Fixing it
  would have been scope creep; it is written down in `notes.md` for a later pass.

## Architectural deviations

- **`lf-setup:58` got a pointer line, not a rewrite.** The RD-11 table said
  "reconcile both to the RD-10 text" for the two live goroutine inventories. The
  `APP_CONTEXT.md` bullet — a living document — carries the reconciled text and
  its previous hedge ("this paragraph may still disagree… T-PAR-44's status was
  not reverified") is gone. The `lf-setup` statement is in an **archived plan**,
  and both `CLAUDE.md` and the ledger's own header forbid editing an archive into
  agreement with the present: "an old plan that has been edited into agreement
  with the present is no longer evidence of anything." It therefore keeps its
  original sentence and gains a pointer naming the one canonical text, which now
  lives in the ledger. The plan's requirement — one text, not two disagreeing
  ones — is met; the mechanism is the repository's existing convention rather
  than the literal rewrite the table implied.
- **Two engineers on one user story**, split by disjoint file ownership (the
  `.aiteam/` record vs. the `internal/` doc comments) rather than the usual one
  engineer per story. Process, not product; it kept the 41→42→43 chain inside a
  single agent while the independent T-PAR-44 pass ran beside it.

## Three plan claims the tree contradicted

Recorded because the record is this story's subject, and because the engineers
corrected the prose rather than copying the plan:

1. **RD-9's secondary-limit probe has not shipped.** T-PAR-36 is known-open;
   `RateLimitError.Secondary` is logged and nothing more. The deferral entry says
   the plan *expresses* the dispatch-time ceiling, not that it exists.
2. **AC-FC-2.6's subject is `daemon.ListingOverlap`, not `CursorLookback`**, and
   it carries no `Options` override. See above.
3. **The shutdown join is `defer d.pool.wait()` in `loop` plus `close(done)` in
   `Run`'s wrapper goroutine**, not the two defers in `loop` the plan describes.
   Same effect; the ledger describes what is there.

## Verdict

**CLEAN** — delivered matches the plan. The three additions are all corrections
of the same class the story was written to make, found while making it, and the
one methodological deviation (`lf-setup:58` pointed at rather than rewritten)
follows the repository's own archive rule over the plan's looser wording.
