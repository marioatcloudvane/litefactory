# Research brief

## Request

Reassess graph Implement using current Bug experience, maximize agent engineering authority and reduce token overhead and elapsed time without an orchestration agent.

## Decisions

- **d1** — This shaping pass defines the proposed scope for owner review without implementing changes, creating child issues or entering Plan.
- **d2** — The owner's current direction requires DOT-controlled workflow execution and replaces agent orchestration with factory scheduling.
- **d3** — Users continue to select modes, and neither graph completion nor Validate failure automatically starts another mode.
- **d4** — Implementation consumes approved frozen stories and preserves existing permission, result, publication and save boundaries.
- **d5** — Owner answer to the concurrency question: parallel execution is essential and is in this release, as wide as can be done safely, with a configurable cap.
- **d6** — Owner correction to q2: acceptance testing and acceptance assessment belong to Validate, while Implement retains scoped engineering checks.
- **d7** — Owner answer to q3: the repair-cycle limit is activity configuration with a default of two.
- **d8** — Owner answer to q4: after story exhaustion, continue independent ready stories and block the failed story's dependents.
- **d9** — Sorting rule: the factory owns whether and when (order, scope, limits, commits, merging, visible refusals); agents own how and why (design, code, tests, checks, diagnosis, blocked reports). Unclear outcomes park as owner questions rather than failing the run, and an agent's blocked report is never overridden by a rule.
- **d10** — Implement is strictly implementation: code, its own unit tests, race tests on affected packages, build, format, vet and a build after each merge. Acceptance checks and tests, real-boundary integration tests, the full suite once Validate can run it, code and architecture review and plan-conformance move to Validate.
- **d11** — Parallel safety rule: independence is the dependency list only; no overlap prediction. Isolation uses a separate git working copy per running story, merging is serial in plan order, and a story's dependents start only after it is merged.
- **d12** — The owner's six rules govern graph Implement: git is the record with always-commit; validation only at submit; incomplete and blocked are answers that park as batched owner questions; no feature-wide gates; checks are the agent's job; every refusal is visible.
- **d13** — One worker agent per story writes code and unit tests, runs the repo's own commands, fixes failures and submits; there is no separate test-runner role and no factory verification of command execution.
- **d14** — Each story works on its own story branch, always committed and pushed; only stories reported done are merged into the feature branch in plan order, and a story is done when its merge marker is in branch history.


## Assumptions

