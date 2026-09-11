# Research Brief — a-report-that-posts-but-fails-to-record

## Request

Research issue #210's indefinite duplicate-report loop, including the owner's widened scope: an ordinary preparation refusal after run allocation can strand a running row just as a lost process can. Determine how allocation, replay, report delivery, durable completion, consumption, live-worker protection and orphan recovery interact; identify the boundaries and evidence needed for Plan mode. Runs 0001–0003 are Research only and make no production changes.

Run 0002 request (2026-09-10): “List all open questions, give me a suggestion on those you can already answer. Always explain a suggestion and list pros vs cons if there are different options.” That update preserved the six original question identities, expanded their bundled decisions, and provided recommendations that were not yet approved in run 0002.

Run 0003 request: the owner explicitly accepted the recommendations for all six numbered questions (“1: yes go with your proposal”; “2” through “6: ok with recommendation”). This update records those directions as owner-approved research decisions for a future human-selected Plan run. It does not approve an implementation plan, select another mode, settle historical facts, or supply unspecified policy values.

Provenance: **[D3]** direction explicitly approved by the owner in run 0003; **[V]** repository behavior verified in run 0002 through static code inspection; **[O]** evidence reported by the owner, not independently checked against runtime state; **[W]** second-hand unverified evidence; **[R0]** material retained from run 0001 in this same session. Earlier findings below retain their historical citations; run 0002 verification and corrections are recorded separately. No source verification was repeated in run 0003; [V] continues to mean verification in run 0002, not in the current run. Inferences and recommendations are identified explicitly rather than presented as observed execution.

[O] On #170, run 12 / database row 287 was allocated before a managed-plan refusal. The supplied logs show worktree preparation at 23:14:49, refusal at 23:14:52, then repeated report posts followed by rejection of completion because the row had not finished. The owner reports no agent launch or run log. Earlier run 11 / row 283 allegedly followed process loss, with duplicate comment IDs 5608586009, 5608598042 and 5608609360. Manual terminal-state updates reportedly stopped both loops. These incidents were not reproduced against the daemon or inspected in its database; runtime state and forge access were outside this research.

## Codebase Context

### Two-pass map and ownership

[R0] This is a single Go 1.26 module (`go.mod:1`). The relevant flow is daemon dispatch → in-flight/task ownership checks → existing-run lookup → preparation/allocation → execution and harvest → report rendering/posting → transactional report completion and source consumption. The daemon owns dispatch, concurrency and retry timing; the turn layer conducts one turn and delivers its result; the store owns durable state and SQL; the report layer produces typed redacted bodies; the GitHub client performs external requests.

[R0] Production setup acquires a singleton lock before returning the daemon (`internal/daemon/setup.go:245`). `Daemon.Run` starts recovery before normal ticking (`internal/daemon/daemon.go:639`). These ownership boundaries matter when deciding whether a running row is still active or can be declared abandoned.

The focused second pass covered preparation and replay, managed approval, harvest, both reporter entry points, store report transactions, daemon queue behavior/startup/shutdown, report markers, existing acknowledgement recovery, comment listing, and their tests. The findings below retain the relevant source locations rather than reproducing unrelated package internals.

### Allocation and refusal: the deterministic stranded-row path

[R0] The allocation operation is `AllocateRun`, not `CreateRun`; its wrapper reserves the run ordinal and log path before process launch (`internal/turn/prepare.go:1438`). Preparation allocates at `internal/turn/prepare.go:261`, then reaches managed approval at `internal/turn/prepare.go:353`.

[R0] `ApproveManagedStories` requires the planning state and matching generation, `ready=1`, and no unconsumed refinements (`internal/store/approved_stories.go:25`). Failure produces the supplied error, `store: plan is not ready or has pending refinements` (`internal/store/approved_stories.go:34`). The readiness refusal itself is intentional.

[R0] This preparation error escapes through `Execute` (`internal/turn/execute.go:614`) before the allocated run is copied to the returned result (`internal/turn/execute.go:620`). The deferred task-lock release still executes (`internal/turn/execute.go:565`), but no corresponding cleanup finishes the allocated row. The daemon leaves the source comment unconsumed (`internal/daemon/tick.go:1745`).

