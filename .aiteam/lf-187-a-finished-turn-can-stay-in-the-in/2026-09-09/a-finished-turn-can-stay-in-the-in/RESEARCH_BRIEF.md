# Research Brief — a-finished-turn-can-stay-in-the-in

## Request

[O] Run 0003 asks: “ok, how would you do the recommended next step?” In this explicitly Research run, that means explaining a concrete, source-grounded approach to logging dispatched sessions and reasons, and distinguishing polling from agent execution. It does not authorize beginning Plan or Implement.

[O] Run 0002 corrects issue #187: withdraw the original in-flight residue diagnosis and investigate why every tick dispatches two workers with `issues=0`, while no agent run is prepared. The requested outcome is to identify the recurring work and improve the interpretation of activity counters, with a session-and-reason dispatch diagnostic suggested by the owner.

[O] The owner reports a clean restart on `03d0481` followed by repeated `dispatched=2 in_flight=2`, including ticks with `issues=0`. The supplied database checks exclude sessions with `needs_poll=1`, running sessions, pending schedules, automatic continuations, active story progress runs and unconsumed refinements. These observations were supplied to this run; production data and logs were not independently read.

**Finding:** [R0, inference] Persistent managed-feature registration is the overlooked dispatch source. Every registered managed parent becomes a candidate on every tick, independently of all the supplied exclusions. Two eligible open managed parents can produce exactly the reported pattern: two polling workers, no new agent runs, clean idle sessions between ticks. The mechanism is verified in the worktree; the identities and count of the production managed parents remain unverified.

Suggested issue title: **“Managed parents dispatch polling workers every tick, obscuring whether any agent work occurred.”** This is a recommendation only; no forge state was changed.

Provenance: **[V]** verified from repository source in run 0003; **[O]** supplied owner evidence; **[W]** second-hand, unverified information; **[R0]** carried forward from earlier research. Inferences and proposals are labelled explicitly. The detailed earlier findings below retain their provenance and revision limits; current-run design findings are identified separately. Research only: no production edits, runtime-state access, tests, commits or pushes.

## Codebase Context

### Repository boundaries and revision limits

[R0] This is a single Go module with command wiring under `cmd/litefactory`, implementation and colocated tests under `internal/`. `daemon` owns discovery, polling, admission and time; `store` owns SQL and durable managed-feature/session state; `turn` owns actual execution; `github` owns forge access.

[R0] The read-only worktree HEAD reflog inspected by the tour ends at merge `225e8bd620c234cd966ab278515bfb4d5ef08204`. The tour did not establish equivalence of the relevant files to deployed revision `03d0481`. All run 0002 source citations below refer to that run’s supplied worktree, not a verified historical deployment snapshot. [R0] Run 0001 similarly did not establish equivalence to its reported deployed revision `fbc4b02`.

### All discovery and dispatch sources

[R0] `Tick` discovers ordinary candidates, dispatches automatic continuations and schedules, then dispatches ordinary candidates without joining their workers (`internal/daemon/tick.go:190`, `:207`, `:210`, `:217`). Only ordinary offers, scheduled offers and automatic continuation dispatch increment `Dispatched` (`internal/daemon/tick.go:938`; `internal/daemon/schedules.go:115`; `internal/daemon/continuation_dispatch.go:75`). Announcement replays do not increment it.

| Source | Relationship to the reported exclusions |
|---|---|
| Forge-listed and comment-discovered issues | Ordinary discovery counts forge-discovered issues; this is what `issues=0` describes (`internal/daemon/tick.go:75`, `:363`, `:368`). |
| Stored managed-feature parents | Added independently after forge discovery and before backlog. Not excluded by any supplied database observation (`internal/daemon/tick.go:383`; `internal/daemon/story_refinements.go:26`). |
| Ordinary session backlog | Added after managed parents (`internal/daemon/tick.go:386`). [O] No flagged or running sessions were observed. |
| Due schedules | Separate dispatch source. [O] All supplied schedule rows were already successful. |
| Automatic continuations | Separate dispatch source. [O] The table was empty. |
| Pending refinements and active story progress | Their absence does not remove managed parents from discovery. Managed registration is enumerated first, before refinement readiness is inspected. |

