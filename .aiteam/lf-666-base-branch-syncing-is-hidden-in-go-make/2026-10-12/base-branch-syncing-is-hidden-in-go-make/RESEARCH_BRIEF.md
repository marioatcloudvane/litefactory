# Research brief

## Request

Shape the additional lf_sync_base activity: settle graph placements, bindings, permissions, retained-work safety, outcome routes and release boundaries while preserving current syncing logic.

## Decisions

- **d1** — The owner's direct instruction preserves current logic and makes this additive, superseding the issue body's proposed removal of hidden synchronization.
- **d2** — Reuse the existing activity, output fields and four outcomes rather than add a second merge mechanism.
- **d3** — Bind branch targets and mutation permissions from admitted session authority rather than agent text or node attributes.
- **d4** — Keep all legacy preparation syncing and doctor repair behavior intact, including for graphs that add explicit sync.
- **d5** — Require compatible branch-writer locking and recognize the already-held Bug task lock without recursive acquisition.
- **d6** — Preserve retained edits and unpublished commits and make the published head visible before the next agent runs.
- **d7** — Stop with an actionable reason when local divergence cannot be reconciled safely instead of discarding work or allowing a stale agent checkout.
- **d8** — Keep invalid targets, missing branches, cancellation and infrastructure failures as errors rather than successful graph outcomes.
- **d9** — Bound new sync retry loops to eight attempts and park on exhaustion.
- **d10** — Modify canonical team sources only and leave release generation, version selection and repository linking to the owner.
- **d11** — Acceptance requires observed graph attempts, correct target selection, declared outcome routes, safe local refresh and unchanged behavior for pinned graphs.
- **d12** — The owner selected q1 bug_worker: add sync before every approved Bug fixer and permitted Implement story-worker attempt across canonical teams, leaving other graphs opt-in.
- **d13** — The owner selected q2 explicit: explicit graph calls remain independent of automatic-sync enablement and behind limits.
- **d14** — The owner selected q3 park: new Bug and story sync conflicts park with branch names, available conflict paths and a reason, then retry through existing owner gates.
- **d15** — Place new sync steps after Bug approval or story budget admission and before initial, revalidation or repair worker attempts.
- **d16** — Sync retries do not consume the worker repair budget.
- **d17** — Route synced and current to the pending worker, conflict to parking and retry through a bounded sync loop.
- **d18** — Park when existing authorized save and reconciliation cannot preserve local work safely before publication or local refresh.


## Assumptions

- **a1** — The existing generic activity and outcome vocabulary are sufficient because their current contract already covers clean merges, conflicts and contention.
- **a2** — Expose the activity to admitted graph modes and Bug while making Research, Spike, Plan and Validate opt-in only, since no supplied user story requires default sync in those graphs.
- **a3** — Use the existing story parent-branch selection including its repository-base fallback because that choice also determines story starting branches.
- **a4** — No new persistence schema is needed because graph attempts and existing activity output already record the required sync evidence.
- **a5** — Existing automatic-sync settings and behind limits remain unchanged, while the owner settled explicit-call independence in d13.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
