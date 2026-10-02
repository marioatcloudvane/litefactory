# Architect: Go Architect
# Created: 2026-09-23

## Verdict

Proceed within the settled scope. No major deviation is required. The implementation needs coordinated changes across discovery, durable story state, pure Plan policy and effect recovery; moving one call after selection does not solve the defect. This review used repository source and the approved feature specification, without forge reads, test execution or implementation edits.

## System Overview

### How This Feature Fits

`internal/daemon/plan_children.go` currently rejects a non-retired child whose state, identity or parent differs before recovery runs. `internal/turn/plan_graph.go` subsequently recovers every pending parent candidate before selection and refuses retained retirement outright. `internal/turn/plan_story_activity.go` already journals effect intent and reads back GitHub, but the candidate journal does not retain complete original retirement authority. Its applied-effect fast path can leave the separately persisted workflow transition unfinished.

Keep these existing owners. Daemon owns eligible passes, capacity, child discovery and backoff. Turn owns factory-authenticated reconciliation and mutations. `internal/planbrief/plan_workflow.go` owns pure story eligibility, dependency invalidation, approvals and assembled readiness. `internal/store/plan_story.go` and `plan_workflow.go` own all durable records and transactions. `internal/github` remains the remote adapter; `internal/report` remains a pure rendering boundary. No runner, command, graph topology or general synchronization service is needed.

### New Packages / Components

No new package is justified. Add a narrow retained lifecycle/reconciliation model to existing Plan state and store ownership, plus a turn coordination seam callable during daemon discovery even without an owner comment. Extend existing report delivery ownership with durable per-material-change notices. Use an additive embedded SQL migration for new persisted records, preserving existing candidates, histories and workflow routes.

### Key Boundaries

- Daemon supplies a verified observation or a classified observation failure, parent identity and an admitted work budget to turn; it does not manufacture retirement approval.
- Pure Plan policy consumes lifecycle, operation and hold values, producing membership, invalidation and readiness decisions. It performs no I/O or clock reads.
- Turn passes exact retained request and authenticated confirmation evidence to the activity. Agent output remains incapable of selecting identity, approval or remote targets (SEC-8, SEC-20, CONC-22).
- Store accepts expected workflow/content revisions and records coherent local completion after readback. No SQL transaction spans forge calls; preserve cross-process parent ownership and the single connection pool (CONC-5, CONC-10).
- Turn/report delivery receives a retained notice identity and sanitized explanation; confirmed delivery precedes input consumption (SEC-6, CONC-4).

### Data Flow

Eligible daemon pass → observe bound child lifecycle and pending request debt → record per-story lifecycle/hold changes → bounded reconciliation of pending or partially completed effects → persist coherent completion or retained uncertainty → serve healthy child/parent input and select an eligible independent story → render/recover notices → evaluate current active-set publication and readiness.

Recovery enumeration must include unbound creates and effects already applied whose workflow completion is missing. It must not be derived only from active/eligible stories or only from pending candidate outcome. Closed stories remain eligible for observation and reconciliation, without becoming eligible for shaping.

### Concurrency & Context

Use existing per-parent ownership and daemon capacity; introduce no worker pool or goroutine. All blocking operations use caller context. Avoid recursively acquiring the same parent lock when introducing the coordinator: activity and coordinator must have a clearly owned acquisition boundary. Save under expected revision after remote observation; stale local completion is a conflict requiring refreshed state, not an overwrite. Pass scheduling decisions from daemon; no sleeps, retry loops or clock reads in turn/github (CONC-2, CONC-5–7). A deterministic rotating recovery cursor or equivalent persisted fairness mechanism should prevent a bounded batch from reconsidering only the first stuck request after every restart. Preserve a chance to select healthy work on the same eligible pass.

### Shared State and Side Effects

Use three separate facts: observed GitHub lifecycle, Plan stage/approval, and operation settlement. Preserve `Retired` for exact owner-confirmed factory retirement semantics; do not set it merely because a user closed an issue. An external closure can coexist with uncertain historical effects. Closed-as-completed never creates acceptance, engineering checks or Plan approval. An external reopen changes current Plan lifecycle but cannot unfreeze an already promoted managed scope (CONC-9, CONC-11, CONC-22).

