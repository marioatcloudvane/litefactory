# Research brief

## Request

Revise the Implement scope from the owner's answers, place acceptance execution and assessment in Validate, and explain the dependency-graph connection, mode workflow and additional activity contracts.

## Decisions

- **d1** — This shaping pass defines the proposed scope for owner review without implementing changes, creating child issues or entering Plan.
- **d2** — The owner's current direction requires DOT-controlled workflow execution and replaces agent orchestration with factory scheduling.
- **d3** — Users continue to select modes, and neither graph completion nor Validate failure automatically starts another mode.
- **d4** — Implementation consumes approved frozen stories and preserves existing permission, result, publication and save boundaries.
- **d5** — Owner answer to q1: the first release executes stories serially.
- **d6** — Owner correction to q2: acceptance testing and acceptance assessment belong to Validate, while Implement retains scoped engineering checks.
- **d7** — Owner answer to q3: the repair-cycle limit is activity configuration with a default of two.
- **d8** — Owner answer to q4: after story exhaustion, continue independent ready stories and block the failed story's dependents.


## Assumptions

- **a1** — A prerequisite releases dependents after implementation and required scoped engineering checks pass with retained evidence, without waiting for acceptance or full-suite verification.
- **a2** — Proposed graph surface: one editable Implement DOT workflow uses registered lf_implement_* actions to control a repeated story lifecycle over a separate frozen dependency DAG, without adding nested DOT syntax.
- **a3** — Plan #362 remains unverified, so compatibility must be demonstrated against an actual frozen-plan version rather than inferred from that issue's status.
- **a4** — All four owner choices are recorded in d5–d8; the revised boundary and additional activity details form a completed scope draft for review rather than a claim of owner approval.
- **a5** — Primary user story: after the owner selects Implement for an approved plan, the factory selects ready work, invokes specialists, executes checks, repairs bounded failures and reports engineering progress without routine approval gates.
- **a6** — Proposed execution record: retain the feature and frozen-plan generation, story identities and revisions, dependency edges, workflow and role content hashes, contract version, deterministic dispatch order, lifecycle state, attempt identities, retry counters and evidence references.
- **a7** — Proposed readiness validation: reject empty or incompatible plans, duplicate identities, missing or self dependencies, cycles and broken parent relationships before dispatch; order ready stories by their frozen plan order with stable story identity as a tie-breaker.
- **a8** — Dispatch the implementation specialist and engineering-test specialist through the existing agent mechanism with the selected frozen story; no acceptance-test specialist or acceptance-review step runs in Implement.
- **a9** — A versioned engineering recipe records its digest, story revision, engineering purpose and scope, executable and arguments, repository-relative working directory, prerequisites, environment references, finite timeout and expected exit/result criteria.
- **a10** — Engineering-test specialists author scoped tests and recipes, while the factory validates scope, permissions, prerequisites and nonempty required engineering coverage before execution; recipes neither execute nor certify acceptance criteria.
- **a11** — Proposed evidence record: factory executions retain attempt and recipe identities, exact source snapshot, start/end times, exit status, redacted output references and pass/fail/timeout/cancelled/skipped outcomes; an empty, unexecuted, skipped or stale required check never passes.
- **a12** — Source changes invalidate the changed story's required check evidence, and reopening a prerequisite invalidates its transitive dependents' engineering completion while preserving historical evidence.
- **a13** — Repairs remain within frozen scope and permissions; missing infrastructure, denied permissions, invalid authority or required scope changes become explicit blockers rather than automatic administration or replanning.
- **a14** — Proposed recovery rule: persist state before dispatch, recover known attempt receipts before replay, never double-count completion, stop active work on cancellation and preserve retry budgets across restarts and resumes.
- **a15** — Proposed completion interface: publish story engineering state and dependency release separately from aggregate verification, save status, acceptance and closure, with a final handoff containing snapshot, evidence, unresolved work and required Validate checks.
- **a16** — Existing frozen plans need an explicit supported-version mapping, and reduced Implement checks remain disabled until a versioned Validate entry executes full-suite and acceptance checks against the handed-off snapshot.
- **a17** — Exclude parallel writers, general nested-DOT execution, automatic mode changes, acceptance testing or acceptance verdicts in Implement, automatic issue closure, legacy-path retirement, infrastructure provisioning, Temporal adoption, a visual editor, a marketplace and fleet management.
- **a18** — Proposed success criteria: deterministic readiness, dependency release only after valid tailored evidence, bounded recovery across crashes, honest cancellation/exhaustion, preserved scope and permissions, and no reduced-check activation without a working full-suite Validate entry must be demonstrated before rollout.
- **a19** — Interpret the owner's acceptance boundary as execution and assessment ownership: frozen acceptance requirements still guide implementation, but only Validate executes designated acceptance checks and records acceptance verdicts.
- **a20** — The dependency DAG is derived from approved story identities and dependency lists, and a DOT rendering is a view of those records rather than a second editable source of execution authority.
- **a21** — The scheduler selects one pending story whose prerequisites are engineering-complete, durably binds it as current, and returns ready, finished or stalled, with stalled meaning unfinished work exists but none is eligible.
- **a22** — Configure max_repairs on the repair-budget activity as a finite nonnegative integer defaulting to two, where zero disables automatic repair and the initial attempt is additional to the configured repair cycles.
- **a23** — Pin the repair limit at execution admission, reserve each repair cycle durably before specialist dispatch, and retain counts across crashes, resumes, recipe edits and workflow-file edits.
- **a24** — Only a new explicit owner retry authorization or a new approved story revision may create a fresh budget epoch, with prior attempts retained and cancellation or infrastructure failure never causing an automatic reset.
- **a25** — Before continuing after a blocked or exhausted story, retain its failed delta and diagnostics and restore the factory-owned workspace to that story's starting checkpoint, stopping if restoration cannot preserve unrelated work.
- **a26** — Activity contracts are prepare for admission and recovery, next for DAG scheduling, recipe for engineering-recipe validation, checks for execution and evidence, repair for budget reservation, record for completion or failure isolation, and handoff for final evidence and save-state reporting.
- **a27** — Reuse the generic agent action for implementation, engineering-test preparation and diagnosis/fix, and reuse the generic comment mechanism for authorized publication through an Implement-specific result binding.
- **a28** — The activity sequence is prepare → next → implement → engineering-test preparation → recipe → checks → record → next, with failed work passing through repair and diagnosis/fix before renewed test preparation, and exhausted or blocked work passing through failure recording before next.
- **a29** — Cancellation, unrecoverable authority failures and unsafe workspace state stop dispatch globally, while story-local blockers skip that story and its dependents so safe independent work can continue.
- **a30** — Graph visit limits remain separate from per-story repair limits, and admission must reject a frozen plan whose required bounded visits exceed the pinned workflow's capacity rather than silently shortening execution.
- **a31** — A successful engineering handoff requires every story engineering-complete, while an exhausted or blocked remainder produces a partial handoff with no implicit acceptance, issue closure or automatic Validate launch.
- **a32** — Validate must execute full-suite and designated acceptance checks on the exact handed-off source, keep its reviewing agents read-only, and return failures with story attribution or an explicit unknown attribution for an owner-selected Implement repair run.
