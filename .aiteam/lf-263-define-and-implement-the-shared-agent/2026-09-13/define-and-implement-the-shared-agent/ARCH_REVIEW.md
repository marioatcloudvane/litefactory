# Architect: Go Architect
# Created: 2026-09-13

Verdict: READY. No standard deviation or owner decision is required. This is an additive library delivery with offline exercised persistence, not production routing adoption.

## System Overview

### How This Feature Fits

Introduce `internal/agentresult` as the reusable application contract: typed values, strict decoding, semantic validation, stable-ID patching, scoped projections and advisory progress reduction. It may import `internal/agent` and `internal/planbrief` for existing neutral evidence and completed Plan semantics; neither existing package imports it. Keep `internal/store` independent of the new contract package by persisting immutable encoded snapshots and explicitly typed storage binding/decision values. `internal/turn` composes trusted scope, retained evidence and storage through a new, directly callable result service. This service is exercised by tests but is not wired into runner admission, the helper broker, daemon dispatch or publication in this issue.

The shared library is internal to this Go module: future application adapters can reuse it here. No current external Go-module consumer justifies a new module or public `pkg/` API.

Existing owners remain authoritative: `planbrief` for completed typed Plan obligations, `agent` for contribution diagnostics and evidence interpretation, `submission` for local transport/correlation, `store` for transactions/retention, `turn` for frozen scope and decisions. Source files inspected include `internal/agent/contribution.go`, `contribution_capture.go`, `internal/submission/{doc,protocol,broker}.go`, `internal/store/{store,submission_receipts,engineering}.go` and the submission entrypoint inventory under `internal/turn`.

### Components and boundaries

| Component | Responsibility | Crosses boundary with |
|---|---|---|
| `internal/agentresult` | Application schema v1 and pure result/update/projection/progress validation | Trusted value context supplied separately from authored JSON; no SQL, files, clock, runner or forge |
| `internal/planbrief` | Existing complete-story validation plus reusable draft-field checks where needed | Typed values/diagnostics; preserve current complete contract and legacy decoding |
| `internal/agent` | Existing retained proof, captures and contribution findings | Reuse existing validation through conversion or narrow extracted helpers, preserving old entrypoints |
| `internal/store/result_*.go` and additive migration | Continuing result identity, active writer, full revisions, request deduplication, receipt observations, aggregate evidence associations | Context-first accessors and atomic compare-and-record; no application import needed |
| `internal/turn/agent_results*.go` | In-process application service assembling authorization/evidence, preflight and submit orchestration | Calls pure library and storage; no live call-site adoption |
| `teams/source/skills/agent-finalization/` | Early discovery instructions and detailed per-mode references/examples | Source-only catalog discovery and isolated rendering; no installation or release |
| `docs/agent-result-contract.md` and versioned schema/fixtures | Normative wire semantics, examples, edit matrix and reproducible size comparison | Same fixture corpus consumed by Go tests and source guidance |

### Contract and editing decisions

The application schema is integer 1. `result_revision` starts at 1 and increments only for accepted creates/updates; it is independent of schema, existing submission contract 2 and workflow 1/2. Trusted context selects versions. An assembled snapshot includes `meta` and `result`; authored create data includes only `result`. Agent requests cannot inject `meta`, workflow decisions or execution telemetry. Application updates carry only `expected_revision` and ordered `operations`; transport assigns request correlation and bound logical result. Read-only snapshots are data, never authority.

Use explicit operation names `set`, `remove`, `add_item`, `update_item`, `remove_item`, `reorder`. Targets are typed paths: a sequence of allowlisted object field names and stable-ID selectors, never JSON Pointer execution or array indexes. Every collection definition identifies its key (`id`, Plan `key`, Implement/Validate `story`, criterion ID or existing review name). Nested criteria/tasks require their parent story selector; keys have collection-local identity rather than ambiguous global lookup.

- `set` changes an allowed field. Supplied object members merge recursively, with omitted members preserved. Scalars replace. Arrays explicitly replace that editable collection and undergo identity/retirement validation; `[]` clears it only when the final contract permits emptiness.
- `remove` deletes an existing optional editable field; it does not mean set-null. Removing a required field or absent target rejects.
- `add_item` appends one new item; its stable key must never have been used/retired in that result's relevant collection. A later `reorder` places it elsewhere.
- `update_item` merges supplied non-key fields of an existing selected item. A supplied changed identity rejects; identity changes use removal and a genuinely new key.
- `remove_item` deletes the selected editable item and records its key as retired for that logical result. Frozen/published requirement IDs additionally obey the existing generation retirement rules.
- `reorder` supplies exactly the current keys once each for the selected collection. Missing/extra/duplicate keys reject. No implicit sorting or numeric positions.
- JSON null rejects everywhere in authored input. Creation omission means absent; update omission means unchanged. Unknown operation fields and conflicting selector/value identities reject.