[R0] A later execution attempt finds the existing run (`internal/turn/execute.go:575`). Replay maps every unreported row, including `running`, to `DispositionAlreadyRan`; only failed rows receive an explicit failed outcome and reason (`internal/turn/execute.go:632`). Thus the source supports the widened incident mechanism without any process crash being necessary.

[R0] This is a broader lifecycle gap than the readiness check alone. Allocation also precedes fallible synchronization recording, history, managed handoff/refinement/approved context, and prompt preparation (`internal/turn/prepare.go:272`, `internal/turn/prepare.go:285`, `internal/turn/prepare.go:356`). Harvest can fail while recording a resume token or terminal result (`internal/turn/harvest.go:96`, `internal/turn/harvest.go:107`). Inference: moving only one gate would remove one deterministic trigger while leaving other ways to lose finalization.

### Reportability and the external-post/local-record gap

[R0] `Reporter.reported` rereads the row, but its status test answers only whether it is already `reported`; it does not reject a `running` row (`internal/turn/report.go:768`). The report path renders and posts before attempting durable completion (`internal/turn/report.go:573`, `internal/turn/report.go:584`, `internal/turn/report.go:604`).

[R0] `CompleteRunReport` accepts `succeeded`, `failed`, and already `reported`, but rejects `running` (`internal/store/runs.go:491`). Its transaction records report status and comment ID, continuation-announcement identity, and refinement consumption or the applicable watermark advance (`internal/store/runs.go:499`). Repeated completion can overwrite the recorded result comment ID; this is not independently a first-delivery receipt invariant.

[R0] On completion failure the return value retains `Posted.CommentID`. The daemon's `orphaned` handling only logs that identity (`internal/daemon/tick.go:1957`). There is no report-specific retry counter or durable post receipt on this path. Because source consumption did not commit, a later attempt can post again. `context.WithoutCancel` protects recording against cancellation after a confirmed post, but it does not make an invalid state valid or solve another database failure or an uncertain POST response (`internal/turn/report.go:590`).

Inference from these verified branches: the refusal case produces a deterministic cycle—unfinished row, replay, external post, local rejection, unconsumed source, repeat. An early reportability check would prevent those invalid posts, but would leave the queue stranded unless another transition finishes or otherwise resolves the row. Recovering an existing post would bound duplication, but would likewise not make completion of a running row legal. Both state lifecycle and delivery recovery belong in scope.

### Live work and abandoned work are already treated differently in some layers

[R0] Daemon `offer` silently skips a session already in flight and neither posts nor advances its watermark (`internal/daemon/tick.go:851`). Task locking happens before replay lookup (`internal/turn/execute.go:541`). A turn simply lasting longer than one tick therefore does **not** establish a production path into unsafe replay in this revision. The missing reportability guard remains an unsafe boundary, but existing live-work protections should be preserved rather than described as absent.

[R0] Startup already calls `ReconcileOrphans` and then reports abandoned runs (`internal/daemon/daemon.go:499`, `internal/daemon/daemon.go:519`). The store selects all running rows and transactionally marks them failed/abandoned with finish time/duration, abandons meter attempts, updates sessions and reconciles related recovery state (`internal/store/runs.go:640`). It relies on exclusive daemon startup ownership; it is not a detector for live workers during normal ticks.

[R0] Shutdown intentionally permits rows to remain running after the grace deadline and documents next-start repair (`internal/daemon/daemon.go:672`). Consequently, the owner-reported killed-process occurrence is not fully explained by an absence of startup reconciliation in this checked-out revision. Its deployed revision, restart sequence and recovery outcome remain unverified. A blanket live-tick call that abandons every running row would violate the ownership assumption of the existing repair operation.

### Runless reports and replies share the delivery risk

