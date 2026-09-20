# Architect: Go Architect
# Created: 2026-09-20

## System Overview

### How This Feature Fits

Use the existing graph interpreter and factory publication pipeline. Add Plan-specific application wiring and retained review state; do not build a second engine, draft service, publisher or team installation system. The feature is feasible in three sequential delivery slices: shipped Plan package; durable three-stage conversation and admission; checked publication, final decision and assembled recovery verification. These are structural recommendations for the orchestrator, not authored stories.

Current source confirms that Research's package lives at `internal/teamsource/research/`, independently of installed team catalogs. `CaptureResearch` captures roles and skill resources; `internal/graphexec` supplies pure validation/navigation; `internal/turn/graph_node.go` and `graph_activity.go` execute registered activities. Research-specific admission, owner reply and daemon dispatch code must be extended deliberately for Plan: `store.ClaimGraphOwnerReply` currently explicitly rejects modes other than Research. A Plan DOT and Markdown alone cannot supply the agreed behavior.

The existing shared result `agentresult.Plan` already supports partially populated draft stories; completed typed content uses `internal/planbrief` and `turn/typed_plan.go`. Preserve these layers and their factory-issued identities. Existing `turn.PublishPlan` performs complete-batch validation, durable publication/reconciliation and link-only index generation. Existing `markManagedPlanReady` makes successful publication/save ready without the new post-publication final review. Graph Plan must add that final-review prerequisite while preserving legacy behavior.

### New Packages / Components

No new Go package or external dependency is necessary. Add an application-owned `internal/teamsource/plan/` asset directory mirroring Research: `plan.dot`, stage role Markdown files such as `stories.md`, `engineering.md`, `acceptance.md`, and `skills/plan-evidence/SKILL.md` with narrowly scoped supporting Markdown. The precise skill name is ordinary implementation detail; the directory organization and dependency/resource capture are binding.

Add mode-specific Plan capture, action registration, draft/review persistence and turn orchestration in the existing owning packages. A small shared asset reader or delivery helper may be extracted from Research when it serves both modes; avoid a generalized plugin/catalog framework. Keep mode-specific policy explicit.

### Key Boundaries

- `teamsource` resolves and captures application-owned DOT, role and selected skill/resource bytes. Capture includes content hashes and native delivery closure for both hosts. `graphexec` validates registered `lf_plan_*` action vocabulary and topology without I/O.
- `agentresult` and `planbrief` own bounded typed draft/completion content, not approval authority. A node may propose a revision and classify an owner response; factory validation must bind approval to authenticated reply evidence, the displayed review and retained set revision.
- `store` owns SQL, transactions and durable draft revision, whole-set review approvals, supersession links, owner wait/correlation and publication/final-review references. Reuse graph instances/attempts/outputs/waits rather than mirroring their entire lifecycle in a separate state engine.
- `turn` assembles captured invocation context, applies draft changes, validates transitions and invokes the existing publisher; `daemon` admits numeric-owner replies and schedules retained dispatch/recovery. Cross-package integration remains in-process.
- `report` owns pure owner-facing rendering and redaction. Existing report delivery and managed publication owners retain external effects. Neither agents nor Markdown content gain forge credentials or direct publication authority.

### Data Flow

Explicit Plan admission -> persisted execution path and immutable captured package -> stories draft/review -> authenticated matching approval -> engineering draft/review -> authenticated matching approval -> acceptance draft/review -> authenticated matching approval -> consistency check of that exact revision -> existing complete-story publisher -> confirmed full published set and saved link-only index -> owner final review -> final approval of those exact identities/revisions plus successful save -> Plan ready, still selected.

At every stage, change requests retain content and reopen the earliest affected stage and all later stages for the whole set. Explicit continuation starts/resumes work but does not approve it. Final rejection follows the same route and updates surviving managed issues. No intermediate node's completed application result may fall through to ordinary completed-Plan publication.

### Concurrency & Context

Reuse task/parent coordination and bounded node attempts; add no goroutines, clocks, polling loops or new retry service. Owner waits end the current invocation and release capacity. Persist approvals/invalidation atomically with the revision they describe. Never hold a SQLite transaction or clone gate over an agent invocation or forge operation. Keep the store's single connection, ctx-first accessors, copied boundary values and daemon-owned time (CONC-1–6, CONC-9–10; TEST-8).

### Shared State and Side Effects

Retain a compact Plan draft-set projection keyed to managed parent/generation and graph execution, backed by immutable revisions/outputs. Track stable story keys and active/superseded membership independently of title and issue number. Reviews reference a set revision; final review references confirmed issue identities and revisions. The stage state, content revision and approval evidence must survive dates, restarts and provider fallback together.

Current publisher rejects any omission of an already indexed identity (`plan_publication.go`, "indexed identity must remain in the proposal"). Therefore post-publication removal/split/merge needs an explicit narrow managed-story retirement/supersession extension; do not assume simply omitting a story already works. Preserve historical issue identities and links, exclude superseded stories from active publication/review/readiness, and keep completed historical dependencies immutable. This is required by the approved scope, not a new issue-management system. Do not delete history or add automatic closing policy.

