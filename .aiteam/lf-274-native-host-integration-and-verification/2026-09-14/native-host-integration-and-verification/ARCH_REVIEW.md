# Architect: Go Architect
# Created: 2026-09-14

Verdict: READY FOR STORY DECOMPOSITION. No major standard deviation is required. Runtime wiring is substantive but bounded; authentic evidence remains an external, unsatisfied delivery prerequisite.

## System Overview

### How This Feature Fits

The feature joins existing services in `internal/turn` to provider-owned subprocess registration in `internal/harness` and `internal/codex`, using the vendor-neutral `internal/agent.MCPLaunchSpec`. `internal/submission` remains the local broker/protocol owner; `internal/mcpadapter` remains the stdio edge. Existing `internal/store` records remain the authority and receipt source. No new SQL owner, schema, transport protocol, tool business logic, scheduler or publication path is needed.

Source confirms that `AttemptAuthority` is expressly runtime-owned but not adopted in production, `MCPLaunchSpec` lacks the tool list, `runSubmissionBound` creates only managed typed-submission sessions, and both runners presently ignore MCP registration. These are integration gaps, not optional configuration polish. `StartSubmissionSession` returning nil for unmanaged Research/Spike must not suppress the new independent MCP attempt preparation.

### New Packages / Components

No new top-level package or dependency is recommended. Add a narrowly scoped production MCP preparation component inside `internal/turn`, a neutral copied catalog on the existing launch value, provider-local config serialization/resource helpers, and a conditional prompt renderer in the existing shared prompt path. Add offline evidence mappings/fixtures using the repository capture convention, not a new observation service.

### Key Boundaries

- Runtime composition (`internal/daemon`, startup wiring in `internal/cli`) constructs one `turn.AttemptAuthority` against the existing migrated store and closes it on owned shutdown. Do not construct it per turn: `StartMCPInstance` invalidates the previous runtime identity.
- `internal/turn` owns trusted run/mode/scope and composes `AgentResultService`, installed family owners, `FamilyPermissionDigest`, `AttemptGuard`, `NewFamilyToolDispatcher`, and a tool-enabled `submission.Broker`. Providers receive only immutable registration data, never service objects, store access, credentials or permission decisions.
- Use the actual-launch seam around `runSubmissionBound`, including every cold/resume/fallback call. Preserve current typed-helper callbacks and contracts. An MCP-only attempt still needs result binding when no managed submission session exists; it must not invent a managed feature/generation to obtain one.
- The tool catalog is projected from the same installed profile used by `ToolDispatcher`. A small read-only projection on the dispatcher/profile is appropriate; providers and prompts must not maintain independent mode catalogs. Preserve no-creation/target grants and current handler installation rules.
- Claude consumes a private attempt-local JSON file and explicit fixed-server MCP authorization; Codex consumes structured config overrides. Both invoke the same fixed executable/serve-arguments/descriptor. Host filtering supplements the broker's per-call authorization; neither discovery nor preapproval grants business permission.
- `SubmissionSession` remains the checkpoint owner for managed engineering/Validate. `EvidenceService` delegates through it; terminal reconciliation reads durable matching checkpoint records and avoids replaying their effects. Application-result receipts and submission checkpoint receipts retain their existing distinct dimensions.

### Data Flow

Runtime startup → trusted actual launch → result binding and current immutable service grants → installed profile and permission digest → attempt lease → dispatcher and private broker descriptor → neutral registration/catalog → provider config plus relevant guidance → host stdio MCP adapter → existing authenticated broker → guarded existing service → durable correlated receipt → same-attempt agent receipt observation → terminal compatibility reconciliation → attempt revocation and transport/file cleanup.

Enabled preparation failure stops that launch with a useful outcome; it must not silently continue with advertised but disconnected tools. Absent/disabled registration preserves the previous runner path. MCP completion observation does not replace final findings/artifacts or activate #264 scheduling/readiness changes.

### Concurrency & Context

