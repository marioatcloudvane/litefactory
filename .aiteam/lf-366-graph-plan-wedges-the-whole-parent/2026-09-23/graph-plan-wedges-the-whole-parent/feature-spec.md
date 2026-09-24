# Feature Spec: graph-plan-wedges-the-whole-parent
# Status: APPROVED
# Date: 2026-09-23

## Goal
- Keep graph Plan available to independent stories while adopting verified GitHub child lifecycle changes and recovering interrupted story operations without inventing approval or losing history.

## Scope authority and source bounds
- Owner `/plan this` selects completion of the previously explained technical direction; GitHub remains the child-state source of truth.
- Primary scope source: `RESEARCH_BRIEF.md`, decisions d1–d27 and assumptions a1–a6; technical evidence: `RESEARCH_TECHNICAL.md`.
- Earlier reopen-first question is withdrawn and must not be reintroduced.
- Saved `PLAN_CONTEXT.json` is absent at the supplied session path; supplied owner history and retained research provide the continuation context.
- Research source claims are bounded to revision `40fa12605a4c5bb5b366a6e2806add039c7cab40`; current architecture must verify exact implementation locations.
- Deployment status and absence of currently wedged sessions remain unverified owner reports; no live observation or executed tests are claimed by this specification.

## User Stories
| Need | User outcome |
|---|---|
| Independent progress | As the Plan owner, I can continue parent discussion and eligible sibling shaping while another story has an unresolved operation or observation conflict. |
| GitHub lifecycle adoption | As the Plan owner, I can close or reopen a child on GitHub and have Plan reevaluate its actual state without requiring a duplicate command. |
| Durable recovery | As the Plan owner, I can resume after interruption without duplicate mutations, lost retirement intent or stale intent overriding later changes. |
| Understandable readiness | As the Plan owner, I can see adopted changes and remaining blockers, and approve only a consistent current active scope. |

## Logical Data Model
| Entity | Required information | Constraints |
|---|---|---|
| Story binding | Parent/story key, bound issue identity, content revision | Preserve history; observations cannot silently rebind identity. |
| Lifecycle observation | Issue state, actual closure reason, lifecycle source, current parent relationship, ordering evidence available from GitHub | Distinguish external closure from authorized factory retirement; unknown facts remain unknown. |
| Story operation | Original request, exact candidate/revision, effect kind and history, known remote binding, outcome/uncertainty | Create, attach, update and retire retain request identity across recovery. |
| Retirement authority | Original authenticated owner confirmation, proposal/wait binding, exact replacements and expected revision | Never derive authority from closure alone or modify replacements during replay. |
| Story hold | Story key, reason, related request when present, relevant observation, next action | Reconstruct after restart; clear only after supporting reconciliation. |
| Plan projection | Active membership, dependencies, approvals, current workflow state | Selection, publication and readiness use the same adopted lifecycle. |
| Parent notice | Material story change or hold, explanation/action, durable delivery identity/state | Retry delivery without repeating unchanged notices on every poll. |

### Relationships
- Story operations and holds belong to their original parent/story binding, including creates without a bound child yet.
- Dependencies refer to retained story identities; closure does not erase edges or invent replacement stories.
- Closure observations do not create engineering, acceptance or Plan-approval evidence.

## Interface and Permissions
| Surface | Behavior | Authority |
|---|---|---|
| Existing GitHub issue state | Adopt verified child closure/reopen; respect observed reparenting | Current verified remote state governs lifecycle, independently of factory mutation approval. |
| Existing owner retirement reply | Reconcile or replay the exact authorized request | Existing authenticated owner and candidate/revision checks remain required. |
| Existing parent Plan comments | Explain adopted changes, story holds and required action | Existing durable delivery and input-cursor ownership. |
| Existing child replies | Serve eligible replies; explain stale replies without promoting stale content | Existing author, binding and revision validation. |

