# Feature Spec: wire-graph-based-execution-into-plan
# Status: READY FOR PLAN AUTHORING
# Date: 2026-09-20

## Goal
- Make Plan an owner-reviewed conversation over stories, engineering and acceptance, using the existing graph engine and the same embedded DOT/role/skill Markdown organization as Research.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As an owner, I want Plan's shipped workflow and role Markdown organized like Research so that the execution is understandable and easy to maintain. |
| US-2 | As an owner, I want to shape and approve the complete story set at each of three stages so that technical detail and acceptance follow agreed problems. |
| US-3 | As an owner, I want complete issues created only after the reviews and consistency check, then a final decision on the published issues so that publication and approval remain distinct. |
| US-4 | As an owner, I want resumed conversations, rewinds and publication retries to preserve identity and review state so that interruption cannot skip approval or duplicate stories. |

## Decisions
- Source: approved Research brief; latest `/plan` selects Plan without changing the settled scope.
- q1=b: create complete story issues after all three reviews and consistency checking; take final approval on those issues.
- q2=a: each stage approves the whole current story set; discussion may focus on one story.
- q3=a: meaningful change reopens the earliest affected stage and every later stage for the whole set.
- Owner priority: keep the implementation simple; reuse existing graph controls, managed story contracts and publication, and match Research's application-owned Markdown structure.
- Implementation authorization remains a separate explicit mode selection.

## Existing Features to Reuse
| Need | Existing facility |
|------|-------------------|
| Workflow and role package | `internal/teamsource/research/`: DOT, stage role Markdown and `skills/<name>/SKILL.md` with supporting Markdown |
| Bounded execution and owner waits | `internal/graphexec`, persisted graph state and owner replies in `internal/store` |
| Plan discussion | `internal/turn/plan_context.go`, `plan_attempt.go`, `plan_handoff.go` |
| Typed stories and validation | `internal/planbrief`; shared `teams/source/skills/planning-contracts/SKILL.md` |
| Publication, revision and identity | Existing factory Plan publisher and managed-story contracts |
| Operator admission and compatibility | Research graph opt-in and persisted execution-path pattern; keep legacy Plan supported |

## Data Model
| Entity | Retained information | Required constraints |
|--------|----------------------|----------------------|
| Plan draft set | Source issue, stable identity, revision, active stage, story membership, decisions, assumptions, unresolved questions | Durable across runs and dates; nonempty before publication |
| Story draft | Stable identity, revision, title, description, problem solved, stage content, dependencies, supersession links | Renames retain identity; splits/merges create linked identities; removed stories leave active set |
| Stage review | Stage, reviewed set revision, owner identity/reply, approval, change summary | Approvals bind to the displayed whole-set revision and stage |
| Execution | Selected path, captured workflow/roles, current node, bounded attempt, retained replies | Active definitions stay immutable; continuation never implies approval |
| Publication | Draft-to-issue identity, published revision, per-story receipt and uncertain effect state | Recover existing effects before retry; retain surviving issue identity |
| Final review | Exact confirmed published set and revisions, owner decision | No final approval for incomplete publication; content changes invalidate approval |

### Relationships
- Plan draft set contains the active story identities and references retained superseded history.
- Reviews refer to the exact draft-set revision; final review refers to confirmed issue identities and revisions.
- Existing external dependencies retain issue identity; story and task dependency graphs remain acyclic.

## API / Interface Changes
| Surface | Actor | Required behavior |
|---------|-------|-------------------|
| Existing issue conversation | Authenticated owner | Show current stage, revision, changes, questions and requested approval; retain replies across headless runs |
| Shipped Plan package | Maintainer | One Plan DOT, stage role Markdown and reusable skill/supporting Markdown laid out like `internal/teamsource/research/`; registered actions use `lf_plan_*` |
| Explicit graph admission | Operator / factory | Admit new Plan graph executions through the existing admission pattern; preserve active legacy path and captured definitions |
| Typed Plan result and publisher | Factory | Intermediate drafts remain discussion; only reviewed complete content reaches durable publication |
| Final review | Authenticated owner | Approve the confirmed published set, or request changes that reopen affected stages |

## Business Rules
- Story stage: titles, descriptions, problems solved and intended benefit; no remote story creation.
- Engineering stage: architecture, boundaries, dependencies and provisional tasks; no remote story creation.
- Acceptance stage: prose success intent, concrete canonical criteria, explicit completion signals and complete task-to-criterion mappings.
- Clear authenticated approval advances only the displayed stage/revision; silence, agent completion, input receipts and ordinary continuation do not advance it.
- Story additions/removals/splits/merges reopen stories; design/dependency changes reopen engineering; criteria-only changes reopen acceptance unless they alter earlier intent.
- Reopening retains content and history while invalidating the affected whole-set approvals and all later approvals.
- Wording-only changes preserve stage approvals only with a recorded comparison establishing unchanged meaning; uncertain meaning takes the semantic-change path.
- Acceptance approval authorizes publication after the same revision passes the consistency check, without another pre-publication approval gate.
- Consistency requires a nonempty complete typed story set, all three current approvals, resolved questions, valid criterion/task mappings and dependencies without cycles or missing/superseded targets.
- Factory publication confirms the entire set before final review; publication alone does not complete Plan or authorize Implement.
- Final rejection follows the same rewind rules, reruns checks and creates managed revisions before another final review.
- Finishing Plan never selects another mode.

