# Architect: Go Architect
# Created: 2026-09-13

Verdict: READY for typed Plan authoring, conditional on #263/#264 landing before implementation. No owner question is required. One mechanical spec correction is needed: the evidence row must not require adding an MCP SDK module; TEST-2 forbids a new module. Use the stdlib bounded protocol adapter described below. This changes no requested behavior.

## System Overview

### How This Feature Fits

`internal/cli` wires a provider-neutral stdio command to new `internal/mcpadapter`. The adapter consumes a small Backend interface declared at that consumer; a thin local bridge invokes the existing `internal/submission` client. The private broker forwards authenticated operations to `internal/turn.ToolDispatcher`. Turn composes adopted JSON result services, the live attempt guard and creation authorization. `internal/store` remains the only SQL owner. Daemon owns launch/control scheduling and numeric-owner admission. Neither adapter nor submission imports store, turn, github or secrets.

The delivered JSON contract is reused, not reconstructed. Current source qualification: `turn/submission_runner.go:runSubmissionBound` still creates the managed-contract-only `StartSubmissionSession` in `turn/submissions.go`; `docs/agent-result-contract.md` describes `BindAgentResultService` as unwired. `store/result_bindings.go` already binds exact task/mode/frozen context and revokes older writers. It intentionally permits some historical reader/replay access. These facts do not prove #264 production adoption. Implementation must inspect its landed launch/result bridge and extend the adopted record; do not implement #264 inside this feature or silently ship a managed-only foundation.

### New Packages / Components

- `mcpadapter.Server`: bounded JSON-RPC stdio session, initialization, tools/list, tools/call and request cancellation. Only protocol behavior belongs here.
- `mcpadapter.Backend`: consumer-owned list/call interface with a fake for transport tests; the production bridge delegates to submission without exposing capabilities in response values.
- `submission.ToolSpec` and `ToolProfile`: immutable neutral catalog metadata, input schema references, explicit mode/selector permissions and effect classification; discovery and dispatch use one policy source. Existing Call/protocol/client code remains the transport owner rather than introducing a second socket client.
- `turn.ToolDispatcher`: authoritative guarded dispatch into the adopted shared service, preserving complete diagnostics and separate receipt states.
- `turn.AttemptGuard`: checks current factory authority on every operation before history or replay access. Durable lease state lives in store, associated with the adopted launch record.
- `turn.CreationAuthorizer`: matches retained canonical intent, loaded gate and daemon-held grant; atomically reserves one operation and quota through store.
- `turn.OwnerGrantCommand` and trusted control input: exact public grammar separated from daemon-supplied actor/comment/timestamp metadata.
- Store records: lease extension, immutable creation intent, one next-launch owner intent, attempt grant, durable operation and artifact-unit reservations. Avoid a parallel attempt identity or database abstraction.

### Key Boundaries

1. Host to adapter: newline-delimited UTF-8 JSON-RPC through stdio; stderr carries bounded diagnostics. No network listener, resources, prompts, sampling or task capability.
2. Adapter to broker: existing private descriptor/socket and framed protocol, extended with explicit list/call operations. Authentication bytes remain private and are stripped before application dispatch.
3. Broker to turn: in-process authenticated request and trusted launch context. Repository, mode, target task and frozen scope cannot be overridden by tool arguments.
4. Turn to shared JSON service: existing preflight/submit/receipt, preserving payload bytes and application correlation. MCP request ID is connection-local only.
5. Daemon to owner-control service: trusted GitHub numeric actor and immutable comment provenance, never a model claim or fetched comment text alone (SEC-8/20).
6. Turn to store: context-first transactional accessors; no SQL/connection outside store, no transaction across external I/O (CONC-5/10/18, TEST-6).
7. Future #273 handler to authorizer: canonical immutable intent in; refusal, existing operation, or reserved permit out. A permit is internal factory dispatch state, not a model-facing bearer. #273 supplies proposal presentation, actual forge effects and reconciliation.

### Data Flow

Ordinary communication: fresh launch binding → descriptor → MCP handshake → tools discovery/call → broker capability check → live lease and profile check → adopted JSON service → unchanged diagnostics/receipt projection.

Creation control: retained exact publishable proposal → public owner command → daemon numeric-identity and command checks → one immutable next-launch intent → fresh eligible launch consumes intent → factory-side grant bound to that launch → authorizer recomputes intent digest and reserves full batch transactionally → internal permit/operation. No real creation tool is advertised by #272.

Approval expires at the trusted owner comment creation timestamp plus 30 minutes. Grant expiry is min(original approval deadline, attempt expiry), refusing at equality. Delayed ingestion, reconnect, retry, edit, resume and fallback do not extend that window. No new universal 30-minute run duration is introduced. The baseline lease remains bounded by the adopted lifecycle. A consumed grant cannot transfer to replacement launches.

