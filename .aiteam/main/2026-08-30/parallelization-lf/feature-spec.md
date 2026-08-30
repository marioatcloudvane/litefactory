# Feature Spec: Concurrent turn execution (parallelization-lf)
# Status: APPROVED
# Date: 2026-08-30

## Goal

litefactory works different issues and different repositories concurrently, bounded by one
daemon-wide cap, while keeping one daemon process and strictly sequential runs per issue — so a
long run on one issue never delays discovery or execution of work on another.

## Sources of truth

| Document | Standing |
|---|---|
| `.aiteam/main/2026-08-30/parallelization-lf/SPIKE.md` | **Measured. Wins over all prose.** |
| `.aiteam/main/2026-08-30/parallelization-lf/RESEARCH_BRIEF.md` | Context, minus the sections its own "MEASURED 2026-08-30" banners correct |
| `.aiteam/APP_CONTEXT.md` | Conventions, security invariants, package map |
| `.aiteam/AC_AMENDMENTS.md` | Format for every record change this spec requires |

The brief's claim that `git worktree add` does not contend is **false**. The brief's prediction
that `push --set-upstream` fails loudly is **false** — it fails at exit 0.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As the owner, I want a run in flight on issue #7 to not stop litefactory noticing and starting issue #8, so that a 30-minute run does not freeze the whole daemon |
| US-2 | As the owner, I want different repositories worked at the same time, so that one busy repo does not queue every other |
| US-3 | As the owner, I want two comments on one issue to still run one after the other in comment order, so that my second instruction never overtakes my first |
| US-4 | As the operator, I want one number in `config.yaml` that bounds how many runs exist at once, so that concurrency cannot saturate the machine |
| US-5 | As the operator, I want every repo to make progress even while another repo has continuous work, so that repo 2 is not starved behind repo 1 |
| US-6 | As the operator, I want concurrent git operations against one clone to be serialized, so that turns do not fail with git ref-lock errors that read as flakes |
| US-7 | As a future maintainer, I want each invariant this change alters recorded in-tree, so that the written record does not contradict the code |

## Data Model

No schema migration is mandated by this spec. Two candidate migrations are architect decisions
(see *Required Decisions* RD-2, RD-8).

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `config.Config` | `MaxConcurrentRuns` (`max_concurrent_runs`) | `int` | No — defaulted | Default `3`; valid `1..16`; out of range is a startup error, never a clamp; an explicit `0` is refused (decode-on-top-of-`Defaults()`); bounds are exported consts with a comment stating *why* each bound is where it is |
| `store.Session` | `lock_owner`, `lock_acquired_at` | existing | — | Unchanged columns; **role changes** — becomes real in-process mutual exclusion between sibling workers, not only between daemon instances |
| `store.PollCursor` | `tasks_updated_since` | existing | — | Single row (`CHECK (id = 1)`); its scope is RD-2 |
| `daemon.TickResult` | 6 counters + `pending` | existing | — | Must stop being a shared mutable accumulator (AC-PAR-4.1) |

### Relationships

- `max_concurrent_runs` → daemon-wide run slots (**not** per repo, **not** per issue)
- run slot → at most one `turn.Execute` in flight
- session (`UNIQUE (repo_id, issue_number)`) → at most one worker, ever — this is what preserves per-issue ordering
- `store.Repo.Path` → one per-clone git mutex (keyed registry; there is no per-repo object today)

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| config | `max_concurrent_runs: <int>` in `config.yaml` | operator-authored file | New key. Must be added to `Defaults()`, to `internal/config/template.go`, and validated in `Validate()` via the aggregating `errors.Join` pattern. `TestDefaultFileMatchesDefaults` will fail if the template line is missed |
| internal | `daemon.Tick` | — | **Stops being synchronous-and-awaitable.** Dispatch returns without joining runs. `internal/daemon/doc.go`'s "an ordinary synchronous method" invariant is amended, not preserved |
| internal | daemon-owned per-clone git mutex | — | Injected into `turn` as a `turn.Options` field, matching "Options + dependencies validated at wiring time". **Never** in `internal/git` (leaf; would falsify `clone.go:27`) |
| internal | `git.Worktree` push path (`internal/git/push.go:47`) | — | Gains an exit-code-independent upstream check. git exits 0 while failing to write `branch.<n>.remote` |
| CLI | none | — | No new command, no new flag. `repo add --max-parallel N` stays unbuilt (deferred, see Out of Scope) |
| tests | daemon test settle primitive | — | ~50% of `internal/daemon`'s ~1,300-line tick suite reads the DB on the line after `f.tick()` and must be re-shaped. A `time.Sleep` or polling `Eventually` in `internal/daemon` tests is a rejected solution |

