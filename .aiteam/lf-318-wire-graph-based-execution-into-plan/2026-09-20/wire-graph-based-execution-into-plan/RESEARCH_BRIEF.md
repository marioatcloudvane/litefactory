# Research brief

## Request

Investigate wiring graph execution into Plan with owner-reviewed loops for story titles/descriptions/problems, engineering details, then acceptance criteria; challenge the assumption that only DOT and Markdown changes are needed and compare early versus final story creation.

## Decisions

- **d1** — Research only: establish current behavior and gaps. Do not implement, create child stories, publish directly or select Plan.
- **d2** — The latest owner message requests story shaping, engineering review, then acceptance shaping. This supersedes the earlier issue's proposed acceptance-before-architecture order for this investigation; it does not settle the final design.


## Assumptions

- **a1** — Treat 'creation' as creating remote managed story issues, distinct from retaining private drafts. This matches the owner's distinction between proposing stories and creating them.
- **a2** — Use existing Plan discussion retention and final publication contracts as the reuse baseline. Late publication is a recommendation, not an owner-approved design decision.
- **a3** — Open shaping decisions: explicit stage approval signals; approval scope across multiple stories; which changes invalidate downstream drafts and approvals; handling splits, merges and dependency changes; final review before versus after issue creation; loop exhaustion and continuation behavior. These do not prevent establishing the current implementation facts.
- **a4** — Evidence is repository inspection at revision 7d339edde1326fc509e0a5966d55db88ec77f274 in the supplied worktree. Existing tests were read, not executed. No live daemon, external issue state, authentic host recovery or implementation-size estimate was verified.