- **a1** — A prerequisite releases dependents once its story branch is merged with a done marker after the agent reported done; the agent's own checks are the engineering evidence, and acceptance and full-suite verification are not waited for.
- **a2** — Proposed surface: the editable DOT workflow controls direct story workers and explicit runner/verifier stages over frozen dependency records, without general nested-DOT syntax.
- **a3** — Current Plan graph and activity registration are observed, but compatibility still requires an explicit supported frozen-plan version.
- **a4** — Previous owner decisions remain effective; parallelism is now settled as in scope, with serial dispatch used only while Pass 1 is proven.
- **a5** — After owner-selected Implement, ready stories go directly to one worker each, which implements, writes unit tests, runs the repo's checks, diagnoses failures and submits an outcome.
- **a6** — Retain nothing beyond git and the run's result: story branches, merge markers, and the submitted outcomes, summaries and optional evidence; no snapshots, bytes, digests or checkpoint sequences.
- **a7** — Proposed readiness validation: reject empty or incompatible plans, duplicate identities, missing or self dependencies, cycles and broken parent relationships before dispatch; order ready stories by their frozen plan order with stable story identity as a tie-breaker.
- **a8** — One implementation worker per story owns code, unit test authoring, running checks and diagnosis; no separate test runner.
- **a9** — Evidence is optional free-form text naming commands run and their results; the factory validates only its shape at submit.
- **a10** — Workers choose and run the repo's own check commands; the factory neither validates recipes nor proves semantic test adequacy.
- **a11** — The graph path does not verify command execution from transcripts or hashes; whether a later optional transcript pass exists is decided by q9.
- **a12** — No invalidation machinery in the graph path: a reopened prerequisite arises only from an owner-requested rerun that produces new commits, and readiness is re-derived from git markers.
- **a13** — Repairs remain within frozen scope and permissions; missing infrastructure, denied permissions, invalid authority or required scope changes become explicit blockers rather than automatic administration or replanning.
- **a14** — Recovery reads git as truth: story branches and merge markers survive restarts, an interrupted story is parked as incomplete with its work committed, and completion is derived from markers so it cannot be counted twice.
- **a15** — The final handoff is rendered from git and the submitted results: story states, parked stories with reasons first, save status and the checks still owed to Validate; no acceptance or closure claims.
- **a16** — Existing frozen plans need an explicit supported-version mapping, and reduced Implement checks remain disabled until a versioned Validate entry executes full-suite and acceptance checks against the handed-off snapshot.
- **a17** — Exclude shared-workspace writers, general nested-DOT syntax, automatic mode changes or closure, acceptance in Implement, legacy retirement, provisioning, team recipe configuration, Bug migration and unrelated platform expansion.
- **a18** — Proposed success criteria: deterministic readiness, dependency release only after valid tailored evidence, bounded recovery across crashes, honest cancellation/exhaustion, preserved scope and permissions, and no reduced-check activation without a working full-suite Validate entry must be demonstrated before rollout.
- **a19** — Interpret the owner's acceptance boundary as execution and assessment ownership: frozen acceptance requirements still guide implementation, but only Validate executes designated acceptance checks and records acceptance verdicts.
- **a20** — The dependency DAG is derived from approved story identities and dependency lists, and a DOT rendering is a view of those records rather than a second editable source of execution authority.
- **a21** — Readiness selects eligible stories in frozen order under the configured concurrency cap and workspace/resource leases, with optional concurrency contingent on q5.
- **a22** — The per-story repair limit defaults to two and is expressed through the workflow's existing per-node visit limit on the fix loop of each story's child workflow; zero means no automatic repair; whether it can be configured as wanted is for Plan to confirm.
- **a23** — The limit is pinned with the workflow definition for the run; exhausting it parks the story as an owner question and never fails the run.
- **a24** — Only a new explicit owner retry authorization or a new approved story revision may create a fresh budget epoch, with prior attempts retained and cancellation or infrastructure failure never causing an automatic reset.
- **a25** — Preserve failed story deltas and isolate or restore their workspace before continuing, without discarding unrelated work.
- **a26** — Required factory capabilities are next-ready selection with admission checks, serial merging with a build (Pass 2 only), and the handoff, plus always-commit-and-push after each story run.
- **a27** — Reuse generic agent invocations for implementation and final test running and existing authorized publication mechanisms.
- **a28** — Proposed lifecycle per run: next ready, story worker child workflow, always commit and push, merge if done and build, repeat; park incomplete or blocked stories; final handoff with one batched question.
- **a29** — Cancellation, unrecoverable authority failures and unsafe workspace state stop dispatch globally, while story-local blockers skip that story and its dependents so safe independent work can continue.
- **a30** — Graph visit limits remain separate from per-story repair limits, and admission must reject a frozen plan whose required bounded visits exceed the pinned workflow's capacity rather than silently shortening execution.
- **a31** — A successful engineering handoff requires every story engineering-complete, while an exhausted or blocked remainder produces a partial handoff with no implicit acceptance, issue closure or automatic Validate launch.
- **a32** — Owner-selected Validate executes full-suite and acceptance checks using agents against the handoff source, retains read-only acceptance assessment and returns attributed or explicitly unattributed repair findings.
- **a33** — Parallel workers use separate workspaces, serialized merging in plan order, a build of the merged code after each merge and a story-test recheck when the merge was not a straight fast-forward, before dependency release.
- **a34** — The factory does not predict file overlap or shared resources; clashes surface as merge conflicts or failed checks and go through repair, and a plan can force order with a dependency.
- **a35** — Context is story-local with original evidence accessible on demand, compact handoffs and safe same-story repair continuation, without promising measured savings.
- **a36** — Harness command-proof requirements apply only if the owner keeps transcript verification as a later optional pass (q9); the graph path does not depend on them.
- **a37** — Compare equivalent frozen workloads using elapsed time, invocation count, available token/cache usage and repeated checks, and report missing usage explicitly.
- **a38** — General delegation is not part of default story execution; specialist help is exceptional and remains within pinned authority and budget.
- **a39** — Treat the owner's phrase test manager as the existing test-engineer roles and test-evidence guidance, since no component of that name exists.
- **a40** — Superseded: no snapshots are stored at all in the graph path, so the factory-versus-agent capture question is moot.
- **a41** — The factory builds three activities: next ready (with admission checks), merge with build (Pass 2 only), and handoff; commit and push, agent runs and publication are reused.
- **a42** — The three passes are: serial git-record workflow, parallel workers, then Validate taking the end-of-run work; this reading is flagged to the owner for correction.
- **a43** — Each story runs as a child workflow reusing the pattern Bug already uses for its test child, so the workflow engine needs no fan-out feature; the parent selects the ready set up to the cap, runs the children together and merges in order.
- **a44** — The shipped Implement path stays available until the new path is proven, as a failure limiter, and the aggregate run is not dropped from Implement until the versioned Validate entry exists.
- **a45** — A result that fails validation at submit returns diagnostics the agent fixes in the same run and costs no repair; a run that ends without any accepted result is recorded as incomplete with its work committed; a default parallel cap of four is unmeasured and adjustable.
- **a46** — Run scope is every story not yet done at run start, or the stories the triggering comment names; parked questions are batched into one report when no further work can start.
- **a47** — Until Validate can run the full suite against the branch, the full suite without race runs once at the end of the run; per story, the worker runs race tests on affected packages.
- **a48** — The graph path never reads or writes the old bookkeeping and nothing new is built on it; both paths coexist until the dedicated retirement.


