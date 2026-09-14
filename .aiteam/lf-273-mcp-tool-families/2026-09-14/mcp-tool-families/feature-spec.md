# Feature Spec: mcp-tool-families
# Status: APPROVED FOR PLAN AUTHORING
# Date: 2026-09-14

## Goal
- Give bound Research, Implement and Validate agents concrete MCP tools for validation, repository-local issue discovery and authorized publication, workflow evidence, advisory activity and durable recovery, using the landed groundwork.

## Scope provenance and decisions
| Decision | Source / interpretation |
|---|---|
| Include all five families, `issues_create_batch` and `activity_get`. | Issue completion criteria, research recommendation, and owner's run-0003 instruction to plan the accepted direction. |
| Preserve factory-held grants and private one-use permits; no caller-carried approval token. | Settled run-0002 reuse direction; current `CreationAuthorizer` and `CreationStore` contract supersede conflicting issue wording. |
| Register callable family handlers and expose reusable creation preparation/reconciliation operations in #273. | Agreed family-versus-integration boundary. |
| Defer actual host registration, trusted owner intake/next-attempt wiring, production recovery scheduling and authentic host verification to #274. | Agreed handoff; #274 is not a prerequisite. Existing documentation still names #264 for some production seams; implementation documentation must record the handoff without claiming those seams work. |
| Keep Plan/Spike's existing communication behavior; new families are scoped to Research/Implement/Validate. | Research permission recommendation; no request to broaden other modes. |
| No protected instruction-tree changes. | Tool-family implementation and public technical documentation suffice; installed agent instructions are host-adoption work. |

## User needs
| Need | User story |
|---|---|
| Validation | As an agent, I want correction diagnostics before recording or publishing a proposed contribution. |
| Discovery | As an agent, I want bounded issue titles and recognized mode labels from my bound repository. |
| Publication | As an authorized agent, I want a comment or explicitly granted researched batch to publish with recoverable identities and safe uncertain outcomes. |
| Evidence | As an Implement or Validate agent, I want to record my permitted checkpoint through the existing workflow engine. |
| Activity | As an agent and owner, I want bounded current progress without treating progress as proof or authority. |
| Recovery | As a scoped agent, I want to know what happened to a retained request after a lost reply. |

## Existing behavior to reuse
| Owner | Reused behavior / inspected source |
|---|---|
| `submission.ToolProfile`, `turn.ToolDispatcher`, `AttemptGuard` | Current catalog has only three `communication_*` tools; immutable profile/digest/lease checks remain authoritative (`internal/submission/tools.go`, `internal/turn/mcp_tools.go`). |
| `AgentResultService`, `agent.ParseContribution`, `SubmissionSession` | Shared application and stage validation, snapshot checking, durable checkpoint recording (`internal/turn/agent_results.go`, `internal/turn/submissions.go`, `internal/agent/contribution.go`). |
| `CreationAuthorizer`, `CreationStore`, `CanonicalCreation` | Exact reviewed envelope, retained owner intent, grant expiry, one-use dispatch and volume accounting (`internal/turn/mcp_creation.go`, `internal/store/mcp_creation.go`, `internal/submission/creation.go`). |
| `github.Client`, existing follow-up/story effects | Forge access, redacted comments, marker/author recovery and uncertain-write precedent. |
| `agentresult.ReduceProgress` | Sequence replay/conflict/stale rules, message/count/retention bounds and factory-owned terminal cleanup (`internal/agentresult/progress.go`). |
| Submission client and durable receipts | Existing communication identity compatibility; extend family identity/correlation and scoped projection (`internal/submission/client.go`). |

