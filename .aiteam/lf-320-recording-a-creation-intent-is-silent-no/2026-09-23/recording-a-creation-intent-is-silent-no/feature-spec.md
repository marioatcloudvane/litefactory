# Feature Spec: recording-a-creation-intent-is-silent-no
# Status: APPROVED
# Date: 2026-09-23

## Goal
- An owner approving a retained creation proposal receives reliable feedback identifying the accepted consent, its deadline, and the action or workflow limitation affecting publication.

## Scope Authority
- Owner selected option A on 2026-09-23; latest `/plan this out!` requests a completed plan within that feedback-only scope.
- `RESEARCH_BRIEF.md` decisions d1–d14 define settled behavior; `RESEARCH_TECHNICAL.md` records the existing implementation and offline observations.
- No unresolved product decisions; implementation architecture remains the architect's responsibility.

## User Stories
| ID | Story |
|---|---|
| US-1 | As an owner, I want proposal and acceptance replies to explain approval, expiry, renewal and publication eligibility so that I know what to do next. |
| US-2 | As an owner, I want acceptance feedback to survive delivery failures and restarts so that silence or duplicated replies do not obscure whether my approval was recorded. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| Retained creation proposal | Proposal hash, relationship, source identity, eligibility | Existing durable values | Yes | Reuse current proposal authority and exact matching rules. |
| Owner creation intent | Source approval identity, proposal hash, relationship, created time, expiry | Existing durable values | Yes | Accepted consent is authoritative only after durable admission; lifetime remains 30 minutes from trusted comment creation. |
| Acknowledgment delivery | Stable acknowledgment identity and pending/delivered state | Existing delivery mechanism or narrowly added durable metadata | Yes | Separate from admission, binding and publication; reuse identical identity during recovery. |

### Relationships
- Acceptance acknowledgment → admitted intent → retained proposal.
- Admission or acknowledgment does not create a run, publication result, additional grant or refreshed deadline.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|---|---|---|---|
| Existing owner comment | `/mcp-create <hash> <relationship>` | Existing numeric-owner authentication and policy | Add acceptance reply after durable success; preserve input syntax and rejection behavior. |
| Existing issue reply | Proposal presentation | Existing delivery path | Explain deadline basis, separate start action and applicable publication limitation. |
| New issue reply behavior | Acceptance acknowledgment on source issue | Admitted durable intent | Full hash, relationship, original absolute UTC expiry, next action or limitation, and renewal guidance. |

## Business Rules
- Acceptance reply confirms recording of the exact proposal hash and relationship and explicitly states that approval alone starts no publication.
- Expiry is an absolute UTC timestamp; explain that the 30 minutes begins when the owner posts the approval and that an eligible publishing attempt must bind it before the deadline.
- Eligible legacy Research publishing workflows require a further ordinary comment to start an attempt; wording does not guarantee the attempt will publish successfully.
- Graph Research and other unavailable publication paths explicitly explain the limitation; another ordinary comment does not unlock graph publication.
- Proposal and acceptance replies use consistent eligibility guidance and do not invent mode-switching or permission workarounds.
- Renewal requires a new approval comment; the same proposal may be reused only while current and eligible, otherwise a new proposal is needed.
- Acknowledgment uses durable admitted values, not mutable approval comment text or a deadline recomputed from processing/retry time.
- Keep existing owner authentication, proposal/relationship matching, source scope, mode eligibility and expiry validation unchanged.
- Reuse current rejection explanations and correction guidance; infrastructure errors remain pending rather than being relabeled policy rejection.
- Cover newly processed approvals and admitted approvals still pending processing; do not retrospectively acknowledge already-consumed approvals.

## Edge Cases & Error Handling
- Admission fails before durable success → no acceptance claim; retain existing rejection or infrastructure-failure behavior.
- Admission succeeds and delivery fails → acknowledgment remains pending; retry preserves the same intent, source identity and expiry.
- Reply is posted but response or local completion is lost → reconcile the same acknowledgment before posting again; produce one visible acceptance reply.
- Restart between admission, reply delivery and consumption → complete pending acknowledgment without new consent, deadline extension or duplicate visible reply.
- Acknowledgment arrives after expiry or other loss of usability → report the historical acceptance and original deadline without claiming the grant is currently usable.
- Fresh approval after expiry → validate the retained proposal under existing rules and create a new intent with a deadline based on the new comment; replaying the old comment never renews it.
- Proposal becomes stale or ineligible → renewal guidance requires a new eligible proposal and does not promise reuse.
- Graph workflow receives approval → truthful feedback, no grant binding or publishing tool added to graph Research.

