# Research brief

## Request

Reassess graph Implement using current Bug experience, maximize agent engineering authority and reduce token overhead and elapsed time without an orchestration agent.

## Decisions

- **d1** — This shaping pass defines the proposed scope for owner review without implementing changes, creating child issues or entering Plan.
- **d2** — The owner's current direction requires DOT-controlled workflow execution and replaces agent orchestration with factory scheduling.
- **d3** — Users continue to select modes, and neither graph completion nor Validate failure automatically starts another mode.
- **d4** — Implementation consumes approved frozen stories and preserves existing permission, result, publication and save boundaries.
- **d5** — The owner previously selected serial first; the current request reopens optional concurrency, so serial remains the settled default pending q5.
- **d6** — Owner correction to q2: acceptance testing and acceptance assessment belong to Validate, while Implement retains scoped engineering checks.
- **d7** — Owner answer to q3: the repair-cycle limit is activity configuration with a default of two.
- **d8** — Owner answer to q4: after story exhaustion, continue independent ready stories and block the failed story's dependents.


## Assumptions

- **a1** — A prerequisite releases dependents after implementation and required scoped engineering checks pass with retained evidence, without waiting for acceptance or full-suite verification.
- **a2** — Proposed surface: the editable DOT workflow controls direct story workers and explicit runner/verifier stages over frozen dependency records, without general nested-DOT syntax.
- **a3** — Current Plan graph and activity registration are observed, but compatibility still requires an explicit supported frozen-plan version.
- **a4** — Previous owner decisions remain effective; optional first-release parallelism is reopened by the current request and awaits q5.
- **a5** — After owner-selected Implement, ready stories go directly to workers that implement, author engineering tests and diagnose failures, with agent-run checks and factory-verified evidence.
- **a6** — Retain frozen plan/story revisions and edges, role/workflow hashes, workspace/base-source identities, leases, attempts, budgets, recipe digests, transcript evidence, integrated source and final states.
- **a7** — Proposed readiness validation: reject empty or incompatible plans, duplicate identities, missing or self dependencies, cycles and broken parent relationships before dispatch; order ready stories by their frozen plan order with stable story identity as a tie-breaker.
- **a8** — One implementation worker owns code, scoped engineering test authoring and diagnosis, and a separate test runner executes the final pinned recipe.
- **a9** — A versioned engineering recipe records its digest, story revision, engineering purpose and scope, executable and arguments, repository-relative working directory, prerequisites, environment references, finite timeout and expected exit/result criteria.
- **a10** — Workers propose engineering recipes and runners execute them; factory validation enforces recipe shape, binding and permissions without claiming to prove semantic test adequacy.
- **a11** — Evidence comes from harness-owned command records bound to recipe, invocation and source, including actual command exit status and source stability; missing or ambiguous evidence blocks completion.
- **a12** — Source changes invalidate the changed story's required check evidence, and reopening a prerequisite invalidates its transitive dependents' engineering completion while preserving historical evidence.
- **a13** — Repairs remain within frozen scope and permissions; missing infrastructure, denied permissions, invalid authority or required scope changes become explicit blockers rather than automatic administration or replanning.
- **a14** — Proposed recovery rule: persist state before dispatch, recover known attempt receipts before replay, never double-count completion, stop active work on cancellation and preserve retry budgets across restarts and resumes.
- **a15** — Proposed completion interface: publish story engineering state and dependency release separately from aggregate verification, save status, acceptance and closure, with a final handoff containing snapshot, evidence, unresolved work and required Validate checks.
- **a16** — Existing frozen plans need an explicit supported-version mapping, and reduced Implement checks remain disabled until a versioned Validate entry executes full-suite and acceptance checks against the handed-off snapshot.
- **a17** — Exclude shared-workspace writers, general nested-DOT syntax, automatic mode changes or closure, acceptance in Implement, legacy retirement, provisioning, team recipe configuration, Bug migration and unrelated platform expansion.
- **a18** — Proposed success criteria: deterministic readiness, dependency release only after valid tailored evidence, bounded recovery across crashes, honest cancellation/exhaustion, preserved scope and permissions, and no reduced-check activation without a working full-suite Validate entry must be demonstrated before rollout.
- **a19** — Interpret the owner's acceptance boundary as execution and assessment ownership: frozen acceptance requirements still guide implementation, but only Validate executes designated acceptance checks and records acceptance verdicts.
- **a20** — The dependency DAG is derived from approved story identities and dependency lists, and a DOT rendering is a view of those records rather than a second editable source of execution authority.
- **a21** — Readiness selects eligible stories in frozen order under the configured concurrency cap and workspace/resource leases, with optional concurrency contingent on q5.
- **a22** — Configure max_repairs on the repair-budget activity as a finite nonnegative integer defaulting to two, where zero disables automatic repair and the initial attempt is additional to the configured repair cycles.
- **a23** — Pin the repair limit at execution admission, reserve each repair cycle durably before specialist dispatch, and retain counts across crashes, resumes, recipe edits and workflow-file edits.
- **a24** — Only a new explicit owner retry authorization or a new approved story revision may create a fresh budget epoch, with prior attempts retained and cancellation or infrastructure failure never causing an automatic reset.
- **a25** — Preserve failed story deltas and isolate or restore their workspace before continuing, without discarding unrelated work.
- **a26** — Required capabilities are admission/readiness and leases, durable attempt limits, generic evidence verification, serialized integration and evidence-bound story completion/handoff.
- **a27** — Reuse generic agent invocations for implementation and final test running and existing authorized publication mechanisms.
- **a28** — Proposed lifecycle is admission/readiness → worker → source and recipe pin → runner → verifier → integration and necessary recheck → engineering record, with reserved repairs returning to the worker.
- **a29** — Cancellation, unrecoverable authority failures and unsafe workspace state stop dispatch globally, while story-local blockers skip that story and its dependents so safe independent work can continue.
- **a30** — Graph visit limits remain separate from per-story repair limits, and admission must reject a frozen plan whose required bounded visits exceed the pinned workflow's capacity rather than silently shortening execution.
- **a31** — A successful engineering handoff requires every story engineering-complete, while an exhausted or blocked remainder produces a partial handoff with no implicit acceptance, issue closure or automatic Validate launch.
- **a32** — Owner-selected Validate executes full-suite and acceptance checks using agents against the handoff source, retains read-only acceptance assessment and returns attributed or explicitly unattributed repair findings.
- **a33** — Recommend optional concurrency with default one worker, separate workspaces, stable serialized integration and post-integration scoped verification before dependency release; q5 decides inclusion.
- **a34** — Known shared files or exclusive resources serialize work, missing independence information defaults to serial, and disjoint paths alone do not prove independence.
- **a35** — Context is story-local with original evidence accessible on demand, compact handoffs and safe same-story repair continuation, without promising measured savings.
- **a36** — Harness compatibility requires reliable individual command identity, order, directory, source and exit evidence; absent evidence blocks the new check path instead of trusting summaries.
- **a37** — Compare equivalent frozen workloads using elapsed time, invocation count, available token/cache usage and repeated checks, and report missing usage explicitly.
- **a38** — General delegation is not part of default story execution; specialist help is exceptional and remains within pinned authority and budget.
- **a39** — Treat the owner's phrase test manager as the existing test-engineer roles and test-evidence guidance, since no component of that name exists.
- **a40** — Factory-captured snapshots (q6) are recommended; until answered, snapshot transport stays as shipped.


## Open questions

### q5 — Should this release include optional isolated parallel story workers?

_Asked of: Product Owner Questions_

- **optional_concurrency** — Recommended: include concurrent workers in separate workspaces with serialized integration and rechecks; default one worker, higher limit by configuration.  *(recommended)*
- **direct_serial** — Keep this release serial; deliver direct dispatch, factory-verified checks and smaller inputs first, and defer parallelism.

### q6 — Should the factory capture story and aggregate snapshots instead of the agent?

_Asked of: Technical Questions_

- **factory_capture** — Recommended: the factory reads changed files from the worktree and binds them to the story; the agent only names the story. Cuts tokens and removes hand-built encoding errors.  *(recommended)*
- **agent_capture** — Keep agent-built snapshots and only trim the rest; smaller change, but the largest token cost remains.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
