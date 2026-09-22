# Architect: Go Architect
# Created: 2026-09-22

Verdict: feasible within the approved creation-command scope; no standard deviation required.

Source baseline: `bef0007a8fc77aefb93e57d93b79d9c2411d79a8`. This is source inspection, not executed verification or a live-daemon observation. Earlier research used a different revision. No implementation or tests were run in this review.

## System Overview

### How This Feature Fits

`internal/daemon/mcp_creation.go` currently routes mode, preparation and approval failures through `leave`. `internal/turn/mcp_creation.go` checks authority before parsing; `internal/store/mcp_creation.go` checks current eligibility before finding an existing owner intent. Consequently, fixing only the daemon's error branch would miss successful intake followed by failed consumption. `internal/turn/publication_recovery.go` prepares owner proposals without a durable source-comment presentation association, whereas the agent-run presentation path already has such an association.

Keep daemon intake and retry timing in `internal/daemon`, creation policy and orchestration in `internal/turn`, durable outcomes and all SQL in `internal/store`, and bounded public wording in `internal/report`. Reuse `internal/submission` canonical validation and `internal/redact`; introduce no package or dependency.

### New Packages / Components

No new package. Add a creation intake outcome component within existing store/turn ownership. Its logical record binds trusted source task/session/comment, command kind and immutable outcome to either rejection category/report delivery, retained proposal/presentation, or original owner intent. Prefer a narrow additive migration and source-comment accessors over repurposing run IDs as comment IDs in run-owned presentation records.

The record does not grant publication authority. Existing proposal, owner-intent and grant records remain the authority owners. Use their immutable references and associate successful creation intake atomically with the relevant durable mutation, so a crash cannot leave an undiscoverable admission. Existing pre-change owner intents must also be discoverable by trusted repository/comment identity before eligibility reevaluation; do not require the new journal to exist for those rows.

### Key Boundaries

- Daemon → turn: trusted numeric identity and source metadata; explicit intake outcome separates confirmed rejection, recovered/successful intake and operational error. Unclassified errors remain retryable (CONC-3, STYLE-5).
- Turn → store: context-first operations owning transactions and immutable source association. No SQL or database handles escape store, no transaction spans rendering or forge I/O, and the single-connection pool stays unchanged (CONC-5, CONC-10).
- Turn → report: typed rejection category plus command kind; static correction wording, no raw payload/error interpolation. Renderer revalidates echoed input and applies outbound redaction (SEC-5, SEC-6).
- Turn reporter → forge: existing durable `ReportDelivery`, retained body, marker, possibly-sent state and exact bot/destination/content reconciliation. Do not introduce another posting protocol.

### Data Flow

1. Existing `handleComment` numeric identity filter runs first. Preserve filtered comment recording/consumption and bot exclusion.
2. Existing `resumeReport` remains early. Also resolve retained creation intake by trusted source identity before parsing the current body or checking mode/configuration. This placement matters when the source comment has been edited so it no longer parses as a creation command.
3. With no retained outcome, evaluate fresh creation controls and canonical payload/proposal policy. Confirmed input refusal becomes a typed rejection; unknown failures remain errors. In particular, do not blanket-convert `ErrCreationAuthority` or any wrapped error to rejection: distinguish known policy refusal from failed database observation at its origin.
4. Retain the rejection and its exact safe delivery intent before any post. Recover a gap between outcome retention and report creation from the retained category/body, never by reclassifying mutable input. Report completion confirms remote evidence before source consumption.
5. Successful preparation atomically retains the original proposal association, then presents that same proposal through durable reporting. It must not rerun retention and reset the current proposal after a newer proposal exists.
6. Successful approval records/reuses the original immutable owner intent and consumes without a new reply or launch. Recovery is acknowledgment of earlier admission, never a new grant, extended expiry or a bypass of later grant-binding checks.

### Concurrency & Context

Use existing per-task worker ownership and daemon retry/backoff, with no new goroutine or timer (CONC-2, CONC-6). Preserve cancellation and distinguish inability to record/observe from a policy decision. Source association uniqueness and transactional consistency protect retries; do not depend only on in-memory worker exclusion. Daemon supplies scheduling time; preserve existing store-injected time for authority checks without adding a clock to turn.

### Shared State and Side Effects

Pending source outcomes must remain discoverable after restart and through normal backlog handling (CONC-15). Integrate with existing report recovery debt, retaining original source identity independently of mutable comment text. Report delivery owns exact bytes and remote evidence; intake records should reference it rather than maintain a competing copy of delivery state.

`ReportPending` already resumes before command parsing, and `CompleteReportDelivery` is the consumption boundary to reuse. Existing reporter recovery conservatively refuses to resend an uncertain post after an empty lookup; preserve that behavior. A transport error after marking possibly-sent is not proof that nothing was posted. Tests promising recovery before posting should fail before dispatch intent is claimed or use a proven no-send failure boundary; uncertain absence remains pending.

Runless delivery currently probes legacy task-only replies. Give creation rejection a dedicated purpose and evaluate the same narrowly justified post-journal exemption used by creation presentation: an unrelated old task-only report must not permanently prevent a new deterministic refusal. Preserve marker/content/bot/destination checks for uncertain delivery; do not weaken global runless recovery.

Loaded creation disable is a known policy refusal; a nil runtime/authorizer or failed construction without explicit disable provenance is an operational failure. Recovery reads must remain available when fresh creation is disabled. No configuration hot reload or new operator control is required.

## Per-task Structural Hints

Task labels below are architectural work areas for the orchestrator to map to its typed task IDs, not additional requirements or stories.

### Intake classification and safe diagnostics
**Agent:** go-implementation-engineer

