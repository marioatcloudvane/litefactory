# Architect: Go Architect
# Created: 2026-09-13
# Updated: 2026-09-14 — Plan continuation run 0005

Verdict: READY for typed Plan authoring, conditional on #263/#264 landing before implementation. No owner question is required. The continued feature spec resolves additive tool scope and configurable automatic startup. Its evidence row now correctly uses a stdlib adapter under TEST-2.

## System Overview

### How This Feature Fits

`internal/cli` wires a provider-neutral stdio command to new `internal/mcpadapter`. The adapter consumes a small Backend interface declared at that consumer; a thin local bridge invokes the existing `internal/submission` client. The private broker forwards authenticated operations to `internal/turn.ToolDispatcher`. Turn composes adopted JSON result services, the live attempt guard and creation authorization. `internal/store` remains the only SQL owner. Daemon owns launch/control scheduling and numeric-owner admission. Neither adapter nor submission imports store, turn, github or secrets.

The delivered JSON contract is reused, not reconstructed. Current source qualification: `turn/submission_runner.go:runSubmissionBound` still creates the managed-contract-only `StartSubmissionSession` in `turn/submissions.go`; `docs/agent-result-contract.md` describes `BindAgentResultService` as unwired. `store/result_bindings.go` already binds exact task/mode/frozen context and revokes older writers. It intentionally permits some historical reader/replay access. These facts do not prove #264 production adoption. Implementation must inspect its landed launch/result bridge and extend the adopted record; do not implement #264 inside this feature or silently ship a managed-only foundation.

### New Packages / Components

- `agent.MCPLaunchSpec` (proposed neutral value): immutable command/arguments and fresh attempt descriptor reference for a host consumer; separate from `Request.Permissions`. Turn constructs it after binding and attaches it to the existing agent request. No provider-specific registration fields or credentials.
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

Creation eligibility initially remains Research-only for future registered handlers; Plan/Implement/Spike have no assumed creation permission, and Validate categorically denies. No shell, execute, waiver, dispatch, mode change or acceptance-override tools are registered by this MCP server. Its ToolProfile is not the coding agent's global tool inventory. Native tools and unrelated MCP configuration already permitted by current host policy remain governed by that existing policy; the new catalog neither removes those tools nor enables previously restricted ones. Validate only communicates retained assessments/results within existing scope and cannot submit fresh capture (SEC-21, CONC-11).

The command records one next-launch intent through ordinary daemon admission; it does not add live polling during active runs or silently switch mode. If no reviewed proposal exists, it refuses. Without #273 proposal production, tests seed retained immutable proposal records through factory-only seams; no placeholder public proposal tool is added.

## Binding Architecture Decisions

