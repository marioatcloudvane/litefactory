# Feature Spec: a-run-never-reconciles-its-task-branch
# Status: READY FOR ARCHITECTURE — owner policy settled; factory publication/readiness pending
# Date: 2026-09-10

## Goal
- Prevent ordinary runs from inspecting an existing local task branch that silently omits fetched operator commits; automate safe reconciliation and reuse proposed recovery with owner acceptance when automation cannot complete.

## Sources and Settled Decisions
- Source: current owner instruction and run-0004 policy recorded in `RESEARCH_BRIEF.md`; earlier assistant recommendations are not independently approved requirements.
- Automatically perform safe fast-forwards and compatible merges; equal/ahead-only state needs no reconciliation.
- Preserve local work; propose recovery for owner acceptance when automation cannot complete; do not silently run against known stale task inputs.
- Reuse current Git safeguards, conflict advisor/proposal/confirmation flow, synchronization vocabulary and save lifecycle; add only the fields and behavior needed for task reconciliation.
- Preserve Validate retained-evidence review and exact-input approval freshness.

## User Stories
| ID | Story |
|---|---|
| US-1 | As an operator, I want my pushed task commits incorporated before normal execution so that runs inspect the intended branch state. |
| US-2 | As an operator, I want obstructed reconciliation explained through the familiar proposed-resolution and acceptance flow so that my work survives and I control nonautomatic recovery. |
| US-3 | As an operator, I want reconciliation outcomes and publication status reported accurately so that a successful local preparation is neither silently lost nor mistaken for a successful push. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| Synchronization result | Phase, compared reference, local and remote commits | Existing result extended with task/base discriminator and commit strings | For observed reconciliation | Distinguish task and base; never overwrite one phase with the other. |
| Synchronization result | Outcome, ahead, behind, reason, conflicts, resulting commit | Existing vocabulary and collections | Applicable fields | Missing historical task observation means unrecorded, never inferred success. |
| Recovery proposal | Target phase/ref and observed task remote state | Bounded fields extending existing proposal | New task-aware proposals and protected base approvals | Bind local HEAD and relevant remote inputs; preserve old records without granting unbound approval. |
| Save result | Local resulting commit and publication outcome | Existing result fields where possible | Successful and failed publication | Distinguish retained local work from confirmed publication. |

### Relationships
- Task/base observations belong to the existing task/session and preparation/run lifecycle.
- Recovery proposals retain existing session ownership, numeric-owner authorization, expiry, validation and consumption semantics.
- Retained implementation evidence remains bound to its recorded snapshot independently of current remote changes.

## API / Interface Changes
| Action | Auth | Notes |
|---|---|---|
| Ordinary run preparation | Existing authorized run | Observe current task remote after fetch; reconcile before base synchronization and runner execution. |
| Proposed recovery and confirmation | Existing owner confirmation | Extend existing target binding; no new policy commands or arbitrary command execution interface. |
| Reports/history | Existing reporting | Distinguish task/base results and prelaunch refusals; preserve historical report compatibility. |
| Successful save | Existing factory authority | Publish outstanding successful task commits even when preparation is the only source of change. |

## Business Rules
- Reused worktrees and new worktrees using an existing local branch obey the same reconciliation rule; cold/resumed ordinary executions share it.
- Establish reliable coverage of the task reference before interpreting absence; reuse the normal fetch where sufficient and minimally handle restrictive fetch configuration.
- Confirmed absent remote task branch remains an ordinary silent path; remote-only recovery remains supported.
- Clean behind-only state fast-forwards; clean compatible divergence merges preserving both histories; equal/ahead-only state remains unchanged.
- Task freshness is not disabled by base-sync opt-out or permissive base-conflict settings; existing base policy otherwise remains unchanged.
- Dirty work is preserved. When missing remote work prevents safe reconciliation, ordinary execution stops and exposes a concrete recovery proposal or explicit manual recovery guidance using the existing fallback experience.
- Where the advisor can produce a supported resolution, present it through existing preview/confirmation semantics; unavailable, excluded or invalid suggestions must be reported honestly and never manufacture an executable proposal.
- Confirmations verify local HEAD and relevant remote inputs before any application; changed inputs supersede the proposal and require a fresh proposal and acceptance. Do not reconcile first and silently reuse approval against changed inputs.
- Validate observes/discloses freshness without task/base merge mutation or replacement of retained review evidence.
- A successful save includes unpublished preparation-created commits, including those retained from earlier failed attempts; failure does not authorize publishing failed-run work or rolling it back.
- If remote movement prevents publication, preserve local work and report verified state plus proposed recovery. Automate recovery only when it leaves required verification and approvals valid; no unverified post-check merge may be called complete.

