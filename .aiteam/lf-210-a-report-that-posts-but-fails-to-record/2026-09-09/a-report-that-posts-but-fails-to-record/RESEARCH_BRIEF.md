# Research Brief — a-report-that-posts-but-fails-to-record

## Request

Research issue #210's indefinite duplicate-report loop, including the owner's widened scope: an ordinary preparation refusal after run allocation can strand a running row just as a lost process can. Determine how allocation, replay, report delivery, durable completion, consumption, live-worker protection and orphan recovery interact; identify the boundaries and evidence needed for Plan mode. This run is Research only and makes no production changes.

Provenance: **[V]** repository behavior verified this run through static code inspection; **[O]** evidence reported by the owner, not independently checked against runtime state; **[W]** second-hand unverified evidence; **[R0]** previous-run material. No previous-session material was used. Inferences and recommendations are identified explicitly rather than presented as observed execution.

[O] On #170, run 12 / database row 287 was allocated before a managed-plan refusal. The supplied logs show worktree preparation at 23:14:49, refusal at 23:14:52, then repeated report posts followed by rejection of completion because the row had not finished. The owner reports no agent launch or run log. Earlier run 11 / row 283 allegedly followed process loss, with duplicate comment IDs 5608586009, 5608598042 and 5608609360. Manual terminal-state updates reportedly stopped both loops. These incidents were not reproduced against the daemon or inspected in its database; runtime state and forge access were outside this research.

## Codebase Context

### Two-pass map and ownership

[V] This is a single Go 1.26 module (`go.mod:1`). The relevant flow is daemon dispatch → in-flight/task ownership checks → existing-run lookup → preparation/allocation → execution and harvest → report rendering/posting → transactional report completion and source consumption. The daemon owns dispatch, concurrency and retry timing; the turn layer conducts one turn and delivers its result; the store owns durable state and SQL; the report layer produces typed redacted bodies; the GitHub client performs external requests.

[V] Production setup acquires a singleton lock before returning the daemon (`internal/daemon/setup.go:245`). `Daemon.Run` starts recovery before normal ticking (`internal/daemon/daemon.go:639`). These ownership boundaries matter when deciding whether a running row is still active or can be declared abandoned.

The focused second pass covered preparation and replay, managed approval, harvest, both reporter entry points, store report transactions, daemon queue behavior/startup/shutdown, report markers, existing acknowledgement recovery, comment listing, and their tests. The findings below retain the relevant source locations rather than reproducing unrelated package internals.

### Allocation and refusal: the deterministic stranded-row path

[V] The allocation operation is `AllocateRun`, not `CreateRun`; its wrapper reserves the run ordinal and log path before process launch (`internal/turn/prepare.go:1438`). Preparation allocates at `internal/turn/prepare.go:261`, then reaches managed approval at `internal/turn/prepare.go:353`.

[V] `ApproveManagedStories` requires the planning state and matching generation, `ready=1`, and no unconsumed refinements (`internal/store/approved_stories.go:25`). Failure produces the supplied error, `store: plan is not ready or has pending refinements` (`internal/store/approved_stories.go:34`). The readiness refusal itself is intentional.

[V] This preparation error escapes through `Execute` (`internal/turn/execute.go:614`) before the allocated run is copied to the returned result (`internal/turn/execute.go:620`). The deferred task-lock release still executes (`internal/turn/execute.go:565`), but no corresponding cleanup finishes the allocated row. The daemon leaves the source comment unconsumed (`internal/daemon/tick.go:1745`).

[V] A later execution attempt finds the existing run (`internal/turn/execute.go:575`). Replay maps every unreported row, including `running`, to `DispositionAlreadyRan`; only failed rows receive an explicit failed outcome and reason (`internal/turn/execute.go:632`). Thus the source supports the widened incident mechanism without any process crash being necessary.

[V] This is a broader lifecycle gap than the readiness check alone. Allocation also precedes fallible synchronization recording, history, managed handoff/refinement/approved context, and prompt preparation (`internal/turn/prepare.go:272`, `internal/turn/prepare.go:285`, `internal/turn/prepare.go:356`). Harvest can fail while recording a resume token or terminal result (`internal/turn/harvest.go:96`, `internal/turn/harvest.go:107`). Inference: moving only one gate would remove one deterministic trigger while leaving other ways to lose finalization.