1. Reuse #264 launch identity and #263 JSON semantics; stronger MCP admission precedes historical replay, with no duplicated validator.
2. All authority and credentials stay factory-side; daemon verifies numeric owners and grants are stored authorization records, never model-visible elevated bearers.
3. Grant deadline is non-sliding 30 minutes from trusted comment creation and clamped to attempt expiry; quota is reserved in artifact units with uncertain history retained.
4. One authoritative profile/effect classification governs only this MCP server's discovery and execution, including deferred indirect creation. It does not replace agent Permissions or existing permitted tools; Validate's retained-evidence restrictions remain intact.
5. Persistence separates authorization from operation outcome: restart revokes old authority, retained operation IDs and reservations survive, cancellation does not claim rollback or successful delivery.
6. Automatic startup is a loaded true-default policy producing an optional neutral launch specification per fresh bound attempt. #274 owns native consumption, additive host registration and actual child startup/teardown; disabled automatic policy preserves explicit startup.
7. Implement the small supported MCP surface using stdlib and existing JSON helpers, not a new SDK dependency. TEST-2 says a new module MUST NOT be added; the optional research SDK preference and spec evidence wording cannot override it. Pin/document the supported protocol contract and fixtures instead. A future SDK adoption needs a separately approved rule amendment, not a silently weakened purity test.

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
- **Watch out for:** Validate fresh-capture rejection and indirect Plan publication bypass; wrong frozen selectors must fail before service access. Keep the MCP ToolProfile distinct from `agent.Request.Permissions` and config native tool ceilings. Registration generation must preserve native permission bytes and existing allowed host configuration.
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
- **Automatic policy integration:** add strict true-default `settings.mcp_auto_start`, template parity, and a neutral optional launch value on `agent.Request`, populated after each actual bound launch by turn. Keep flag arguments structured, use the resolved absolute factory executable, and copy argument slices (CONC-1). This produces registration data and starts no process by itself.
- **Not in scope:** native Claude/Codex host installation/configuration, translating the launch value into host flags/config, or actual automatic process management (#274).

### T-003.2: Protocol edge tests
**Agent:** go-unit-test-engineer
- **Pattern:** pipe-driven handwritten client with Backend fake and deterministic cancellation barriers; no live provider subprocess.
- **Watch out for:** duplicate active IDs, unknown/completed cancellation IDs, lost replies and numeric/string ID correlation; assert protocol versus domain result separation. Add absent/true/false/invalid setting cases and cloned before/after agent-permission snapshots; enabled cold/resume/fallback must carry fresh launch references, disabled must omit them, and construction must cause no subprocess or config writes. Real explicit serve behavior with valid/invalid descriptors belongs in the pipe/socket tests.

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

No owner approval is needed for this architecture. The optional SDK choice is resolved in favor of the normative no-new-module rule and the spec now reflects it. Additive scope preserves SEC-3/20/21 and existing host restrictions. True-default automatic startup is a recorded policy assumption consistent with CFG-1/2/3; CFG-9's false default is specific to automatic quota recovery, not a blanket rule for every setting. No rule deviation is proposed.


## Run 0005 — Additive Tools and Automatic Startup Contract

### Source seams verified

`internal/agent/request.go` already carries `Permissions`, mode, `RunPath` and `SubmissionRoot`; it has no MCP launch value today. Use an optional provider-neutral value alongside these fields, not a new permissions representation. Any existing request clone/validation path must copy and validate the new value without aliasing argument slices or changing the permissions payload. `internal/turn/submission_runner.go` resolves the factory executable and obtains the fresh descriptor after binding; the adopted #264 equivalent is the construction seam. Do not reopen #264's lifecycle scope to manufacture adoption.

`internal/config/config.go` has immutable Settings and Defaults, including both false-default AutoResumeOnSessionLimit and true-default AutoSyncBase. `mcp_auto_start` therefore follows strict load/default/template patterns with explicit false preserved. `allow_mcp_story_creation` stays an independent false-default setting.

`internal/harness/runner.go` currently appends `--strict-mcp-config`; its documented effect is to ignore MCP configurations except those explicitly supplied. `internal/harness/settings.go` disables automatic project MCP enablement. Those existing restrictions must remain. Additive means preserving what is already allowed, not turning on ambient/project servers. #274 may register the factory-owned server explicitly while preserving native tool ceilings and restrictions; it must not replace them with this server's tool names. Actual allowed-host-config merge/collision behavior and version-specific flags are #274 work, not a guessed implementation in #272.

### Concrete neutral policy behavior

| Input state | #272 output | What it proves |
|---|---|---|
| Loaded setting absent or true, valid fresh bound attempt | Optional launch value containing absolute factory executable, fixed `mcp serve --descriptor` argument sequence, absolute private descriptor path and matching factory attempt reference | Factory prepared automatic-registration data; it does not prove host consumption or connection. |
| Loaded setting false | No automatic launch value | Host consumer has no automatic registration request; explicit serve remains available with a live descriptor. |
| No active bound attempt or idle daemon | No launch value and no stdio child | Enabling configuration alone does not allocate authority or start a background process. |
| Fresh cold/resume/fallback | Newly constructed value from that actual attempt's binding | Previous descriptor/grant is not copied forward. |
| Explicit serve, either setting value | Same normal descriptor and server authority checks | Auto-start is a convenience policy, not an authorization bypass or global disable switch. |

The neutral value is factory-generated structured data, not a shell command string and not an agent-supplied override. It contains no raw capability or forge credential. Matching descriptor/attempt validation prevents accidental stale registration; the live broker remains the final authority after construction. Do not put native tools, unrelated server configuration, raw environment variables or sandbox broadening into this value.

No host-support flag is inferred from the setting. The presence of a prepared value never authorizes a report saying MCP connected. Until #274 supplies native consumers and evidence, automatic host integration remains unavailable; existing helper/terminal communication continues under its current behavior. This does not block #272's neutral-contract completion, and it does block claiming #274 automatic startup complete.

This neutral contract is additive by construction: it neither reads nor rewrites a complete host MCP registry. #272 tests request fields before and after adding/removing its optional launch value. #274 separately verifies that consuming that value preserves any already-permitted unrelated server entries and native tool access, handles its owned registration identity safely, and cleans up only its owned child/registration.

### Evidence additions

| Affected area | Proof method | Source availability | Bounds and prerequisite |
|---|---|---|---|
| #276 additive catalog | Policy unit tests plus immutable before/after Request.Permissions snapshots and source-boundary assertions | Existing request/permission and Validate tests; new neutral fixtures required | Offline proof that our profile and registration construction do not mutate global tool policy; real host inventory proof remains #274. |
| #277 default/start policy | Strict config/default/template cases; enabled/disabled/no-attempt/cold/resume/fallback launch-value fixtures | Existing config settings, request and submission-runner patterns | No host subprocess launched; default true is a planning assumption, not runtime evidence. |
| #277 explicit startup | Real local pipes/private socket and live/stale descriptor cases with auto-start disabled | Existing broker fixtures and planned stdio tests | Proves explicit neutral command works without automatic policy; no real Claude/Codex claim. |
| #274 handoff | Source documentation review of neutral producer/consumer contract and unchanged permissions | This spec and existing native runner restrictions | Actual registration merge, supported versions, automatic startup/teardown and connection observation must be independently implemented and verified in #274. |

The grant policy remains unchanged: absolute 30 minutes from trusted comment creation, earlier attempt-expiry clamp, one batch/five artifacts per attempt and twenty units per repository rolling 24 hours with unresolved reservations charged. Auto-start never mints or transfers approval. No implementation or tests were executed during this review.