## Open questions

### q7 — Do you approve the three passes: serial git-record workflow, then parallel workers, then Validate takes the end-of-run work?

_Asked of: Product Owner Questions_

- **phased** — Recommended: Pass 1 serial git-record workflow (removes every audited failure group), Pass 2 parallel workers with ordered merging, Pass 3 move the final suite to Validate once the branch-diff review exists.  *(recommended)*
- **all_at_once** — Deliver serial, parallel and the Validate move as one release; fewer handovers, but the largest and riskiest single change, and it blocks the audit fixes behind the merge work.

### q8 — What should the default parallel worker cap be?

_Asked of: Product Owner Questions_

- **four** — Recommended: four at once, adjustable. Real speedup on typical plans without overloading the machine or hitting session limits quickly.  *(recommended)*
- **two** — Two at once, adjustable. Safer for the first runs and easier to diagnose, but slower.
- **ready_set** — Every ready story at once with no default cap. Fastest on paper, but unbounded load and token bursts.

### q9 — Should the factory ever verify agent test runs from the harness transcript?

_Asked of: Technical Questions_

- **drop_for_now** — Recommended: no. Checks are the agent's job and evidence is optional, per your rule 5. Revisit only if false-done shows up in practice. Least new code.  *(recommended)*
- **later_optional** — Keep it as a later, separate pass that reads transcripts and flags (not fails) runs whose reported checks it cannot find. More code, needs the unconfirmed Claude exit-code answer.

### q10 — In what order do graph Implement and the Validate branch-diff change (#321) ship?

_Asked of: Technical Questions_

- **validate_first** — Recommended: land the branch-diff review for Validate first, so graph Implement's output (git only) is readable from day one. Safest coupling.  *(recommended)*
- **implement_first** — Ship graph Implement first as opt-in and have it keep writing the minimal story-done flag current Validate reads until the change lands; faster, but leaves a compatibility shim to remove later.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
