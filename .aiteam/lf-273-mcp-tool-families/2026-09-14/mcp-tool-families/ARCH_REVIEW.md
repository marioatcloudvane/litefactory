# Architect: Go Architect
# Created: 2026-09-14
# Verdict: READY FOR STORY DECOMPOSITION — no major deviation

## System Overview

### How This Feature Fits

This adds in-process Go responsibilities to the existing `submission → turn → store/github` path. It does not add deployed services or a transport. `internal/submission` continues to own strict tool transport, catalog and local correlation; `internal/turn` owns factory-bound authorization and orchestration; `internal/store` owns SQL and durable transitions; `internal/github` owns authenticated forge I/O. The current `ToolDispatcher` only dispatches `communication_*`; `ToolProfile.Allows` also explicitly limits effects to read/input, so adding names alone cannot enable publication. Both catalog and enforcement must deliberately change.

Current source confirms the research boundary: `CreationAuthorizer` already exposes proposal retention, trusted approval admission, grant binding, reservation, private dispatch, and outcome/reconciliation operations. `CreationOperation` currently stores only aggregate state, not artifact identities. `SubmissionSession.handle` already performs frozen-scope validation, snapshot verification, allocation, engineering and assessment recording under workflow locks. `submission.Call` currently journals only communication submission and maps its arguments to the existing helper identity. Those are reuse seams, not alternative engines to rebuild.

### New Packages / Components

No new package is necessary. Suggested responsibility names are conceptual, not mandatory exported types:

| Component | Placement | Responsibility |
|---|---|---|
| Family binding/catalog | `internal/submission/tools.go`, `internal/turn/mcp_tools.go` | Enumerate the exact supported family/profile intersection and bind all immutable permissions into the lease digest. |
| Family request ledger | `internal/store`, new embedded migration | Record scoped request identity, bounded disposition and references to authoritative workflow/publication records. |
| Feedback service | `internal/turn` | Route validation kinds to existing application, checkpoint and canonical creation validators without domain mutation. |
| Issue query service | `internal/turn`, `internal/github/issues.go` | Perform bounded repository-local reads and project only number/title/recognized mode labels. |
| Issue publication service | `internal/turn`, `internal/store` | Persist ordinary comment intent and per-artifact creation results; coordinate dispatch and observation without duplicating creation authorization. |
| Creation preparation | `internal/turn`, pure rendering in `internal/report` where needed | Produce exact final redacted bytes, correlation markers and relationships before canonical hashing/retention. |
| Evidence facade | `internal/turn/submissions.go` | Expose reusable checkpoint operations shared by the existing helper and MCP. |
| Activity service | `internal/turn`, existing `internal/agentresult/progress.go` | Synchronize reducer-owned advisory display state with factory-provided time/lifecycle. |
| Receipt projection | `internal/turn` | Authorize current reads and combine ledger references with underlying authoritative receipt states. |

### Key Boundaries

- Dispatcher consumes narrow, consumer-owned interfaces or concrete validating options; transport contains no SQL, forge credentials, grant minting or workflow policy (`TEST-5`, `SEC-20`, `STYLE-3`). A family options/binding aggregate can coexist with the existing communication constructor to retain compatibility.
- Include explicit allowed comment targets, approved historical receipt-read scope, catalog version and creation-gate visibility in immutable permission material. Do not hash mutable grant expiry or operation state; those remain freshly checked by their owner. Missing dependencies or an inconsistent profile fail construction. A denied direct call must be denied server-side even if the caller ignores discovery.
- Repository and source task are factory values. Reads do not add comment targets. Historical receipt access uses an explicit current reader capability; it does not reactivate the old lease.
- Factor checkpoint preflight/submit/receipt operations out of `SubmissionSession.handle` only as needed so both adapters use the same operation path and frozen scope. Do not start another broker or call the terminal helper from MCP. Preserve the communication context restriction on publishable Plan content while allowing explicitly permitted evidence operations to reach their own existing workflow engine.
- Keep `CreationAuthorizer`/`CreationStore` authoritative for grant provenance, quota, expiry, deduplication and single-use dispatch. Extend publication detail beside their operation ID; do not make the generic ledger a second permit authority.
- Production construction of these scopes, trusted approval intake/next-attempt binding, lifecycle cleanup scheduling and host startup remain #274. #273 provides callable APIs and controlled fixture composition only.

### Data Flow

