# Research brief

## Request

Record the owner's approval of the reviewed graph-based Plan scope while preserving its findings, decisions, assumptions and mode boundaries.

## Decisions

- **d1** — Research only: shape the scope without implementing, creating child stories, publishing directly or selecting Plan.
- **d2** — Use the owner's latest requested order: story shaping, engineering review, then acceptance shaping.
- **d3** — Shaping decision: story shaping contains titles, descriptions and problems solved, with intended benefit expressed within the description.
- **d4** — Shaping decision: engineering records architecture, technical boundaries, dependencies and provisional tasks before acceptance finalizes criteria and completion signals.
- **d5** — Shaping decision: each draft has a stable identity, content revision, stage content and status, dependencies, decisions, assumptions and revision-bound owner approvals.
- **d6** — Shaping decision: the plan retains its source issue, pinned workflow and roles, active stage, draft-set revision, durable owner replies and per-story publication receipts.
- **d7** — Shaping decision: each owner review exposes the stage, revision, changes, questions and exact approval requested through the existing issue conversation.
- **d8** — Shaping decision: only authenticated owners approve, agents propose, and the existing factory publisher owns remote story effects.
- **d9** — Shaping decision: a rename preserves draft identity, while splits and merges create new identities linked to superseded drafts and removals exclude drafts from publication.
- **d10** — Shaping decision: publication readiness requires complete story content, current approvals for all three stages, no unresolved questions, valid task-to-criterion mappings and an acyclic dependency set with no missing or superseded targets.
- **d11** — Owner decision q1=b: final approval reviews the exact published story set and its revisions after creation, and any subsequent content change invalidates that final approval.
- **d12** — Shaping decision: recovery preserves pinned definitions, originating review and retained replies, and cannot apply duplicate or stale approvals to a newer revision.
- **d13** — Shaping decision: bounded-loop exhaustion pauses with retained work and an honest explanation, without approving, publishing or selecting another mode.
- **d14** — Shaping decision: publication retries reconcile existing receipts and uncertain remote outcomes while preserving story issue identity.
- **d15** — Shaping decision: provide the standard editable Plan DOT and role package, registered Plan actions and runtime publication integration while retaining both execution paths.
- **d16** — Shaping decision: verification must cover all owner gates, semantic rewinds, stale approvals, restart recovery, loop exhaustion, no intermediate publication, partial-publication replay and post-publication rejection with managed revisions.
- **d17** — Exclude partial-story publication after the first loop, legacy retirement, automatic mode transitions, visual editing, Temporal adoption, marketplace and fleet management.
- **d18** — Owner decision q1=b: create complete story issues after all three owner reviews and the consistency check, then request final approval on those issues.
- **d19** — Owner decision q2=a: each stage approves the whole current story set at the displayed revision, while discussion may focus on individual stories.
- **d20** — Owner decision q3=a: a meaningful change clears approval for the whole set at the earliest affected stage and every later stage while retaining content for revision.
- **d21** — Owner decision q3=a: story additions, removals, splits and merges reopen story shaping, design or dependency changes reopen engineering, and criteria-only changes reopen acceptance unless earlier intent changes.
- **d22** — Shaping decision: final-review rejection reopens affected stages under the same invalidation rule, reruns consistency checks and publishes managed revisions before requesting final approval again.
- **d23** — Shaping decision: successful publication enters awaiting-final-approval status rather than completing Plan or authorizing implementation.
- **d24** — Shaping decision: the acceptance-stage approval authorizes publication only after the consistency check passes for that same reviewed revision.
- **d25** — Shaping decision: final review waits until the complete published set and its current issue revisions are confirmed, including recovery from partial or uncertain publication.
- **d26** — Owner approval in run 0006: “we are done here” accepts the reviewed shaping scope, retaining q1=b, q2=a and q3=a without selecting another mode or authorizing implementation.


## Assumptions

- **a1** — Treat 'creation' as creating remote managed story issues, distinct from retaining private drafts, following the owner's distinction.
- **a2** — Reuse existing Plan discussion and managed publication contracts, adding a post-publication final-review gate as required by owner decision q1=b.
- **a3** — All three material questions are answered by q1=b, q2=a and q3=a, and the owner's run 0006 approval accepts the remaining reviewed shaping rules and recorded assumptions.
- **a4** — Evidence remains repository inspection at revision 7d339edde1326fc509e0a5966d55db88ec77f274; no tests, live recovery or authentic host observations were executed in shaping.
- **a5** — Use existing explicit admission controls for new graph Plan executions and retain active legacy sessions on their current path, following the agreed dual-path rollout boundary.
- **a6** — Workflow edits affect newly admitted executions only, because the agreed rollout pins active executions to immutable definitions.
- **a7** — A wording-only edit may preserve stage approvals only when a recorded comparison establishes unchanged meaning; uncertain impact is treated as semantic change.
- **a8** — An empty story set cannot authorize publication, and external dependencies must resolve to retained existing issue identities rather than missing draft references.
- **a9** — After loop exhaustion, an explicit owner continuation may start a fresh bounded attempt using retained work without implicitly approving any review.
- **a10** — Post-publication revisions preserve issue identity for surviving stories, retain supersession links for splits or merges, and remove superseded stories from the active review set through existing managed-story contracts.
- **a11** — The publication-kind defect is fixed according to the owner. This continuation does not independently verify the fix or a successful review publication; only the factory's explicit graph comment activity can establish that remote effect.
