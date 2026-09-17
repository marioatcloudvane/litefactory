# Drift Report — Implement run 0015

## Delivered as planned

- **US-001 (#309)** — additive migration `0047_submission_check_snapshot_scope.sql`
  recreates `submission_checks` with `PRIMARY KEY(run_id,snapshot_ref,reference)`
  following the repo's existing rebuild-in-place migration pattern (mirrors
  `0024_owner_exceptions.sql`); `RetainSubmissionCheck`/`GetSubmissionCheck`
  scoped accordingly; every internal/turn caller (submission_records.go,
  agent_result_context.go's shared-result reader, submissions.go's
  scopeForContribution) now threads the trusted `snapshot.Reference` through.
  Matches the architecture guidance and both AC-001/AC-002 exactly, including
  the explicit call-out to fix `agent_result_context.go`'s shared-result
  reader alongside the primary submission path.
- **US-002 (#310)** — new scoped queries (`ListPendingSubmissionsForAttempt`,
  `ListStoryProgressRunsForParent`, `GetStoryProgressRun`) alongside
  (not replacing) the existing global queries; new
  `RecoverSubmissionsForAttempt` and typed `*SubmissionRecoveryError`; every
  foreground call site (checkpoint, session Close, terminal Validate intake,
  Finish) now scoped; daemon-wide maintenance (`internal/daemon/executor.go`,
  `tick.go`) left byte-for-byte untouched, confirmed via empty `git diff` on
  that package. Matches guidance exactly.
- **US-003 (#311)** — `harvestStopped` now branches on `runResult == nil`
  (genuine startup failure) rather than any non-nil `runErr`, preserving the
  real process outcome and attaching the local factory failure (cleanup,
  finalization, or US-002's recovery error) as a separate, phase-labeled fact
  via the new `describeFactoryFailure` helper; interruption reporting
  untouched. Matches guidance exactly, including correct attribution of
  `*SubmissionRecoveryError` as "local submission recovery" rather than
  "did not start."

## Added scope (small, judged necessary, documented)

- A one-line guard in `internal/turn/plan_handoff.go`'s `publishPlanHandoff`
  (`result.Err != nil ||`) — not named in the story's known-file list, added
  by the US-003 engineer because `save()` already bailed on a pre-existing
  `result.Err` but `publishPlanHandoff` (which runs first) did not; without it,
  a Plan run whose subprocess succeeded but whose cleanup failed would still
  have been published, violating AC-005's "local factory failure still blocks
  workflow success and success-only save/publication." Verified as a no-op on
  every previously-passing path.

## Dropped or deferred

- Nothing dropped from the three frozen stories' acceptance criteria. Plan
  orphan receipt recovery remains explicitly out of scope per the plan
  (unchanged from the brief).

## Architectural deviations

None. No new packages, no new protocol/wire fields, no daemon scheduling
changes. `StoryProgress.Finish` kept its existing 3-argument signature
(deriving its own registered parent internally) rather than adding a `parent`
parameter, to avoid churn to an existing test call site — behaviorally
equivalent to the guidance's intent, a signature-preservation choice rather
than a deviation.

## Known gap (not drift, but flagged for Validate)

The literal single `make check` invocation could not be run to completion by
this orchestrator within its available per-command time budget, due to a
pre-existing, unrelated `internal/daemon` test
(`TestRetirementExecutionRealLifecycle`) that already hangs past a 9-minute
timeout in this environment (confirmed via empty `git diff` on that package —
zero changes from any of these three stories) and `internal/turn`'s very large
full-suite parallel test volume also exceeding a bounded gate independent of
`-race`. `make`'s fmt/vet/lint gates ran directly and passed; the test gate
was run in scoped, bounded batches instead of the one unbounded `./...`
invocation and every batch passed. See TEST_REPORT.md for full detail. This is
an environment/pre-existing-suite characteristic, not something introduced by
this implementation, and fixing the daemon test is out of scope for these
three frozen stories (which explicitly leave `internal/daemon` untouched).

## Verdict

**CLEAN** — no scope drift. The one open item is the aggregate `make check`
execution limitation above, which is an environmental/pre-existing-suite gap
rather than a divergence from the approved plan or stories. No follow-up
planning cycle is warranted for the three delivered stories; a separate,
unrelated ticket may be warranted for the pre-existing
`internal/daemon.TestRetirementExecutionRealLifecycle` timeout if it also
blocks other work's `make check` runs.


## Run 0017 — recovery scope record (not an acceptance review)

Verdict: AGGREGATE RECOVERY INCOMPLETE. Completed implementation and frozen stories were not changed. All 24 changed implementation/test files match the supplied retained implementation digest `872fb398c09f0fc8df7591ff5e4c5f71a0235079406bcc461b21f553d6219abf`. This is byte-identity verification, not a fresh code/architecture/acceptance assessment.

Build passed; make check passed format/vet/lint but timed out during full race tests at 480.181 seconds, recorded exit 124. No new success evidence was fabricated. There is no scope expansion or source edit in this recovery; Plan orphan recovery remains deferred. The unresolved item is executed aggregate completion, so existing engineering cannot yet be reported as a successfully saved feature.


## Run 0018 scope record

Aggregate-only recovery performed; no implementation or acceptance review and no changes to completed stories. No scope additions or architectural changes. Required aggregate proof remains incomplete because four package suites exceeded the bounded seven-minute deadline. This is an execution scope record, not a new acceptance/drift assessment of the implementation. See run0018-aggregate-report.md.