## Business Rules

### Preserved invariants

- **One daemon process per `$LF_HOME`.** The `daemon.lock` flock singleton (AC-2.6) is unchanged, byte for byte.
- **One issue's comments run strictly sequentially, in ascending comment id order** (AC-2.11). The partition key is the **session**; anything finer destroys it.
- **The watermark re-read stays a store read** after each comment, never a value passed back from a worker. `AdvanceWatermark` takes a MAX and never rewinds.
- **Post first, then commit.** Unchanged.
- **`internal/daemon` owns "when", `internal/turn` owns "what".** Pool, queue and dispatcher are all "when". Nothing in `internal/turn`'s business logic changes for concurrency.
- **`SetMaxOpenConns(1)` stays.** Not a convoy point — the longest hot-path transaction is three statements and none spans `claude`, git or GitHub.
- **The child-environment allowlist, `--tools` boundary, redaction ordering and `killSubtree`'s pid-derived sweep are untouched.** `killSubtree` is already concurrency-correct and must stay that way.

### Concurrency

- The ceiling is `max_concurrent_runs`, **one daemon-wide number shared across all repos** (`poll_interval_seconds` precedent). This is a deliberate departure from AC-FC-2.6's "a Go constant, not a `config.yaml` key" precedent and the departure must be recorded.
- `max_concurrent_runs: 1` must reproduce today's observable serial behaviour.
- Slots refill **one at a time as each run finishes** — never a batch barrier that waits for all N.
- A comment queued behind a busy session occupies **no** slot.
- Discovery must never block on a run. Polling continues at `poll_interval_seconds` regardless of what is in flight.
- Discovery must skip sessions already in flight without consuming or posting.
- **Fairness is a required deliverable with an explicit written justification**, not whatever `ListRepos` order produces. A daemon-wide cap plus list-order dispatch starves the last repo indefinitely.

### Git serialization (measured, not precautionary)

Probed at parallelism 4 on git 2.50.1 / macOS, mirroring litefactory's exact argv:

| Command | Site | Measured failure |
|---|---|---|
| `git fetch --prune` | `internal/git/clone.go:98` | **180/240 (75%)** when refs move; 0/400 when nothing to fetch. `cannot lock ref 'refs/remotes/origin/main': is at X but expected Y` |
| `git worktree add` | `internal/git/clone.go:138` | **3/400 (0.75%)**, fatal and unretried. `failed to read .git/worktrees/<sibling>/commondir` |
| `git push --set-upstream` | `internal/git/push.go:47` | **0/240 non-zero exits.** `could not lock config file .git/config` printed 89/240; **60/240 branches left with no upstream.** git exits 0 |

- A **per-clone mutex covering `fetch`, `worktree add` and `push` is a measured requirement.** Fetch alone is insufficient.
- Retry-on-lockfile is the **wrong fix** for fetch: it is a losing race to update the same remote-tracking ref, not a stale lockfile.
- **CS1 is one acquisition covering `fetch` through `ensureWorktree`.** Splitting it reintroduces the `resolveBranchStart` → `worktree add` ref TOCTOU the lock exists to prevent.
- **CS2 covers commit + push.**
- **The `claude` run sits between CS1 and CS2 and outside both.** That is the entire source of surviving parallelism.
- Git command deadlines must keep starting at exec time; neither `daemon` nor `turn` may set an ambient deadline that a mutex wait would consume.
- Concurrent `claude` processes are **not** a risk: 14/14 clean, including concurrent `--resume` of two distinct sessions sharing one `~/.claude`.

### Observability

