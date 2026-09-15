# Feature Spec: adopt-the-shared-communication-contract
# Status: APPROVED SCOPE — ready for architecture and typed story authoring
# Date: 2026-09-15

## Goal

- Make the existing shared communication contract authoritative across all five modes so agents can finish, resume and recover work consistently without weakening workflow or acceptance authority.
- Owner approved the run 0003 draft in run 0004: “Plan is ok, make it firm!”
- Deliver two explicit milestones: production adoption, then verified retirement of superseded inputs.

## User Stories

| Area | User need |
|---|---|
| Finalization | As an operator, I want Research, Plan, Implement, Validate and Spike to submit through shared validation so the factory can distinguish recorded input, engineering, acceptance and delivery. |
| Continuation | As a continuing agent, I want authoritative prior results and revisions so I can safely make sparse edits without reconstructing state from prose. |
| MCP composition | As an operator, I want the already-built tools connected to production authority, publication and recovery so enabled capabilities actually work. |
| Progress | As an operator, I want bounded advisory progress through termination and cleanup without granting workflow authority. |
| Retirement | As a maintainer, I want obsolete ingress removed after compatibility checks while existing work and historical records remain usable. |
| Instructions | As an agent, I want matching canonical and installed instructions that describe actual production behavior. |

## Existing Features to Reuse

| Responsibility | Existing owner / seam |
|---|---|
| Result validation, revisions, history, receipts | AgentResultService and internal/agentresult; internal/turn/agent_results.go |
| Engineering completion proof | SubmissionSession, retained engineering checkpoints and ValidateAgentResultCompletion; internal/turn/agent_result_aggregates.go |
| Launch authority and transport | Existing MCP runtime, AttemptGuard, private broker/socket, CLI/helper and native Claude/Codex integration |
| Publication and uncertain outcomes | IssuePublicationService and CreationPublicationService with existing reconciliation APIs |
| Exact-content creation authority | CreationAuthorizer, retained proposal and authenticated owner-control intake |
| Advisory progress | ActivityService update/read/lifecycle and run-scoped lease |
| Legacy compatibility | Supported story-contract upgrade/admission, StoryProgress recovery/delivery, live v2 Plan publication and historical migrations |

## Data Model

| Entity | Required meaning | Constraints |
|---|---|---|
| Bound attempt | Factory-owned mode, identities, workflow/content/submission versions and grants | Reuse current binding; agents cannot invent identity or extend lifetime. |
| Result baseline | Authoritative selected result fields, stable IDs and observed revision | Explicit empty baseline for cold starts; scoped history only; retain retired ID rules. |
| Result update | Existing typed final outcome, questions, comment, artifact references and applicable workflow contribution | Validate the complete merged result, including unchanged fields and evidence references. |
| Receipt | Durable request correlation and separate input/engineering/acceptance/remote-effect states | Recorded input or pending effects never imply completion; replay must preserve identity. |
| Publication intent | Exact approved content, authorized target/relationship and durable operation state | Reuse retained proposal and dispatch/reconciliation state; no blind redispatch after an uncertain reply. |
| Progress lease | Bound attempt, advisory state, terminal state and retention lifetime | No acceptance/completion authority; factory owns terminal transition and expiry. |
| Compatibility disposition | Input path, supported versions/cohorts, pending effects, migration/completion prerequisite and removal proof | Evidence-backed disposition for each path; no manual persisted-version rewriting. |

### Relationships

- Attempts authorize scoped result reads/updates, receipts, target publication and progress.
- Engineering/assessment contributions reference existing frozen story identities and retained evidence; shared application results do not replace those authorities.
- Publication intents and receipts survive attempt termination where recovery is still owed.
- No speculative new schema is required by this spec; architect chooses any necessary additions within existing owners.

## API / Interface Changes

