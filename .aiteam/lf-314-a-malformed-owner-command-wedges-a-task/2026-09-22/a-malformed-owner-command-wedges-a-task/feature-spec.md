# Feature Spec: a-malformed-owner-command-wedges-a-task
# Status: APPROVED
# Date: 2026-09-22

## Goal
- Let an owner recover from a rejected creation approval or preparation command through an actionable issue reply and automatic queue progress after confirmed reporting.

## User Stories
| ID | Story |
|---|---|
| US-1 | As the owner, I want rejected creation commands explained and consumed after reporting so my next valid instruction runs without deleting comments or editing the database. |
| US-2 | As the owner, I want reporting and successful intake recovered across retries and restarts so failures neither duplicate effects nor erase my original outcome. |

## Decisions and Provenance
| Decision | Source |
|---|---|
| Cover both creation approval and preparation; exclude other command families and operator recovery tooling. | Owner accepted both recommendations, then instructed proceed and `/plan this out`; brief d4/d19. |
| Confirm diagnostic delivery before consumption; preserve unknown and transient failures for retry. | Accepted shaping, brief d6/d27. |
| Retain original rejection and successful intake outcomes before reevaluating mutable eligibility. | Accepted shaping, brief d11/d13/d26/d29. |
| Keep all existing approval restrictions, syntax and limits. | Accepted shaping, brief d14. |
| No further material product decisions remain. | Recorded owner approval and brief a5. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| Creation intake outcome | Source identity | Repository/task/session identity and comment ID | Yes | Bound to trusted intake; one retained outcome per source comment; never inferred from payload identity. |
| Creation intake outcome | Command kind | Approval or preparation | Yes | Distinguishes correction guidance and successful recovery. |
| Creation intake outcome | Outcome | Confirmed rejection or successful intake | Yes | Unknown or uncertain failures cannot manufacture a rejection. |
| Rejection | Reason category | Stable classified value | Yes | Verified origin; never classify using message text alone. |
| Rejection delivery | Safe reply | Bounded rendered text | Yes | Retain exact correction text before dispatch; no raw input, secrets or hidden proposal content. |
| Rejection delivery | Delivery identity and evidence | Stable marker/key and remote confirmation | Yes; evidence when available | Reused for retries and reconciliation; marker alone is not confirmed delivery. |
| Intake recovery | Lifecycle | Pending reporting, delivered, consumed; equivalent existing representation allowed | Yes | Consumption follows confirmed delivery for rejection and preparation presentation. |
| Successful preparation | Proposal reference | Retained proposal identity/content | For preparation | Recover same proposal without replacing or regenerating it. |
| Successful approval | Admission reference | Retained owner intent identity | For approval | Recover original admission without refreshing authority or expiry. |

### Relationships
- Source comment → one logical retained creation intake outcome → existing recoverable report delivery where reporting is required.
- Successful preparation → existing proposal and presentation records; successful approval → existing owner intent.
- Physical schema and any required migration remain architectural choices; reuse existing records wherever their semantics satisfy these requirements.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|---|---|---|---|
| Owner issue comment | `/mcp-create <proposal SHA256> linked\|attach` | Existing trusted numeric owner checks | Preserve exact whole-command grammar, lowercase 64-hex hash and restrictions. |
| Owner issue comment | Existing `/mcp-create prepare ...` payload surface | Existing trusted numeric owner checks | Preserve current decoder, payload version and limits; no new grammar. |
| Factory issue reply | Rejection diagnostic | Existing bot/report delivery path | Only changed public surface; names command, safe reason and concrete correction. |