- A per-repo child logger in `poll`'s loop and a per-task child logger built in `processIssue` immediately after `ParseRef`. Today `poll`, `processIssue`, `bootstrap` and `drain` log on the bare `d.log`, so no line below the turn carries task identity.
- Reuse the existing vocabulary — `task`, `session_id`, `issue`, `trigger_comment_id`, `instance_id`. A `worker` key is an invention; `task` already disambiguates any two lines.
- `d.now()` must continue to be called **only from `Tick`, never below `poll`**. The test clock advances one second per read; a per-issue goroutine reading it makes the `since`-string assertions flaky.

### Record keeping

- All acceptance criteria introduced by this work use the **`AC-PAR-` prefix family**. The bare `AC-3.1` / `AC-2.9` namespace already collides across plans and must not be extended.
- Every amendment uses `.aiteam/AC_AMENDMENTS.md`'s fixed section format: *First stated in / Amended / Status / It used to say / It now says / Why the change is safe*.

## Edge Cases & Error Handling

| Case | Expected behaviour |
|---|---|
| 5 comments on ONE issue, cap 3 | Run one at a time in comment order, occupying exactly **one** slot between them; 2 slots stay available to other issues throughout |
| 5 comments on 5 DIFFERENT issues, cap 3 | Exactly 3 start; the 4th starts the moment the first finishes, slot-by-slot, not in batches of 3 |
| New issue created while a 30-minute run is in flight | Discovered and started on the next tick, in parallel, without waiting for the run |
| Session lock lost to a sibling worker | Loser consumes nothing, posts nothing, advances no watermark, and the work is re-discoverable (RD-4 fixes the discovery path) |
| `git fetch` / `worktree add` contention | Prevented by the per-clone mutex. The measured stderr strings must not appear in a `-race` concurrency test |
| `push --set-upstream` returns 0 with no upstream written | Detected by an exit-code-independent check and surfaced as a run failure, not silent success |
| N goroutines each receive a 403 in one dispatch window | The failure counter advances **at most once**; the delay is `retryAfter · 2^(n)` for one increment, never `retryAfter · 2^(N-1)` |
| Rate limit hit while sibling runs are in flight | In-flight `claude` runs are **not** cancelled or killed. Backoff gates the dispatch of *new* runs only |
| SIGTERM / shutdown with N runs in flight | All N terminate under the existing shutdown contract; no `runs` row is left `running`; the next start's reconciliation invariant still holds |
| Second SIGTERM | Existing immediate-exit behaviour unchanged |
| Panic inside a worker goroutine | Governed by RD-6. Whichever way it is decided, the outcome is written down — this cannot be left implicit |
| Two repos both mapping issue #42 to one worktree directory (global `worktree_root`) | Detected and refused **at startup** with a clear error naming both repos. Not fixed by changing `naming.WorktreePath` |
| Wedged worker holding a session lock | Governed by RD-7. `ReconcileOrphans` releases `lock_owner <> instanceID`, which by construction excludes the daemon's own locks; `lock_acquired_at` is written and never read |
| `max_concurrent_runs` absent from config | Default `3` applies; behaviour is concurrent |
| `max_concurrent_runs: 0` or out of range | Startup error listing the valid range, aggregated with any other config errors |

## Required Decisions (carried forward — the architect must close each one, with a recorded rationale)

These are not open questions for the owner. They are deliverables of Plan mode.