Creation cap assumptions: one batch/grant per attempt, at most five artifacts per batch/attempt, twenty artifact units per repository rolling 24 hours. Pending and uncertain reservations remain charged regardless of age; exact duplicates reserve zero additional units. A definitively never-dispatched reservation can be released by a recorded transition, without reopening the single-use grant. Cap refusal is final for that admission, never a delayed queue entry.

### Concurrency & Context

Reuse the broker's bounded worker and cancellation patterns. Bound the adapter's active request map and workers, serialize stdout writes, and join all work on EOF/cancellation. Every additional goroutine documents its owner and stop/join path (CONC-2); no unbounded per-message goroutines. Copy profiles and nested scopes at boundaries (CONC-1).

The application transaction establishes the admission linearization point: recheck mutable lease, grant and quota while reserving mutation. Revocation before admission refuses; revocation after admission does not invent rollback. Already admitted remote work can become uncertain and must retain history. No SQL transaction survives a pipe write or forge call.

Use current store bookkeeping time for live expiry and quota comparisons with a deterministic test seam, following `store/store.go` and its existing bookkeeping clock convention. Do not reuse the long-running turn's start/Tick timestamp or introduce polling/retry clocks inside turn. Daemon retains scheduling and backoff (CONC-6); expiry checks are demand-driven. Test boundary instants and concurrent transitions through channels, not sleeps (TEST-8).

### Shared State and Side Effects

A new broker instance nonce is generated independently at start; persisted rows referring to an earlier instance never restore live authority. Historical records remain readable by authorized internal recovery, while dead MCP descriptors are denied even on exact replay and receipt reads. Admission and internal historical recovery are separate APIs.

The creation gate is loaded once as `settings.allow_mcp_story_creation: false`, following `AutoResumeOnSessionLimit`; add strict/default/template coverage without hot reload (CFG-1/2/3/9). Config is a permission ceiling, not an owner approval.

Effect classification covers indirect creation. In particular, accepting MCP Plan input must not smuggle automatic story publication around the grant gate. Preserve existing terminal/helper publication semantics while the MCP-origin path either retains input with truthful pending effects or refuses if the adopted bridge cannot safely separate acceptance from creation. Carry a factory-stamped origin/effect constraint to deferred publication, not a model field. #272 must prove no later recovery path drops that constraint.

Creation eligibility initially remains Research-only for future registered handlers; Plan/Implement/Spike have no assumed creation permission, and Validate categorically denies. No shell, execute, waiver, dispatch, mode change or acceptance-override tools are registered. Validate only communicates retained assessments/results within existing scope and cannot submit fresh capture (SEC-21, CONC-11).

The command records one next-launch intent through ordinary daemon admission; it does not add live polling during active runs or silently switch mode. If no reviewed proposal exists, it refuses. Without #273 proposal production, tests seed retained immutable proposal records through factory-only seams; no placeholder public proposal tool is added.

## Binding Architecture Decisions

1. Reuse #264 launch identity and #263 JSON semantics; stronger MCP admission precedes historical replay, with no duplicated validator.
2. All authority and credentials stay factory-side; daemon verifies numeric owners and grants are stored authorization records, never model-visible elevated bearers.
3. Grant deadline is non-sliding 30 minutes from trusted comment creation and clamped to attempt expiry; quota is reserved in artifact units with uncertain history retained.
4. One authoritative profile/effect classification governs discovery and execution, including deferred indirect creation; Validate remains read-only in its evidence obligations.
5. Persistence separates authorization from operation outcome: restart revokes old authority, retained operation IDs and reservations survive, cancellation does not claim rollback or successful delivery.
6. Implement the small supported MCP surface using stdlib and existing JSON helpers, not a new SDK dependency. TEST-2 says a new module MUST NOT be added; the optional research SDK preference and spec evidence wording cannot override it. Pin/document the supported protocol contract and fixtures instead. A future SDK adoption needs a separately approved rule amendment, not a silently weakened purity test.

## Per-task Structural Hints

The feature spec has five areas but no frozen task IDs. IDs below are suggested mappings for the orchestrator to align to its own typed tasks; they do not create additional scope.

### T-001.1: Persist and enforce live attempt authority
**Agent:** go-implementation-engineer
- **Pattern:** Extend adopted launch binding/state transitions, reusing `store/result_bindings.go` and `turn/submission_runner.go` after #264 lands.
- **Package placement:** store for embedded migration and atomic access, turn for guard/lifecycle composition, submission for private transport only.
- **Key interfaces:** trusted launch scope, capability lease check/revoke, broker-instance identity.
- **Concurrency/context:** atomic replacement/revocation; store bookkeeping clock seam; no timer loop in turn.
- **Watch out for:** historical reader/replay APIs deliberately bypass writer checks today. Guard before calling them; do not globally destroy their internal recovery semantics.
- **Not in scope:** #264 production adoption or host launch flags.