## Data model and validation
| Entity | Fields | Constraints |
|---|---|---|
| Bound family authority | Repository, source task, run/attempt, mode, frozen story revisions, explicit existing comment targets | Factory supplies immutable values; caller cannot select another repository, authority, stage or target grant. |
| Issue query | `state`, `page`, `per_page`, `include_mode_labels`; local `issue_number` for get | Assume open default, open/closed only; page defaults 1 and is positive; per-page defaults 30 and is 1–100; positive issue numbers; reject unsupported/unknown fields. Get defaults open and refuses a state mismatch. |
| Issue view | `number`, `title`, optional `mode_labels`; page result next-page metadata | No bodies, arbitrary labels, PRs, comments or account details. Only normalized recognized mode labels. Listing performs bounded remote page work, including pages containing PRs. |
| Comment input | Existing local issue number, message body | Target must be explicitly bound for commenting and remain an issue; nonblank valid UTF-8, bounded by existing forge/redaction limits; no caller-selected receipt identity or creation authority. |
| Creation input | Exact reviewed artifact content and linked/attach relationship | Use existing version-1 envelope internally with factory-bound repository/source; reject mismatches if envelope scope is supplied. 1–5 artifacts; title 1–256 bytes and body 1–65,536 bytes after preparation, valid UTF-8/nonblank; at most 16 positive link targets; attach parent equals source issue, linked has no parent. Shared canonical validator owns these rules. |
| Prepared creation | Canonical envelope/hash, rendered/redacted final bytes and relationships | Markers and links included before retention/approval; preparation publishes nothing and grants nothing; changed bytes require a fresh retained proposal and explicit approval. |
| Durable family request | Stable request ID, family/method, bound scope, payload identity, disposition and underlying receipt/operation reference | Journal correlation before transmission; identical retry keeps identity; family names prevent cross-family collisions; lookup cannot treat possession of ID as authority. |
| Publication result | Operation state, per-artifact issue ID/number or comment ID, relationship outcomes | Confirmed identities only when observed; partial/unknown effects remain uncertain; no full-batch success while an artifact/relationship is unresolved. |
| Evidence input | Existing typed stage contribution, story selector, capture or retained snapshot/check references, reason where applicable | Existing stage/workflow/frozen-revision validation; no new competing evidence schema; Validate cannot capture or execute checks. |
| Activity | Positive sequence and nonblank UTF-8 message | Existing 2 KiB message, 1,024 accepted updates, 32 retained replay identities, terminal retention at most 24 hours; factory time/active state only. |
| Receipt view | Request disposition plus separate input, engineering, acceptance and remote-effect dimensions where applicable | Pending/rejected/uncertain/confirmed remain distinct; no private permits, credentials or unrelated payloads. Reads/preflight retain bounded disposition/reference, not a durable full issue catalog. |

## API and permission matrix
| Tool | Research | Implement | Validate | Semantics |
|---|---|---|---|---|
| `feedback_preflight` | Yes | Yes | Yes | Explicit kind selects existing application contribution, permitted stage contribution, or creation batch validation; no domain mutation, proof capture, proposal retention or authority. |
| `issues_list`, `issues_get` | Yes | Yes | Yes | Repository-bound minimal reads; discovery never confers publication authority. |
| `issues_comment` | Bound targets only | Bound targets only | Bound targets only | Ordinary durable write; independent of creation gate/grant. |
| `issues_create_batch` | Gate-visible; grant required for dispatch | No | No | Reuse existing authorizer, exact content and private dispatch permit. |
| `evidence_submit` | No | Existing engineering/legacy stage rules | Existing retained assessment stage rules | Shared workflow operations; permitted blocked/invalidation outcomes remain stage-specific. |
| `activity_update`, `activity_get` | Yes | Yes | Yes | Advisory per-attempt status; get returns unavailable/cleared after process-local loss. |
| `receipts_get` | Scoped read | Scoped read | Scoped read | Recover bounded request state across families; lookup creates no recursive receipt and never redispatches. |

