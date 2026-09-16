# Research brief — #259, fresh reassessment

## Current assessment — 2026-09-16, run 0003

Request: “we did several things now, re-check here where we are! … research from scratch”. This is Research only: assess current behavior and remaining solution, including #258's communication work. No production implementation, publication, migration, daemon restart or workflow transition is authorized by this investigation.

Status: **Research complete; #259 remains unfixed at the inspected revision.** The communication architecture has materially advanced, but all four original storage/lifecycle defects remain. No material owner decision is needed to establish this finding. Prior run conclusions below are historical, not evidence for this assessment.

Revision: `aaa8b9ee60e29c9fb75a9f22b0b6a8c95c1b5143`; clean worktree at initial inspection. Evidence is current local source, freshly fetched GitHub issues/comments, and current offline tests. Historical runs 372/375 were not replayed and their daemon records were not inspected.

### Findings and expected behavior

| Area | Current behavior | Expected behavior / remaining work |
| --- | --- | --- |
| Check identity | Still keyed by `(run_id, reference)`; two stories using `build` collide even with different snapshots. Reproduced now. | A check reference resolves within its exact immutable snapshot; changed facts at that same identity still conflict. |
| Snapshot identity | Still keyed by `(run_id, reference)` with story/revision as immutable provenance. | Either make factory-issued snapshot handles unambiguous, or explicitly namespace local snapshot labels; do not leave a second hidden run-wide naming trap. |
| Failure isolation | Session Close, non-Plan intake and terminal Validate intake call global recovery. A foreign pending conflict can escape through the caller's failure. | Run/attempt-scoped foreground reconciliation, global daemon maintenance with per-receipt attribution. |
| Diagnostics | Harvest still treats any nonnil runner error as “coding agent did not start”, even if a RunResult exists. | Preserve process outcome/exit facts and report launch, submission, finalization or cleanup failures distinctly. |
| Orphaned Plan | Recovery/abandonment still skip receipts whose engineering/acceptance are not pending, including completed Plan input awaiting publication. | Stage-aware settlement after definitive failure/supersession; keep normal success/save publication authoritative. |
| Shared communication | Common result contract, update semantics, helper/MCP adapters and post-exit checks now exist. | Reuse these; fix the underlying checkpoint store and recovery rather than inventing another contract. |

### 1. Check collision is presently executable, not just historical

`internal/store/migrations/0028_submission_contracts.sql:48` declares the check table with `PRIMARY KEY(run_id,reference)`. `internal/store/submission_evidence.go:154` searches that pair and `:156` rejects a different snapshot; `:181` exposes the same incomplete identity to readers. `internal/turn/submission_records.go:41` still persists the agent's check ID without adding scope. `internal/agent/contribution_capture.go:57` checks uniqueness within the capture only.

A temporary Go overlay using the repository's real migrated SQLite and submission-session fixtures submitted US-1 and US-2 with distinct story snapshots and the same `build` check ID. Both preflights were valid. US-1 submitted successfully; US-2 returned `store: immutable check reference conflict`. Calling global recovery repeated the same conflict. This also establishes the mismatch between current preflight and storage validation.

The existing concurrent-story test (`internal/turn/submissions_test.go:459`) remains green because its fixture constructs `check-` plus the story key (`:112`). It does not cover the demonstrated input.

Retaining snapshot and checks uses separate transactions before progress commitment (`internal/turn/submission_records.go:26`, `:41`, `:88`). A late failure can therefore leave partial retained evidence and a pending receipt for recovery. A fix needs both early diagnostics and durable, correctly attributed failure/recovery behavior; preflight alone cannot prevent races.

### 2. Issue/story identity is already factory-bound, but local names are not unique

The prior owner question remains relevant. The real database run identifies the task/issue through its session. `BindSubmissionAttempt` verifies run/task/generation and copies authorized story key, numeric child issue ID and frozen revision (`internal/store/submission_receipts.go:18`, `:27`, `:48`, `:70`). Snapshot retention validates that authority and stores parent, generation, story and revision (`internal/store/submission_evidence.go:75`, `:86`). The printed task-local ordinal such as “run 0003” is not that database identity.

Thus **`(run_id, snapshot_ref, check_ref)` remains sufficient for checks while snapshots retain run-wide unique references**. Adding issue/story columns to this key would duplicate already bound provenance. Include those values in diagnostics/receipts for humans, but resolve and validate them from factory authority, not arbitrary agent metadata.