Runtime authority may serve concurrent task attempts; revocation is per attempt, and runtime-wide close happens only at shutdown. Reconnects reuse the binding; actual replacement launches get fresh scope/resources. Context cancellation must revoke admissions promptly, cancel in-flight calls and join the broker's workers. File cleanup follows provider exit on every outcome. Do not hold SQLite transactions over process, pipe, broker or forge I/O. Preserve the single-connection pool. Use existing bounded cleanup conventions, propagate cleanup failures and keep clock/deadline/backoff ownership in the daemon/configured request context (`CONC-2`, `CONC-5`–`CONC-7`, `CONC-18`). Clone catalogs and nested request metadata at boundaries (`CONC-1`).

### Shared State and Side Effects

Only `config.Paths` derives paths below LF_HOME (`CFG-8`); provider temporary files must remain owned and private. Reuse the narrowly scoped submission directory admission rather than granting LF_HOME. Validate sandbox access to adapter transport is a separate compatibility question from allowing agent shell execution: do not solve it by enabling shell or broad writes. Keep forge grants with existing factory owners, no ambient token inheritance (`SEC-4`, `SEC-6`, `SEC-20`). Server naming/collision rules must preserve current user-config policy while refusing collisions that could replace litefactory's authorized registration.

### Cross-story decisions

1. One runtime authority; one scoped resource owner per actual launch, independent of managed-submission eligibility.
2. One authoritative installed catalog supplies both provider filters and prompt cards; registration is immutable and absent registration preserves behavior.
3. Provider registration supplements existing native ceilings. Claude retains `--tools` and `--strict-mcp-config`; explicit MCP preauthorization is a separate validated surface, never a substitute for the native ceiling (`SEC-3`). Codex retains its effective sandbox, user-config isolation and Validate shell/multi-agent disabling.
4. Dynamic attempt guidance is the delivery surface; no installed skill-tree changes are necessary. Checkpoint and application-result contracts remain distinct, and live-plus-terminal reconciliation stays in their current owner.
5. Automated checks are offline. Real working-mode plus Validate captures for both hosts are separately acquired by an authorized operator and reviewed against the implemented revision. Missing proof remains unsatisfied (`EXT-4`, `EXT-5`).

## Per-task structural hints

The identifiers below are architecture work-item references, not frozen story IDs or new requirements. The orchestrator can group them into the six requested delivery stories and map each to the feature-spec criteria. Test roles use the supplied roster, overriding the decomposer's generic single-role assumption.

| Work item | Type | Depends on | Est | Role |
|---|---|---|---|---|
| A1 Neutral registration/catalog contract | data | — | S | go-implementation-engineer |
| A2 Runtime authority lifecycle composition | logic | — | M | go-implementation-engineer |
| A3 Actual-launch service/broker bridge | logic | A1,A2 | L | go-implementation-engineer |
| A4 Contract/catalog unit coverage | test | A1,A3 | M | go-unit-test-engineer |
| A5 Claude registration | infra | A1 | M | go-implementation-engineer |
| A6 Claude offline coverage | test | A5 | M | go-unit-test-engineer |
| A7 Codex registration | infra | A1 | M | go-implementation-engineer |
| A8 Codex offline coverage | test | A7 | M | go-unit-test-engineer |
| A9 Conditional payload guidance | logic | A3 | M | go-implementation-engineer |
| A10 Prompt contract/size coverage | test | A9 | M | go-unit-test-engineer |
| A11 Live/terminal checkpoint reconciliation | logic | A3 | M | go-implementation-engineer |
| A12 Real local lifecycle/reconciliation coverage | test | A3,A5,A7,A11 | L | go-integration-test-engineer |
| A13 Aggregate executed checks | test | A4,A6,A8,A10,A12 | M | go-implementation-engineer |
| A14 Capture procedure and supplied evidence integration | logic | A5,A7,A9,A11 | M | go-implementation-engineer |
| A15 Offline authentic-capture replay and claims | test | A14 and external captures | M | go-integration-test-engineer |

### A1–A4: Neutral contract and minimum runtime bridge

**Agents:** implementation; unit-test engineer for A4.