## Edge Cases and Error Handling
- Merge conflicts, unrelated histories, reserved-path violations or failed Git checks: preserve work, stop ordinary execution, identify the obstruction and offer the supported proposal/manual recovery route.
- Detectable rewritten history: do not infer destructive replacement authority; apply existing safety/approval policy. Ahead/behind alone is not evidence of a rewrite.
- Missing tracking reference without reliable fetch coverage: establish observation or report inability; never treat uncertainty as a confirmed new branch.
- Prelaunch refusal: preserve a reportable outcome even though no agent run was allocated; do not imply an agent executed.
- Final push race: distinguish verified remote movement from other push failures; never force-push or discard local commits.
- Existing base-only proposals with insufficient task-input binding: retain history but require refreshed safe approval before applying against unverified remote state.

## Assumptions
- Existing manual-resolution fallback is the intended recovery experience when the advisor cannot safely represent dirty work, excluded files or exceptional history; this feature does not introduce a general command approval executor.
- "Automatically can be done" means preserving local work, reserved-path safeguards, exact approval inputs and required checks; mechanical merge success alone is insufficient after those checks.
- A late remote change that requires changing the verified tree follows proposed recovery and a newly verified run; simple publication recovery that preserves the verified tree can proceed automatically.
- No new persistent remote-history tracker is required; confirmed absence is allowed without attempting to distinguish an undocumented deletion from first publication.
- Existing base-sync caps remain base policy; the architect should not introduce a second task-distance policy or configurable stale-tree bypass.

## Out of Scope
- General issue-cache/session-cache freshness changes or historical incident reconstruction.
- Destructive resets, force pushes, automatic stashing/discard, history-rewrite heuristics or a new remote-history subsystem.
- New recovery DSL, arbitrary shell-command approval mechanism, background retry scheduler or unbounded retry/reverification loop.
- Changes to historical acceptance semantics, evidence expiry or Validate check execution.
- Agent instruction-tree changes, nested live agent experiments, or claims of authentic harness capability.

## Candidate Acceptance Criteria
- [ ] AC-001: Ordinary runs incorporate confirmed remote task commits before base sync/runner execution: fast-forward clean behind-only state, merge compatible divergence, preserve equal/ahead-only state, and cover reused worktrees plus existing-local-branch worktree creation.
- [ ] AC-002: Confirmed absent and remote-only task branches preserve normal startup; restrictive fetch coverage cannot silently hide an existing remote task branch.
- [ ] AC-003: Obstructed task reconciliation preserves local edits/commits and prevents stale ordinary execution, with concrete obstruction and proposed recovery; supported merge resolutions use existing owner-confirmed previews, and unsupported suggestions use explicit existing manual fallback.
- [ ] AC-004: Task and base recovery confirmations retain numeric-owner controls and refuse stale local/relevant remote inputs without mutation; Validate does not synchronize code or alter retained evidence targets.
- [ ] AC-005: Reports distinguish task/base outcomes and retain truthful prelaunch refusal diagnostics; historical reports do not invent task synchronization evidence.
- [ ] AC-006: Successful save publishes outstanding preparation-only commits even when the runner adds no change; failed attempts retain work and accurately report unpublished state.
- [ ] AC-007: Concurrent remote movement never causes force-push, lost local work, stale approval application or success claims for rejected publication; recovery follows the automate-if-still-valid, otherwise-propose rule.

## Candidate Evidence Inventory
| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-001 | Offline real local Git repositories and fake runner assertions on inspected HEAD/order | Existing internal/git and internal/turn fixtures; new cases required | Checked-out implementation and pinned Go/Git environment; proves local workflow behavior | Implement must execute and retain checks. |
| AC-002 | Offline local remote refs/refspec scenarios | Existing worktree/fetch fixtures; new cases required | No GitHub network behavior or historical incident claim | Implement must execute and retain checks. |
| AC-003 | Offline dirty/conflicted/unsupported cases with fake advisor and runner counters | Existing dirty and conflict fixtures; new task-target cases required | Fake advisor proves orchestration and preservation, not model suggestion quality | Implement must execute and retain checks. |
| AC-004 | Offline confirmation input-change cases and retained-evidence Validate cases | Existing apply/Validate fixtures; new task binding cases required | Proves authorization/freshness logic, not authentic nested agent operation | Implement must execute and retain checks. |
| AC-005 | Store round trips, migration/history fixtures and report assertions | Existing store/report/turn tests; new two-phase cases required | Local SQLite/rendering and prelaunch outcome lifecycle | Implement must execute and retain checks. |
| AC-006 | Real local remote save cases with no-op/success/failed fake runner | Existing sync persistence and harvest fixtures; new cases required | Local commit/push state; no external delivery capability claim | Implement must execute and retain checks. |
| AC-007 | Deterministic second-writer local Git race and result assertions | Existing real Git fixtures; injected remote advancement required | Bounded publication race; no perpetual synchronization guarantee | Implement must execute and retain checks. |

## Readiness
- Requirements ready for architecture and typed story authoring; no unresolved owner question.
- Candidate criteria must receive canonical IDs in their final story scope and matching evidence inventories; feature-spec is shared context, not an editable story source.
- No checks executed during requirements work; no nested-runtime evidence obligation is introduced by this feature.