This does not solve two stories choosing `snapshot-1`: snapshot lookup still uses `(run_id, reference)` and checks story provenance only afterward (`submission_evidence.go:88`, `:110`). Factory-issued handles are the cleanest long-term option; alternatively scope snapshot identity by story and then update every dependent reference consistently. Do not change the check key assuming snapshot references became story-local when they have not.

### 3. Global recovery still contaminates the foreground caller

Current fatal chain:

1. `runSubmissionBound` defers `session.Close` into its named error (`internal/turn/submission_runner.go:53`).
2. `SubmissionSession.Close` calls global `RecoverSubmissions` before ending the attempt (`internal/turn/submissions.go:178`).
3. Recovery enumerates all pending receipts and joins their failures (`internal/turn/submission_recovery.go:17`, `:47`).
4. The actual process result and joined error return together through metering (`internal/turn/meter.go:36`, `:73`).
5. Harvest's generic error branch relabels the failure, sets signal exit metadata and bypasses normal harvest/save (`internal/turn/harvest.go:101`).

Ordinary checkpoint intake also performs global recovery before allocating its own receipt (`submissions.go:265`). Terminal Validate intake has the same dependency (`submission_runner.go:128`), including the new shared finalization projection path (`result_finalization.go:274`).

`StoryProgress.Finish` itself also polls globally (`internal/turn/story_progress.go:107`, `:114`), but its harvest caller only logs errors (`harvest.go:67`). Fixing Finish alone still would not repair the fatal chain. Foreground methods should only reconcile their authorized scope; daemon recovery should continue covering all outstanding receipts and should attribute failures by originating run/attempt/story/receipt.

The new finalizer correctly preserves failed/cancelled/interrupted process causes for its own output validation (`internal/turn/result_finalization.go:123`), but the outer deferred submission Close runs afterward and the old harvest branch still overrides it. The new runtime's own deferred cleanup can also return a nonnil error with an observed process result (`internal/turn/mcp_runtime.go:118`). Fix diagnostic classification at the shared harvest boundary, not only in one adapter. Filesystem edits are not shown to be deleted; the defect skips ordinary saving/publication.

### 4. Plan recovery remains incomplete

`submitPlan` assigns engineering/acceptance `not_applicable` and completed Plan state `pending` (`internal/turn/submissions.go:363`). Pending discovery includes `plan_state='pending'` (`internal/store/submission_receipts.go:268`), but both `RecoverSubmissions` (`internal/turn/submission_recovery.go:23`) and `AbandonSubmission` (`internal/store/submission_recovery.go:54`) require engineering AND acceptance pending.

Normal success is different: `FinalizeTypedPlanPublication` still advances Plan to published/delivered after guarded readiness/save (`internal/store/typed_plan.go:11`). Shared finalization intentionally hands Plan back to that existing owner (`internal/turn/result_finalization.go:298`). Therefore do not reject every Plan receipt when the broker closes: closure precedes legitimate publication. Distinguish normal closed-broker handoff from terminal failure, supersession and already-completed publication; recover/settle by stage and durable lifecycle facts.

### 5. What changed through #258

