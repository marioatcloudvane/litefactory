# Research brief — duplicate checks and cross-run failure (#259)

Status: complete, Research only. Date: 2026-09-13.
Source revision: `60187208d9a9bcadcca8f01edb39ec11cac23c3e`.

## Request and scope

Investigate the reported submission collision, unrelated-run failure, misleading error and stranded Plan receipt; explain causes and recommend a bounded fix. Expected behavior: check names can be reused across distinct story snapshots; a failed submission affects its own authority scope; actual runner completion is preserved in diagnostics; pending receipts eventually reach an honest terminal state. The issue supplies actual reported behavior and sufficient reproduction conditions. No material owner decision blocks this investigation. No implementation, migration, daemon-state changes, commit or publication was performed.

## Findings and evidence

All repository citations below refer to the revision above. Historical run IDs, timing, receipt 57 and outcomes in the issue are owner-supplied reports, not independently inspected production telemetry.

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