## Business rules and error handling
- Deny unknown tools/fields, invalid bounds, missing or mismatched immutable bindings, expired/cancelled/replaced attempts and direct calls forbidden by mode; hiding a tool is insufficient enforcement.
- Preserve existing communication tool names, semantics, journal identity and prohibition on publishable Plan content through MCP communication.
- Repeat shared validation on submission; successful preflight neither reserves work nor promises later acceptance.
- Persist comment intent before remote I/O and serialize dispatch across concurrent callers/restart. Reconcile with authenticated bot identity, exact marker/content and authorized target. An ambiguous POST never permits blind reposting.
- Retain request identity even when transport fails. If no durable record exists, distinguish that from rejected/pending/uncertain operation; a current live scope may recover permitted old receipts without reviving old authority.
- Creation uses the enabled startup gate, Research mode, exact current proposal and a valid attempt-bound grant; arguments, prose, labels, repeated comments and copied approval syntax cannot mint authority.
- Reuse one batch/five artifacts per attempt, twenty artifacts per repository rolling 24 hours and 30-minute approval expiry bounded by lease expiry; unknown/partial effects remain charged. Duplicate/replayed requests never obtain a second permit; expired grants refuse dispatch/replay and scoped receipt reads remain separate.
- Preparation and trusted approval APIs are factory-only; no model-callable grant/proposal-retention shortcut. Link by default; attach only where exact relationship approval permits it. Never silently add or modify reviewed publication bytes after approval.
- Factory-only reconciliation observes existing effects and records facts; it cannot elect a fresh dispatcher or recreate ambiguous artifacts. A partial batch retains confirmed identities and unresolved relationship outcomes.
- Evidence accepted as input is distinct from applied engineering/assessment. Preserve separated and legacy semantics, snapshot freshness, historical acceptance, and owner-controlled mode transitions.
- Activity cannot complete stories, approve scope, renew leases/grants, record executed checks or change workflow decisions. Durable update disposition may survive while current process-local display is unavailable.
- Propagate cancellation through handlers and forge I/O; separately bounded factory recovery may record known facts after cancellation but cannot obtain new agent dispatch authority.

## Assumptions
| Assumption | Basis |
|---|---|
| Use numeric page pagination, defaults 1/30 and a 100-item page cap; get honors open/closed filter. | Existing GitHub page API and issue's bounded open-default direction; exact numeric defaults were not owner decisions. |
| Generalized receipts cover bounded read/preflight dispositions as well as writes; receipt lookup itself is nonrecursive. | Explicit cross-five-family recovery criterion and research recommendation. |
| New comment targets come from an explicit immutable factory grant, never from a successful discovery read. | Existing authorized-target requirement; production construction is #274. |
| Reusable preparation/presentation is testable without live report/intake wiring. | Accepted #273/#274 boundary. |
| Acceptance here concerns offline executable family semantics under controlled forge/socket/SQLite fixtures. | Authentic host integration explicitly remains #274; no present-day host capability claim. |

