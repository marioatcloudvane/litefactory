# US-009 — per-input compatibility disposition

Status: **BLOCKED; all three removals remain incomplete.** This records implementation prerequisites, not acceptance or an approved scope change.

## Identity and evidence boundary

- Parent: github:marioatcloudvane/litefactory#264; story: US-009, #306, issue ID 5463756074, parent ID 5439211198.
- Frozen revision: `ef612f5b50be87b3692b0d38b645e4eef2d87e61e1a5db067e8aab5e211cefb7`; dependencies: US-001, US-002 (engineering recorded by orchestrator).
- Source inspection: 2026-09-15 18:40 UTC; checkout HEAD `16634ed33075ea3acfc449bcb6316163bdc0b807` plus uncommitted run-0007 implementation. HEAD alone is not the implementation snapshot.
- Runtime cohort inspected: **none**. Observation time for a runtime cohort: **unavailable**. No count of zero consumers/effects is claimed.
- Supplied runtime facts are limited to the factory launch: parent #264, run 450/ordinal 7, generation 1, workflow 2, selected immutable stories, and its attempt-scoped submission endpoint. This is not a daemon-wide cohort export and supplies no legacy drain proof.
- No daemon database/configuration was opened or changed; no forge reads, upgrades, completions or drains were performed.

## Safe inventory access finding

`internal/cli/commands.go` registers no complete local legacy-cohort/pending-effect export. `daemon status` reports launchd/process/tick status, not generation contracts or pending effects. Metering, logs and repository listing cannot prove the required complete inventory.

`internal/cli/cmd_story_contract.go:runStoryContractUpgrade` previews without migration, but resolves the configured credential, calls `forge.AuthenticatedUser`, and invokes `turn.PreviewStoryContractUpgrade`, which reads current issue bodies/relationships. Thus even its default preview is outside this run's factory-only forge authority. `--apply` additionally performs migrations and publication and requires explicit operator authority; none was inferred here.

Existing `store.Open` is not a read-only database opener: it can create the file and its DSN requests WAL mode (`internal/store/store.go:Open/DSN`). A custom inspection script using raw SQL outside `internal/store`, manually copying/changing runtime versions, or a new ad hoc opener would not be the requested supported read-only route. The attempt receipt helper exposes scoped submissions, not a whole-daemon compatibility inventory. No such workaround was used.

Required next evidence: a factory/operator-produced, read-only consistent inventory identifying environment/database cohort, observation time, parent/generation/run identities, workflow/content/submission versions, supported continuation/admission routes, and each outstanding operation/receipt. Include active, failed, interrupted, restartable and pending-upgrade scopes, not merely currently running tasks. Remote effects need trusted observation/settlement evidence through factory owners; local absence alone is insufficient.

## Plan v1 file ingress

**Disposition: retain; AC-003 incomplete.**

Consumers: `prepare.go` calls `preparePlanHandoff`; `harvest.go` calls `publishPlanHandoff` after successful execution. `plan_handoff.go` reads the current outcome/proposal using rooted artifact readers. `typed_plan.go:prepareTypedPlanSubmission` returns no typed callback for missing generation contracts or content version other than 2. Legacy supported Plans therefore still need their file ingress. `submissions.go:StartSubmissionSession` similarly bypasses typed submission for absent/non-2 contracts.

Supported versions: persisted content/submission 1 compatibility; content/submission 2 typed Plan also retains factory-generated internal files consumed by the same publication owner. Workflow version is independent and must remain unchanged. Do not remove `publishPlanHandoff`, rooted shared artifact helpers or the whole file as if exclusive to legacy input.

Actual cohort and pending effects: unknown. Required inventory includes planning generations, saved/restartable Plan intent, retained upgrade intent, proposal publication/create/attach/index/save/readiness debt and pending receipts. Before deletion, prove every supported v1 Plan consumer complete or eligible for supported upgrade/admission, retain exact revision-bound upgrade/completion receipts, and prove remaining v2 internal publication still works. Source inspection alone proves none of these runtime conditions.

## Implement event-file ingress

**Disposition: retain; AC-004 incomplete.**

Consumers: `approved_context.go:prepareApprovedContext` supplies `.litefactory-story-events/<run>` whenever StoryProgress is present and selected work is not parent recovery. `harvest.go` registers it through `StoryProgress.Register`. `story_progress.go:Register` creates the transport; `Poll`/`Finish` enter `pollParent`, which calls `ingest` and reads ordered event files. This registration/ingress is not shown to be exclusive to workflow v1; persisted contract version and workflow version must be inventoried separately.

Shared live owners: `pollParent` waits for reliable ingestion before calling `deliverParent`, acceptance/follow-up/delivery owners and `CompleteManagedFeature`. `store.ListStoryProgressRuns`, `ListStoryProgressEffects`, pending submission/acceptance records and historical progress remain recovery state. Deleting the whole StoryProgress service would destroy supported behavior.

Actual cohort and pending effects: unknown. Inventory must include registered active/ended streams, unread/invalidating input, snapshots/checks, close/reopen/report debt, engineering checkpoints, acceptance/follow-up/delivery and pending submission effects. Drain through existing factory owners only; retain proof that unread input cannot invalidate an earlier close. No zero-effect claim, completion command or upgrade was made.

## Validate v1 assessment parser

**Disposition: retain; AC-005 incomplete.**

Consumers: `harvest.go` calls `harvestValidation`; `validation.go:harvestValidation` parses successful `ResultText` with `agent.ParseValidationOutput` unless the typed submission path already set `submissionValidated`. `prepareValidationContext` requires a separated workflow in implementing state; this is legacy submission/parser compatibility, not evidence that Validate supports workflow-v1 combined verification.

Supported versions: legacy structured validation output version 1 and retained frozen context/checks; submission version 2 uses shared validation and typed terminal reconciliation. The core parser/assessment data may remain shared even after exclusive legacy ingress can retire. Preserve complete reference-only restricted terminal assessment, retained assessment/acceptance history, and workflow-v1 combined verification through its existing owner.

Actual cohort and pending effects: unknown. Identify separated-workflow generations with legacy submission contracts, suspended/restartable Validate attempts, retained assessments and acceptance/report/projection/closure debt. Prove compatible reference-only routes after supported upgrade or completion, preserving workflow versions and exact evidence attribution. No shell permission or capture expansion is allowed for Validate.

## Criteria and milestone accounting

- AC-001: source consumers/versions documented; factual runtime cohort/effect portion unverified.
- AC-002: no authorized upgrade/completion receipts available; no migration performed.
- AC-003 / AC-004 / AC-005: readers retained; removal not performed.
- AC-006: explicit incompletion and owner-decision proposal prepared; offline regression execution delegated to the test specialist and must be recorded separately. Passing fixtures cannot establish live drain.
- Retirement milestone: incomplete. Production adoption is assessed separately by the orchestrator; this document makes no adoption-complete claim.
- US-008's separately proven unreachable Markdown discovery removal does not prove any of these three readers removable.
- Authentic #274/#296 captures remain external and unsatisfied; none were collected here.

See `US-009-retirement-followup.md` for the exact proposed follow-up and scope revision. It is not submitted, approved, or a waiver.