- **Pattern:** typed reportable outcome at the creation boundary, operational errors preserved with causes (CONC-3, STYLE-5).
- **Package placement:** `internal/turn/mcp_creation.go`, `publication_recovery.go`, canonical validation boundary in `creation_publication.go`, origin-specific store refusals, `internal/report` rendering, `internal/daemon/mcp_creation.go` routing.
- **Key interfaces:** trusted `OwnerGrantControl`; outcome containing known refusal category or successful retained reference; existing reporter request flow.
- **Integration points:** distinguish parse/field/version/limit failures, mode refusal, explicit disable and confirmed proposal eligibility failure without exposing proposal details.
- **Watch out for:** canonical validation and redaction are separate stages; a redactor failure is operational, not malformed input. Missing proposal after a successful query differs from a failed query. Hash grammar and relationship checks remain exact.
- **Not in scope:** global error taxonomy, other command families, approval grammar relaxation or auto-skip.

### Durable source outcomes and successful admission recovery
**Agent:** go-implementation-engineer

- **Pattern:** immutable source-comment outcome with transactional references to existing authority records.
- **Package placement:** `internal/store/mcp_creation.go`, source-associated creation persistence beside it, new numbered SQL migration if needed; turn authorizer/preparation orchestrates these operations.
- **Key interfaces:** source lookup independent of current body/mode; retained rejection, preparation and approval references; creation-specific lifecycle recovery.
- **Integration points:** successful proposal retention plus source association; successful intent admission plus source association; lookup of existing legacy intents before TTL/current-proposal checks.
- **Watch out for:** `AdmitOwnerIntent` currently checks TTL/current proposal before duplicate lookup. Merely moving that lookup is insufficient if the daemon rejects mode or parses changed text first. Recovery must not update current proposal, envelope bytes, intent expiry or grant eligibility.
- **Not in scope:** live watermark repair or migration that marks historical unconsumed comments consumed.

### Queue and delivery recovery integration
**Agent:** go-implementation-engineer

- **Pattern:** existing durable report intent/confirmation/consumption ordering (CONC-4, CONC-10).
- **Package placement:** `internal/daemon/tick.go`, `mcp_creation.go`, `report_recovery.go`; `internal/turn/report_delivery.go` and report entry points; source backlog discovery in existing store owners.
- **Key interfaces:** `ReportPending`, report creation/dispatch, original source lookup and reporter consumption.
- **Integration points:** recover source outcome before fresh command parsing; early existing report recovery; restore debt without requiring a new forge comment.
- **Watch out for:** source edits, mode changes, disabled capability, failed consumption, source-associated preparation versus run-owned presentation, unrelated legacy task-only reports, and crash between journal and delivery creation.
- **Not in scope:** duplicate-free claims based on marker alone, retry thresholds, alerts or operator commands.

### Classification and rendering unit coverage
**Agent:** go-unit-test-engineer

- **Pattern:** table-driven boundary tests with hand-written fakes and stable category assertions (TEST-1, TEST-2, TEST-7).
- **Package placement:** existing turn creation unit tests, submission validation tests as relevant, report tests.
- **Integration points:** all confirmed categories and unknown/wrapped failures, safe command-specific guidance, unchanged identity/grammar/limit restrictions.
- **Watch out for:** raw payload/hidden proposal leakage, executable fabricated approval guidance, errors classified by message text, nil wiring confused with explicit disable. Unit tests alone cannot establish transactional recovery.

### Durable queue and recovery evidence
**Agent:** go-integration-test-engineer

- **Pattern:** existing offline daemon fixture with real migrated temporary SQLite, httptest forge and fake executor (TEST-3, TEST-6); ordinary `_test.go` files, no integration tag/name convention.
- **Package placement:** existing daemon creation/report recovery tests and store creation tests.
- **Integration points:** malformed approval/preparation followed by valid work; report creation failure, confirmed/lost-response posting, reopen/restart, consumption fault; successful preparation and approval then changed eligibility; queued pre-change comments and legacy intent recovery.
- **Watch out for:** assert original IDs, exact bytes and expiry/current proposal state as well as reply count and watermark. Include counterfeit marker/wrong bot/destination/conflicting body refusal. Use channel rendezvous and `Quiesce`, never sleeps (TEST-8, TEST-9).
- **Evidence boundary:** these tests prove offline source behavior against the final implementation snapshot only. `make check` belongs to Implement; no live forge or provider launch is required.

## Platform and Security Findings

Platform: existing internal package ownership, consumer-owned interfaces, context-first persistence, single-connection SQLite and numbered migrations apply. No new HTTP endpoint, external dependency, process, timer or observability component is warranted. Repository rules override generic platform suggestions for migration libraries, mocking libraries and pool tuning.

| Area | Status | Forward-looking finding |
|---|---|---|
| Input validation | Concern | Preserve exact canonical validation while distinguishing confirmed invalid content from unavailable validation infrastructure. |
| Authentication and authorization | Concern | Source recovery acknowledges only immutable earlier intake and cannot renew creation authority; keep numeric identity checks before recovery. |
| Secrets and configuration | Compliant by design | No new secret or configuration surface; outbound redaction remains mandatory. |
| Data handling | Concern | Retain bounded safe diagnostic bytes, not a new raw payload/error log in public state. |
| Dependencies | Compliant by design | Existing stdlib/store/report components suffice; no dependency audit or new package is needed. |
| Error handling | Concern | Broad authority sentinel reuse conceals distinct failure origins; establish verified refusal categories at their source. |

## Deviations and Evidence Limits

No deviation requested. The two referenced evidence/instruction documents are absent as supplied by the orchestrator; no content is inferred from them. The feature spec's explicit evidence inventory remains the planning basis. No authentic host, nested-agent, helper-delivery or deployment claims are made.