[R0] `ReportReply` checks whether a source was consumed, posts, then consumes it (`internal/turn/report.go:661`, `internal/turn/report.go:695`). When consumption fails it returns the posted comment identity, but the source remains eligible for a new post on retry (`internal/turn/report.go:705`). `ReportRun` when `RunID == 0` has the same post-before-consumption gap (`internal/turn/report.go:594`). These paths require no stranded run row to duplicate delivery.

[R0] Report status markers identify a task reference and ordinal (`internal/report/marker.go:57`); ordinary reply markers identify only the task (`internal/report/marker.go:69`). Runless preparation failures borrow the next ordinal without allocating it (`internal/turn/report.go:807`). Thus successive runless failures and a later real run can share a status marker. A universal recovery scheme cannot assume the current status marker uniquely identifies every logical delivery.

[R0] Refinement reports can be delivered to a child issue while rendering the parent's execution task identity (`internal/turn/story_refinement.go:17`, `internal/turn/report.go:543`). Recovery must use the actual destination as well as the correct logical delivery identity.

### Existing recovery building block and its limits

[R0] Story-refinement acknowledgement recovery lists destination comments and checks numeric bot authorship, marker, exactly one match, positive comment ID, and exact body before recording recovered delivery (`internal/turn/story_refinement.go:127`). This is useful existing precedent, but its identity and body policy cannot be copied without examining the wider report cases above.

[R0] `ListCommentsAfter(..., 0)` retrieves a full thread, filters locally, and sorts the results (`internal/github/comments.go:41`, `internal/github/comments.go:58`). Inference: searching the complete discussion on every report attempt could replace comment spam with additional read traffic, especially on the long thread reported by the owner. No comment-edit API was found in the inspected client surface. Updating an existing report is therefore an additional capability decision, not an existing turnkey recovery action.

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

- [R0] Preserve ownership: SQL stays in the store, turn/report orchestration stays separate from daemon timing, and forge mutations continue through typed redacted bodies. Relevant security rules require redaction and numeric bot provenance (`.aiteam/APP_CONSTRAINTS.md:51`, `.aiteam/APP_CONSTRAINTS.md:57`). Marker-only matching could trust copied or forged content; existing acknowledgement recovery provides stronger checks.
- [R0] CONC-7 requires the session-running transition last and prohibits replacing orphan reconciliation with compensating state logic (`.aiteam/APP_CONSTRAINTS.md:185`). Plan mode must reconcile any proposed preparation cleanup/finalization with this explicit rule; it must not silently introduce a competing orphan-repair mechanism.
- Inference: delaying allocation can prevent refusal-created rows, but allocation currently supplies the ordinal/log identity used during preparation. The lifecycle must be considered across all post-allocation failure points; deleting or rolling back an allocated row is not automatically equivalent to moving allocation safely.
- Inference: accepting `running` in `CompleteRunReport` would remove a useful invariant instead of establishing a real terminal outcome. It could falsely report active work and consume a trigger prematurely.
- Inference: a live-process stranded row needs authoritative ownership evidence before recovery. Neither a missing log, elapsed time, nor `status=running` alone proves that no worker can finish it. Startup's all-running repair is safe only within its startup ownership assumptions.
- Inference: retry limits must separate retrying bookkeeping from posting another comment. Stopping all retries after an arbitrary count could hide an unresolved queue blockage. The owner asks for bounded repeated posting and visible failure; policy for repair/escalation remains a Plan decision.
- Inference: marker collisions, several historical duplicates, changed rendered body, a deleted comment, and an uncertain POST response all require explicit recovery decisions. Exact-body recovery is conservative but may fail if a regenerated report changes; marker-only recovery is too weak.
- [O] The owner relates duplicate comments to API pressure in #208. That issue was not fetched; this research establishes potential request amplification from this repository rather than independently measuring the quota impact.

### Existing verification and missing regression scenarios

[R0] `TestCompleteRunReportIsOneTransaction` covers running-row rejection and atomicity (`internal/store/store_test.go:1220`). Report tests cover post failure, render failure, successful replay, runless replies and preparation failures (`internal/turn/report_test.go:82`, `internal/turn/report_test.go:148`, `internal/turn/report_test.go:230`, `internal/turn/report_test.go:333`, `internal/turn/report_test.go:372`). Startup abandoned reporting and closed-issue recovery have tests (`internal/daemon/daemon_test.go:32`, `internal/daemon/daemon_test.go:120`), as does in-flight skipping (`internal/daemon/crossrepo_test.go:281`).