### Reportability and the external-post/local-record gap

[V] `Reporter.reported` rereads the row, but its status test answers only whether it is already `reported`; it does not reject a `running` row (`internal/turn/report.go:768`). The report path renders and posts before attempting durable completion (`internal/turn/report.go:573`, `internal/turn/report.go:584`, `internal/turn/report.go:604`).

[V] `CompleteRunReport` accepts `succeeded`, `failed`, and already `reported`, but rejects `running` (`internal/store/runs.go:491`). Its transaction records report status and comment ID, continuation-announcement identity, and refinement consumption or the applicable watermark advance (`internal/store/runs.go:499`). Repeated completion can overwrite the recorded result comment ID; this is not independently a first-delivery receipt invariant.

[V] On completion failure the return value retains `Posted.CommentID`. The daemon's `orphaned` handling only logs that identity (`internal/daemon/tick.go:1957`). There is no report-specific retry counter or durable post receipt on this path. Because source consumption did not commit, a later attempt can post again. `context.WithoutCancel` protects recording against cancellation after a confirmed post, but it does not make an invalid state valid or solve another database failure or an uncertain POST response (`internal/turn/report.go:590`).

Inference from these verified branches: the refusal case produces a deterministic cycle—unfinished row, replay, external post, local rejection, unconsumed source, repeat. An early reportability check would prevent those invalid posts, but would leave the queue stranded unless another transition finishes or otherwise resolves the row. Recovering an existing post would bound duplication, but would likewise not make completion of a running row legal. Both state lifecycle and delivery recovery belong in scope.

### Live work and abandoned work are already treated differently in some layers

[V] Daemon `offer` silently skips a session already in flight and neither posts nor advances its watermark (`internal/daemon/tick.go:851`). Task locking happens before replay lookup (`internal/turn/execute.go:541`). A turn simply lasting longer than one tick therefore does **not** establish a production path into unsafe replay in this revision. The missing reportability guard remains an unsafe boundary, but existing live-work protections should be preserved rather than described as absent.

[V] Startup already calls `ReconcileOrphans` and then reports abandoned runs (`internal/daemon/daemon.go:499`, `internal/daemon/daemon.go:519`). The store selects all running rows and transactionally marks them failed/abandoned with finish time/duration, abandons meter attempts, updates sessions and reconciles related recovery state (`internal/store/runs.go:640`). It relies on exclusive daemon startup ownership; it is not a detector for live workers during normal ticks.

[V] Shutdown intentionally permits rows to remain running after the grace deadline and documents next-start repair (`internal/daemon/daemon.go:672`). Consequently, the owner-reported killed-process occurrence is not fully explained by an absence of startup reconciliation in this checked-out revision. Its deployed revision, restart sequence and recovery outcome remain unverified. A blanket live-tick call that abandons every running row would violate the ownership assumption of the existing repair operation.

### Runless reports and replies share the delivery risk

[V] `ReportReply` checks whether a source was consumed, posts, then consumes it (`internal/turn/report.go:661`, `internal/turn/report.go:695`). When consumption fails it returns the posted comment identity, but the source remains eligible for a new post on retry (`internal/turn/report.go:705`). `ReportRun` when `RunID == 0` has the same post-before-consumption gap (`internal/turn/report.go:594`). These paths require no stranded run row to duplicate delivery.

[V] Report status markers identify a task reference and ordinal (`internal/report/marker.go:57`); ordinary reply markers identify only the task (`internal/report/marker.go:69`). Runless preparation failures borrow the next ordinal without allocating it (`internal/turn/report.go:807`). Thus successive runless failures and a later real run can share a status marker. A universal recovery scheme cannot assume the current status marker uniquely identifies every logical delivery.

[V] Refinement reports can be delivered to a child issue while rendering the parent's execution task identity (`internal/turn/story_refinement.go:17`, `internal/turn/report.go:543`). Recovery must use the actual destination as well as the correct logical delivery identity.

### Existing recovery building block and its limits

