# Research brief

## Request

Investigate parallel execution of independent graph Plan story stages, including story, technical/architecture and acceptance drafts, using settings.max_concurrent_runs as the capacity boundary while preserving ordering, exact approvals, workspace safety and recovery.

## Decisions

- **D-1** — The owner's latest instruction makes the existing config.yaml global concurrency limit the capacity boundary; it supersedes the issue note's suggestion of an additional configurable per-parent cap.
- **D-2** — This bounded pass establishes current behavior and affected contracts only. Implementation and design selection belong to subsequent shaping.


## Assumptions

- **A-1** — Parallelism means independent stories may overlap; stages of the same story remain sequential. This follows the issue's explicit exclusion, and the owner's latest comment does not explicitly reverse it. Implement and Validate parallelism remain outside this request.
- **A-2** — The issue's reported setting of 5 and stage timings are owner observations, not measurements from this pass. No live config or daemon state was inspected. The repository default is 3, with supported values 1–16.
- **A-3** — Fairness for other issues remains a requirement from the issue note; it need not imply another numerical per-parent limit. Scheduling policy, state update strategy and workspace isolation remain design choices for shaping.