- **Pattern/placement:** validating immutable `agent.MCPLaunchSpec`; startup Options wiring and `turn` composition. Read each package's doc specification before changing it (`STYLE-7`).
- **Interfaces:** existing `AttemptAuthority`, result service binding, `FamilyDispatcherOptions`, permission digest, guarded dispatcher, broker options and actual runner request. Small consumer seams only (`TEST-5`, `STYLE-3`).
- **Integration:** ensure the tool-enabled handler routes tools without redirecting legacy helper operations. Install only services whose real current scope is available; no fake production result resolver or permissive default grants.
- **Watch out:** nil managed sessions, current mode/snapshot context, aliases in catalog slices, invalid descriptor/attempt mismatches, startup authority reset races, and cleanup after partial setup. Keep disabled behavior distinguishable from enabled failure.
- **Not in scope:** #264 state machine, new tool families, creation authorization, schema or retries.

### A5–A6: Claude registration and offline checks

**Agents:** implementation; unit-test engineer for A6.

- **Pattern/placement:** provider-owned structured JSON and file lifecycle alongside `internal/harness/runner.go` argv assembly.
- **Interfaces:** neutral spec and existing `surface(req)` tool ceiling; derive only exact names for the fixed litefactory server.
- **Watch out:** current restrictive native config rejects MCP strings; do not widen the user-config grammar or replace native tools. Preserve explicit `--tools`, strict MCP isolation, allowed native rules, cold/resume parity and ambient `.mcp.json` exclusion. Cleanup private files on launch errors/cancellation as well as success. Exact version-dependent host interpretation requires later capture, not assumed parser semantics.
- **Tests:** inspect decoded JSON, argv, file permissions/lifetime, exact catalog and unrelated native tools with fake executable fixtures. No real Claude in tests (`EXT-1`).

### A7–A8: Codex registration and offline checks

**Agents:** implementation; unit-test engineer for A8.

- **Pattern/placement:** provider-owned structured config encoding in `internal/codex`, adjacent to `args`.
- **Interfaces:** command/args/minimal env/exact `enabled_tools` derived from neutral data; preserve placement before resume subcommand and stdin marker.
- **Watch out:** quoted/backslash/newline values, empty catalogs, server collision, ambient user-config policy and delivery strict config. Keep `features.shell_tool=false`, `features.multi_agent=false` and read-only Validate intact. Never add a shell wrapper for MCP or broaden broker root admission.
- **Tests:** decode overrides and inspect effective scoped config across cold/resume/fallback and disabled paths using fake providers.

### A9–A10: Conditional guidance

**Agents:** implementation; unit-test engineer for A10.

- **Pattern/placement:** small shared renderer at the existing `internal/harness` prompt/`turn` binding seam, reached by both providers after installed services are known.
- **Interfaces:** installed catalog plus trusted relevant payload/baseline context; use current application create/update and checkpoint contribution types as source of examples.
- **Watch out:** application Plan submission cannot publish a completed Plan through communication tools; preserve supplied typed Plan helper path. Include one applicable payload shape, not the current unconditional engineering boilerplate in every mode. Keep exact receipt names, request correlation, preflight repair, pending dimensions, lost-reply guidance and required final findings. Validate's sentence must refer to retained evidence only.
- **Tests:** per-mode/card eligibility, parseable example shapes, no hidden operation advertising, identical provider content when context matches, word/byte counts. Record actual token measurement availability without claiming measured savings from byte counts.
- **Not in scope:** protected `.claude`/`.codex` role changes or schema/business redesign.

### A11–A13: Compatibility reconciliation and aggregate checks

**Agents:** implementation; integration-test engineer for A12; implementation owns A13 execution/fixes.

- **Pattern/placement:** retain `SubmissionSession.consumeTerminalValidation` as compatibility owner; reconcile against existing submission/evidence durable records before applying terminal contributions.
- **Interfaces:** complete terminal contribution set, supplied immutable snapshot, request/attempt correlation and separately dimensioned authoritative receipts. Match identity and canonical content, not generated text or request IDs alone.
- **Watch out:** identical live/terminal findings must not double-apply; conflicting content must not silently overwrite; partial live coverage never excuses incomplete terminal findings. Lost replies/recovery consult recorded outcomes. Do not turn an input acknowledgment into acceptance/publication success (`CONC-3`, `CONC-10`, `CONC-11`, `SEC-21`).
- **Real I/O tests:** use temporary migrated SQLite, real local broker/descriptor and fake host runners at the actual production seam (`TEST-3`, `TEST-6`). Rendezvous on channels for concurrent isolation, cancellation, replacement and worker joining; no sleep/polling (`TEST-8`). Test unmanaged modes plus managed checkpoints and enabled/disabled/absent behavior. No build tags or `_Integration` naming.
- **Aggregate:** retain actual `make check` output on the final implemented snapshot; safe builds never target `dist`. No execution occurred during this architecture review.

