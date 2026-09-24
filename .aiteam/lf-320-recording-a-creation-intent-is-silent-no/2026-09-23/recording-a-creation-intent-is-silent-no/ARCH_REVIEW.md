# Architect: Go Architect
# Created: 2026-09-23

## Verdict

APPROVED FOR PLANNING. The feedback-only specification fits existing creation authority and durable report delivery. No standard deviation or unresolved product decision is required. This review proposes structure, not implementation verification.

Citation base: `208e5990bebb44cffeece025bc0eb8e1ae49c9df` (current checkout). The earlier research baseline is historical evidence; the source observations below use this current base.

## System Overview

### How This Feature Fits

Owner commands enter `internal/daemon/mcp_creation.go`; trusted admission remains in `internal/turn` and `internal/store`. `internal/report` renders safe values; `internal/turn.Reporter` persists and delivers issue comments; the daemon owns retry timing and queue ordering. No package, external API, worker, timer or dependency is needed.

`handleMCPCreation` and `resumeCreationSuccess` currently consume accepted approvals without reporting. The authoritative intent already stores original comment identity, hash, creation and expiry timestamps. `internal/store/creation_success.go` currently returns only the intent ID; extend the read projection to expose immutable acknowledgment data joined to the retained proposal relationship. Do not derive acceptance from mutable command text or perform fresh admission during recovery.

`internal/turn/report_delivery.go` already supplies deterministic delivery identities, retained sanitized bodies, intent-before-POST ordering, bot/source/body reconciliation, confirmed receipt persistence and consumption on completion. Reuse this journal for acceptance; a separate acknowledgment table is not necessary for the baseline design. The existing unconsumed comment plus admitted intent supplies the debt before the delivery journal row exists. Already-consumed comments remain excluded by normal intake and the reporter's consumption guard.

### New Packages / Components

No new packages. Add a typed acknowledgment projection in `store`, a creation-acceptance reply variant in `report`, and the small `turn`/`daemon` wiring needed to use existing delivery and recovery. Extend proposal guidance at both presentation entry points.

### Key Boundaries

- Store → daemon/turn: a source-scoped immutable intent view containing intent identity, source identity, full hash, relationship, original creation time and expiry. This is reporting data, never an additional grant.
- Daemon/turn → report: validated creation data and a descriptive publication-context value, with legacy-eligible, graph-unavailable and unavailable/unknown cases. It cannot authorize execution.
- Report → turn Reporter: existing redacted body contract; no forge or database access inside report (SEC-5, SEC-6).
- Turn Reporter → store/GitHub: existing durable report delivery path, outside any transaction across network I/O (CONC-4, CONC-10).
- Daemon → recovery: same source comment and stable acceptance purpose on fresh and recovered intake. The original source issue is the destination.

### Data Flow

Fresh approval: authenticated intake → existing durable admission → read original intent projection → render historical acceptance and applicable guidance → persist report intent/body → post or reconcile → confirm delivery → complete delivery/consume source.

Restart before report retention: owner identity filter → pending report recovery → admitted-intent lookup by trusted source → reconstruct acknowledgment from stored values → same delivery path. Preserve the existing placement of `resumeCreationSuccess` before mutable command parsing, mode and configuration checks.

Restart after report retention: recover the exact journal body and identity before reevaluating mutable inputs. Never silently replace retained text or renew consent. Historical phrasing and conditional next steps make a delayed original body truthful even when eligibility or expiry changed.

Proposal guidance: explicit prepare and post-run retained proposal presentation → the same pure guidance rendering → existing respective journal purposes. Keep the retained exact JSON, hash and relationship unchanged.

### Concurrency & Context

Reuse current per-task processing and the daemon's report-recovery scheduling. No new goroutines or sleeps. Accessors and blocking operations take caller context (CONC-2, CONC-5, CONC-6). SQL remains in `internal/store`; keep its one-connection pool. A read transaction may join intent and proposal data but must end before rendering/forge I/O. Preserve completion bookkeeping cancellation behavior already owned by the reporter.

### Shared State and Side Effects

Admission, report delivery and grant binding are separate states. Acceptance changes only reporting and source consumption; no new run, grant, tool registration, mode transition, current-proposal movement or TTL change. The existing report completion transaction owns the watermark advance; remove silent consumption from successful fresh/recovered approvals rather than adding an extra earlier advance.

The acceptance purpose must be stable and source-specific, distinct from preparation, rejection and post-run proposal presentation. Register it alongside the existing creation-purpose exemptions from the legacy runless-ambiguity probe: old generic task replies must not block a new source-specific acceptance. Continue strict marker, exact body, destination and numeric bot verification for uncertain deliveries.

## Decisions Binding Multiple Stories

