# Research brief

## Request

Shape factory-controlled Implement: settle story readiness, specialist interfaces, test recipes and evidence, bounded repair and recovery, engineering handoff, and a coordinated versioned Validate test boundary.

## Decisions

- **d1** — This shaping pass defines the proposed scope for owner review without implementing changes, creating child issues or entering Plan.
- **d2** — The owner's current direction requires DOT-controlled workflow execution and replaces agent orchestration with factory scheduling.
- **d3** — Users continue to select modes, and neither graph completion nor Validate failure automatically starts another mode.
- **d4** — Implementation consumes approved frozen stories and preserves existing permission, result, publication and save boundaries.


## Assumptions

- **a1** — Proposed dependency rule: a prerequisite releases its dependents only after implementation and every required tailored check pass with retained evidence; full-suite verification and acceptance remain separate.
- **a2** — Proposed graph surface: one editable Implement DOT workflow uses registered lf_implement_* actions to control a repeated story lifecycle over a separate frozen dependency DAG, without adding nested DOT syntax.
- **a3** — Plan #362 remains unverified, so compatibility must be demonstrated against an actual frozen-plan version rather than inferred from that issue's status.
- **a4** — The material pending decisions are q1 through q4; all other scope details below are shaping proposals based on the supplied findings rather than claims of delivered behavior.
- **a5** — Primary user story: after the owner selects Implement for an approved plan, the factory selects ready work, invokes specialists, executes checks, repairs bounded failures and reports engineering progress without routine approval gates.
- **a6** — Proposed execution record: retain the feature and frozen-plan generation, story identities and revisions, dependency edges, workflow and role content hashes, contract version, deterministic dispatch order, lifecycle state, attempt identities, retry counters and evidence references.
- **a7** — Proposed readiness validation: reject empty or incompatible plans, duplicate identities, missing or self dependencies, cycles and broken parent relationships before dispatch; order ready stories by their frozen plan order with stable story identity as a tie-breaker.
- **a8** — Proposed specialist interface: dispatch the implementation role with the complete selected frozen story and completed dependency context, then the unit-test role and a boundary-test role only when the story requires real-boundary checks; agents do not choose the next story or grant completion.
- **a9** — Proposed test-recipe record: retain a version and digest, story revision, criterion-to-check mapping, executable and argument list, repository-relative working directory, required prerequisites, declared environment references, timeout and expected exit/result criteria.
- **a10** — Proposed recipe authority: test specialists author tests and recipes within frozen scope, while the factory validates paths, permissions, prerequisites, finite timeouts and criterion coverage before executing them without a routine owner approval.
- **a11** — Proposed evidence record: factory executions retain attempt and recipe identities, exact source snapshot, start/end times, exit status, redacted output references and pass/fail/timeout/cancelled/skipped outcomes; an empty, unexecuted, skipped or stale required check never passes.
- **a12** — Proposed invalidation rule: any source change after testing requires all required checks for the changed story to rerun against the new snapshot, and reopening a prerequisite invalidates completion of its transitive dependents.
- **a13** — Proposed repair boundary: fixes remain inside the frozen story scope and permissions, while missing infrastructure, denied permissions or a need to change approved scope produces an explicit blocker rather than automatic administration or silent replanning.
- **a14** — Proposed recovery rule: persist state before dispatch, recover known attempt receipts before replay, never double-count completion, stop active work on cancellation and preserve retry budgets across restarts and resumes.
- **a15** — Proposed completion interface: publish story engineering state and dependency release separately from aggregate verification, save status, acceptance and closure, with a final handoff containing snapshot, evidence, unresolved work and required Validate checks.
- **a16** — Proposed compatibility rule: accept existing frozen plans only through an explicit supported-version mapping, pin running executions to immutable definitions, and keep the old aggregate-test requirement until the coordinated Validate contract is available.
- **a17** — Proposed exclusions: parallel writers unless q1 changes scope, a general nested-DOT engine, automatic mode changes, automatic acceptance or closure, legacy-path retirement, external infrastructure provisioning, Temporal adoption, a visual editor, a marketplace and fleet management.
- **a18** — Proposed success criteria: deterministic readiness, dependency release only after valid tailored evidence, bounded recovery across crashes, honest cancellation/exhaustion, preserved scope and permissions, and no reduced-check activation without a working full-suite Validate entry must be demonstrated before rollout.


## Open questions

### q1 — Should the first release run one story at a time?

_Asked of: Product Owner Questions_

- **serial** — Recommended: run each story through implementation and tailored tests serially, while retaining its dependency graph for later concurrency; this keeps isolation and merge handling outside this release.  *(recommended)*
- **parallel** — Include parallel independent stories now, requiring isolated workspaces, conflict handling and checks against the merged result before dependency release.

### q2 — Do you approve the proposed split between Implement and Validate?

_Asked of: Product Owner Questions_

- **split** — Recommended: Implement runs all story-tailored checks, including story-level acceptance-test execution; owner-selected Validate runs full-suite and combined acceptance tests before read-only acceptance review, and failures return a repair handoff for an owner-selected Implement run; activate the reduced-check contract only when both paths are ready.  *(recommended)*
- **retain** — Keep full-suite execution in Implement for this release and leave Validate as a read-only evidence review; defer the test-policy migration.

### q4 — Should unrelated ready stories continue after one story exhausts its repair budget?

_Asked of: Product Owner Questions_

- **continue** — Recommended: mark the failed story exhausted, block its dependents and continue independent ready stories, then report partial engineering completion; cancellation or an unsafe shared workspace stops the whole run.  *(recommended)*
- **stop** — Stop the whole run on the first exhausted story, preserving completed work and all remaining stories for an explicit resume.

### q3 — Should each story get two automatic repair cycles with durable retry limits?

_Asked of: Technical Questions_

- **two** — Recommended: allow the initial attempt plus two diagnosis/fix/retest cycles; failed implementation attempts or executed required checks consume a cycle, infrastructure blockers pause without automatic retry, resumes and recipe edits do not reset the budget, and only an explicit owner retry or new approved story revision starts a fresh recorded budget.  *(recommended)*
- **one** — Use the same durable rules but allow only one repair cycle, reducing cost while returning more unfinished stories to the owner.