GitHub was read on 2026-09-16. [#258](https://github.com/marioatcloudvane/litefactory/issues/258) is closed as an umbrella and its [split comment](https://github.com/marioatcloudvane/litefactory/issues/258#issuecomment-5652382668) names #263, #264 and #265. #263 is closed; #264 remains open; #265 is closed as duplicate after further decomposition. Issue states alone are not implementation or validation evidence.

Current source now implements:

- Factory-owned `meta` with task, database run, attempt, mode, result/revision/request and version information; agent input contains `result` only (`internal/agentresult/types.go:26`).
- Typed Research/Plan/Implement/Validate/Spike payloads; strict shared decoding, semantic checks, explicit typed operations with expected revision, stable IDs and full merged validation (`internal/agentresult/`, `docs/agent-result-contract.md`).
- Explicit authorized baseline in launch context and distinct result-helper versus checkpoint descriptors (`internal/turn/result_finalization.go:24`, `:45`).
- One service for helper/MCP/terminal input and separate input/engineering/acceptance/publication observations (`internal/turn/agent_results.go:42`, `result_finalization.go:87`).
- Post-exit completeness enforcement, current-attempt binding, conflict detection and preservation of failed/interrupted process causes within that finalizer (`result_finalization.go:123`, `:174`).
- Runtime setup and native host wiring, with shared completion retained when native MCP is disabled; explicit compatibility routing where managed checkpoint authority is absent (`internal/daemon/setup.go:260`, `internal/turn/mcp_runtime.go:92`, `:106`).
- Separate aggregate capture and scoped story checkpoint evidence. Capture/receipt acknowledgment alone cannot complete Implement.

**This is now an implementation-integration problem, not a request to design a JSON envelope from scratch.** Keep the shared contract. Make exact evidence identity and scoped error ownership hold all the way through its checkpoint backend. The new shared evidence resolver adds a reader that must be included in a key migration: `internal/turn/agent_result_context.go:84`, alongside `submissions.go:334` and `submission_records.go:55`.

Aggregate checks intentionally have their own retained identity and forbid relabeling scoped execution. `internal/store/result_aggregates.go:239` currently compares aggregate reference/execution IDs against scoped check references within the parent. Review this check during identity changes: relaxing local check-name uniqueness must not grant permission to relabel scoped execution as aggregate proof, nor accidentally make ordinary label reuse a substitute for execution identity.

Adoption and reader retirement remain distinct. Current docs explicitly retain supported legacy compatibility and state live-drain prerequisites (`docs/agent-result-contract.md:350`). Latest [#264 report](https://github.com/marioatcloudvane/litefactory/issues/264#issuecomment-5701855181) says scripts for #230/#259 were written, not executed; migration/retirement remains outstanding. This research did not execute those scripts or inspect another worktree.

[#274](https://github.com/marioatcloudvane/litefactory/issues/274) and [#296](https://github.com/marioatcloudvane/litefactory/issues/296) now appear closed. Their latest substantive [parent report](https://github.com/marioatcloudvane/litefactory/issues/274#issuecomment-5701701610), child comments and `docs/agent-result-contract.md:398` still report the four authentic host captures outstanding. Do not infer authentic verification from closure or this research run. No new host certification is claimed.

### Recommended bounded fix and regression cases

1. Change the retained check primary key and accessor contract using an additive migration, preserve old evidence/foreign keys, and pass the exact snapshot to every lookup including shared result context. Define snapshot-label scope explicitly; preserve same-identity immutability and attempt provenance.
2. Add scoped foreground recovery for submit/Validate/Close; retain global daemon recovery with originating identities in each diagnostic. Test two concurrent parent issues: one poison receipt cannot block the other run's submit, final output, save or Plan publication.
3. Preserve actual RunResult and process exit facts through cleanup errors. Represent subsystem errors separately and truthfully; do not turn an invalid own submission into success merely to avoid the misleading launch message.
4. Make Plan retirement/recovery stage-aware without abandoning normal post-close publication. Test terminal failure, successful save/publication, restart, replacement attempt and stale generation.
5. Extend current shared-contract tests end to end: generic `build` under two story snapshots; same-snapshot changed check remains rejected; duplicate snapshot labels; second-snapshot rework; request replay preserves receipt; corrected content uses new request; shared result resolver retrieves exact check; aggregate proof cannot be fabricated by renaming scoped proof.

Fixing #259 need not wait for legacy-reader retirement or external host captures. Retain existing authority boundaries and verify migration/recovery with current shared completion active. This is an advisory next step, not a mode selection or started Plan.

### Verification and limitations

Environment: macOS/darwin arm64, `go version go1.27.0 darwin/arm64`, workspace revision above, temporary SQLite/socket/worktree fixtures, no live coding-agent process or remote writes.

- PASS: `go test ./internal/store ./internal/turn ./internal/agentresult -run 'Test(Submission|ValidationCheckpoint|AggregateLifecycle|ResultPlanContribution|ResultDiagnosticError|AgentResult|Shared)' -count=1`; store 2.179s, turn 58.922s. The agentresult package had no matching tests in this command (not treated as coverage).
- PASS: `go test ./internal/agentresult -count=1`; 0.308s.
- PASS as a **bug-observation probe**, not a fix regression: `go test -overlay=/tmp/lf259-research-probe/overlay.json ./internal/turn -run '^TestResearch259DuplicateCheckProbe$' -count=1 -v -timeout=90s`; 1.405s. Output: second story preflight VALID, submit immutable check reference conflict; global recovery repeats it. The probe's appended test is retained as `DUPLICATE_CHECK_PROBE.go.txt` in this session. It can be appended to a temporary copy of `internal/turn/submissions_test.go` and mapped with Go's Replace overlay, leaving repository source unchanged.
- Selected core source files measured separately: 157,099 bytes across the 13 core store/turn files listed in the evidence; this is a bounded subset, not total bytes read or billed tokens. Billed input tokens are unavailable; no token-savings claim.
- No full `make check` or live daemon historical reproduction was run: no production code changed. No direct execution test of cross-issue harvest or Plan orphan retirement was added; those findings follow current source/caller tracing, while the check collision and global recovery poison were directly observed.

## Research history

- 2026-09-13 runs 0001/0002: prior investigation and initial #258 design comparison, preserved below.
- 2026-09-16 run 0003: fresh current-source/GitHub investigation, current offline tests and executable duplicate-ID probe. Research complete. No production code, runtime state, issue state or workflow mode changed.

---

## Historical brief from runs 0001/0002 (superseded by current assessment above)

# Research brief — duplicate checks and cross-run failure (#259)

Status: complete, Research only, including run 0002 follow-up. Date: 2026-09-13.
Initial source revision: `60187208d9a9bcadcca8f01edb39ec11cac23c3e`. Follow-up source revision: `5c4bf0b9ce3b32ea66f633280556d57420e42893`.

## Request and scope

Investigate the reported submission collision, unrelated-run failure, misleading error and stranded Plan receipt; explain causes and recommend a bounded fix. Expected behavior: check names can be reused across distinct story snapshots; a failed submission affects its own authority scope; actual runner completion is preserved in diagnostics; pending receipts eventually reach an honest terminal state. The issue supplies actual reported behavior and sufficient reproduction conditions. No material owner decision blocks this investigation. No implementation, migration, daemon-state changes, commit or publication was performed.

## Findings and evidence

Repository citations in the original investigation refer to the initial revision; the run 0002 section cites the follow-up revision. Historical run IDs, timing, receipt 57 and outcomes in the issue are owner-supplied reports, not independently inspected production telemetry.

### 1. The collision is real; validation and storage disagree about identity

`internal/agent/contribution_capture.go:57` checks duplicate IDs within one capture. Its check facts include story, revision and snapshot (`:63`). But `internal/store/migrations/0028_submission_contracts.sql:48` defines the check primary key as `(run_id, reference)`. `internal/turn/submission_records.go:41` passes the agent's check ID directly as the reference; `internal/store/submission_evidence.go:154` retrieves any existing check by run and reference and rejects a different snapshot at `:156`.

Consequently, two authorized stories in one run, each with a different retained snapshot and a check ID `build`, pass capture-local ID validation but conflict at durable retention. Distinct snapshots of the same story can hit the same defect. The generated instructions show generic `check-1` and call the snapshot name local (`internal/turn/submission_runner.go:77`); they do not establish run-wide check uniqueness.

Retention is incremental: snapshot and checks commit separately, before `CommitSubmissionProgress` (`internal/turn/submission_records.go:26`, `:41`, `:87`). A collision leaves the receipt allocated and pending because the handler returns the error (`internal/turn/submissions.go:260`, `:277`). It does not immediately issue a terminal rejected receipt. Recovery replays that pending request while authority/current files remain valid (`internal/turn/submission_recovery.go:94`). Once authority expires, recovery can abandon it. This supports repeated errors followed by eventual rejection without confirming the reported timestamps.

### 2. Cross-run contamination is confirmed, but the fatal path differs from the issue's explanation

`StoryProgress.Finish` is global: it calls `poll`, discovers every active run and every parent with effect debt, and passes the supplied run ID only as an end marker (`internal/turn/story_progress.go:107`, `:113`, `:197`; `internal/store/story_progress.go:85`). However, its harvest caller only logs errors (`internal/turn/harvest.go:67`). Fixing Finish alone would leave the reported fatal mechanism intact.

The direct fatal chain is:

1. A runner actually completes at `internal/turn/submission_runner.go:81`.
2. Deferred cleanup joins `session.Close` into the returned runner error (`:53`).
3. Close calls global `RecoverSubmissions` before ending this attempt (`internal/turn/submissions.go:178`).
4. Recovery scans all pending receipts and returns joined errors, including immutable conflicts from another active run (`internal/turn/submission_recovery.go:17`, `:42`, `:47`).
5. `runMetered` returns that error with the observed runner result (`internal/turn/meter.go:36`, `:72`), and harvest takes its generic error branch (`internal/turn/harvest.go:101`).

There is a second entry point: ordinary non-Plan submit calls global recovery before allocating its own receipt (`internal/turn/submissions.go:249`). Another run's broken receipt can therefore block new submissions as well as final cleanup. Daemon ticks intentionally run global recovery independently of workers (`internal/daemon/executor.go:37`). Per-receipt recovery continues after errors, so global maintenance should remain, with failures attributed to their own run/attempt/receipt.

### 3. The error branch loses truthful completion information

`internal/turn/harvest.go:102` assumes any non-nil `runErr` means no runner result exists. The deferred Close path disproves that assumption. The branch labels the failure “coding agent did not start,” overwrites exit metadata with a signal exit code, and passes nil to finish (`:106–111`), bypassing ordinary successful harvesting. This supports loss of the save/publication path, not proof that filesystem edits were deleted. The metering wrapper also marks the attempt failed when a successful result accompanies a cleanup error (`internal/turn/meter.go:40`).

### 4. Orphaned Plan receipts are stuck; ordinary Plan publication works

A valid completed Plan submission deliberately sets engineering/acceptance to `not_applicable` and plan to `pending` (`internal/turn/submissions.go:339`). Both recovery and abandonment skip receipts unless engineering AND acceptance are pending (`internal/turn/submission_recovery.go:23`; `internal/store/submission_recovery.go:54`). The pending-list query continues selecting plan-pending receipts (`internal/store/submission_receipts.go:268`). Thus an aborted completed Plan can remain in recovery discovery forever.

The broader claim that a Plan receipt can never advance is incorrect. Normal successful harvest invokes `FinalizeTypedPlanPublication` (`internal/turn/plan_handoff.go:249`), which writes `published`/`delivered` only under readiness, generation, stage and attempt guards (`internal/store/typed_plan.go:11`). Broker closure occurs before this legitimate finalization. Treating every closed Plan broker as abandoned would break the normal path.

## Options and recommendation

Recommended coordinated fix:

- Add a new store migration rebuilding the check table with primary key `(run_id, snapshot_ref, reference)`, copying original rows and preserving its snapshot foreign key. Update immutable lookup, insertion and `GetSubmissionCheck` to require snapshot context. Update both production readers (`internal/turn/submission_records.go:55`, `internal/turn/submissions.go:310`). Retain conflict detection for changed facts under the same full identity and retain byte-budget/replay semantics. A schema-only change leaves ambiguous reads and is insufficient. Snapshot references themselves remain run-wide, so document that separate invariant explicitly.
- Split scoped run/attempt recovery used by submission intake and Close from global recovery used by daemon ticks. Make Finish discover only its relevant parent/run lifecycle; retain parent locking and ordered-ingestion barriers before remote effects. Do not simply filter discovery lists while the inner parent reload still processes unrelated authority. Keep global maintenance and recoverable effect debt, but attach parent/run/attempt/receipt context to each fault. Preserve terminal rejection for deterministic invalid evidence separately from retryable infrastructure errors so one immutable conflict does not retry forever or wedge later sequence numbers.
- Separate launch errors from post-execution submission/finalization errors. Preserve the observed result, output, exit code and usage even when its own bookkeeping cannot complete. Label the phase accurately, for example “submission finalization failed for run …, receipt …”. Maintain the success-only save rule for genuinely incomplete own-run evidence; do not mask all errors merely because the subprocess exited successfully.
- Make receipt recovery/abandonment stage-aware. Allow normal post-close Plan harvest first. For failed, superseded or orphaned Plan attempts with no confirmed publication, terminally reject/abandon plan state without granting readiness or engineering state and preserve payload/diagnostics. If publication may already have occurred, reconcile durable publication evidence before claiming completion or abandonment. Automatic replay of an old proposal is a larger feature, not necessary for an honest terminal recovery state.

Alternative: require every agent to prefix IDs and preflight run-wide uniqueness. This may improve early diagnostics but leaves correctness dependent on naming discipline and does not address isolation; it is not the recommended structural fix. Automatically swallowing cleanup errors is also insufficient because current-run evidence failures must stay visible.

## Regression coverage to add during implementation

1. Two stories, one run, distinct snapshots, both IDs `build`: both record; exact snapshot reads return correct original facts. Also test distinct snapshots of one story, idempotent identical replay, same-identity mutation refusal, failed-check preservation, migration of existing records, foreign keys and unchanged byte accounting.
2. Active run A has an immutable-conflict pending receipt; run B submits and closes successfully and retains its commit/Plan publication. Include a successful fake runner plus cleanup failure to verify truthful result/exit/output preservation. Exercise Close and submit separately from Finish.
3. Global ticks continue processing healthy independent receipts while reporting A's run/receipt identity; current-run deterministic failures settle without advancing missing ordered evidence or allowing stale remote effects. Preserve restart recovery and parent locks.
4. Plan normal success still becomes published after broker Close; failed/superseded/restarted pending Plans leave the pending queue without readiness, while confirmed publication is not overwritten. Cover allocation-before-decision and post-save/finalization failure windows.

Existing tests explain why this escaped: `TestSubmissionConcurrentStoriesKeepCorrelation` uses `capturedContribution`, which prefixes every check with its story (`internal/turn/submissions_test.go:111`, `:459`). The immutable evidence test covers one snapshot (`internal/store/submission_test.go:302`); final-drain recovery covers one healthy run (`internal/turn/submissions_test.go:506`). The normal typed Plan publication test already expects successful receipt finalization (`internal/turn/typed_plan_lifecycle_test.go:203`).

## Verification and limits

Environment: Darwin arm64, `go version go1.27.0 darwin/arm64`, worktree revision above. Executed existing focused tests:

```text
go test ./internal/store ./internal/agent ./internal/turn -run 'TestSubmission|TestContributionCapture|TestTypedPlanRunnerPublicationAndDurableReceipt' -count=1
PASS internal/store (2.015s), internal/agent (1.272s), internal/turn (15.490s)
```

These passing tests establish the current baseline; they do not dynamically reproduce the missing collision/cross-run cases or certify a fix. No new code/tests were written; full `make check` is for implementation completion. No live daemon/database/provider was inspected or invoked, so historical run details remain unverified. No sandbox denial affected the investigation. A focused measured corpus of 19 source/test files occupied 229,141 bytes on disk (files were partly read); this is a source-size measure, not billed input tokens or exact bytes transmitted. Billed token counts are unavailable.

Relevant boundaries: SQL and migrations remain in store; retain its one-connection pool, immutable evidence and parent lock/ordered-effect constraints (SEC-21, CONC-8, CONC-10, CONC-22). No permission relaxation, runtime edits or daemon rebuild/restart is needed for the proposed code work.

## Decisions and history

- Settled from supplied context: investigate all four linked concerns and recommend solutions; remain in Research.
- No material questions for the owner. Historical database replay is optional corroboration, not a prerequisite for the source-level conclusion.
- 2026-09-13: Completed source tracing, corrected the fatal attribution and Plan overstatement, ran existing focused tests, and saved this handoff. Suggested next step: Plan a coordinated store identity, recovery isolation, diagnostics and receipt-lifecycle change; Plan was not started.

## Run 0002 — story/issue identity and the communication contract in #258

Request: establish whether the proposed check key also needs a user-story reference and issue ID; inspect parallel #258 and evaluate its actual contract proposal against this bug. This is a bounded research extension, not approval to implement or begin Plan. No owner clarification is needed to answer it.

### Identity: necessary context, but not extra columns in this check key

The story and issue are relevant and already enforced through the ownership chain:

| Identity | Current source and scope | Recommended contract treatment |
|---|---|---|
| Run | `Run.ID` is the database surrogate; `Run.Ordinal` is only the number within a task (`internal/store/runs.go:100`, `:106`). | Use the actual factory-bound run ID. The prompt's `run 0002` is not a globally unique ID. A portable envelope also carries the canonical task reference; cross-installation exchange would need a factory namespace. |
| Parent issue/task | Run → session → canonical `github:owner/repo#number` (`internal/store/runs.go:102`; `internal/store/sessions.go:26`, `:35`, `:64`). Attempt admission verifies this relationship (`internal/store/submission_receipts.go:48`). | Factory-supplied task reference distinguishes repository and issue. Bare `#259` is insufficient outside repository context. |
| Story and its issue | Attempt scope stores story key, forge issue ID and frozen revision; binding validates the mapping against managed stories (`internal/store/submission_receipts.go:13`, `:67`, `:87`, `:189`). | Story is the agent's authorized selector. Factory resolves its issue ID, generation and revision. Distinguish parent task issue from the story's child issue; `US-001` is not globally unique. Plan/Research reports may have no story, and newly proposed Plan stories may not yet have child issue IDs. |
| Snapshot | `(run_id, reference)` uniquely binds parent, generation, story, revision and immutable bytes (`internal/store/migrations/0028_submission_contracts.sql:43`; `internal/store/submission_evidence.go:77`, `:91`). | Reference an exact retained snapshot. The same reference in the same run cannot describe another story or revision. |
| Check | Store resolves snapshot → story/revision, then checks active attempt authority (`internal/store/submission_evidence.go:139`, `:143`). | `(run_id, snapshot_ref, check_ref)` is sufficient under the current snapshot invariant. Check IDs are local to that snapshot, so each story can use `build`. |

Adding story and issue to the check primary key would duplicate facts already determined by the snapshot. It would not repair cross-run recovery, and inconsistent redundant values would need extra validation. Replacing the snapshot with story/issue would lose the distinction between successive snapshots of the same story. Preserve these fields in canonical correlation, receipts and diagnostics; enforce them through trusted binding and foreign-key relationships.

There is a separate naming hazard: two stories using the same snapshot name still conflict because snapshot references themselves are run-wide. The bounded check-key fix does not change that. For #258, explicitly define snapshot identity as well: prefer a factory-issued immutable handle (with an optional agent label), or deliberately introduce story-scoped snapshot references and migrate every dependent lookup/key. Merely adding story to the check key while snapshots remain run-wide does not solve the snapshot collision. Existing evidence records should remain immutable; changing execution under the same snapshot/check identity needs a new check reference, and changed snapshot bytes need a new snapshot.

### What #258 actually proposes

Read #258's issue body and all six comments through the GitHub connector on 2026-09-13. Its original request is a communication-path inventory, not an implemented replacement. The latest owner comment proposes JSON with sibling `meta` and `result` objects, text for the issue comment plus mode-specific fields, editing only changed fields of a prior result, token efficiency, and loading the contract skill only when a run finishes. Source: [owner's contract proposal](https://github.com/marioatcloudvane/litefactory/issues/258#issuecomment-5652059930).

The preceding research recommends common validation/correlation/retry semantics, separate received/accepted/published states, and an optional thin MCP adapter over the existing submission service. These are recommendations in the discussion, not settled owner decisions or implemented capabilities. Source: [#258 prior research](https://github.com/marioatcloudvane/litefactory/issues/258#issuecomment-5648178928). No other session directory or linked archived research artifact was opened.

JSON is a good fit for this proposal because current contributions and receipts already use it. Use a valid object with `meta` and `result` as siblings; use a versioned, mode/message-specific schema rather than one permissive bag of optional fields. Schema shape alone is insufficient: namespace, authority, retry, failure ownership and lifecycle rules must be part of the contract.

The key authority constraint already exists: agent contributions intentionally forbid factory-owned run, issue, generation, revision, attempt and receipt fields (`internal/agent/contribution.go:22`; `internal/agent/contribution_schema.go:234`, `:285`). `internal/turn/submissions.go:229` resolves the story selector through the authorized map. Therefore, an outer `meta` envelope should be assembled by the factory/adapter or verified against its binding if echoed by an agent. It must not let agent-authored IDs select another run or manufacture scope. Do not simply add these fields to current contribution JSON and weaken its validation. Current parsers will reject the new envelope; adopting it requires an explicitly versioned adapter/migration.

An illustrative factory-assembled message could have `meta` containing contract version, message kind, canonical task, actual run ID, attempt, mode and request ID; `result` could carry report text and stage-specific contributions with story selector, snapshot reference and local check IDs. Canonical receipts/logs should enrich each story contribution with its resolved child issue ID, generation and revision. Identity shared by a batch need only appear once; each story-specific entry retains its own scope. This is a recommendation, not an agreed schema.

### How that contract addresses this incident

| #259 defect | Contract rule needed | Code change still required |
|---|---|---|
| `build` collides across stories | Document check identity as snapshot-local; reject changed facts under the same full identity with a typed diagnostic identifying the affected story/snapshot/check. Align preflight and durable validation. | New check-table migration, exact-context reads, retention and replay changes. An envelope alone cannot alter the SQL key. |
| Run A's receipt fails run B | Every receipt and failure has factory-bound task/run/attempt/request scope. A submission operation affects only its scope; global recovery emits independently attributed outcomes. | Scope submit/Close recovery, preserve daemon-wide maintenance and ordered/locked parent effects. Merely attaching IDs to an error still leaves propagation broken. |
| Successful execution reported as not started | Separate process execution outcome from submission/finalization/publication outcome. Use phase, stable error code, retryability and affected correlation; preserve actual exit/output/usage. | Correct error propagation, metering/harvest classification and result preservation. A human comment can explain both outcomes truthfully. |
| Plan receipt remains pending | Define valid state transitions by stage, including `not_applicable`, terminal rejection and uncertain publication recovery. Acknowledged/received is distinct from engineered, accepted and published. | Stage-aware orphan handling with publication guards. Do not abandon a normal Plan at broker Close before harvest publishes it. |

Deterministic identity conflicts should produce terminal rejection for that request with actionable diagnostics; retryable infrastructure errors keep recoverable state. Retrying the same request must return the original result, not create new evidence. Current allocation already binds request identity to payload, attempt, story and revision (`internal/store/submission_receipts.go:131`, `:143`), and the helper journals request IDs (`internal/submission/client.go:50`, `:111`). Preserve that distinction: a request ID identifies delivery/idempotency, while a check ID identifies evidence. A corrected payload is a new request; an unchanged replay retains its request ID.

### Partial changes, token efficiency and finish-time loading

For the owner's Plan-edit example, recommend a new typed change request with a base proposal revision and explicit changes addressed by stable story/criterion IDs. The factory verifies the expected base, applies only authorized changes to its canonical document, validates the complete resulting proposal, then creates a new immutable proposal revision and receipt. Define omission as unchanged and deletion explicitly. Reject stale-base changes instead of overwriting a newer plan; unchanged replay returns the prior receipt. Published/approved story changes must still pass the existing generation/readiness/approval workflow. Existing full contribution submissions are not already a patch API, and immutable check/receipt history must never be edited in place. A patch is a new operation, not a content-changing retry of an old request.

For tokens, keep common metadata factory-owned, omit inactive fields, reference retained snapshots/receipts rather than returning their complete content again, and load only the relevant mode schema and changed Plan item plus necessary dependencies. Keep descriptive field names unless measurement shows shortening helps; token savings from a schema or late loading were not measured here. “Only changed fields” reduces output but does not remove the need for sufficient current context or full validation after applying the patch.

Loading the full contract only at the final report fits final-only serialization, but is too late for incremental Implement submissions, early preflight, questions/status during a run, or deciding what execution evidence to retain. Current instructions explicitly request preflight and submit, generic local check names and request retention during execution (`internal/turn/submission_runner.go:73`, `:77`), while Validate uses final JSON under its read-only profile (`:79`). Recommend a small initial contract pointer plus essential obligations and identity scope; load the appropriate schema on first use (which may be at finish for Research or Validate). Load final-report formatting at finish. Do not replace each story's immediate receipt with one end-of-run submission, because that would lose existing incremental acknowledgment and early failure visibility.

A lazy skill is an instruction-discovery mechanism, not server-side enforcement. It also does not guarantee previously loaded data is removed from model context. Enforce the contract in the common receiver and store independently of whether an agent loaded the skill. No MCP adoption is required for these fixes; if #258 later chooses it, use the same scoped service/validation/receipts through the adapter. Transport selection remains separate from the proposed data contract.

### Recommendation, verification and history

Use #259 to implement the narrow identity, recovery isolation, truthful diagnostics and Plan lifecycle repairs once moved to Plan/Implement. Coordinate #258 around the same identity namespaces, authority model, stage-aware receipts and error taxonomy; make #259's regression scenarios conformance cases for every future adapter. A broad communication overhaul is not a prerequisite to fixing the current defects. Resolve factory-issued versus story-scoped snapshot handles and full-result versus typed-change operations when designing #258, without silently expanding #259's storage migration.

Additional regression cases for that future contract: spoofed run/story/issue metadata is rejected; same story label across different issues cannot redirect evidence; stale attempts and proposal bases fail; same request replay is idempotent while changed payload under that ID conflicts; omission/deletion semantics preserve untouched fields; final-only and incremental adapters yield identical canonical authority/receipt outcomes. Retain the original #259 cases for collisions and cross-run failure attribution.

Follow-up verification: read focused source at revision `5c4bf0b9ce3b32ea66f633280556d57420e42893`, compared changed paths with the initial revision, and fetched current #258 body/comments. Relevant submission identity/runner/receipt sources retain the traced behavior; other code, including metering, has changed since the original test baseline. No tests were rerun and the original baseline is not claimed as verification of this newer revision. A measured ten-file source corpus is 131,574 bytes on disk, partly read; this is not input-token usage or exact transmitted bytes. Billed token counts are unavailable. Some initial guessed filenames did not exist; their actual owners were located and read. No network/permission blocker occurred; no production telemetry was accessed.

- 2026-09-13, run 0002: Answered that story/issue identity is required for trusted correlation and already bound, but redundant in the proposed check key under current snapshot uniqueness. Evaluated the latest #258 owner proposal, its authority/versioning implications, partial changes and on-demand loading; saved this extension. Research complete, no material unanswered owner question. No source code or runtime state changed, no commit/push, and Plan was not started.
