# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The scan indexed 1,547 Go files, including 727 non-test files across internal and cmd, and examined source references, SQL migration/table uses, shipped team resources and workflow documentation. Targeted tracing focused on mode admission, old Plan machinery, shared delivery and recovery. No edits or test execution occurred; this is a bounded inventory rather than an exhaustive reachability proof.

Evidence:

- `internal/turn/execute.go:696`
- `internal/turn/workflow_registry.go:14`

## f2

All five requested modes plus standalone Bug have graph policies in the closed registry. Execute attempts Plan, Research, Spike, Implement and Validate admissions before continuing through the common older execution path. Registry coverage establishes shipped policies, not universal legacy retirement.

Evidence:

- `internal/turn/workflow_registry.go:14`
- `internal/turn/execute.go:696`

## f3

Research always requests graph admission; a persisted legacy selection refuses with manual operator migration required, and a missing graph driver fails closed. Spike similarly refuses legacy/ambiguous history instead of running the old single-agent route. Keep refusal/provenance guards and historical readers; these are safety behavior, not obsolete execution.

Evidence:

- `internal/turn/research_admission.go:68`
- `internal/turn/spike_admission.go:20`
- `internal/store/spike_admission.go:1`

## f4

New Plan sessions select graph, but AdmitPlanWorkflowPath retains a previously persisted route. admitPlanExecution returns unhandled for legacy, allowing old execution. Migration 0058 seeds prior Plan runs as legacy. Do not remove legacy Plan preparation, context, handoff or typed submission as a group until supported saved sessions and unsettled effects drain or undergo an explicit supported upgrade.

Evidence:

- `internal/store/plan_workflow.go:15`
- `internal/store/plan_workflow.go:76`
- `internal/turn/plan_graph.go:69`
- `internal/store/migrations/0058_plan_workflow.sql:7`
- `docs/workflow/plan-graph.md:65`

## f5

Implement still falls through for unmanaged tasks, non-graph automatic recovery and bare harness launches without the graph runtime. Validate still falls through without runtime bindings, managed feature state or an Implement graph instance, and for non-entry commands. These are live branches, even if the daemon's normal new managed-work path uses graphs. Removing them requires explicit compatibility/admission decisions and cohort evidence.

Evidence:

- `internal/turn/implement_graph.go:54`
- `internal/turn/validate_admission.go:173`
- `internal/daemon/setup.go:350`

## f6

Strong small cleanup candidate: store.ApplyPlanStoryDraft has test callers but no production call sites found. Active story effects use CompletePlanStoryEffect, which atomically updates draft, workflow state, notices, effects and completion debt. Move fixture setup to a test helper or the supported completion API before removing the older weaker production method; keep its active tables.

Evidence:

- `internal/store/plan_story.go:157`
- `internal/store/plan_story_recovery.go:70`
- `internal/store/plan_workflow_boundary_test.go:159`
- `internal/daemon/plan_children_boundary_test.go:78`

## f7

Strong wrapper cleanup candidates: daemon.claimResearchReply forwards to claimGraphReply and is called only by tests; production tick calls claimGraphReply directly. turn.PrepareResearchDelivery and PrepareWorkflowDelivery have only test callers found; ReportResearchCandidate and RecoverResearchComment are Research-only wrappers around active generic publication/recovery functions. RegisterResearchAgent/Control/Comment and teamsource.NativeResearch similarly expose older specialized entry points with test-only or no callers found. Remove only the wrappers after retargeting tests; preserve generic driver, policy, segment selection, native rendering and publication safety checks.

Evidence:

- `internal/daemon/research_owner_resume.go:14`
- `internal/daemon/tick.go:2243`
- `internal/turn/research_delivery.go:25`
- `internal/turn/research_delivery.go:138`
- `internal/turn/research_comments.go:148`
- `internal/turn/research_comments.go:221`
- `internal/turn/research_workflow.go:60`
- `internal/teamsource/research_delivery.go:20`

## f8

Additional low-risk helper candidates outside orchestration: cli.notImplemented exists for router tests only; turn.renderImplementHandoff is a test-only convenience wrapper over the active notes renderer. Old conflict-marker collection and validateConflictResolution entry points have only tests found; active suggestion uses snapshot/region validation instead. Keep the live ConflictAdvisor, attemptConflictSuggestion and region functions; remove old helper clusters only after tracing their dependent types/constants and transferring useful coverage.

Evidence:

- `internal/cli/command.go:138`
- `internal/cli/router_test.go:282`
- `internal/turn/implement_handoff.go:110`
- `internal/turn/implement_handoff.go:175`
- `internal/turn/conflict.go:53`
- `internal/turn/conflict.go:165`
- `internal/turn/conflict.go:218`
- `internal/turn/conflict.go:326`

## f9

The double-stored Plan draft is currently used, not dead: DraftJSON is read for owner-edit comparison, dependency binding, effect recovery and lifecycle reconstruction when workflow state lacks the story. Consolidating it would need a replacement reconstruction path and persisted-data compatibility checks. Keep plan_story_drafts identities/ordinals regardless; daemon discovery, doctor and archive also depend on them.

Evidence:

- `internal/turn/plan_graph_edits.go:52`
- `internal/turn/plan_graph.go:297`
- `internal/turn/plan_story_recovery.go:268`
- `internal/store/plan_lifecycle.go:25`
- `internal/store/plan_workflow.go:40`
- `internal/store/archive_registry.go:202`

