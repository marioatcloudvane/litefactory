# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Observed at revision adfe773b890319948432268f9246cf089029d3bf: recoverResearchPublications loads the latest Research instance, compares its research/research.dot digest against CaptureResearch, and returns the reported error without inspecting instance status or pending work. Execute calls it after Plan recovery but before replay and mode admission; thus 'first' is not literal, but its cross-mode blocking effect is real. d.leave logs and leaves the source unconsumed.

Evidence:

- `internal/turn/research_workflow.go:421`
- `internal/turn/execute.go:612`
- `internal/daemon/tick.go:2129`

## f2

At the same revision, store admission already creates a linked successor for changed topology or a non-running prior instance, preserves the original definition, checks active node attempts, and refuses session-wide unsettled comment deliveries. Its owner-wait guard only applies to running prior instances. Generic publication recovery walks predecessor instances; merely weakening the latest-instance digest check must not permit old pending effects to replay under current policy.

Evidence:

- `internal/store/research_admission.go:110`
- `internal/turn/research_workflow.go:449`
- `docs/workflow/research-graph.md:43`

## f3

At the same revision, dispatches use pending/reserved/running as open states, and finished/cancelled as terminal states. Owner waits retain waiting/claimed states. Comment debt is linked to report delivery completion and a confirmed comment ID. These persisted records, plus active node attempts, are existing inputs for a safe recovery predicate. resumeResearchDispatch independently refuses a non-running instance and changed shipped digest.

Evidence:

- `internal/store/graph_dispatches.go:65`
- `internal/store/graph_owner_waits.go:106`
- `internal/store/research_admission.go:140`
- `internal/turn/research_admission.go:144`

## f4

At the same revision, ApplyOwnerException still returns raw errors for a missing managed feature and changed retained authority/content; daemon handling routes both to d.leave. reportException already finds a bot-authored exact marker before posting, and consumption follows successful reporting. Reuse must retain the original accepted exception as authoritative.

Evidence:

- `internal/turn/owner_exceptions.go:100`
- `internal/turn/owner_exceptions.go:298`
- `internal/daemon/owner_exceptions.go:32`

## f5

The broad item-3 diagnosis is not established on this revision. ReportReply already builds a stable delivery intent, dispatchDelivery resumes confirmed deliveries or recovers uncertain posts by marker, and CompleteReportDelivery transactionally advances consumption. Schedule and confirmation replies enter this shared path. orphaned ignores Posted but is not the sole recovery mechanism. A remaining duplication claim requires a specific failing path or fault test, not replacing the existing journal.

Evidence:

- `internal/turn/report.go:599`
- `internal/turn/report_delivery.go:134`
- `internal/store/report_deliveries.go:157`
- `internal/daemon/tick.go:1717`
- `internal/daemon/confirm.go:131`

## f6

At this revision, general d.leave failures have no identical-error bound or diagnostic consumption. Report recovery already persists attempts, backoff and log escalation, but does not implement the requested universal terminal diagnostic. Graph dispatch launch retries have their own limit of three. The full CLI registration lists task clean and task retire but no command to inspect and skip a stuck owner comment.

Evidence:

- `internal/daemon/tick.go:2129`
- `internal/daemon/report_recovery.go:139`
- `internal/store/graph_dispatches.go:31`
- `internal/cli/commands.go:8`

## f7

At this revision, story progress is a separate tick-level reconciliation path. It gathers unprojected validation assessments and other durable debt, then polls parents; retained acceptance parses saved output using current validation logic. Tick logs failures and retries later. A comment-only retry counter cannot cover this loop. The exact submission-v2 production failure in #370 was not reproduced in this pass.

Evidence:

- `internal/daemon/tick.go:172`
- `internal/daemon/executor.go:42`
- `internal/turn/story_progress.go:156`
- `internal/turn/acceptance.go:26`

## f8

Validation on the supplied local worktree at adfe773b890319948432268f9246cf089029d3bf: go test ./internal/turn ./internal/store -run 'TestResearchPreCutoverCaptureRefusesRecoveryEffects|TestResearchAdmissionImmutablePathAndDefinitionBoundaries|TestResearchAdmissionRefusesUncertainPredecessorPublication|TestReportDeliveryRefusalSurvivesReceiptLossAndReadinessChange' -count=1 passed both packages. These existing real-store tests establish current refusal, successor and delivery behavior; they do not prove the requested fix. Add terminal failed/cancelled cross-mode cases, pending dispatch/wait/intent cases, exact-one diagnostic recovery across faults, and preservation of the running-dispatch guard. No full suite or production probe was run.

Evidence:

- `internal/turn/research_workflow_test.go:322`
- `internal/store/research_admission_test.go:10`
- `internal/store/research_admission_test.go:66`
- `internal/turn/report_delivery_db_test.go:13`

## f9

Observed at d4baf006eaa386fd4c817a6b9cfb80208417e58c: submission-v2 acceptance inserts a materialized validation-assessment marker, but follow-up discovery selects rows absent from the separate follow-up materialization ledger; retention passes that marker into the version-1 parser, which rejects its missing validation version, and delivery returns before processing existing proposals for that parent. This independently confirms #370's reported code mechanism; production run IDs, log counts and affected rows remain owner-reported. Issue #370 and its empty comment thread were read through GitHub on 2026-09-23.

Evidence:

- `internal/store/submission_progress.go:124`
- `internal/store/followups.go:106`
- `internal/turn/followups.go:130`
- `internal/agent/validation.go:141`

## f10

Inference from f1–f7 and f9: this is a family of durable-work eligibility and failure-disposition gaps, not one parser bug; historical records are treated as current work without consistent checks of format, remaining obligations and recoverability, while permanent errors follow retry-only paths and block their queue or parent.

Evidence:

- `internal/turn/research_workflow.go:421`
- `internal/turn/followups.go:130`
- `internal/daemon/tick.go:2129`
