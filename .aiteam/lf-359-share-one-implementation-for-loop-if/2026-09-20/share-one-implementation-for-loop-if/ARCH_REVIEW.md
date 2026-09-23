# Architect: Go Architect
# Created: 2026-09-20

Verdict: READY. A bounded refactor is feasible without new packages, dependencies or a database redesign. This is more than a handler rename: package capture, provider request validation, launch persistence and reply admission currently contain Research gates. Keep two delivery stories, each with separate implementation and test tasks. No material owner decision or standards deviation is needed.

Citation base: `8fb96c32aab20f7ac5c245139448236d44b8aea1`. Source references below describe that baseline, not implementation evidence.

## System Overview

### How This Feature Fits

Keep the existing dependency direction. `internal/graphexec` owns pure graph contracts and validation; `internal/teamsource` captures shipped Markdown/DOT and renders provider delivery; `internal/agent` carries provider-neutral request identity; `internal/agentresult` validates Research content; `internal/turn` coordinates runners, activities, documents and reporting; `internal/store` owns durable state; `internal/daemon` authenticates and schedules continuation.

Reuse the graph engine and retained records. The seams are already present: `graphexec.ActionSpec` and `Registry` (`internal/graphexec/registry.go:22`), `turn.ActivityRegistry`, `ExecuteNodeAction`, and injected invocation preparation. Generic names need no multi-mode registry: build one registry for the admitted mode, register the same five implementations with that explicit mode, and retain the `spec.Mode == profile.Mode` check (`internal/graphexec/validate.go:128`). Register `lf_research_brief` only in Research. Name syntax is no longer authority.

### New Packages / Components

No new package. Within existing owners:

- A small static workflow policy/options value in `turn`, with pure graph policy supplied to `graphexec`, combines admitted mode, response vocabulary, typed result decode/validation, Research guidance/presentation, and owner-gate/failure policy. Prefer function fields where sufficient (STYLE-3, STYLE-4, TEST-5), not a plugin registry or a universal untyped result schema.
- Shared agent/control/publication activities replace the Research mechanism implementations. `lf_comment` and `lf_ask` use one retained-publication mechanism with different registered capabilities: only ask opens an owner wait; comment accepts final/failure reporting. Keep persisted question/review/final/failure kinds.
- `lf_research_brief` is a concrete Research activity in `turn`, reusing the existing two renderers and artifact validator. Its retained output binds source attempt/candidate to the two document references.

### Key Boundaries

1. Package capture and native delivery: generalize the mechanics in `internal/teamsource/research.go:47` and `internal/turn/research_delivery.go:25`. The shipped Research entry point may remain a thin registration/asset selector; shared capture takes an admitted mode, package filesystem and DOT path. The test-only second package uses this same path. Preserve bounded reads, resource closure, metadata validation and permission intersection. No arbitrary repository-provided workflow admission.
2. Invocation and result acceptance: `internal/turn/research_agent.go:78` owns attempt/run checks, helper closure, dispatch, refused-resume fallback, result retention and commit narrative. Move those mechanics behind the shared policy; Research decoding remains in `agentresult`. `internal/agent/request.go:143` currently permits graph invocation only for Research and must follow explicit admitted graph identity without granting another mode production launch permission.
3. Lifecycle and persistence: share the driver in `internal/turn/research_workflow.go:23`, delivery preparation, captured-launch handling, continuation and report recovery. `internal/store/research_admission.go` contains additional mode-specific predicates beyond `graph_*.go`; generalize required accessors, preserving existing table names and data. The `research_*` tables in migration 0052 do not inherently require a schema rename. Keep the production Research opt-in/legacy selection and `researchDefaultActive=false` in `internal/turn/research_admission.go:23` unchanged. A test-only second-mode registration is sufficient.
4. Reply authority: `internal/store/graph_owner_waits.go:60` hard-codes Research before already checking the stored definition mode. Replace that hard-coded admission with valid explicit mode matching; preserve numeric owner identity, task/source match, completed delivery, comment ordering and current wait/attempt checks. `internal/daemon/research_owner_resume.go` and `graph_dispatch.go` need the same shared continuation seam, not parallel implementations. Production registration remains Research-only.
5. Documents: `internal/turn/research_agent.go:184` currently writes briefs before artifact checks. Separate declaration/pre-existing-artifact validation from generated-document existence. Only the explicit scoped activity generates them; success/report/handoff consumers must verify a successful generation bound to the same source. File existence alone is never proof.

### Data Flow

Admitted mode/package → captured generic graph and role closure → existing attempt/run reservation → shared agent invocation → retained mode-validated candidate → Research brief activity → controls → shared comment or ask delivery → durable owner wait or normal stage continuation.

