# Code Review
# Feature: parallelization-lf
# Reviewed by: go-code-reviewer
# Date: 2026-08-30
# Status: ADVISORY_ONLY

---

## Scope

Eleven commits, `1d2890c..HEAD`, covering T-PAR-1, 2, 3, 4, 11, 12, 18, 19, 20,
26, 27, 28, 29, 30, 31, 32, 34, 39, 40 — the "land these first, green" set plus
the clone-gate story, per the plan's Sequencing table. Per the task brief, the
"necessarily-red commit" (T-PAR-5, 6, 7, 8, 9, 13, 15, 22) has deliberately not
landed, and its absence (synchronous `Tick`, unused `pool`/`inflight` call
sites, unread `ListBacklogSessions`, un-split `TickResult`) is treated as
expected, not as a finding.

Files read: `internal/daemon/{gate,pool,inflight,daemon,tick,backoff,setup}.go`
and their internal test files (`gate_internal_test.go`,
`pool_internal_test.go`, `inflight_internal_test.go`, `clock_test.go`,
`logging_test.go`, `backlog_test.go`, `backoff_internal_test.go`,
`setup_test.go`, `fixture_test.go`); `internal/turn/{execute,prepare,harvest,
doc}.go` and `gate_test.go`, `fixture_test.go`; `internal/git/{push,errors}.go`
and `push_test.go`; `internal/store/sessions.go`,
`migrations/0002_session_backlog.sql`, `store_test.go`;
`internal/repos/{collision,doc}.go`, `collision_test.go`;
`internal/cli/{cmd_daemon,cmd_repo}.go`, `cmd_repo_test.go`;
`internal/config/{config,validate,template}.go` and their `_test.go`
counterparts. `go.mod`/`go.sum` diffed (unchanged).

Verification performed beyond static reading: `gofmt -l` (clean), `go vet
./internal/...` (clean), `go test -race ./...` (all packages green, including
the new `-race` fixtures in `internal/daemon` and `internal/turn`).

---

## BLOCKING findings

None.

---

## ADVISORY findings

### CR-A-001: `markBacklog`'s call site will need to move when dispatch splits from discovery

**File:** `internal/daemon/tick.go:386` (`processIssue`, the `d.markBacklog(ctx, t)` call)
**Story:** US-1 / US-2 boundary (T-PAR-12 today; relevant to T-PAR-5/6/13/22 next)
**Category:** Forward-looking hazard, not a defect in what is committed

**Finding:** Today `markBacklog` is called for every task `processIssue`
reaches, immediately after the task struct is built and before the watermark
lookup. That is correct *now* because `Tick` is still fully synchronous:
"discovered" and "dispatched" are the same event, so marking every discovered
task is marking every dispatched one, exactly as RD-2's protocol asks.

Once `Tick` splits into a discovery pass and a second, slot-bounded dispatch
pass (T-PAR-5/T-PAR-22), "discovered" and "dispatched" stop being the same
event: a session can be eligible, found by discovery, and then turned away by
`pool.try()` returning false because the pool is full. The plan's own pool
comment is explicit that this is not an error case to special-case away — "a
dispatch either starts now or its session is left needing a poll and
reconsidered from scratch, against fresh state, on the next tick" (`pool.go:17`).
Read literally, that means a session denied a slot for lack of capacity must
also get `needs_poll` set, not only a session that was actually handed to a
worker — otherwise a session parked behind three long-running turns, in a tick
where the cursor has already advanced past it, could go unlisted until
something else nudges it.

**Suggested fix (for T-PAR-5/6/13/22, not for this cycle):** when the call site
moves, make sure it covers both branches of the dispatch decision — the
session that got a slot and the session that was deferred for lack of one —
and add a test analogous to `TestAnUnconsumedCommentLeavesTheBacklogFlagSet`
for "eligible but the pool was full at the moment of dispatch." Worth a
one-line note in the `markBacklog` doc comment now, since the comment already
anticipates T-PAR-13 giving the flag its consumer but doesn't yet anticipate
the pool-full case.

---

### CR-A-002: A held rate-limit backoff cannot self-clear while zero repositories are registered

**File:** `internal/daemon/tick.go:179-215` (`poll`), `internal/daemon/backoff.go:105` (`clear`)
**Story:** US-1 (T-PAR-34)
**Category:** Edge case, already flagged by the implementing engineer