[V] Story-refinement acknowledgement recovery lists destination comments and checks numeric bot authorship, marker, exactly one match, positive comment ID, and exact body before recording recovered delivery (`internal/turn/story_refinement.go:127`). This is useful existing precedent, but its identity and body policy cannot be copied without examining the wider report cases above.

[V] `ListCommentsAfter(..., 0)` retrieves a full thread, filters locally, and sorts the results (`internal/github/comments.go:41`, `internal/github/comments.go:58`). Inference: searching the complete discussion on every report attempt could replace comment spam with additional read traffic, especially on the long thread reported by the owner. No comment-edit API was found in the inspected client surface. Updating an existing report is therefore an additional capability decision, not an existing turnkey recovery action.

## Domain / Problem Space

The system must preserve two independent promises: a logical delivery should not repeatedly create external comments after delivery has already succeeded, and every accepted trigger must eventually have an explainable terminal disposition without consuming unfinished work out of order. The owner expects an early managed-plan refusal to result in one explanatory comment; active work should defer silently; abandoned work should close and report once.

The repository supports the following distinctions:

| Situation | Verified behavior now | Required planning distinction |
| --- | --- | --- |
| Legitimately active turn | Session in-flight guard and task lock protect normal dispatch | Defer; do not infer abandonment from age or status alone |
| Managed approval refuses after allocation | Error escapes, source stays unconsumed, row stays running | Resolve the lifecycle and give one refusal explanation |
| Process lost before terminal persistence | Startup reconciliation already exists | Explain incident discrepancy and preserve exclusive recovery ownership |
| Finished report posts but local completion fails | Comment ID returned/logged; later delivery can post again | Recover delivery separately from completing local bookkeeping |
| Runless post succeeds but consumption fails | Source remains eligible; markers may not identify one trigger | Give replies/runless reports a recovery identity tied to logical delivery |

These are source-derived distinctions, not results of a runtime fault-injection test. The database transaction already protects local atomicity; the missing guarantee spans the external side effect and that transaction. Recording intent before posting can help recovery discover unfinished delivery, but intent by itself cannot prove whether a request reached GitHub. Searching an existing delivery can help resolve that uncertainty only if identity, authorship, destination and ambiguity policy are sound.

## Constraints & Risks

- [V] Preserve ownership: SQL stays in the store, turn/report orchestration stays separate from daemon timing, and forge mutations continue through typed redacted bodies. Relevant security rules require redaction and numeric bot provenance (`.aiteam/APP_CONSTRAINTS.md:51`, `.aiteam/APP_CONSTRAINTS.md:57`). Marker-only matching could trust copied or forged content; existing acknowledgement recovery provides stronger checks.
- [V] CONC-7 requires the session-running transition last and prohibits replacing orphan reconciliation with compensating state logic (`.aiteam/APP_CONSTRAINTS.md:185`). Plan mode must reconcile any proposed preparation cleanup/finalization with this explicit rule; it must not silently introduce a competing orphan-repair mechanism.
- Inference: delaying allocation can prevent refusal-created rows, but allocation currently supplies the ordinal/log identity used during preparation. The lifecycle must be considered across all post-allocation failure points; deleting or rolling back an allocated row is not automatically equivalent to moving allocation safely.
- Inference: accepting `running` in `CompleteRunReport` would remove a useful invariant instead of establishing a real terminal outcome. It could falsely report active work and consume a trigger prematurely.
- Inference: a live-process stranded row needs authoritative ownership evidence before recovery. Neither a missing log, elapsed time, nor `status=running` alone proves that no worker can finish it. Startup's all-running repair is safe only within its startup ownership assumptions.
- Inference: retry limits must separate retrying bookkeeping from posting another comment. Stopping all retries after an arbitrary count could hide an unresolved queue blockage. The owner asks for bounded repeated posting and visible failure; policy for repair/escalation remains a Plan decision.
- Inference: marker collisions, several historical duplicates, changed rendered body, a deleted comment, and an uncertain POST response all require explicit recovery decisions. Exact-body recovery is conservative but may fail if a regenerated report changes; marker-only recovery is too weak.
- [O] The owner relates duplicate comments to API pressure in #208. That issue was not fetched; this research establishes potential request amplification from this repository rather than independently measuring the quota impact.

