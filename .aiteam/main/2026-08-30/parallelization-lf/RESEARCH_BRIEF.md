# Research Brief — parallelization-lf

2026-08-30. Branch `main`. Research mode, no code written.

## Request

In the owner's words:

> What is correct is that there should only be sequential work on an issue. But we
> should work on different issues in parallel, also different repos. So the rules
> remaining: (1) only one daemon running (2) only sequential runs for a specific
> issue (e.g. two comments on issue #4 fire sequentially). What should change is
> (3) we can monitor different repositories and (4) we can monitor different
> issues per repo — e.g. if there is a run on issue #7 of repo 1 and I create
> issue #8 in repo 1, litefactory should start it in parallel.

Two invariants kept, two properties added.

| # | Rule | Status |
|---|---|---|
| 1 | One daemon process per `$LF_HOME` | **KEEP** — `flock` on `daemon.lock`, AC-2.6 |
| 2 | One issue's comments run strictly sequentially, in order | **KEEP** — AC-2.11, the `drain` invariant |
| 3 | Different repositories worked concurrently | **ADD** |
| 4 | Different issues within one repo worked concurrently | **ADD** |

---

## Codebase Context

Four codebase tours were run in parallel over `internal/daemon`; `internal/store`
+ `internal/turn`; `internal/git` + `internal/repos` + `internal/harness` +
`internal/github`; and the documentation-as-spec layer.

### Where the serialism actually lives

Three layers, and only the middle one is the feature.

1. **Daemon singleton** — `internal/daemon/lock.go`, a non-blocking exclusive
   `flock`. Orthogonal to this change and confirmed KEPT. Note one subtlety: the
   flock is per-`$LF_HOME`, not per-machine. A reader who thinks "the flock is
   why concurrency is safe" is half wrong — the flock stops two *processes*
   colliding; `sessions.lock_owner` is what will stop two *workers* colliding.
2. **One tick, one goroutine** — `Daemon.Run` spawns exactly one loop goroutine
   (`internal/daemon/daemon.go:449`); `loop` calls `d.Tick(ctx)` synchronously.
   The `time.Ticker` has a buffer of 1, so beats arriving during a long turn are
   **dropped**. This is the layer that changes.
3. **Nested sequential loops** — `poll` is `for repo → for issue → processIssue`;
   `drain` walks comments in ascending id order; `turn.Execute` blocks on
   `cmd.Wait()`.

### The good news, and there is a lot of it

The object graph was, largely by accident of other decisions, built for this.

- **No package-level mutable state** anywhere in `turn`, `harness`, `github`, or
  `store`. Every package-level var is a sentinel error, an immutable slice, or a
  compiled regexp.
- **Nearly every shared object already documents itself concurrency-safe** and
  means it: `store.DB`, `github.Client`, `redact.Redactor` (with a concurrency
  test), `report.Reporter`, `turn.Reporter`, `daemon.backoff`,
  `logging.FileSink`. The one exception is `daemon.TickResult`.
- **The session lock is already exactly the right granularity.** It is a pair of
  columns on the `sessions` row (`lock_owner`, `lock_acquired_at`), and
  `sessions` has `UNIQUE (repo_id, issue_number)`. Acquire is a single atomic
  conditional `UPDATE ... WHERE id = ? AND lock_owner IS NULL`
  (`internal/store/sessions.go:267`) — not a held transaction. `TestSessionLockIsAtomic`
  (`internal/store/store_test.go:393`) already fires 16 goroutines at one session
  and asserts exactly one winner. **It already covers the primary in-process
  contention case without knowing it.**
- **No global sequence to contend on.** `runs.ordinal` is per-session
  (`SELECT MAX(ordinal) ... WHERE session_id = ?`). `runs.id` is a plain rowid
  used only as an identity key. The only genuinely global row is `poll_cursor`
  (`CHECK (id = 1)`), and `turn` never touches it.
- **No transaction spans `claude`, git, or GitHub.** The longest hot-path
  transaction is `CompleteRunReport` at three statements. The single-connection
  pool is therefore *not* a convoy point.
- **`killSubtree` is already concurrency-correct.** It walks `pgrep -P` from this
  run's own pid and signals the pgids it finds; each child is `Setpgid` into its
  own group. It does **not** match on process name. A sibling's `claude` can
  never be swept. This was deliberate (the naive single `kill(-pgid)` was
  measured and rejected, SD-1) and it happens to be exactly right here.
- **Path derivation is discriminated.** `WorktreePath`, `Branch`, `RunLogPath`,
  `RunMetaPath`, `ClaudeSessionID` all carry the issue number or the full task
  ref. Run artefacts live under a per-issue directory.
- **The fan-out point is clean.** Strip the shared `*TickResult` and
  `processIssue` (`internal/daemon/tick.go:305`) is already a pure per-issue unit
  of work with a goroutine-local `task` struct.
- **`turn.Execute` sits behind a three-line, single-caller seam**
  (`internal/daemon/executor.go`). Confirmed by grep: there is no CLI path, no
  `litefactory run`. Concurrency can be introduced entirely above it, and
  `turn.Execute` never needs to know it is one of N.
- **`freezeSlug` is already written for a concurrent racer** — the store fills the
  branch/worktree columns only while empty and returns the winner, so two racers
  converge instead of forking the branch.
- **The serial loop is recorded as deferred, not decided.** M0's plan says so in
  as many words: *"Locking and concurrency beyond one task (LF-10). No per-repo
  parallelism cap, no queueing of comments arriving during a run."* And
  `initialplan.md` designed the feature — LF-10, and a `repo add --max-parallel N`
  flag that was drawn and never built. **There is no "one at a time" commitment
  anywhere in the record.** This is the strongest documentary support the change
  has.

### The blockers, in descending order of how much they cost to find late

> **MEASURED 2026-08-30 — see `SPIKE.md`. This section's git conclusions are
> corrected by probe, and two of them were wrong:**
>
> - `git fetch --prune` contention is **real but differently caused**: not
>   `FETCH_HEAD.lock`/`packed-refs.lock`, but `error: cannot lock ref
>   'refs/remotes/origin/main': is at X but expected Y`. Rate: **0/400 when
>   nothing is fetched, 180/240 (75%) when refs actually move.**
> - `git push --set-upstream` fails the **opposite** way to the prediction:
>   **exit 0**, objects landed, `could not lock config file .git/config` printed
>   in 89/240 runs, and **60/240 branches left with no upstream**. Silent, not
>   loud. `push.go:47`'s `err != nil` check cannot see it.
> - **`git worktree add` DOES contend** — 3/400 (0.75%), `fatal: failed to read
>   .git/worktrees/<sibling>/commondir`. The "good news" below is false. Linked
>   worktrees do get their own index; the race is on the shared registry in
>   `.git/worktrees/`.
>
> Consequence: the per-repo mutex must cover **fetch, worktree add and push**,
> not fetch alone.


**1. Two concurrent `git fetch --prune` against one clone fail hard.**
This is the hidden blocker. Both turns run the *identical* command
(`internal/git/clone.go:98`) against the same `.git`. They collide on
`FETCH_HEAD.lock`, on `refs/remotes/origin/*.lock`, and — because of `--prune` —
on `packed-refs.lock`. Git's lock-file paths die on first `EEXIST`; they do not
block and do not retry. Nothing in litefactory retries either: `runner.run`
(`internal/git/exec.go:138`) has no retry loop, so a collision surfaces as an
ordinary `*git.CommandError` and **fails the whole turn with a failure comment**.
Nothing is corrupted — that is git's design — but the turn is lost.

There is a matching TOCTOU: `resolveBranchStart` reads `refs/remotes/origin/<x>`
with `show-ref` and hands the *name* to `worktree add` several commands later. A
concurrent `fetch --prune` can delete that ref in the window, producing
`fatal: invalid reference` — a failure that reads like a bug.

**2. `git push --set-upstream` is the most misleading failure mode found.**
`--set-upstream` writes `branch.<name>.remote` / `.merge` into the **shared
`.git/config`**, taking `.git/config.lock`. Two concurrent pushes on one clone:
the second exits non-zero **after its objects have already reached GitHub**. The
turn then reports *"the push failed, your commit is in the worktree"* for a
branch that is, in fact, on the remote.

**3. But `git worktree add` does *not* contend** — and this is what makes the
whole feature tractable. A linked worktree gets its own index at
`.git/worktrees/<name>/index`; the main `.git/index` is never touched. Distinct
issues mean distinct worktree names and distinct branch refs. This is the
load-bearing claim of the design and it is reasoned, not measured — **it needs a
probe** (see Open Questions).

**4. `TickResult` is a shared mutable accumulator.** It is passed by pointer from
`Tick` down through `poll → processIssue → {bootstrap, drain} → {run, reply}`.
Six of its twelve fields are non-atomic `++` from the per-issue path: `Issues`,
`Bootstrapped`, `Comments`, `Runs`, `Replies`, `Deferred`. There is a second,
easier-to-miss accumulator: `pending *time.Time`, a **min-reduction** threaded
alongside it, which needs different treatment from the counters.

**5. Deferral pins the *global* poll cursor.** `poll` holds the single-row cursor
back to the earliest deferred issue's `UpdatedAt`. Under concurrency: tick N's
run on issue #7 is still going when tick N+1 discovers #7, drains it, hits the
lock, defers — and rewinds the shared cursor **for every repo** for the duration
of the run. With a 30-minute run that is a 30-minute cursor rewind applied fleet-
wide. This cannot happen today because tick N+1 cannot start until tick N ends.
It is the most consequential second-order effect found and it is invisible in the
code as written.

**6. The backoff over-doubles.** `backoff.fail` assumes it is called once per
rate-limit event — guaranteed today because one 403 unwinds the entire tick. With
N goroutines each receiving a 403 in the same tick, `failures` increments N times
and the delay becomes `retryAfter · 2^(N-1)`. With 5 repos and a 60s `Retry-After`
that is 16 minutes instead of 2, capped at 1h. A daemon parked for an hour is, in
the words of the code's own comment, *"indistinguishable from one that has died"*.

**7. `DispositionLocked` is a drop, not a defer.** The loser posts nothing,
consumes nothing, and is retried only by the next poll — a latency floor of one
full tick (10–60s) after each run finishes. It is also **effectively dead code in
production today**: with one daemon and one goroutine, the lock is never
contended. Concurrency promotes it to the primary flow-control mechanism.

**8. A latent worktree path collision.** `naming.WorktreePath` derives
`<root>/task-<issue>` with **no repo component**, and
`repos.resolveWorktreeRoot` appends a per-repo component on only one of its three
paths. So a global `worktree_root` maps repo A issue #42 and repo B issue #42 to
the *same directory*. Today this fails loudly and safely. Concurrently, both
goroutines can pass `checkVacant` before either directory exists — two turns
holding two **correctly granted, different** session locks, operating on one
directory. The session lock structurally cannot prevent this.

**9. `fixture_test.go` will fail `-race` on day one.** It builds two `slog`
handlers over one `*bytes.Buffer` (`:697-698` and `:764`). Each handler holds its
own mutex; they do not serialize against each other. `make test` runs `-race`.
**This is a prerequisite, not a follow-up**, and it touches the file every daemon
test depends on.

### The decision the request does not settle

This is the finding that most shapes the work, so it is stated plainly.

The owner's example is: *"if there is a run on issue #7 of repo 1 and I create
issue #8 in repo 1, litefactory should start it in parallel."*

A `Tick` that fans out and joins **does not deliver that**. It turns
`sum(issues)` into `max(issues)`, but the tick still does not return until its
slowest issue finishes, and the `time.Ticker` (buffer 1) drops the beats in
between. With a 30-minute run on #7, a joining `Tick` still would not *notice*
#8 until that run completed.

Delivering the owner's sentence literally requires decoupling **discovery** from
**execution**: `Tick` polls and dispatches, a bounded pool drains issue queues
asynchronously, and discovery skips sessions already in flight.

The two shapes differ enormously in cost:

| | Fan-out and join | Decoupled dispatch |
|---|---|---|
| Delivers the owner's example | **No** | Yes |
| `Tick` stays synchronous/awaitable | Yes | No |
| Cursor semantics | Unchanged | Must be redesigned |
| `internal/daemon` tests rewritten | ~5% | ~50% |
| `doc.go` invariant *"an ordinary synchronous method"* | Survives | Must be amended |

They are not mutually exclusive: fan-out-and-join is a strictly smaller step that
can ship first and does not foreclose the other. But it should not be shipped
*believing* it satisfies the request, because it does not.

> **RESOLVED by the owner, 2026-08-30 — Decoupled dispatch.**
> Asked to choose between the two shapes, the owner answered *"of course option
> B"* — B being decoupled dispatch, described to them as "one part looks, another
> part does; a long run never freezes discovery". Open question 1 below is
> therefore closed: **`Tick` no longer waits for runs.** The fan-out-and-join
> shape is not a fallback to reach for during Plan mode; it was considered and
> rejected because it does not deliver the requesting example. The consequences
> the table above prices in — cursor redesign, ~50% of the `internal/daemon`
> tests, and an amendment to the `doc.go` "ordinary synchronous method"
> invariant — are accepted as in scope.


---

## Domain / Problem Space

### What must be preserved, precisely

The comment-queue invariant (`internal/daemon/doc.go:27-41`) is the one rule that
is *"not obvious and is expensive to get wrong"*:

> `store.DB.AdvanceWatermark` takes a MAX and never rewinds, so consuming comment
> 12 after failing to report comment 11 puts the watermark past 11 for ever: the
> report is never retried and nothing ever says so. The loop therefore checks the
> watermark itself after each comment — the store, not an inference from a return
> value — and stops that task's queue at the first comment the watermark did not
> move past.

Three consequences for a concurrent design:

1. **The partition key must be the session.** Per-issue serialization is
   preserved *for free* if one session is only ever drained by one worker and
   `drain` is called unchanged. Anything finer-grained destroys it.
2. The watermark re-read must stay a **store read**, not a value passed back from
   a worker. The expensive case is precise: a post that reached GitHub but whose
   recording transaction did not commit returns a comment id *and* an error, and
   only the watermark knows which to believe.
3. `processIssue` learns the session id only *after* the closed-issue skip, so a
   fan-out keyed on `(repo.ID, issue.Number)` is dispatching before the session
   exists. That mapping is 1:1 (`GetOrCreateSession` is keyed on exactly those
   two columns), so it is sound — but it is implicit and stated nowhere.

### Sequentiality has a scope question

The session lock is released when `turn.Execute` returns. **Reporting happens
afterwards, in the daemon, outside the lock.** Today that window is empty. Under
concurrency a second turn on the same issue could acquire the lock and start
running while the first turn's report is still posting. No corruption — the
replay guard and watermark still hold — but the *thread* can show run 2 starting
before run 1's report lands.

If "strictly sequential per issue" is to mean what a reader of the GitHub thread
would expect, sequentiality must cover **execute *and* report** as one unit.

### The session lock's changed role

It survives intact as the mutual-exclusion primitive and needs no code change for
correctness. What changes is its *documented meaning*, and in three load-bearing
ways:

- `lock_owner` holds `opts.InstanceID`, which is **per-process**. Every worker in
  the daemon shares it. So the lock cannot distinguish sibling goroutines, and
  `ReleaseSessionLock(sessionID, instanceID)` offers no protection against a
  future caller releasing a live sibling's lock. Today the only release is the
  acquiring goroutine's own defer, so this is latent.
- Two log lines become **factually false**: `internal/daemon/tick.go:726` and
  `internal/turn/execute.go:293` both say *"task is locked by another daemon
  instance"*. Under concurrency the contender is a sibling goroutine in the same
  instance. This codebase treats an untrue comment as a defect.
- `internal/turn/doc.go:37` heads a section **"The lock is the concurrency
  model"** — *"One task, one lock, no queue... Everything else — two daemons, a
  crash mid-run, the same comment arriving twice — is handled by that."* The
  enumeration is "two daemons", not two goroutines. After this change the lock is
  one of two mechanisms; the pool's partition is the other.

There is also a real hole: **a wedged worker's lock is never reclaimed while the
process lives.** `ReconcileOrphans` releases locks `WHERE lock_owner <> instanceID`,
which by construction excludes the daemon's own. `lock_acquired_at` is written but
never read at runtime, so there is no TTL and no reaper.

### Reconciliation

`ReconcileOrphans` is already N-safe by construction: no `LIMIT`, no
`WHERE session_id = ?`, it fails every `running` row and releases every foreign
lock in one transaction, and `Start` already loops over the result.

But the scoping is **asymmetric**: locks are scoped by ownership
(`lock_owner <> instanceID`), runs by nothing at all (`status = 'running'`) —
the `runs` table has no owner column. That is correct today because
reconciliation runs once, at startup, before any turn. With N workers the
invariant changes shape from *"no run is in flight in this process"* (trivially
true) to *"no worker goroutine has been started yet"* (still satisfiable, but now
something a person must hold deliberately).

If any mid-life reclaim, TTL, or stuck-worker reaper is ever wanted, `runs` needs
an owner column and that is a schema migration. Worth deciding now rather than
later. The failure mode if the invariant were violated is not subtle: the
sibling's row is flipped to `failed/abandoned`, an abandonment comment is posted,
and then the still-running sibling posts a *second*, contradictory report for the
same trigger comment.

### Resource cost

litefactory's own per-run footprint is negligible — ~200 KB of buffers, exactly
two goroutines, five fds. **The constraint is the `claude` process tree**: a Node
process plus an unbounded tool subtree (the kill code's own bounds —
`maxProcessNodes = 4096`, `maxProcessDepth = 32` — are the codebase's admission
that this is unbounded in practice). Several concurrent `go build`s alone will
saturate a laptop.

Secondary constraints, in the order they bite: GitHub **secondary** rate limits
(which target concurrent-request patterns specifically, and which nothing in the
process coordinates); the per-repo git mutex, above which concurrency on a single
repo converts to queueing rather than throughput; N simultaneous full checkouts;
and N× the Anthropic burn rate. The default `claude.timeout_seconds` is 1800, so
slot occupancy is long.

This is a machine-shaped number, not a code-shaped one. It wants a conservative
default and a probe on real hardware, not a derivation.

---

## Constraints & Risks

### Design constraints the codebase imposes

- **`internal/daemon` owns "when", `internal/turn` owns "what"** — a named M0
  invariant. A worker pool, a queue and a dispatcher are all "when", so they
  belong in `daemon`. Good news: **nothing in `internal/turn` needs to change for
  the daemon to go concurrent.**
- **The goroutine-budget convention.** M0's Global Invariants say *"Goroutine
  inventory is exactly four... A fifth requires justification in review."*
  `daemon.go:438-448` is a doc block headed *"Why the loop is on its own
  goroutine"*. The convention a pool must satisfy, assembled from the three
  existing sites: say how many; name the termination condition as a single named
  thing; say what happens when the bound is exceeded; say what the goroutine buys
  in the measured units of the thing it protects. A *variable* fifth goroutine is
  a bigger change than that sentence anticipates.
- **`internal/git` cannot host the lock.** It is a leaf whose `Clone` doc promises
  *"safe for concurrent use: it holds no mutable state"* — true of the Go struct,
  **false of the git repository it points at**. A package-level map+mutex there
  would falsify the comment and hide the serialization from every caller. That
  sentence (`internal/git/clone.go:27`) is the single most likely thing in the
  tree to mislead an implementer into skipping the lock.
- **There is no per-repo object to hang a lock on.** A repo is a `store.Repo` row
  value re-read every tick and every turn. A keyed registry is needed;
  `store.Repo.Path` is the natural key (canonical, symlink-resolved, the table's
  unique key).
- **Config conventions**: strict `KnownFields(true)` decoding; defaults applied by
  decoding *on top of* `Defaults()` so an explicit `0` is distinguishable from
  absent and can be *refused*; out of range is an error, never a clamp; bounds as
  exported consts with a comment saying **why** each bound is where it is;
  validation aggregates via `errors.Join`; the shipped template is interpolated
  from `Defaults()` and `TestDefaultFileMatchesDefaults` fails if you add a key
  and not a template line.
- **Leave `SetMaxOpenConns(1)`.** The burden of proof is on raising it. Two
  concrete reasons: `verifyForeignKeys` only proves the FK pragma for *one*
  connection, so with N the invariant rests on driver behaviour rather than a
  startup assertion; and `withTx` uses `BEGIN DEFERRED` with ten of eleven
  transactions reading-then-writing — the exact pattern that produces
  `SQLITE_BUSY_SNAPSHOT`, which `busy_timeout` does **not** retry. Raising the cap
  without `BEGIN IMMEDIATE` converts a queue into intermittent hard errors. If it
  is ever raised, the cap, `BEGIN IMMEDIATE` and per-connection FK verification
  must change as one decision.

### The AC/decision inventory

The written record is the spec here. Assessment of every identifier in the blast
radius:

**Unaffected** — AC-2.5, AC-2.6 (the singleton, explicitly preserved), AC-2.7,
AC-2.10/2.10b, **AC-2.11** (the ordering invariant — preserved for free by a
per-session partition), AC-2.16a–d, AC-2.22/2.22b, AC-2.23, AC-3.4, AC-4.1 (in
substance), AC-4.9, AC-4.10 (in substance), AC-5.x, D-D, D-I, D-8, SD-8.

**Meaning changes** — AC-2.8 (tick cost is the same count but arrives as a
*burst*), AC-2.9 (*"after each successful tick"* — what is a successful tick when
repos finish at different times?), AC-2.12 (backoff must gate every worker),
AC-2.13 (what is the failure unit?), AC-2.17 (the 👀 latency guarantee gets
*better*), **AC-3.1** (the most load-bearing entry — the lock survives, its
stated role does not), AC-3.2 (instance id is per-process; a wedged worker's lock
is unreclaimable), AC-3.5 / 3.8 / 3.9 (the git contention above), AC-3.20,
AC-3.22, D-A, D-7, P-3.

**Needs restating** — AC-2.1 and AC-2.21 and D-B (all say *"an in-flight run"*,
singular; the store is already plural, every prose statement is not), AC-3.3
(below), AC-3.10 (*"unchanged versus pre-turn"* is only well-defined when one turn
runs at a time — the guarantee survives, the assertion form does not, and this is
the product's headline test), SD-1.

**A genuine conflict** — **AC-3.3** promises lock release on every exit path
*"including panic"*. In a worker goroutine an unrecovered panic kills the whole
process and abandons every sibling run. Isolating it needs a second `recover()`,
which APP_CONTEXT forbids **by name**: *"The only `recover()` in the tree... Do
not add a second."* Either the invariant is amended or the exposure is accepted
and written down. It cannot be quietly ignored.

**A possible blocker in the record** — **D-X**: *"Rejected for M0: run-rate
circuit breaker. No rate limiting until LF-11/M1. **No task or AC may reintroduce
it.**"* A `max_concurrent_runs` key is a run-rate bound. D-X was "superseded" once
— but only inside a document that is *itself* superseded, and the amendment that
supersession required was never written. **The record does not currently say
whether D-X is live.** This needs an explicit ruling before any cap is proposed.

**Two defects in the record itself**, both of which a parallel plan will trip
over:

- **The AC namespace collides.** `AC-3.1` means *"the per-task lock is acquired
  before preparation"* in `internal/turn/execute.go:285` and *"an unknown profile
  name refuses the run"* in `internal/daemon/tools_test.go:152`. Same for AC-2.3,
  AC-2.9, AC-2.10, AC-2.12, AC-3.7. Nothing disambiguates them but the package a
  comment sits in. Two plans solved this with a prefix (`AC-FC-x`, `AC-CI-x`);
  the tool-permissions plan did neither, which is how the collision arose.
- **An amendment was specified and never written.** `fix-comments-pickup` Story 5
  required AC-2.8 and AC-2.9 to be restated in an `AMENDMENTS.md`. `ListingOverlap`
  and the second per-repo listing shipped; the amendment did not. Both criteria
  are squarely in this feature's blast radius and **their current text is already
  false**.

### Observability

Logs are the entire surface — there is no metrics endpoint, by decision, and
`daemon.log` has no rotation (accepted M0 debt, *"do not add lumberjack to fix
it"*). Concurrency multiplies log volume against an unrotated file.

The per-turn logs are already interleave-safe: `turn.Execute` binds `session_id`,
`issue`, `trigger_comment_id`, `instance_id`, `task` at the top. **The gap is
above the turn** — `poll`, `processIssue`, `bootstrap` and `drain` all log on the
bare `d.log`, so any new line added inside `drain` carries no task identity. The
fix follows the existing vocabulary rather than inventing one: a per-repo child
logger in `poll`'s loop, a per-task child logger built in `processIssue` right
after `ParseRef`. A `worker` key would be an invention; `task` already
disambiguates any two lines. A `tick_id` is the one genuinely new field, and it
is only needed if ticks can overlap.

`d.now()` is currently called **only from `Tick` itself, never below `poll`** —
worth preserving deliberately, because the test clock advances one second per
read and the moment a per-issue goroutine reads it, the `since`-string assertions
go flaky.

**The operator has no global view today.** `litefactory status` was designed in
`initialplan.md` and explicitly deferred (LF-11). `daemon status` reports on the
daemon *process* and says nothing about runs — safe under concurrency and
simultaneously useless for it. With four issues in flight this becomes the first
thing anyone notices.

### Test impact

`internal/daemon/tick_test.go` is ~1,300 lines / 29 tests. The dominant pattern is
**drive the tick, then read the database and the fake forge** — `f.watermark()`,
`f.turns.triggers()` compared with `slices.Equal` against ordered literals,
`f.forge.snapshot()`. `TickResult` fields are read in only ~12 places.

- Under fan-out-and-join: **~95% survive unmodified**. Single-issue tests are
  untouched by definition. One test breaks and *should* — `TestDeferredTaskDoesNotStallTheOthers`
  asserts an exact trigger order across two different issues, which is precisely
  the property being changed.
- Under decoupled dispatch: **~50% break**, because every test reads the database
  on the line after `f.tick()`. They would all need a settle primitive — which is
  the same thing as an awaitable `Tick`, spelled differently and only in tests.

**Is a joining `Tick` a legitimate design or a smell?** Honestly, both readings
are available. It is legitimate — it is a structured-concurrency scope, exactly
`errgroup`'s shape, and the codebase has already committed to it in prose
(*"Every acceptance criterion about polling is asserted by calling it directly"*).
It becomes a smell only if it is kept **specifically because the tests need it**,
against a product requirement pointing the other way. The tell would be a
`time.Sleep` or an `Eventually` appearing in `internal/daemon`'s tests to work
around it.

Also worth noting: **nothing anywhere has ever run two `turn.Execute` calls
concurrently.** `internal/turn`'s tests call it serially; `internal/daemon`'s
tests never call the real one. `stubRunner` is already built to act *"at the exact
moment the subprocess would have been running"*, so the hook for such a test
exists.

---

## Open Questions

Ordered by how much they change the shape of the answer. Item 1 is closed;
items 2–3 remain for the owner, the rest are for Plan mode.

1. ~~**Fan-out-and-join, or decoupled dispatch?**~~ **CLOSED 2026-08-30 —
   decoupled dispatch.** The owner chose the shape in which polling genuinely
   never blocks on a run. `Tick` stops being synchronous-and-awaitable, the poll
   cursor is redesigned, and the `doc.go` invariant is amended. Questions 4, 5
   and 6 below are now *live* rather than conditional, because they only arise in
   this shape. See *The decision the request does not settle*.

2. **What is the resource ceiling, and where does it live?** N concurrent
   `claude` processes is expensive and nothing bounds it today. Three competing
   precedents exist in the record: `AC-FC-2.6` says the analogous tuning value is
   *"a Go constant with a documented rationale... not a `config.yaml` key"*;
   `initialplan.md` puts it on `repo add --max-parallel N` (a repo column + a
   migration); `poll_interval_seconds` is the config-key precedent. All three are
   defensible. Related: **does D-X still bind?**

3. **Does "strictly sequential per issue" cover reporting, or only execution?**
   The lock is released when `Execute` returns; the report posts after. Today the
   window is empty. This determines whether the unit of mutual exclusion is
   `Execute` or `Execute`+`ReportTurn`.

4. **Should a long-running issue block its own repo's polling?** And what is
   fairness across repos — `ListRepos` order is stable, so a naive bounded pool
   could starve the last repo indefinitely.

5. **Is the deferral-pins-the-global-cursor interaction acceptable**, or does the
   cursor become per-repo (a migration — `poll_cursor` has `CHECK (id = 1)`) or
   disappear in favour of a local "sessions whose watermark is behind" query? Note
   the third option needs a *new discovery path*: the 5-minute `ListingOverlap`
   is not sufficient, because an issue deferred for 30 minutes has an unchanged
   `UpdatedAt` — which is the exact shape of the issue-#3 loss the overlap was
   added to fix.

6. **On a mid-tick rate limit, are siblings cancelled or allowed to finish?**
   Cancelling via a derived context would also kill in-flight `claude` runs, which
   is almost certainly wrong. Letting them finish means the tick's error is not
   known until join. And should **secondary** limits be scoped differently from
   primary ones, now that concurrency makes them materially more likely?

7. **Does the instance id gain a worker component?** Without one, a wedged
   worker's session lock is unreclaimable while the process lives. Changing its
   shape touches `sessions.lock_owner`, `ReconcileOrphans` and AC-3.2's wording.

8. **Does `runs` gain an owner column?** Needed only if mid-life reclaim is ever
   wanted; it is a schema migration, and this codebase's convention is one `.sql`
   file per change. Cheaper to decide now.

9. **AC-3.3's panic tension** — amend the no-second-`recover()` invariant, or
   accept and document that one worker's panic abandons every sibling run?

10. **How is the AC namespace collision resolved** for this plan's own criteria —
    a prefixed family (`AC-PAR-x`), or continued M0 numbering?

11. **Should the never-written AC-2.8 / AC-2.9 amendment be paid off in this
    pass?** Both are in the blast radius and both are already false as written.

12. **Is a minimal "what is in flight" view in scope**, or deferred to LF-11
    again?

### Probes required — empirical, not answerable by reading code

The codebase has a convention for exactly this: probe read-only in a scratch
directory outside the repo, write it up in a document stating the version verified
against, and do **not** commit a test that spawns a real `claude`.

- **P1 — Can two `claude` processes run concurrently?** Neither
  `CLAUDE_CLI_CONTRACT.md` nor `TOOL_SURFACE_PROBE.md` says a word about it
  (grepped). Unknown: whether `~/.claude/` holds a shared index/history/statsig
  file two processes write; whether the CLI locks its own state dir; whether
  concurrent Keychain lookups succeed; whether there is an Anthropic-side
  concurrency ceiling that would surface as an ordinary run failure. **This is a
  feature-level go/no-go and nothing in the repo answers it.** Extend
  `CLAUDE_CLI_CONTRACT.md` rather than starting a third document.
- ~~**P2 — Confirm two `git worktree add` on one clone do not contend**~~ **RUN
  2026-08-30 — they DO contend, 3/400. See `SPIKE.md`.** Original text:, and
  confirm two `git fetch --prune` do. The first is the load-bearing claim of the
  whole design and it is currently reasoned, not measured. `internal/gittest`
  gives a real clone with a real local bare origin, offline — 100 iterations of
  two goroutines. Record the exact stderr strings.
- **P3 — Does `git worktree add` implicitly prune a worktree being created
  concurrently?** `gc.worktreePruneExpire` should protect a fresh admin entry, but
  the window between writing `.git/worktrees/task-N/gitdir` and the directory
  existing is exactly what a prune looks for.
- **P4 — What does a detached `gc --auto` do to a concurrent turn?** `git fetch`
  triggers it, it forks into the background (`gc.autoDetach=true`), and it holds
  `packed-refs.lock` past the fetch's return. `clone.go:98` passes no
  `-c gc.auto=0`.
- **P5 — `max_concurrent_runs` default on real hardware.** A machine-shaped
  number.

---

## Recommended scope for Plan mode

### In scope

1. **Answer question 1 first.** Nothing else can be planned until the concurrency
   shape is chosen. Recommend planning it as two user stories — a per-session
   partitioned fan-out first, decoupled dispatch second — so the smaller step can
   ship and be measured without foreclosing the larger one.
2. **A per-repo git mutex.** Keyed on `store.Repo.Path`, owned by `internal/daemon`,
   injected as a `turn.Options` field (matching the existing
   "Options + validated dependencies at wiring time" pattern). **Not** in
   `internal/git`. Two critical sections, and the boundaries matter:
   - **CS1** — one acquisition covering `fetch` *through* `ensureWorktree`. Not
     two, because releasing between them reintroduces the ref TOCTOU the lock
     exists to prevent.
   - **CS2** — around `Commit` + `Push`.
   - **The `claude` run sits between them and must be outside both.** That is the
     entire value of the design.
   - The existing git deadlines are safe with a mutex because `runner.run` starts
     its timeout at exec time, and neither `daemon` nor `turn` sets an ambient
     deadline (verified). Preserve that property.
3. **Make `TickResult` safe** — per-goroutine results merged at the join is
   cleaner than a mutex, because it keeps `Daemon`'s *"its only mutable state is
   the backoff"* doc sentence true. Handle `pending` as a min-reduction. Resolve
   the `Runs++`-before-the-`DispositionLocked`-check ambiguity deliberately —
   either move it or rename the field, but do not leave it ambiguous.
4. **Fix the backoff double-count** — first-failure-wins per tick, an epoch
   counter, or a single post-join `observe`.
5. **Fix `fixture_test.go`'s shared buffer.** A prerequisite.
6. **The documentation work is part of the change, not a follow-up.** At minimum:
   `daemon/doc.go` (the tick and comment-queue sections), `turn/doc.go:37` ("The
   lock is the concurrency model"), `git/clone.go:27` (make the concurrency claim
   precise), the two false log lines, `store/store.go`'s "concurrent callers
   queue" sentence, `prepare.go`'s `previousRun` justification, M0's goroutine
   inventory, and `AC_AMENDMENTS.md` entries for every criterion in the
   "needs restating" list.
7. **Probes P1 and P2 before committing to the design.** P1 especially — it is a
   go/no-go.
8. **At least one test that would fail without the synchronization.** A `-race`
   run over a fixture with 2 repos × 3 issues is the cheapest. Plus a turn-level
   contention test: two `Execute` calls at one session, asserting exactly one runs
   row, one report, and that the loser consumed nothing. Nothing in the tree
   tests this today.

### Out of scope — recommend explicitly excluding

- **Raising `SetMaxOpenConns(1)`.** Not needed, and actively risky without
  `BEGIN IMMEDIATE`. If measurement later says otherwise it is its own decision.
- **A queue for `DispositionLocked`.** Making the loser wait rather than drop is a
  real improvement but it is LF-10's "queue and coalesce", a separate feature.
- **`litefactory status`.** Worth flagging as the likely next request; not this
  change.
- **Log rotation**, notwithstanding that concurrency multiplies the volume.
- **Changing `naming.WorktreePath` to include a repo component.** The collision is
  real but pre-existing, only reachable with a global `worktree_root`, and fixing
  it relocates every existing worktree on disk — a migration question. Recommend
  **validating against it at startup** instead, and recording the deferral.
- **Cross-repo tasks, remote workers.** Already out by decision.

### A note on sequencing

Plan mode should treat the D-X ruling (question 2) and the P1 probe as **gates**,
not as tasks. If D-X binds, the cap needs an amendment before it can be specified.
If P1 comes back badly — a shared `~/.claude` lock, or an Anthropic-side ceiling —
the feature's shape changes fundamentally and the brief should be revisited rather
than the plan patched.