1. Deliver two vertical stories: first accurate proposal guidance on all proposal paths; second acceptance content and reliable delivery/recovery together, depending on the shared guidance from the first. Never ship silent-consumption replacement without durable acknowledgment recovery.
2. Reuse the existing report journal and unconsumed-comment recovery debt; extend durable intent reads rather than introduce a second consent or delivery state machine. No baseline migration is required.
3. Use one pure guidance representation across proposal and acceptance. Resolve descriptive context from existing workflow/capability selection or known run provenance without preparing or launching a run. Unknown context must not default to eligible legacy publication.
4. Wording is historical and conditional: approval was recorded, approval alone started no publication, original deadline was derived from the trusted comment, and an eligible attempt must bind before it. Retained feedback must never claim the grant is currently usable or promise publication.
5. Keep current authority checks, graph exclusion, delivery ambiguity policy and watermark ordering intact. A trustworthy reconciled receipt completes delivery; missing/conflicting remote evidence leaves it pending without blind reposting.

## Publication Guidance Design

`internal/turn/mcp_runtime.go` currently excludes `agent.InvocationResearchNode` from creation grant lookup/tool installation. Legacy publication additionally needs Research mode, native MCP support, MCP enabled, enabled creation authorization and a creation forge. Merely observing a retained proposal or an admitted intent proves none of these current capabilities.

Use a small presentation-only context owned by the consuming layer, fed from existing workflow selection/capability configuration and run provenance. The report package must not import turn or query the store. A post-run graph proposal can use its graph run provenance; explicit prepare and acceptance must use the factory's existing workflow selection information. If selection cannot safely establish eligibility, show unavailable/unknown wording plus the invariant that graph Research cannot publish and another ordinary comment does not unlock that capability. Do not execute workflow preparation, bind leases, consume grants or change mode to discover guidance.

All paths explain the conditional distinction: an eligible legacy publishing path needs a further ordinary comment; graph Research cannot publish this batch through that path. Where configuration can change between first rendering and eventual delivery, frame the observation as the context at recording/presentation and keep the future instruction explicitly conditional on eligibility. The absolute expiry is the stored deadline, not processing time plus 30 minutes. Renewal always needs a fresh approval, and reuse of the retained proposal is conditional on it remaining current and eligible; otherwise a new proposal is needed.

## Per-task Structural Hints

The following labels are architecture work units for the orchestrator to map into its final typed story/task IDs; they do not create separate requirements.

### Guidance implementation

**Agent:** go-implementation-engineer

- **Pattern:** shared pure rendering plus caller-supplied descriptive context.
- **Placement:** `internal/report/reply.go` (or a focused creation-guidance file), `internal/daemon/mcp_creation.go`, and `internal/turn/creation_presentation.go`; workflow observation belongs in daemon/turn, not report.
- **Interfaces:** extend the creation reply data with publication context; reuse `RenderReply`, `ReportReply` and `ReportCreationPresentation`.
- **Integration:** cover explicit prepare, recovered preparation and agent-drafted post-run presentation. Preserve the exact envelope and command fingerprint.
- **Watch out:** graph provenance must survive the post-run presentation path; absence of an active graph is not proof that the next run is legacy. Default descriptive uncertainty must be conservative.
- **Not in scope:** capability enablement, run scheduling, approval syntax or authority changes.

### Guidance unit evidence

**Agent:** go-unit-test-engineer

- **Pattern:** renderer tables using stdlib testing and existing fixtures (TEST-1, TEST-2).
- **Placement:** report creation/reply tests; small consumer-owned fakes only where needed (TEST-5).
- **Evidence:** exact approval line, unchanged retained bytes, 30-minute comment-time basis, separate legacy start action, graph restriction, unknown/unavailable behavior and conditional renewal.
- **Watch out:** tests should assert product meaning and safety boundaries, not only a golden copy of implementation text.

### Guidance path evidence

**Agent:** go-integration-test-engineer

- **Pattern:** normal repository tests using real migrated SQLite and local fake forge (TEST-3, TEST-6); no integration tag or special naming.
- **Placement:** existing creation preparation and proposal presentation fixtures in daemon/turn.
- **Evidence:** explicit/recovered preparation and legacy/graph post-run presentations receive correct context and unchanged canonical proposal content; publishing permissions remain unchanged.

### Acceptance projection and rendering

**Agent:** go-implementation-engineer

- **Pattern:** extend source-scoped store observations and typed reply rendering.
- **Placement:** `internal/store/creation_success.go`, `internal/report/reply.go`, `internal/turn/report.go` and focused creation reporting code if appropriate.
- **Interfaces:** immutable source-bound intent view; distinct acceptance reply kind/payload; no opaque command-body parsing in the renderer.
- **Watch out:** relationship comes from the proposal bound by the admitted intent, not the current proposal pointer. Recovery lookup must still succeed after expiry, supersession or mode/configuration changes. The old intent-ID read contract can remain for existing callers; avoid unrelated churn.
- **Constraints:** revalidate echoed hash/relationship and retain redaction (SEC-5, SEC-6); preserve numeric provenance (SEC-8); original nanosecond timestamps convert to absolute UTC for display without changing stored authority.