On an authenticated reply, claim the existing wait → preserve candidate/reply identity → resume the shared driver at the authorized stage. Agent `done` or `approved` text does not create reply authority. A failed agent follows the failure-report path without generating a brief; a failed brief preserves that failure and cannot release a successful report/handoff.

### Concurrency & Context

No new workers, goroutines or clocks. Keep execution under existing task ownership and existing clone-gate boundaries; do not hold SQLite transactions over model, file or forge I/O. Preserve cancellation and bounded cleanup, one agent stage per run and durable dispatch of the next stage (`research_workflow.go:64`). Distinct refused-resume/fallback invocations retain distinct metering attempts (CONC-1–6, CONC-18). Shared policies and captured slices/maps must be immutable or cloned at boundaries.

### Shared State and Side Effects

Retain graph definition/instance, node attempt/output, launch snapshot, comment intent, report delivery, wait and dispatch records. Keep physical database names even where they say Research. New SQL remains exclusively in `store`, with its single connection unchanged. Preserve delivered-before-acknowledged ordering (CONC-4), success-only save (CONC-8), stable candidate/effect identity and uncertain-delivery reconciliation (CONC-10). Generic action names rotate graph digests; reject pre-cutover captures clearly before dispatch or effects. Do not add aliases, old-envelope decoders, data deletion or migration/resumption behavior.

`WriteResearchBriefs` currently writes sequentially and `ValidateResearchArtifacts` checks paths afterward (`research_briefs.go:32`, `research_artifacts.go:14`). The new activity must establish safe destinations before writes as well as validate artifacts afterward; `os.Root` confinement alone is not symlink refusal. Deterministic document bytes and source-bound successful output are enough for replay; no cross-file transaction system is needed. Partial files may remain after failure, but no downstream success may treat them as a completed generation.

## Minimal Delivery Split and Task Hints

Suggested split: two stories, second depending on first. These are structural task hints for the orchestrator, not new story requirements. Keep estimates qualitative: modest refactor with meaningful boundary tests; source inspection does not justify an hours promise.

### T-001.1: Shared registry, package capture and mode policy
**Agent:** go-implementation-engineer

- **Pattern:** Per-launch registry plus small immutable policy; retain pure validation in `graphexec` and bounded owned package capture in `teamsource`.
- **Package placement:** `internal/graphexec/{registry,validate,research_control,research_refs}.go`, `internal/teamsource/research*.go`, mode policy wiring in `internal/turn`.
- **Key interfaces:** Existing `ActionSpec.Mode`, `Registry`, `Profile`, captured role/resource values and mode-owned response decoder.
- **Integration points:** Register five generic action names and shared source-control evaluation; parameterize owner-gated path validation. Research vocabulary/templates remain scoped.
- **Watch out for:** Removing the naming fence without retaining explicit mode checks; accidentally allowing any scoped action by syntactic name alone; source defaults after a failed/missing-result attempt.
- **Not in scope:** New schema, plugin discovery, production Plan registration or Plan DOT changes.

### T-001.2: Shared activities and lifecycle with Research converted
**Agent:** go-implementation-engineer

- **Pattern:** Extract existing mechanisms and inject policy; do not create one new implementation per mode.
- **Package placement:** `internal/turn/research_{agent,control,comments,workflow,admission,delivery,owner_resume,meter}.go`; required request/provider delivery seams in `agent`, `harness`, `codex`; admission/reply queries in `store`; authenticated continuation wiring in `daemon`.
- **Key interfaces:** `ActivityRequest`/`ActivityResult`, `agent.Request` graph/delivery identity, `AgentResultService` trusted binding, existing report delivery and store accessor contracts.
- **Integration points:** Agent dispatch/result lifecycle, controls, shared report/ask mechanism, package/role delivery, captured admission and restart/owner continuation. Convert Research generic action registrations, DOT and relevant captured role instructions together; remove old-name registrations.
- **Watch out for:** Hidden hard-coded Research predicates in launch/store/request paths; failure settlement or commit narrative losing process identity; only a refused resume allows the current cold fallback. A minimal intermediate Research result-policy hook may retain existing document generation until the dependent document-activity story removes it; this is not the final feature acceptance boundary.
- **Not in scope:** Physical database renaming, provider CLI feature changes, production activation or compatibility.

### T-001.3: Focused shared-mechanism checks
**Agent:** go-unit-test-engineer

- **Pattern:** Existing co-located table tests and hand-written fakes (TEST-1–5). Verify registry authorization, source evaluation/defaults, malformed envelopes, graph paths, generic request binding and mode-specific policies.
- **Package placement:** Existing `graphexec`, `teamsource`, `turn`, `agent` and provider-adapter test files.
- **Watch out for:** A second vocabulary tested only in controls is insufficient proof of lifecycle reuse. Reserve the complete assembled fixture for T-002.3; no paid provider execution (EXT-5).

### T-002.1: Explicit Research documents and graph cutover
**Agent:** go-implementation-engineer