## f10

The Plan notice outbox has active writers and consumers: workflow updates and effect completion retain semantic notices, daemon reportPlanProgress dispatches them, and report delivery completion settles notice debt. The PlanNoticeNamespace is also used by newer Plan summary and Validate owner reporting. Removing the outbox would lose current notifications/recovery; removing the namespace would affect additional modes. Retain unless replaced with equally durable delivery.

Evidence:

- `internal/store/plan_workflow.go:207`
- `internal/store/plan_story_recovery.go:107`
- `internal/store/plan_notices.go:20`
- `internal/daemon/plan_reporting.go:22`
- `internal/turn/plan_reporting.go:17`
- `internal/store/report_deliveries.go:194`
- `internal/turn/plan_summary.go:120`
- `internal/turn/validate_owner.go:179`

## f11

Two named no-writer Plan tables can be identified as plan_workflow_history and plan_story_owner_edits: production table references found outside migrations are archive registry entries, while archive tests insert historical rows. Keep historical migrations, triggers, archive mappings and any retained data for now. Any future schema retirement needs a new migration coordinated with doctor holds/archive grants; deleting old migration files or dropping tables because current writers are absent is unsafe.

Evidence:

- `internal/store/migrations/0058_plan_workflow.sql:14`
- `internal/store/migrations/0060_plan_owner_edit_observations.sql:5`
- `internal/store/archive_registry.go:196`
- `internal/store/archive_registry.go:203`
- `internal/store/archive_plan_unit_test.go:18`
- `internal/store/migrations/0089_plan_archive_grants.sql:14`

## f12

The issue's PendingPlanStoryCandidates name was not found in the current tree. Current recovery uses bounded plan_story_candidates scanning and completion markers; candidate/effect writers remain active in PlanStoryActivity and recovery. Treat the old name as potentially already superseded, not evidence that the current journal is removable.

Evidence:

- `internal/store/plan_story_recovery.go:48`
- `internal/turn/plan_story_activity.go:144`
- `internal/turn/plan_story_activity.go:235`
- `internal/turn/plan_story_activity.go:259`

## f13

Do not delete result_finalization.go, typed_plan.go, plan_handoff.go or plan_attempt.go wholesale. Shared result helper guidance and final consumption/projection still reside in finalization; typed Plan preparation is invoked by harvest and transport preparation by prepare/fallback. plan_handoff already prefers retained submit-time bytes when available. Isolate old transport/equality checks from active shared result authority and legacy contracts before proposing any deletion.

Evidence:

- `internal/turn/result_finalization.go:23`
- `internal/turn/result_finalization.go:174`
- `internal/turn/result_finalization.go:297`
- `internal/turn/harvest.go:56`
- `internal/turn/harvest.go:250`
- `internal/turn/prepare.go:436`
- `internal/turn/plan_handoff.go:148`

## f14

The missing parent 'stories are ready for review' notice is consistent with current gating: storiesCreated refuses while any first draft is owed, and then requires every active story to have a wait and no approval. Dependent first drafts cannot run until upstream same-stage approval. Thus dependency sets can require approval before all first drafts exist, then fail the no-approval condition. This is a source-derived likely cause, not a fresh executed reproduction. Preserve the durable notice machinery and fix the guide's eligibility separately.

Evidence:

- `internal/turn/plan_graph_notices.go:192`
- `internal/planbrief/plan_workflow.go:251`
- `internal/planbrief/plan_workflow.go:284`
- `internal/planbrief/plan_set.go:158`

## f15

Old installed-team implementation internal/team is absent from this checkout; current team management uses teampackage and teamsource. render remains active for native workflow delivery. The release generator explicitly treats historical releases/saved captures as immutable evidence. Keep teams/go-development/v1 and historical resources unless pin/reference evidence permits retirement; do not mistake generated v1.1.0 distribution copies for dead authored code.

Evidence:

- `internal/teampackage/doc.go:1`
- `internal/daemon/workflow_source.go:24`
- `internal/turn/research_delivery.go:66`
- `internal/teamrelease/release.go:162`
- `internal/teamsource/release.json:4`

## f16

Bug retirement already has a defined history-preserving boundary: unsupported pinned definitions refuse dispatch, old tables become read-only through migration 0096, and records are excluded from orphan repair. buganalysis, bugrepair and bugchecks are still imported by active graph services; Spike and Validate reuse checks. Do not remove packages on legacy-looking names alone.

Evidence:

- `docs/workflow/bug-runtime.md:30`
- `docs/workflow/bug-runtime.md:45`
- `internal/turn/bug_graph_fixer.go:12`
- `internal/turn/spike_check.go:14`
- `internal/turn/validate_suite.go:16`
- `internal/store/graph_diagnoses.go:14`

## f17

Documentation cleanup is safe and useful: APP_CONTEXT still describes the removed internal/team installation architecture, and APP_CONSTRAINTS CONC-24 still says new Plan sessions select legacy while current store admission selects graph. Update these statements against current source without removing still-applicable compatibility and evidence requirements. Legacy config-key rejection diagnostics, persisted run success fallback, metering history and contract-upgrade readers serve current support and should stay.

Evidence:

- `.aiteam/APP_CONTEXT.md:30`
- `.aiteam/APP_CONSTRAINTS.md:1`
- `internal/store/plan_workflow.go:15`
- `internal/config/config.go:822`
- `internal/store/runs.go:1`
- `internal/cli/cmd_story_contract.go:24`