## Business Rules
- Resolve retained rejection, presentation or successful admission before fresh mode, configuration, payload or proposal eligibility checks; source edits do not alter a retained outcome.
- Confirmed rejection categories: malformed syntax or payload; unsupported payload version; invalid payload fields or limits; missing, wrong, ambiguous or malformed Research selection; disabled or deliberately unavailable loaded creation capability; confirmed absent, expired, superseded, mismatched or ineligible proposal; invalid owner controls that reach trusted creation handling.
- Reconcile recorded effects before declaring proposal or authority refusal final; broad authority errors that can conceal storage failure are insufficient evidence of rejection.
- Infrastructure/storage failures, rate limits, forge outages, shutdown, unknown internal failures, unexplained missing components and uncertain persistence/remote effects remain pending under current ordering.
- Rejection creates no owner intent, approval grant, publication, task execution or mode change.
- Approval syntax guidance describes the existing grammar without generating an executable approval command. Preparation guidance describes the invalid requirement without echoing raw payloads.
- Mode guidance asks for one Research selection; capability guidance identifies required operator configuration; proposal guidance requests a current eligible proposal and a fresh owner comment.
- A rejected command remains rejected after mode/configuration/proposal changes; correction requires a new comment.
- Preserve identity filtering, bot exclusion, binding, relationship, freshness, edit restrictions and proposal limits. Filtered bot/third-party comments retain existing record-and-consume behavior with no new diagnostic or execution path.
- Valid preparation retains and presents the same proposal across recovery. Valid approval retains intent and consumes its comment without a new success reply or starting work.
- Apply corrected behavior when existing unconsumed creation comments are encountered; never replay previously consumed comments.

## Edge Cases & Error Handling
| Case | Required behavior |
|---|---|
| Failure retaining a rejection | Leave source pending; do not send an unretained diagnostic or consume. |
| Failure before posting | Retry the retained diagnostic; later comments remain ordered behind it. |
| Posting succeeds but response is lost | Reconcile stable delivery identity; do not consume until confirmed or post a duplicate. |
| Restart before/after delivery | Resume original retained outcome and exact reply; finish consumption once confirmed. |
| Consumption fails after diagnostic delivery | Retry consumption without reposting. |
| Consumption fails after successful approval | Recover original intent before eligibility checks, including after expiry/supersession; no false rejection, duplicate intent or authority renewal. |
| Preparation succeeds but presentation fails | Recover same proposal/presentation; never convert reporting failure into input rejection. |
| Mode, configuration, proposal or source text changes during recovery | Finish retained outcome without reevaluating or replacing it. |
| Unresolved infrastructure failure | Keep later comments blocked under existing ordering; resume once resolved. |

## Existing Features to Reuse
| Existing feature | Location / role |
|---|---|
| Trusted creation intake and authorizer | `internal/daemon/mcp_creation.go`, `internal/turn/mcp_creation.go` |
| Preparation/presentation and proposal admission records | `internal/turn/publication_recovery.go`, `internal/store/mcp_creation.go` |
| Durable rendered reply bytes, marker and uncertain delivery reconciliation | `internal/turn/report_delivery.go`, `internal/daemon/report_recovery.go` |
| Safe public rendering and redaction | `internal/report`, `internal/redact` |
| Queue filtering, watermark and backlog handling | Existing `internal/daemon` intake and store/report consumption paths |

## Assumptions and Risks
- Research source revision `1be56786ec00c214d60733f6b16ac87a83a0bd5d` and historical tests are evidence, not a deployed-daemon reproduction or proof of this future implementation.
- Current direct source reads confirm creation errors still use `leave`; approval still checks authority before syntax. Detailed error propagation and recovery seams are architect responsibilities.
- Evidence documents `docs/story-contract-instruction-manifest.md` and `docs/decisions/nested-agent-evidence.md` are absent per orchestration context; preserve explicit candidate evidence obligations below rather than infer missing policy content.
- Existing durable delivery machinery is the reuse candidate; engineering must verify source-comment recovery and successful intake debt remain discoverable across restart.
- Deliberately disabled capability is deterministic; unexplained missing runtime wiring is retryable. Capability absence alone does not prove safe rejection.
- No live host observation, nested agent capability proof or deployment is required for this offline command-handling fix; synthetic tests establish only tested source behavior.