## Existing Features to Reuse
- `internal/daemon/mcp_creation.go`: command admission and recovery routing.
- `internal/store/mcp_creation.go`: proposal and owner-intent authority, expiry and binding.
- `internal/report/reply.go` and `creation_rejection.go`: proposal rendering and rejection explanations.
- Existing durable reply delivery/reconciliation and creation presentation recovery.
- `internal/turn/mcp_runtime.go`: legacy/graph publication capability boundary.

## Out of Scope
- Approval-triggered execution, enabling graph publication, new publishing authority or automatic mode switching.
- Changes to approval lifetime, input grammar, authentication or permissions.
- Scheduled expiry reminders, historical acknowledgment backfill, proposal drafting redesign or general rejection redesign.
- Production daemon changes, live-host verification or implementation during this Plan run.
- Protected instruction-tree changes; no `.claude/**` or `.codex/**` split is required.

## Acceptance Criteria
- [ ] AC-001: A durably admitted approval receives a source-issue acknowledgment identifying the full proposal hash and relationship, confirming acceptance and stating that approval alone starts no publication.
- [ ] AC-002: Acceptance feedback displays the stored original expiry as an absolute UTC timestamp and explains its trusted-comment 30-minute basis and the requirement to bind an eligible attempt before that deadline.
- [ ] AC-003: Proposal and acceptance guidance distinguish eligible legacy Research start-by-ordinary-comment behavior from graph or otherwise unavailable publication; graph guidance explicitly says another ordinary comment does not enable publication.
- [ ] AC-004: Proposal and acceptance guidance requires fresh approval for renewal, permits reuse only of a current eligible proposal, and otherwise directs the owner to obtain a new proposal.
- [ ] AC-005: Failed or uncertain acknowledgment delivery and restart at admission/delivery/consumption boundaries remain recoverable and yield one visible acknowledgment with the original intent and deadline; processing does not mark undelivered acknowledgment complete.
- [ ] AC-006: Delayed feedback remains historically accurate after expiry or lost usability and never asserts current grant usability; retries or replay of the original approval do not extend authority, while a newly admitted approval derives its deadline from its own trusted comment timestamp.
- [ ] AC-007: Existing authentication, exact proposal/relationship matching, source and mode checks, expiry enforcement, rejection explanations and infrastructure-failure pending behavior remain intact; no acceptance is reported before durable admission.
- [ ] AC-008: Approval acknowledgment starts no run and changes no graph publishing or binding permissions; existing legacy eligible-binding behavior remains intact.
- [ ] AC-009: New approvals and admitted-but-pending approvals receive feedback under this behavior, while already-consumed approvals receive no retrospective acknowledgment.

## Candidate Evidence Obligations
| Criteria | Proof method | Available source / version bounds | Missing prerequisite |
|---|---|---|---|
| AC-001–AC-004 | Renderer assertions plus daemon/presentation behavior tests for supported legacy and graph paths | Current `internal/report`, daemon and turn fixture infrastructure; research baseline revision `21b40106fdbc51bbf5fade401067e98b1c76c8f9` | New implementation and executed tests in Implement. |
| AC-005–AC-006, AC-009 | Real SQLite fixture tests with delivery failure, uncertain response, restart, delayed processing, renewal and prior consumption scenarios | Current daemon creation-success tests, durable delivery fixtures and store creation tests | New fault scenarios and executed evidence in Implement; no live GitHub required. |
| AC-007–AC-008 | Behavioral regression tests for rejection queues, expiry, approval dispatch and graph/legacy grant/tool access | Current daemon rejection tests and turn creation-proposal-catalog tests | Execute preserved and added checks against the implementation snapshot. |
| All | Repository required aggregate checks including `make check` | Checkout tooling; no integration build tag | Execute in Implement; Plan provides no implementation verification. |

## Decisions, Assumptions and Risks
- Decision: feedback-only option A is owner-approved; no additional scope confirmation is needed.
- Assumption: reuse existing authority and delivery primitives, adding acknowledgment metadata only if required; based on brief a2.
- Assumption: upfront expiry guidance satisfies the warning request; no scheduled notification; based on brief a3.
- Assumption: already-consumed approvals are excluded from backfill; based on brief a4 and settled shaping response.
- Risk: accepted consent may expire before its acknowledgment is delivered; historical wording and original deadlines are mandatory.
- Risk: uncertain delivery requires reconciliation, not blind reposting; fault evidence must cover the external-success/local-failure boundary.
- Evidence limitation: prior research proves checkout behavior with offline fixtures only; deployed revision and live GitHub behavior remain unverified and are not claimed.
- Documentation limitation: referenced `docs/decisions/nested-agent-evidence.md` and `docs/story-contract-instruction-manifest.md` are absent in this checkout; supplied skill/factory contracts govern. No nested-agent runtime capability proof is required by this feature.
