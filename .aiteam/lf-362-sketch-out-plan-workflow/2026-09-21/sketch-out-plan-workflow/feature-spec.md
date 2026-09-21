# Feature Spec: sketch-out-plan-workflow
# Status: READY FOR PLAN ASSEMBLY
# Date: 2026-09-21

## Goal
- Give the owner a runnable, DOT-defined Plan workflow that shapes stories on stable sub-issues, then technical choices, then acceptance criteria, with explicit revision-bound promotion and independent story progress.

## Requested Delivery Boundaries
| Boundary | User outcome |
|---|---|
| Activity | As an owner, I want draft stories created, updated and retired through one recoverable story activity so my sub-issues remain the durable planning surface. |
| Agents and DOT | As an owner, I want three focused planning conversations expressed in the Research-style graph and Markdown structure so I control progression without repeating settled answers. |
| Wiring and retirement | As an owner, I want the new graph to run through the factory and replace obsolete Plan dispatch so publication, restart recovery and existing plans remain dependable. |
- Author exactly three delivery stories as requested; these boundaries are scope groupings, not separately authored story contracts.

## Decisions and Assumptions
| Kind | Decision | Basis |
|---|---|---|
| Owner decision | Stories first, technical shaping second, criteria plus assembled-plan review third. | Issue and saved brief d1, d20. |
| Owner decision | First usable stories become sub-issues immediately; each progresses independently. | Owner reply and d11, d14. |
| Owner decision | Deliver one story for the activity, one for agents/DOT, one for wiring/old-code retirement. | Latest explicit /plan request. |
| Assumption | Activity name is lf_plan_story; add no other activity names. | Current graph spelling admission and brief a15. |
| Assumption | Delete means owner-confirmed retirement/closure as not planned, preserving history. | Saved brief a9, a18; no request for destructive deletion. |
| Assumption | Active legacy plans retain their persisted workflow; an explicit owner-directed replan may adopt the new graph through supported admission. | Saved brief a11 and workflow compatibility contract. |
| Assumption | Retire obsolete default dispatch and unused source instructions; retain only explicitly identified compatibility readers/handlers needed by supported persisted cohorts. | Latest retirement request plus persisted-version obligations. |
| Assumption | Add shared neutral package sources; do not edit installed .claude/** or .codex/** trees in these three stories. | Research package precedent and independent protected-tree maintenance contract. |
| Assumption | Third-stage reviews can approve individual criteria candidates incrementally; feature readiness requires approval of the exact assembled current set within that same third stage. | Saved brief a20; no fourth stage. |

## Data Model
| Entity | Fields | Required constraints |
|---|---|---|
| Story draft | Stable key; parent; optional issue identity until creation; actor; need; benefit; optional boundaries/priority; active/retired status | Initial publication requires usable intent; architecture, tasks and criteria are absent in story-only output. |
| Stage candidate | Story key; stage; immutable revision; upstream candidate revisions; shared-decision revisions; decisions; assumptions; questions; stage payload | Candidate identity is stable across retries; edits create a new revision; material open questions block promotion. |
| Technical payload | Reuse; interfaces; access control; entities and relationships; invariants; dependencies; failure behavior; provisional tasks | References approved story intent and explicit relevant dependencies. |
| Acceptance payload | Stable criterion IDs; observable outcomes; proof methods; final tasks and criterion links | Complete assembled stories pass the existing typed complete-plan validator. |
| Owner approval | Authenticated owner; source reply; explicit story/set; stage; displayed candidate and dependency revisions | Stale, ambiguous, mixed change-and-approval replies do not promote. |
| Story effect | Stable request identity; operation; story key; parent/issue identity; expected content revision; candidate revision; result revision/URL/status | Distinguish applied, conflict, failed and uncertain; durable recovery reconciles the same operation. |
| Final approval | Included story revision set and shared-decision revisions | Any set or canonical content change invalidates final approval; retired stories are excluded explicitly. |
| Workflow selection | Persisted admitted workflow and supported compatibility cohort | Restart and ordinary owner replies do not silently switch workflow versions or modes. |

### Relationships
- Story drafts own successive stage candidates and approvals; candidate dependencies identify affected story stages and shared decisions.
- Draft issue identity survives final publication; completed-plan handoff must reuse these sub-issues.
- Retirement retains historical candidates, approvals, effects and replacement links.

## API / Interface Changes
| Surface | Authorization | Behavior |
|---|---|---|
| lf_plan_story create/update/retire | Admitted Plan scope; retirement additionally requires exact authenticated owner confirmation | Validate structured stage data, persist effect intent, invoke existing forge boundary and confirm resulting state. |
| plan.dot and three role Markdown files | Existing graph admission and agent execution policy | Story, technical and acceptance roles plus shared plan-evidence and focused shaping skills; match Research package organization without changing Research agents. |
| Child replies and direct edits | Existing authenticated owner admission | Correlate to the story, stage and candidate; ingest edits as candidate changes; keep parent for index/shared decisions/assembled review. |
| Plan graph registration/policy | Persisted factory mode and workflow authority | Decode stage outputs, authorize actions, retain waits and candidates, route owner replies and hand off final typed Plan. |
| Final Plan adapter | Existing complete-plan publication/readiness controls | Generate shared plan_brief.md and link-only Story Index, reuse draft identities, confirm publication and save before readiness. |

## Business Rules
- Preserve settled answers, decisions and unanswered questions across dates, restarts and provider continuations.
- Promote only explicitly approved stories whose required upstream revisions are valid; unrelated stories may remain in earlier stages.
- User-intent changes return affected stories to story shaping; technical-only changes return to technical shaping; dependent approvals reopen according to recorded impact.
- Demonstrably unrelated approvals survive; uncertain impact holds the changed story and potentially affected dependents for explicit impact resolution.
- Pure presentation changes preserve canonical approvals; canonical content changes require new review.
- Architecture can recommend retirement; only owner confirmation removes a story from active scope.
- Acceptance agreement defines future implementation obligations; it does not establish engineering acceptance, select Implement or close implemented stories.
- Questions, review-ready candidates and owner promotion are separate outcomes; loop exhaustion is a resumable stop, never approval.
- Reject duplicate identities, dangling references, dependency cycles, invalid stage payloads and unauthorized operations before effects.
- Never treat draft publication, helper input acknowledgment, process success or pending effects as completed Plan readiness.
- Final ready scope is nonempty and contains complete validated stories, valid dependencies, exact final approval, confirmed publication and successful save.

## Edge Cases and Error Handling
- Create succeeds but attachment/reply fails: retain created issue identity and request, reconcile and attach without duplicate creation.
- Current remote content differs from expected base: preserve owner edits, record conflict and require reconciliation rather than overwrite.
- Update/retirement response is lost: reconcile remote state using retained effect identity before retry; failed or uncertain remains incomplete.
- Unauthorized, stale or wrong-story reply: no promotion; retain a precise recoverable diagnostic and refreshed review when needed.
- External closure, deletion or reparenting: hold the affected story for reconciliation; do not infer retirement or completion.
- Shared-decision change: reopen its affected dependents and assembled approval without resetting unrelated stories.
- Retirement or replacement changes dependencies: validate the remaining active graph and renew assembled approval before readiness.
- Restart during owner wait, publication or save: restore exact candidate, correlation and effect debt; do not duplicate side effects.
- Unsupported legacy migration: preserve existing route and diagnose explicitly; do not rewrite persisted versions.

## Existing Features to Reuse
| Capability | Existing owner |
|---|---|
| Generic agent, branch, loop, ask and comment graph activities | internal/graphexec and WorkflowPolicy |
| DOT/role/skill package layout and durable owner waits | internal/teamsource/research and Research workflow policy |
| Create/attach/update sub-issues; identity checks | internal/github |
| SQL, immutable revisions and durable effects | internal/store |
| Typed complete-story validation | internal/planbrief |
| Child discussion routing and mode selection | internal/daemon and existing story-refinement admission |
| Complete publication, shared index, readiness and save | internal/turn Plan owners |
| Neutral source rendering without installed-tree writes | internal/teamsource and internal/render |

## Acceptance Obligations
| ID | Observable requirement | Candidate proof |
|---|---|---|
| R1 | One admitted story activity creates/attaches usable incomplete drafts, updates the same issue and retires only after owner confirmation using not-planned semantics. | Go activity/policy tests and forge boundary fixture assertions for payload, parent identity and closure reason. |
| R2 | Retries, lost responses, partial attachment, stale content and remote drift preserve identity/owner edits and never report uncertain effects as applied. | Failure-injected forge tests plus durable store/restart checks. |
| R3 | Shared DOT and role/skill package separates three stages, excludes technical/criteria obligations from first-stage output, and exposes explicit human gates without altering Research. | Graph parser/admission tests, role-contract checks, source review and in-memory rendering for both supported harnesses. |
| R4 | A three-story scenario advances two independent stories while one remains in shaping; editing one retains unrelated approval and reopens an affected dependent. | Persisted graph execution tests with controlled owner replies and revision changes. |
| R5 | Stale/ambiguous/unauthorized replies, questions and exhausted loops cannot advance stages; restart resumes the exact child wait/candidate. | Negative authorization and recovery tests with fake runners and real store boundary. |
| R6 | Technical rejection proposes retirement, owner confirmation removes the story with history preserved, and final validation catches broken dependencies/empty scope. | End-to-end factory orchestration fixtures through activity and final handoff. |
| R7 | Third-stage final approval binds complete current stories and shared decisions; publication reuses draft issue identities and readiness requires confirmed publication/save. | Full workflow/handoff fixture including uncertain remote effects and save failure. |
| R8 | New eligible Plan runs use the graph; obsolete default dispatch and unused neutral Plan instructions are removed; supported persisted cohorts retain explicitly inventoried compatibility paths and restart behavior. | Production registration/dispatch tests, legacy cohort regressions, source inventory of removed and retained paths with retention prerequisites. |
| R9 | The feature passes repository-required formatting, vet, pinned lint and race tests without touching the running daemon. | Executed make check during Implement; no execution claimed in Plan. |

## Evidence Availability and Limits
- Repository source, fake-runner tests, temporary SQLite stores and controlled forge responses are available for deterministic workflow and recovery proof.
- Those fixtures prove source behavior only; they do not prove actual hosted forge operations or authentic nested Claude/Codex execution.
- No new provider capability or authentic nested-agent claim is required; existing external host-observation obligations remain separately unsatisfied until authorized operator evidence exists.
- Missing docs/story-contract-instruction-manifest.md and docs/decisions/nested-agent-evidence.md were checked; canonical teams/source/skills/workflow/SKILL.md supplies current submission, protected-tree and synthetic-evidence limits.
- Retirement inventory must identify any unsupported-to-remove compatibility path and its real cohort/pending-effect prerequisite; passing offline tests cannot waive that prerequisite or count retained required paths as removed.

## Out of Scope
- Production implementation, remote issue creation and workflow activation during this Plan run.
- Additional activity names, changes to Research agents, new planning UI or a fourth approval stage.
- Permanent issue deletion, automatic promotion/retirement/implementation or resetting unrelated approval sets.
- Automatic migration of active legacy plans, rewriting historical accepted stories, or removal of compatibility required by supported persisted state.
- Direct changes to installed protected instruction trees; if later required, admit independent harness-owned scope rather than silently expanding these three stories.
- Launching nested paid agents or manufacturing external runtime evidence.

## Owner Questions
- None; latest instructions and recorded decisions settle delivery scope.

## Run 0005 Amendment: Activity-Owned Story Presentation
- Owner requirement: the story activity owns title construction and body rendering; agents supply typed content rather than formatted issue titles or full Markdown bodies.
- Scope remains the same three delivery stories: activity; agents and DOT; wiring and retirement. Apply presentation requirements within those boundaries without adding a story.
- Interpret the owner's wording “activity bot by the agents” as “activity, not the agents.”

### Title and Stable Ordinal
| Field | Requirement |
|---|---|
| Title | `[Main Story ID]-[Numerical Id in 00x format]: Story Name`; example: `362-001: Story Name`. |
| Main Story ID | Parent issue number; do not substitute the child issue number or a transient run identity. |
| Numerical ID | Persisted per-parent story ordinal, starting at 1 and rendered with a minimum of three digits (`001`, `002`, …, `1000`). |
| Allocation | Bind the ordinal durably to the stable story identity before recoverable publication; retries, restart, update and retirement never renumber or reuse an allocated ordinal. |
| Story Name | Current typed story title supplied by the agent; the activity adds the parent and ordinal prefix. |

### Ordered Body Contract
| Order | Exact heading or label | Content |
|---|---|---|
| 1 | `# Userstory` | Current actor, want and benefit from typed story content. |
| 2 | `## What this story solves` | Brief plain-language description of the problem this story solves. |
| 3 | `## Scope` | Separate `In-Scope` and `Not in Scope` lists from typed scope content. |
| 4 | `# Task` | Table with columns `Task Id`, `Description`, `Owner`, `Dependencies`, in that order; rows are filled during later shaping. |
| 5 | `# Acceptance Criterias` | Criteria filled during later shaping, retaining the existing criterion-ID and checklist format. |
| 6 | `# Architecture` | Architecture filled during later shaping, retaining the existing architecture content format. |
- Render every heading in this exact order and spelling from the first draft; later-stage sections remain unpopulated until their stage supplies content, and the initial task table has headers but no invented task rows.
- Empty later-stage scaffolding does not introduce technical decisions, task obligations or criteria into story-only shaping.
- Preserve stable criterion/task identities, task owners and links, story dependencies, semantic metadata and revision checks independently of the changed visible layout; do not add a visible Dependencies heading to this specified sequence.
- The new renderer and its handoff must support the requested presentation without weakening draft validation, complete typed-plan validation, approval binding or owner-edit conflict handling.
- Final publication and subsequent updates preserve the activity-assigned title/ordinal and body contract while continuing to reuse the same issue identity.

### Additional Acceptance Obligations
| ID | Observable requirement | Candidate proof |
|---|---|---|
| R10 | The activity produces titles such as `362-001: Story Name` from parent identity, durable ordinal and typed story name; retries, restarts and retirement cannot reuse or renumber ordinals. | Allocation/recovery tests including partial publication, retired stories, independent parents and ordinals exceeding three digits. |
| R11 | First drafts and later revisions render the exact ordered body contract; initial later-stage sections are empty, and subsequent typed content fills them with the existing criteria and architecture formats. | Renderer fixtures for first-stage, technical-stage and complete stories, checking heading spelling/order and task table columns. |
| R12 | Agents supply structured intent/problem/scope and later-stage content; final handoff preserves activity formatting, semantic dependencies and IDs while still rejecting incomplete final plans and conflicting remote edits. | Role-contract checks and final publication/reconciliation tests through the existing validators. |

### Amendment Readiness
- Ready for plan assembly; no material owner questions remain.
- No production code, runtime checks or remote effects are claimed by this amendment.