[R0] Managed-parent enumeration calls `ListManagedFeatures`, resolves each feature's parent session, requires a registered/pollable repository and avoids same-tick duplicate candidates. It does not require pending work (`internal/daemon/story_refinements.go:26`–`:45`). `ListManagedFeatures` selects every row in `managed_features`, without a state, readiness, active-run or pending-refinement predicate (`internal/store/story_refinements.go:109`–`:124`). An ordinary idle session is not automatically enrolled merely because it exists; **its managed-feature registration** is the decisive additional condition identified here.

### The recurring no-run worker lifecycle

[R0] The following path explains why a clean database snapshot between ticks can coexist with recurring dispatch:

1. Each tick adds registered managed parents as candidates, even with an empty incremental forge listing (`internal/daemon/tick.go:383`; `internal/daemon/story_refinements.go:26`).
2. Ordinary admission enters the in-memory set, marks backlog, tries to acquire a pool slot and resolves the parent issue if needed (`internal/daemon/tick.go:827`, `:842`, `:844`, `:971`–`:981`).
3. An explicitly closed parent clears backlog and is skipped without incrementing the dispatch count (`internal/daemon/tick.go:983`–`:992`, `:851`–`:867`). Persistent registration can still rediscover it next tick. Thus the proposed explanation for **two counted dispatches** requires eligible open parents, not merely two registry rows.
4. An eligible open parent starts a worker and increments `Dispatched` (`internal/daemon/tick.go:924`–`:938`).
5. The worker calls `refineStories` before draining parent comments (`internal/daemon/tick.go:1019`–`:1025`). This polls up to 16 children using persisted rotation, before inspecting pending refinements (`internal/daemon/story_refinements.go:65`–`:101`).
6. With no pending refinement, it returns `false, nil` and the worker proceeds to ordinary parent comment draining (`internal/daemon/story_refinements.go:95`–`:101`; `internal/daemon/tick.go:1026`–`:1046`). An empty comment result never reaches comment handling or run preparation (`internal/daemon/tick.go:1241`–`:1247`).
7. A clean drain clears backlog; worker return releases its in-memory admission (`internal/daemon/tick.go:1042`–`:1046`, `:934`). Persistent managed registration remains, so discovery repeats next tick.

[R0, inference] Two eligible open managed parents, with empty child and parent work, therefore produce two worker handoffs without an agent run and leave sessions idle/unflagged between ticks. Whether both workers remain in the set at the end-of-dispatch sample depends on timing; the reported `in_flight=2` is fully compatible with this path. Neither the supplied aggregate logs nor this source trace identifies which production sessions they are.

### Why the polling exists and what it costs

[R0] The source explicitly explains that child changes need independent discovery because they may not update the parent timestamp, and closed children must also be inspected (`internal/daemon/story_refinements.go:23`–`:25`). Current-generation story enumeration includes completed stories (`internal/store/managed_stories.go:106`–`:108`). After promotion, child requests still receive a visible lock refusal (`internal/daemon/story_refinements.go:129`–`:136`). This is deliberate discovery behavior, not evidence of an unbalanced admission.

[R0] Each polled child ordinarily incurs issue, parent-relationship and comment reads (`internal/daemon/story_refinements.go:65`–`:99`, `:157`–`:174`), and the persisted rotation is updated per child (`:88`).

[R0, inference] For an open managed parent with no new work, the ordinary path can entail approximately two parent requests (issue and comments), plus three API operations for each of up to 16 children. Actual request totals depend on child count, failures, pagination and client behavior. These estimates describe source-level operations, not measured production traffic. The worker also sets and clears backlog. Consequently “no agent run” does not mean “no resource cost”: repeated discovery consumes API access, database writes and worker capacity. Whether that cost is acceptable for this deployment remains unresolved.

### Metrics and logging

| Field | Verified meaning | Implication |
|---|---|---|
| `Issues` | Forge-discovered issues (`internal/daemon/tick.go:75`–`:78`, `:368`). Managed registration does not increment it. | Zero does not mean the complete candidate set is empty. |
| `Dispatched` | Worker handoffs, not run preparation or execution success (`internal/daemon/tick.go:79`–`:85`). | Poll-only work legitimately increments it. |
| `InFlight` | Outstanding workers sampled after dispatch (`internal/daemon/tick.go:86`–`:90`, `:218`). | A short-lived polling worker can appear in the count and leave immediately afterward. |