### Existing verification and missing regression scenarios

[V] `TestCompleteRunReportIsOneTransaction` covers running-row rejection and atomicity (`internal/store/store_test.go:1220`). Report tests cover post failure, render failure, successful replay, runless replies and preparation failures (`internal/turn/report_test.go:82`, `internal/turn/report_test.go:148`, `internal/turn/report_test.go:230`, `internal/turn/report_test.go:333`, `internal/turn/report_test.go:372`). Startup abandoned reporting and closed-issue recovery have tests (`internal/daemon/daemon_test.go:32`, `internal/daemon/daemon_test.go:120`), as does in-flight skipping (`internal/daemon/crossrepo_test.go:281`).

[V] `TestQueueStopsWhenAPostedReportWasNotRecorded` uses a scripted reporter to assert queue stop, watermark behavior and logging; it does not establish bounded external posts across retries or restart (`internal/daemon/tick_test.go:399`). Inference from inspected coverage: the material missing scenarios are a real managed-not-ready refusal through allocation and subsequent retry; successful external post followed by real local completion failure with repeated recovery; report/reply marker collisions and ambiguous matches; and runless post-success/consumption-failure recovery. Preservation checks also need active-turn silence, atomic consumption, and startup abandoned-run behavior.

[V] Existing test conventions use real migrated SQLite for store transactions, real git plus fake runners for turns, and HTTP/fake executor daemon fixtures. The required aggregate check is `make check` (format, vet, pinned lint and race tests; `Makefile:25`). This research performed static inspection only: no tests ran, no failing test was added, and no implementation was attempted. The identified coverage gaps are research findings, not a measured coverage report.

## Open Questions

1. **Incident reconciliation:** why did row 283 evade the startup orphan handling already present here? A future investigation needs the deployed revision and daemon startup/recovery sequence. The supplied logs establish the owner's observation but do not settle that discrepancy. Planning should not depend on assuming startup repair is absent.
2. **Refusal versus infrastructure error:** which preparation outcomes should become a consumed, terminal explanatory refusal, and which must remain retryable? Plan mode should distinguish the known managed-not-ready refusal from transient preparation failures and preserve queue ordering.
3. **Lifecycle ownership:** what authoritative signal can identify a current-process allocated row that has no remaining executor? The design must preserve existing live-turn guards and CONC-7 while resolving deterministic preparation failure.
4. **Delivery identity:** what stable identity covers allocated runs, runless status reports and ordinary replies, including refinement destinations? Existing task/ordinal and task-only markers are insufficient as a universal unique key.
5. **Recovery policy:** how should the system handle multiple matches from existing spam, body drift, deleted reports, and an uncertain post response? Should it adopt one existing comment, reconcile a stored receipt, or require a visible repair action? These are design/product choices for Plan mode, not established behavior.
6. **Retry and escalation policy:** how should unresolved recording failures surface once repeated posting is suppressed, without silently skipping the blocked source? Daemon-owned retry scheduling and any durable failure accounting need an explicit policy.

No question was sent to the owner because this run is explicitly headless. Assumptions: investigate the broader refusal case as first-class scope; retain the readiness gate; treat the owner's desired one-comment refusal as intended behavior; treat current checkout behavior as the implementation baseline while keeping historical deployment facts unresolved. No external protocol/API fact was needed to establish the repository defect, so no web research or forge access was used.

## Recommended scope for Plan mode

Cover the complete failure chain: preparation/run lifecycle for deterministic refusal and other post-allocation errors; terminal-state gating before reporting; recovery of delivery after post-success/local-record-failure; runless replies and runless reports; preservation of active-work deferral and startup orphan reconciliation; bounded duplicate effects with visible unresolved failure; and regression scenarios that exercise repeated attempts and restart. Reuse existing transactional completion, ownership checks and authenticated acknowledgement recovery where their semantics fit.

Keep the managed-plan readiness requirement intact. Do not frame the work as merely adding startup reconciliation, relaxing terminal-state checks, or searching current markers indiscriminately. Do not include unrelated rate-limit redesign, manual runtime database repairs, implementation tasks, or automatic transition into another mode. Plan mode should resolve the policy questions above and assign implementation scope against the relevant constraints.