The shared lifecycle record needs bound issue identity, observed parent relationship, remote state/reason, available observation/version evidence and previous verified transitions. A request-linked hold needs a machine-readable reason plus explanation/next action. The original candidate/request and append-only effect history remain authoritative; a superseded request does not erase an unknown remote result.

## Cross-story Architecture Decisions

1. **Lifecycle is independent of retirement and evidence.** Add explicit observed lifecycle and active-membership policy instead of overloading `Retired`. All consumers use the same membership decision. Unknown identity or observation is a hold, never inferred closure or rebinding.
2. **Story outcomes are values; shared failures remain errors.** Reconciliation returns per-story outcomes such as adopted, reconciled, held or superseded. Persist and aggregate story-specific failures while continuing unrelated work (CONC-3, STYLE-9). Storage failures, unavailable parent authority and repository-wide API/rate-limit failures preserve typed error/backoff handling (STYLE-5). Do not indiscriminately convert all remote errors into local holds.
3. **Recovery is independent of shaping selection.** Daemon admits bounded recovery on eligible passes without new owner input, including unbound creations, inactive stories and applied-effect/workflow gaps. It then allows healthy selection; neither discovery nor one request's receipt can fail the whole parent for a story-local condition.
4. **Original authority is durable before retirement mutation.** Persist numeric owner and actor evidence, source reply, wait/proposal binding, exact candidate/content revision, exact replacements and original request correlation. Revalidate current parent/identity/provenance/replacement eligibility before writes. Historical entries lacking enough evidence remain held; never reconstruct authority from closure, a later unrelated reply or current owner ID alone.
5. **Readback precedes replay; completion is one local transaction.** Match actual remote state to original intent. Exact already-closed/not-planned retirement can complete without another close. A verified later reopen/reparent prohibits replay. The current `github.Issue` supplies state/reason and `UpdatedAt`, but no lifecycle event sequence: `UpdatedAt` alone cannot prove absence of a reopen. Use retained verified transitions; when available evidence cannot establish safe ordering, hold rather than guess. No new timeline API is required for this bounded fix. Atomically persist new lifecycle, draft/effect and workflow completion with revision guards, and repair old partial records explicitly.
6. **Membership changes invalidate assembly, not every independent approval.** Selection, dependency traversal, active-set hashing, publication and readiness use the same lifecycle projection. Independents retain current approvals; dependents need explicit resolution. Check unresolved material request/hold debt across all retained stories before excluding inactive stories from the active contract. A nonempty remaining reviewed set can become ready once debt is resolved; an empty set cannot.

## Suggested Delivery Grouping and Task Hints

These are architectural work groupings for the orchestrator, not newly authored requirements or story contracts. Criterion references below refer to feature-spec.md.

### Group A: Adopt child lifecycle without blocking healthy Plan work

Independently demonstrates the manual-closure fix and its safe membership semantics. Covers AC-003–005 and the lifecycle portions of AC-011–012/015. Establish the lifecycle and hold model used by later recovery work.

### A1: Retain lifecycle and centralize Plan membership
**Agent:** go-implementation-engineer

- **Pattern:** Extend pure `planbrief.PlanState` policy and durable store snapshots; additive SQL migration where persistence needs more than the existing snapshot.
- **Package placement:** `internal/planbrief/plan_workflow.go`, `internal/store/plan_story.go`, `internal/store/plan_workflow.go`, `internal/store/migrations`.
- **Key interfaces:** Existing state revision, candidate/effect history and draft binding accessors.
- **Integration points:** `EligiblePlanStories`, `ReviewRevision`, `PlanSetRevision`, `ValidateComplete`, complete publication projection, replacement validation.
- **Watch out for:** `ReviewRevision` presently checks retired dependencies but not their unresolved effect/hold; transitive availability must be explicit. Do not hide unresolved effect debt merely by excluding a closed item from active iteration. Preserve unspecified closure reasons as unspecified.
- **Not in scope:** Acceptance lifecycle changes, scope upgrades or automatic replacement dependencies.

### A2: Adopt observations during daemon discovery
**Agent:** go-implementation-engineer