[R0] Candidates carry task/session-decorated loggers but do not retain a discovery-source field (`internal/daemon/tick.go:540`–`:550`). Ordinary successful admission lacks a reason-bearing dispatch log. Empty refinement and parent-drain paths lack an affirmative “no actionable work” outcome message. Existing duplicate refusal already logs identity (`internal/daemon/tick.go:832`), and GitHub comment reads log at debug (`internal/github/comments.go:101`); these can provide clues but do not give a single causal admission record. Discovery documentation omits managed registry enrollment (`internal/daemon/doc.go:19`–`:27`).

[R0, inference] The metric implementation matches its documented contract. The operational problem is that the displayed aggregate fields do not distinguish discovery effort from productive agent work. Renaming or adding fields requires an explicit decision about what operators should interpret as activity; merely moving the in-flight sample would not explain the two workers or remove their cost.

### Test evidence and gaps

[R0] Existing managed refinement tests cover child refinement, failed delivery and post-promotion refusal (`internal/daemon/story_refinements_test.go:45`, `:74`, `:97`). The fixture uses a local HTTP forge and real SQLite, replacing relationship lookup (`:16`–`:41`). No dedicated test was found that repeats empty-listing ticks for managed parents and asserts worker counts alongside zero agent runs. This is a scoped coverage finding, not a claim that no indirect coverage exists.

[R0] Backlog tests check clean drain/bootstrap clearing (`internal/daemon/backlog_test.go:28`, `:49`). Logging tests check identity on emitted messages rather than guaranteeing admission-reason or empty-worker outcome messages (`internal/daemon/logging_test.go:23`). Inflight tests cover exclusion, peak and concurrency (`internal/daemon/inflight_internal_test.go:13`, `:48`, `:79`). These fixtures support meaningful future behavior checks without inventing integration build tags.

[R0] The tour used its testing scanner. `.aiteam/rulebook.md` was absent, so its fallback was interpreted alongside project fixture conventions. Architecture and security scanners were skipped because no structural or authentication change was being researched. No web research was needed. Tests were inspected, not executed; no reproduction or passing test result is claimed. Future implementation remains subject to `make check` (formatting, vet, pinned lint and race tests; `Makefile:21`, `:26`, `:42`).

### Run 0003: recommended diagnostic design

[V] The current source continues to separate daemon worker admission from turn preparation. `Tick` launches workers asynchronously and samples them at dispatch completion (`internal/daemon/tick.go:218`, `:241`); the candidate carries identity but no source provenance (`:503`). The recommendation below is a design proposal grounded in this run's codebase tour, not implemented behavior or a story/task plan.

#### Admission: identity plus enrollment sources

**Recommendation:** emit a structured DEBUG `worker dispatched` record at each of the three successful launch sites. Include existing `repo`, canonical `task`, `session_id`, a bounded `dispatch_sources` field, and `schedule_id` or `continuation_id` when applicable. Log after all admission/reservation gates, immediately before launching the goroutine, so fast workers cannot log completion first. Only actual handoffs get this event; a discovered, busy, full, closed, or deferred candidate is not a successful dispatch.

[V] The sites matching `Dispatched` are ordinary admission (`internal/daemon/tick.go:827`, `:844`, `:852`, `:907`, `:924`), schedule admission after reservation (`internal/daemon/schedules.go:97`, `:110`, `:115`) and continuation admission after reservation (`internal/daemon/continuation_dispatch.go:62`, `:74`, `:75`). Ordinary identity already exists (`internal/daemon/tick.go:540`–`:550`), and schedules include their durable identity (`internal/daemon/schedules.go:64`). Continuations currently decorate only with `continuation_id` (`internal/daemon/continuation_dispatch.go:41`); normalize their repository/task/session fields as part of the same diagnostic contract. Their silent duplicate admission refusal (`:54`) should use the established DEBUG refusal convention already used for ordinary and scheduled work (`internal/daemon/tick.go:832`; `internal/daemon/schedules.go:70`).

**Recommended source vocabulary:** `forge_discovery`, `managed_registry`, `backlog`, `due_schedule`, `automatic_continuation`. These say why a worker was considered, not whether a new command or agent run exists. In particular, `managed_registry` means periodic discovery of managed-child changes; it must not be named as though a refinement is already pending.

[V] Forge issue and comment discovery already merge and filter duplicates before enrollment (`internal/daemon/tick.go:367`, `:610`–`:618`). Managed registration and backlog later discard seen sessions through a boolean set (`internal/daemon/tick.go:322`, `:375`, `:426`; `internal/daemon/story_refinements.go:37`). **Recommendation:** retain a reference to the enrolled candidate and union source flags in fixed order when subsequent sources encounter it. This preserves one dispatch per candidate while exposing all contributing categories. Keep the current combined forge category; promising separate exhaustive issue-list/comment causes would require earlier provenance changes unnecessary for this incident. Keep queue ordering and rotation unchanged (`internal/daemon/tick.go:390`).

