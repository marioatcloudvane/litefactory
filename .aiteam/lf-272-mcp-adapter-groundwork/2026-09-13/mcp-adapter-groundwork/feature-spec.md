# Feature Spec: mcp-adapter-groundwork
# Status: APPROVED FOR PLAN AUTHORING; IMPLEMENTATION DEPENDS ON #263 AND #264
# Date: 2026-09-14 (Plan continuation, run 0005)

## Goal
- Give agent hosts a stdio MCP interface to the delivered JSON communication service, with factory-enforced attempt authority, mode scope and owner-controlled creation authorization, ready for #273 tool families and #274 host wiring.

## Sources and Decisions
- Primary evidence: this session's `RESEARCH_BRIEF.md`, including run 0003's daemon-held grant refinement; `.aiteam/APP_CONTEXT.md` reuse inventory.
- Owner run 0004: proceed with Plan; use approximately 30-minute expiry.
- Owner run 0005: #276 must preserve coding agents' existing options; #277 needs configurable automatic MCP startup.
- MCP is additive: the catalog/profile controls only this server's operations, not the agent's complete tool inventory. Preserve existing native tools, unrelated MCP configuration and pre-existing mode/sandbox policy; do not introduce an MCP-only agent mode.
- Startup assumption: `settings.mcp_auto_start` defaults to `true`, making MCP available automatically to each supported active host attempt once #274 wires hosts. `false` disables automatic registration, while the explicit command remains usable with a live descriptor.
- Automatic startup follows the per-host/per-attempt stdio lifecycle, not an idle daemon-wide stdio process. #272 owns the loaded setting and provider-neutral launch specification; #274 consumes it in actual host configuration.
- Creation authorization remains entirely factory-side; public approval hashes identify reviewed content and are not secrets.
- No unresolved owner decisions; exact constants below are recorded conservative planning assumptions.
- Plan scope targets five deliverable areas, below the six-story threshold; story/task decomposition belongs to the orchestrator.

