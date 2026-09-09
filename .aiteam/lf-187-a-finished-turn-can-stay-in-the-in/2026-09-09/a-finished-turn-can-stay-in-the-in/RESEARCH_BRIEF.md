# Research Brief — a-finished-turn-can-stay-in-the-in

## Request

[O] Issue #187, “A finished turn can stay in the in-flight set, silently disabling that session until restart,” asks for research into a suspected in-memory dispatch reservation leak, distinct from the persistent `needs_poll` defect reported fixed in #169. The triggering instruction was “research this!”

[O] The owner reports a fresh daemon at revision `fbc4b02`, two prepared turns (#183 and #73), successful completion and report publication for #73, and later `in_flight=2` despite only one observed database session lock. The owner reports #73 idle with `needs_poll=0`, and an empty `automatic_continuations` table. These are supplied observations, not live runtime evidence independently captured in this run.

The research scope is acquisition/release control flow, whether the reported evidence uniquely establishes a leak, dispatch diagnostics, and the safety of reconciling in-memory reservations against database locks. This run produces research only; “find and close” in the issue is treated as the eventual product objective, not authorization to move this run into Implement mode.

Provenance: **[V]** verified from current repository source this run; **[O]** reported in the supplied issue/comment; **[W]** second-hand and unverified; **[R0]** carried forward from an earlier run. Inferences from verified source are explicitly described as such. Repository citations refer to this worktree; no previous session artifacts or live runtime state were consulted.

## Codebase Context

### Repository and relevant ownership

[V] The repository is a single Go 1.26 module with command wiring in `cmd/litefactory`, implementation in `internal/`, and colocated tests (`go.mod:1`). The relevant boundaries are `daemon.Tick` for discovery/admission, worker `work` for bootstrap or comment draining, `turn.Execute` for a single agent run, and reporting for publication and durable consumption updates. SQL belongs to `internal/store`; SQLite has a one-connection limit (`internal/store/store.go:132`).

[V] Ordinary work follows `Tick → discover → dispatch → offer → materialise → work → bootstrap/drain → handleComment → run → Execute → report`, then returns through the remaining worker bookkeeping. A dispatched worker can bootstrap, find no new comments, ignore comments, or process controls without preparing any agent run (`internal/daemon/tick.go:1033`, `:1082`, `:1383`, `:1410`). Counting prepared turns is therefore not equivalent to counting admitted workers.

[V] In-memory session exclusion and concurrent-worker capacity have different owners: `inflight` holds a mutex-protected set of session IDs, while `pool` uses a bounded channel (`internal/daemon/inflight.go:40`; `internal/daemon/pool.go:44`, `:96`). Neither is identical to the durable execution lock.

### Acquisition/release audit

[V] The specialist inspected all five production entry sites in the current source. Every successful acquisition has visible synchronous cleanup or an installed defer. **No concrete enter-without-leave path was located.** This is a source audit finding, not proof that the reported running process had no defect or blocked worker.

| Admission path | Entry | Synchronous cleanup | Asynchronous or deferred cleanup |
|---|---|---|---|
| Ordinary offer | `internal/daemon/tick.go:827` | Pool full `:845`; materialisation error/nil `:860`; backoff `:912` | Worker defer `:934` |
| Scheduled offer | `internal/daemon/schedules.go:69` | Pool full `:74`; issue unavailable/not open `:82`; backoff `:93`; reserve error `:100`; reservation lost `:106` | Worker defer `:112` |
| Automatic continuation | `internal/daemon/continuation_dispatch.go:54` | Pool full `:58`; reserve failure/loss `:65` | Worker defer `:74` |
| Continuation report replay | `internal/daemon/continuation_reports.go:52` | All subsequent returns covered by immediate defer | Defer `:55` |
| Continuation cancellation replay | `internal/daemon/continuation_cancellations.go:44` | All subsequent returns covered by immediate defer | Defer `:47` |

[V] Continuation readiness, due time, repository/reference validity, and backoff checks occur before acquisition. Replay helpers install their cleanup before attempting durable lock acquisition, so a lock failure still releases their in-memory ownership (`internal/daemon/continuation_reports.go:55`–`:60`; `internal/daemon/continuation_cancellations.go:47`).

[V] Ordinary materialisation copies the candidate session into the task (`internal/daemon/tick.go:999`). The scheduled and continuation workers likewise clean up the session they acquired. The inspected production references contain no later assignment replacing the task session or its ID; no wrong-key release was found.

[V] `enter`, `leave`, and `size` use the same map and mutex (`internal/daemon/inflight.go:53`, `:74`, `:82`). Normal worker defer ordering releases in-memory ownership before returning the pool token (`internal/daemon/tick.go:928`, `:934`). A double leave does not explain a remaining map entry. No unowned stale-release path was found either; introducing one through recovery logic would create a separate exclusion risk.

### Existing diagnostics and tests

[V] Ordinary duplicate admission already emits a debug log using the candidate's logger (`internal/daemon/tick.go:832`). Scheduled duplicate refusal also logs (`internal/daemon/schedules.go:70`). Automatic continuation admission and both replay duplicate paths return silently (`internal/daemon/continuation_dispatch.go:54`; `internal/daemon/continuation_reports.go:52`; `internal/daemon/continuation_cancellations.go:44`). The issue's proposed logging improvement is thus partly present, with specific remaining gaps.

[V] The set records session IDs and its peak count, without admission-instance identity, start time, worker phase, or release history (`internal/daemon/inflight.go:40`). There is no production per-worker completion event at the actual release boundary. Existing logs cannot establish that a particular admission survived its own worker return.

| Inspected coverage | Evidence and limit |
|---|---|
| Set mechanics and concurrency | Duplicate refusal, leave/readmission, size, peak, and concurrent access: `internal/daemon/inflight_internal_test.go:13`, `:48`, `:79`. |
| Same-session exclusion and recovery | Blocks a turn, checks duplicate refusal and identifying logging, releases it, then permits subsequent same-session work: `internal/daemon/crossrepo_test.go:281`. |
| Capacity refill | Another worker starts after a sibling finishes: `internal/daemon/refill_test.go:63`. The file explicitly describes post-report SQLite work before worker completion (`:16`). |
| Admission cleanup | Backoff and closed-backlog coverage: `internal/daemon/dispatch_test.go:59`; `internal/daemon/backlog_closure_test.go:114`. |
| Continuation duplication | Behavioral duplicate automatic admission coverage: `internal/daemon/continuation_admission_test.go:149`. |
| Lifecycle diagnostic gaps | No dedicated inspected test links the “turn complete” message to the distinct release boundary or distinguishes equal counts with different admission identities. No explicitly named blocked-report/watermark exclusion test was located; the existing exclusion cases mostly block in the executor. This is a scoped search finding, not a claim that all indirect coverage is absent. |

[V] Tests use temporary real SQLite/filesystem fixtures and fake external services. Channel rendezvous is an established concurrency-test pattern. `make check` includes formatting, vet, pinned lint, and race tests (`Makefile:22`). **Tests were inspected but not executed**; no passing runtime verification is claimed.

The tour ran its testing scanner. `.aiteam/rulebook.md` was absent, so that scanner applied generic testing principles. Architecture and security scanners were skipped because the question concerns the existing ownership lifecycle and does not propose package restructuring or trust-boundary changes. No web research was needed for these repository-local findings. Unrelated package details and additional tests with the same fixture patterns were omitted from the specialist's return.

## Domain / Problem Space

### The finish message does not establish worker completion

[V] **“turn complete” is emitted by `run`, not by the worker goroutine at exit** (`internal/daemon/tick.go:1762`). The owner's explanation that this is the last worker line immediately before the inflight defer is inconsistent with the current source.

[V] After an ordinary comment's run returns, control passes through `handleComment` (`internal/daemon/tick.go:1422`), reads consumption state (`:1255`, `:1283`), can continue to later comments or stop for one-shot classifier handling (`:1247`, `:1268`), and on a clean drain clears backlog (`:1042`, `:1352`). Only return from `work` executes the admission and pool defers (`:934`, `:928`). Scheduled work also reads the outcome and settles the schedule after `run` returns (`internal/daemon/schedules.go:122` onward).

[V, inference] A worker paused or blocked in this later work can legitimately remain admitted after a successful turn and report. The existing refill test's description independently acknowledges this interval (`internal/daemon/refill_test.go:16`). The source identifies a plausible category of explanation; it does **not** establish that such blocking occurred in the reported incident or identify a blocking call.

### A constant count does not establish constant membership

[V] `Tick` samples the live map size after dispatch (`internal/daemon/tick.go:217`–`:218`). The metric is a real snapshot, as the owner states, but contains no membership or admission-generation evidence.

[O] The quoted `13:17:12` count precedes the reported `13:17:17` finish. The quoted `13:19:10` line reports `dispatched=1`. [V, inference] A sequence consistent with the supplied lines is that the old worker exits and a later polling worker enters, restoring the count to two. Such a worker need not emit “run prepared,” because admission can end with no executable comment (`internal/daemon/tick.go:1033`, `:1082`, `:1383`). The samples therefore do not uniquely distinguish a leaked member, an unfinished worker, and replacement membership. This alternative explanation does not disprove the owner's broader observation that subsequent comments stopped being serviced.

### Three distinct lifetimes must remain distinct

| State | What it represents | Why it cannot replace the other states |
|---|---|---|
| In-memory admission | A session worker or replay has ownership of its wider dispatch/publication interval | It begins before execution and can remain after execution ends. |
| Durable session lock | Ownership of `turn.Execute` | Its defer releases the lock while reporting and comment-drain work can still remain. |
| Pool token | A bounded concurrent worker slot | Capacity is controlled by channel admission, not by map cardinality. |

[V] Execution acquires and releases its durable lock within `Execute` (`internal/turn/execute.go:541`, `:565`). The wider inflight interval deliberately spans publication and watermark processing (`internal/daemon/inflight.go:17`–`:27`; `internal/daemon/tick.go:929`). Replay also enters before it attempts the durable lock (`internal/daemon/continuation_reports.go:55`–`:60`).

[V, inference] **An empty `sessions.lock_owner` is insufficient evidence of an orphaned inflight entry.** Erasing such entries on every tick would admit another worker during legitimate pre-execution or post-execution work. A recovery mechanism based only on that predicate would weaken exclusion precisely where publication and consumption still need protection.

[V] Pool capacity is enforced by `pool.try`'s buffered channel (`internal/daemon/pool.go:96`), not `inflight.size()`. [V, inference] A hypothetical orphan map entry alone would prevent that session's redispatch but would not independently consume a pool slot. A still-blocked worker could retain both. The issue's broad capacity consequence requires distinguishing these failure modes rather than assuming that count inflation automatically means capacity loss.

## Constraints & Risks

1. **Incident attribution remains unresolved.** [O] The reported daemon revision is `fbc4b02`. [V] This run audited current worktree source, without comparing that deployed revision or inspecting the running daemon. The source finding cannot be promoted to a historical incident diagnosis.
2. **Do not repair a reservation merely because the execution lock is absent.** [V, inference] The two intervals differ by design, including real publication and consumption work (`internal/daemon/inflight.go:17`; `internal/turn/execute.go:565`). False recovery can cause overlapping workers and repeated or inconsistent processing.
3. **Do not equate elapsed time with loss of ownership.** [V, inference] Duration or a successful run message does not prove that the worker returned. Any future recovery design needs evidence about the actual worker/admission lifetime and protection against a stale cleanup affecting a newer admission.
4. **Preserve package boundaries and runtime safety.** The supplied project rules place SQL and database ownership in `internal/store`, time and tick/backoff policy in `internal/daemon`, and individual execution in `internal/turn`. This run did not modify runtime state, touch `dist/`, restart a daemon, obtain forge credentials, or commit/push.
5. **Preserve normal low-activity behavior.** [V] A worker with no prepared turn can still be valid work (`internal/daemon/tick.go:1082`). Diagnostic interpretation must distinguish no executable comments from refused admission and a worker still finishing.
6. **Tests are evidence of intended behavior, not an incident reproduction.** [V] Coverage exists for cleanup and readmission, but this run performed no execution and found no reproduction. Claiming a confirmed leak or a proven fix would exceed the evidence.
7. **Silence is only partly addressed today.** [V] Ordinary and scheduled duplicate admission already log at debug; three continuation-related sites do not. More diagnostic detail must tie refusal to a specific session and active admission, rather than add another aggregate count with the same ambiguity.

## Open Questions

These are recorded for subsequent investigation, not questions requiring a response before this headless research run can finish.

1. **Does the deployed source match the audited lifecycle?** A subsequent engineering investigation should compare the relevant paths against `fbc4b02` before attributing this incident to current code.
2. **Which session and which admission accounted for the second member?** Subsequent diagnostic evidence needs membership and admission identity around both refusal and release. The supplied aggregate count cannot answer this.
3. **Did the reported worker return?** Engineering needs an actual worker-release observation or a captured worker stack/phase to distinguish an orphan map entry from ongoing post-execution work. A run-finish message is insufficient.
4. **What did the later `dispatched=1` worker do?** The incident evidence does not identify whether it was a polling-only worker or which session it admitted.
5. **Were database state and counts sampled contemporaneously for the same admission?** An idle row and cleared backlog could describe an earlier completed drain; timestamped evidence must tie them to the claimed remaining member.
6. **If a worker is blocked, where and why?** The audit locates post-run database/bookkeeping steps but does not establish deadlock, starvation, a held connection, or any other concrete cause. The one-connection database constraint is context, not proof of a store defect.
7. **What proves safe recovery?** Plan/engineering must establish a reliable worker-liveness/ownership invariant before considering self-healing. Lock absence alone is ruled out by the current lifecycle; whether another recovery mechanism is necessary remains open.

## Recommended scope for Plan mode

The following is a scope recommendation, not an implementation plan or a transition into another mode.

- Establish enough lifecycle evidence to distinguish refused dispatch, an active worker after run completion, an actually released worker, and a later admission of the same session. Keep the existing useful debug messages and consider the silent continuation paths.
- Cover the real exclusion boundary in regression checks, especially reporting/consumption after durable lock release, cleanup/readmission following admission failures, and the difference between map size and pool capacity. Existing tests provide relevant fixtures; they are not replaced by another test that only mirrors map insertion/deletion.
- Compare the incident revision and pursue a reproducible admission that survives actual worker completion. If a missing release is established, scope its correction to the demonstrated path and verify that exact behavior. If the worker is still alive, investigate its blocked phase instead.
- Preserve ownership across pre-execution and post-execution intervals. Do not adopt per-tick clearing based solely on `sessions.lock_owner`, and do not present set cardinality as the capacity semaphore.
- Keep this separate from the previously reported persistent `needs_poll` defect unless new evidence establishes a link. Avoid a broad scheduler or continuation redesign without a demonstrated cause.

Research is complete with a narrowed diagnosis: the supplied observations do not prove a leaked admission, every current acquisition has visible cleanup, and the requested lock-based reconciliation would be unsafe. The actual incident cause remains unconfirmed. The assumption for this headless run was to complete research and document these uncertainties without asking questions or implementing changes.