**Finding:** `backoff.clear()` fires only from inside the `for _, repo := range
repos` loop in `poll`, on the first listing that returns `200` (`cleared`
guards it to once per tick). With zero registered repositories the loop body
never executes, so a standing backoff hold has no evidence-of-success event to
clear it on, and it would sit until its own `until` timestamp lapses via
`hold`'s check rather than being cleared early.

Assessed as harmless: with zero repos there is also no forge traffic and
therefore nothing the backoff is protecting against being sent while held, and
the moment a repo is (re-)registered its first successful listing clears it
normally. This is a real edge of the design, correctly identified during
implementation, but not one that produces an incorrect outcome — the
degenerate case can only make an already-idle daemon wait slightly longer
before the hold lapses on its own, which costs nothing.

**Suggested fix:** none required. If it is worth doing anything, a one-line
comment on `poll`'s `cleared` variable or on `backoff.clear`'s doc noting "a
tick over zero repositories cannot clear a standing hold; harmless because
zero repositories also means zero backoff-relevant traffic" would save the
next reader from re-deriving this.

---

## Cross-story summary

The clone-gate story (US-6) is the one the plan itself calls the riskiest part
of the feature, and it is the strongest part of what was committed. The gate
(`internal/daemon/gate.go`) is exactly the 1-buffered-channel-per-clone shape
the plan mandates, acquired via `select` on `ctx.Done()`, never a
`sync.Mutex`; the registry's own `sync.Mutex` is provably held only across the
map lookup/insert (`gateFor`) and never across an acquisition or a git
command. CS1 (`prepareClone`: fetch → freezeSlug → ensureWorktree) and CS2
(`commitAndPush`: Commit → Push → upstream verification) are each a single
`Acquire`/`defer release()` pair, and this is enforced structurally, not just
by convention — `internal/turn/gate_test.go`'s
`TestCriticalSectionsAreSingleAcquisitions` walks the AST and fails if any of
`fetch`/`freezeSlug`/`ensureWorktree` or `Commit`/`Push` is called from outside
its designated section, or if either section acquires more than once or
releases outside a `defer`. The claude run sits between the two sections and
is proven to overlap across siblings by `TestClaudeRunsOutsideTheCloneGate`,
and a 2-repo × 3-issue concurrent `-race` fixture
(`TestConcurrentTurnsAgainstTwoClones`, AC-PAR-3.5) asserts none of the three
measured failure strings (`cannot lock ref`, `failed to read
.git/worktrees/`, `could not lock config file`) appear in either the results
or the logs. `internal/git` gained no mutex, no registry, and no method on
`Clone` — the gate lives entirely in `internal/daemon`, consumed through an
interface declared in `internal/turn`, exactly as RD's ownership split
requires.

The `needs_poll` protocol (US-2, RD-2/RD-4) is equally solid: the write
happens before any work begins, the single clear site is a statement on the
success path rather than a `defer`, and every early-return path in
`processIssue`/`bootstrap`/`drain`/`run` was checked by hand against the
stale-true-safe/stale-false-impossible asymmetry and found consistent, then
independently confirmed by `backlog_test.go`'s four scenarios (failed
bootstrap, unconsumed comment, locked disposition, and the two clean paths).
The one edge this reviewer went looking for beyond the plan's own list — what
happens to the flag when the daemon-owed listing for `ListBacklogSessions`
itself fails — doesn't yet arise, because nothing reads that method in this
phase.

The backoff idempotence work (T-PAR-34/35) reads correctly against
AC-PAR-4.2/4.3/4.4: `fail` is idempotent inside a standing hold, `clear` moved
to evidence-of-success rather than tick completion, and `rateLimited` is
documented and structured so it is safe to call from any future goroutine
(mutex-guarded `backoff`, no `CancelFunc` stored anywhere in it). The one
genuine edge case in this area (CR-A-002) was already surfaced by the
implementing engineer and is correctly assessed as benign.

Config plumbing for `max_concurrent_runs` (US-4) hits all five sites and all
three clamp/validation layers the plan calls for, the template's positional
`Sprintf` arguments line up correctly against the new `%d` placeholders (this
reviewer traced every argument against every verb in `configHeader` by hand),
and `go.mod` is untouched — still exactly three direct requires. No `"worker"`
log key was introduced anywhere in the diff. `gofmt -l`, `go vet ./internal/...`,
and `go test -race ./...` are all clean against the full tree, not just the
touched packages.

