# Research brief

## Request

Establish why a terminal Research instance pinned to older shipped graph bytes blocks later owner comments, identify safe reuse and refusal paths, and assess related owner-command and story-progress queue failures in #371.

## Decisions

- **d1** — Owner-specified behavior: an old-pinned terminal instance with no unsettled work must not block any execution mode; a later authorized Research launch must create a successor using the current definition.
- **d2** — Owner-specified boundary: running old-pinned instances and instances with unsettled dispatches, owner waits or publications remain refused, with one confirmed diagnostic reply. Preserve resumeResearchDispatch's running-instance and capture guards.
- **d3** — Research only: establish facts and hand over. No implementation, production state edits, issue publication or mode selection was performed.


## Assumptions

- **a1** — The latest instruction makes item 5 the central reproduction; items 1–4 and #370 remain related handoff context rather than being silently dropped. Work packaging belongs to shaping.
- **a2** — The #320 instance IDs, affected-session list, deployment dates and manual repair are owner-reported evidence, not independently verified production observations. Repository evidence suffices to establish the failure mechanism.
- **a3** — Unsettled work should be judged from retained state, not instance status alone. Dispatch pending/reserved/running and owner wait waiting/claimed are relevant states; cancelled dispatches are terminal. Exact predicate, predecessor coverage, retry policy and operator command semantics remain shaping points.