### A14–A15: Authentic evidence handoff, replay and documentation

**Agents:** implementation engineer for repository procedure/artifacts; integration-test engineer for offline replay. The external operator is a prerequisite source, not an invented roster role or task that launches paid agents inside managed execution.

- **Pattern/placement:** extend `docs/agent-evidence-capture.md` conventions with a #274 assertion matrix and reviewed sanitized sources in the established fixture layout. Update `docs/story-contract.md` evidence statements only for established observations.
- **Interfaces:** capture provenance, host version/configuration, implemented revision, source hashes/counts and ordered attempt/request/call/result/exit identities. Preserve distinct receipt dimensions and effective inventory/refusal observations.
- **Watch out:** four scenario cells: Claude working mode, Codex working mode, Claude Validate and Codex Validate. Both working scenarios need actual permitted native use plus same-attempt diagnostic → repair → submit → matching decision before exit. Validate needs live allowed checkpoint/receipt behavior and observable shell/delegation unavailability or refusal, without fresh checks/edits in Validate. Host trace inventories or rejected harmless attempts may establish absence; simple non-use cannot.
- **Prerequisite status:** all authentic captures are currently unavailable. A14 can prepare concrete operator instructions now; A15 cannot claim delivery until supplied captures support the assertions. Offline replay supports only the recorded implementation/version/context, never blanket present/future compatibility.

## Architecture Notes

Platform: layout/interfaces — existing internal owners, consumer-sized seams, no new dependency or exported provider identifiers in `agent`.

Platform: errors/logging — use contextual wrapped errors for preparation/transport failures, durable outcome values for conducted operations and injected structured logs; do not print capability or descriptor contents (`CONC-3`, `STYLE-2`, `STYLE-5`).

Platform: concurrency/database — per-launch cancellation and joined broker workers; runtime-only authority close; context-first store access and existing one-connection SQLite with no transaction across external I/O.

Platform: HTTP/new service topology — not applicable; reuse the existing local broker and stdio adapter.

Platform: dependency management — no new module justified; preserve pinned dependencies and hand-written fakes (`TEST-2`).

Platform: testing/lint — stdlib table tests, real local I/O where claimed, race coverage through repository `make check`; no live provider in CI. Validate consumes retained output only.

### Security Findings

**Scope:** forward-looking integration boundaries only; not an audit or claim of existing runtime correctness.
**Rulebook:** `.aiteam/APP_CONSTRAINTS.md`.

| Area | Status | Finding |
|---|---|---|
| Input validation | Concern addressed by design | Validate immutable paths/catalog, serialize provider config structurally, and refuse malformed/mismatched registration before launch. |
| Authentication/authorization | Concern addressed by design | Bind one shared installed scope digest and check existing lease on discovery/calls/replay; provider filtering alone is insufficient. |
| Secrets/configuration | Concern addressed by design | Keep minimal environment and private descriptors; never log capabilities, inherit forge secrets or persist user-host configuration. |
| Data handling | Concern addressed by design | Reuse store ownership and sanitize external capture artifacts while preserving correlation and provenance. |
| Dependencies/supply chain | Compliant plan | No new dependency, adapter binary or download mechanism proposed; resolved factory executable is reused. |
| Error handling | Concern addressed by design | Surface enabled preparation failures and uncertain effect state without silently falling back or fabricating receipt success. |

## Deviations and Delivery Boundaries

No major deviation. Repository constraints take precedence over generic platform suggestions about new test dependencies, migrations, pools or test naming. Dynamic prompts avoid protected-tree maintenance scope. If actual integration proves broader #264 orchestration or forbidden Validate access is necessary, return that concrete blocker through Plan rather than widening this architecture.

The feature-spec evidence inventory AC-001–AC-011 remains authoritative for story decomposition. Carry each affected criterion's proof method, source availability, version/context bounds and missing prerequisite into its typed story. AC-009–AC-011 are not satisfied by this architecture, synthetic tests, configuration or a receipt acknowledging this Plan. Owner questions: none required for Plan.