Graph Plan readiness must be distinct from input receipt, creation, stage approval, final approval and Implement admission (CONC-11–12, CONC-21–22). Existing content/submission and workflow versions remain independent; no implicit upgrades. Persist selected graph/legacy path and resume from retained package bytes, never silently recapture changed active instructions. Research's current resume checks against shipped DOT are not proof that the stronger Plan pinning requirement already holds.

## Per-task Structural Hints

The feature spec has no assigned task identifiers yet. The following hint labels identify bounded units for the orchestrator to map to its final tasks; they do not create additional scope.

### H-1: Ship the Plan package and capture/validation support
**Agent:** go-implementation-engineer

- **Pattern:** Mirror `teamsource/research.go` and `research/` application-owned layout with one DOT, stage roles and skill/supporting Markdown.
- **Package placement:** `internal/teamsource/plan/`, capture code in `internal/teamsource`, action specs and safe control validation in `internal/graphexec`.
- **Key interfaces:** Existing graph Profile/Registry, resolved role/native delivery types, immutable resource closure.
- **Integration points:** Register Plan-specific agent/control/comment/check/publication capabilities only where actual activities exist; use closed attributes and bounded loops.
- **Watch out for:** No arbitrary executable DOT expressions, unchecked relative resource paths, unsupported metadata, missing resources, skill cycles or basename collisions. Keep both provider native deliveries faithful to the captured bytes.
- **Not in scope:** Installed `.claude/**` or `.codex/**` regeneration. Application-owned assets allow this feature without protected-tree stories; legacy installed instructions remain supported.

### H-2: Verify the shipped package
**Agent:** go-unit-test-engineer

- **Pattern:** Existing `teamsource` capture/native rendering and `graphexec` validation tests; render in memory or temporary roots.
- **Package placement:** Co-located package tests.
- **Key interfaces:** Package capture, safe role/skill closure, graph registry/validator.
- **Watch out for:** Verify required review/publish ordering, bounded rewind routes, unresolved actions and resource failures; ensure Research remains compatible. Scoped checks must not require installed sibling parity.

### H-3: Retain drafts, approvals and whole-set invalidation
**Agent:** go-implementation-engineer

- **Pattern:** Extend typed draft values and existing durable graph records; persist the minimum extra Plan revision/review data in store migrations and ctx-first accessors.
- **Package placement:** `internal/agentresult`, `internal/planbrief`, `internal/store`, `internal/turn`.
- **Key interfaces:** Typed stage result, immutable set revision, stage approval tied to retained owner reply, explicit changed-stage classification.
- **Integration points:** Partial drafts remain discussion and cannot enter completed typed Plan handoff. Rename retains key; additions/removals/split/merge invalidate stories onward; design/dependency changes invalidate engineering onward; criteria changes invalidate acceptance unless earlier intent changes.
- **Watch out for:** Wording equivalence requires recorded comparison; uncertainty reopens review. Never infer approval from prose alone, stale reply, run success, receipt or silence. No per-story Markdown (CONC-12); preserve untrusted prompt framing (SEC-20–21).

### H-4: Wire Plan admission, invocations, reviews and continuation
**Agent:** go-implementation-engineer

- **Pattern:** Research's opted-in persisted execution path, durable owner waits, registered node activities, shared result helper and daemon dispatch.
- **Package placement:** `internal/config`, `internal/turn`, `internal/daemon`, narrowly shared graph store support; existing host delivery adapters only where necessary.
- **Key interfaces:** Factory Plan action registry, captured package, invocation/result service, authenticated owner claim, report delivery and dispatch records.
- **Integration points:** Default remains legacy; an explicit Plan selection setting follows Research's repository-local admission pattern. Legacy active sessions keep their path. Cold/resumed/fallback requests all carry current stage/revision, saved discussion and captured definitions.
- **Watch out for:** Existing Research mode guards must remain strict while admitting Plan explicitly. Gate review advancement in factory state. Review comments show whole-set content, stage/revision, changes, unresolved questions and approval requested. Loop exhaustion preserves state and requires an explicit fresh bounded continuation. Cancel on mode switch without selecting another mode automatically.

### H-5: Verify transitions and durable continuation
**Agent:** go-unit-test-engineer for pure transitions; go-integration-test-engineer for store/turn paths

- **Pattern:** Pure table-driven validation plus ordinary Go tests with real migrated SQLite and assembled turn/daemon fixtures. Use existing hand-written fakes for remote and provider boundaries.
- **Package placement:** `internal/planbrief`, `internal/agentresult`, `internal/store`, `internal/turn`, `internal/daemon`.
- **Watch out for:** Cover every approved/rejected stage, wrong owner, stale/duplicate reply, changed revision, restart before/after reply claim, worker release, node exhaustion and pinning after shipped source changes. Assert zero story creation during all intermediate conversations. No integration build tags or paid provider launches (TEST-1–8; EXT-4–5).