- **Pattern:** Replace the early child-state hard error with persisted per-story outcomes while preserving shared failures.
- **Package placement:** `internal/daemon/plan_children.go`, existing turn child-edit reconciliation boundary, pure Plan policy.
- **Key interfaces:** Existing issue/parent observation, state revision and child discovery cursor APIs.
- **Integration points:** Revisit externally closed bindings to discover reopening; do not simply skip them forever. Apply current identity/parent validation before content refinement or reply promotion.
- **Watch out for:** Parent discovery cache cannot serve as authoritative write preflight; mutation paths need current readback. Reparenting stops old-parent writes without auto-admitting elsewhere. External reopening cannot reopen frozen scope.
- **Not in scope:** Parent closure semantics, moving issues, new owner commands.

### A3: Verify lifecycle and membership behavior
**Agents:** go-unit-test-engineer for pure policy; go-integration-test-engineer for actual store/HTTP boundaries

- **Pattern:** Table-driven state tests plus existing daemon fixtures using migrated SQLite and httptest GitHub.
- **Package placement:** Existing `plan_children_boundary_test.go` and Plan policy tests.
- **Integration points:** Closed completed/not-planned/unspecified states, unavailable/changed identity, reopen/reparent, dependency blocking and healthy sibling advancement.
- **Watch out for:** Demonstrate readiness for a resolved closure with remaining valid active work as well as refusal cases. Verify independent approval survival and no remote reopen/reparent writes.

### Group B: Recover effects independently and finish retirement consistently

Depends on Group A's shared lifecycle/hold projection. Demonstrates the interrupted-operation fix, including retirement. Covers AC-001–002, AC-006–010 and operation/error portions of AC-015.

### B1: Retain authority and complete local state coherently
**Agent:** go-implementation-engineer

- **Pattern:** Intent before I/O, immutable request identity, guarded transaction after readback; store-owned transaction helpers compose writes without nested DB calls.
- **Package placement:** `internal/store/plan_story.go`, `plan_workflow.go`, additive migrations; `internal/turn/plan_graph.go` retirement confirmation and `plan_story_activity.go`.
- **Key interfaces:** `ConfirmRetirement`, candidate/effect journal, `ApplyPlanStoryDraft`, workflow history/revision.
- **Integration points:** Persist confirmation before first remote mutation; include workflow retirement and reply settlement in consistent local completion, while delivery cursors remain separately gated.
- **Watch out for:** The current applied-effect fast path must not skip pending workflow repair. Preserve exact replacement list; active replacement checks include observed closure. Legacy insufficient authority remains an explained hold.
- **Not in scope:** New retirement grammar or retroactive fabricated owner approval.

### B2: Add bounded recovery independent of owner input
**Agent:** go-implementation-engineer

- **Pattern:** A per-parent coordinator enumerates debt and produces per-story results before selection, with scheduling delegated to daemon.
- **Package placement:** `internal/turn/plan_graph.go`, `plan_story_activity.go`; daemon Plan admission/discovery and existing backlog ownership.
- **Key interfaces:** Original requests, retirement authority, pending-effect enumeration extended for completion debt; existing `PlanStoryForge` capabilities.
- **Integration points:** Preserve request markers for create readback, bound identity for attach/update, current lifecycle for retire. Continue healthy selection after story-local uncertainty.
- **Watch out for:** No owner comment must be necessary to revisit debt. Do not blindly create again when search has no match; ambiguous matches remain held. A verified later change can supersede intent, but cannot turn an unknown effect into a successful result. Missing ordering evidence does not authorize closing an open child.
- **Not in scope:** Scheduler replacement, additional background workers, timeline service, route kill switch.

### B3: Verify authority, replay and persistence gaps
**Agents:** go-unit-test-engineer for pure outcome/ordering policy; go-integration-test-engineer for real SQLite and HTTP fixture paths

- **Pattern:** Controlled remote-response/persistence failures, reopen the same test database and replay exact retained requests.
- **Package placement:** Existing `plan_story_boundary_test.go`, `plan_graph_boundary_test.go`, store tests and daemon boundary fixture.
- **Integration points:** Remote close before local effect/draft persistence; applied effect/draft before workflow retirement; sibling progress; recovery without new comment; unbound create, attach and update uncertainty.
- **Watch out for:** Assert exact remote mutation counts, stale revision rejection and absent authority rejection. Exercise the discovery gate before recovery, rather than invoking the activity directly as the only proof.

### Group C: Make holds and changed scope actionable across the complete path

