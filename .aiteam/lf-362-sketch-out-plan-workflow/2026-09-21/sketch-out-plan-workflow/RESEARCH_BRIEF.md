# Research brief

## Request

Compare current Plan with the owner's proposed story-only, technical-shaping and acceptance conversations; explain improvements and missing rules; provide an advisory DOT workflow and Markdown/skill layout, reusing activities and preserving Research structure.

## Decisions

- **d1** — Owner requires three major loops: user stories without technical details or acceptance criteria; architecture/reuse/data-model shaping; then a separate acceptance conversation. Human promotion controls progression.
- **d2** — Owner requires reuse of DOT notation and existing activities where they fit, with the Research agent structure preserved. This pass provides research and a first draft, not implementation or published stories.


## Assumptions

- **a1** — Interpret dot notation as the repository's Graphviz DOT workflow format, supported by the shipped Research graph and the request to reuse activities.
- **a2** — Treat first-loop story creation as private draft shaping rather than immediate GitHub issue creation: the owner requests a draft workflow, while current managed publication requires complete stories.
- **a3** — The draft proposes an explicit owner gate after each loop. Exact promotion language, backward routing, draft schema, iteration limits and approval invalidation are open design points for shaping, not settled choices.
- **a4** — No new activity is established as necessary. Existing generic mechanisms and Plan publication owners are the reuse baseline; whether publication or document generation requires an explicit Plan effect adapter remains open.