[R0] `TestQueueStopsWhenAPostedReportWasNotRecorded` uses a scripted reporter to assert queue stop, watermark behavior and logging; it does not establish bounded external posts across retries or restart (`internal/daemon/tick_test.go:399`). Inference from inspected coverage: the material missing scenarios are a real managed-not-ready refusal through allocation and subsequent retry; successful external post followed by real local completion failure with repeated recovery; report/reply marker collisions and ambiguous matches; and runless post-success/consumption-failure recovery. Preservation checks also need active-turn silence, atomic consumption, and startup abandoned-run behavior.

[R0] Existing test conventions use real migrated SQLite for store transactions, real git plus fake runners for turns, and HTTP/fake executor daemon fixtures. The required aggregate check is `make check` (format, vet, pinned lint and race tests; `Makefile:25`). This research performed static inspection only: no tests ran, no failing test was added, and no implementation was attempted. The identified coverage gaps are research findings, not a measured coverage report.

## Current-source verification — run 0002

[V] A fresh focused two-pass codebase tour confirms that the central defect remains: allocation now occurs at `internal/turn/prepare.go:264`, followed by managed preparation at `:353`; readiness rejection remains at `internal/store/approved_stories.go:25`. Preparation can return before `result.Run` is populated (`internal/turn/execute.go:614`, `:620`). Replay still treats unreported running rows as already run (`:632`). The reporter checks already-reported status rather than terminal reportability (`internal/turn/report.go:768`), posts at `:584`, and completes at `:604`. Completion rejects running rows (`internal/store/runs.go:491`) and its retry can replace the stored comment ID (`:499`). These are source observations, not executed reproductions.

[V] Active-session deferral remains at `internal/daemon/tick.go:851`, and task locking precedes replay (`internal/turn/execute.go:541`). Startup calls recovery before ticks (`internal/daemon/daemon.go:647`); store orphan recovery is at `internal/store/runs.go:633`. Shutdown still relies on next-start reconciliation (`internal/daemon/daemon.go:672`). Harvest has other fallible terminal-persistence boundaries (`internal/turn/harvest.go:110`, `:121`). Current source does not establish why historical row 283 escaped recovery.

[V] Runless report/reply exposure remains (`internal/turn/report.go:594`, `:695`, `:705`). Borrowed run ordinals (`:807`), task-plus-ordinal/task-only markers (`internal/report/marker.go:57`, `:69`), and differing refinement destination (`internal/turn/report.go:543`) still constrain identity design. Authenticated acknowledgement recovery checks numeric author, marker, positive comment ID, uniqueness and exact body (`internal/turn/story_refinement.go:127`). Full comment listing still fetches all pages before local filtering, even with a nonzero cursor (`internal/github/comments.go:58`). The inspected client has no comment-edit operation.

[V] CONC-7 remains applicable (`.aiteam/APP_CONSTRAINTS.md:183`): session-running transition last; do not replace orphan reconciliation with compensating state logic. SQL, daemon timing, typed redacted transport bodies and numeric bot provenance remain mandatory boundaries. The scripted queue regression at `internal/daemon/tick_test.go:399` does not prove bounded posting across real completion failures and restart. No tests were run; code and incident behavior were not reproduced. No external API semantics are required for these source-derived recommendations.

## Open Questions

### Owner-approved directions — run 0003

[D3] All six numbered recommendations from run 0002 are accepted. The approval covers the recommended defaults and their stated scope; it does not select the alternatives listed for comparison. The retained rationale and pros/cons below explain those decisions. This is approval of research directions, not a completed implementation plan or permission to switch modes automatically.

