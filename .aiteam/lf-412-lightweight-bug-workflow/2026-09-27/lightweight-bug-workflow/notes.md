# Implement notes — run 0020 (2026-09-28)

## Environment
- `TEST_BASE_URL` and `TEST_DATABASE_URL` are both unset in this run. None of the
  four stories' integration-test requirements need an external HTTP target or an
  external database: the "real dependency" for this feature is the repository's
  own embedded sqlite migration/store engine, exercised via the existing
  `*_integration_test.go` convention in `internal/store` (no build tag, plain
  `go test`, real local sqlite via `store.Open`+`Migrate`). Phase 2 environment
  provisioning was therefore skipped as not applicable, not because the
  variables were missing.
- One integration scenario (story-003's full self-check cycle run inside the
  real sandbox, `internal/bugchecks/self_check_sandbox_integration_test.go`) is
  skipped because `LF_TEST_CODEX` (a real Codex CLI) is unset in this
  environment — the same pre-existing skip condition already used by this
  package's other sandbox integration tests. This is a genuine environment
  limitation, not a missing-URL gap; no substitute target was invented.

## Full frozen story bodies
- The four approved GitHub issue bodies (#425–#428) were fetched via `gh issue
  view --json body` and saved to `/tmp/story425.md`..`/tmp/story428.md` for
  engineers to read directly (avoiding double-reading ~200KB of story text
  through the orchestrator). These are the authoritative AC-001..AC-0NN texts
  used by every engineer dispatched in this run.

## Sequencing decision
- story-001 was implemented first and alone (prerequisite data layer).
- story-002, 003, 004 were then implemented **sequentially, not in parallel**,
  because their production-code tasks overlap substantially at the file level
  (bug-fixer.md in both `internal/teamsource/bug/` and `teams/source/bug/`,
  `internal/turn/bug_pr.go`, `internal/report/bug_delivery.go`,
  `BugDeliveryReviewDigest`, the bug_events/bug_checks accessors). Serializing
  avoided concurrent-writer conflicts at the cost of wall-clock time.
- Test-writing (Phase 3) was split into three parallel dispatches instead of
  one, purely to manage per-agent context budget on an unusually large surface
  area: internal/store (all four stories' store-layer tests), everything else
  behavioral/wiring (internal/turn, internal/report, internal/agent,
  internal/bugrepair, internal/buganalysis, internal/submission,
  internal/teamsource), and integration tests. File ownership was partitioned
  up front to avoid collisions; verified no naming collisions after the fact.

## Genuine production bugs found and fixed during test-writing (not new scope —
## regressions introduced by the schema consolidation itself)
1. `internal/store/bug_analyses.go`: `bugRetireCurrentAnalysisReviewTx` hit
   `bugEventTx`'s idempotent-append conflict guard on the very first ordinary
   retirement of a confirmed analysis review, because `RetainBugAnalysisReview`
   wrote the review's `bug_events` row with `CommentID:0` and
   `ConfirmBugAnalysisReview` never synced it. Fixed by syncing the event's
   content at confirmation time so retirement re-derives byte-identical
   content (a true no-op), rather than weakening the general replay guard.
2. `internal/store/bug_analyses.go`: `ApplyBugAnalysis` asserted
   `revision == pass.Ordinal`, conflating two unrelated counters (pass-budget
   reservation count, which includes failed passes, vs. successfully-applied
   revision count) — broke any bug lifetime with an earlier failed analysis
   pass followed by a later success (exactly the intended use of the 8-pass
   budget). Fixed by removing the erroneous assertion; the actual
   optimistic-concurrency protection (`ExpectedRevision`/`state.Revision` and
   `proposal.BaseRevision`/`state.CurrentAnalysis`) was already present and
   correct elsewhere in the function.
3. `internal/store/bug_delivery.go`: `EnsureBugMerge`'s "no unresolved prior
   merge" guard treated the column default `merge_outcome='pending'` (present
   on every brand-new row, before any merge was ever attempted) as an
   unresolved prior merge — the very first merge attempt for any session
   always refused. Fixed by additionally requiring `merge_operation IS NOT
   NULL AND merge_operation!=''` in the guard (that column is only set once a
   merge is actually first claimed), rather than adding a new migration.
4. `internal/turn/bug_runtime_replies.go`: `continueExhausted`'s stop branch
   hardcoded source attempt id `0` in its `PublishEvent` call, which always
   failed (`RetainBugLifecycleReport` requires a real `graph_node_attempts`
   row) — the store-side "owner-stopped" decision committed but the
   acknowledgement comment was never posted and the caller saw an error with
   no way to replay it. Fixed by adding `BugExhaustionSourceAttempt` to
   resolve and pass the real source attempt id, matching every other
   `PublishEvent` call site in the file.

A regression test that had pinned bug 4's defect as expected behavior
(`TestBugRuntimeContinueExhausted_StopMatchingN`) was updated afterward to
assert the corrected (successful) behavior.

## Known gap flagged, not fixed (judged out of proportion for this run)
- `continueExhausted` never explicitly checks `life.Cancelled`; the
  cancel-blocks-grant guarantee currently holds only as an emergent side
  effect of `CancelBugLifecycle` overwriting disposition and the single
  caller always re-fetching a fresh lifecycle snapshot before dispatch. A
  test (`TestBugRuntimeContinueExhausted_CancelBeforeGrantRefused`) confirms
  current behavior is correct today, but the design is fragile if a second
  call site or a cached snapshot is ever introduced. Left as a note for
  Validate / a future hardening story rather than widening this run's scope
  to add an explicit guard.

## Assumptions of note (see each story's originating engineer report for the
## full list — recorded here only where they affect acceptance framing)
- story-001: `BugTestCall.ID`/`ChildInstance` are now the same durable
  `graph_instances.id`; superseded reviews/merges/closures are retired into
  `bug_events` rather than kept as separate rows; cancel-blocks-dispatch is
  covered by 7 triggers (down from 11) plus application-code checks for the
  two cases that no longer have a direct trigger equivalent.
- story-002: fact redaction relies on the existing `Redactor.Apply` pass over
  the fully composed review body rather than a new `internal/store` ->
  `internal/redact` dependency; analysis-review staleness via a later fact
  changes the digest but has no existing re-dispatch mechanism to surface it
  to the owner again without a new analysis revision (flagged as a partial
  wiring gap in REFUSAL_REVIEW.md).
- story-003: self-check reads the recipe from the repair's own pinned
  `SourceCommit` (no forge access, no network) rather than the factory
  check's live remote-HEAD target; "uncertain" crash-left runs are
  represented as `outcome='pending'` rows cancelled on restart, not a
  literal stored "uncertain" value.
- story-004: "why it fell short" per historical attempt is derived from the
  repair row's own outcome/state/reason or a following `pr_feedback` row's
  body; a completed attempt that itself directly exhausted the budget has no
  backfilled review text.