No BLOCKING cross-story boundary issues were found. The two ADVISORY items
above are forward-looking notes for the still-pending red commit and a
documented, low-consequence edge case — both explicitly invited by the review
brief rather than defects in what was actually committed.

---

## Verdict

ADVISORY_ONLY — 2 advisory notes recorded (one forward-looking hazard for
T-PAR-5/6/13/22, one documented benign edge case in T-PAR-34). Proceeding to
environment setup.

---
---

# Code Review — Cycle 2 (the red commit)
# Feature: parallelization-lf
# Reviewed by: go-code-reviewer
# Date: 2026-08-31
# Status: ADVISORY_ONLY

---

## Scope

Two commits: `f0aa5a2` (turn: the Story Index rewrite becomes the turn's third
critical section, T-PAR-28..31) and `f565ba2` (daemon: `Tick` dispatches and
stops waiting — the plan's deliberately-red commit, T-PAR-5,6,7,8,9,13,15,22).

Files read in full: `internal/daemon/tick.go` (all 1533 lines),
`internal/daemon/daemon.go`, `internal/daemon/pool.go`,
`internal/daemon/inflight.go`, `internal/daemon/backoff.go`,
`internal/daemon/doc.go`, `internal/daemon/tools.go`; `internal/turn/subissues.go`,
`internal/turn/gate_test.go`, `internal/turn/execute.go` (diff only — comment-only
change); `internal/store/sessions.go` (diff only — comment-only change).
Diffed and spot-checked for weakened assertions: `internal/daemon/{ask,tools,
backlog,daemon,tick,logging,fixture}_test.go`. `internal/store/repos.go` and
`internal/github/issues.go` read to check struct shape for cross-goroutine
aliasing. `IMPLEMENTATION_PLAN.md` re-read in full for the Architecture Notes of
US-1, US-2, US-3, US-5, US-6, US-7.

Verification performed: `go build ./...` (clean); `go test -race
./internal/turn/... ./internal/daemon/... ./internal/store/...` (all green).

This cycle focuses on the eight review items named in the task brief —
concurrency correctness of `Daemon.offer`, the `TickResult` single-writer
claim, worker context propagation, cross-goroutine data sharing, the backlog
protocol, round-robin correctness, the rate-limit path, and the `f0aa5a2`
critical-section generalisation — plus a check on two tests reported as
reshaped.

---

## BLOCKING findings

None.

---

## Findings by review item

**1. Concurrency correctness of `Daemon.offer` (`tick.go:701-763`).** Traced
every path after a successful `inflight.enter`: `pool.try()` failure undoes
with `inflight.leave` before returning `offerFull`; `materialise` returning
`(nil, _)` or `(_, err)` undoes with `pool.done()` then `inflight.leave` — the
named deliberate deviation, both calls made synchronously on the Tick
goroutine, never in a worker defer; the dispatched path hands both releases to
the worker's own two defers (`pool.done` registered first so it runs last,
`inflight.leave` second). No path was found that enters without a matching
leave, or that leaves a pool slot taken. The `pool.try()`/`inflight.enter()`
ordering rationale in the comment (enter first, so the common refusal — a full
pool — costs one cheap undo rather than the reverse) checks out against the
code as written.

The `pool.done()` call on the materialise-failure path is sound against
`pool.go`'s semantics: `try()`'s `wg.Add(1)` and the materialise-failure
`done()`'s `wg.Done()` both execute on the Tick goroutine, sequentially, with
no worker goroutine involved. `pool.wait()`/`pool.quiesce()` (the only `Wait`
call sites) run only from `loop`'s shutdown defer or a test's own `Quiesce`
call between ticks — never concurrently with a `Tick` in progress, since `loop`
calls `Tick` synchronously and to completion before its next select
iteration. `sync.WaitGroup`'s "Add must not race a Wait that could return"
constraint is therefore never in play.

**2. `TickResult` single-writer claim.** Every field (`StartedAt`, `Duration`,
`Skipped`, `RetryAt`, `Repos`, `Issues`, `Dispatched`, `InFlight`, `Deferred`,
`Cursor`) is written only inside `Tick`, `discover`, `dispatch`, `offer`, or
`deferRest` — all of which execute exclusively on the Tick goroutine, before or
around (never inside) the `go func(...)` literal in `offer`. `result.Dispatched++`
happens immediately after the `go` statement, on the Tick goroutine, not in the
spawned closure. Confirmed no worker-reachable code path writes through the
`*TickResult` pointer. `-race` over the full `internal/daemon` suite is clean,
consistent with the structural claim.