[V] Repository/task/session identity is already tested for duplicate attributes (`internal/daemon/logging_test.go:23`, `:53`). Reuse decorated loggers rather than attaching the same keys twice. Ordinary workers also carry the dispatch tick time (`internal/daemon/tick.go:638`, `:663`). **Recommendation:** include that time for grouping recurring ordinary polls, without claiming it is a universally unique dispatch ID. A new worker-ID system is not needed to identify these two sessions and is outside the smallest scope. Existing run records correlate by task/session and their run/trigger identities; executor loggers are bound at startup, so daemon worker attributes do not automatically propagate into turn logs (`internal/daemon/executor.go:15`, `:28`; `internal/turn/execute.go:482`).

#### Completion: distinguish worker termination from run completion

**Recommendation:** add one structured DEBUG `worker finished` record at the actual end of each admitted worker, after its ownership and pool cleanup. Reuse the admission identity/source fields and record duration, terminal status (`completed`, `deferred`, `failed`, or shutdown cancellation), and a precisely named `execution_requests` count if the outcome needs to distinguish workers that never reach the executor. This count means calls requested by that worker, not prepared runs, successful runs, or process launches. Terminal logging must cover early returns and must not depend on reaching a per-comment completion message. Keep existing ERROR diagnostics for failures; a final DEBUG summary must not replace them or turn errors into successful emptiness.

[V] The existing `turn complete` log occurs inside per-trigger processing, before worker cleanup (`internal/daemon/tick.go:1762`; cleanup defers at `:928`, `:934`), and workers can handle several comments (`:1247`). Empty ordinary/refinement paths and their early returns are at `:1019`–`:1046`. Shutdown logging already uses DEBUG (`:1060`). A completion record intended to mean ownership is released must respect deferred execution order; adding it at an arbitrary inner return would repeat the original misleading finish boundary.

[V] `refineStories` returning true does not imply execution or empty work: locked-story replies also return true (`internal/daemon/story_refinements.go:129`–`:136`). Parent processing can consume ignored comments, save schedules, confirm proposals and post replies without running an agent (`internal/daemon/tick.go:1383`, `:1410`–`:1422`). Therefore **do not classify `execution_requests=0` as `no_actionable_work` or `idle`**. It accurately says the worker completed without requesting execution; it may still have performed useful control work. The existing `clearBacklog` logs its own failure but returns no status (`:1351`), so a generic worker completion cannot promise a successful durable backlog clear. A richer control-action or drain-success taxonomy would require explicit result plumbing and is not necessary to explain the recurring workers.

#### Activity display: keep worker measurements and reuse run evidence

[V] `Dispatched` counts worker handoffs and `InFlight` samples outstanding workers after dispatch, exactly as documented (`internal/daemon/tick.go:79`–`:90`, `:244`). There is no truthful synchronous count of all runs these workers will eventually prepare at tick completion: the workers outlive the tick (`:1009`). Moving the sample, subtracting fast workers, or redefining the count as productive work would conceal real capacity/API demand.

**Recommendation:** retain existing exported result fields and logged `dispatched`/`in_flight` values for compatibility. Add explicit INFO log aliases `workers_dispatched` and `workers_in_flight`, and an explicit `forge_issues` alias for `issues`, documenting that worker inflight is sampled at dispatch completion. Update operator-facing documentation to distinguish discovery workers from agent runs. A later removal of legacy log fields can be considered only after log consumers are known; there is no need to break them here. Do not add an `agent_runs=0` value to the tick summary merely because no run was prepared before its sample.

[V] Prepared runs already emit INFO `run prepared` with `run_id`, ordinal, branch and worktree (`internal/turn/prepare.go:415`). Execution logging supplies session/task/trigger and daemon `instance_id` (`internal/turn/execute.go:482`, `:515`). **Recommendation:** use this existing preparation event as the initial agent-work signal and the new DEBUG records to explain discovery work. State explicitly that it records preparation, not completion or guaranteed process launch. One prepared run can invoke the agent process twice after a resume fallback (`internal/turn/harvest.go:153`, `:191`); counting processes is a separate question. Existing executor result dispositions include runs, locks, prior-run/report replay, preparation failure and refusal (`internal/turn/result.go:21`–`:68`), so neither an executor call nor a nonzero returned run ID is a reliable new-run counter.