### H-6: Join checked publication to final review and supersession
**Agent:** go-implementation-engineer

- **Pattern:** Reuse `PublishPlan`, typed submission projections and report/publication receipts; add graph-specific readiness gate and managed supersession support.
- **Package placement:** `internal/turn/plan_publication.go`, `plan_handoff.go`, graph Plan activity code, `internal/store`, content validation and report projections in existing owners.
- **Key interfaces:** Complete typed proposal after consistency validation, reviewed revision and authenticated approval references, existing publication intents, confirmed remote identity/revisions, saved index and final review.
- **Integration points:** All three current approvals, nonempty complete content, resolved questions, valid criterion mappings and acyclic live dependencies precede writes. Acceptance approval authorizes this checked publication without an extra gate. Confirm all effects before final review; ready only after final approval and save.
- **Watch out for:** Preserve issue identity on retry/rename/update; do not clear uncertain effects to force new creation. Existing managed publication requires indexed identities retained, so coordinate historical membership handling with consistency and readiness. Owner changes to remote content invalidate the exact final review; do not overwrite unobserved competing edits. Keep legacy readiness behavior version/path gated.

### H-7: Verify assembled publication, recovery and compatibility
**Agent:** go-integration-test-engineer

- **Pattern:** Extend ordinary `turn` publication/daemon recovery fixtures using real store and deterministic forge loss/retry behavior.
- **Package placement:** `internal/turn`, `internal/store`, `internal/daemon` tests and `docs/workflow/plan-graph.md` evidence/activation guidance (documentation authored by implementation engineer).
- **Watch out for:** Test rejection of unchecked/stale/empty plans before remote writes; lost create/update/attach acknowledgments; restart with uncertain effects; full confirmation before final review; final rejection/rewind/republication; rename and split/merge/removal history; final approval becoming stale; save failure; legacy Plan still functioning. Run repository `make check` before engineering completion. No live-daemon mutation or `make build`.
- **Evidence boundary:** Offline checks establish source behavior only. Document exact prerequisites for independently authorized authentic Claude and Codex role/skill/helper/correction/receipt observations, leaving unavailable evidence unverified and default activation disabled.

## Architecture Notes

Platform: No public HTTP service, additional binary, new module, cache or goroutine is needed. Preserve consumer-owned interfaces and existing injected function seams (TEST-5; STYLE-3–4). Use new embedded SQL migrations only if additional retained state requires them; SQL stays in store and its one-connection pool is unchanged. Use aggregate deterministic validation diagnostics (CFG-5; TEST-7), injected structured logging, existing error/result distinctions and ordinary tests. Project constraints override generic platform suggestions for migration frameworks, new dependencies, panic recovery and assertion libraries.

### Security Findings

Scope: Forward-looking review of this Plan design, not an audit or executed security test.
Rulebook: `.aiteam/APP_CONSTRAINTS.md`.

| Area | Status | Finding |
|---|---|---|
| Input validation | Concern to cover | Bounded partial stage payloads and graph resources need strict validation; completed publication still uses the existing typed contract. |
| Authentication/authorization | Concern to cover | Research owner claims are mode-specific; Plan must add numeric-owner, published-review, stage and revision correlation rather than trust agent approval text. |
| Secrets/configuration | Design compliant | Reuse factory forge ownership, runner permission/environment boundary and strict opt-in configuration; no new credentials. |
| Data handling | Concern to cover | Drafts/replies remain untrusted in prompts; persisted history and outgoing review text follow existing rooted-access and redaction rules. |
| Dependencies/supply chain | Design compliant | No added dependency; no vulnerability audit claimed. |
| Errors/recovery | Concern to cover | Process failure, input acceptance, publication uncertainty and readiness remain independently reported; preserve original effect identity. |

## Decisions Binding Multiple Delivery Slices

1. Use the embedded Research-style package layout; do not create installation or protected-tree parity work unless later implementation establishes a concrete necessity.
2. Keep graph controls generic/pure, Plan approval policy explicit and factory-enforced, SQL store-owned and effects turn-owned.
3. Model one current set revision and whole-set stage approvals; invalidate from the earliest affected stage, retaining immutable history and stable story keys.
4. Reuse managed publication but explicitly extend active/superseded membership and post-publication final-review readiness; surviving remote issue identities must never be replaced during retries.
5. Preserve selected execution path and captured definitions; stay opt-in, support legacy and leave default activation gated on unavailable authentic observations.

## Deviations and Evidence Limits

No major standards deviation requested. The missing nested-agent decision and story-contract manifest are recorded in the feature spec; available workflow/planning contracts and Research activation guidance govern this review. Documentation describing Research as a single role is stale relative to the current two-stage shipped DOT; current package sources control the layout recommendation. This review inspected source and instructions only: no implementation, tests, helper publication or authentic host observations were executed.