## Out of Scope
- Other owner-command families or a global retry policy change.
- Automatic skipping by age/retry count, new alerts or operator recovery commands.
- Proposal-generation changes, broader creation feature changes or grammar/limit relaxation.
- Live repair, daemon restart, deployment, historical #230 reproduction or live forge mutation during planning.
- Protected instruction-tree changes; no harness split is needed.

## Acceptance Criteria
- [ ] AC-001: An eligible malformed approval comment followed by a valid instruction yields one safe actionable rejection reply, no approval/work effect, consumption only after confirmed reporting, and eventual execution of the later instruction.
- [ ] AC-002: An eligible malformed preparation comment followed by a valid instruction yields one safe actionable rejection reply, no proposal admission/publication/work effect from the rejected input, consumption only after confirmed reporting, and eventual execution of the later instruction.
- [ ] AC-003: Each confirmed rejection category in Business Rules is covered with command-specific safe correction guidance; no raw payload, secret or hidden proposal content appears in diagnostic output, and syntax guidance generates no executable approval command.
- [ ] AC-004: Retryable/unknown infrastructure and uncertain persistence/effect failures are not classified as input rejection or consumed; later comments stay ordered and resume after the original outcome resolves, including when an authority error wraps storage failure.
- [ ] AC-005: Rejection recovery retains trusted source identity, command kind, reason, exact safe reply, stable delivery identity, remote evidence and lifecycle; source edits or changed mode/configuration/proposal eligibility cannot alter the retained outcome.
- [ ] AC-006: Failure before posting, successful posting with a lost response, restart before and after delivery, and failed consumption recover to one confirmed diagnostic and consumption without duplicate replies or premature queue advancement.
- [ ] AC-007: Valid preparation recovers the same retained proposal and presentation across reporting failure/restart without duplicate proposals, refreshed proposal state or false rejection; valid approval records intent and consumes without a new success reply or launching work.
- [ ] AC-008: Successful approval followed by failed consumption recovers original admission before fresh eligibility checks, including after expiry, supersession or mode change, without duplicate intent, false rejection, extended expiry or renewed authority.
- [ ] AC-009: Existing identity filtering, bot exclusion, exact grammar, proposal limits, source/relationship binding, freshness and edit restrictions remain enforced; filtered identities retain existing consumption without new diagnostic/execution, and rejected corrections require a new comment.
- [ ] AC-010: Already queued unconsumed creation comments receive corrected rejection/recovery handling without manual watermark repair; previously consumed comments are not replayed and unrelated command-family behavior is unchanged.

## Candidate Evidence Inventory
| Criteria | Proof method | Source availability | Version bounds | Missing prerequisite |
|---|---|---|---|---|
| AC-001, AC-002, AC-010 | Daemon queue regression with fake forge, real migrated temporary store and recorded executor calls; assert reply count, watermark, effect absence and later execution | Existing daemon creation, tick and backlog test patterns | New checks must run against final implementation snapshot; historical tests insufficient | Implement regression fixtures and capture executed results |
| AC-003, AC-004, AC-009 | Table-driven parser/authorizer/classification tests plus storage fault injection and diagnostic redaction assertions; unchanged-policy regression | Existing turn creation tests, store creation code and report rendering | Final implementation snapshot, current grammar/limits | Complete category/fault matrix and execute checks |
| AC-005, AC-006 | Durable report recovery tests with fake remote success/lost response, process-equivalent reopen, changed source/configuration and failed consumption | Existing report delivery/recovery and temporary SQLite patterns | Final implementation snapshot; simulated restart proves persistence behavior only | Add deterministic failure seams where necessary; execute checks |
| AC-007, AC-008 | Admission/presentation recovery tests with real temporary store, failed consumption and changed eligibility; assert stable IDs/content/expiry and no new effects | Existing creation proposal/intent records and turn/daemon seams | Final implementation snapshot; no live daemon claim | Implement and execute recovery regressions |
| All | Repository-required `make check` and source review mapped to explicit criterion IDs | Existing fmt/vet/pinned lint/race suite | Exact final implementation snapshot | Execute in Implement; retain evidence for read-only Validate |
