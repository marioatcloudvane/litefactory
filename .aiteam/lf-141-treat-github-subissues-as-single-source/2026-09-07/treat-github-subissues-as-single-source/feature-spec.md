# Feature Spec: treat-github-subissues-as-single-source
# Status: APPROVED
# Date: 2026-09-07

## Goal
- Give the owner one editable story specification in each GitHub child issue, with planning discussion, fixed implementation scope, and visible execution outcomes.

## User Stories
| ID | Story |
| --- | --- |
| US-1 | As the owner, I want complete stories published and revised in GitHub so implementation never depends on a competing tracked story file. |
| US-2 | As the owner, I want comments on managed child issues to refine their parent plan so discussions share the feature context and worktree. |
| US-3 | As the owner, I want promotion to freeze the approved story revisions and announce the policy so later comments cannot change active implementation. |
| US-4 | As an implementation agent, I want the selected GitHub story revisions supplied on initial execution and resume so the work follows the approved scope. |
| US-5 | As the owner, I want story-level run progress and verified completion reported on each child so finished stories close and failed work remains visible. |
| US-6 | As the owner, I want existing file-backed plans reconciled safely so adopting issue authority does not lose technical detail or human edits. |

## Data Model
| Entity | Field | Type | Required | Constraints |
| --- | --- | --- | --- | --- |
| Feature plan | Parent identity | Repository identity + issue ID/number | Yes | Existing registered repository and parent session remain coordination authority. |
| Feature plan | Planning generation | Stable generation identifier | Yes | Distinguishes an explicitly reopened planning cycle from a retry. |
| Feature plan | Lifecycle | Planning / freezing / implementing / completed | Yes | Durable; never inferred solely from removable mode labels. |
| Managed story | Identity | Parent identity + stable story key + child issue ID/number/URL | Yes | Unique within the feature; verify actual direct GitHub relationship before adoption and execution. |
| Managed story | Specification | Issue title and complete structured body | Yes | Story statement, acceptance criteria, dependencies, task ownership and architectural guidance must all survive publication; no truncation that removes required content. |
| Managed story | Lifecycle | Planning / frozen / running / blocked / verified / completed | Yes | Verified means evidence accepted; completed additionally requires successful GitHub reporting and close. |
| Execution revision | Source and revision | Child identity + fetched title/body + content digest + fetch time + generation | Yes | Immutable derived execution record; GitHub remains the sole editable specification; issue updated_at alone is insufficient identity. |
| Story execution | Run and outcome | Parent run identity + story identity + revision + state + evidence references | Yes | Outcome identifies the exact story/revision and validation performed; subprocess exit alone is insufficient. |
| Pending publication | Operation and recovery key | Stable operation identity + desired state + delivery status | Yes | Supports recovering creation, attachment, notices, outcomes and closure without re-running implementation. |
| Routed comment | Provenance | Child identity + authorized actor identity + comment ID + parent generation | Yes | Preserves source URL and supports deduplication and durable consumption. |

### Relationships
- Parent plan → managed direct child stories in the same repository.
- Parent generation → frozen story revisions → story execution attempts → validation evidence and pending GitHub updates.
- Authorized child comment → parent planning session and worktree; child issue never becomes an independent implementation task.
- Parent Story Index → story identifiers, dependency/order references and GitHub links; it does not duplicate acceptance criteria or tasks.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
| --- | --- | --- | --- |
| Existing owner comment/mode selection | Parent Plan and Implement actions | Existing numeric owner authorization | No new CLI required; explicit parent Plan selection reopens unfinished scope only after active implementation has ended. |
| Existing issue/comment polling | Managed child comment admission and routing | Existing numeric owner authorization | Observe managed children independently of parent activity, including completed children; preserve source attribution. |
| GitHub read | Read parent relationship, list direct children, read full child issues | Existing GitHub token with required issue read access | Pagination required; fetched relationship and repository identity validate index references. |
| GitHub write | Create/attach story, update complete body, publish notices/results, close completed issue | Existing GitHub token with required issue write access | Application owns publication; redact outbound content and report permission failures. |
| Plan agent output | Structured story publication proposal | Existing trusted runtime envelope | Carries complete new/changed stories plus source revisions through a transient handoff; no tracked per-story Markdown files in the resulting workflow. |
| Implementation agent input | Selected story context on every run/resume | Existing runner contract | Include exact source issue, execution revision, dependency context and parent context; issue text remains untrusted data. |
| Implementation agent output | Structured per-story outcomes and validation evidence | Existing runner contract | Missing, unknown, duplicate or mismatched story/revision identities cannot authorize closure. |