**3. Worker context.** `Tick`'s worker goroutine is launched as
`go func(ctx context.Context, t *task) {...}(ctx, t)` where `ctx` is `Tick`'s
own parameter — the daemon's long-lived context, passed through with no
`WithTimeout`/`WithCancel` anywhere between `Daemon.Tick`'s signature and the
`go` statement. `Daemon` holds no context field (confirmed in `daemon.go`'s
struct). `execute.go`'s diff in this cycle is comment-only — no context
narrowing was introduced there either. Matches the plan's RD-10/Platform note
verbatim.

**4. Data shared across the goroutine boundary.** `store.Repo` (in `task.repo`,
`candidate.repo`) has no slice or map fields — copying it by value is
unconditionally safe. `github.Issue` (in `task.issue`) does carry
`Labels []string` and `Assignees []User`; nothing in `tick.go` mutates either
in place after a `github.Issue` value is read from the forge, and the one place
a `task`'s issue crosses into another package's request
(`turn.Request.Issue.Labels`, `tick.go:1311`) is an explicit
`append([]string(nil), t.issue.Labels...)` copy, matching the pattern the plan
calls out. `resolveTools` (`tools.go:183-185`) already copies `Tools` and
`AllowedTools` out of the shared `d.toolProfiles` map before returning them,
and that map is read-only after `New` (cloned once via `maps.Clone`), so
concurrent reads from multiple workers are safe. No candidate or task struct
field is written after its containing `go` statement — confirmed by reading
every function reachable from `Daemon.work`. `q.candidates`, `d.lastRepoID` and
`d.repoSessionCursor` are touched only from `dispatch`, on the Tick goroutine;
no worker holds a reference to a `repoQueue`.

**5. The backlog protocol (RD-2).** `offer` calls `d.markBacklog` unconditionally
right after a successful `inflight.enter` and before `pool.try()` — so both the
"got a slot" and "pool was full" outcomes mark the session. `deferRest`
additionally marks every candidate `pop()` never reached this tick (the
remainder of each `repoQueue`, via `q.rest()`), and does not double-count
against the one candidate `offer` already marked and counted as `Deferred` in
the `offerFull` branch (that candidate was already popped, so it is excluded
from `q.rest()`). This resolves the forward-looking hazard recorded as
**CR-A-001** in the previous review cycle — the commit message cites it by
name and the code confirms it. The only clear site is the unconditional
statement in `Daemon.work` (`if !deferred { d.clearBacklog(ctx, t) }`), never a
`defer`, so an error return can never silently clear the flag. Consistent with
"stale-true safe, stale-false impossible."

**6. Round-robin correctness in `Daemon.dispatch`.** The repo-rotation `start`
index and the per-repo session cursor (`q.next`) are both computed once per
call, from `d.lastRepoID`/`d.repoSessionCursor`, before the dispatch sweep
begins; the sweep is a series of one-candidate-per-queue passes in rotation
order until no queue makes progress or the pool refuses a slot — matching the
`⌊S/R⌋`/`⌈S/R⌉` guarantee in the doc comment. `repoQueue.pop()`/`rest()` account
correctly: `rest()` returns only candidates beyond `taken`, so a candidate that
was just popped and offered (dispatched, skipped, or deferred-for-no-slot) is
never double-counted by the subsequent `deferRest` sweep. One minor edge,
recorded below as **CR-A-003**.