- **Pattern:** Concrete domain activity over existing renderers, with source-bound retained output and mandatory graph policy checks.
- **Package placement:** `internal/turn/research_briefs.go`, `research_artifacts.go`, Research policy validation in `graphexec`, and `internal/teamsource/research/`.
- **Key interfaces:** Named retained Research source, admitted session/worktree, two artifact references, success/failed outcome.
- **Integration points:** Both investigation and shaping success routes pass through a brief node before report, ask/review or handoff. Controls keep referring to their agent source, not the brief output. Both generic report/ask source checks and graph admission enforce the generation prerequisite.
- **Watch out for:** Old files satisfying a new candidate; source/candidate mismatch on recovery; broad exemption of missing artifacts; symlinks followed before validation; newly introduced failure paths lacking clear diagnostics. Remove document generation from `lf_agent` entirely by this story's completion.
- **Not in scope:** General document generation framework, extra Research activities, Plan publication.

### T-002.2: Document-boundary checks
**Agent:** go-unit-test-engineer

- **Pattern:** Pure policy/rendering tests with faked external access. Cover exact generated-path exemptions, deterministic bytes, source-bound prerequisite selection and report/handoff bypass rejection.
- **Package placement:** Existing Research rendering and graph validation tests; retain typed failures rather than string-only branching (STYLE-5).

### T-002.3: Assembled reuse, durability and regression proof
**Agent:** go-integration-test-engineer

- **Pattern:** Extend normal untagged tests with real temporary files, migrated SQLite and existing git fixtures; injected runner/forge transport. No integration build tag or `_Integration` naming. TEST-3, TEST-6, TEST-8 apply.
- **Package placement:** Existing `turn/research_*_test.go`, `daemon/research_*_test.go`, graph/store tests and `teamsource` capture fixtures.
- **Integration points:** Complete converted Research and a small test-only second-mode package traverse the same loading, role delivery, invocation, controls, report/ask, wait and restart machinery; the latter never invokes Research document generation. Add a Markdown role plus DOT node without new handler or role-specific runtime branch.
- **Watch out for:** Prove acknowledgment loss and stale/duplicate reply handling against real durable state; unsafe destination and partial document-write failures against actual files; preserve failed process outcomes and attempt/meter identities. Include unsupported old captures and unchanged non-graph/default behavior.
- **Validation:** Implementation runs required `make check` and confirms empty `gofmt -l .`; retain actual check evidence. This review executes no tests. Synthetic/provider-adapter tests establish application behavior only, not authentic Claude/Codex compatibility.

## Cross-story Decisions

1. One per-admitted-mode registry, explicit `ActionSpec.Mode`, a small static policy and one shared lifecycle; no dynamic system or multi-mode registry required.
2. Generic ask/comment share delivery machinery but only ask opens a durable owner wait. Persisted kinds and existing identity/reconciliation rules stay intact.
3. Agent result retention and Research document generation are separate. Defer file existence only for the two generated documents; success/report/handoff requires the matching generation output.
4. Reuse physical store tables; parameterize mode-dependent accessors and keep production Research activation/legacy selection unchanged.
5. Full Research cutover, no compatibility; the second mode exists only as a test registration/package. No Plan changes.
6. Limit instruction changes to captured Research assets/shared documentation. No protected-tree edits are currently necessary.

## Platform and Security Notes

Platform: existing package owners, context-first blocking operations, typed wrapped failures and consumer-owned narrow seams apply. No new HTTP entry point, service, dependency or goroutine is needed. Tests use existing stdlib/fake/real-store conventions and required repository checks; generic platform advice does not override the repository's single SQLite connection or hand-rolled migrations.

Forward-looking security review under APP_CONSTRAINTS (not a codebase security audit): input validation and authorization are concerns at the removed naming fence and generated-document boundary, addressed by explicit mode/identity checks and pre-write safe-path validation; secrets/configuration need no change; outbound data keeps typed redaction and immutable retained delivery (SEC-6); numeric owner checks stay authoritative (SEC-8); captured role permissions cannot widen the factory ceiling (SEC-3, SEC-20–21); no dependency or new external API surface is proposed. Preserve bounded actionable diagnostics without leaking credentials.

## Deviations and Evidence Limits

No standard deviation requested. `.claude/skills/go/platform-constraints.md`, security-scanner and task-decomposer are present and were read. The task roster supplied by the factory controls test ownership despite older decomposer text assigning everything to implementation.

`docs/decisions/nested-agent-evidence.md` and `docs/story-contract-instruction-manifest.md` are missing, as recorded in the feature spec; use the available workflow contract and research graph documentation for current boundaries. This plan's offline checks do not satisfy unrelated authentic #274/#296 host obligations or authorize production activation. No source code, tests, runtime state or protected instruction trees were modified during this review.
