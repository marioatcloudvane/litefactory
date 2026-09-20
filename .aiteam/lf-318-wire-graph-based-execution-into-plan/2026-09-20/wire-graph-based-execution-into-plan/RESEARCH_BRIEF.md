# Research brief

## Request

Shape graph-based Plan around owner-reviewed story descriptions, engineering, then acceptance, with durable reviews, explicit publication authority and safe return to earlier stages.

## Decisions

- **d1** — Research only: shape the scope without implementing, creating child stories, publishing directly or selecting Plan.
- **d2** — Use the owner's latest requested order: story shaping, engineering review, then acceptance shaping.
- **d3** — Proposed scope: story shaping contains titles, descriptions and problems solved, with intended benefit expressed within the description.
- **d4** — Proposed scope: engineering records architecture, technical boundaries, dependencies and provisional tasks before acceptance finalizes criteria and completion signals.
- **d5** — Proposed scope: each draft has a stable identity, content revision, stage content and status, dependencies, decisions, assumptions and revision-bound owner approvals.
- **d6** — Proposed scope: the plan retains its source issue, pinned workflow and roles, active stage, draft-set revision, durable owner replies and per-story publication receipts.
- **d7** — Proposed scope: each owner review exposes the stage, revision, changes, questions and exact approval requested through the existing issue conversation.
- **d8** — Proposed scope: only authenticated owners approve, agents propose, and the existing factory publisher owns remote story effects.
- **d9** — Proposed scope: a rename preserves draft identity, while splits and merges create new identities linked to superseded drafts and removals exclude drafts from publication.
- **d10** — Proposed scope: final readiness requires complete story content, current stage approvals, no unresolved questions, valid task-to-criterion mappings and an acyclic dependency set with no missing or superseded targets.
- **d11** — Proposed scope: final approval binds the exact complete draft set and becomes invalid after any content change.
- **d12** — Proposed scope: recovery preserves pinned definitions, originating review and retained replies, and cannot apply duplicate or stale approvals to a newer revision.
- **d13** — Proposed scope: bounded-loop exhaustion pauses with retained work and an honest explanation, without approving, publishing or selecting another mode.
- **d14** — Proposed scope: publication retries reconcile existing receipts and uncertain remote outcomes while preserving story issue identity.
- **d15** — Proposed scope: provide the standard editable Plan DOT and role package, registered Plan actions and runtime publication integration while retaining both execution paths.
- **d16** — Proposed scope: verification must cover owner gates, semantic rewinds, stale approvals, restart recovery, loop exhaustion, absence of intermediate publication and partial-publication replay.
- **d17** — Exclude early partial-story publication under the recommendation, legacy retirement, automatic mode transitions, visual editing, Temporal adoption, marketplace and fleet management.


## Assumptions

- **a1** — Treat 'creation' as creating remote managed story issues, distinct from retaining private drafts, following the owner's distinction.
- **a2** — Reuse existing Plan discussion and final publication contracts; the recommended late-publication policy remains pending q1.
- **a3** — The remaining material owner choices are publication timing (q1), approval granularity (q2) and semantic-change invalidation (q3); other scope rules are recorded as shaping proposals.
- **a4** — Evidence remains repository inspection at revision 7d339edde1326fc509e0a5966d55db88ec77f274; no tests, live recovery or authentic host observations were executed in shaping.
- **a5** — Use existing explicit admission controls for new graph Plan executions and retain active legacy sessions on their current path, following the agreed dual-path rollout boundary.
- **a6** — Workflow edits affect newly admitted executions only, because the agreed rollout pins active executions to immutable definitions.
- **a7** — A wording-only edit may preserve stage approvals only when a recorded comparison establishes unchanged meaning; uncertain impact is treated as semantic change.
- **a8** — An empty story set cannot authorize publication, and external dependencies must resolve to retained existing issue identities rather than missing draft references.
- **a9** — After loop exhaustion, an explicit owner continuation may start a fresh bounded attempt using retained work without implicitly approving any review.
- **a10** — Post-publication changes follow existing managed-story revision contracts rather than silently overwriting published or approved stories.


## Open questions

### q1 — When should Plan create the story issues?

_Asked of: Product Owner Questions_

- **a** — Recommended: keep drafts private through all three reviews and the final consistency check, then get approval of the complete set before creating issues; later implementation authorization remains separate.  *(recommended)*
- **b** — Create complete issues after the three reviews and consistency check, then obtain final approval on those issues; rejected final reviews require managed revisions.
- **c** — Create partial issues after story shaping; expand scope to define incomplete-story status, later updates and protection against premature implementation.

### q2 — Should each stage approve the whole story set or individual stories?

_Asked of: Product Owner Questions_

- **a** — Recommended: approve the whole current set at each stage, with discussion allowed on individual stories; a clear approval applies only to the displayed stage and revision, and acceptance review is followed by final approval of the assembled set.  *(recommended)*
- **b** — Approve stories individually and let them progress at different speeds; expand scope to track mixed-stage approvals and synchronize dependencies before final publication.

### q3 — How much review should a meaningful draft change reopen?

_Asked of: Product Owner Questions_

- **a** — Recommended: clear approval for the whole set at the earliest changed stage and every later stage, retaining prior content for revision; story additions, removals, splits and merges reopen story shaping, dependency or design changes reopen engineering, and criteria-only changes reopen acceptance unless they change earlier intent.  *(recommended)*
- **b** — Preserve approval of unaffected stories and reopen only changed stories and their transitive dependents; expand scope to track approval validity and demonstrate why each retained approval remains valid.