## Business Rules
- Shared parent identity, shared authority and storage failures keep parent-level handling; repository-wide rate limits retain normal backoff.
- Story-specific remote observation failures block that story and work needing its unresolved state, while healthy independent stories remain eligible.
- Recovery revisits held stories on subsequent eligible daemon passes without a new owner comment or shaping selection; bounded work must not starve eligible healthy stories.
- A verified closed child leaves active shaping for all closure reasons, preserving the actual reason including unspecified; closing as completed grants no implementation or acceptance evidence.
- Reopening the same bound child requires fresh content, parent, dependency and approval reevaluation; it cannot revive stale approval automatically.
- Reparenting stops writes through the old parent; do not move the child back or automatically admit it elsewhere.
- Reconcile current remote facts before mutation replay; older intent cannot overwrite a verified later lifecycle change. Ambiguous ordering keeps a story-local hold.
- New local completion must keep lifecycle, draft/effect outcome and workflow transition consistent under revision checks; recovery also repairs historical partial completion.
- Valid retirement replacements remain active stories under the same parent. A changed replacement invalidates replay authority rather than altering the approved request.
- Active-set changes require renewed assembled-plan review; preserve independent story approvals when their own content and prerequisites remain valid.
- Final readiness requires a nonempty reviewed active set, valid dependencies and no unresolved material hold/effect; adopted closure alone is not a permanent blocker.

## Acceptance Criteria
- [ ] AC-001: Given unresolved create, attach, update or retire work on story A and eligible independent B, eligible daemon passes retain A's operation/hold and advance B without a parent-wide story-error failure; a story dependent on A's unresolved state remains unavailable.
- [ ] AC-002: Held operations are reconsidered without a new owner comment or shaping selection after restart; bounded recovery attempts permit healthy work to progress rather than repeatedly selecting only stuck work.
- [ ] AC-003: Discovery adopts a verified bound child's closed state with completed, not-planned or unspecified reason, preserves history and reason, and removes it from active shaping without forced reopening, a duplicate closure command or fabricated approval/evidence.
- [ ] AC-004: Reopening a bound child reevaluates its current revision, parent, dependencies and approvals before eligibility; a retained older retirement cannot close it again when a later reopen is verified, and uncertain ordering produces only a story hold.
- [ ] AC-005: Reparented children receive no mutation through the original parent and are not automatically moved or admitted elsewhere; missing/unverifiable or identity-mismatched children become explained story-local holds without inferred closure or rebinding, while independent siblings continue.
- [ ] AC-006: A child already closed as not planned with exact retained retirement authority reaches reconciliation despite the ordinary open-child discovery check, completing local retirement and workflow state without another close mutation.
- [ ] AC-007: An open-child retirement retry requires matching original authenticated owner confirmation, proposal/wait, candidate, revision, identity, provenance, parent and exact replacements still valid in the same parent; missing or stale authority blocks mutation without blocking unrelated work.
- [ ] AC-008: Restart recovery converges after remote closure but before local effect/draft completion, and after effect/draft completion but before workflow retirement; repeated recovery or duplicate owner replies do not repeat successful mutations or lose history.
- [ ] AC-009: Uncertain create/attach/update effects inspect current remote facts using their original request identity and known binding before retry; absent or ambiguous creation matches do not trigger blind recreation, and an obsolete request can be superseded without concealing an unknown remote outcome.
- [ ] AC-010: New successful local completion persists a consistent lifecycle, draft/effect outcome and workflow transition under expected-revision protection; concurrent stale completion cannot overwrite newer state.
- [ ] AC-011: Selection, active-set hashing, publication and final readiness consistently exclude externally closed children from active shaping; dependency edges remain visible, affected dependencies and assembled-plan approval require reevaluation, and unchanged independent story approvals survive.
- [ ] AC-012: A Plan with a nonempty reviewed remaining active set and valid dependencies may become ready after a resolved external closure; empty active sets, unresolved material effects/holds, invalid dependencies or stale assembled-plan review cannot become ready.
- [ ] AC-013: Existing parent reporting identifies adopted lifecycle changes and remaining held story/request, cause and next action once per material change; restart and uncertain delivery recover the same notice rather than creating a notice per poll.
- [ ] AC-014: Parent discussion and healthy child replies remain serviceable during another story's hold; replies bound to closed or changed story state cannot promote stale candidates and receive an explanation before being marked handled, with replay-safe input cursors.
- [ ] AC-015: Story-specific failures remain isolated, while shared storage, parent identity/authority and repository-wide API failure paths preserve existing failure/backoff behavior without false success or readiness.
- [ ] AC-016: Automated regressions exercise the actual daemon discovery-to-recovery path for manual closure and interrupted retirement alongside a healthy sibling, plus restart, both retirement persistence gaps, duplicate replay, create/attach/update uncertainty, reopen, reparenting, identity/authority rejection, dependency changes and readiness outcomes.