Operations run sequentially against a private copy. Structural/target validity is checked at each operation; complete semantic validation runs on the final result so coordinated dependency edits can be atomic. Removing a referenced item without fixing its dependents rejects. Any invalid operation or merged result discards all mutations, including retirement changes. Duplicate/retired identities are checked across full collection replacements as well as item operations. Limit operation count and decoded depth/bytes with named constants; initially reuse the existing contribution byte ceiling and depth 32, and cap one update at 256 operations. Errors identify operation index, typed target and diagnostic code; stale errors include expected/current revision without exposing unauthorized items.

The allowed-edit matrix is explicit per mode. Common authored narrative fields remain editable. Plan requirements can evolve only in authorized planning context; completed Plan invokes existing full typed obligations. Draft Plan can omit later sections, but supplied dependencies/criteria/task references must be valid. Frozen story requirements, retained snapshots/check facts, owner answers, acceptance and publication fields are never patch targets. Implement can change its own outcomes/evidence references within trusted scope. Validate can revise authored assessments in a new result revision, never overwrite the original retained assessment. Research and Spike edits retain prior full snapshots.

Completed Validate means the assessment is complete; it may contain unmet/unverified findings. Separated Implement cannot include an acceptance assessment. Legacy verified semantics remain distinct. A blocked or incomplete exit must not demand invented successful engineering proof. All five modes get concrete completed, incomplete and blocked fixtures; questions are tested only where trusted permission allows them.

### Data Flow and persistence

Create: trusted factory context → strict decode and semantic validation → prepared candidate → store atomic authorization check, request deduplication, identity allocation and revision 1 append → receipt with input accepted and other decisions independently pending/not applicable.

Update: authorized scoped baseline read → authored operations with expected revision → trusted evidence/context assembly → private-copy patch and complete validation → store transaction checks current writer, scope and expected revision → append full snapshot, retired-key changes and request/receipt observation atomically. A compare failure records no revision. Do not call ordinary DB accessors from inside the transaction: the single connection pool would deadlock. Pass prepared values into a transaction-owning accessor, and use private transaction helpers inside store.

Keep a logical result bound to task, mode and a factory work-context identity. Fresh same-context runs replace the active writer; a different mode or approved frozen-scope identity starts a new logical result. Plan's mutable draft revision is not a frozen-scope change; normal Plan rounds continue the result. Attempts can read authorized historic receipts after revocation but cannot allocate/apply/decide again. Failed attempts do not destroy prior accepted revisions. Snapshot metadata records each revision's originating run/attempt while the logical ID stays constant.

The existing `BindSubmissionAttempt` is intentionally managed-only: it requires a managed generation, submission v2 and compatible stage. The existing broker likewise checks Plan/story selectors. Do not call these with fabricated managed scope for Research/Spike, or broaden live broker dispatch. Add a distinct application-result binding path in store for all five modes, sharing existing private request-hash, idempotency and receipt-state primitives where appropriate. Extend/factor transaction internals deliberately; existing public v2 paths retain their current behavior. Snapshot-to-v2-receipt association is optional and factory-owned, allowing future managed adapters to associate real receipts without pretending every Research result is a story event.

Retain immutable request bytes/hash, result revisions and append-only receipt observations; a mutable latest receipt projection may coexist. Exact request retry under its original authorized correlation returns the original application/receipt even if the result now has a newer revision. Reusing that correlation with changed bytes rejects. Corrected bytes get new correlation. Preflight performs the same discoverable checks, including evidence reads, but allocates no request, accepted proof or revision; submit rechecks. Received/input-accepted means retained valid application input only. Engineering, acceptance, Plan publication, remote effects and terminal save remain separate factory dimensions. A new application result must not enter existing pending-submission recovery queues as a v2 event by accident.

### Aggregate evidence binding

Retain aggregate evidence as a first-class association owned by store, not as a fake story check. It binds parent/work context, exact selected frozen story key/revision set, final implementation snapshot/digest, source execution/check IDs and executed/passed facts. Existing deterministic file capture and manifest hashing are reused; a new aggregate association links immutable bytes/check records to the exact set. Story keys alone are insufficient: revision digest and selected-set equality are checked. An arbitrary authored `passed` field or reused story-only check cannot establish aggregate proof.

The result service obtains trusted retained facts and compares the final snapshot with the authorized worktree through the existing turn/git evidence boundary when admitting new evidence. Capture helpers serialize bytes and factual execution claims; they do not run tests or certify provider provenance. Validate accepts references only and cannot capture/execute. Aggregate-only recovery can add its own final snapshot/check association while retaining earlier story checkpoints, without allocating new engineering events for those stories. No new acceptance or closure path is introduced. Final-result completion validation requires the selected engineering obligations and required aggregate proof; terminal save remains an additional factory gate.