| Surface | Required behavior |
|---|---|
| MCP communication tools | Connect shared final-result submission to actual post-exit processing for every supported mode. |
| CLI/helper | Adapt existing typed Plan and engineering paths to shared semantic validation/receipts; preserve factory-controlled publication. |
| Structured terminal output | Keep Validate able to return its complete structured assessment without shell/delegation; reconcile with live submissions through one semantic path. |
| Prior state delivery | Supply a sufficient authoritative snapshot in launch context or grant scoped shared-service reads; include stable IDs/base revisions and permitted historical receipts explicitly. |
| MCP family composition | Register existing authorized comment publication, eligible creation and activity reads alongside current handlers; enforce profile and target grants in discovery and calls. |
| Owner creation controls | Present retained exact title/body/source links and relationship with existing approval mechanism; bind authenticated matching approval to the next eligible Research attempt and loaded creation gate. |
| Factory lifecycle | Schedule bounded observation of uncertain publication, reconcile durable receipts, terminate progress and clean up capabilities on every exit path. |
| Auxiliary callers | Inventory classifier/scheduler/conflict consumers and apply shared version/error conventions where semantically applicable; preserve typed payloads and document justified exclusions. |

## Business Rules and Decisions

- All five modes use the shared service for supported business results; transport adapters do not duplicate business validation or receipt logic.
- Existing helper-based typed Plan publication remains a transport/owner boundary, not an independent acceptance implementation; an MCP comment alone cannot publish or freeze stories.
- Managed and supported unmanaged/legacy launches have an explicit route: supported compatibility adapter or supported upgrade/admission requirement. Silent bypass of required finalization is not acceptable.
- Final-result processing is enforced by the factory after execution, not solely by an agent finalization skill.
- Implement completion resolves aggregate evidence and prior completion from authoritative retained records before validation; a claimed result cannot manufacture executed proof.
- Validate remains read-only, with reference-only assessments and retained checks; this feature does not authorize fresh checks, edits, shell access or delegation for Validate.
- Creation remains gated and Research-only under existing semantics; only exact authenticated owner intent authorizes it. Ordinary authorized comments do not inherit the creation gate.
- Target access and historical receipts remain explicitly scoped; use existing authorized targets rather than implicit forge-wide access.
- Progress includes Research and Spike, uses the existing advisory interface and is available only within permitted run-scoped profiles. Plan need not gain unrelated family tools.
- Canonical instructions precede independent installed-tree work: Codex owns the Claude tree and Claude owns the Codex tree in separate stories. Individual scoped checks do not require sibling parity; aggregate validation checks both.
- Record the owner preference for sequential execution of US-001 through US-010. Publication runtime composition precedes activity registration; independent installed-tree engineering gates remain separate, with a manual Codex-to-Claude switch between US-006 and US-007.
- Canonical documentation work explicitly includes docs/agent-evidence-capture.md current-versus-historical status and docs/story-contract-instruction-manifest.md ownership references for this feature's US-005/006/007/010.
- Persisted workflow versions remain unchanged through content/submission upgrade; combined workflow-v1 verification retirement is outside this cutover decision.

## Edge Cases & Error Handling

| Trigger | Required result |
|---|---|
| Cold start or restart | Deliver an explicit empty or authoritative existing baseline with fresh attempt authority. |
| Sparse update | Merge against authorized revision and validate the complete candidate; preserve untouched fields and stable IDs. |
| Stale revision or invalid evidence | Reject without applying partial changes; return field-specific diagnostics and current authorized revision/read route so an agent can correct and resubmit. |
| Lost submit reply / repeat request | Recover the same durable receipt using stable request identity; do not duplicate workflow changes or effects. |
| Conflicting live and terminal assessment | Reconcile through existing typed rules; expose conflict without granting acceptance. |
| Successful process with absent/malformed required final result | Record actionable incomplete/invalid finalization; do not report workflow success. |
| Failure, cancellation or interruption | Preserve its actual process outcome, valid checkpoints and recovery intent; missing output does not mask the primary cause and valid input does not convert failure to success. |
| Rejected final output when process has exited | Retain diagnostics and resumable state for the next authorized attempt; no fabricated automatic correction or acceptance. |
| Publication may have succeeded remotely | Observe/reconcile the retained operation with bounded factory-owned scheduling; keep remote effect pending until confirmed. |
| Expired/replaced attempt | Refuse obsolete capability calls, close progress lifecycle and retain only authorized history/recovery records. |
| Legacy work or pending effects cannot drain | Keep required readers, report retirement incomplete and prepare a concrete focused follow-up/scope revision for owner decision; do not silently declare removal done. |