## Business Rules
- BR-1: After successful publication, GitHub issue bodies are the only editable story specifications. Plan agents receive current issues and propose changes; derived snapshots and transport files are disposable/runtime records and are never committed as story specifications.
- BR-2: New and revised stories must publish completely and attach to the intended parent before the feature is ready for promotion. Missing, invalid, oversized, inaccessible or partially published stories block readiness with an actionable parent report.
- BR-3: Read current issue content before applying a proposed Plan change; if it differs from the proposal's base revision, refuse automatic replacement and report the conflict. Verify the result after writing; never claim GitHub offers an atomic edit lock.
- BR-4: Child comments are relevant to planning only while their parent generation is Planning. Route authorized comments through the parent session/worktree and return a child acknowledgement/result linked to the parent run. Other actors retain the existing non-execution behavior.
- BR-5: Serialize parent planning and promotion decisions. A Plan refinement already admitted must finish or visibly fail before promotion; a comment arriving after durable freeze receives refusal, even if its timestamp predates polling.
- BR-6: Parent promotion freezes all indexed unfinished stories together. Validate their full content and relationships, persist the selected revisions, and successfully publish visible freeze notices before launching implementation. Failure leaves launch blocked and retryable.
- BR-7: Freeze notices state that the story is locked for implementation, identify the approved revision and parent, and direct scope changes to parent Plan mode. Later authorized comments receive a concise refusal linked to that notice and start no agent; retries must not multiply identical notices for the same triggering comment.
- BR-8: The lock is litefactory's processing policy. Native GitHub conversation locking is excluded; owners can still physically comment or edit in GitHub. Later issue edits cannot replace the recorded execution revision and detected drift is reported visibly.
- BR-9: On first run, retry and resume, agents receive the selected approved revision explicitly. They must not silently consume a changed issue body, a stale conversation context, or a legacy story file instead.
- BR-10: An owner-selected parent Plan action after execution is quiescent begins a new planning generation for unfinished stories, announces their availability for refinement and invalidates previous approval for those stories. Completed stories remain closed; additional work uses a new story. During active implementation, a parent Plan request receives a deferred/refusal explanation and cannot mutate frozen scope.
- BR-11: Publish child milestones for frozen, started, blocked/failed and completed, with parent/run links and approved revision. Keep milestone delivery retryable and bounded; continuous token streaming is excluded.
- BR-12: A story becomes verified only when structured results identify its selected revision, acceptance criteria are satisfied, required review and testing gates pass, and litefactory has successfully saved and pushed the relevant implementation. Merge is not required. A failed feature-wide gate prevents affected story verification; absent finer evidence, it prevents every story in that run from verification.
- BR-13: Publish completion evidence and close verified stories with reason completed. Persist closure/reporting failures for retry without rerunning implementation; partial or failed stories remain open with an explanation. A human-closed issue alone is not evidence of implementation success.
- BR-14: Continue observing tracked completed children for authorized comments and return the frozen/completed policy explanation. External reopening alone does not clear approval state or start execution.
- BR-15: Adopt legacy stories only after comparing full tracked content, current issue content and provenance. Preserve complete technical detail and human additions; ambiguous divergence blocks migration for that feature with an actionable report. Remove current tracked story files and their live index references only after complete issue publication is confirmed; retain historical commit links as history.
- BR-16: Both Claude and Codex planning, implementation, review and test workflows follow the same issue authority, runtime handoff, revision and completion contracts. Update all affected instruction copies and documentation together.
- BR-17: Manually attached direct children may be explicitly adopted only through a validated parent Story Index and complete story contract. Unindexed children are left untouched; nested and cross-repository references in an index are rejected with actionable diagnostics.

## Edge Cases & Error Handling
- GitHub unavailable, rate limited, unauthorized or issue removed → preserve pending work and cursor correctness, report the failure when possible, and retry through existing scheduling; never fall back silently to a legacy file.
- Create succeeded but attachment/report failed → recover the existing child using durable identity and validated relationship; do not create a second story.
- Conflicting body edits during planning → retain observed current body and proposal evidence, refuse known stale updates, and require a new Plan run to reconcile; read/check/write cannot eliminate the final external-edit race.
- Child detached, moved, deleted or externally closed before launch → block affected promotion/dispatch and report; never infer a replacement by title or marker alone.
- Child relationship changes after freeze → retain immutable run evidence and block subsequent dispatch/resume until the feature is reconciled; running work uses its frozen revision and reports detected drift.
- Multiple owner comments and concurrent parent commands → process through the existing parent serialization boundary with stable comment IDs; none may create a competing child worktree.
- Crash after freezing some notices → retry remaining notices for the same revision set; no agent starts until all required notices succeed.
- Crash after verified work or successful GitHub close → reconcile reporting/closure state idempotently; no second implementation run solely to deliver status.
- Body publication exceeds supported limits → fail with the specific affected story; do not truncate technical instructions or silently split authority into tracked files.
- Legacy truncated issue with available complete source → reconcile and publish complete content before file removal; missing or ambiguous source → block migration and identify what is missing.