Read/preflight: bound broker → live guard/profile → scoped request disposition → shared validator or one bounded forge page → bounded projected result. Receipt recording is transport bookkeeping, not evidence or domain publication. A receipt lookup does not recursively allocate another receipt; it observes a referenced request.

Comment: exact request bytes → immutable target/content validation → redaction and final marker preparation → durable intent and first-dispatch ownership → one POST → durable confirmed comment ID or uncertain state. Recovery observes exact target, numeric bot provenance, marker and expected final content. An empty/incomplete scan after ambiguous dispatch is not permission to repost.

Creation: factory-only preparation → final render/redaction/markers/relationships → canonical envelope and retained hash → separately supplied trusted approval and bound attempt grant → `ReserveCreation` → private first-dispatch permit → `BeginDispatch` → reviewed artifacts and explicitly approved relationships → per-artifact outcomes and aggregate reconciliation. Generate marker correlation before hashing; do not derive a marker from a hash that includes that same marker. Exact prepared bytes can be checked/redacted idempotently at publication, but any changed result requires a new proposal and approval. No unreviewed post-approval links or markers are appended.

Evidence: guard/profile → shared checkpoint preflight/submit → existing submission receipt and engineering/assessment transitions → generalized reference/projection. Recovering a missing ledger link uses the same underlying stable request identity; it must not allocate fresh proof or duplicate effects.

Activity: guard/profile → serialized existing reducer with injected factory time → bounded durable disposition and advisory current view. A durable accepted update receipt does not promise that process-local display survived restart.

### Concurrency & Context

Use context-first calls and existing attempt cancellation. No new permanent goroutines are needed. Keep process-local advisory state behind a mutex and clone mutable slices/maps across boundaries (`CONC-1`, `CONC-2`, `CONC-5`). Never hold a SQLite transaction across forge I/O; retain the single connection pool and finish queries before invoking another accessor. Cross-process durable dispatch reservation is necessary for comments; an in-memory dispatcher mutex alone is insufficient. Reuse parent-effect coordination where publication touches managed parent relationships (`CONC-10`). Check cancellation and authority before new outward effects, and stop dispatch after a failed/unknown outcome. A separately authorized factory observer may retain already-observed facts after caller expiry but cannot become a new dispatcher.

Recovery APIs perform one bounded observation pass and return honest unresolved state; daemon-owned scheduling/backoff stays in #274. Do not implement sleeping retry loops in turn/github (`CONC-6`). Tests rendezvous via channels, not sleeps (`TEST-8`).

### Shared State and Side Effects

The generic ledger owns correlation and disposition, while submission and creation stores retain authoritative decisions. References avoid copying mutable engineering/acceptance truth into competing state machines. New migration work must cover comment dispatch state and creation artifact/relationship details as well as ledger references. Partial creation consumes the original reservation and retains its existing charges. Confirmed artifact identity and relationship completion are separate facts; full-batch success requires every approved effect confirmed.

A page of GitHub issues may contain only PRs. Return a bounded filtered page and next-page metadata instead of chasing enough pages to fill a requested issue count. `ListIssues` is daemon-oriented and accumulates pages; add a deliberately bounded page operation and update the pinned forge surface (`TEST-10`). Existing `GetIssue` and `PostComment`/`CreateIssue` remain useful; authenticated recovery needs full internal forge representations while the agent-facing read projection remains minimal.

## Per-task structural hints

The spec supplies criteria rather than task IDs. The following hint keys identify already-required work for the orchestrator to map onto final typed tasks; they do not add stories or obligations.

### H-01: Durable correlation and receipt projection (AC-018–020)
**Agent:** go-implementation-engineer

- **Pattern / placement:** Extend the existing private journal in `internal/submission/client.go`; ledger migration/accessors belong in `internal/store`; scoped projection belongs in `internal/turn`.
- **Key interfaces:** Existing `submission.Call`, result/submission receipts and `CreationOperation` references.
- **Integration points:** Foundation consumed by all families; preserve byte-identical communication/helper journal behavior. New identities namespace method and bound scope, with exact payload bytes retained as identity input.
- **Watch out for:** A client ID is not authority. Dispositions for denied/malformed authenticated calls must be distinguishable from transport failures with no durable record. Keep bounded read/preflight metadata, not full issue snapshots. Cross-family ID collisions and historical scope require explicit tests.
- **Not in scope:** Expanding directory permissions, a new public receipt service, or replaying effects during lookup.