| Original question | Approved direction | Still unresolved |
| --- | --- | --- |
| 1 — Historical incident | Proceed with the confirmed refusal and delivery defects; investigate the earlier recovery discrepancy separately when evidence is available | Why row 283 escaped recovery: deployed revision, restart sequence and recovery outcome remain unverified |
| 2 — Refusal versus retry | Give a definite managed-not-ready refusal one explanation that nothing launched; require a fresh command after readiness changes. Consume only after confirmed delivery and successful local recording. Keep transient infrastructure failures retryable and preserve source order | Full error classification and exact refusal wording |
| 3 — Allocation and ownership | Perform feasible deterministic readiness checks before allocation; account for every later exit and distinguish a never-launched outcome. Preserve authoritative worker ownership, silent active-work deferral and exclusive startup recovery; require terminal reportability before rendering/posting | Exact finalization/ownership contract, including failed terminal persistence and compliance with CONC-7; approval does not waive that rule |
| 4 — Delivery identity and persistence | Use stable logical delivery identity covering task, destination, trigger and purpose plus run identity where applicable; combine durable intent, remote marker and confirmed receipt, retaining verifiable intended redacted content | Identity encoding/versioning, retention, legacy compatibility and lookup details |
| 5 — Recovery policies | Prefer a trustworthy receipt; otherwise adopt a deterministically selected strongly verified equivalent delivery. Diagnose conflicts and uncertain delivery while suppressing new posts. Recover against retained intended content; honor confirmed historical delivery after deletion. Leave historical duplicates in place and defer comment-edit capability | Equivalent-match selection and legacy rules, trustworthy proof of nondelivery, repaired-evidence resumption and any separately authorized resend policy |
| 6 — Retry and escalation | Separate posting from bookkeeping retries; after confirmed delivery retry only local completion. Retain unresolved state and diagnostics, use daemon-owned backoff and deduplicated operational visibility, and preserve consumption ordering. Ambiguity or confirmed delivery immediately suppresses new posts | Backoff/cap, escalation threshold and surface, diagnostic retention, and authorized repair/resume behavior; no numeric value was approved or inferred |

The **six original questions remain numbered 1–6** below so the approval maps directly to the prior conversation. Their “Suggestion” and “recommended” labels retain the run 0002 rationale and now identify the approved directions above. Question 1 remains a factual evidence gap despite approval of how to proceed; the remaining “Still to specify” entries are design/policy details for Plan, not reopened requests to approve these same defaults. No new code research, runtime investigation, or external API verification was conducted in run 0003.

### 1. Incident reconciliation: why did historical row 283 evade existing startup recovery?

**Status:** unresolved fact. [O] The incident and manual repair are owner-reported. [V] Current startup already reconciles running rows before normal ticking (`internal/daemon/daemon.go:647`; `internal/store/runs.go:633`). Neither the deployed revision nor the actual restart/recovery sequence was inspected.

**Suggestion:** keep the historical cause open, preserve the supplied timeline, and proceed with the independently verified refusal and delivery defects. When incident evidence is available, compare the deployed revision, process stop/restart sequence, startup recovery outcome/errors and relevant task/run identities. The operator is the source of that evidence; a future authorized investigation can interpret it. No runtime database mutation is warranted by this research.

| Option | Pros | Cons |
| --- | --- | --- |
| Proceed with verified defects; investigate the historical discrepancy separately — recommended | Avoids inventing an absent recovery mechanism; does not delay the routine refusal fix | Cannot yet claim every cause of the historical incident is explained |
| Hold all work until the incident is fully reconstructed | Gives a more complete historical root cause before changing behavior | Delays a confirmed defect; required evidence may no longer exist |
| Assume shutdown/startup reconciliation is missing | Superficially straightforward explanation | Contradicts this checkout and risks redundant or unsafe repair logic |

### 2. Refusal versus infrastructure error: which outcomes consume the trigger?

**Suggestion:** a known deterministic managed-not-ready refusal should produce one explanation that no agent launched and that a fresh command is needed after readiness changes. Consume that trigger only after delivery is confirmed and the local completion/consumption transaction succeeds. Treat transient infrastructure failures as retryable, preserving source order. Unclassified errors should not silently become accepted refusals.