| ID | Decision | Why it cannot be skipped |
|---|---|---|
| RD-1 | **Fairness / dispatch policy** — round-robin across repos, oldest-comment-first globally, or another | A daemon-wide cap plus `ListRepos` order starves repo 2 indefinitely. Must be chosen deliberately and justified in writing |
| RD-2 | **Poll cursor redesign** — per-repo cursor (a migration; `poll_cursor` has `CHECK (id = 1)`), a "sessions whose watermark is behind" query, or another | Deferral currently pins the single global cursor fleet-wide for the duration of the longest run. The 5-minute `ListingOverlap` is *not* sufficient on its own: a deferred issue's `UpdatedAt` does not change |
| RD-3 | **Scope of "strictly sequential per issue"** — `Execute` only, or `Execute` + report as one unit | The lock is released when `Execute` returns and the report posts afterwards. Today the window is empty; concurrently the thread can show run 2 starting before run 1's report lands. Either choice needs a test asserting the observable |
| RD-4 | **How in-flight work is re-discovered** after its run finishes | Ties to RD-2. `DispositionLocked` is a drop, not a defer, and is promoted from dead code to the primary flow-control mechanism |
| RD-5 | **`TickResult` under concurrency** — per-goroutine results merged, or a mutex | 6 non-atomic `++` fields plus `pending`, a **min-reduction** needing different treatment. Also resolve the `Runs++`-before-the-`DispositionLocked`-check ambiguity: move it or rename the field, do not leave it ambiguous. Prefer the shape that keeps `Daemon`'s "its only mutable state is the backoff" sentence true |
| RD-6 | **AC-3.3's panic invariant vs APP_CONTEXT's "do not add a second `recover()`"** | An unrecovered panic in a worker kills the process and abandons every sibling run. Either amend the no-second-`recover()` rule or accept and document the exposure |
| RD-7 | **Instance id and lock reclaim** — does the instance id gain a worker component? | Without one, a wedged worker's lock is unreclaimable while the process lives, and `ReleaseSessionLock(sessionID, instanceID)` cannot protect a live sibling |
| RD-8 | **Does `runs` gain an owner column?** | Needed only for mid-life reclaim/TTL. It is a schema migration (one `.sql` file), cheaper to decide now than to retrofit |
| RD-9 | **Rate-limit scoping** — should secondary limits (which target concurrent-request patterns specifically) be treated differently from primary ones? | Concurrency makes secondary limits materially more likely and nothing in the process coordinates request patterns |
| RD-10 | **Goroutine-budget justification** for a *variable* pool | M0's Global Invariants fix the inventory at four and require a review justification for a fifth. The existing convention: say how many; name the termination condition as one named thing; say what happens when the bound is exceeded; state what the goroutine buys in measured units |
| RD-11 | **Which log lines and doc sentences become false**, and the replacement text | At minimum: `internal/daemon/tick.go:726` and `internal/turn/execute.go:293` ("locked by another daemon instance" — the contender is now a sibling goroutine), `internal/turn/doc.go:37` ("The lock is the concurrency model"), `internal/git/clone.go:27` ("safe for concurrent use" — true of the struct, false of the repository it points at), `internal/daemon/doc.go`, `internal/store/store.go`'s "concurrent callers queue", `prepare.go`'s `previousRun` justification, M0's goroutine inventory |
| RD-12 | **The never-written AC-2.8 / AC-2.9 amendment** (`fix-comments-pickup` Story 5) | Both are in the blast radius and both are already false as written. Decide whether this pass pays it off — and if not, say so in-tree |

## Acceptance Criteria

### Dispatch and concurrency

- [ ] **AC-PAR-1.1** A tick returns without waiting for any run it dispatched: with a run blocked mid-`Execute`, a subsequent tick discovers and dispatches a *different* issue.
- [ ] **AC-PAR-1.2** Two issues in the same repo run concurrently — both are observably inside `Execute` at the same instant.
- [ ] **AC-PAR-1.3** Two issues in two different repos run concurrently.
- [ ] **AC-PAR-1.4** Two comments on one issue run one after the other, in ascending comment id order; the second never starts before the first's unit of mutual exclusion (per RD-3) completes.
- [ ] **AC-PAR-1.5** 5 comments on one issue with `max_concurrent_runs: 3` → they run one at a time in order, occupying exactly one slot; two slots remain available to other issues for the whole sequence.
- [ ] **AC-PAR-1.6** 5 comments on 5 different issues with `max_concurrent_runs: 3` → exactly 3 start; the 4th starts when the first finishes and the 5th when the second finishes. A test fails if refill happens only after all 3 complete.
- [ ] **AC-PAR-1.7** A comment queued behind a busy session occupies no worker slot: with a cap of 1 and issue A busy, issue B's eligible comment is not blocked by A's *queued* second comment.
- [ ] **AC-PAR-1.8** `max_concurrent_runs: 1` never has two runs in flight at the same instant.
- [ ] **AC-PAR-1.9** The number of concurrent runs never exceeds `max_concurrent_runs`, asserted by an instrumented high-water mark across a multi-repo multi-issue fixture.
- [ ] **AC-PAR-1.10** The daemon singleton is unchanged: a second process against the same `$LF_HOME` still fails to acquire `daemon.lock` and exits as it does today (AC-2.6).