## Retirement Boundary

| Input | Required disposition |
|---|---|
| Plan v1 file handoff | Inventory supported consumers; complete or upgrade eligible scopes through supported flows; remove exclusive ingress only when no supported reader dependency remains. Preserve v2 internal handoffs/publication. |
| Implement event-file ingress | Verify supported legacy scopes and pending effects are completed/drained; remove exclusive event-file ingress and wiring while preserving StoryProgress records/recovery/delivery. |
| Validate v1 assessment parser | Verify all supported scopes have a compatible structured route or completed migration; remove exclusive legacy parsing without weakening restricted terminal assessment. |
| Production-unreachable Markdown story discovery | Confirm production unreachability and remove dead discovery with its exclusive wiring/tests; retain shared live publication/render helpers. |

- Maintain separate adoption and retirement completion records; old-path removal must appear in the resulting code changes, not only a deprecation note.
- Required historical reads, old database migrations and workflow-v1 behavior survive where still supported.
- Runtime cohort/effect inventory is a read-only evidence prerequisite; do not edit daemon state or persisted versions by hand. Operator-authorized upgrade/completion is required where the supported flow calls for it.

## Acceptance Criteria

- [ ] AC-001: Research, Plan, Implement, Validate and Spike finalize supported results through shared validation and receipts across MCP, helper and applicable terminal adapters, preserving identity and workflow/publication authority; no unaccounted supported-launch bypass remains.
- [ ] AC-002: Cold, continued and restarted attempts receive sufficient explicit authoritative prior state, stable IDs and observed revisions; scoped historical access cannot expose unrelated results or receipts.
- [ ] AC-003: Sparse changes validate the full merged result; stale revisions, invalid references and malformed contributions reject without partial application and return actionable correction diagnostics; repeated requests recover durable results without duplicate effects.
- [ ] AC-004: Factory finalization handles missing/malformed output after nominal success, failure, cancellation and interruption using the stated policy, preserving checkpoints, causes and resumable state without false success.
- [ ] AC-005: Implement final completion uses authoritative aggregate/checkpoint evidence; Validate live and terminal assessments reconcile against frozen scope and retained checks without broadening its restricted execution profile.
- [ ] AC-006: Production installs eligible comment/creation handlers with exact target grants, loaded gates and authenticated exact-content approval; uncertain replies recover through bounded observation and durable scheduling without blind redispatch.
- [ ] AC-007: Eligible runs including Research and Spike expose advisory progress and scoped reads; termination, expiry, replacement and cleanup revoke activity authority without granting acceptance/completion or losing pending durable effects.
- [ ] AC-008: Auxiliary classifier/scheduler/conflict callers have documented shared version/error mappings or reasoned exclusions, retain their typed payloads and reject unsupported input according to their authority.
- [ ] AC-009: Canonical instructions, independently maintained installed bundles and current documentation match actual routes, failure/recovery semantics, authority and compatibility boundaries; aggregate checks confirm consistency without changing immutable historical releases.
- [ ] AC-010: Each of the four old inputs has a verified consumer/version/effect disposition; production-unreachable Markdown discovery and its exclusive wiring are removed while shared live helpers remain.
- [ ] AC-011: Promised Plan v1, Implement event-file and Validate v1 ingress removal is completed only after supported compatibility prerequisites are evidenced; legacy restart/history/migrations and shared live workflows remain usable. An unresolved prerequisite keeps this criterion incomplete until an explicit scope revision, never an implicit pass.
- [ ] AC-012: Appropriate production-composition, persistence/recovery and compatibility tests pass, including the failure matrix; make check and formatting checks pass. Claims about authentic Claude/Codex tool use and restricted Validate behavior remain bounded by separately supplied #274/#296 evidence and are not inferred from synthetic tests.

## Candidate Evidence Inventory

| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-001 | Production composition and adapter-path assertions through real local broker/helper boundaries | Existing runtime, submission and family catalog test seams; new cutover tests required | Repository implementation under tested bindings; not real host capability | Execute and retain all five mode/adapter coverage; authentic host proof remains external. |
| AC-002 | Cold/resume/restart and cross-scope isolation tests using persisted result history | Existing service read/history and lifecycle test seams | Test database and explicit bound revisions | New launch-baseline coverage and retained execution needed. |
| AC-003 | Merged validation, stale/retry and receipt persistence tests | Existing agentresult/update and result service tests | Tested schema/version and request identities | Extend to production adapters and retain executed results. |
| AC-004 | Table-driven runner outcome tests and restart recovery tests | Existing lifecycle/continuation mechanisms | Controlled runner outcomes prove factory behavior, not provider response behavior | Missing-output and all termination cases must be covered. |
| AC-005 | Aggregate resolver, live/terminal reconciliation tests and source review of host profiles | Existing aggregate validator and submission reconciliation tests; authentic captures not supplied | Offline assertions over retained evidence; host enforcement bounded to capture provenance | #274/#296 retain four authentic working/Validate capture obligations; unavailable observations cannot pass host claims. |
| AC-006 | Production registration/control-intake tests with controlled forge and persisted uncertain operations across restart | Existing publication, creation and family catalog fixtures | Actual repository composition with controlled HTTP responses | Add real production-constructor coverage and executed restart reconciliation proof. |
| AC-007 | Lifecycle/permission tests across modes and attempt replacement | Existing activity/runtime recovery test seams | Factory-owned lifecycle in tested environment | Spike profile and terminal cleanup coverage needed. |
| AC-008 | Source mapping review and focused version/error tests | Current auxiliary typed callers available in repository | Only applicable conventions; no business-payload unification claim | Inventory callers during implementation and retain explicit exclusions. |
| AC-009 | Scoped source/render assertions and final aggregate installed contract comparison | Shared-source manifest and isolated rendering tests exist | Source/in-memory rendering proves instruction consistency, not live invocation | Both protected-tree stories require their authorized harness; authentic delivery is separate. |
| AC-010 | Reachability/caller review, removed-path diff and live-helper regression tests | Research identifies four paths and production gate | Reviewed repository version; no inference about runtime legacy inventory | Verify current callers before deletion. |
| AC-011 | Read-only supported-cohort/effect inventory, supported upgrade/completion receipts and old-database/restart regression tests | Migration/admission code available; live cohort/drain evidence not supplied | Evidence must identify inspected cohort/version and pending effects; fixtures alone cannot establish live drain | Required inventory and any operator-controlled migration/completion remain outstanding; keep retirement incomplete if unavailable. |
| AC-012 | Retained executed checks plus source/evidence review | Existing test seams, make check and external capture procedure | Executed revision and environment recorded; external captures historical and version-bounded | No tests run in this Plan. #274/#296 authentic evidence remains separately unsatisfied until supplied. |

## Assumptions and Risks

- Approved draft and research establish scope; current focused source inspection confirms shared-service/read/aggregate and runtime composition seams still exist.
- Existing authorization semantics are sufficient; presentation uses the existing retained proposal and owner-control flow without adding a new approval language.
- No live legacy cohort inventory or authentic host captures were supplied. Retirement feasibility and real-host claims therefore retain explicit evidence prerequisites.
- #274/#296 are intentionally open external host-verification tracks; #264 neither recreates their capture work nor claims they are complete. Any affected acceptance claim still needs that evidence.
- Planning is firm on required behavior; exact package-level changes and independently deliverable story dependencies belong to architecture and typed story authoring.

## Out of Scope

- New MCP server, replacement native provider integration or MCP-specific business validation.
- Forcing artifact contents, vendor stdout, usage transcripts or process diagnostics into the business-result schema.
- Implicit migration of persisted workflow versions or retirement of combined workflow-v1 verification.
- Live nested agent captures, permission expansion, daemon restart or hand-editing runtime state.
- Automatic issue creation, attachment, waiver, mode change or merge without existing explicit authority.

## Questions for the Owner

- None; approved scope is sufficient to produce the implementation-ready proposal. Outstanding evidence prerequisites are recorded above, not converted into new product questions.