## Acceptance criteria candidates
- [ ] AC-001: Existing bound MCP transport discovers and executes every applicable tool in the matrix; each forbidden direct call fails without a domain effect, including Plan/Spike restrictions and missing/mismatched/expired/revoked authority.
- [ ] AC-002: Existing communication discovery, helper behavior, identity correlation and publishable-Plan MCP refusal remain compatible; family additions are included in immutable permission binding.
- [ ] AC-003: Feedback delegates application, checkpoint and batch validation to shared owners, returns correction diagnostics and creates no domain submission, captured proof, retained proposal or grant; actual submissions validate again.
- [ ] AC-004: Issue list/get default open, honor explicit closed state, validate positive local numbers and bounded page parameters, and refuse caller-selected foreign repository scope.
- [ ] AC-005: Issue responses expose only number/title/optional recognized mode labels and pagination metadata, exclude PRs and bodies/arbitrary labels, and enforce bounded work/output even for pages containing PRs.
- [ ] AC-006: Comments require an explicitly authorized existing issue and valid bounded redacted content; enabled or disabled creation policy does not change ordinary comment authorization.
- [ ] AC-007: Comments persist intent before dispatch and return a durable receipt with confirmed comment ID when known; concurrent identical retries, restart and lost replies cannot cause another POST for an already dispatched operation.
- [ ] AC-008: Comment reconciliation accepts only matching target, authenticated bot and exact marker/content; ambiguous outcomes remain uncertain and receipt lookup does not publish.
- [ ] AC-009: Factory-only creation preparation returns reviewable final bytes, relationships and canonical hash, retains an exact proposal without publication/grant, and requires new approval after any prepared-content change.
- [ ] AC-010: Creation is hidden/refused with gate off and refused outside Research or without a valid live attempt-bound grant; arguments, GitHub content, arbitrary tokens and comment volume cannot grant permission or expose the private permit.
- [ ] AC-011: Creation refuses absent/expired/reused/mismatched grants, changed exact content/source/relationship, stale attempts and wrong approval provenance using the existing primitive; accepted reservation/dispatch never reimplements its authorization or quota decisions.
- [ ] AC-012: Creation enforces existing attempt/repository caps and expiry, isolates repository budgets and retains charges for uncertain/partial outcomes; repeated, concurrent and spam-like identical calls reconcile one operation with at most one dispatch owner.
- [ ] AC-013: A valid fixture-bound grant publishes only reviewed artifacts and relationships, returns confirmed per-artifact identities, and persists partial/lost-response/restart outcomes without blind recreation or full-batch confirmation while unresolved.
- [ ] AC-014: Evidence calls apply the shared checkpoint engine rather than input-only result storage; Implement engineering/legacy and Validate assessment obey frozen story/revision, snapshot/check references, permitted capture/invalidation and existing workflow version rules.
- [ ] AC-015: Evidence refusal/retry and scoped recovery preserve separate input/engineering/acceptance/effect states, historical accepted snapshots and no automatic mode chaining; Validate cannot capture or execute tests through this tool.
- [ ] AC-016: Activity update/get reuse the reducer's sequence/replay/conflict/stale and size/count/retention behavior, synchronize concurrent calls and return unavailable/cleared state after process-local loss.
- [ ] AC-017: Activity updates and lifecycle cleanup never change workflow, evidence, scope or authority and never extend lease/grant/terminal retention deadlines.
- [ ] AC-018: New family requests obtain stable journal correlation before transmission, isolate family/scope identities, recover identical lost-response retries and preserve existing communication identity behavior.
- [ ] AC-019: Scoped receipts recover bounded dispositions across feedback, issues, evidence, activity and receipt lookup semantics, including confirmed publication identities and honest missing/pending/rejected/uncertain states, without recursive recording or redispatch.
- [ ] AC-020: Unauthorized cross-run/repository/attempt receipt access fails; separately authorized current readers may inspect permitted history after old authority is revoked without restoring that authority or exposing grants/permits/secrets.
- [ ] AC-021: Controlled end-to-end MCP pipe/socket/SQLite/HTTP fixture calls verify the matrix and cancellation/recovery invariants; `make check` passes and documentation describes callable contracts plus the remaining #274 production/authentic-host handoff.

