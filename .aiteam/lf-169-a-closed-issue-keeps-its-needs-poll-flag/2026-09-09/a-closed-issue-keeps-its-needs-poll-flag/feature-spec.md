# Feature Spec: a-closed-issue-keeps-its-needs-poll-flag
# Status: APPROVED
# Date: 2026-09-09

## Goal
- Retire ordinary automatic retry backlog after authoritative issue closure so abandoned work stops causing repeated issue lookups and database writes while recoverable work and reporting obligations remain intact.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As a daemon owner, I want confirmed closed issues removed from ordinary polling backlog so later ticks and restarts stop retrying them and open work continues to progress. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Session | needs_poll | Existing persisted boolean | Yes | Cleared after authoritative closed observation in ordinary backlog admission; retained when closure cannot be confirmed or clearing fails. |
| Session | Identity, state, comment watermarks | Existing fields | Yes | Preserve session identity, local state and consumed-comment positions when retiring backlog. |
| Run/report | Outstanding report state | Existing fields | Yes | Closure retirement does not erase or acknowledge owed reports. |

### Relationships
- Existing session identifies its registered repository and forge issue; no new entity, relationship or schema is needed.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Existing daemon poll | Ordinary persisted-backlog admission | Existing configured forge credentials | Successful issue lookup explicitly reporting closed retires the ordinary retry flag without starting an agent. |
| Existing forge discovery | Reopened issue appears in normal open discovery | Existing configured forge credentials | Reuse retained session and watermarks; pending eligible comments remain processable. |

## Business Rules
- Only a successful authoritative closed observation is sufficient for terminal clearing; failure, inaccessible/deleted responses and unknown states are not closure evidence.
- Retire the durable flag at the closed observation; a failed retirement must remain retryable and follow existing error handling.
- Confirmed closed ordinary backlog candidates start no worker/agent, increment no dispatched count, and release admission so useful candidates can proceed during that tick.
- Preserve existing successful-drain clearing and error/lock/deferred retention behavior.
- Reopening resumes through existing normal discovery; terminal retirement does not promise continued direct polling of a closed issue or immediate reopening detection before normal discovery observes it.
- Correct backlog lifecycle and admission comments to describe terminal clearing and actual issue-read behavior without claiming concurrency enforces a request budget.

## Edge Cases & Error Handling
- Existing idle or failed flagged session whose issue is closed → retire backlog without converting its local state or altering watermarks.
- Repeated ticks and daemon/store restart after successful retirement → no direct issue GET attributable solely to that retired backlog.
- Lookup failure, including authentication/access/deletion ambiguity or rate limiting → preserve backlog and existing retry/backoff behavior.
- Store clear failure → report through established error handling, retain retry eligibility, and release admission.
- More closed backlog candidates than worker slots together with eligible open work → closed candidates consume no permanent slots and open work can dispatch.
- Reopened issue discovered normally → use the original session and process previously unconsumed eligible owner comments without replaying consumed comments.
- Interrupted run with an owed report and subsequently closed issue → ordinary backlog retirement does not suppress existing report reconciliation/delivery.
- Issue closes after an open listing observation → retain existing observation-race semantics; no extra mandatory pre-execution lookup is introduced.

## Out of Scope
- Independent per-tick issue-read budgets, scheduler/fairness redesign, configuration changes and performance quotas.
- Scheduled-work closure policy, new user-abandonment state, deletion/permanent-access-loss policy and forced reopening scans.
- Reconciling historical dispatch counters with an unavailable deployed binary revision or changing counters without a reproduced defect.
- Live runtime-state edits, daemon restart, migration, new public endpoints or permission changes.

## Assumptions
- From RESEARCH_BRIEF.md: durable stale flags are confirmed; current closed-materialisation code already releases admission and does not count a dispatch.
- From the brief's recommended scope and this headless Plan instruction: confirmed closure retires ordinary automatic retry intent, while session history, comment watermarks and owed reports survive.
- Reopening relies on existing normal open-issue discovery; no stronger immediate-detection guarantee is added.
- Lookup errors retain backlog because unsuccessful reads do not establish terminal closure.
- One cohesive story covers the lifecycle fix, protocol documentation and behavioral regression coverage; request budgets and schedule policy remain separate concerns.

## Acceptance Criteria
- [ ] AC-1: Given a preexisting ordinary flagged session and a successful issue lookup reporting closed, polling clears its persisted needs_poll flag, starts no worker or agent, and records no dispatch for that candidate.
- [ ] AC-2: Following successful closure retirement, subsequent ticks and a daemon/store restart do not fetch that issue solely from ordinary local backlog and the retired flag remains clear.
- [ ] AC-3: With more closed flagged candidates than configured worker slots and eligible open work, admission is released for every closed skip and open work can dispatch in the same tick.
- [ ] AC-4: Failed issue lookups and failed persistence of terminal clearing retain retry eligibility and release admission through existing error handling; rate-limit handling remains intact.
- [ ] AC-5: Terminal clearing preserves session identity, local state, comment watermarks and owed reports; existing abandoned-run report reconciliation still delivers its required report for a closed issue.
- [ ] AC-6: When normal discovery observes that a retired issue has reopened, it reuses the existing session and can process previously unconsumed eligible owner comments without replaying consumed comments.
- [ ] AC-7: Existing clean-drain clearing and error, lock and deferral retention behavior remain covered and passing; protocol and admission comments describe terminal closure clearing and do not promise a concurrency-derived issue-read budget.