### T-001.2: Attempt isolation and lifecycle tests
**Agent:** go-integration-test-engineer
- **Pattern:** existing broker/submission/result fixtures with real temporary SQLite and sockets (TEST-3/6), ordinary test naming without tags.
- **Integration points:** cold/resume/fallback lifecycle seam, revoke/close, replaced scope, restart nonce, receipt and duplicate admission.
- **Watch out for:** a fake store cannot prove transactional revocation; barrier-synchronized races must identify the admission boundary.

### T-002.1: Authoritative catalog and shared-service dispatch
**Agent:** go-implementation-engineer
- **Pattern:** immutable allowlist catalog plus consumer-side dispatcher; borrow scoped validation, never parse a second application schema.
- **Package placement:** neutral metadata in submission, policy/composition in turn.
- **Key interfaces:** ToolSpec/Profile, authenticated list/call, adopted preflight/submit/receipt; factory-only effect/origin context on deferred paths.
- **Watch out for:** Validate fresh-capture rejection and indirect Plan publication bypass; wrong frozen selectors must fail before service access.
- **Not in scope:** #273 tool families or shell tools.

### T-002.2: Profile and effect policy matrix
**Agent:** go-unit-test-engineer
- **Pattern:** table-driven operation × mode × policy state with handwritten service-call counters; stable output order (TEST-1/5/7).
- **Watch out for:** discovery hiding alone proves nothing; direct invocation and deferred creation must be covered as distinct paths. Real shared-service compatibility joins belong in the aggregate real-I/O task.

### T-003.1: Neutral bounded stdio transport and CLI wiring
**Agent:** go-implementation-engineer
- **Pattern:** small stdlib protocol edge, bounded parsing and existing submission client; CLI command constructor closes over flags (STYLE-6).
- **Package placement:** internal/mcpadapter; internal/cli/cmd_mcp.go; extend existing submission protocol/client instead of duplicating it.
- **Key interfaces:** Server consumes Backend list/call; byte payloads and durable correlation preserved independently of MCP request IDs.
- **Concurrency/context:** bounded workers, active-ID map, connection-local cancellation, serialized writes and join on EOF.
- **Watch out for:** unsupported initialization/protocol state, duplicate JSON keys, oversized/deep payloads, response budget and stdout purity. No recover handler (SEC-11), no new SDK module (TEST-2).
- **Not in scope:** native Claude/Codex host installation/configuration.

### T-003.2: Protocol edge tests
**Agent:** go-unit-test-engineer
- **Pattern:** pipe-driven handwritten client with Backend fake and deterministic cancellation barriers; no live provider subprocess.
- **Watch out for:** duplicate active IDs, unknown/completed cancellation IDs, lost replies and numeric/string ID correlation; assert protocol versus domain result separation.

### T-004.1: Gate and authenticated owner control
**Agent:** go-implementation-engineer
- **Pattern:** strict immutable setting/default/template; `daemon/followups.go` and `turn/followups.go` numeric-owner precedent, but include trusted creation timestamp in the new control.
- **Package placement:** config for setting, daemon for actual GitHub actor admission, turn for exact grammar/context checks, store for immutable comment intent.
- **Watch out for:** quoted commands, bot marker lookalikes, copied author fields, replayed/edited comments, absent proposals, stale deadlines and mode switching. Intent storage precedes comment consumption (CONC-4/10).

### T-004.2: Transactional grant and operation primitive
**Agent:** go-implementation-engineer
- **Pattern:** content-bound intent/grant/operation relation and store reservation transaction, not a grant token schema in a tool.
- **Package placement:** turn.CreationAuthorizer and store entity accessors/migration. Future family consumes internal permit.
- **Key interfaces:** canonical versioned intent, bound launch, reservation or existing-operation result; factory-only result transition.
- **Watch out for:** semantic content hashing must include repository, exact content, count and relationship targets. #272 hashes the supplied canonical envelope; #273 owns normalization/presentation. Grant uniqueness and dispatch ownership are durable constraints, not process mutexes alone.
- **Not in scope:** actual batch creation, UI or forge reconciliation.

### T-004.3: Grant refusal, expiry and quota proof
**Agent:** go-integration-test-engineer
- **Pattern:** real migrated SQLite with trusted metadata fixtures, config/parser unit assertions and deterministic authorizer races.
- **Watch out for:** exact 30-minute boundary, delayed consumption, attempt clamp, cross-repository budgets, 5/20 thresholds, unresolved old reservations, duplicates versus changed content, restart reopening and no elevated-secret serialization.

