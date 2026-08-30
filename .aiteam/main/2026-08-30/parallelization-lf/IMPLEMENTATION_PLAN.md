# Implementation Plan — parallelization-lf
# Branch: main
# Date: 2026-08-30

litefactory works different issues and different repositories concurrently, bounded by one
daemon-wide cap, while keeping one daemon process and strictly sequential runs per issue.

Sources of truth, in precedence order: `SPIKE.md` (measured — wins over all prose),
`RESEARCH_BRIEF.md` (minus the sections its own "MEASURED 2026-08-30" banners correct),
`.aiteam/APP_CONTEXT.md`, `.aiteam/AC_AMENDMENTS.md`.

---

## The shape, in one paragraph

`Tick` splits into **discovery** (Tick goroutine: listings, session rows, eligibility, fairness
ordering, dispatch decision, cursor) and **drain** (worker goroutine: one session, its whole
comment queue, in order). The dispatch unit is the **session**, never the comment — that is what
preserves AC-2.11 and what makes "5 comments on one issue occupy one slot" true by construction
rather than by policy. Concurrency is a buffered-channel semaphore plus a `sync.WaitGroup`, with
**no queue**. Two new pieces of daemon-owned state: an **in-flight set** keyed on `session.ID`,
and a **per-clone git gate** injected through `turn.Options`. The poll cursor stops being held
back and becomes monotonic; outstanding work becomes a durable flag on `sessions`. `TickResult`
stops describing work *done* and starts describing work *dispatched*.

## Settled decisions

Owner rulings, not to be re-opened.

| # | Decision |
|---|---|
| D1 | **Decoupled dispatch**, not fan-out-and-join. `Tick` stops being synchronous-and-awaitable. Cursor redesign, ~50% of the `internal/daemon` suite, and amending `doc.go`'s "ordinary synchronous method" are accepted as in scope. Fan-out-and-join was considered and rejected: it does not deliver the requesting example. |
| D2 | The ceiling is **`max_concurrent_runs` in `config.yaml`** — one daemon-wide number. Not a Go constant, not per-repo. Per-repo `repo add --max-parallel N` is deferred, not rejected, and must not be foreclosed. |
| D3 | **D-X does not bar this.** D-X rejected a limit on runs *per unit time* (spending control); this is a limit on runs *at the same instant* (resource control). Different mechanism, different purpose. D-X's "no task or AC may reintroduce it" therefore does not reach it — and because D-X is worded that way, the ruling is recorded in-tree (US-7). |

Architect rulings closing the spec's RD-1..RD-12 are stated in the Architecture Notes of the
story each belongs to.

## Record corrections found during planning

- **D-X was never superseded.** The claimed supersession (`D-CI-3`) exists only in
  `comment-intent/IMPLEMENTATION_PLAN.superseded.md:19`, and that same document reverses itself at
  `:483` — *"OD-3 — RESOLVED: no ceilings enforced at ship. Against recommendation. D-X stands in
  effect."* The 4-task replacement plan dropped the thread entirely. D-X originates at
  `lf-setup/IMPLEMENTATION_PLAN.md:31`, restated `:421`.
- **The shipped amendment convention is the repo-root ledger**, not the session-local file. Both
  prior designs (`comment-intent` S9, `fix-comments-pickup` S5) specified `$session/AMENDMENTS.md`
  and neither was written. `first-comment-pickup` T-6.16 shipped `.aiteam/AC_AMENDMENTS.md` plus a
  pointer line, precisely because session-scoped files are unfindable. That is the convention this
  plan follows.
- **AC-FC-2.6 is narrower than the brief implied.** It is scoped to `CursorLookback`, and its
  reason is *"a value no operator should have to choose"*. `max_concurrent_runs` is the opposite —
  machine-shaped, and the operator must choose it. A distinction, not a contradiction. Its test-seam
  pattern (config key + `Options` override where zero means default) is reused here.
- **`errgroup` is a named rejected dependency** (`lf-setup/IMPLEMENTATION_PLAN.md:62`, beside
  `testify`, `cobra`, `fsnotify`, `lumberjack`). `platform-constraints.md` recommends it; that
  recommendation is overridden here, for three independent reasons (see US-1 notes).
- **The goroutine inventory is stated in three places that disagree.**
  `lf-setup/IMPLEMENTATION_PLAN.md:58` counts *tick loop / stdout / stderr / `logs -f` tailer*;
  `APP_CONTEXT.md:280-284` counts *stdout / stderr / daemon loop / second-signal watcher*;
  `comment-intent/IMPLEMENTATION_PLAN.superseded.md:37` states a third. Both live statements are
  corrected to one text (T-PAR-44).

---

## User Story 1 — A run in flight must not stop discovery

> As the owner, I want a run in flight on issue #7 to not stop litefactory noticing and starting
> issue #8, so that a 30-minute run does not freeze the whole daemon.

Splits `Tick` into a synchronous discovery phase and asynchronous per-session workers, and repairs
the three mechanisms that decoupling breaks: shared result accumulation, rate-limit propagation,
and shutdown.

### Architecture Notes

- **RD-5 — `TickResult` is reshaped, not synchronized.** Remove `Bootstrapped`, `Comments`, `Runs`,
  `Replies`; add `Dispatched` and `InFlight`; narrow `Deferred` to "eligible sessions that found no
  free slot"; delete `pending` and its min-reduction (RD-2 removes the need). Afterwards **no
  `TickResult` field is written by more than one goroutine**, so AC-PAR-4.1 is structural and
  `-race` confirms it, rather than a mutex that `-race` merely fails to find a hole in. A mutexed
  counter would be a snapshot whose value depends on timing — meaningless to assert on.
- **The `Runs++`-before-`DispositionLocked` ambiguity is resolved by deletion.** `result.Runs++`
  (`tick.go:723`) fires before the disposition check, so it always meant "handed to `Execute`",
  never "ran". Under dispatch that is exactly `Dispatched`. A rename, not a move.
- **Testability relocates to where the fixture already puts it.** `fixture_test.go:168-170` states
  the fixture's purpose — *"order records the sequence of externally visible actions, so that 'the
  reaction happened before the run' is an assertion rather than a hope"*. `f.turns.triggers()` and
  `f.forge.snapshot()` were always the right observables; the counters were an aggregate shortcut.