Depends on A and B. Delivers durable notices, safe stale-reply handling and aggregate path coverage. Covers AC-013–014 and AC-016, with end-to-end checks for AC-011–012/015.

### C1: Deliver material-change notices and safe reply outcomes
**Agent:** go-implementation-engineer

- **Pattern:** Durable semantic-change identity and recoverable delivery debt using existing reporter ownership; no per-tick notice identity.
- **Package placement:** Existing turn reporting/recovery path, pure report rendering, store delivery intent and daemon child input routing.
- **Key interfaces:** Existing graph comment delivery and research-reporter delivery machinery, story discovery cursor and consumed reply state.
- **Integration points:** Include adopted lifecycle, held request/cause and next action; recover uncertain delivery before cursor acknowledgment.
- **Watch out for:** `plan_graph.go` currently retains graph comments using graph instance/source-attempt identities; an observation without a new graph run must not fabricate these. Extend existing durable delivery ownership with valid observation correlation. Multiple causes for one story must not overwrite unresolved debt or continually create new notices. Failed notice delivery retains input and report debt without losing already recorded lifecycle.
- **Not in scope:** New dashboard, command, public API or disclosure of raw untrusted API text.

### C2: Verify the whole discovery-to-recovery path and repository checks
**Agent:** go-integration-test-engineer

- **Pattern:** Existing daemon fixture with real store and controlled HTTP, real tick admission and deterministic channel synchronization (TEST-3, TEST-6, TEST-8–9).
- **Package placement:** Existing daemon/turn boundary test files; ordinary Go test names and no build tags.
- **Integration points:** Both reported wedges with healthy siblings, restart, report response loss, stale/healthy queued child replies and parent discussion; global failure/backoff compatibility and current readiness/publication.
- **Watch out for:** A unit fake cannot prove atomicity or that discovery actually reaches recovery. Retain actual executed evidence at Implement; require repository `make check`, including race tests and pinned lint, before claiming completion. Do not write `dist/` or run the live daemon.

## Architecture Notes

- Platform: layout/interfaces — existing internal packages and consumer-owned narrow interfaces suffice (TEST-5); no new public API or dependency.
- Platform: errors — typed remote/global failures preserve retry information; story conflicts are recorded outcomes (CONC-3, STYLE-5).
- Platform: database — store-only SQL, embedded migration, one SQLite connection, context-first accessors, no transaction over I/O. Test transaction claims against real SQLite (TEST-6).
- Platform: concurrency — reuse bounded daemon execution and parent ownership; no new goroutine or sleep (CONC-2/6).
- Platform: HTTP/server topology — not applicable; retain existing GitHub adapter.
- Platform: logging/reporting — injected structured logging, redacted outbound reports, stable sorted diagnostics (SEC-6, STYLE-2, TEST-7).
- Platform: tests/lint — repository conventions supersede generic advice: no new assertion library, integration tags or unpinned linter (TEST-1–4/10).

### Security Findings

Scope: forward-looking risks of this Plan change. Rulebook: `.aiteam/APP_CONSTRAINTS.md`.

| Area | Status | Finding |
|---|---|---|
| Input validation | Concern | Lifecycle adoption must verify stored numeric binding and parent; issue text remains untrusted data. |
| Authentication/authorization | Concern | Durable original retirement evidence must survive restart; closure must never manufacture authority. |
| Secrets/configuration | Compliant design | Existing factory forge/redaction boundaries retained; no credentials enter runner context. |
| Data handling | Compliant design | Store owns parameterized SQL; report rendering redacts and bounds remote explanation text. |
| Dependencies | Compliant design | No dependency addition needed; no vulnerability audit performed. |
| Error handling | Concern | Explicit separation of story outcomes and shared infrastructure failure prevents both wedges and concealed global outages. |

## Evidence Bounds and Deviations

No tests executed, no runtime observations, no deployment claims. Planned proof is offline source behavior using existing fixtures; authentic provider or production observations are neither required nor claimed. Referenced `docs/story-contract-instruction-manifest.md` and the nested-agent evidence decision are absent; supplied workflow and factory contracts remain authoritative. These are documentation gaps, not a request to expand this bug fix. No rule deviation requested: external closure adoption is expressly distinct from owner-confirmed retirement, preserving CONC-22.
