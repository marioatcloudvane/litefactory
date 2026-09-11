# Feature Spec: a-report-that-posts-but-fails-to-record
# Status: PLANNED — architecture approved; owner approved six directions; defaults below are Plan assumptions
# Date: 2026-09-10

## Goal
- Prevent refused or abandoned runs from poisoning the queue and prevent delivery retries from repeatedly posting the same report or explanation.

## User Stories
| Area | User need |
| --- | --- |
| Preparation and lifecycle | As an owner, receive one clear not-ready refusal, with no agent launched, then submit a fresh command when ready; active work must remain undisturbed. |
| Durable delivery | As an owner, receive one logical report/reply even when posting succeeds but local recording fails or the daemon restarts. |
| Recovery and visibility | As an operator, see unresolved delivery and let safe retries recover without duplicate comments or silently skipped work. |

## Approved Decisions
- Apply all six owner approvals from Research run 0003; do not ask again.
- Keep historical row 283's unexplained startup-recovery discrepancy separate; no claim of a reconstructed incident and no dependency on unavailable logs.
- Preserve readiness checks; known deterministic readiness refusals require a fresh command after the explanation is durably completed.
- Preserve live-worker ownership and exclusive startup orphan reconciliation; no age, missing-log or PID heuristics.
- Combine stable logical identity, durable pre-post intent, retained redacted body, remote marker and confirmed receipt for both Reporter.Report and Reporter.ReportReply, including runless and redirected/refinement destinations routed through them.
- Prefer confirmed receipts, adopt strongly equivalent existing comments, suppress new posts on uncertainty/conflict, retain existing duplicates, and do not recreate confirmed-but-deleted comments.
- Retry local completion separately from posting; keep source ordering and durable unresolved diagnostics.

## Data Model
| Entity | Required information | Validation and retention |
| --- | --- | --- |
| Logical delivery | Versioned stable key; canonical execution task; actual destination; trigger namespace and identity; purpose; optional allocated run ID | Full tuple defines identity; retries/restart reuse it; distinct triggers and purposes cannot collide. |
| Delivery intent | Exact intended final redacted body; state distinguishing not attempted, possibly sent, confirmed and completed | Persist before external effects; retain with run/task history; no automatic pruning in this feature. |
| Receipt | Confirmed numeric comment ID and matching destination; confirmation provenance | Historical proof survives later remote deletion; never infer a receipt from intent. |
| Recovery diagnostic | Redacted bounded reason, attempts, next eligible retry, escalation state | Durable when storage is available; no credentials or raw unsanitized error payloads. |
| Run | Existing durable lifecycle and semantic outcome plus distinguishable prelaunch refusal/failure where needed | Never report a nonterminal run; retain audit identity rather than deleting allocated rows. |

## Interface Scope
| Surface | Change |
| --- | --- |
| Existing report and reply delivery | Share delivery recovery semantics without changing normal successful user output beyond the durable marker. |
| Readiness refusal explanation | State: request refused because plan is not ready or has pending refinements; no agent launched; submit a new implement command after readiness. |
| Daemon logs | Structured diagnostic on first failure, escalation and changed state; identify task, delivery, optional run and next retry. |
| External APIs and permissions | Reuse existing authenticated comment create/list operations and numeric bot identity; no edit/delete API or new credentials. |

## Business Rules and Plan Defaults
- Readiness and pending-refinement domain rejections are final refusals; classify them explicitly, not by string matching. Network, database, filesystem and unclassified preparation errors remain retryable and unconsumed.
- Perform deterministic readiness checks before run allocation wherever feasible; revalidate atomically at the existing approval boundary to handle races.
- The existing lifecycle owner must account for every post-allocation return. Preserve one allocation per trigger: identity-dependent preparation uses an explicit durable prelaunch state, and transient preparation retries reuse that same row/ordinal. This pending request is neither running nor terminal/reportable. Final refusal is a truthful terminal never-launched outcome when allocation already occurred. Never reset terminal executions, delete audit rows or consume transient failures merely because a row exists.
- Failed terminal persistence must preserve positive owner evidence of the stopped execution and prevent report/consumption until authoritative state is repaired; architect must fit this to existing lifecycle constraints.
- Require terminal reportability before rendering or posting any run report. Active work silently defers. Preserve startup reconciliation and its existing interruption semantics.
- Persist the possibly-sent boundary before attempting POST. Failure to persist any prerequisite means no POST. Once confirmed or possibly sent, retries cannot blindly repost.
- Retain exact intended redacted bytes. Recovery does not render a new body and demand equality with mutable current task state.
- Search only when recovering uncertain/unrecorded delivery. Verify actual destination, numeric bot author, full logical identity and exact intended body before adoption; select lowest numeric comment ID among equivalent candidates.
- Prefer a valid stored receipt over listing; otherwise conflicting candidates block. A failed, partial or empty lookup does not prove nondelivery after an uncertain attempt.
- Legacy allocated-run markers are hints only: adopt only if destination, bot identity, persisted task/run/trigger relationship and exact reconstructed intended content establish uniqueness. Runless/task-only collisions or unverifiable legacy content block with diagnostics rather than permitting a new post.
- A retry with proven local pre-dispatch failure may attempt delivery; an uncertain dispatched request requires positive receipt/matching-comment evidence to resume completion. No automatic assertion of nondelivery based only on elapsed time or absence.
- Complete required local bookkeeping/source consumption transaction only after delivery confirmation. Retry that transaction without another POST after failure.
- Proposed simple retry policy: first recovery after the configured poll interval, double per failed eligible attempt, cap at the existing one-hour backoff ceiling; honor any later global rate-limit hold. Persist per-delivery retry eligibility across restart; do not stall unrelated tasks.
- Proposed simple escalation policy: first failure diagnostic, persistent-failure escalation on the third failed eligible attempt, then only changed-state/resolution diagnostics; counters and latest reason stay available durably. These numeric defaults are planning choices, not incident-derived evidence.
- Continue safe recovery checks at the cap; resume automatically when trusted evidence becomes available or local persistence succeeds. Never consume because a retry budget expired. Permanently ambiguous delivery remains visibly unresolved; no force-resend/skip CLI in this scope.