## Criterion evidence inventory
| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-001 | Table-driven discovery/direct-call assertions through real bound broker fixture | Existing `internal/submission/tools_test.go`, `internal/turn/mcp_tools_test.go`; family cases to add | Current local Go implementation; synthetic factory bindings, no host claim | Implement and retain executed checks |
| AC-002 | Compatibility and immutable-digest regression tests | Existing communication/client/dispatcher tests | Existing communication contract retained | Implement and retain executed checks |
| AC-003 | Shared-validator parity and no-domain-write assertions | Existing contribution and agent-result tests; batch validator | Current typed contract/workflow versions; no remote effect | Implement and retain executed checks |
| AC-004 | HTTP fixture query/bound tests | Existing `internal/github` HTTP fixtures | Simulated forge responses, repository-local contract | Implement and retain executed checks |
| AC-005 | PR-only/mixed pages and projection/budget assertions | Existing GitHub issue fixtures | Simulated pagination; no live GitHub completeness claim | Implement and retain executed checks |
| AC-006 | Target/refusal and redaction tests | Existing redaction/comments and turn fixtures | Explicit fixture factory grants | Implement and retain executed checks |
| AC-007 | SQLite concurrency/reopen plus accepted-POST/lost-reply HTTP fixture | Existing store/forge patterns; new generic journal tests needed | Local dispatch guarantee, not remote exactly-once proof | Implement and retain executed checks |
| AC-008 | Authenticated-marker reconciliation and no-repost assertions | Existing story/follow-up recovery fixtures | Controlled matching/malicious/ambiguous responses | Implement and retain executed checks |
| AC-009 | Exact render/hash/retention and mutation refusal tests | `CanonicalCreation`/authorizer tests; presenter to add | Factory-only operation; actual intake lifecycle excluded | Implement and retain executed checks |
| AC-010 | Tool-profile and complete server refusal matrix | Existing authorizer/store tests | Trusted synthetic identities, public content treated as data | Implement and retain executed checks |
| AC-011 | Expiry/content/provenance/reuse boundary assertions | Existing `mcp_creation_test.go` and serialization tests | Existing primitive, injected factory time | Implement and retain executed checks |
| AC-012 | Quota/repository/retry/concurrency fixture assertions | Existing creation store fixtures | Fixed current caps and rolling-window semantics | Implement and retain executed checks |
| AC-013 | Real SQLite and HTTP fixture publication/partial/restart checks | Existing follow-up recovery precedents; family publisher to add | Simulated GitHub; no promise of remote exactly-once delivery | Implement and retain executed checks |
| AC-014 | Shared checkpoint parity, capture/snapshot and stage refusal tests | Existing submission/engineering/assessment fixtures | Persisted legacy/separated workflow versions | Implement and retain executed checks |
| AC-015 | Durable evidence retry and decision/history assertions | Existing submission receipt/store fixtures | Local workflow effects; owner mode selection retained | Implement and retain executed checks |
| AC-016 | Reducer parity, concurrent service calls and cleared-state assertions | Existing progress reducer tests | Injected time; explicitly process-local display | Implement and retain executed checks |
| AC-017 | Before/after workflow/authority invariants | Progress and store fixtures | No real host or timer-lifecycle claim | Implement and retain executed checks |
| AC-018 | Journal lost-reply/reopen and cross-family collision tests | Existing submission client fixtures | Actual local journal/socket, synthetic requests | Implement and retain executed checks |
| AC-019 | Family-spanning durable receipt projection tests | Existing result/submission/creation stores | Bounded read dispositions; receipt lookup nonrecursive | Implement and retain executed checks |
| AC-020 | Cross-scope/current-reader/revocation and secret-exclusion tests | Existing AttemptGuard/store fixtures | Trusted factory-supplied reader grants only | Implement and retain executed checks |
| AC-021 | Offline MCP-to-family fixtures, executed `make check`, scoped documentation review | Existing pipe/socket/SQLite/HTTP facilities and pinned checks | Current checkout/toolchain; no live nested agent invocation | Implementation checks required; authentic host evidence explicitly deferred to #274 |

## Out of scope and evidence limits
- Native Claude/Codex startup, permission/config merging, real-host discovery/teardown/resume/fallback, and authentic present-day host verification (#274).
- Production trusted-owner intake, next-attempt grant binding and recovery scheduling beyond reusable handler/preparation/reconciliation APIs; #274 handoff must enumerate still-missing seams.
- New authorization primitive, exposed bearer grant/token, approval through model content, automatic mode selection, shell/execution/waiver tools, or creation outside Research.
- Full issue bodies/comments/arbitrary labels in discovery, cross-repository access, new unrestricted comment targets, or automatic issue attachment.
- Durable activity history/dashboard, whole issue-catalog receipt snapshots, new MCP transports/capabilities, and protected installed instruction-tree edits.
- Live nested Codex capture, sandbox expansion or paid nested-agent execution as an implementation/CI requirement. Existing authentic host evidence is not supplied; no candidate criterion claims it, and the #274 external prerequisite remains outstanding.

## Risks
| Risk | Required treatment |
|---|---|
| Forge accepted a write but local acknowledgment is absent | Preserve uncertainty and known identities; refuse blind repetition. |
| Journal identity differs between family calls | Namespace family/method and bound scope while keeping communication compatibility. |
| Evidence facade records only JSON input | Exercise shared checkpoint state transitions and snapshots in acceptance checks. |
| Approval hash changes during rendering/redaction | Prepare final reviewed bytes before retention and hash; reject mismatches. |
| Concurrent/restarted publication elects another writer | Durable reservation/dispatch state and existing private creation permit; no transaction over network I/O. |
| Fixture success is reported as real-host capability | State offline proof bounds and retain #274's authentic verification obligation. |

## Open decisions
- None; reasonable interface defaults are recorded above, and settled owner decisions are not reopened.
