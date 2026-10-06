# Plan graph and compatibility inventory

Graph Plan has three owner-led stages: story intent, technical shaping, and
acceptance criteria with assembled-plan review. A usable first-stage draft may
be published as a child issue while the plan remains unready. This is the narrow
CONC-22 exception for factory-admitted graph Plan; legacy discussion continues
to save context without publishing stories.

## Package and ownership

`internal/teamsource/plan/` contains the embedded neutral roles, skills and graph;
`teamsource.CapturePlan` captures the complete bounded resource closure and
`NativeWorkflow` renders it in memory for either supported harness. Package
capture does not select the production route or authorize remote effects.

`lf_plan_story` is the only new activity. Roles supply typed semantic content.
The activity derives parent/ordinal titles, renders the prescribed issue body,
retains issue identity and journals candidate/effect history before mutations.
A lost create reply remains uncertain until the original request is reconciled;
a known creation survives attachment failure without another creation.

The architecture role is technology-neutral. Its extensible Skills roster starts
with Go and agent-setup guidance, selected from actual scope. Architecture -
General is mandatory when architecture is populated. Data Model, Service Design,
Solution Design and frontend Design references are conditional. Subsequent
iterations reuse applicable decisions and references; explicit scoped changes
replace them.

## Approval, readiness and routing contract

The factory selects eligible stories independently. Questions and review-ready
output are not approval. Authenticated owner promotion binds the current story,
stage, candidate, upstream dependencies and shared decisions. Stale, ambiguous,
wrong-story and mixed change-and-approval replies do not promote. Exhausted loops
stop resumably and preserve context without granting approval.

Intent changes reopen story shaping; technical changes reopen technical shaping.
Dependency-aware invalidation preserves demonstrably unrelated approvals and
holds uncertain impact for explicit resolution. Retirement is owner-confirmed
not-planned closure with preserved history, never deletion or implementation
completion. The remaining active set must have valid dependencies.

Final readiness requires a nonempty complete typed active set, exact third-stage
assembled approval, no unresolved material questions or pending edits/effects,
confirmed publication using existing child identities, a link-only Story Index
and successful save. Draft publication and local stage approval never freeze
implementation scope. Plan approval, engineering completion and Validate
acceptance are separate states. Ordinary replies do not change modes.

Workflow selection is a separate persisted admission decision from content,
submission and implementation workflow versions. New eligible Plan admission
uses the graph route only through the application admission owner. A saved legacy
route remains legacy through replan, reply and restart unless a supported explicit
upgrade admits another route; neither a role nor a label-like sentence upgrades
it. An incomplete wiring attempt must not claim activation from package tests.

## Retained compatibility inventory

These are current compatibility owners, not deleted or unused code. Removal is a
separate operation requiring evidence that each supported cohort and pending
effect has drained or undergone a supported explicit upgrade.

| Surface | Supported cohort and retained behavior | Removal prerequisite |
|---|---|---|
| `internal/turn/plan_attempt.go`, `plan_context.go`; `internal/planbrief/outcome.go` | Saved legacy Plan discussion, identity-bound `PLAN_CONTEXT.json`, per-attempt outcome/proposal transport and resumed/fallback attempts | No supported persisted legacy Plan discussion or active attempt requiring these readers; saved contexts upgraded through an explicit supported path |
| `internal/turn/plan_handoff.go`, `typed_plan.go`; `internal/store/typed_plan.go` | Legacy full-body proposal v1 and typed contribution v2 Plan handoff, readiness invalidation, receipts and save confirmation | Every supported legacy content/submission cohort and unsettled handoff has drained or been explicitly upgraded; a graph admission does not rewrite these versions |
| `internal/turn/plan_publication.go`, `plan_index.go` | Existing complete-plan publication, expected-base checks, pending creation/attachment/update reconciliation and link-only index replacement | All legacy publication effects resolved and all still-used complete-publication responsibilities transferred with equivalent recovery guarantees |
| `internal/planbrief/proposal.go`, `typed_content.go`, `legacy_upgrade.go`, `reconcile.go` | Existing complete/frozen typed validation, legacy bodies and explicit contract upgrades | No supported frozen generation, retained acceptance or pending reconciliation depends on the format; incomplete graph drafts are not a replacement complete contract |
| Installed `.claude/` and `.codex/` instruction trees | Existing factory roles and installed legacy workflows | Separate authorized instruction maintenance and supported cohort migration; this feature does not rewrite installed trees |

The new package does not substitute its neutral architecture role into the
existing factory implementation roster. Research resources, dispatch and its
independent activation/evidence gate remain unchanged.

## Evidence boundaries

Graph/parser/schema, resource closure, digest parity and in-memory native-render
checks establish package behavior. Real temporary SQLite, controlled HTTP forge
and temporary git fixtures establish the exercised recovery and handoff behavior.
They do not establish authentic hosted mutations or either provider's live
instruction delivery. Those observations remain separate external prerequisites,
including the existing #274/#296 obligations. Required retained compatibility
must not be described as retired merely because offline checks pass.

Production default adoption requires the actual admission/dispatch wiring and
its executed regressions; removal requires the additional cohort/effect evidence
in the table. The session engineering evidence records which changes and checks
were actually completed. No daemon restart, `dist` rewrite or automatic mode
transition is part of this workflow.
