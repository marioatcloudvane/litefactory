# Research Brief — a-closed-issue-keeps-its-needs-poll-flag

## Request

Research issue #169: “A closed issue keeps its needs_poll flag and is re-dispatched on every tick forever.” The triggering instruction was “research this bug!” This run is Research only; it does not implement a fix or start Plan mode.

Provenance: `[V]` verified from repository source during this run; `[O]` reported by the issue owner, not independently observed against the running daemon. Recommendations and unresolved decisions are explicitly identified.

## Codebase Context

[V] This is a single Go 1.26 module (`go.mod:1`). The daemon owns discovery, admission, timing and backoff; a turn owns individual execution/reporting (`internal/daemon/doc.go:1`). SQLite session access belongs to the store. The relevant path spans `internal/daemon/tick.go`, `internal/daemon/pool.go`, `internal/store/sessions.go` and `internal/store/migrations/0002_session_backlog.sql`.

### Backlog lifecycle

1. [V] Forge issue listings request open issues (`internal/github/issues.go:144`). Discovery rejects a non-open listed issue before creating a session, without clearing an existing session's backlog (`internal/daemon/tick.go:437`, `internal/daemon/tick.go:443`). That rejection does not put the existing session into the discovery deduplication set.
2. [V] Local backlog supplements forge discovery. `ListSessionsNeedingPoll` selects flagged sessions regardless of local session state or forge issue state (`internal/store/sessions.go:393`; `internal/daemon/tick.go:350`, `internal/daemon/tick.go:393`). Local states are idle/running/failed, not open/closed (`internal/store/sessions.go:18`). Failure is therefore not a prerequisite for this bug.
3. [V] A backlog-only candidate is unresolved until admission. `offer` enters the in-flight set, sets `needs_poll`, and tries to reserve a worker slot (`internal/daemon/tick.go:797`, `internal/daemon/tick.go:814`, `internal/daemon/tick.go:816`). Setting an already-set flag still updates `updated_at` (`internal/store/sessions.go:368`).
4. [V] `materialise` fetches the unresolved issue and returns `nil, nil` when it is not open (`internal/daemon/tick.go:940`, `internal/daemon/tick.go:955`). This branch does not clear the flag.
5. [V] On that result, `offer` releases both the semaphore slot and in-flight membership synchronously and returns `offerSkipped`; no worker goroutine starts (`internal/daemon/tick.go:825`). The dispatcher continues considering candidates, without consuming its fairness cursor for that skip (`internal/daemon/tick.go:733`).
6. [V] The only production clear site is `clearBacklog`, reached after an error-free, nondeferred bootstrap/drain (`internal/daemon/tick.go:1004`, `internal/daemon/tick.go:1315`). A closed backlog-only issue never reaches that worker path. Its durable flag therefore survives for future ticks and restarts; store tests cover flag persistence (`internal/store/store_test.go:599`).

### Existing verification surface

[V] Daemon tests use a fake HTTP forge, real temporary migrated SQLite and a fake turn executor (`internal/daemon/fixture_test.go:272`, `internal/daemon/fixture_test.go:896`). Worker synchronization is available through the existing `settle` helper (`internal/daemon/fixture_test.go:1123`).

| Behavior | Existing source coverage and gap |
|---|---|
| Successful bootstrap/drain clears; errors/locks retain backlog | [V] Covered in `internal/daemon/backlog_test.go:26`, `:48`, `:74`, `:108`, `:157`. |
| Closed issue discovery | [V] Tests begin without a preexisting flagged session, leaving this regression uncovered (`internal/daemon/tick_test.go:1271`). |
| Abandoned work whose issue is closed | [V] Tests assert no further executor calls, but not backlog clearing or avoidance of repeated GETs (`internal/daemon/daemon_test.go:116`). |
| Durable flags and repository selection | [V] Covered in `internal/store/store_test.go:473`, `:533`, `:599`. |
| Rediscovery, in-flight deduplication, fairness and backoff | [V] Existing behavioral coverage includes `internal/daemon/crossrepo_test.go:164`, `:281` and `internal/daemon/dispatch_test.go:59`. |
| Bounded issue reads for many closed/error candidates | [V] No corresponding request-budget test was located during the tour. |

