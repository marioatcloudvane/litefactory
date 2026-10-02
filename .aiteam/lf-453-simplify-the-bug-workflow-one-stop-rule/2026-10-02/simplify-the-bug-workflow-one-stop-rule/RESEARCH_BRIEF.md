# Research brief

## Request

Heavily simplify Bug to two loops using lf_loop and lf_agent, followed by one final activity that merges to main and closes. First establish a clear bug and proposed solution with a flagship analyst and minimal role Markdown; then repair until verified fixed. Remove substantial old Bug machinery and use communication JSON exclusively. The latest owner instruction supersedes the older proposed multi-activity graph.

## Decisions

- **d1** — Owner-set direction: only loop, agent and final merge/close activity types; two stages, diagnosis/solution then repair/verification. This is the latest explicit request, not the older issue sketch.
- **d2** — Owner-set requirements: flagship analyst, minimal role Markdown, substantial code reduction, and strict shared JSON communication.


## Assumptions

- **a1** — Treat minimal Markdown as a request to simplify role instructions and prose, not to replace the JSON transport: the owner explicitly requires JSON communication.
- **a2** — Use the repository's configurable flagship tier rather than hard-coding a vendor model; the analyst already declares this tier.
- **a3** — The earlier one-stop/any-owner-reply intent remains relevant unless shaping changes it; the latest instruction does not specify the exact wait mechanism or approval semantics.
- **a4** — Open shaping points: placement of publication/wait/resume within the permitted activities; what evidence makes the second loop verified; final merge approval and recovery semantics; cutover for pinned tasks; historical schema retirement. No live daemon state, active-task inventory or deployed migration state was inspected.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