[V] Default production logging is INFO; DEBUG requires explicit configuration (`internal/logging/logging.go:21`, `:38`, `:97`). **Recommendation:** keep recurring per-worker diagnostics at DEBUG to avoid INFO noise, while keeping clear worker-summary aliases and prepared-run events at INFO. Operators would enable DEBUG through their normal supported operational configuration for a bounded observation window. This research does not change runtime settings or restart the daemon.

A representative proposed trace would show one dispatch and completion per managed session, with `dispatch_sources=[managed_registry]` (or multiple sources when applicable) and `execution_requests=0`, beside a tick summary showing two workers and zero forge issues. The absence of corresponding prepared-run events, subject to complete logs and the stated observation window, establishes that these were discovery/control workers rather than prepared agent runs. That identifies *which sessions* and *why they were admitted* without pretending to know that every poll was unnecessary.

#### Validation and scope boundaries

[V] Existing tests provide local HTTP forge, real test SQLite and fake executor fixtures (`internal/daemon/story_refinements_test.go:16`; `internal/daemon/fixture_test.go:272`), plus settle/tick helpers (`internal/daemon/fixture_test.go:1088`, `:1123`). No dedicated repeated empty-listing attribution test was found in the inspected files. The following are meaningful checks for a later implementation, not tests executed or newly written during research:

- Two open managed parents over repeated empty forge-listing ticks: one dispatch per parent, accurate registry attribution, no execution requests, no newly prepared run, completed cleanup/backlog clearing and readmission next tick. Use explicit worker completion synchronization; fast workers make exact unsynchronized inflight expectations race-dependent.
- One session enrolled by several sources: one worker, deterministic union of source categories, unchanged scheduling order, no duplicate log attributes. Busy/full/closed/skipped candidates do not emit successful-dispatch events.
- Existing behavioral preservation: actionable child changes still found without parent timestamp changes; closed/completed child polling, post-promotion replies and rotation continue to work.
- Worker outcome distinctions: empty polling, ignored comments/control replies, refinement deferral, errors, lock refusal, report replay and more than one parent comment must not all be labelled idle or newly executed. Validate new prepared-run evidence at the turn boundary, not solely by fake executor-call counts.
- Separate launch pathways: scheduled and continuation workers have complete identity and cleanup-aligned terminal records; refusal paths retain DEBUG severity; failures remain ERROR; diagnostics are absent at INFO while existing prepared-run events remain visible.
- Run the repository's required `make check` when production implementation occurs. Do not invent integration tags, use live dependencies, run `make build`, write `dist/`, or change daemon state for these checks.

[V] The codebase tour used the testing scanner; `.aiteam/rulebook.md` is absent. Architecture/security scanners were skipped because the proposed change uses existing owners and does not alter authentication or input processing; bounded identifiers/enums/counters avoid copying comment bodies or prompts into diagnostics. No external factual question required web research. No tests were executed.

## Domain / Problem Space

### Correction to run 0001

[O] The owner withdraws the original leak interpretation: the earlier `dispatched=1 in_flight=2` represented a long-running implement worker plus a freshly dispatched worker, and the completed #73 worker released. The newer repeat-dispatch series prompted the corrected investigation. The active research question is recurring polling activity, not repairing a demonstrated stale entry.

[R0] Run 0001 audited all five admission sites and found visible cleanup at every successful entry. It also established that the “turn complete” message precedes actual worker return and that successive equal counts need not have equal membership. Those earlier findings support the reframing; they are not presented here as a new full lifecycle audit.

[R0, inference] Aggregate dispatch counts alone do not mathematically rule out an unrelated leaked session elsewhere, since they do not identify membership. That logical limit is not a reason to retain the withdrawn incident diagnosis. There is no supplied evidence requiring a leak repair in this scope, and the current source provides a concrete recurring discovery mechanism compatible with all reported exclusions.

### Preserve ownership while improving interpretation

[R0] In-memory worker ownership spans more than the durable execution lock: reporting and consumption can continue after execution unlocks (`internal/daemon/inflight.go:17`–`:27`; `internal/turn/execute.go:541`, `:565`; `internal/daemon/tick.go:929`). Pool capacity uses a separate channel, not set cardinality (`internal/daemon/pool.go:96`). Therefore per-tick deletion of entries solely because `sessions.lock_owner` is empty would allow overlapping legitimate work; an orphan set entry alone would not establish that a pool token was also retained.