### T-005.1: Recovery boundary and aggregate documentation
**Agent:** go-implementation-engineer
- **Pattern:** retain operation intent and terminal/pending/uncertain states independently of channel delivery; daemon owns reconciliation scheduling.
- **Package placement:** existing turn/store recovery seams and operator/adapter documentation. No second recovery engine.
- **Watch out for:** reopening must not elect a second dispatcher or revive a grant; internal observation must not be callable as an MCP acceptance override. Document #273/#274 consumer contracts and pending-state meaning.

### T-005.2: Cross-boundary fault injection and compatibility verification
**Agent:** go-integration-test-engineer
- **Pattern:** real temporary pipes/socket/SQLite against adopted shared service plus deterministic fake creation handler; repository `make check` after focused races.
- **Integration points:** reservation/dispatch/ack barriers; EOF, request cancel, run close, replacement, process-instance restart; JSON helper regressions.
- **Watch out for:** primitive tests prove one durable reservation/dispatch owner and truthful recovery, not GitHub exactly-once creation. Do not launch paid agents or add integration tags.

## Architecture Notes

- Platform: layout/interfaces — new Go edge lives in internal; interfaces belong to consumers and are justified by the transport fake (TEST-5).
- Platform: errors — protocol failures and domain refusal results remain distinguishable; preserve typed errors without content/capability leakage (STYLE-5, CONC-3).
- Platform: concurrency — bounded workers and cancellation joins; no extra retry clock; channel-based race evidence (CONC-2/6, TEST-8).
- Platform: data — existing modernc sqlite driver, one connection, embedded incremental SQL migration and context-first access; no new migration library.
- Platform: dependencies — stdlib adapter preserves current module surface (TEST-2). HTTP server, infrastructure and frontend concerns are not applicable.
- Platform: logging — injected slog/stderr diagnostics, protocol-only stdout and redacted public notices; config paths remain config-owned (SEC-6, CFG-8, STYLE-2).
- Platform: checks — source tests plus make check during Implement; this architecture pass executed no tests and establishes no runtime evidence.

### Security Findings

Scope: forward-looking architecture review only. Rulebook: `.aiteam/APP_CONSTRAINTS.md` SEC rules, supplemented by generic input/least-privilege principles.

| Area | Status | Finding |
|---|---|---|
| Input validation | Planned control | Bounded strict protocol envelope; existing application validator remains authoritative. |
| Authentication/authorization | Concern addressed by design | Every live operation and deferred creation admission must enforce factory scope before replay/history. |
| Secrets/configuration | Planned control | No elevated bearer, immutable false-default setting, credentials stay factory-side. |
| Data handling | Planned control | Canonical content digest/context and parameterized store access; no raw private capability in diagnostics. |
| Dependencies | Corrected design | SDK preference conflicts with TEST-2; stdlib keeps the current dependency policy. |
| Error/recovery handling | Concern addressed by design | Lost acknowledgments and uncertain effects retain durable states; no fabricated rollback or success. |
| Local isolation | External boundary | Same-UID descriptor/database access is not prevented by this service architecture; authentic host sandbox proof belongs to #274. |

## Evidence Inventory for Typed Story Authoring

The orchestrator must attach final criterion IDs to these obligations in each affected story, per EXT-4 and `docs/decisions/nested-agent-evidence.md`.

| Area | Proof method | Available source | Version/context bounds | Missing prerequisite/disposition |
|---|---|---|---|---|
| Attempts/profiles | Real SQLite and operation/mode/state tables, deterministic races | broker_test.go, submissions_test.go, result binding/service patterns inspected | Offline factory authority behavior only | #264 landed launch bridge must be inspected before implementation. |
| Stdio | Real pipes plus private socket and protocol fixture assertions | Existing bounded transport; new adapter fixtures required | Supported MCP contract documented/pinned; target from research 2025-11-25, no SDK assumption | No native-host claim; #274 authentic capture remains separate. |
| Owner/grants | Trusted metadata/parser/config fixtures and transactional reservation races | followup/config/store patterns inspected | Synthetic metadata tests admission, not live API identity retrieval | No live forge requirement in #272. |
| Recovery | Fault injection and reopen with deterministic handler | Existing receipt/recovery patterns plus new operation seam | Proves local operation ownership/state preservation, not remote exactly-once effects | #273 remote reconciliation proof remains its obligation. |
| Compatibility | Real adopted JSON service invoked by broker/adapter and old helper paths | Delivered agentresult/service and submission tests | Current shared service documentation still says unwired | No claim of #263/#264 delivery merely from local tests. |

## Deviations

No owner approval is needed for this architecture. The optional SDK choice is resolved in favor of the normative no-new-module rule; amend the spec evidence row accordingly before submitting the typed Plan. No other rule deviation is proposed.