**Why:** [O] The owner explicitly wants one explanatory refusal. [V] Readiness rejection is deliberate (`internal/store/approved_stories.go:25`), while the current allocation/error path strands a run. A stale command should not unexpectedly execute hours later merely because readiness changed. The distinction needs typed/domain-aware classification, not a blanket “every preparation error is terminal” policy.

| Option | Pros | Cons |
| --- | --- | --- |
| Terminal refusal with a fresh command required — recommended | Predictable intent; unblocks later source work once refusal delivery is recorded; one explanation | Owner must submit a new command |
| Keep the refused command pending and execute automatically once ready | No resubmission; preserves original request | Can block the queue and execute at an unexpected later time; explanation still needs deduplication |
| Consume every preparation failure | Simple queue progress | Loses work during temporary outages and masks infrastructure failures; not recommended |

**Still to specify:** the complete classification of readiness/refinement conflicts versus retryable infrastructure errors and the refusal wording. The owner has approved the behavior above; Plan must specify its classification and wording. Code inspection supplies the boundary and the known readiness case.

### 3. Lifecycle ownership: what proves an allocated row has no remaining executor?

This original question includes both allocation placement and live-versus-abandoned handling.

**3a — Allocation and preparation failure. Suggestion:** validate deterministic readiness before allocating where dependencies permit, while explicitly assigning responsibility for every remaining post-allocation exit. Allocation supplies run/log/prompt identity, so moving one gate alone does not close the full lifecycle gap (`internal/turn/prepare.go:264`, `:353`; `internal/turn/execute.go:614`). If an allocated run must be retained after refusal, its outcome must clearly distinguish “never launched” from an agent failure.

| Option | Pros | Cons |
| --- | --- | --- |
| Delay allocation through feasible deterministic checks and account for every later exit — recommended | Prevents the known refusal trigger; addresses other fallible preparation stages | Requires careful sequencing because later preparation uses allocated identity |
| Allocate early, then retain and finalize a known refusal | Preserves audit history and stable references | Failed-looking runs without processes need clear semantics; must satisfy CONC-7 |
| Delete/roll back an allocated row after later refusal | Removes the apparent poisoned row | Can lose evidence or leave ordinal/log/filesystem references inconsistent; moving a boundary is safer than ad hoc deletion |

**3b — Active versus abandoned work. Suggestion:** use authoritative worker/session ownership. Preserve current in-flight checks and locks; defer active work silently. A worker that has returned/refused can supply positive evidence that its execution path ended. Reserve whole-daemon orphan reconciliation for exclusive startup ownership. Reject nonterminal runs before report rendering or posting. Do not infer abandonment from age, a missing log or running status alone.

| Option | Pros | Cons |
| --- | --- | --- |
| Ownership-aware handling plus reportability guard — recommended | Protects long turns; prevents invalid external reports; preserves existing startup repair | A guard alone leaves a stranded row, so lifecycle resolution is still necessary |
| Time/PID/log heuristics or global orphan repair on ordinary ticks | Appears simpler and can detect some stale work quickly | Risks terminating/reporting legitimate work; global repair has startup-only ownership assumptions |
| Allow reporting while still running | Avoids the completion rejection | Falsely treats unfinished work as terminal and can consume it prematurely; reject this option |

**Still to specify:** the exact transition/ownership contract for a known prelaunch exit or failed terminal persistence. [V] CONC-7 (`.aiteam/APP_CONSTRAINTS.md:183`) forbids competing compensating state logic. Plan must demonstrate that its normal owner-controlled finalization fits this rule, or explicitly propose a narrowly justified normative change; research does not grant one. Startup reconciliation remains required. A second “cleanup everything running” mechanism is not recommended.

### 4. Delivery identity: what uniquely identifies runs, runless reports, and replies?

**4a — Identity. Suggestion:** persist one logical delivery identity bound to canonical execution task, actual destination, trigger namespace/identity and operation kind, with run identity when allocated. Keep it stable across repeated attempts and restart. Different retries of one explanation share the key; unrelated triggers must not. Refinements must use their actual delivery destination.