## Edge Cases & Error Handling
- Stale or duplicate owner reply → retain evidence without advancing a different stage or revision.
- Restart, interrupted invocation or fallback → restore originating review, saved content, reply correlation and captured definitions.
- Bounded-loop exhaustion → retain work and explain the pause; explicit continuation may begin another bounded attempt without approving it.
- Partial or uncertain remote publication → reconcile existing receipts/issue identity before further writes; keep final review unavailable until complete.
- Post-publication rename → retain issue identity; split/merge/removal → preserve history and supersession while changing active membership through managed-story contracts.
- Invalid graph package/admission configuration → actionable failure; do not silently migrate an active execution.
- Permission, process or publication failure → retain its actual state; valid input acknowledgment cannot establish publication or readiness.

## Acceptance Criteria
- [ ] AC-001: The shipped Plan DOT and role/skill Markdown follow the existing embedded Research package organization; captured role references resolve and actions use registered `lf_plan_*` names.
- [ ] AC-002: Newly admitted graph Plan runs preserve the three-stage order, retain legacy compatibility and pin active definitions without selecting another mode.
- [ ] AC-003: Each review displays the current whole set, stage/revision, changes, questions and requested approval; only an authenticated matching owner approval advances it.
- [ ] AC-004: Drafts contain the required stage content, and no issue is created during story, engineering or acceptance discussion.
- [ ] AC-005: Meaningful changes invalidate approvals from the earliest affected stage onward while retaining content; rename/split/merge/removal preserve the required identity/history relationships.
- [ ] AC-006: Publication rejects empty, incomplete, unapproved or inconsistent sets, unresolved questions, invalid criterion/task mappings and invalid dependencies; acceptance approval authorizes publication only for the checked revision.
- [ ] AC-007: Existing factory publication confirms complete managed issues before final approval, keeps surviving issue identities on revision and leaves Plan awaiting final review after creation.
- [ ] AC-008: Final approval binds to the exact published set/revisions; rejection or content changes reopen the correct reviews and produce managed revisions before another final decision.
- [ ] AC-009: Restart, stale/duplicate replies and loop exhaustion preserve durable state and cannot skip a gate; partial/uncertain publication recovery does not duplicate remote issues.
- [ ] AC-010: Verification distinguishes offline checks from authentic host observations; unsupported observations remain explicit prerequisites to default activation, and legacy retirement remains outside this feature.

## Candidate Evidence Obligations
| Criteria | Proof method | Available source / bounds | Missing prerequisite |
|----------|--------------|---------------------------|----------------------|
| AC-001–AC-002 | Package validation and graph-admission regression tests | Current application-owned package, graph/store and turn test facilities; exact implementation snapshot | Implementation and executed checks |
| AC-003–AC-006 | State transition and assembled turn tests covering approved/rejected/stale revisions and blocked effects | Existing Research shaping and Plan discussion tests; current contract versions | New Plan cases and executed checks |
| AC-007–AC-009 | Store/restart and publisher tests with deterministic remote-loss, retry and revision scenarios | Existing durable owner waits and managed publisher; controlled offline remote fixtures | New assembled recovery cases and executed checks |
| AC-010 | Retained authentic Claude and Codex invocation, correction and authoritative receipt observations | Exact shipped snapshot and actual host CLI versions; offline fixtures demonstrate only their tested behavior | Separately authorized external observations; not available from this Plan run |

## Assumptions
- Match Research's embedded application package structure, including stage-role Markdown and reusable skills; do not introduce a new team/catalog system solely for Plan.
- Prefer shared application sources and in-memory package/render verification. If installed `.claude/**` or `.codex/**` artifacts must change, split opposite-harness installation work into independent stories after shared-source work; neither sibling tree is a scoped prerequisite for the other.
- No new public API or standalone draft service is required; extend existing durable Plan/graph state only where the reviewed behavior requires it.
- Remain opt-in while required authentic host evidence is unavailable, following Research's documented activation policy.
- `docs/decisions/nested-agent-evidence.md` and `docs/story-contract-instruction-manifest.md` are absent in this checkout; current `teams/source/skills/planning-contracts/SKILL.md`, workflow/evidence skills and `docs/workflow/research-graph.md` supply applicable available contract/evidence guidance.

## Risks
- Intermediate node completion must not trigger the existing completed-Plan publisher.
- Post-publication rejection must update the existing managed issues without losing identity or treating an input receipt as final approval.
- The older APP_CONTEXT and Research operator prose contain historical details; current source inspection by architecture authoring must settle wiring details without replacing approved scope.

## Out of Scope
- Partial story issues after the first loop; independently advancing individual stories.
- Replacing the graph engine, creating a new publication system or automatic mode selection.
- Silent migration of active legacy sessions, legacy retirement or editing definitions during active execution.
- Implementing these changes in this Plan run; changing Implement/Validate testing responsibilities.
- Visual workflow editing, Temporal adoption, marketplace or fleet management.
- Treating offline fixtures as authentic host evidence or removing existing external observation obligations.