## Assumptions and Provenance
| Decision | Basis |
| --- | --- |
| Freeze all unfinished stories at parent promotion; refinements share parent session/worktree. | Research identifies the parent workflow as the coordination point; this avoids different stories entering ambiguous approval states. |
| Lock means application refusal plus visible notice; no native conversation lock. | Research establishes that GitHub owners/collaborators can bypass conversation locks; refusing execution provides the enforceable behavior requested. |
| Immutable runtime snapshots are acceptable and must be supplied on resume. | Research recommends execution records to preserve approval despite mutable issue bodies; they are not independently editable specifications. |
| Completion requires gates plus saved/pushed work, without merge. | Existing litefactory owns persistence/publication; “finished implementation” reasonably precedes merge while requiring recoverable code. |
| Parent Plan may reopen unfinished scope only when no run is active; completed stories use successors. | Provides a deterministic correction path without adding cancellation or automatic reopen semantics. |
| Same-repository, direct indexed children are initial scope; milestone updates supply progress. | Research recommends the existing parent-feature workflow and excludes generalized project management. |
| Planning output needs a transient structured publication handoff despite eliminating committed story files. | Agents still need to propose new issue content before the daemon can publish it; transient transport is not another editable authority. |
| No unresolved owner decision blocks writing this plan. | Headless run explicitly requires reasonable assumptions and forbids questions or approval waits; decisions remain reviewable in this artifact. |

## Out of Scope
- Implementation during this Plan run; live GitHub changes, commits or pushes by the planning agents.
- Native conversation locks, preventing owners/admins from editing GitHub, or claiming atomic GitHub body updates.
- Cross-repository/nested story execution, automatically enrolling arbitrary attached children, generalized project management or automatic merges.
- Rewriting historical commits, bulk migration of unrelated archived sessions, or silently discarding ambiguous legacy content.
- Agent-specific divergence, independent child implementation sessions, streamed child token logs or interruption/cancellation controls.

## Acceptance Criteria
- [ ] AC-1: Creating and revising a Plan produces complete attached GitHub stories; a second publication updates the same identities and successful plans contain no tracked per-story specification files.
- [ ] AC-2: Missing/invalid/oversized/inaccessible stories and partial publication visibly prevent promotion; retries recover prior successful creation/attachment without duplicates.
- [ ] AC-3: Authorized child Plan comments execute in the parent context and produce attributable replies; unauthorized comments and unrelated marker-like issues cannot gain execution privileges.
- [ ] AC-4: Promotion persists one approved revision set, orders admitted planning work deterministically and publishes all required lock notices before any implementation launch.
- [ ] AC-5: Authorized comments during or after implementation produce a retryable lock explanation and no agent execution, including comments on tracked closed children.
- [ ] AC-6: Initial execution, retry and resume deliver the approved GitHub-derived revision explicitly; body edits after freeze never silently change implementation context.
- [ ] AC-7: Known stale Plan proposals do not overwrite current issue content; detected concurrent changes produce conflict/drift reports without claiming an atomic remote lock.
- [ ] AC-8: Child milestones identify the parent run and revision; valid per-story evidence plus passing gates and saved/pushed work permits completion reporting and closure, while failed/incomplete/unknown outcomes leave stories open.
- [ ] AC-9: Restart and API-failure tests prove pending notices, reporting and closure resume without duplicate story creation or repeat implementation.
- [ ] AC-10: Legacy migration retains required technical content and human additions, blocks ambiguity, and removes live story files only after confirmed issue publication.
- [ ] AC-11: An explicit parent Plan action after execution ends reopens only unfinished scope in a new generation; comments or external reopening alone cannot unfreeze a completed story.
- [ ] AC-12: Claude and Codex workflow instructions, specialist inputs, documentation and tests agree on the issue authority and structured result contracts.