**Why:** [V] Current task/ordinal and task-only markers are not universally unique; runless reports borrow an ordinal that a later run may use (`internal/report/marker.go:57`, `:69`; `internal/turn/report.go:807`). Numeric bot authorship and full identity, not merely marker presence, must establish trust.

| Option | Pros | Cons |
| --- | --- | --- |
| Stable persisted identity for all logical deliveries — recommended | Covers allocated, runless, reply, refinement and synthetic-trigger cases; supports restart recovery | Requires schema/marker evolution and conservative legacy matching |
| Reuse current markers unchanged | Small change; useful legacy hints for some allocated runs | Known collisions prevent safe universal adoption and can consume the wrong trigger |

**4b — Marker lookup versus intent/receipt. Suggestion:** combine durable pre-post intent, a recoverable remote marker and a confirmed comment receipt. Preserve the intended redacted content or a sufficient verifiable representation. Use receipts on ordinary recovery and remote lookup when delivery is uncertain. Intent is evidence of a planned action, not proof the action happened.

| Option | Pros | Cons |
| --- | --- | --- |
| Intent plus marker plus confirmed receipt — recommended | Preserves identity/content across restart and separates posting from local completion; avoids needless normal thread scans | Additional persisted state and reconciliation logic |
| Search comments on every attempt | Can recover existing comments without a receipt | Current client downloads the full thread even with a cursor; increases API reads; requires strong match rules |
| Record intent only | Makes pending work durable before a crash | Cannot distinguish a request never sent from one that succeeded remotely before the response was lost |

**Still to specify:** key encoding/versioning, retention of redacted intended content, collision-safe legacy compatibility and the lookup strategy supported by the actual client. This research makes no external exactly-once delivery guarantee.

### 5. Recovery policy: how are duplicates, body drift, deletion and uncertain responses handled?

**5a — Multiple historical matches. Suggestion:** prefer a previously recorded valid receipt. Without one, adopt one deterministic existing comment only when numeric bot identity, full delivery identity, destination and equivalent intended content prove all candidates represent that same delivery. Conflicting or legacy-colliding candidates should stop new posts and produce a diagnostic. Do not automatically delete old duplicates.

- **Adopt equivalent matches:** pro — recovers the observed spam without adding another comment; con — requires carefully defined equivalence and legacy rules.
- **Reject every multiple-match result:** pro — simpler and conservative; con — leaves already-spammed incidents requiring manual intervention even when equivalence can be proven.
- **Delete duplicates automatically:** pro — cleans the visible thread; con — removes historical evidence and expands mutation scope; not recommended.

**5b — Changed body or editing an existing report. Suggestion:** recover against the persisted intended redacted content, not whatever a new render happens to produce. Keep historical delivery evidence stable; conflicting content needs diagnostics.

- **Preserve original intended content:** pro — recovery survives renderer/state changes without rewriting history; con — storage/versioning burden.
- **Require equality with a fresh render:** pro — avoids new content storage; con — legitimate content drift may block recovery permanently.
- **Update the old comment to the latest body:** pro — shows fresher information without a new comment; con — alters history and requires an edit API absent from the inspected client plus an explicit update policy. Defer this capability unless product requirements justify it.

**5c — Deleted report. Suggestion:** a trusted confirmed historical receipt remains proof of delivery; do not automatically recreate a subsequently deleted comment. This recommendation applies to a real confirmed receipt, not intent or an ambiguous timeout.

- **Honor the historical receipt:** pro — avoids reversing intentional removal and bounds comment effects; con — the explanation may no longer be visible.
- **Recreate missing comments automatically:** pro — restores visibility; con — can undo deliberate removal and create a restoration loop. Any explicit resend should be a separately authorized delivery, not an automatic retry.

**5d — Unknown POST outcome or incomplete lookup. Suggestion:** recover a strongly verified existing delivery when possible. If the outcome remains ambiguous, fail closed for new posting and expose unresolved state. Inability to find a comment is not automatically proof that the prior request had no remote effect. A failed or incomplete lookup must never authorize a new post.