### Progress policy

Advisory progress is a separate typed value/reducer, not `agent.StoryProgress` and not a generic event bus. Factory binds run/attempt; the agent supplies positive sequence and nonblank message. Sequence is the dedup key within an attempt. Exact replay in the retained window is a no-op; same sequence/different content rejects; an older sequence outside the retained window is ignored as stale and cannot replace latest progress. Gaps are allowed. New attempts reset sequence only with fresh factory authority; old writers reject.

Named v1 limits: 2,048 UTF-8 bytes per message; last 32 accepted sequence/hash identities retained per active attempt; at most one latest visible message per attempt; maximum 1,024 distinct accepted updates per attempt, after which a bounded limit diagnostic refuses new content. This bounds dedup memory as well as rendered progress. Coalesce the visible message to the highest accepted sequence; keep no unbounded history. Caller supplies factory time; retain terminal-attempt progress for at most 24 hours or until explicit cleanup, whichever is earlier. Cleanup is caller-driven, no timer/goroutine. Default delivery is a process-local reducer, so restart may discard advisory progress; that loss is explicitly acceptable. Durable result receipts are unaffected. Every update returns attribution/sequence disposition; none grants completion, readiness, engineering, acceptance, publication or a result revision.

### Concurrency, state and security

No new goroutines, retry clocks, server or background cleanup. Context-first I/O, deep-copy values at boundaries and deterministic diagnostic ordering follow CONC-1/2/5/6 and TEST-7. SQLite remains one connection; migrations use the next available numeric migration and existing transaction machinery. Real SQLite tests prove compare-and-swap, competing writers, rollback, reopen and dedup (TEST-6). No transaction spans filesystem, runner, output or forge I/O (CONC-10).

Artifact references validate relative rooted syntax in the pure contract; content resolution retains symlink refusal and stable reads under SEC-21. Untrusted references cannot choose capabilities or arbitrary roots. Diagnostics return target/type/correction, never capability strings or raw secret values. Keep schema bounds before allocation and duplicate-key rejection before typed JSON decoding can hide fields. Recommendations are advisory and the initial allowlist remains `action.moveto.planmode`; factory policy applies CONC-21/23 independently.

## Per-task structural hints

Task labels below name architectural work areas for the orchestrator to map to final story/task IDs; they are not additional scope or a replacement story list.

### Contract and create validation

**Agent:** go-implementation-engineer; separate fixture task: go-unit-test-engineer.

- **Placement/pattern:** `internal/agentresult`, normative docs and versioned schema. Reuse existing `agent.ContributionDiagnostics` shape; extract only needed pure helpers without changing legacy behavior.
- **Interfaces:** authored result, trusted context, assembled snapshot and diagnostics are separate structs. Reuse `planbrief` complete validation; use presence-aware draft fields to distinguish missing from zero/empty.
- **Watch out:** JSON Schema validates structural shape; Go enforces scope/evidence/graph invariants. Keep these layers consistent without adding a schema-library dependency or claiming schema alone proves semantics (TEST-2, STYLE-9).
- **Verification:** executable valid/invalid corpus for all five modes and three outcomes, disallowed questions, duplicate keys, null, depth/size, partial diagnostic coverage and completed Plan requirements.
- **Not in scope:** external APIs, live agent prompts or automatic upgrade.

### Sparse update and scoped read

**Agent:** go-implementation-engineer; separate behavioral tests: go-unit-test-engineer.

- **Placement:** pure patch/projection files in `internal/agentresult`, building on contract values.
- **Interfaces:** typed stable-ID paths and selection allowlists; projections always carry the same full-result revision and enough authorized identity/evidence references for deliberate retries.
- **Watch out:** read projections are never full mutable snapshots, and whole-array replacement must obey retirement. Draft criteria/architecture enrichment preserves untouched siblings. Deep copies prevent caller aliasing (CONC-1).
- **Verification:** all-mode update round trips, remove/reorder/null/clear, immutable field attempts, graph-invalid merged results, retired-key reuse and stale target diagnostics.

### Durable history and receipt integration

**Agent:** go-implementation-engineer; transaction tests: go-integration-test-engineer.

- **Placement:** store-owned additive migration/accessors, plus direct in-process `internal/turn` result service. Reuse v2 semantics without forcing Research/Spike into managed submission admission.
- **Interfaces:** trusted binding, revision compare, immutable candidate bytes, request hash/correlation, and independent decision observation. Storage accessors own complete transaction boundaries.
- **Watch out:** changing active writer and checking authority must be atomic with append; scope change never mutates old result identity. No current submission route, recovery queue, Plan readiness or forge publication is switched.
- **Verification:** real temporary migrated SQLite, close/reopen, two competing updates, revoked attempt, retry after lost response, changed-byte correlation collision, preflight no writes, and unchanged historic report/proof bytes. No integration build tags (TEST-6).

