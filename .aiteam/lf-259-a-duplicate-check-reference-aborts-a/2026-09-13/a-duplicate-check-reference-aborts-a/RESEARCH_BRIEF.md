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