**7. The rate-limit path.** `d.rateLimited` is called from three places:
`Daemon.observe` (Tick goroutine, listing failure), `Daemon.leave` and
`Daemon.stopped` (both reachable only from a worker goroutine). `backoff` is
entirely mutex-guarded (`hold`/`fail`/`clear` each take `b.mu`), so concurrent
calls from multiple workers are safe, and `fail`'s idempotence-within-a-hold
(AC-PAR-4.2) does not depend on which goroutine calls it. The old
`leave → handleComment → drain → processIssue → poll → Tick` escalation chain
is confirmed gone: `handleComment`/`reply`/`run` all funnel failures through
`d.leave`, which logs and returns `nil` for anything but a shutdown-context
error, and `Daemon.stopped` (the worker's top-level failure handler) never
returns anything to a caller that still exists. No worker-side error
propagates anywhere Tick's error branch could see it — `Tick`'s own `error`
return is reachable only from `discover`/`InitPollCursor`/`ListRepos`/
`SetPollCursor`, all Tick-goroutine-only.

**8. `f0aa5a2` — `commitStoryIndex` as a third critical section.**
`commitStoryIndex` acquires the clone gate once, releases via one `defer`, and
every one of its four failure paths (gate acquisition, `Commit` error,
`commit.Committed == false`, `Push` error) returns `("", false)` without
touching `fileStoryIssues`'s `result` — confirmed by reading both functions
side by side; `fileStoryIssues` does `sha, ok := commitStoryIndex(...); if !ok
{ return result }`, so `result` is genuinely untouched on every failure branch.
`TestCriticalSectionsAreSingleAcquisitions` was rewritten from a hardcoded pair
to a registered `sections`/`gated` map, asserting per-call (not per-section):
every gated call (`fetch`, `freezeSlug`, `ensureWorktree`, `Commit`, `Push`)
must appear inside every section that claims it and nowhere else in the
package, and the total acquisition count across the package must equal the
number of registered sections. Read the test logic (not just its comment)
against this claim: a fourth, ungated git-write function added anywhere in the
package would fail on its own name via the per-call loop (`countCalls(funcs[other].decl,
call) > 0` for a function not in `allowed`), independently of the final
`total != len(sections)` check, which catches the case where a new section is
added without registering it. `go test -race ./internal/turn/...` confirms the
rewritten test passes as committed; this reviewer did not mutate the source to
force-fail it (review-only, no source edits per the task brief), so this is a
static read of the test's logic rather than an empirical regression check.

---

## Reshaped tests

Both tests named in the task brief were checked against what they claim to
assert, not just against what compiles.

- **`TestTransportErrorLeavesCursorAndWatermarksUnchanged`** (`tick_test.go:1045`)
  moved its injected failure to the third issue-listing call — a genuine
  discovery-phase (Tick-goroutine) transport error, which is now the *only*
  kind of failure that still ends a tick with a returned error (a worker's
  transport error no longer can, by design — RD-5 severed that chain). The
  test's own doc comment says exactly this and points at
  `TestAFailedBootstrapLeavesTheBacklogFlagSet` for the worker-side case. This
  is not a weakening; it is the correct test for AC-2.13 under the new
  architecture, and the old shape (a failure that no longer exists in the
  worker-reachable code path) could not have been kept without asserting
  something false.

- **`TestAFailedBootstrapLeavesTheBacklogFlagSet`** (`backlog_test.go:74`)
  dropped the `f.tickErr()` / `err == nil` check and switched to `f.settle()`.
  This still enforces the same fact, implicitly rather than explicitly:
  `f.settle()` calls `f.tick()`, which calls `t.Fatalf` on any non-nil error
  from `Tick`. If a bootstrap failure on a worker somehow still propagated to
  `Tick`'s return value, this test would fail immediately at that line, just
  with a less specific message. The remaining explicit assertions — watermark
  unplanted (`-1`) and `needs_poll` still set — are exactly what the test's
  name promises, and are the assertions that actually distinguish "worker
  failed, flag stands" from "worker succeeded." Both tests still earn their
  names.

Also spot-checked `ask_test.go`, `tools_test.go`, `daemon_test.go` for the same
pattern (removed `result.Runs`/`result.Replies`/`result.Deferred`/
`result.Bootstrapped` assertions against the deleted `TickResult` fields).
Every removal found a same-or-stronger direct replacement already in the diff
(`f.turns.calls()`, `f.forge.snapshot()`, `f.watermark()`,
`f.session().NeedsPoll`) — none left a gap. One test
(`TestRunTicksFromItsTickSource`, `daemon_test.go`) improved on the pre-existing
version: it switched from `cancel()` to `close(ticks)` as the join signal,
correctly noting in a new comment that `cancel()` would otherwise race a
not-yet-scheduled worker against its own context.

---

## ADVISORY findings

### CR-A-003: The repo-rotation cursor resets to index 0 when the last-dispatched repo has since dropped out of the eligible set

**File:** `internal/daemon/tick.go:635-641` (`dispatch`)
**Story:** US-5 (T-PAR-22)
**Category:** Performance / fairness edge case, not a correctness defect

**Finding:** `start` is computed by scanning `queues` for `d.lastRepoID` and
taking `(i+1) % len(queues)`; if no queue in the current tick matches
`d.lastRepoID` (the repository that was last dispatched from has since become
ineligible — no candidates this tick, deregistered, or switched to an
unsupported forge), `start` silently falls back to its zero value, i.e.
whichever repository happens to sort first into `queues` (registration order).
This does not violate any stated acceptance criterion — AC-PAR-2.3 is about
reordering `ListRepos`' *output*, and this is about a repo that drops out of
eligibility entirely, which the plan does not appear to address — but it does
mean that a repository which happens to sit early in `ListRepos`' natural order
gets a small, repeated fairness edge over the long run in a fleet where the
previously-served repository intermittently has no work (goes idle, then comes
back), rather than the rotation resuming from a neutral point.

**Suggested fix:** none required for this cycle — it is a minor, unspecified
edge and not a regression against any AC. Worth a one-line comment on `dispatch`
noting the fallback behaviour if a future task revisits fairness under
repository churn, so a future reader doesn't have to re-derive it.

---

## Cross-story summary

The red commit's core claim — that discovery and dispatch are cleanly
separated, `TickResult` is written from exactly one goroutine, workers carry
the daemon's own long-lived context, and no data crosses the goroutine
boundary through a shared backing array — held up under a full read of
`tick.go`, `pool.go`, `inflight.go`, `backoff.go` and the goroutine's own
call graph. The one thing the plan calls out by name as a review item and
this reviewer went looking hardest for — cross-turn tool-surface contamination
via a shared slice or map — was not found: every slice that crosses into a
worker or into a `turn.Request` is copied at the boundary, matching the
existing discipline the architecture notes describe as already verified.

The `f0aa5a2` clone-gate generalisation is the stronger of the two commits
reviewed this cycle: the structural test was rewritten to assert per-call
rather than per-section, which is a genuine hardening (a future ungated
section fails on its own name rather than requiring the test author to
remember to update a hardcoded section count), and `commitStoryIndex`'s
failure containment was verified by hand against `fileStoryIssues`'s
containment rule — no failure path mutates `result`.

The one item resolved from the previous review cycle: **CR-A-001**
(`markBacklog` needing to cover the deferred-for-no-slot case once dispatch
split from discovery) is fixed exactly as anticipated, and the commit message
cites it by number. **CR-A-002** (backoff cannot self-clear with zero
registered repos) is unrelated to this commit's files and remains open as
recorded — still correctly assessed as benign.

No BLOCKING issues were found in either commit. The one new advisory
(CR-A-003) is a minor, unspecified fairness edge under repository churn, not a
regression against any stated acceptance criterion.

---

## Verdict

ADVISORY_ONLY — 1 new advisory note recorded (CR-A-003, a minor round-robin
fallback edge under repository churn). CR-A-001 from the previous cycle is
resolved. Proceeding to environment setup / testing.

---

## Disposition (orchestrator, 2026-08-31)

- **CR-A-001** (cycle 1) — **CLOSED** by `f565ba2`. `markBacklog` now covers the
  deferred-for-no-slot case as well as the dispatched one, via `Daemon.offer`
  and `Daemon.deferRest`. Verified by this cycle's review item 5.
- **CR-A-002** (cycle 1) — **STANDS, accepted.** A standing backoff still cannot
  self-clear with zero repos registered. Unchanged by this cycle and still
  benign: with no repos there is no traffic the backoff protects.
- **CR-A-003** (cycle 2) — **CLOSED** by `256a8d9`. Taken rather than deferred,
  because the same commit wrote the `⌈R/S⌉` bound into `internal/daemon/doc.go`
  and the fallback-to-index-0 made that promise false under repository churn.
  `Daemon.dispatch` now sorts the queues by repo id and starts at the first id
  greater than `lastRepoID`, wrapping — a cyclic successor that needs no match.
  The sort also makes AC-PAR-2.3 structural rather than incidental, since
  `store.DB.ListRepos` orders by owner/name/path and not by id.

Verdict for the run: **ADVISORY_ONLY**, zero blocking findings. Two of the three
advisories are closed in code; one is accepted and documented.
