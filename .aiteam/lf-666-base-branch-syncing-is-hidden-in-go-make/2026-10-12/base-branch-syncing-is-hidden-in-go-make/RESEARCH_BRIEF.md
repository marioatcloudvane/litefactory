# Research brief

## Request

Shape the additional lf_sync_base activity: settle graph placements, bindings, permissions, retained-work safety, outcome routes and release boundaries while preserving current syncing logic.

## Decisions

- **d1** — The owner's direct instruction preserves current logic and makes this additive, superseding the issue body's proposed removal of hidden synchronization.
- **d2** — Reuse the existing activity, output fields and four outcomes rather than add a second merge mechanism.
- **d3** — Bind branch targets and mutation permissions from admitted session authority rather than agent text or node attributes.
- **d4** — Keep all legacy preparation behavior and doctor repair behavior for graphs that do not opt in.
- **d5** — Require compatible branch-writer locking and recognize the already-held Bug task lock without recursive acquisition.
- **d6** — Preserve retained edits and unpublished commits and make the published head visible before the next agent runs.
- **d7** — Stop with an actionable reason when local divergence cannot be reconciled safely instead of discarding work or allowing a stale agent checkout.
- **d8** — Keep invalid targets, missing branches, cancellation and infrastructure failures as errors rather than successful graph outcomes.
- **d9** — Bound new sync retry loops to eight attempts and park on exhaustion.
- **d10** — Modify canonical team sources only and leave release generation, version selection and repository linking to the owner.
- **d11** — Acceptance requires observed graph attempts, correct target selection, declared outcome routes, safe local refresh and unchanged behavior for pinned graphs.


## Assumptions

- **a1** — The existing generic activity and outcome vocabulary are sufficient because their current contract already covers clean merges, conflicts and contention.
- **a2** — Expose the activity to admitted graph modes and Bug while making Research, Spike, Plan and Validate opt-in only, since no supplied user story requires default sync in those graphs.
- **a3** — Use the existing story parent-branch selection including its repository-base fallback because that choice also determines story starting branches.
- **a4** — No new persistence schema is needed because graph attempts and existing activity output already record the required sync evidence.
- **a5** — Preserve automatic-sync settings and behind limits unchanged; the policy for explicit graph calls remains subject to q2.


## Open questions

### q1 — Which graphs should gain default sync steps in this change?

_Asked of: Product Owner Questions_

- **bug_worker** — Recommended: add sync before each approved Bug fixer attempt and each Implement story-worker attempt across canonical teams; retain coordinator steps and leave other graphs opt-in.  *(recommended)*
- **capability_only** — Expose and document reusable bindings only; leave all default graph topology unchanged so teams choose placements later.
- **all_writers** — Also add default sync before Plan, Research and Spike agent work; this broadens automatic branch mutation and needs their graph-specific routes.

### q2 — Should an explicit graph sync keep its existing policy when automatic syncing is disabled or capped?

_Asked of: Product Owner Questions_

- **explicit** — Recommended: keep explicit calls independent of automatic-sync enablement and behind limits, because placing the step is the team's deliberate opt-in; still preserve local work and require admitted write authority.  *(recommended)*
- **respect_settings** — Make explicit calls obey automatic-sync enablement and behind limits; define a distinct skipped outcome and graph routes so skipped work is never reported current.

### q3 — How should conflicts from the new Bug and story-worker sync steps be handled?

_Asked of: Product Owner Questions_

- **park** — Recommended: park the Bug workflow or story with branch names, conflict paths when available and a reason; resume through the existing owner gate and retry sync, keeping this change focused on the additional activity.  *(recommended)*
- **resolve** — Add dedicated conflict-resolution paths that save a resolution and rerun sync; Bug resolution must stay within the approved remedy or return to the owner for a changed remedy, which expands the scope.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
