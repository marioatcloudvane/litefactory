# Plan: sketch-out-plan-workflow

- Branch: `lf/362-sketch-out-plan-workflow`
- Session date: 2026-09-21
- Parent: https://github.com/marioatcloudvane/litefactory/issues/362
- Citation base: `29e069e328d5d240b93e8a7039e7d5c2a0ef7361`
- Status: completed typed proposal; publication, save and readiness are factory-owned and unconfirmed here.

## Shared Decisions

- Deliver exactly three stories: the story activity, agents and DOT, then wiring and old-code retirement.
- Add only `lf_plan_story`; reuse existing graph execution and conversation controls. The factory binds authority, identities and revisions; agents propose structured content.
- Usable stories become stable sub-issues during story shaping. Stage candidates retain immutable provenance; GitHub remains the editable story surface. Final publication reuses those identities.
- Keep three stages: intent only; technical shaping; criteria and assembled-plan review. Explicit approval binds displayed candidate and relevant upstream/shared-decision revisions. No fourth stage, automatic promotion or automatic mode change.
- Persist independent story progress and dependency-aware invalidation. Changed scope reopens only affected stages/dependents, while uncertainty requires impact resolution. Canonical active-set changes invalidate assembled approval.
- Interpret deletion as exact owner-confirmed retirement with not-planned closure and preserved history.
- Deliver neutral source assets and their captured runtime closure. Installed protected instruction trees are outside these stories.
- Stage approval is distinct from final implementation freeze. Pending effects, acknowledgment and draft publication never establish readiness; complete validation, exact final approval, confirmed publication and save are required.

## Workflow

`plan-story-activity` → `plan-agents-dot` → `plan-wiring-retirement`

The activity supplies the shared draft/effect contract. The package consumes it without activating production dispatch. Wiring consumes both delivered stories and owns combined regression/engineering checks. The three stories are deliberately broad owner-requested boundaries; wiring contains the largest state/recovery scope.

## Cross-Story Constraints

- Keep SQL/migrations in store, forge effects in the existing forge boundary, time/retries in daemon, and one-turn orchestration in turn. Preserve the single SQLite connection and never hold a transaction across external I/O.
- Retain durable request/candidate correlation and distinguish conflict, failure and uncertainty. Reconcile lost replies before another mutation; observed revision checks are not atomic remote compare-and-swap.
- Preserve Research behavior and its independent activation/evidence gate. Do not create a second graph engine or weaken complete-story validation.
- Keep existing persisted routes and supported legacy recovery until explicit supported adoption. Production adoption and legacy removal need separate evidence; retained paths must name their cohort/removal prerequisite.
- Tests use ordinary Go files, real temporary stores and controlled forge/git boundaries where needed. No integration build tags or special Integration test names. Combined engineering must pass `make check`; never write `dist`, restart the daemon, commit or push.
- Per-story architecture records carry criterion evidence inventories. Offline fixtures establish implementation behavior only; authentic hosted/provider observations remain separate external obligations. No tests or runtime effects were executed during Plan.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| plan-story-activity | Manage planning sub-issues through a recoverable story activity | [#363](https://github.com/marioatcloudvane/litefactory/issues/363) |
| plan-agents-dot | Deliver the three-stage Plan agents and DOT workflow package | [#364](https://github.com/marioatcloudvane/litefactory/issues/364) |
| plan-wiring-retirement | Activate the Plan workflow and retire obsolete Plan dispatch | [#365](https://github.com/marioatcloudvane/litefactory/issues/365) |

## Out of Scope

- Production edits, workflow activation or remote story creation by this Plan worker.
- Permanent deletion; automatic retirement/promotion/implementation; a new planning UI or fourth review stage.
- Changing Research agents, rewriting accepted implementation history, automatic migration, or removing compatibility still needed by supported state.
- Protected installed-tree edits, new provider capabilities or nested paid-agent execution.

## Provenance

Owner instruction and saved Research context settle the three-story scope and independent per-story progress. Requirements are recorded in `feature-spec.md`; structural review and the authorized narrow CONC-22 reconciliation are in `ARCH_REVIEW.md`. Typed story content is carried by the factory submission rather than local per-story Markdown.

Source findings are pinned to the citation base above. Recheck touched interfaces, dispatch callers and persisted compatibility cohorts against the implementation revision; do not treat line locations or legacy-removal assumptions as immutable. The two referenced documentation paths absent in this checkout were not invented; the canonical shared workflow contract supplied submission/protected-tree/evidence limits.

No owner questions remain. Assumptions to retain visibly are not-planned retirement, neutral package delivery and preservation of required legacy compatibility. Publication links and readiness must be filled/confirmed by the factory after successful finalization.