- **RD-10 — the goroutine budget, in the four terms the M0 convention demands.** *How many:* four,
  plus at most `max_concurrent_runs` turn workers; the value is validated 1..16 and refused (never
  clamped) outside it, so the inventory has a hard ceiling of 20. *Termination condition, as one
  named thing:* `Daemon.Quiesce`. *What happens when the bound is exceeded:* it cannot be — the
  semaphore is a buffered channel created once in `New`, with exactly one send site (the dispatch
  loop, on the Tick goroutine) and one receive site (the worker's defer); the send is `select` with
  `default`, so a full pool never blocks discovery. *What the goroutine buys:* the wall-clock of a
  turn minus its git critical sections, overlapped across sessions — a 30-minute run stops delaying
  discovery by 30 minutes.
- **Semaphore + `WaitGroup`, not a fixed worker set reading a channel.** The fixed-set shape needs a
  work channel, and a work channel is a queue. A queue commits a task to a worker at enqueue time
  and dispatches it later under **stale fairness state and stale backoff state** — contradicting
  RD-1 and AC-PAR-4.4 — outlives the tick, and is LF-10's "queue and coalesce", which is out of
  scope. The semaphore has no queue: a dispatch either starts now or is marked `needs_poll` and
  reconsidered from scratch next tick.
- **`errgroup` is excluded for three independent reasons**, all recorded so nobody re-proposes it:
  it is named in the rejected-dependency list; it would be a fourth direct require against a
  `go.mod` with exactly three; and it *is* fan-out-and-join with first-error semantics, which is the
  design the owner rejected.
- **Shutdown changes `Run` by zero lines.** In `loop`, `defer close(done)` registered first and
  `defer d.pool.wait()` registered second means the second runs first, so `done` already means *"the
  loop returned **and** every worker finished"*. `Run`'s existing grace `select` (`daemon.go:462-491`)
  and the D-B abandonment comment work unmodified — they just now cover N runs. No sixth goroutine.
- **Workers take the daemon's long-lived context.** `Tick` must never wrap it in
  `WithTimeout`/`WithCancel` before dispatch; a Tick-scoped context handed to a goroutine that
  outlives `Tick` would cancel every run the instant discovery finished. `harvest.go:33`'s
  `context.WithoutCancel` already covers the other side, so a cancelled worker still records itself.
- **Rate-limit propagation today is a return-value chain** — `leave` → `handleComment` → `drain` →
  `processIssue` → `poll` → `Tick`'s error branch → `observe`. Dispatch severs it at `drain`.
  Collapse it: extract `d.rateLimited(ctx, log, err)` callable from any goroutine (`backoff` is
  already mutex-guarded), have `leave` call it directly and return nil like everything else. This is
  a simplification — `leave`'s special case and the five-frame escalation both disappear.
- **`backoff.fail` becomes idempotent within an active hold** (AC-PAR-4.2): inside the existing
  mutex, if `now < until`, record the observation and return the standing delay without incrementing
  `failures`. Five simultaneous 403s give one increment, never `retryAfter · 2^4`. A second 403
  *after* a hold expires still increments — which is what "consecutive refusals" always meant.
- **`backoff.clear()` must stop meaning "the tick completed"** (`tick.go:114`). Under dispatch that
  no longer implies "no rate limit was hit". Clear on **evidence of success** — the first forge
  listing of the tick that returns 200, on the Tick goroutine. A worker's later 429 re-arms it,
  correctly, as a *new* refusal. Worst case is one extra dispatch window; state that in the comment
  rather than defending against it.
- **The backoff is checked at two points, never one** (AC-PAR-4.4): at the top of `Tick`, and
  immediately before each `go`, because a worker's 429 can arm it mid-tick. **The backoff owns no
  `CancelFunc` and never touches a worker context** (AC-PAR-4.3) — a one-line structural invariant.
- **RD-9 — secondary limits are treated differently in exactly one way.**
  `github.RateLimitError` already carries `Secondary` and `Resource` and `observe` already logs
  them, so the signal is free. A secondary limit means the *request pattern* is the problem, so
  resuming at full concurrency reproduces it. After a secondary limit clears, the first dispatch
  window is capped at 1 — a single-slot probe — returning to `max_concurrent_runs` after a clean
  window. Primary limits are quota, not pattern, and get no reduction. Expressed as a **ceiling
  consulted at dispatch**, not a mutation of the configured value, so per-repo bounds stay
  composable (D2).
- **This feature does not increase the read-request rate.** Discovery stays single-threaded on the
  Tick goroutine at `poll_interval_seconds`. What concurrency multiplies is writes
  (`AddCommentReaction`, `PostComment`), bounded by the cap.
- **RD-6 — no second `recover()`. Accept and document the exposure.** The exposure is not new, only
  its blast radius: `loop` does not recover today either, so a panic in a turn already kills the
  daemon. The abandonment path already handles N — `ReconcileOrphans` fails every abandoned run,
  posts a failure comment on every affected issue, advances every watermark; `kill -9` produces the
  identical outcome, so it must work regardless. A worker `recover()` would have to decide what to
  do with a half-written worktree, an open `runs` row and an unposted report, and the honest answer
  is "abandon it" — which is what the crash already does, more loudly. `defer` unwinding does run
  during a panic, so `ReleaseSessionLock` still fires; AC-3.3's comment already contemplates a panic
  without recovery.
- **Observability: `d.now()` must stay on the Tick goroutine.** All four call sites (`tick.go:68,
  98, 106, 108`) are in `Tick` today. `testClock` advances one second per read
  (`fixture_test.go:556-577`), so a per-issue goroutine reading it makes every `since`-string
  assertion flaky. Per-repo and per-task child loggers use the existing vocabulary only —
  **a `worker` key is an invention and must not be added**; `task` already disambiguates any two
  lines, and `worker` would leak the pool's internals into the log contract.
- Platform: goroutine exit paths — every worker exits on one of three bounded conditions (list
  exhausted / comment unconsumed / `ctx.Err()`), all joined by `Quiesce`; no worker can outlive
  `loop`.
- Platform: `Daemon` holds the pool and the in-flight set but **must not hold a context** — a
  semaphore is a channel and a WaitGroup is a counter; the context arrives as `Tick`'s parameter and
  is passed into the `go` closure as an argument.
- Platform: `context.Background()` is lint-forbidden outside `main`; the only permitted stripping
  remains the existing `context.WithoutCancel` at `execute.go:302` and `harvest.go:33`.
- Risk: **T-PAR-9 is the schedule risk, not T-PAR-5.** Re-shaping ~50% of a 1,300-line suite is the
  largest single piece of work and the hardest to size before starting.
- Risk: **T-PAR-8 gates eight downstream test tasks.** Build the settle primitive early and review
  it hard. A `time.Sleep` or polling `Eventually` used to unblock it would make every downstream
  test permanently untrustworthy, and the spec rejects both by name.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-PAR-1 | **Prerequisite:** `fixture_test.go` — one `slog` handler over one buffer, mutex-guarded accessor replacing the bare read at `:818`; `make test` (`-race`) green | test | — | S |
| T-PAR-2 | `inflight` type: `map[int64]struct{}` + `sync.Mutex`, `enter`/`leave`, high-water counter | logic | T-PAR-1 | S |
| T-PAR-3 | `pool` type: buffered-channel semaphore, `sync.WaitGroup`, non-blocking `try()`, exported `Quiesce(ctx)`; goroutine-budget comment in the RD-10 four-term form | logic | T-PAR-1, T-PAR-19 | M |
| T-PAR-4 | Join the pool in `loop`'s shutdown defer (`defer close(done)` then `defer d.pool.wait()`); verify `Run` unchanged | logic | T-PAR-3 | XS |
| T-PAR-5 | Split `Tick`: discovery phase separated from dispatch phase; `task` construction and `ParseRef` stay on the Tick goroutine | logic | T-PAR-2, T-PAR-3, T-PAR-11 | L |
| T-PAR-6 | Move `processIssue` from `GetWatermark` onward onto the worker goroutine; worker takes the daemon context, never a Tick-scoped one | logic | T-PAR-5 | L |
| T-PAR-7 | Reshape `TickResult` per RD-5; delete `pending` and its min-reduction; update the "tick complete" log line | logic | T-PAR-5 | M |
| T-PAR-8 | Fixture settle primitive over `Daemon.Quiesce`; sorted `triggers()` + ordered `triggersFor(sessionID)`; per-issue projection of `forge.snapshot().order`. **No `time.Sleep`, no `Eventually`.** | test | T-PAR-3, T-PAR-7 | M |
| T-PAR-9 | Re-shape the existing tick suite (~50%) onto the settle primitive and the new observables | test | T-PAR-8, T-PAR-6 | L |
| T-PAR-10 | Tests AC-PAR-1.1, 1.2, 1.10 | test | T-PAR-9 | M |
| T-PAR-34 | Backoff: `fail` idempotent within an active hold; extract `d.rateLimited` callable from any goroutine; `leave` calls it directly; `clear()` on the first 200 listing, not on tick completion | logic | — | M |
| T-PAR-35 | Backoff checked at both the top of `Tick` and before each `go`; backoff owns no `CancelFunc` and never touches a worker context | logic | T-PAR-34, T-PAR-5 | S |
| T-PAR-36 | Secondary-limit single-slot probe as a dispatch-time ceiling, restored after a clean window | logic | T-PAR-35 | S |
| T-PAR-37 | Tests AC-PAR-4.1, 4.2, 4.3, 4.4 | test | T-PAR-36, T-PAR-8 | M |
| T-PAR-38 | Test AC-PAR-4.6: shutdown with N in flight | test | T-PAR-4, T-PAR-9 | M |
| T-PAR-39 | Per-repo child logger in `poll`'s loop, per-task child logger in `processIssue` after `ParseRef`; `bootstrap` and `drain` stop using bare `d.log`. Existing vocabulary only | logic | — | S |
| T-PAR-40 | Structural check that `d.now()` is called only from `Tick`, never below `poll` | test | T-PAR-6 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-PAR-1.1 | A tick returns without waiting for any run it dispatched: with a run blocked mid-`Execute`, a subsequent tick discovers and dispatches a *different* issue. |
| AC-PAR-1.2 | Two issues in the same repo run concurrently — both observably inside `Execute` at the same instant. |
| AC-PAR-1.10 | The daemon singleton is unchanged: a second process against the same `$LF_HOME` still fails to acquire `daemon.lock` and exits as it does today (AC-2.6). |
| AC-PAR-4.1 | `TickResult` is correct under concurrency, asserted with `-race`; no field is written by more than one goroutine. |
| AC-PAR-4.2 | N concurrent rate-limit errors within one hold advance the failure counter at most once; a test with 5 simultaneous 403s asserts the resulting delay, not `retryAfter · 2^4`. |
| AC-PAR-4.3 | A rate limit observed while runs are in flight does not cancel their contexts or kill their `claude` processes; it gates the dispatch of new runs only. |
| AC-PAR-4.4 | While backoff is active no new run is dispatched, for any repo — including when it arms mid-tick. |
| AC-PAR-4.6 | Shutdown with N in-flight runs terminates all N; no `runs` row is left `running`; the next `Start`'s reconciliation finds nothing live to abandon. |
| AC-PAR-4.7 | *(prerequisite)* `fixture_test.go` no longer builds two `slog` handlers over one `*bytes.Buffer`; `make test` (`-race`) is green **before** any concurrency is introduced. |
| AC-PAR-7.1 | Every log line emitted below `poll` carries task identity via a per-task child logger; per-repo lines carry repo identity. No line from a concurrent path is attributable only by guesswork. |
| AC-PAR-7.2 | `d.now()` is called only from `Tick`, never below `poll`; a structural check fails if a per-issue path reads the clock. |

---

## User Story 2 — Different repositories worked at the same time

> As the owner, I want different repositories worked at the same time, so that one busy repo does
> not queue every other.

Replaces the held-back global poll cursor with a monotonic cursor plus a durable per-session
backlog flag, so no repo's in-flight work can hold back another's discovery.

### Architecture Notes

- **RD-2 — the cursor becomes a pure forge-listing watermark advancing monotonically to
  `startedAt` every tick. Outstanding work moves into `sessions.needs_poll`.** Cost: one migration.
- **Why `ListingOverlap` cannot be tuned into a fix.** `pending` (`tick.go:210-215`) is a
  min-reduction fed only by synchronous `deferred` returns that cease to exist. And the second
  discovery path does not save it either: `commentedIssues` (`tick.go:246`) keys on
  `ListCommentedIssues(repo, since)` — the **comment's** timestamp, as fixed as the issue's
  `UpdatedAt`. **Both forge listings are timestamp-indexed, so once the cursor passes `T`, neither
  re-finds a comment written at `T`.** A busy session is invisible to both after `ListingOverlap`
  elapses. The re-discovery axis therefore cannot be a forge timestamp; it must be local and durable.
- **Migration `0002_session_backlog.sql`** — `ALTER TABLE sessions ADD COLUMN needs_poll INTEGER
  NOT NULL DEFAULT 0;` plus a partial index `WHERE needs_poll = 1`. The `0` backfill is correct
  because during the two-phase landing the old cursor hold-back is still in force and re-finds
  everything.
- **Protocol — one write site, one clear rule.** The tick sets `needs_poll = 1` for every session it
  dispatches, *before* the worker starts. The worker clears it only on a fully drained,
  error-free `drain`/`bootstrap`. Stale-true is safe and self-healing (the next tick re-lists, finds
  nothing above the watermark, clears). **Stale-false is impossible.** That asymmetry is the same
  posture as `AdvanceWatermark`'s never-rewind: a bad outcome leaves the record pointing at *more*
  work, never less.
- **Discovery becomes the union** of the forge listing and `ListBacklogSessions()` — a store query
  with zero API cost, and cross-repo. The `github.Issue` for a backlogged session is fetched
  **lazily, only when the dispatcher selects it**, capping the extra API cost at exactly
  `max_concurrent_runs` `GetIssue` calls per tick. The naive version's cost grows with backlog
  depth; that bound is worth stating in the code.
- **RD-4 — `DispositionLocked` is promoted from a drop to a marker-set.** `needs_poll = 1` survives
  every abnormal exit: worker abandoned at shutdown, daemon killed, drain stopped on an unconsumed
  comment, `DispositionLocked` from a second process, an error inside `handleComment`.
- **Rejected: a per-repo cursor.** Same migration cost. Fixes AC-PAR-5.1 across repos and nothing
  else — a 30-minute run still pins its own repo's cursor, so the identical intra-repo starvation
  and re-listing cost remain, and AC-PAR-5.2 is satisfied only by the hold-back it was meant to
  remove. Half a fix at full price.
- **Rejected: an in-flight-aware cursor floor.** No migration, but it preserves the fleet-wide pin
  the spec calls the defect, and it makes the cursor a function of in-memory state — which is lost
  on restart while the cursor is not.
- **Rejected: an in-memory backlog with no migration.** Fatal. Advancing the cursor while holding
  the backlog in memory means a restart loses the backlog *and* the cursor has already moved past
  the comment. Permanent silence, which AC-4.1 forbids by name. The backlog must be durable or the
  cursor must not advance; there is no third option.
- **Rejected: `backlog_comment_id` instead of a boolean.** Self-clearing by comparison against the
  watermark is elegant, but the tick does not know the highest comment id without a
  `ListCommentsAfter` it must not make. Mixing a tick-written sentinel with a worker-written id in
  one column is worse than a boolean with an explicit clear.
- **No logical race on the flag** despite `SetMaxOpenConns(1)`: the in-flight set (US-3) guarantees
  no tick can set the flag on a session a worker owns.
- Platform: per APP_CONTEXT a schema change is **a new embedded `NNNN_*.sql` file and nothing
  else** — version from the filename prefix, one transaction, recorded in `schema_migrations`. A
  migration library remains an explicitly rejected dependency. `store_test.go`'s migrate-idempotency
  test covers it for free.
- Platform: the new query and the new write are parameterised `QueryContext`/`ExecContext` in
  `internal/store`, the only package holding SQL.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-PAR-11 | Migration `0002_session_backlog.sql`: `sessions.needs_poll` + partial index; `SetSessionNeedsPoll` / `ClearSessionNeedsPoll` / `ListBacklogSessions` accessors | data | T-PAR-1 | M |
| T-PAR-12 | Set `needs_poll` at dispatch; clear only on a fully-drained, error-free `drain`/`bootstrap`; promote `DispositionLocked` to a marker-set | logic | T-PAR-11, T-PAR-6 | S |
| T-PAR-13 | Cursor becomes monotonic; discovery unions the forge listing with `ListBacklogSessions()`; `GetIssue` fetched lazily at selection | logic | T-PAR-11, T-PAR-5 | M |
| T-PAR-14 | Tests AC-PAR-1.3, 5.1, 5.2, 5.3 | test | T-PAR-13, T-PAR-8 | M |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-PAR-1.3 | Two issues in two different repos run concurrently. |
| AC-PAR-5.1 | A deferred or in-flight issue in repo A does not rewind or hold back discovery for repo B. |
| AC-PAR-5.2 | An issue busy for longer than `ListingOverlap` is re-discovered after its run finishes, without any change to its `UpdatedAt`. |
| AC-PAR-5.3 | Discovery skips a session already in flight: nothing is posted, no watermark moves, and the skip is logged with task identity. |

---

## User Story 3 — Two comments on one issue still run in order

> As the owner, I want two comments on one issue to still run one after the other in comment order,
> so that my second instruction never overtakes my first.

Makes the daemon's in-flight set the unit of mutual exclusion spanning execute *and* report, while
the store's session lock keeps its unchanged cross-process role.

### Architecture Notes

- **RD-3 — sequentiality covers `Execute` + report, enforced by the in-flight set, not by widening
  the store lock.** Moving `AcquireSessionLock` out of `turn.Execute` so it could span the report
  would make `internal/turn` depend on a reporting step it must not know about, and falsify
  `execute.go:285`'s AC-3.1 comment. **Two mechanisms, two purposes:**

  | Mechanism | Scope | Protects against |
  |---|---|---|
  | `sessions.lock_owner` (durable, cross-process) — **unchanged** | `turn.Execute` only | A second daemon *process* touching the same worktree and `runs` row |
  | daemon in-flight set (in-memory) — **new** | dispatch → drain → last report → watermark advance | Thread ordering, and duplicate dispatch within this process |

- **Why execute-only is not acceptable.** The watermark for comment *N* advances inside
  `CompleteRunReport`, i.e. *after* the report posts ("post first, then commit"). If the lock
  releases when `Execute` returns, the session sits momentarily **unlocked and unconsumed** —
  exactly the state discovery reads as eligible. The observable under execute-only is that comment
  2's 👀 and its run can appear *before* comment 1's report lands, with comment 1 then re-entered
  and caught as a replay by `GetRunByTrigger` — safe, but the thread reads out of order and the log
  shows a replay that should never have happened. Under execute+report the thread is strictly
  `c1-👀 → c1-report → c2-👀 → c2-report`.
- **Load-bearing side effect.** Because the in-flight set is checked-and-inserted on the Tick
  goroutine, and `Tick` is never concurrent with itself, **two sibling workers can never both reach
  `AcquireSessionLock` on one session.** `DispositionLocked` therefore remains reachable only from a
  second *process*, so `tick.go:726` / `execute.go:293` stay true with one word changed. The store
  lock becomes defence in depth against a dispatcher bug rather than the primary mechanism — and
  that must be said in a comment, because a reader will otherwise assume the DB lock is doing the
  intra-process work.
- **RD-7 — the instance id gains no worker component. Verified, not assumed.**
  `AcquireSessionLock` (`sessions.go:267-293`) is `UPDATE ... WHERE id = ? AND lock_owner IS NULL` —
  **not** the re-entrant `OR lock_owner = ?`. Two sibling workers passing the same `instanceID`
  cannot both win. And `ReleaseSessionLock`'s defer (`execute.go:301-306`) is registered *after* the
  `!acquired` early return, so a refused caller never registers it and cannot clobber a sibling's
  lock. Intra-process mutual exclusion is already correct with one instance id.
- **What a worker component would buy is mid-life reclaim — the one thing that would let two
  `claude` processes into one worktree.** It buys nothing safe and costs the identity contract that
  `ReconcileOrphans` and every log line depend on. "Wedged forever" is also not reachable: every
  blocking call in a worker is deadline-bounded from exec time (git commands — pinned by
  `git/exec_internal_test.go`; `claude` — `claude.timeout_seconds`, validated 60–86400; GitHub — the
  client's own). The only un-deadlined wait is the clone gate, whose holder is bounded by the above,
  so a waiter's wait is bounded by queue depth × critical-section duration, and CS1/CS2 are git-only:
  seconds. `ReconcileOrphans`' exclusion of own-instance locks is therefore correct and stays correct.
- **RD-8 — `runs` gains no owner column.** Ownership is derivable via `session_id` →
  `sessions.lock_owner` for exactly as long as the lock is held, which is exactly as long as
  ownership means anything. The only use for `runs.owner` is mid-life reclaim or TTL, which RD-7
  rules out. The migration budget goes to `needs_poll`, which is load-bearing.
  `UNIQUE (session_id, trigger_comment_id)` and `UNIQUE (session_id, ordinal)` continue to do the
  real work of refusing a replay. Deferral recorded per AC-PAR-8.6.
- **Duplicate dispatch: the in-flight set is required, and the cost is quantifiable.** Under RD-2
  the backlog union re-lists *exactly* the in-flight sessions, by construction. Without the set, a
  30-minute run at `poll_interval_seconds: 30` would burn, on each of ~60 ticks, one slot + one
  `GetIssue` + one `ListCommentsAfter` + one `AcquireSessionLock` on work guaranteed to return
  `DispositionLocked` — safe, but not free, and it would starve real work out of a slot every tick.
- Platform: the in-flight set, the pool and the rotation cursors are unexported concrete types with
  no interface — one implementation each and no substitution requirement. The fixture substitutes at
  the existing `fakeExecutor` seam.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-PAR-15 | Wire the in-flight set as the RD-3 unit of exclusion: `enter` at dispatch, `leave` after `ReportTurn` returns; comment at `execute.go:285` explaining the two-mechanism split and that the store lock is now defence in depth | logic | T-PAR-2, T-PAR-6 | S |
| T-PAR-16 | Tests AC-PAR-1.4, 1.5, 1.7 | test | T-PAR-15, T-PAR-8 | M |
| T-PAR-17 | Test AC-PAR-4.5 | test | T-PAR-15 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-PAR-1.4 | Two comments on one issue run one after the other, in ascending comment id order; the second never starts before the first's report has posted. |
| AC-PAR-1.5 | 5 comments on one issue with `max_concurrent_runs: 3` → they run one at a time in order, occupying exactly one slot; two slots remain available to other issues for the whole sequence. |
| AC-PAR-1.7 | A comment queued behind a busy session occupies no worker slot: with a cap of 1 and issue A busy, issue B's eligible comment is not blocked by A's *queued* second comment. |
| AC-PAR-4.5 | Two `turn.Execute` calls at one session: exactly one `runs` row, exactly one report, and the loser consumed nothing, posted nothing and advanced no watermark. |

---

## User Story 4 — One number in `config.yaml`

> As the operator, I want one number in `config.yaml` that bounds how many runs exist at once, so
> that concurrency cannot saturate the machine.

Adds `max_concurrent_runs` following the `poll_interval_seconds` precedent across all five config
sites and all three clamp layers.

### Architecture Notes

- **Default `3`, range `1..16`.** The default is a conservative placeholder, **not a measurement** —
  probe P5 (the machine-shaped number) was never run, and `SPIKE.md` says so. Because it is a config
  key, revising it after a real-hardware measurement is an operator edit, not a code change. Each
  bound const carries a comment saying *why* it is where it is, per `validate.go:12-30`.
- **An explicit `0` must be refused, not read as "unlimited".** Defaults are applied by decoding on
  top of `Defaults()`, which is what makes an explicitly written zero distinguishable from an absent
  key (`config.go:39-45`). Out of range is an error, never a silent clamp.
- **Five config sites, and the template's `Sprintf` args are positional.** Struct field with a
  decision-id doc comment (`config.go:47-50` shape); `Defaults()` (`:198`); exported
  `DefaultMaxConcurrentRuns` (`:219-227` shape); `Min`/`Max` bounds consts (`validate.go:12-30`);
  `Validate()` appending to `errs` for `errors.Join` (`:52-55`). Then template text
  (`template.go:34-36`) and its arg at `template.go:179` — **inserting mid-list silently shifts every
  later argument**. `TestDefaultFileMatchesDefaults` is what catches a missed line.
- **Three clamp layers, mirroring the interval's deliberate redundancy.** `daemon.Startup`
  (`setup.go:60-86` — the struct exists to be printed at boot, so the value goes there), `Open`'s
  re-clamp (`setup.go:117-123`), and `Options.validate` (`daemon.go:166-191`), whose comment already
  says it "catches every other way a value can reach the loop".
- **`max_concurrent_runs: 1` reproduces today's *external* behaviour, not its internal synchrony.**
  `Tick` returns immediately either way. AC-PAR-1.8 must therefore assert on the external observable
  — no two `Execute` calls overlap — and never on `Tick` blocking. This is the clarification the
  spec leaves implicit and that the tests will otherwise get wrong.
- **AC-PAR-1.9's high-water instrumentation belongs in the fixture's `fakeExecutor`**, counting
  concurrent `fn` entries, so production carries no test-only instrumentation.
- Platform: `internal/config` stays stdlib-only — an `int` field adds no import, so
  `TestValidationHasNoThirdPartyImports` is satisfied trivially. `secretscan.go` reflects over
  strings only, so an `int` adds no secret surface.
- Platform: the AC-FC-2.6 test-seam pattern is reused — a `daemon.Options` override where zero means
  default, mirroring `Options.Interval`.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-PAR-18 | `max_concurrent_runs` across all five config sites, plus template text and its **positional** `Sprintf` arg | infra | — | M |
| T-PAR-19 | Mirror all three clamps: `daemon.Startup`, `Open`'s re-clamp, `Options.validate` | infra | T-PAR-18 | S |
| T-PAR-20 | `newDaemon(overrides)` gains a `MaxConcurrentRuns` per-field override | test | T-PAR-19, T-PAR-1 | XS |
| T-PAR-21 | Tests AC-PAR-6.1, 6.2, 6.3, 6.4, 1.8, 1.9 | test | T-PAR-20, T-PAR-8 | M |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-PAR-1.8 | `max_concurrent_runs: 1` never has two runs in flight at the same instant, asserted on the external observable. |
| AC-PAR-1.9 | The number of concurrent runs never exceeds `max_concurrent_runs`, asserted by an instrumented high-water mark across a multi-repo multi-issue fixture. |
| AC-PAR-6.1 | `max_concurrent_runs` decodes strictly (`KnownFields(true)`), defaults from `Defaults()`, and is rejected — not clamped — outside `1..16`; an explicitly written `0` is a startup error. |
| AC-PAR-6.2 | The shipped template carries the key with its range comment and `TestDefaultFileMatchesDefaults` passes. |
| AC-PAR-6.3 | The validation error for an out-of-range value is aggregated with other config errors via `errors.Join`, not returned alone. |
| AC-PAR-6.4 | `internal/config/purity_test.go` still passes — no third-party import is added. |

---

## User Story 5 — Every repo makes progress

> As the operator, I want every repo to make progress even while another repo has continuous work,
> so that repo 2 is not starved behind repo 1.

Replaces dispatch-as-you-discover with a materialised eligible set drained by a two-level
round-robin.

### Architecture Notes

- **RD-1 — two-level round-robin: repos rotate, and sessions rotate within a repo.** Both rotation
  cursors live in memory on `Daemon` and are touched only on the Tick goroutine.
- **The fix is separating the walk from the decision, not reordering the loop.** Today `poll`
  dispatches *as it discovers*, inside `for _, repo := range repos`, which with a daemon-wide cap is
  unconditionally starving — repo 1 fills every slot before repo 2's listing is even issued.
  Discovery walks everything; dispatch is a **second pass** over a materialised slice.
- **Ordering.** Repos are visited from `(lastRepoIndex + 1) mod len(repos)`, advancing one per
  dispatched task; within a repo, eligible sessions are visited from just after the last session id
  dispatched from that repo, ascending and wrapping; dispatch proceeds until slots or candidates run
  out.
- **What it guarantees.** With `S` free slots and `R` repos holding eligible work, every repo gets
  `⌊S/R⌋` or `⌈S/R⌉` slots in the same tick when `S ≥ R`. When `S < R`, the rotation cursor advances
  past the repos already served, so every repo is served within **`⌈R/S⌉` ticks** — the bounded,
  assertable number AC-PAR-2.2 asks for. Within a repo the session cursor makes service FIFO across
  ticks. Because order comes from the rotation cursors and not from `ListRepos`, AC-PAR-2.3 falls out.
- **What it does NOT guarantee** — this must be written into the `doc.go` comment AC-PAR-2.1
  requires. *Not proportional:* a repo with 100 eligible issues and one with 1 get the same share;
  starvation-freedom was chosen over throughput, deliberately. *Not preemptive:* a 30-minute run
  holds its slot for 30 minutes — round-robin governs who gets a *free* slot, never who keeps a
  taken one, and with cap 3 and three long runs nothing else starts, which is correct behaviour and
  not starvation. *Not a priority system:* there is no way to make a repo or issue go first. *Not
  fair over comments:* the rotation is over sessions, so a session with 5 queued comments is served
  once per turn of the wheel, not five times.
- **The deferred per-repo bound (D2) is not foreclosed** — it is one extra counter consulted in the
  per-repo step of the dispatch loop.
- **Rejected: global oldest-comment-first.** It needs every candidate session's oldest unconsumed
  comment id at dispatch time, i.e. a `ListCommentsAfter` per candidate on the Tick goroutine — an
  unbounded API cost that grows with backlog, on the one goroutine that must never be slow.
- **Rejected: a persistent cross-tick work queue.** That is LF-10's "queue and coalesce", out of
  scope, and it would commit a task to a slot under stale fairness and stale backoff state.
- Platform: rotation cursors are Tick-goroutine-only state and need no synchronisation — say so in a
  comment, because their placement on `Daemon` invites the opposite assumption.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-PAR-22 | Two-level rotation: repo cursor + per-repo session cursor, in-memory, Tick-goroutine-only; dispatch as a second pass over the materialised eligible set | logic | T-PAR-5 | M |
| T-PAR-23 | The AC-PAR-2.1 justification comment in `internal/daemon/doc.go`: the `⌈R/S⌉` guarantee and the four things round-robin does **not** promise | infra | T-PAR-22 | S |
| T-PAR-24 | Tests AC-PAR-2.2, 2.3 | test | T-PAR-22, T-PAR-8 | M |
| T-PAR-25 | Test AC-PAR-1.6 | test | T-PAR-22, T-PAR-8 | M |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-PAR-1.6 | 5 comments on 5 different issues with `max_concurrent_runs: 3` → exactly 3 start; the 4th starts when the first finishes and the 5th when the second finishes. The test fails if refill happens only after all 3 complete. |
| AC-PAR-2.1 | The dispatch policy is named and justified in `doc.go`, stating what it guarantees and what it does not. |
| AC-PAR-2.2 | With repo A holding continuously eligible work sufficient to saturate the cap, repo B's eligible issue starts within a bounded, asserted number of dispatch decisions. Indefinite starvation fails the test. |
| AC-PAR-2.3 | Reordering `ListRepos`' output does not change which work eventually runs. |

---

## User Story 6 — Serialize concurrent git against one clone

> As the operator, I want concurrent git operations against one clone to be serialized, so that
> turns do not fail with git ref-lock errors that read as flakes.

Introduces a daemon-owned per-clone gate covering the two git critical sections, closes the silent
`push --set-upstream` failure, and refuses the cross-repo worktree-path collision.

### Architecture Notes

- **The gate is a measured requirement, not a precaution.** Probed at parallelism 4 on git 2.50.1 /
  macOS with litefactory's exact argv: `git fetch --prune` **180/240 (75%)** failed when refs move
  (`cannot lock ref 'refs/remotes/origin/main': is at X but expected Y`) and 0/400 when nothing is
  fetched; `git worktree add` **3/400 (0.75%)**, fatal and unretried
  (`failed to read .git/worktrees/<sibling>/commondir`); `git push --set-upstream` **0/240 non-zero
  exits** yet `could not lock config file .git/config` printed 89/240 and **60/240 branches left
  with no upstream**.
- **Retry is the wrong fix for fetch.** It is a losing race to update the same remote-tracking ref,
  not a stale lockfile. Retrying re-runs the race.
- **The gate must be a 1-buffered channel acquired via `select` on `ctx.Done()`, not a
  `sync.Mutex`.** `sync.Mutex.Lock` is uninterruptible, so a shutdown with a worker queued behind a
  60-second `fetch` would block `Quiesce` past the grace budget with no way out. **This is the single
  most likely implementation mistake in the feature.**
- **CS1 is ONE acquisition spanning `prepare.go:87` (`fetch`) through `:97` (`ensureWorktree`).**
  Splitting it reintroduces the `resolveBranchStart` → `worktree add` ref TOCTOU
  (`ensure.go:112-133`) that the gate exists to prevent. **CS2 spans `harvest.go:222` (`Commit`)
  through `:256` (`Push`) and the upstream verification.** **The `claude` run sits between them and
  outside both — that is the entire source of surviving parallelism.**
- **Ownership: `internal/daemon` produces, `internal/turn` consumes, `internal/git` declares
  nothing.** `internal/git` is a leaf whose `Clone` doc promises concurrency-safety; a package-level
  map+mutex there would falsify that comment and hide the serialization from every caller. The
  interface is declared in `internal/turn` (the consumer, per "interfaces at the point of
  consumption"), the concrete type lives in `internal/daemon` (which owns "when"), and it is injected
  through the existing immutable `turn.Options` — whose doc at `execute.go:29` says *"nothing here is
  per-comment"*, and the registry is per-process. This mirrors `turn.Runner` / `Renderer` / `Poster`
  exactly. `git/surface_test.go` pins `Clone`'s method set as the S-6 allowlist, so the gate adds no
  method to `Clone`.
- **`push --set-upstream` needs an exit-code-independent check.** `push.go:47` checks only
  `err != nil`, and git exits 0 while failing to write `branch.<n>.remote`. **Classify as
  correctness, not security** — no boundary is crossed and the objects reached the remote in all 240
  measured cases. What makes it non-deferrable is report integrity: litefactory signs its own name
  to the report comment, and the codebase's stated posture is that what it signs must be true. The
  failure must surface as a **run failure disposition (a value)**, matching "a bad outcome is a
  value, not an error".
- **Security: the cross-repo worktree-path collision is the one genuinely new exposure, and the gate
  does not cover it.** `naming.WorktreePath` (`paths.go:62-71`) is `Join(root, prefix + itoa(issue))`
  with **no repo component**, so with a global `worktree_root` repo A #42 and repo B #42 map to one
  directory. Today `checkVacant` (`ensure.go:123`) fails loudly because calls are sequential.
  Concurrently, `checkVacant` → `MkdirAll` → `worktree add` is a TOCTOU: both turns can pass
  `checkVacant` before either directory exists. They take *different* gates (different clones) and
  hold **two correctly granted, different** session locks (different `sessions` rows), so **nothing
  else in this design stops them** — a correctly authorised turn can land in a directory another
  issue's `claude` is working in, with that issue's content and diff. Most interleavings fail loudly
  at the second `worktree add`; "most" is not a security property. **AC-PAR-4.8 is therefore a
  security control, not hygiene:** a hard startup error naming both repos (the `ErrLiteralSecret` →
  exit 3 shape is the precedent), **and the same check at `repo add`** — otherwise a colliding repo
  is registered into a live daemon and discovered only at the next restart, with colliding turns
  running in between. A store query at registration does not violate `repos/network_test.go`, which
  pins *no network call*.
- **Verified concurrency-safe and needing no change:** `redact.Redactor` (`literals` snapshotted at
  `New`, `dedupeLiterals` sorts a construction-time local, `Apply` mutates nothing on the receiver);
  the `--tools` ceiling (per-request — `t.issue.Labels` is goroutine-local, `turn.Request.Labels` is
  copied, `d.toolCeiling` is resolved once and read-only, and `harness.Request.validate` re-checks
  before argv); `killSubtree` (`kill.go:62`, walks `pgrep -P` transitively from the run's own pid,
  guards its own pgid, **never matches on process name**, and each child is `Setpgid`'d into its own
  group — a sweep for turn A cannot reach turn B).
- **Security review item: the slice-copy discipline changes meaning.** APP_CONTEXT justifies the 11
  `append([]string(nil), …)` sites as preventing "one `append` into spare capacity" rewriting the
  operator's configuration for a later run. Under concurrency that upgrades from a sequential bug to
  **cross-turn tool-surface contamination between live siblings**. Every existing copy site is now
  load-bearing for isolation, and every new slice or map crossing into a worker must be copied at the
  boundary. Make this an explicit review item rather than trusting habit.
- **Git deadlines must keep starting at exec time.** `runner.run` starts its timeout at exec, and
  neither `daemon` nor `turn` sets an ambient deadline. Time spent waiting for the gate must not
  consume a command's budget (AC-PAR-3.6).
- Platform: `Acquire(ctx, path) (release func(), err error)` — the context is a parameter, never a
  field on the registry.
- Note: AC-PAR-3.3 asks for "a test or structural assertion" that CS1 is one acquisition. A
  behavioural test for "the lock was not released between two calls" is awkward; a structural one (a
  single deferred release in `prepare`, asserted by an AST check in the style of
  `config/purity_test.go`) matches this codebase's existing habit and is more likely to hold.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-PAR-26 | Exit-code-independent upstream verification after `push --set-upstream`; a push that exits 0 without writing `branch.<n>.remote` becomes a run failure disposition. Adds no method to `git.Clone` | logic | — | M |
| T-PAR-27 | Test AC-PAR-3.4 with a fake git that exits 0 without writing the upstream | test | T-PAR-26 | S |
| T-PAR-28 | Clone-gate registry in `internal/daemon` keyed on `store.Repo.Path`: `map[string]chan struct{}` guarded by a mutex, **1-buffered channel per clone acquired via `select` on `ctx.Done()` — not a `sync.Mutex`** | logic | — | M |
| T-PAR-29 | Consumer-side interface in `internal/turn`; new immutable field on `turn.Options`; wired in `internal/daemon`. `internal/git` declares nothing | api | T-PAR-28 | S |
| T-PAR-30 | CS1 — one acquisition spanning `fetch` through `ensureWorktree` | logic | T-PAR-29 | S |
| T-PAR-31 | CS2 — one acquisition spanning `Commit` through `Push` and the T-PAR-26 verification; no ambient deadline may wrap either section | logic | T-PAR-29, T-PAR-26 | S |
| T-PAR-32 | Startup **and `repo add`** refusal of two repos mapping one issue number to one worktree directory, naming both; `naming.WorktreePath` unchanged | logic | — | M |
| T-PAR-33 | Tests AC-PAR-3.1, 3.2, 3.3, 3.5, 3.6, 3.7, 4.8 | test | T-PAR-30, T-PAR-31, T-PAR-32 | L |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-PAR-3.1 | A per-clone gate, keyed on `store.Repo.Path`, serializes `fetch`, `worktree add` and `push` against one clone. It lives in `internal/daemon` and is injected via `turn.Options`; `internal/git` declares no mutex and no registry. |
| AC-PAR-3.2 | The `claude` run is outside both critical sections: two turns on the *same* repo are observably both inside their `claude` phase at one instant. |
| AC-PAR-3.3 | CS1 is a single acquisition spanning `fetch` through `ensureWorktree`; a test or structural assertion fails if the gate is released between them. |
| AC-PAR-3.4 | After `push --set-upstream`, the upstream is verified independently of the exit code; a push that exits 0 without writing `branch.<name>.remote` is reported as a failure, not as success. |
| AC-PAR-3.5 | A `-race` test over a fixture of 2 repos × 3 issues passes, and none of `cannot lock ref`, `failed to read .git/worktrees/`, `could not lock config file` appears in captured stderr. |
| AC-PAR-3.6 | Every git command still starts its own deadline at exec time; time spent waiting for the gate does not consume a command's timeout budget. |
| AC-PAR-3.7 | `internal/git/surface_test.go`'s `Clone` allowlist still passes. |
| AC-PAR-4.8 | A configuration in which two registered repos map an issue number to the same worktree directory is refused **at startup and at `repo add`**, with an error naming both repos. `naming.WorktreePath` is unchanged. |

---

## User Story 7 — The record

> As a future maintainer, I want each invariant this change alters recorded in-tree, so that the
> written record does not contradict the code.

Widens the amendments ledger to admit decisions, records the D-X ruling and every altered criterion,
and corrects the doc sentences and log lines this change makes false.

### Architecture Notes

- **`.aiteam/AC_AMENDMENTS.md` is acceptance-criteria-only by its own text** — *"Acceptance criteria
  that a later piece of work changed or reversed"*, *"the AC id"*. AC-PAR-8.1 requires recording the
  **D-X ruling**, which is a decision. Widen the header to admit ACs, decisions (`D-*`), design
  rulings and deliberate deferrals; generalise "the AC id" to "the identifier (AC, D, P, SD, …)";
  extend the `Status` vocabulary to admit *ruled* / *recorded* beside *reversed in part*; add an
  *Applies to* line for entries that settle rather than amend. A sibling `DECISION_AMENDMENTS.md`
  was considered and rejected: splitting the ledger recreates the findability problem that made the
  session-local files fail.
- **The entry format and the pointer-line form are both fixed by the one shipped precedent.**
  Sections: *First stated in / Amended / Status / It used to say / It now says / Why the change is
  safe*. Pointer line, verbatim in form from `lf-setup/IMPLEMENTATION_PLAN.md:237`:
  `**AC-N has since been amended** — see [.aiteam/AC_AMENDMENTS.md](../../../AC_AMENDMENTS.md)
  (issue #N, date). The row above is left as first written.` The `../../../` depth is correct only
  from `main/<date>/<feature>/`.
- **RD-12 — pay off the never-written AC-2.8 / AC-2.9 amendment in this pass.** AC-2.8 ("one listing
  per repository… the whole per-tick API cost") is **already false**: `commentedIssues`
  (`tick.go:246`) added a second listing per repo plus a `GetIssue` per issue reachable only that
  way, and its own doc comment says so. This change makes it more false — the backlog union adds a
  third source plus up to `max_concurrent_runs` further `GetIssue` calls per tick. AC-PAR-8.3 lists
  AC-2.8 regardless, so folding in the `fix-comments-pickup` Story 5 debt costs one paragraph and
  removes the last known-false, unrecorded AC in the tick path. **AC-2.9 survives intact**
  (`InitPollCursor` and `listingWindow`'s zero-`LastTickAt` branch are untouched) — record it as
  *"reviewed, unchanged"* rather than omitting it, which AC-PAR-8.3 explicitly permits.
- **The RD-11 correction inventory**, with what each becomes:

  | Site | Status | Replacement intent |
  |---|---|---|
  | `tick.go:726`, `execute.go:293` — "locked by another daemon instance" | misleading, not false | → "another daemon **process**". Add: within this process the in-flight set makes this branch unreachable; reaching it is a dispatcher bug, and the store lock is defence in depth. |
  | `turn/doc.go:37` — "The lock is the concurrency model" | false | The store lock is the *cross-process* model. The intra-process model is the daemon's in-flight set and pool, and `internal/turn` deliberately knows nothing about it. |
  | `git/clone.go:24-25` — "safe for concurrent use" | true of the Go value, false of the repository | Keep sentence 1; add that `Fetch`, `WorktreeAdd` and `Push` against one clone contend on `refs/remotes/*`, `.git/worktrees/` and `.git/config` — measured 75%, 0.75% and (silently) 25% at parallelism 4 — that serializing them is the caller's job, and that this package declares no mutex. |
  | `daemon/doc.go` — `Tick` "an ordinary synchronous method" | false | New paragraph on the dispatch model: discovery synchronous on one goroutine, execution dispatched and joined only at shutdown. |
  | `daemon.go:427-448` — "the one goroutine this package is allotted" | false | RD-10's inventory; name `Quiesce` as the single termination condition. |
  | `daemon.go:193-197` — "its only mutable state is the backoff" | false | *"Its mutable state is the rate-limit backoff, the in-flight set and the pool, all mutex- or channel-guarded, plus the dispatch rotation cursors, which are only ever touched on the goroutine that calls Tick. Tick may therefore be called from a test on any goroutine — but it is **not** safe to call concurrently with itself, and never has been: the poll cursor is a read-modify-write."* (Strictly stronger than what it replaces.) |
  | `store/store.go:21-22` — "concurrent callers queue" | still true, now load-bearing | Keep; add that N turn workers now rely on it, and that `SetMaxOpenConns(1)` stays. |
  | `prepare.go` — the `previousRun` justification | verify | Its reasoning assumes one in-flight turn per process; restate against "one worker per session, N sessions". |
  | `lf-setup/IMPLEMENTATION_PLAN.md:58` and `APP_CONTEXT.md:280-284` | false, **and inconsistent with each other** | Reconcile both to the RD-10 text. The `logs -f` tailer vs second-signal-watcher discrepancy is a pre-existing record bug; fix it in the same pass. |
  | `comment-intent/IMPLEMENTATION_PLAN.superseded.md:37` | superseded file | Leave as written; add the pointer line only. |

- The amendments file reaches the branch through the ordinary `.aiteam/` harvest — `.aiteam/` is not
  gitignored and `status.go` uses `--untracked-files=all`.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-PAR-41 | Widen `.aiteam/AC_AMENDMENTS.md`'s header prose to admit decisions, rulings and deferrals; generalise "the AC id"; extend the `Status` vocabulary; add an *Applies to* line | infra | — | S |
| T-PAR-42 | Amendment entries: AC-PAR-8.1 (the D-X ruling), 8.2 (the AC-FC-2.6 distinction), 8.3 (AC-2.1, 2.8, 2.9, 2.12, 2.13, 2.17, 2.21, 3.1, 3.2, 3.3, 3.5, 3.8, 3.9, 3.10, 3.20, 3.22, D-A, D-B, D-7, P-3, SD-1 — or a recorded reason for leaving one alone), plus RD-6's no-`recover()` ruling against AC-3.3 and RD-12's AC-2.8/2.9 payoff | infra | T-PAR-41 | L |
| T-PAR-43 | Pointer lines under the AC-2 and AC-3 tables in `lf-setup/IMPLEMENTATION_PLAN.md`, following the `:237` precedent verbatim in form | infra | T-PAR-42 | S |
| T-PAR-44 | RD-11 doc/log corrections at every site in the table above, including reconciling the two live goroutine-inventory statements to one text | infra | T-PAR-6, T-PAR-22 | M |
| T-PAR-45 | Tests AC-PAR-8.4 (tree grep) and AC-PAR-8.7 (prefix) | test | T-PAR-44 | XS |
| T-PAR-46 | Record the deliberate deferrals with reasons: per-repo `repo add --max-parallel N` (and **how** this design leaves room for it — one extra counter in the dispatch loop), a repo component in `naming.WorktreePath`, the LF-11 in-flight view, and RD-8's rejected `runs` owner column | infra | T-PAR-23 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-PAR-8.1 | `.aiteam/AC_AMENDMENTS.md` records the **D-X ruling** in the file's section format: D-X rejected a limit on runs *per unit time* (spending control); `max_concurrent_runs` is a limit on runs *at the same instant* (resource control); different mechanism, different purpose; therefore D-X's "no task or AC may reintroduce it" does not bar it. |
| AC-PAR-8.2 | The same file records the **AC-FC-2.6 distinction** — that criterion is scoped to `CursorLookback` and reasons from "a value no operator should have to choose"; `max_concurrent_runs` is machine-shaped and the operator must choose it, so it is a `config.yaml` key. |
| AC-PAR-8.3 | The same file records an amendment for every criterion this change alters in meaning or wording, or explicitly records why one was left alone. |
| AC-PAR-8.4 | Every doc sentence and log line in the RD-11 table is either true after the change or rewritten. A grep for "locked by another daemon instance" returns nothing false. |
| AC-PAR-8.5 | `internal/daemon/doc.go` states the new dispatch model and amends the "an ordinary synchronous method" description of `Tick`. |
| AC-PAR-8.6 | Deliberate deferrals are recorded in-tree with their reason: per-repo `--max-parallel N`, a repo component in `naming.WorktreePath`, the LF-11 in-flight view, and the `runs` owner column. The design for `max_concurrent_runs` does not foreclose a per-repo bound. |
| AC-PAR-8.7 | All criteria introduced by this work carry the `AC-PAR-` prefix; none reuse a bare `AC-N.M` identifier. |

---

## Sequencing

### Land these first, green, to shrink the red commit

| Order | Tasks | Why it is safe before dispatch |
|---|---|---|
| 1 | T-PAR-1 | Non-negotiable first move. Two `slog` handlers over one buffer fail `-race` the moment anything is concurrent; fixing it later means debugging a race in the harness while debugging a race in the daemon. |
| 2 | T-PAR-18, 19, 20 | Config only. The value is read by nothing until T-PAR-3. |
| 3 | T-PAR-26, 27 | The upstream check passes today (single-threaded, the upstream is always written); its failure mode is exercised by a fake git. Independently valuable as a correctness fix. |
| 4 | T-PAR-32 | Pure validation — and it closes the one new *security* exposure before the concurrency that would exploit it exists. |
| 5 | T-PAR-11, 12 | **The key risk-reduction move.** The migration lands while `Tick` is still synchronous and the cursor hold-back still in force: `needs_poll` is written and cleared but nothing reads it, the redundancy is harmless, and the `0` backfill is correct precisely because the old mechanism still covers everything. Takes the schema change entirely out of the red commit. |
| 6 | T-PAR-28, 29, 30, 31, 33 | With one worker the gate is an uncontended no-op serializer. Its `-race` test can drive two `turn.Execute` calls directly, without the daemon being concurrent yet. |
| 7 | T-PAR-34 | Backoff changes are sequential-safe. One semantic change to flag in review: a second 403 *within* an active hold no longer increments, which alters two existing sequential assertions. |
| 8 | T-PAR-39 | Child loggers are additive, and removes a class of line-by-line churn from the red commit. |
| 9 | T-PAR-2, 3, 4 | The `inflight` and `pool` types plus the `loop` join, **with no dispatch call site yet**. `Quiesce` on an empty pool returns immediately; the new defer is a no-op. Front-loads the code most likely to need iteration. |

### The necessarily-red commit

**T-PAR-5, 6, 7, 8, 9, 13, 15, 22** must land together. There is **no ordering that keeps
`go build ./...` and `go test ./...` green across this boundary**, and the plan says so rather than
pretending otherwise. Two independent reasons: removing `Runs`/`Comments`/`Replies`/`Bootstrapped`
from `TickResult` breaks *compilation* of every test that reads them; and making `Tick` asynchronous
breaks every test that reads the DB on the line after `f.tick()` — roughly half a 1,300-line suite —
which no shim can bridge, because the shim would be the fan-out-and-join the owner rejected. Land it
as one large, deliberately reviewed commit; steps 1–9 above exist to make its diff as small as it
can be.

### Must land together, or not at all

- **T-PAR-18** cannot be split: `TestDefaultFileMatchesDefaults` fails the instant `Defaults()`
  changes without the template, and the `Sprintf` args are positional. T-PAR-19's three clamps land
  in the same commit or immediately after — one clamp without the others leaves the daemon accepting
  a value at one layer and refusing it at another.
- **T-PAR-28, 29, 30, 31.** A registry with no call site is dead code; CS1 without CS2 leaves the
  measured `push` contention live.
- **T-PAR-41, 42, 43.** Widening the ledger header without the entries it was widened for, or
  entries without pointer lines, leaves the record in a state the file's own rules forbid.

### Other notes

- `Daemon.Quiesce` is the **only exported API this feature adds**. It is used in production (the
  shutdown join), so it is not a test-only export — document it deliberately, and note that it is
  the natural seam for the deferred LF-11 in-flight view, so the next person does not add a second one.

---

## Out of Scope

- **Raising `SetMaxOpenConns(1)`.** Not needed and actively risky without `BEGIN IMMEDIATE`; if ever
  raised, the cap, `BEGIN IMMEDIATE` and per-connection FK verification change as one decision.
- **A queue for `DispositionLocked`** — LF-10's "queue and coalesce" is a separate feature, and a
  queue would break the fairness and backoff freshness this design depends on.
- **`litefactory status` or any global "what is in flight" view** (LF-11). The likely next request;
  `Quiesce`/the in-flight set is its natural seam.
- **Log rotation**, notwithstanding that concurrency multiplies volume against an unrotated
  `daemon.log`.
- **Changing `naming.WorktreePath` to include a repo component.** Refuse the collision at startup and
  at `repo add` instead; record the deferral. Fixing it would relocate every existing worktree on
  disk — a migration question of its own.
- **Per-repo `max-parallel` (`repo add --max-parallel N`). Deferred, not rejected** — the design must
  not foreclose it, and T-PAR-46 records how it fits.
- **A `runs` owner column** (RD-8) — needed only for mid-life reclaim, which RD-7 rules out.
- **A second `recover()`** (RD-6) — the exposure is accepted and documented instead.
- **Cross-repo tasks; remote workers.** Already out by decision.
- **Fan-out-and-join as a first step.** Considered and rejected by the owner: it does not deliver the
  requesting example.
- **A new module, mocking framework, metrics endpoint, or any fourth direct dependency** — including
  `errgroup`, which is a named rejected dependency and also the wrong shape.
- **Committing any test that spawns a real `claude`.**
