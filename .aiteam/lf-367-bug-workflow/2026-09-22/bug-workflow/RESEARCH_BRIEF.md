# Research brief

## Request

Research #367: a lighter graph-driven bug lifecycle with clarification, Root Cause, Impact and Solution analysis, followed by code repair, unit-test updates and a graph-driven suite rerun; reuse #319 activities after that dependency is completed.

## Decisions

- **d1** — Owner-set direction: use the existing graph engine and keep the bug workflow lighter than the existing feature workflows.
- **d2** — Owner-set dependency: wait for #319 to be completed before implementing the activities-dependent bug workflow. This pass may establish facts and hand them to shaping without claiming that prerequisite is met.
- **d3** — Owner-set content: analysis covers Root Cause, Impact and Solution; repair covers code, unit tests and a suite rerun. Ambiguity about where a reported bug occurs must trigger clarification.


## Assumptions

- **a1** — Interpret '# intent per category' as separately identifiable analysis sections for Root Cause, Impact and Solution. The request does not settle whether each section is a distinct graph node or role; shaping owns that choice.
- **a2** — Treat 'entire lifecycle' as an unresolved completion boundary. The minimum explicit scope reaches repair and suite results; acceptance, PR creation, merge and issue closure must not be silently included or excluded.
- **a3** — Open design points for shaping: workflow entry and mode representation, authority to move from analysis into repair, analysis completeness when reproduction fails, complexity thresholds for leaving the lightweight path, full-suite ownership and child-graph composition. These choices do not prevent the bounded factual handoff.
- **a4** — Dependency activity names and contracts from #319 are provisional upstream proposals. Recheck the delivered implementation before selecting adapters or deciding which bug-specific behavior is new; do not copy proposed interfaces as settled data.