### H-02: Profile, dispatcher and shared feedback (AC-001–003)
**Agent:** go-implementation-engineer

- **Pattern / placement:** Extend `submission.ToolProfile` and `turn.ToolDispatcher`; feedback uses `AgentResultService.Preflight`, shared checkpoint operations and `submission.CanonicalCreation`.
- **Key interfaces:** `AttemptGuard`, immutable result binding/permission digest and family dependencies.
- **Integration points:** Register handlers as their implementations become available; final complete catalog must match direct-call enforcement and lease digest. Research creation is gate-visible but requires fresh per-call grant checks.
- **Watch out for:** Existing profile effect filtering refuses writes. Shared stage validation includes live snapshot/current-engineering checks beyond JSON parsing; preflight must share those checks without capture. Plan/Spike retain existing communication behavior only.
- **Not in scope:** New host permissions or adapter-local business validators.

### H-03: Bounded issue discovery (AC-004–005)
**Agent:** go-implementation-engineer

- **Pattern / placement:** Consumer interface in `internal/turn`; bounded page accessor in `internal/github/issues.go`.
- **Key interfaces:** Bound repository, normalized query and minimal issue view; preserve richer internal representations for factory use.
- **Watch out for:** PR-only pages, state mismatch on get, page overflow, arbitrary labels and unknown fields. Update the forge surface intentionally (`SEC-20`, `TEST-10`).
- **Not in scope:** Reusing the unbounded daemon listing loop or granting write targets from read results.

### H-04: Receipted comment publication (AC-006–008)
**Agent:** go-implementation-engineer

- **Pattern / placement:** `internal/turn` orchestrates store intent, redaction and existing `github.PostComment`; SQL remains in store.
- **Key interfaces:** Explicit immutable comment-target grant, final prepared comment and numeric confirmed ID.
- **Concurrency/context:** Durable first-dispatch election precedes I/O. After ambiguous POST, only observation/reconciliation is permitted; no second POST from a retry or restarted process.
- **Watch out for:** Creation gate must not block comments. Marker matches without numeric bot provenance, exact target and content are insufficient (`SEC-6`, `SEC-8`, `CONC-10`). Bound reconciliation work; uncertainty is a valid result (`CONC-3`).
- **Not in scope:** Editing existing comments, arbitrary repository targeting or periodic recovery workers.

### H-05: Exact prepared and authorized creation (AC-009–013)
**Agent:** go-implementation-engineer

- **Pattern / placement:** Preparation and publication in `internal/turn`; pure rendering in report as needed; extend store publication detail around the existing creation operation.
- **Key interfaces:** `CanonicalCreation`, `RetainProposal`, `ReserveCreation`, `BeginDispatch`, `RecordOperationOutcome`, `ReconcileOperation`; existing forge creation and explicit relationship operations.
- **Integration points:** H-01 correlation points to the existing operation, never another grant. Preparation must retain reviewable final bytes before trusted approval.
- **Watch out for:** Hash/marker circularity, redaction changes after review, crash between confirmed issue creation and relationship recording, and restart while a one-use permit has been consumed. Preserve unresolved state; reconciliation cannot recreate ambiguous artifacts or mint a replacement permit. Numeric provenance and exact approved relationships remain mandatory.
- **Not in scope:** Owner-command ingestion, next-attempt production binding, grant exposure or authorization reimplementation.

### H-06: Shared evidence checkpoint facade (AC-014–015)
**Agent:** go-implementation-engineer

- **Pattern / placement:** Extract reusable operation seams within `internal/turn/submissions.go` while retaining the existing helper behavior.
- **Key interfaces:** Shared `agent.Contribution` validation, `SubmissionSession` scope/snapshot checks, `StoryProgress` and existing store receipts.
- **Concurrency/context:** Preserve existing mutex/parent ownership order and recovery ordering; avoid nested acquisition of the same locks.
- **Watch out for:** Input validity is not engineering or assessment success. Validate cannot capture; engineering remains separate from acceptance, and legacy rules remain version-gated (`SEC-21`, `CONC-11`). Existing historical snapshot decisions stay intact.
- **Not in scope:** A new checkpoint schema, new test execution tool or mode chaining.

### H-07: Advisory activity facade (AC-016–017)
**Agent:** go-implementation-engineer