[V] The testing scanner was applied; the optional `.aiteam/rulebook.md` is absent. Architecture and security scanners were skipped because the investigation concerns the existing lifecycle rather than new layering or authentication/input handling. Unrelated team-management internals were outside the focused analysis.

## Domain / Problem Space

### Reported incident

- [O] After three issues were closed, approximately twenty minutes of ticks continued to report `issues=0 dispatched=3 in_flight=3 deferred=0`.
- [O] Sessions 142 (issue 142), 155 (issue 155), and 156 (issue 157) were failed, had `needs_poll=1`, and belonged to closed issues. Their timestamps advanced every tick.
- [O] There were no running session locks, agent child processes, or recent run logs. Restarting the daemon did not remove the flags.
- [O] The owner inferred that each stuck session permanently costs one of five dispatch slots and that enough such sessions can wedge dispatch. The repository investigation must distinguish persistent backlog entries from actual worker-slot lifetime and scheduling fairness.

### Root cause and impact assessment

[V] The durable stale-backlog defect is confirmed by control flow: admission sets the flag, closed-issue materialisation skips work, and only work completion clears it. The closure branch lacks a terminal disposition for the backlog marker. No test execution or live-daemon reproduction was performed; source verification is distinct from reproducing the owner's incident.

[V] Timestamp churn is explained directly by the unconditional flag UPDATE. It does not demonstrate that an agent or even a worker goroutine ran (`internal/store/sessions.go:368`). The absence of run logs and agent child processes is consistent with the materialisation skip.

[V] The permanent worker-slot leak claim is contradicted by current source for backlog-only candidates whose GET returns closed. Their slots are released before the next candidate is offered. `Dispatched` increments only after starting a goroutine (`internal/daemon/tick.go:894`, `internal/daemon/tick.go:910`). Three such candidates cannot, through this path alone, account for `dispatched=3 in_flight=3`, and five do not permanently occupy a pool of five. This does not disprove the reported incident; the deployed revision and the actual candidates represented by those counters were not available for comparison.

[V] The confirmed cost is repeated database writes and issue GETs when these rows are offered with capacity, plus synchronous lookup latency. A large stale backlog can delay a tick and consume API quota. Permanent slot loss should not be presented as the demonstrated mechanism.

[V] An adjacent documentation discrepancy increases the significance of that cost: comments promise at most `max_concurrent_runs` issue reads per tick (`internal/daemon/tick.go:386`, `internal/daemon/tick.go:786`, `internal/store/sessions.go:388`). Semaphore occupancy does not enforce that request budget: a closed/error candidate releases its slot and iteration continues, permitting more reads than the concurrency limit in one tick. No measurement of practical starvation or quota exhaustion was made.

### Lifecycle boundaries

- [V] A GET error preserves backlog, releases admission, logs the failure and observes rate limits (`internal/daemon/tick.go:825`). An error is not confirmation that work is terminal. The rate-limit guard before worker launch follows materialisation; failing materialisation returns earlier, so repeated failing reads also warrant attention when considering a request budget.
- [V] An issue already resolved from an open listing is not fetched again by `materialise` (`internal/daemon/tick.go:942`). Closing it after that observation remains a separate race; the confirmed backlog-only closure path does not cover every closure timing.
- [V] Retaining the flag currently makes a reopened issue actionable on a subsequent successful open GET, reusing its session and watermarks. Removing the flag changes that rediscovery route. Plan must establish how reopened work is rediscovered without silently losing unconsumed comments.
- [V] There is no local user-abandoned session state. Existing “abandoned” handling reconciles interrupted runs: startup marks orphan runs failed, releases stale locks and posts already-owed reports (`internal/daemon/daemon.go:491`, `internal/store/runs.go:603`). It does not clear backlog. Reporting remains required when the issue is closed (`internal/daemon/daemon_test.go:106`).
- [V] Schedules use a separate admission path: a closed issue releases admission while its schedule remains pending (`internal/daemon/schedules.go:79`). Fixing ordinary backlog would not settle scheduled-work closure policy.