## Edge Cases and Error Handling
- No child bound during uncertain creation: keep request-linked hold and recovery eligibility without creating a replacement blindly.
- Closure with a reason different from retained retirement intent: adopt the actual lifecycle separately; reconcile or supersede the operation only from sufficient evidence and never rewrite the reason to satisfy intent.
- Child read forbidden/not found: hold with unavailable observation; do not assert deletion or closure.
- Child closed while a reply is queued: reject stale promotion, retain explanation delivery and advance its input cursor only through existing safe handling.
- Child closes while used as retirement replacement: stop replay on stale authority, retain exact original replacement list and explain the conflict.
- No active stories after closure: preserve history and discussion, but do not claim a completed ready Plan.

## Existing Features to Reuse
| Capability | Existing owner/evidence |
|---|---|
| Per-story holds/effects and eligible selection | `internal/planbrief/plan_workflow.go`; research f8 |
| Retained candidates, effects and retirement reconciliation | `internal/store/plan_story.go`, `internal/turn/plan_story_activity.go`; research f1/f3 |
| Revision-guarded workflow persistence | `internal/store/plan_workflow.go`; research f8 |
| Child routing, restart cursors and normal polling | `internal/daemon/plan_children.go`; research f2/f7 |
| Existing regression fixtures | `plan_story_boundary_test.go`, `plan_graph_boundary_test.go`, `plan_children_boundary_test.go`; research f7 |
| Parent reporting and managed publication | Existing report, turn, store and planbrief package responsibilities in APP_CONTEXT.md |

## Assumptions and Risks
- Existing polling/backoff machinery provides eligible passes; this fix does not promise progress during unavailable shared services or introduce a scheduler.
- Exact schema, transaction boundaries, readback evidence and retry budget are architecture decisions within these behaviors.
- Historical data may lack lifecycle ordering evidence; preserve a local hold rather than assuming authorization to replay.
- Closure removes active Plan work regardless of reason because the owner explicitly made GitHub the UI and lifecycle authority; readiness evidence remains separate.
- No protected instruction-tree edits are required by this scope.
- Missing `docs/decisions/nested-agent-evidence.md` and `docs/story-contract-instruction-manifest.md` are documentation-reference gaps; supplied content contract and workflow evidence rules remain applicable.

## Candidate Evidence Obligations
| Criteria | Proof method | Source availability and limits |
|---|---|---|
| AC-001–AC-005, AC-011–AC-015 | Existing-style daemon/turn regression fixtures with controlled forge responses, durable local store and captured progression/report outcomes | Repository fixtures identified by research; implementation must verify current APIs. Offline tests establish programmed behavior, not live deployment. |
| AC-006–AC-010 | Activity/store tests with injected persistence and remote-response failures; reopen store and replay retained request; assert exact mutation counts and revision rejection | Existing retirement lost-response fixture identified by research; add the missing whole-path and partial-write coverage. |
| AC-016 | Combined discovery/recovery regressions plus repository-required `make check` | No tests executed in Plan. No nested agent or authentic dual-harness observation is required for this ordinary recovery change. |

## Out of Scope
- General GitHub synchronization or arbitrary issue/parent closure handling.
- Graph kill switch, legacy dispatch removal or unreachable promotion-edge cleanup.
- New dashboard, public API, owner command or close-confirmation flow.
- Automatic child reopening, automatic moving/reparenting or blind replacement creation.
- Deployment, daemon restart, live production reproduction or manual store repair.
- Changes to existing workflow/content contract versions, scope-freeze ownership or evidence requirements outside graph Plan.