### Acceptance delivery and recovery

**Agent:** go-implementation-engineer

- **Pattern:** existing `reportAttempt`/`reportReply` and `Reporter.deliver` journal.
- **Placement:** `internal/daemon/mcp_creation.go`, `internal/turn/report.go`, `internal/turn/report_delivery.go`.
- **Integration:** fresh admission and `resumeCreationSuccess` converge on acknowledgment; pending report recovery remains first. Stable source-specific purpose is recognized by the existing journal and legacy-ambiguity exemption.
- **Watch out:** a crash after admission but before delivery retention must recover without a new approval. A posted comment with lost response must reconcile exact retained bytes. Completion/consumption must not precede confirmation (CONC-4, CONC-10). Already-consumed approvals must not be enumerated or backfilled.
- **Not in scope:** expiry timers, blind reposting when remote absence is uncertain, publication orchestration or modifications to `BindGrant` eligibility.

### Acceptance unit evidence

**Agent:** go-unit-test-engineer

- **Pattern:** renderer cases plus source/capability contract assertions in the existing test style.
- **Evidence:** full hash and relationship, historical acceptance/no-start statement, original absolute UTC expiry, delayed-delivery-safe wording and guidance reuse. Preserve rejection classification tests, including retryable infrastructure errors.

### Acceptance persistence and failure evidence

**Agent:** go-integration-test-engineer

- **Pattern:** real SQLite and daemon/turn local HTTP fixture faults, all in ordinary tests (TEST-3, TEST-6, TEST-8).
- **Evidence:** admission-before-render failure; render/retention failure; external success with lost response; confirm/complete failure; process restart at those boundaries; original-body replay after comment edit/mode change/expiry/supersession; renewed approval with new source identity/deadline; no historical backfill; no automatic run and unchanged graph/legacy grant behavior.
- **Watch out:** inject known pre-send failure separately from uncertain dispatch. Existing delivery deliberately refuses to infer non-delivery from an empty lookup; that case stays pending, not a successful retry or a duplicate POST. Demonstrate eventual completion once trustworthy receipt evidence is available. Count visible acknowledgment comments and inspect durable intent, watermark and run/grant state.
- **Regression anchors:** `internal/daemon/creation_success_io_test.go`, `internal/store/creation_success_io_test.go`, existing creation-rejection fixtures, `internal/turn/creation_proposal_catalog_test.go`, and report-delivery fixtures.

## Platform and Security Findings

Platform: layout, interfaces, database, errors and context apply as above. No new API listener, third-party dependency, goroutine, configuration key or infrastructure is needed. Use typed/sentinel error handling and preserve infrastructure failures as pending; do not convert failed observations into policy rejections (CONC-3). Existing injected structured logging is sufficient. Required Implement checks include `make check`; do not run `make build` or modify `dist/`. Additional focused tests should cover the specified failure boundaries rather than invent integration tags.

Security rulebook: `.aiteam/APP_CONSTRAINTS.md` security section, supplemented by the scanner's generic principles; no literal YAML `security:` mapping exists.

| Area | Assessment | Forward-looking implication |
|---|---|---|
| Input validation | Compliant design | Preserve trusted source matching and renderer-local hash/relationship validation. |
| Authentication/authorization | Concern addressed | Historical acceptance must not reauthorize; graph exclusion and owner identity remain unchanged. |
| Secrets/configuration | Compliant design | No new secrets, subprocesses, permissions or config. |
| Data handling | Compliant design | Reuse redacted outbound bodies and parameterized store reads; no new sensitive content. |
| Dependencies | Compliant design | No new dependency; this is not a dependency-vulnerability audit. |
| Error handling | Concern addressed | Never claim acceptance before durable admission or successful delivery after uncertain response. |

## Evidence Bounds and Deviations

No deviation requested. The specification's one-visible-acknowledgment outcome is implemented under the existing truthful-receipt policy: an unresolved uncertain send can remain pending indefinitely until trustworthy evidence appears. This is consistent with retaining failed/uncertain delivery rather than manufacturing success.

No code or tests were changed or executed in this architecture review. Source inspection and the supplied historical offline tests do not verify the deployed daemon, live GitHub behavior, provider support or authentic nested-agent delivery. The missing `docs/decisions/nested-agent-evidence.md` and `docs/story-contract-instruction-manifest.md` remain documented limitations; supplied workflow/factory policy governs this Plan. Candidate proof remains offline renderer, real SQLite and local forge tests bound to the future implementation snapshot, followed by required aggregate checks.