## Constraints & Risks

- [V] Keep daemon timing/admission ownership and context-first store access. Any SQL belongs in the store; this research establishes no need for a schema change. Preserve retry eligibility on genuine errors and preserve watermark ordering.
- [V] Existing test rules require the shared fixtures, real SQLite and channel/Quiesce synchronization; do not invent a slot-release observable (`TEST-3`, `TEST-6`, `TEST-8`, `TEST-9`, `.aiteam/APP_CONSTRAINTS.md:131`, `:144`, `:151`, `:155`).
- [V] Update stale protocol comments with any behavioral change (`STYLE-1`, `.aiteam/APP_CONSTRAINTS.md:212`). The comment that the next listing or this closure check clears the flag by draining an empty queue is already false for persistently closed issues (`internal/daemon/tick.go:933`). The claim that setting an existing flag is a no-op overlooks timestamp writes (`internal/store/sessions.go:368`). The per-tick issue-read promise also needs reconciliation.
- Recommendation: treat authoritative issue closure separately from failures to read an issue. Clearing on an inaccessible/deleted response without a defined policy risks losing work that is only temporarily inaccessible.
- Recommendation: do not equate failed sessions or reconciled interrupted runs with owner abandonment. Both may still carry retry or reporting obligations.
- No production changes, runtime-state edits, daemon operations, commits, or pushes were performed. Research used repository reads and produced session documents only; tests and `make check` were not run. Implementation should use the existing fixtures and finish with the repository's `make check` gate (`Makefile:29`).

## Open Questions

1. **Incident reconciliation — owner/deployment evidence:** Which binary revision generated the supplied counters, and which candidates did it dispatch? Current source confirms stale flags but does not explain those counters solely from closed backlog-only issues. A later investigation needs matching deployment evidence; this run did not access runtime state.
2. **Closure/reopening semantics — Plan and product owner:** Does confirmed closure retire automatic retry eligibility while preserving the session and watermarks? If reopened, should pending comments resume, and what discovery path guarantees this after clearing the flag?
3. **Unavailable issues — Plan:** What distinguishes deletion or permanent loss of access from a transient GET failure? The safe research assumption is to retain backlog on unsuccessful reads.
4. **Request budget — Plan:** Is an independent per-tick backlog read budget part of this fix or a separately tracked performance concern? Current concurrency limits do not supply one.
5. **Schedules — Plan and product owner:** Should closed scheduled work be handled separately? The recommended default is to keep it outside this focused fix while recording the analogous unresolved lifecycle.
6. **Observation race — Plan:** Is closure handling limited to an authoritative closed observation during backlog materialisation, or should a later closure after an open listing also alter behavior? Any stronger guarantee requires explicitly considering additional reads and races.

Headless-run assumptions: use confirmed closure as the candidate terminal signal, retain retry state on lookup errors, preserve watermarks and owed reports, and keep schedule policy separate unless Plan explicitly expands scope. These are recommendations for review, not implemented behavior or owner-approved requirements.

## Recommended scope for Plan mode

Plan a focused terminal disposition for ordinary backlog when the forge authoritatively reports the issue closed. Evaluate clearing at that observation against the required reopening behavior. Merely excluding closed listings does not address locally persisted candidates, and local session state alone cannot identify a closed issue.

Include regression acceptance coverage for a preexisting flagged closed session across repeated ticks and restart; no agent execution or dispatch count for a closed materialisation; useful open work progressing even with more closed candidates than slots; transient GET errors retaining retry eligibility; and reopening under the selected policy. Preserve existing clean-drain, watermark, deduplication, fairness, lock/error retention and abandoned-report behavior. If read budgeting is included, cover many closed/error candidates with explicit request counts using the existing fake forge.

Correct the backlog protocol, closure/drain and issue-read comments. Current code already skips a confirmed closed backlog-only issue without counting it as dispatched; a counter change needs separate evidence of a different defective path. Do not expand this bug into a new user-abandonment state, broad scheduler redesign or unrelated schedule lifecycle change without an explicit scope decision. Producing this brief completes Research; no implementation plan or code change is included.