## User Stories
| Area | User need |
|---|---|
| Attempt authority | As an operator, I want each launch to have fresh bounded authority so that replaced or expired agents cannot keep using MCP. |
| Scoped service dispatch (#276 / US-002) | As a workflow owner, I want this server's discovery and invocation checked against mode and frozen scope while my agent retains its existing permitted tools. |
| Stdio adapter (#277 / US-003) | As an agent host integrator, I want a provider-neutral MCP executable and automatic-start policy so that supported hosts can offer communication tools for each active attempt without a manual server launch. |
| Owner creation authorization | As the owner, I want to approve exact content publicly without distributing secrets, with a short expiry and strict creation limits. |
| Recovery and boundary proof | As an operator, I want disconnects, retries, cancellation and restarts to preserve truthful outcomes without restoring old authority or duplicating effects. |

## Existing Features to Reuse
| Existing owner | Reuse |
|---|---|
| `internal/agentresult`, `turn.AgentResultService` | Delivered strict JSON schemas, sparse updates, scoped reads, preflight, submission and receipt semantics. |
| #264 production lifecycle | Existing launch identity, binding, mode/stage bridge and legacy compatibility; recheck the landed seam before implementation. |
| `internal/submission` | Private descriptor and Unix socket, bounded framed protocol, request journal and helper compatibility. |
| `internal/store` | Existing database, embedded migrations, transaction and fresh bookkeeping time facilities; extend adopted launch records when possible. |
| `internal/daemon`, `turn/followups.go` | Numeric-owner control admission, exact command parsing, launch scheduling and uncertain-effect reconciliation precedents. |
| `internal/config`, `internal/secrets`, `internal/github` | Immutable settings, factory-held credentials and existing forge gateway. |

## Data Model
| Entity | Required values | Constraints |
|---|---|---|
| Capability lease | Existing launch/run identity, broker instance nonce, immutable task/repository/mode/scope digest, expiry, terminal/revoked state | One fresh binding per cold/resume/fallback launch; no provider conversation ID as authority. |
| Tool specification/profile | Name, schema reference, mode allowlist, effect classification, permitted selectors | Unknown operations deny; classification covers indirect creation effects. |
| MCP launch specification | Enabled policy, absolute executable, fixed serve arguments, private live descriptor path, bound attempt identity | Factory-generated immutable value for host consumers; no raw capability copied into arguments, no provider-specific fields and no mutation of existing agent permissions. |
| Creation intent | Versioned canonical content bytes/digest, repository, source task, item keys/count, target parent and relationships | Exact publishable content after defined normalization; untrusted supplied hashes do not establish matching content. |
| Owner grant intent | Trusted numeric actor/comment IDs, comment timestamp, immutable proposal digest/context, one next-launch target, deadline, consumed launch | Idempotent by owner comment; edits cannot mutate recorded approval; consumed once. |
| Creation grant | Exact attempt/lease, owner-intent reference, proposal digest, repository/relationship/count, expiry, consumed operation | No model-visible bearer token; one grant/batch per attempt. |
| Creation operation/reservation | Immutable operation ID and content, reserved artifact units, durable pending/confirmed/failed/uncertain state | Unique grant-to-operation binding; retries resolve one operation; durable state survives restart without live authorization. |

### Relationships
- Owner comment → one grant intent → at most one authorized launch/grant → one creation operation.
- Capability lease → factory-owned scope; tool arguments cannot replace scope fields.
- Creation operation → repository quota reservation; unresolved reservations stay charged across attempts and restarts.

## API / Interface Changes
| Surface | Authority | Behavior |
|---|---|---|
| `litefactory mcp serve --descriptor <path>` | Existing private attempt descriptor | Neutral stdio entry; credentials, store access and workflow decisions stay in factory process. |
| `settings.mcp_auto_start` | Operator-loaded config | Strict boolean, default `true`, documented template and no hot reload; enables generation of neutral per-attempt host registration. Setting `false` preserves explicit `mcp serve` with valid authority. |
| Provider-neutral MCP launch specification | Factory launch binding | On enabled cold/resume/fallback launch, supply executable/arguments and fresh descriptor reference to host consumers. Disabled policy supplies no automatic registration. #274 implements actual host consumption and subprocess startup. |
| MCP initialize/initialized | Protocol session | Negotiate supported version before normal requests; tools capability only. |
| MCP `tools/list` | Live broker guard and profile/gate | Fetch authoritative discovery on each operation; no cached-authority bypass or unimplemented placeholder tools. |
| MCP `tools/call` | Same live guard plus operation policy | Thin initial `communication_preflight`, `communication_submit`, `communication_receipt` mappings to adopted shared services. |
| MCP cancellation notification | Current connection/request | Cancel only matching active request context; ignore unknown/completed IDs; no run-cancel authority. |
| Private broker/client extension | Existing descriptor/capability checks | Explicit list/call operations; preserve current helper methods and their domain semantics. |
| Exact `/mcp-create <proposal-hash> linked` or `/mcp-create <proposal-hash> attach` | Daemon-verified numeric owner and comment metadata | Mint one next-launch intent for retained exact proposal/context; command cannot enable config or change mode. |
| Factory creation authorization API | Live attempt + loaded gate + stored grant | Accept canonical immutable intent, recompute digest, atomically reserve or return existing operation; no forge business handler in #272. |
| `settings.allow_mcp_story_creation` | Operator-loaded config | Boolean false default; strict load, documented template, no hot reload; no additional public quota/TTL settings in this issue. |

## Permissions
- This table describes only litefactory's MCP server. Its deny entries do not remove native tools or unrelated MCP servers already permitted by the host's existing configuration.
| Mode | Initial permitted communication | Creation eligibility |
|---|---|---|
| Research | Own result preflight/submission/receipts | Future registered handler requires gate + exact grant. |
| Spike | Own result preflight/submission/receipts | Deny. |
| Plan | Applicable discussion/completed input and receipts under lifecycle timing | Deny pending an explicit later family policy. |
| Implement | Authorized result/checkpoints within frozen scope | Deny pending an explicit later family policy. |
| Validate | Retained assessment/result preflight/submission and authorized receipts only | Deny unconditionally. |
- This MCP server exposes no shell, fresh test execution, waiver, acceptance override, arbitrary forge, dispatch or mode-change tools in any mode. Existing coding-agent native shell/edit/test access remains governed by the current host/mode policy.
- Preserve existing restrictions, including Validate's retained-evidence/no-shell policy and host restrictions on project-provided MCP configuration; additive MCP is not permission to enable previously prohibited tools or servers.
- Validate assessment submission records findings; it grants no self-acceptance or authority to submit fresh execution capture.
- Every MCP-triggered creation effect, including indirect publication through a communication handler, requires the creation policy. Accepting a Plan input is not permission to create stories; preserve pending states or refuse an unavailable safe route.
- #273 owns additional tool families and explicit policy expansion; #272 advertises only executable initial mappings.

## Automatic Startup Lifecycle
- The private submission broker and stdio MCP child are separate components. A running daemon does not imply an always-running MCP stdio child.
- With automatic startup enabled, #272 prepares one attempt-bound registration for a supported host; #274 configures that host to launch/connect the stdio child as part of its active session without owner intervention.
- Explicit/manual startup remains available regardless of `mcp_auto_start`; it still requires a currently authorized factory descriptor and all server-side policy checks.
- No idle task starts a child. Completion, replacement, cancellation and host disconnect terminate/join the applicable child/request lifecycle without extending the attempt lease.
- Resume and fallback receive a fresh launch specification and capability. Reconnect within one still-live attempt reauthenticates the same authority; neither path inherits an old creation grant.
- Automatic startup never enables `allow_mcp_story_creation`, mints a grant, changes a mode, weakens Validate or replaces the host's existing tool inventory.
- An unavailable native host integration is reported as unavailable, never as successfully connected because the setting loaded. #272 tests the neutral startup contract with local fixtures; #274 proves real host startup and teardown.

## Expiry and Volume Policy
- Owner-requested short expiry: 30 minutes, absolute and non-sliding.
- Conservative assumption: the approval deadline is the trusted owner comment creation timestamp plus 30 minutes; daemon processing, launch allocation, edits, reconnects and retries never reset it.
- A next-launch intent must be consumed before that deadline; grant expiry is the earlier of that original deadline and the bound attempt lease expiry. Reject at `now >= deadline`.
- Short approval window applies to issue-creation authority, not a new universal 30-minute limit on all Research/Implement attempts; base lease policy comes from the adopted lifecycle and must remain bounded.
- Fresh owner approval is required after expiry or a consumed launch's resume/fallback/replacement; never transfer a grant automatically.
- Conservative initial constants from research: one grant/batch and at most five new artifact units per attempt; twenty units per repository per rolling 24 hours.
- Count artifacts, including sub-issues, rather than calls; atomically reserve the full batch before external I/O. Exact duplicate operations consume zero additional quota.
- Count pending/uncertain reservations in addition to confirmed units; unresolved reservations remain charged even outside the rolling window. Definitively non-dispatched failure may be released under an explicit recorded state transition.
- Cap overflow is a hard refusal for that admission, never a queued retry when capacity frees. Delayed dispatch must revalidate applicable quota reservation.

## Business Rules and Validation
- Guard every operation, including list, preflight, status/receipt and exact-replay paths, against replaced, expired, cancelled, closed or wrong-broker attempts before shared-service history access.
- Recheck mutable authority/grant/quota atomically at mutation reservation; no database transaction spans remote I/O.
- Preserve trusted factory scope and existing input bytes/journal request IDs; MCP request IDs are connection-local correlation only.
- Validate tool selectors, strict JSON object shape, schema version, payload size/depth, outstanding request limits and bounded output before expensive processing.
- MCP uses newline-delimited UTF-8 JSON-RPC on stdout and diagnostic stderr; private socket framing remains separate.
- Structural protocol errors and domain/tool rejections retain distinct projections; keep input, engineering, acceptance and remote-effect receipt dimensions separate.
- Public proposal/hash, comment ID or grant ID alone grants no authority. Refuse copied/non-owner/bot/quoted commands, missing or changed proposals, wrong source repository/task/mode and owner-comment replay/remint attempts.
- No elevated bearer is serialized into descriptors, prompts, MCP arguments/results or public notices; preserve existing base-capability confidentiality.
- Preflight remains free of proposal retention, grant minting, quota consumption and publication effects.

## Edge Cases & Recovery
- Same live attempt reconnect → new handshake and fresh authority check; retain application request correlation.
- Host EOF/disconnect/request cancellation → propagate cancellation and join bounded workers; retain any already committed durable outcome.
- Revocation → deny new admission immediately; already admitted work may require factory reconciliation and must not be described as rolled back.
- Broker/daemon restart → fresh instance authority; never rehydrate old descriptors or grants as live; recover history internally.
- Valid grant + concurrent duplicates → one durable operation and one dispatch owner; lost replies resolve same pending/confirmed operation.
- Expired grant → refuse creation invocation even if replay; a separately live receipt permission may inspect already-admitted history.
- Partial/uncertain effect → record truthful state; no blind repeated creation and no automatic new artifacts under dead authority. #273 supplies actual effect reconciliation.

## Out of Scope
- Rebuilding delivered #263 JSON validation or implementing #264 adoption/legacy retirement.
- #273 issue/evidence/activity business handlers, proposal normalization/rendering UI, real batch publication and remote reconciliation.
- #274 host-specific translation of the neutral automatic-start specification, native host configuration/launch arguments, actual automatic startup/teardown, real Claude/Codex probes and operating-system isolation verification.
- Replacing agents' native tool inventories, globally allowlisting all agent tools, enabling previously prohibited external MCP configuration or removing existing mode/sandbox policy.
- HTTP listeners, resources/prompts/sampling/elicitation/MCP Tasks, general live owner-control polling and model-visible grant secrets.
- Protected instruction-tree changes, new integration build tags, daemon restarts and writes to `dist/`.

## Acceptance Criteria Candidates
- [ ] Transport: initialize/list/call and cancellation work through real pipes/private sockets, forwarding initial operations into shared services with equivalent diagnostics and receipts; malformed/version/size/concurrency/stdout cases are bounded.
- [ ] Authority: operation × mode × lease-state matrix denies dead/foreign/wrong-scope requests before service access, including receipts and exact replays; fresh cold/resume/fallback capabilities and restart nonce isolation are proven.
- [ ] Profiles: server checks match discovery; Validate rejects execution capture, shell, creation, waivers and scope expansion, while retained assessment communication succeeds.
- [ ] Additive tools: this MCP profile filters only this server's registered tools; generating its registration preserves existing agent permissions and unrelated MCP configuration, including current Validate and project-configuration restrictions.
- [ ] Automatic startup contract: strict `settings.mcp_auto_start` defaults true; enabled cold/resume/fallback bindings produce fresh neutral launch specifications, disabled bindings omit automatic registration, and explicit `mcp serve` still requires live authority. The setting has no grant or mode side effects and starts no idle daemon-wide process.
- [ ] Gate: strict false-default config hides/refuses every registered creation-class operation, including indirect effects; enabled gate alone grants nothing.
- [ ] Owner control: exact authenticated owner commands consume once into one next launch; public identifiers, edited/replayed comments and bot/non-owner/quoted content cannot mint or mutate authority.
- [ ] Grant policy: exact canonical content/attempt/relationship matching, original 30-minute deadline, lease clamp, five-per-attempt/twenty-per-24-hour caps and boundary refusal work transactionally without secret serialization.
- [ ] Dedup/recovery: concurrent/retry/lost-response calls bind one grant to one operation with one reservation; disconnect/cancel/restart/partial effects retain truthful durable states and cannot resurrect authority or elect duplicate dispatch.
- [ ] Compatibility: existing JSON/helper and internal historical recovery behavior remain valid; #272 adds live MCP admission before those recovery paths and never duplicates semantic submission.

## Candidate Evidence Obligations
| Criterion group | Proof method | Source availability | Version/context bounds | Prerequisite / disposition |
|---|---|---|---|---|
| Transport, compatibility | Pipe-driven MCP client plus real temporary private socket, adopted shared-service assertions | Existing broker/submission/result test patterns; new offline fixtures required | Use stdlib under TEST-2; pin supported MCP protocol contract and fixtures, initial target 2025-11-25 | Recheck #263/#264 landed API before code; no current native-host claim. |
| Authority, profiles | Deterministic operation/mode/state tables, race tests and temporary SQLite | Existing broker/binding/submission tests | Factory service boundary only | Fresh bookkeeping-time seam required; native Validate isolation belongs to #274. |
| Additive tools, automatic startup contract | Config default/strict-load tests, neutral registration fixture assertions and before/after permission snapshots | `internal/config/config.go` immutable Settings precedent; `internal/agent/request.go`; existing `internal/harness/validation_test.go` and runner permission tests | Offline factory contract proof; no claim that a real host consumed registration | #274 owns real host registration merge, startup/teardown and authentic delivery; preserve existing host restrictions. |
| Gate, owner control, grant policy | Config/parser tests, trusted metadata fixtures, transactional store tests with exact expiry/quota boundaries | Existing follow-up/config/store precedents | Synthetic metadata proves admission behavior, not live GitHub identity retrieval | No paid agents or live forge prerequisite. |
| Dedup/recovery | Fault injection before/after reservation and dispatch, reopen temporary database, concurrent deterministic handler | Existing recovery patterns; creation tests to be written | Tests prove primitive dispatch/reservation behavior, not real GitHub exactly-once delivery | #273 owns remote publication/reconciliation proof. |
- Implementation verification: focused meaningful tests including races, then repository `make check`; this Plan executes no source tests and claims no implementation evidence.
- Authentic cross-host delivery and sandbox evidence remain #274 obligations; synthetic or historical captures cannot establish present-day host capability.
- Run 0005 requirements inspection found existing host restrictions on ambient/project MCP in `internal/harness/settings.go` and `internal/harness/runner.go`; preserving unrelated configuration means no new restriction from this feature, not removing those existing restrictions.