- **Pattern / placement:** Thin synchronized wrapper in `internal/turn` over `agentresult.ReduceProgress`, `MarkProgressTerminal`, `CleanTerminalProgress`.
- **Key interfaces:** Factory-provided binding, clock value and terminal lifecycle input; bounded current view and disposition.
- **Watch out for:** Identical request recovery must not reapply updates or extend deadlines. Durable disposition and unavailable process-local display can coexist. Do not add wall-clock reads in turn.
- **Not in scope:** Durable activity history, readiness/evidence mutation, or production timer wiring.

### H-08: Family unit proofs (all applicable criteria)
**Agent:** go-unit-test-engineer

- **Pattern / placement:** Existing package tests with handwritten fakes; shared validation parity, complete profile refusal matrix, projection, reducer behavior and authority non-effects (`TEST-1`, `TEST-2`, `TEST-7`).
- **Integration points:** Each final story should include its own meaningful acceptance-derived unit task rather than depend on an undifferentiated feature-wide test task.
- **Watch out for:** Gate-on does not mean granted; permitted ordinary comments remain independent. Include malformed tokens/prose/spam as non-authority inputs, stale/current-reader distinctions and denied direct calls.
- **Not in scope:** Fake-store assertions as proof of transactions or claims of authentic provider behavior.

### H-09: Durable and transport fixture proofs; contracts/handoff (AC-007–008, AC-012–015, AC-018–021)
**Agent:** go-integration-test-engineer for real local I/O tests; go-implementation-engineer for documentation

- **Pattern / placement:** Normal Go tests using migrated SQLite, actual broker/pipe/socket and HTTP fixtures. Extend existing fixtures rather than introduce build tags or provider subprocesses (`TEST-3`, `TEST-6`, `EXT-4`, `EXT-5`).
- **Concurrency/context:** Channel rendezvous for concurrent duplicate calls, cancelled requests and accepted-POST/lost-response points; reopen durable state to verify restart semantics.
- **Watch out for:** Verify counted POSTs, retained identities, partial relationship states, cross-scope denial and no fresh dispatch during recovery. Split large fixture tasks by family in final decomposition. Execute `make check` at the feature gate; no `make build` or `dist` writes.
- **Not in scope:** Live GitHub publication, native host launches, paid nested agents or authentic-host certification. Documentation must name production integration/approval/recovery seams handed to #274.

## Platform Architecture Notes

- Platform: Layout and interfaces — use the existing internal packages and consumer-owned narrow seams; no new shared/public library or dependency is required.
- Platform: Error handling — expected refusal, uncertainty and pending work are structured outcomes; infrastructure failures preserve typed causes. No new panic recovery (`CONC-3`, `SEC-11`, `STYLE-5`).
- Platform: Database — embedded migration only, modernc SQLite, one connection, context-first accessors, bounded rows and short transactions. Generic pool/migration-library advice does not override project rules.
- Platform: HTTP — existing forge client and transport only; no public listener, new router, middleware framework or process topology.
- Platform: Logging/config — injected logger and sanitized bounded caller diagnostics; no new secrets, gate configuration or logging stack (`SEC-6`, `CFG-4`, `STYLE-2`).
- Platform: Testing/lint — repository `make check` and pinned tooling govern verification, with real SQLite for transaction promises and no new assertion/mocking dependencies.

### Security Findings

**Scope:** Forward-looking family design, not an audit or claim of implemented compliance.
**Rulebook:** `.aiteam/APP_CONSTRAINTS.md`, especially SEC-6/8/20/21 and CONC-10/11.

| Area | Status | Finding |
|---|---|---|
| Input validation | Concern addressed by design | Strict transport decoding must share business validators; final redacted creation bytes must precede approval hashing. |
| Authorization | Concern addressed by design | Catalog enforcement, live guard and immutable target/read scopes precede handler access; creation uses the existing private permit only. |
| Secrets/configuration | Compliant design | No new credentials or caller tokens; permit and descriptor capability stay out of receipts/diagnostics. |
| Data handling | Concern addressed by design | Minimal query projections, bounded read dispositions and store-owned parameterized SQL prevent broad data exposure. |
| Dependencies | Compliant design | Existing pinned dependencies suffice; no new supply-chain surface proposed. |
| Error handling | Concern addressed by design | Unknown external outcomes remain uncertain; mismatched marker/content/provenance cannot establish publication. |

## Deviations and gaps

No major deviation. Settled server-held authorization and #274 production boundaries are honored. The task-decomposer's generic single-role testing advice yields to the supplied implementation/test roster and role scopes. No implementation, tests or authentic host observations were performed in this architecture review.