- **Block new posts while ambiguous:** pro — avoids recreating the duplicate loop; con — may require operator repair and delay a message that actually never arrived.
- **Repost after a timeout or apparent absence:** pro — restores progress faster when nothing was delivered; con — may duplicate a remotely successful request.

**Still to specify:** deterministic equivalent-match selection, legacy collision handling, how repaired evidence permits resumption, and a trustworthy condition for declaring an uncertain post definitively undelivered. Current repository facts support conservative recovery, not a proof that any negative remote lookup guarantees absence. The conservative directions above are owner-approved; Plan must specify these remaining details. External protocol-specific claims would need separate verified research.

### 6. Retry and escalation: how does failure remain visible without skipping blocked work?

**Suggestion:** separate local bookkeeping retries from permission to post. Once delivery is confirmed, only retry local completion. Persist unresolved delivery state and diagnostics, let the daemon own backoff, and surface a deduplicated operational failure without posting another full report. Keep the source unconsumed until its required transaction succeeds; a retry count is never permission to advance its watermark. Stop fresh external posts immediately when a prior outcome is ambiguous or confirmed, rather than allowing N duplicate reports first.

**Why:** [V] The current completion transaction preserves consumption correctness (`internal/store/runs.go:491`), and the daemon currently logs a returned comment ID without retaining a report-specific recovery state. A hard retry limit changes activity, not the truth of whether work completed. The original request to bound retries is best met by bounding repeat side effects independently of repair attempts.

| Option | Pros | Cons |
| --- | --- | --- |
| Retry bookkeeping/reconciliation with daemon backoff and deduplicated diagnostics — recommended | Recovers transient faults without repeated reports; keeps queue correctness and unresolved work visible | Persistent failures still need repair and explicit observability |
| Stop every kind of retry after N failures | Bounds attempts and traffic | Can permanently strand a recoverable queue; needs a defined repair/resume route |
| Continue fixed-interval full report attempts | Simple and may overcome transient errors | Recreates spam and API pressure; reject this option |
| Consume the trigger when retry budget expires | Unblocks following comments | Silently skips unresolved work and breaks consumption semantics; reject this option |

**Still to specify:** backoff timing/cap, escalation threshold and the user-visible operational surface, durable diagnostic retention and authorized repair/resume behavior. The source and supplied incidents do not justify a particular numeric threshold. Suggest choosing these against operational response expectations and existing daemon scheduling conventions, not inventing an N and calling it evidence-based. Owner/Plan owns that policy.

Run 0002 sent no question to the owner and did not assume approval. Run 0003 records the owner’s subsequent explicit acceptance of all six recommendations and sends no new questions. Both updates reuse the same session despite the date change. Assumptions: the readiness requirement stays intact; the broader refusal case is first-class scope; the owner's requested one-comment explanation is the intended baseline; current checkout is the source baseline while historical deployment facts remain unverified. No code, tests, forge data or runtime state were changed; no tests, external research or live incident investigation ran.

## Recommended scope for Plan mode

Cover the complete failure chain: preparation/run lifecycle for deterministic refusal and other post-allocation errors; terminal-state gating before reporting; recovery of delivery after post-success/local-record-failure; runless replies and runless reports; preservation of active-work deferral and startup orphan reconciliation; bounded duplicate effects with visible unresolved failure; and regression scenarios that exercise repeated attempts and restart. Reuse existing transactional completion, ownership checks and authenticated acknowledgement recovery where their semantics fit.

Keep the managed-plan readiness requirement intact. Do not frame the work as merely adding startup reconciliation, relaxing terminal-state checks, or searching current markers indiscriminately. Do not include unrelated rate-limit redesign, manual runtime database repairs, implementation tasks, or automatic transition into another mode. The owner-approved directions above provide a supported handoff to Plan when the owner explicitly selects it. Plan mode should specify the remaining policy details and assign implementation scope against the relevant constraints, preserving the approved defaults. Research remains the selected mode for run 0003; no Plan artifacts or implementation work were started.