## Out of Scope
- Reconstructing the row 283 incident without deployed-version and restart/recovery evidence; runtime database edits; daemon restart or deployment.
- Automatic deletion/editing of historical duplicates; automatic recreation of deleted comments; manual resend/skip controls.
- General GitHub rate-limit redesign, generic messaging infrastructure, new configuration knobs, and guarantees of universal exactly-once delivery.
- Changes to unrelated factory publication/evidence protocols or installed agent instruction trees.

## Candidate Acceptance Obligations
| ID | Required behavior |
| --- | --- |
| AC-001 | Known readiness/refinement refusal launches no agent, produces one durable explanation over retries/restart, consumes only after recording, and needs a fresh command afterward. |
| AC-002 | Feasible readiness checks precede allocation; every injected post-allocation preparation failure is accounted for; transient failures remain retryable without poisoned rows or premature consumption. |
| AC-003 | A nonterminal run cannot render/post a result; active workers defer silently, owner-ended paths recover, and exclusive startup recovery remains functional. |
| AC-004 | Allocated/runless reports and replies retain distinct stable identity, exact redacted intent and confirmed receipts across retries and restart, including actual redirected destinations and synthetic triggers. |
| AC-005 | POST success followed by receipt/completion failure, lost responses and restart never permit repeated blind posting; confirmed delivery retries local completion only. |
| AC-006 | Strong matching adopts the lowest-ID equivalent candidate, rejects wrong authors/destinations/conflicts/ambiguous legacy markers, tolerates saved-body drift, and honors deleted confirmed receipts without editing/deleting/recreating comments. |
| AC-007 | Failed/partial/empty uncertain lookups never authorize posting; uncertainty remains durable and blocks only its ordered work until trusted evidence permits recovery. |
| AC-008 | Persisted daemon-owned capped backoff, third-failure escalation and changed-state diagnostics prevent per-tick retry spam while retaining unresolved state and source ordering; restart and repair preserve safe resumption. |

## Evidence Inventory for Story Authoring
| Candidate IDs | Proof method | Source availability | Version/context bounds | Missing prerequisite |
| --- | --- | --- | --- | --- |
| AC-001–AC-003 | Offline Go tests with fake agent/clock, temporary SQLite, injected preparation/persistence faults; source review of allocation and ownership | Existing preparation, store, execution and daemon tests; additional regression cases to implement | Current implementation snapshot only; no claim to reconstruct deployed #170 incidents | Execute and retain named checks during Implement. |
| AC-004–AC-007 | Offline temporary-SQLite and fake-HTTP/forge tests spanning retries and reconstructed reporter/daemon instances; explicit failure injection at each side-effect boundary | Existing reporter/client/store test infrastructure; synthetic HTTP responses to implement | Establishes application behavior against controlled responses, not live GitHub guarantees or nested-agent capability | Execute and retain named checks during Implement. |
| AC-008 | Fake-clock scheduling tests, diagnostic assertions and temporary-SQLite restart/recovery tests | Existing daemon backoff and queue tests; added delivery-specific cases | Configured poll interval and one-hour cap; no wall-clock sleeps or real service dependency | Execute and retain named checks during Implement. |
- Story authoring must preserve one inventory mapping per final criterion after splitting/reassigning IDs; candidate IDs above are requirements working context, not independent published stories.
- No acceptance obligation requires authentic nested coding-agent observation; historical operator evidence remains unavailable and outside this feature's completion claim.

## Assumptions and Questions
- No owner questions: remaining choices have conservative defaults consistent with the six approvals and the explicit request for a simple plan.
- Architect may simplify representation and reuse existing owners/interfaces while preserving these observable behaviors; any genuinely incompatible lifecycle constraint must be reported explicitly.