### Fairness

- [ ] **AC-PAR-2.1** A dispatch/fairness policy is chosen, named, and justified in a comment or `doc.go` that states what it guarantees and what it does not.
- [ ] **AC-PAR-2.2** With repo A holding continuously eligible work sufficient to saturate the cap, repo B's eligible issue starts within a bounded, asserted number of dispatch decisions. Indefinite starvation fails the test.
- [ ] **AC-PAR-2.3** Reordering `ListRepos`' output does not change which work eventually runs — dispatch order is not an emergent property of list order.

### Git serialization

- [ ] **AC-PAR-3.1** A per-clone mutex, keyed on `store.Repo.Path`, serializes `fetch`, `worktree add` and `push` against one clone. It lives in `internal/daemon` and is injected via `turn.Options`; `internal/git` declares no mutex and no keyed registry.
- [ ] **AC-PAR-3.2** The `claude` run is outside both critical sections: two turns on the *same* repo are observably both inside their `claude` phase at one instant.
- [ ] **AC-PAR-3.3** CS1 is a single acquisition spanning `fetch` through `ensureWorktree`; a test or a structural assertion fails if the lock is released between them.
- [ ] **AC-PAR-3.4** After `push --set-upstream`, the upstream is verified independently of the exit code; a push that exits 0 without writing `branch.<name>.remote` is reported as a failure, not as success.
- [ ] **AC-PAR-3.5** A `-race` test over a fixture of 2 repos × 3 issues passes, and none of the measured strings — `cannot lock ref`, `failed to read .git/worktrees/`, `could not lock config file` — appears in captured stderr.
- [ ] **AC-PAR-3.6** Every git command still starts its own deadline at exec time; time spent waiting for the per-clone mutex does not consume a command's timeout budget.
- [ ] **AC-PAR-3.7** `internal/git/surface_test.go`'s `Clone` allowlist still passes — the mutex adds no method to `Clone` and no write path into a registered clone.

### Shared state and failure handling

