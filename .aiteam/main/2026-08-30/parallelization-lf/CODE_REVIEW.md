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