This prior safety conclusion remains relevant only as a boundary: do not carry the original lock-reconciliation proposal into a polling-observability change. No new evidence in run 0002 motivates reservation recovery.

## Constraints & Risks

1. **Runtime attribution is still conditional.** [R0, inference] Managed parents explain the pattern precisely, but production registration rows and parent identities were not supplied. Deployed revision equivalence is also unverified. Do not report the two sessions as identified or this deployment's cause as conclusively reproduced.
2. **Suppressing polling can suppress discovery.** [R0] Pending refinements are created by child polling; gating discovery on their prior existence is circular. Parent timestamps may not change, and closed/completed children and post-promotion requests have observable behavior that must remain supported (`internal/daemon/story_refinements.go:23`, `:129`).
3. **Polling has a real cost even when benign.** [R0, inference] API reads, rotation writes and temporary worker slots remain in use. Child counts and cadence matter when judging the operational impact.
4. **Changing counter meaning can hide useful information.** [R0, inference] Worker demand and concurrency remain useful independent of agent execution. Distinguish both concepts rather than making worker measurements silently represent prepared runs.
5. **Keep diagnostics causal and bounded.** [R0, inference] Source, session identity and worker outcome answer this incident more directly than another aggregate count. Multiple discovery sources can deduplicate into one candidate, so any reason vocabulary must specify whether it represents the first source or all contributing sources.
6. **Preserve established ownership and safety boundaries.** SQL stays in `store`, tick policy in `daemon`, individual execution in `turn`. Research did not modify runtime state, write to `dist/`, restart a daemon or change forge state. The existing single-connection SQLite and external-binary rules remain applicable.

## Open Questions

These are recorded for subsequent engineering work, not requests for a reply during this headless run.

1. **Production identity:** engineering needs a read-only observation of the managed registry joined to parent sessions/repositories, or a causal dispatch log, to confirm whether exactly two open managed parents account for these workers. Include feature state/readiness and child counts. The supplied exclusions omit this registry.
2. **Revision match:** engineering should compare the relevant discovery, refinement, dispatch and store enumeration paths at deployed `03d0481` with the audited worktree before declaring historical incident attribution complete.
3. **Cost and polling policy:** operators and product owners need to decide what discovery latency and cost are acceptable, especially for promoted features, completed children and registered parents with no current children. The source trace establishes repeated work, not that every instance is unnecessary.
4. **Log consumers:** engineering should identify consumers of the existing summary fields before any removal or rename. Run 0003 recommends additive worker aliases and existing prepared-run events, leaving the current meanings intact. Precise process-invocation metrics or exhaustive control-action classification are optional future scope, not prerequisites for this incident diagnostic.
5. **Behavioral preservation:** if later scope changes eligibility or cadence, engineering must show how unchanged-parent child updates, closed-child inspection, visible post-promotion refusal and persisted rotation continue to work.

## Recommended scope for Plan mode

- Center the work on managed-parent discovery and observable polling outcomes. First confirm the production parent identities and source-revision match; do not reopen a generic leak hunt without new evidence.
- Adopt the run 0003 diagnostic recommendation: successful DEBUG worker admission and cleanup-aligned completion with identity and unioned enrollment categories; retain useful refusal logging and fill the continuation identity/refusal gap. Keep absence of execution distinct from absence of useful control work.
- Clarify INFO summary fields with additive worker/forge aliases, retain legacy meanings, and reuse existing INFO run-preparation events. Do not pretend asynchronous tick completion can provide the eventual number of runs spawned by its workers. Preserve polling behavior and cadence in this initial scope.
- Use existing SQLite/HTTP fixtures to cover repeated empty forge listings with open managed parents, zero prepared runs, clean backlog/readmission and the documented count semantics. Any polling reduction also needs checks for child changes without parent timestamp changes and closed/post-promotion child handling.
- Consider eligibility or cadence changes only after measuring/confirming the relevant managed parents and deciding the discovery guarantee. This research does not prescribe a particular scheduler redesign or assert that polling is dispensable.
- Exclude lock-based inflight reconciliation, unrelated continuation rewrites and implementation in this run.

The earlier research established the managed-feature registry as a concrete explanation for recurring no-run workers, while exact production attribution remains open. Run 0003 keeps that correction and develops the observability recommendation within Research mode. The headless assumption is to explain the design without questions or confirmation, production changes, or edits to the issue title.