- [ ] **AC-PAR-4.1** `TickResult` counters and `pending` are correct under concurrency, asserted with `-race`; no field is mutated by more than one goroutine without synchronization.
- [ ] **AC-PAR-4.2** N concurrent rate-limit errors within one dispatch window advance the backoff failure counter at most once; a test with 5 simultaneous 403s asserts the resulting delay, not `retryAfter · 2^4`.
- [ ] **AC-PAR-4.3** A rate limit observed while runs are in flight does not cancel their contexts or kill their `claude` processes; it gates the dispatch of new runs only.
- [ ] **AC-PAR-4.4** While backoff is active no new run is dispatched, for any repo (AC-2.12's guarantee, restated for N workers).
- [ ] **AC-PAR-4.5** Two `turn.Execute` calls at one session: exactly one `runs` row, exactly one report, and the loser consumed nothing, posted nothing and advanced no watermark.
- [ ] **AC-PAR-4.6** Shutdown with N in-flight runs terminates all N; no `runs` row is left `running`; the next `Start`'s reconciliation finds nothing to abandon that is still alive.
- [ ] **AC-PAR-4.7** *(prerequisite)* `internal/daemon/fixture_test.go` no longer builds two `slog` handlers over one `*bytes.Buffer`; `make test` (`-race`) is green **before** any concurrency is introduced.
- [ ] **AC-PAR-4.8** A configuration in which two registered repos map an issue number to the same worktree directory is refused at startup with an error naming both repos. `naming.WorktreePath` is unchanged.

### Discovery

- [ ] **AC-PAR-5.1** A deferred or in-flight issue in repo A does not rewind or hold back discovery for repo B.
- [ ] **AC-PAR-5.2** An issue busy for longer than `ListingOverlap` is re-discovered after its run finishes, without any change to its `UpdatedAt`.
- [ ] **AC-PAR-5.3** Discovery skips a session already in flight: nothing is posted, no watermark moves, and the skip is logged with task identity.

### Configuration

- [ ] **AC-PAR-6.1** `max_concurrent_runs` decodes strictly (`KnownFields(true)`), defaults from `Defaults()`, and is rejected — not clamped — outside `1..16`; an explicitly written `0` is a startup error.
- [ ] **AC-PAR-6.2** The shipped template carries the key with its range comment and `TestDefaultFileMatchesDefaults` passes.
- [ ] **AC-PAR-6.3** The validation error for an out-of-range value is aggregated with other config errors via `errors.Join`, not returned alone.
- [ ] **AC-PAR-6.4** `internal/config/purity_test.go` still passes — no third-party import is added to `internal/config`.

### Observability

- [ ] **AC-PAR-7.1** Every log line emitted below `poll` carries task identity via a per-task child logger; per-repo lines carry repo identity. No line from a concurrent path is attributable only by guesswork.
- [ ] **AC-PAR-7.2** `d.now()` is called only from `Tick`, never below `poll`; a test or a structural check fails if a per-issue path reads the clock.

### Record

- [ ] **AC-PAR-8.1** `.aiteam/AC_AMENDMENTS.md` gains an entry recording the **D-X ruling** in the file's fixed section format: D-X rejected a limit on runs *per unit time* (spending control); `max_concurrent_runs` is a limit on runs *at the same instant* (resource control); different mechanism, different purpose; therefore D-X's "no task or AC may reintroduce it" does not bar it.
- [ ] **AC-PAR-8.2** The same file records the departure from **AC-FC-2.6** — this tuning value is a `config.yaml` key, not a Go constant — with the reason.
- [ ] **AC-PAR-8.3** The same file records an amendment for every criterion this change alters in meaning or wording: AC-2.1, AC-2.8, AC-2.9, AC-2.12, AC-2.13, AC-2.17, AC-2.21, AC-3.1, AC-3.2, AC-3.3, AC-3.5, AC-3.8, AC-3.9, AC-3.10, AC-3.20, AC-3.22, D-A, D-B, D-7, P-3, SD-1 — or explicitly records why one of them was left alone.
- [ ] **AC-PAR-8.4** Every doc sentence and log line listed in RD-11 is either true after the change or rewritten. A grep for "locked by another daemon instance" returns nothing false.
- [ ] **AC-PAR-8.5** `internal/daemon/doc.go` states the new dispatch model and amends the "an ordinary synchronous method" description of `Tick`.
- [ ] **AC-PAR-8.6** Deliberate deferrals are recorded in-tree with their reason: per-repo `repo add --max-parallel N`, a repo component in `naming.WorktreePath`, and the LF-11 in-flight view. The design for `max_concurrent_runs` must not foreclose adding a per-repo bound later.
- [ ] **AC-PAR-8.7** All criteria introduced by this work carry the `AC-PAR-` prefix; none reuse a bare `AC-N.M` identifier.

## Out of Scope

- Raising `SetMaxOpenConns(1)`. Not needed and actively risky without `BEGIN IMMEDIATE`; if ever raised, the cap, `BEGIN IMMEDIATE` and per-connection FK verification change as one decision.
- A queue for `DispositionLocked` — LF-10's "queue and coalesce" is a separate feature.
- `litefactory status` or any global "what is in flight" view (LF-11). Flagged as the likely next request.
- Log rotation, notwithstanding that concurrency multiplies log volume against an unrotated `daemon.log`.
- Changing `naming.WorktreePath` to include a repo component. Validate against the collision at startup instead and record the deferral.
- Per-repo `max-parallel` (`repo add --max-parallel N`). **Deferred, not rejected** — the design must not foreclose it.
- Cross-repo tasks; remote workers.
- Fan-out-and-join as a first step. Considered and rejected by the owner: it does not deliver the requesting example.
- A new module, mocking framework, metrics endpoint, or `recover()` beyond whatever RD-6 decides.
- Committing any test that spawns a real `claude`.
