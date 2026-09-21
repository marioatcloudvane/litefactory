# Research brief

## Request

Assess factory-controlled Implement orchestration, bug loops, final tests, story dependencies/parallelism and moving full-suite verification and acceptance testing to Validate; compare Research and Plan #362 as evidence allows.

## Decisions

- **d1** — The owner requested a first assessment and discussion, with shaping afterward. This pass does not implement changes or create child issues.
- **d2** — The current owner direction is exclusive DOT workflow execution and less agent orchestration. The older note about retaining both execution paths does not override this direction.


## Assumptions

- **a1** — Use separated workflow as the discussion baseline: dependency release includes passing tailored engineering checks, before aggregate verification or acceptance. This follows current behavior and is a recommendation for shaping.
- **a2** — Interpret subgraph conceptually as a story execution/dependency structure, not an approved requirement for nested DOT syntax. The current parser rejects subgraphs.
- **a3** — Plan #362 contents and delivery status remain unverified. Proceed with repository facts and clearly labelled historical reference evidence.
- **a4** — Open shaping choices: serial versus isolated parallel writers, test-recipe authority and provenance, durable retry accounting and reset conditions, Validate execution policy, and failure handoff to Implement. Mode selection remains owner-controlled.