### Evidence and aggregate completion

**Agent:** go-implementation-engineer; pure attribution tests: go-unit-test-engineer; retained-binding tests: go-integration-test-engineer.

- **Placement:** reuse `agent` capture/evidence helpers, store aggregate association, turn's worktree verification boundary. Keep criterion and review semantics in existing owners.
- **Interfaces:** immutable final snapshot, selected key/revision set and attributable checks; reference-only Validate.
- **Watch out:** earlier scoped story snapshots remain valid history; aggregate-only recovery must not re-emit their engineering. Completed Validate can report unmet obligations without acceptance (SEC-21, CONC-11).
- **Verification:** actual deterministic local file bytes/digests and executed test-fixture evidence; reject stale digest, failed/missing check, wrong revision/set and story-check relabelling. Snapshot comparison uses existing local git fixtures where needed.

### Advisory progress

**Agent:** go-implementation-engineer; deterministic policy tests: go-unit-test-engineer.

- **Placement:** pure `internal/agentresult` reducer with supplied binding/time and bounded owned state.
- **Interfaces:** progress input, state and disposition; no result-store or workflow-decision writer interface.
- **Watch out:** reset only on new trusted attempt; eviction makes older sequence unverifiable and therefore stale, never accepted again. Terminal cleanup is caller-driven.
- **Verification:** exact numeric boundaries, UTF-8 byte length, replay/conflict/gap/order, expiry, count limit, coalescing and unchanged authoritative state. No sleeps (TEST-8).

### Finalization source and cost evidence

**Agent:** go-implementation-engineer; source/fixture tests: go-unit-test-engineer.

- **Placement:** new shared source skill and per-mode reference files; document standalone discovery through existing catalog mechanisms without adding it to live role dependencies. Early source guidance explains when/how to discover detailed guidance and mandates finalization on blocked/questioning exits; required work context and evidence obligations are explicitly available before work.
- **Integration:** isolated capture/render of the new skill with a temporary role fixture proves package discovery/serialization references. Existing active roles, installed trees and immutable releases remain unchanged. No protected-tree sibling stories are needed here; later adoption owns that handoff.
- **Verification:** examples are executable corpus inputs, not copied untested JSON. Fixture/source cross-checks cover each exit. Record an offline comparison over the same representative cold, continuation, stale-retry, incomplete and blocked tasks: input instructions/baselines/responses/diagnostics plus output contributions and retries. Report exact UTF-8 bytes and method; any token estimate names its tokenizer/model/cache assumptions and is clearly an estimate. No new tokenizer dependency is required, and bytes must not be labelled billed tokens.
- **Not in scope:** actual late-loading model behavior, paid nested agents, billed-token savings, production installation, MCP or catalog release (EXT-4/5).

## Planning integration notes

Natural independently testable boundaries are contract/create, patch/projection, persistence/correlation, evidence/aggregate, advisory progress, and finalization/cost evidence. The orchestrator owns the story split and dependencies. Keep tests as explicit rostered tasks and size implementation tasks within one day; split schema/common types from mode validation and persistence schema from service orchestration if needed. Pure contract precedes patch; persistence composes contract/patch; aggregate depends on contract and persistence; progress depends only on common contract; finalization examples depend on stabilized contract/patch/evidence semantics. Final aggregate gate runs repository `make check` over final implementation; no checks were executed by this architecture review.

Platform: internal package placement, consumer-defined interfaces only when needed, stdlib error/JSON/concurrency conventions, context-first SQLite I/O, existing migrations and unchanged dependency set apply. No HTTP/long-running-service change. Project constraints override generic platform suggestions about extra modules, migration libraries, panic recovery, pool tuning or assertion frameworks.

### Security Findings

Scope: forward-looking architecture for this feature only. Rulebook: `.aiteam/APP_CONSTRAINTS.md`.

| Area | Status | Finding |
|---|---|---|
| Input validation | Compliant by design | Bounded strict decode, duplicate/null/unknown rejection, full merged semantic validation and allowlisted targets |
| Authorization | Concern addressed | Pure context is insufficient alone; store rechecks current writer, result scope and revision atomically |
| Secrets/configuration | Compliant by design | No new secret or LF_HOME path; no descriptor capability enters results/diagnostics |
| Data handling | Concern addressed | Immutable bytes and append-only observations; rooted artifact resolution; all SQL remains store-owned |
| Dependencies | Compliant by design | No new module required; this is not a dependency vulnerability audit |
| Error handling | Compliant by design | Structured rejection diagnostics, separate inability-to-record errors, no silent partial write or pending-as-success |

No DEVIATION. Production adoption and authentic agent behavior remain deferred exactly as scoped.
