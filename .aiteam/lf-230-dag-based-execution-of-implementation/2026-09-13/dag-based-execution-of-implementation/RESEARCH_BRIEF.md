# Research brief: file-defined execution workflows

Status: complete for the bounded Research passes through run 0004. No implementation or Plan started. The run 0004 findings supersede earlier provisional YAML notation and clarify optional owner clarification; run 0003 supersedes single-file packaging and Implement-first suggestions. Earlier findings are retained as history, not current format decisions.

## Request and answered decision

Owner request, issue #230 runs 0001–0002, 2026-09-13: execute every mode through an editable, file-defined workflow; ship one standard workflow initially. Use the existing instruction chain as a blueprint. Move deterministic sequencing from model orchestrators into litefactory; keep agents for reasoning, writing and review.

The owner explicitly confirmed in run 0002 that the first duplicated “Implement Mode” heading means **Research Mode**. Research is interactive; Implement is autonomous. The run-0001 clarification is settled and must not be asked again.

Plan should develop story descriptions/problems first, then prose acceptance intent and concrete criteria, then architecture, with owner discussion at those stages. Publish stories only after this is complete. Implement should run story-tailored checks and bounded fixes; Validate should begin with the full suite and then assess acceptance. Research is deliberately shallow: a feasible direction and important boundaries, not a final schema or engineering plan.

## Evidence boundary

All repository citations below refer to revision `0b756e3bdccf2c9c6cacf0b62e280610be050aab`, inspected in the supplied macOS worktree on 2026-09-13. Evidence is focused source, instruction and test inspection, not executed behavior. For runs 0001–0002, no builds, tests, paid agents, external services or web research were run. Run 0004 adds primary-source web research, documented below. No token-cost or latency benchmark was performed; the owner's orchestration-cost concern is a reported motivation, not a measured saving. Source-byte counts and billed tokens are different measures; neither was measured here.

The prior incomplete brief recorded revision `6e52c3e8c0953a60509e65a2a64d2b9259c7bbbb` and inspected routing/session instructions only. Its mode-label uncertainty is now resolved.

## Current blueprint and runtime owners

| Area | Current observation and evidence |
|---|---|
| Entry and routing | `AGENTS.md:5` prioritizes supplied mode and role definitions; `AGENTS.md:14` maps Research/Plan/Implement to orchestrators, Validate to a read-only reviewer, and Spike directly to its contract. This is a routing blueprint, not itself an executable graph. |
| Shared source and delivery | `teams/source/index.json:1` catalogs roles, tiers, skills and mode rosters. `internal/teamsource/snapshot.go:35` captures the mode's source/skill closure with hashes; `internal/turn/delivery.go:35` renders that local snapshot for the selected provider, with parent permissions as a ceiling at line 74. Installed `.codex/skills` are therefore not the only instruction source to evolve. |
| Research | `.codex/skills/research-orchestrator/SKILL.md:8` delegates its contract to research-evidence. `teams/source/skills/research-evidence/SKILL.md:22` requires clarification before material assumptions, saved questions and an ended run; line 49 requires maintained brief/history. This is already a compact conversational loop. |
| Plan | `teams/source/agents/shared/plan-orchestrator.md:12` currently sequences requirements/specification, architecture, then story decomposition. `.codex/skills/plan-orchestrator/SKILL.md:98` preserves selected mode and discussion context; line 114 separates discussion from publication/readiness. Stories-first therefore changes stage order, not merely presentation. |
| Implement | `teams/source/agents/shared/implement-orchestrator.md:19` assigns ready stories to specialists and sequences tests/fixes in model instructions; line 28 bounds fixes to two cycles. `.codex/skills/implement-orchestrator/SKILL.md:45` adds story-level engineering evidence; line 62 requires aggregate gates, run once after all stories at line 70. The targeted-test idea partly exists, but removing final aggregate testing is a behavior change. |
| Runner and structured handoff | `internal/turn/submission_runner.go:20` runs the configured runner with an attempt-bound submission session; line 77 defines captured snapshots/checks as **agent-reported execution**. This is useful reusable transport, but it is not a factory-executed test recipe contract. |
| Publication | `internal/turn/plan_publication.go:49` validates the batch, retains replayable effects and returns a link-only index that must be saved before readiness. `internal/turn/plan_outcome_test.go:76` covers discussion followed by completion and explicitly checks at line 109 that discussion did not publish stories. |
| Engineering versus acceptance | `internal/turn/engineering_progress_test.go:48` covers dependency release without acceptance/closure; line 133 covers restart retention. Preserve this distinction when scheduling graph nodes. |
| Validate | `internal/turn/validation.go:17` builds context from approved revisions and retained engineering evidence; line 66 checks its attribution. Line 89 explicitly excludes fresh checks from harvesting. `internal/turn/validation_test.go:103` covers retained snapshots/checks and rejection of wrong revision attribution. The proposed full-suite entry step does not exist in this examined Validate path. |
| Mode durability | `internal/daemon/mode.go:130` pins effective mode before effects; line 219 recovers confirmed mode transitions. `.aiteam/APP_CONSTRAINTS.md:207` prohibits automatic mode chaining and preserves workflow versions; line 221 requires durable selected-mode semantics. |
| Spike | `docs/workflow/spike.md:5` defines direct, bounded work and a recorded result; `teams/source/index.json:117` has an empty Spike roster. “Every mode” should include a minimal Spike wrapper, without expanding its scope. |

Existing source tests were read as evidence, not run. No claim is made that current model orchestration actually polls continuously: the source establishes model-driven sequencing, while `AGENTS.md:30` itself says not to poll.

## Recommended direction

Use a small, **durable directed workflow graph**, with a **DAG for story dependencies** inside Implement. A graph that includes test → fix → test is not mathematically acyclic. Separating those concepts provides deterministic scheduling without denying the required backward transitions.

Keep three distinct responsibilities:

1. **Workflow file:** stable node IDs, action types, input/output references, outcomes, allowed transitions and retry limits. Ship one standard definition containing the mode entry points. Historical run-0002 suggestion: YAML was a provisional human-editable candidate. Superseded by the dedicated-format comparison in run 0004; YAML is not an agreed format.
2. **litefactory runtime:** choose ready nodes, enforce transitions, execute tools/tests, record results, suspend for owner replies and recover effects. It owns the control flow.
3. **Agent roles and skills:** perform bounded reasoning/writing tasks and return structured results. Keep the existing team roster, tier resolution and permission boundaries; remove duplicated scheduling instructions as the engine takes ownership.

The graph may transition backward **within the selected mode**. It must not automatically move Research → Plan → Implement → Validate. Existing owner-controlled mode selection remains the outer boundary.

A likely owner split is a small pure graph definition/transition component, `turn` action coordination, `store` persistence, and `daemon` readiness/timers/backoff. Reuse the existing runner, submission and publication boundaries. Exact packages are a later design decision, not a recommendation to grow `turn` into an unbounded interpreter.

## Standard mode shapes

| Mode | Proposed surface workflow |
|---|---|
| Research | Clarify scope → focused investigation → options/recommendation → saved brief/report. A material gap produces an owner-question outcome, persists context and ends the run; the reply resumes clarification or investigation. Keep this short and permit a useful partial report. |
| Plan | Draft story problems/descriptions ↔ owner discussion → prose acceptance intent → concrete criteria ↔ owner discussion → architecture draft/overview ↔ owner discussion → consistency/readiness check → factory publication. Architecture can send affected stories/criteria backward; changed earlier content invalidates dependent drafts. No child story publication during intermediate discussion. |
| Implement | Select dependency-ready story → engineer → test specialist prepares/updates story checks → factory executes scoped checks → engineering evidence/dependency release. Failure goes to bounded diagnosis/fix and affected checks. Finish when authorized stories and required scoped checks/save requirements are complete. Exhausted retries produce a blocked/failed result, without a routine human approval gate or invented pass. |
| Validate | Factory executes the configured full suite against the assessed snapshot → reviewer assesses criteria plus code/architecture/plan using recorded results → factory retains assessments and applies existing acceptance effects. Failed suite outcomes remain visible and block a clean overall result; review may still record useful findings. Recommend owner return to Implement for fixes, without switching automatically. |
| Spike | Bounded task action → necessary scoped verification when applicable → saved Spike result. Keep its existing scope limit; detailed redesign is deferred. |

The additional Plan consistency step should check dependency cycles, contradictory criteria, architecture alignment, and whether evidence/test prerequisites are feasible before publishing. It need not add another mandatory owner meeting. A later iteration should define explicit stage-completion signals so the engine never guesses completion from prose. Working story drafts can be structured planning context; they must not create a competing published story source.

## Actions and test-manager handoff

Start with a small typed action set: invoke a rostered agent; validate structured output; select/join dependency-ready work; run a declared check; record snapshot/evidence; persist discussion and emit an owner question; publish the completed plan; record engineering/assessment results; report/save through existing factory operations. Prefer fixed outcomes such as completed, needs-input, failed and blocked over arbitrary code in conditions.

A test-manager responsibility can initially be a focused assignment to an existing test specialist, rather than automatically adding another permanent agent. It should return a **test plan**, not merely prose saying “tests passed.” `teams/source/skills/test-evidence/SKILL.md:18` already requires exact commands and coverage reporting; extend that into an executable typed handoff.

Candidate fields: story/criterion coverage, command executable and argument array, worktree-relative directory, scoped-versus-full-suite classification, timeout, required environment names/fixture prerequisites and evidence output references. The factory supplies authoritative identity/revision/attempt bindings and records actual exit status, timing, logs, skip reason and snapshot identity. Missing prerequisites or a command that selects no required tests must not become success. Agent-authored recipes require validation within the existing execution boundary; a workflow file or story text cannot grant new permissions or credentials.

Full-suite commands should be declared per repository; do not hardcode Go commands into the engine. Here, `AGENTS.md:58` identifies `make check` and race-enabled `make test`, with no invented integration tags. The story's affected packages and acceptance criteria should determine Implement scope, including relevant consumers when a shared boundary changes. Check writers and production-code writers need coordinated access to the shared worktree so recorded evidence matches stable bytes.

## Durability and compatibility worth designing early

Persist the pinned definition/version/digest, selected mode, current node/attempt, input references, results, retry counts, owner-question state and pending external effects. Definition edits should apply to new workflow instances by default; do not silently alter a resumed feature. Keep workflow-definition version distinct from existing managed workflow/content/submission versions.

On restart, reconcile uncertain commands and external effects before replay. Publication must continue using its durable existing path rather than duplicate issue creation. An owner pause releases the runner; no agent stays alive waiting. Keep timer/backoff ownership in the daemon and SQL in store. Begin with serial writers; enable independent story concurrency only where ownership and evidence consistency are established.

This request deliberately changes current policies. `SEC-21` at `.aiteam/APP_CONSTRAINTS.md:102`, `CONC-11` at line 207 and `EXT-4` at line 275 currently prohibit fresh Validate checks and require Implement aggregate gates. Related shared skills and installed orchestrator/reviewer instructions say the same. A future implementation must revise/version these contracts together with code and tests. Making full-suite execution a **factory action before the read-only reviewer** preserves the reviewer boundary, but still requires changing the current mode-wide prohibition. Existing active legacy/separated features must retain their selected semantics unless explicitly migrated.

## Options, recommendation and next investigation

- Keep sequencing in orchestrator prompts: smallest change, but does not meet deterministic control-flow ownership.
- Add a small built-in file interpreter over existing runtime owners: recommended; supports the requested standard workflow while reusing durable mode/publication/evidence machinery.
- Adopt a general external workflow engine or scripting language: defer; this shallow pass establishes no need for the added deployment/runtime surface and makes no vendor comparison.

Next Research iteration should narrow the action/result contract, stage completion and rewind semantics, test execution provenance, and migration/recovery behavior, ideally around one representative Implement story cycle. Measure orchestration-only calls, exclusive tokens and elapsed time against the current path before claiming savings. Provider adapters and source delivery need verification for direct factory specialist invocation; existing support for a root runner plus delivered specialists does not by itself prove that new action path.

There are no unresolved questions blocking this bounded research result. Exact file placement/syntax, stage signals, retry policy and suite-failure reporting are intentionally deferred design topics. No stories were published and no source code or workflow policies were changed.

## Run 0003: one file per mode; start with Research

### Request and scope

The owner asks whether to have one file per stage and whether Research is a good first workflow to sketch. **Yes: recommend one file per mode, with the steps/nodes inside that file.** Here “mode” means Research, Plan, Implement, Validate or Spike; “step” means clarify, investigate or synthesize within Research. This resolves the terminology explicitly without requiring another owner decision. Do not split every node into its own file initially: the complete flow should be readable in one place.

Illustrative future layout: `workflows/research.yaml`, `plan.yaml`, `implement.yaml`, `validate.yaml`, and `spike.yaml` under the same directory. These paths are proposals, not files created by this research or existing executable configuration. Together they form the one shipped standard workflow set. A shared version/manifest can later tie the set together when needed; avoid building a variant registry or include language for the first slice.

This refines the earlier suggestion of one definition containing all mode entry points. Keep mode-specific changes reviewable independently while retaining common action semantics in the runtime. Agent role/skill files still hold the reasoning instructions referenced by nodes; runtime progress, replies, outputs and attempts belong in durable instance state, never in the editable definition file. Pin the definition identity/digest for each instance, as recommended above.

### Focused current-contract evidence

New citations in this section refer to revision `6dca1442b19e846866e4c9005dc7e1bfd68a248e`, inspected on 2026-09-13 in the supplied macOS worktree. Earlier citations remain attributed to the earlier revision and were not all reverified in this continuation.

- `teams/source/agents/shared/research-orchestrator.md:10` delegates the research work to the evidence contract and at line 14 returns after saving, without starting Plan.
- `teams/source/skills/research-evidence/SKILL.md:21` defines the initial clarity check; line 28 defines grouped owner questions, incomplete state and an ended run; line 31 prohibits waiting/polling and requires reusing settled answers.
- `teams/source/skills/research-evidence/SKILL.md:13` requires session reuse; line 49 requires maintained findings, decisions and history, with bounded completion at line 51.

These observations support a small Research graph grounded in the existing behavior. They do not establish that node persistence, typed Research outcomes or an editable graph interpreter already exist. This continuation inspected those contracts and the supplied session only; no tests, builds, external research or runtime experiments were performed. Source-byte counts and billed tokens were not measured, and no performance improvement is claimed.

### Proposed Research flow

```text
clarify -> investigate -> synthesize -> save/report -> complete
                ^             |
                +-- refine ---+  (bounded)

Any reasoning step --needs_input--> save partial brief + questions -> end run
Owner reply ---------------------> resume the saved reasoning step
```

`clarify` checks the request and existing answers, returning immediately when clear; it is not a mandatory owner meeting. `investigate` gathers only the evidence needed for the bounded question. `synthesize` produces options, a recommendation and uncertainties, requesting a further focused investigation only when needed. The factory handles outcome validation, durable state and reporting; bookkeeping and question delivery do not need separate agent calls. Logical node boundaries need not imply extra paid invocations solely to move data between steps; determine call granularity in the first runtime experiment.

The following YAML is **illustrative design notation, not runnable configuration or an agreed/current schema**. It deliberately omits bindings and transport details:

```yaml
mode: research
entry: clarify
refinement_limit: 2  # candidate default; not a settled policy
nodes:
  clarify:
    action: agent
    task: clarify_scope
    on: {done: investigate}
  investigate:
    action: agent
    task: gather_focused_evidence
    on: {done: synthesize}
  synthesize:
    action: agent
    task: recommend_and_draft_brief
    on: {done: finish, refine: investigate}
  finish:
    action: save_and_report
    on: {done: complete}
common_outcomes:
  needs_input: save_questions_and_end_run
  blocked: save_incomplete_result_and_end_run
  failed: record_failure_and_end_run
resume: saved_reasoning_node_with_owner_reply
```

Each agent result should have a typed outcome and artifact references, with questions for `needs_input` and a reason/focused evidence request for `refine`. The factory validates the result and chooses only allowed transitions; it must not infer progress from free-form prose. `done` means that node's work is complete, while Research completes only after its required brief/report is safely retained. Invalid or missing output becomes a recorded failure, never an implicit success.

Keep automatic refinement bounded; the candidate two-round limit is a starting point for discussion. Exhaustion saves an incomplete result explaining the unresolved issue and ends the run. Technical retries are distinct from evidence-refinement loops. Owner replies are new authorized turns, so they must not be rejected merely because a prior automatic loop exhausted its budget.

### The smallest useful runtime slice

Research is a good first slice because it exercises agent invocation, typed branching, artifact retention, owner pause/resume and restart recovery without the test scheduling and shared-writer concerns of Implement. It proves the control-flow foundation, **not** story dependency scheduling, test execution provenance, fix-loop quality or concurrent implementation; those need a later Implement slice.

The first slice should demonstrate:

1. A clear request reaches a saved brief and report through the declared graph.
2. A material question from any reasoning step saves the partial brief, records the resume node and ends the run. The owner reply resumes with the same session and settled answers.
3. A bounded refinement revisits investigation; exhaustion or an action failure ends with honest incomplete/failure state.
4. A restart retains definition identity, node/attempt, accepted output and question/report effect identity. Retried delivery reconciles the recorded effect rather than duplicating an owner question or report; a repeated reply event is not consumed twice.
5. Completing Research ends Research. Starting Plan remains an explicit owner-controlled mode choice.

These are proposed demonstration scenarios, not executed tests or published acceptance criteria. Persist pending question/report intent before delivery, then retain delivery acknowledgement; use the existing factory report channel. No new external message was sent by this research. Exact action envelope, persistence records and reply correlation are the next focused Research topic, with one pause/reply/restart example sufficient to keep it small. No implementation or mode transition is started here.

### Decisions and history

- Settled owner correction remains unchanged: the first original heading means Research; Implement is autonomous.
- Run 0003 recommendation: one definition file per mode, several nodes within each; Research is the first vertical slice. This is a recommendation responding to the owner's question, not a claim of approval to implement.
- No blocking clarification is needed for this sketch. Schema, storage layout, retry count and call granularity remain explicitly provisional.
- 2026-09-13 (run 0003): Preserved prior findings, refined file granularity, and sketched Research branching, bounded refinement, owner pause/resume and recovery. Research complete for this bounded request; no implementation changes, tests or publication.


## Run 0004: dedicated workflow formats and optional clarification

### Accepted direction and scope

The owner accepts one definition file per mode and Research as the first slice. The owner explicitly requires clarification to be skipped when the reasoning agent finds the instructions clear, and asks for a comparison of established graph/workflow formats. The earlier YAML was unapproved illustrative notation; it is superseded, not a schema to implement. There is no material ambiguity blocking this bounded comparison. Visual editing has not been requested, and is treated as a possible future preference rather than an invented requirement.

Clarification is a conditional branch, not an unconditional meeting or a separately required agent invocation. The first reasoning action can assess clarity while beginning useful work. Its structured result distinguishes `scope.clear` from `scope.needs_input`. Clear scope goes directly to investigation. Material gaps save context and questions, end the run, and resume on the owner's reply. Later reasoning steps retain the same ability to request missing input. Litefactory validates and routes these outcomes; it does not attempt to judge clarity from prose.

```text
initial reasoning --scope.clear-------> investigate -> synthesize -> save/report -> complete
                 --scope.needs_input-> save questions -> await owner
                                          owner reply -> resume saved reasoning
investigate/synthesize --needs_input--> same saved pause/resume path
synthesize --refine, within limit-----> investigate
```

This is a conceptual graph, not executable configuration. Automatic refinement remains bounded and runtime retry policy remains distinct from reasoning loops.

### What the format must specify

A serialization syntax alone does not define how a workflow runs. The important comparison is between a graph description language and a workflow/state-machine language with established transition semantics. YAML can serialize an established workflow language; replacing it with XML would not by itself remove a custom execution model. The earlier suggestion was premature because it invented workflow semantics before comparing established options.

Assess against these needs: directed cycles; event/outcome-driven branches; asynchronous agent work; owner pause/reply; editable, versioned mode files; future parallel work; and a small runtime embedded in litefactory. Definition portability, action implementation, durable execution and visual editing are separate capabilities. None of the formats alone supplies litefactory's database recovery, issue-reply correlation, idempotent reporting, or existing permission boundaries.

### Candidate comparison

The external sources below were inspected on 2026-09-13. Versions are identified where available; this is not a claim that an inspected version is the newest release. Suitability and relative complexity are research judgments, not benchmark results.

| Candidate | Established capabilities | Fit and cost for this project |
|---|---|---|
| **SCXML 1.0, `.scxml`** | XML statecharts with event transitions, hierarchy, history, parallel regions and external invocation. [W3C Recommendation, 1 September 2015](https://www.w3.org/TR/2015/REC-scxml-20150901/) | Leading candidate for the small event-driven Research runtime. Agent outcomes map to events and owner waiting to a state. Action bindings and durable persistence remain litefactory responsibilities. |
| **BPMN 2.0.2 XML, `.bpmn`** | Process execution semantics, service/user/receive tasks, exclusive and parallel gateways, loop activities, XML interchange and diagram interchange. [OMG specification, December 2013 document](https://www.omg.org/spec/BPMN/2.0.2/PDF/) | Strong alternative, especially if graphical workflow authoring becomes central. More process concepts than the first Research slice needs; an embedded runtime still needs a clearly bounded supported element set and task bindings. |
| **Graphviz DOT, `.dot`** | Concise directed edges, nodes, subgraphs and attributes in a graph language. [Official DOT grammar, page modified 28 September 2024](https://graphviz.org/doc/info/lang.html) | Excellent for generated diagrams and readable topology. Agent actions, event routing, waits, retries and joins would be a custom litefactory language layered on attributes. Does not eliminate the central semantics-design work. |
| **GraphML 1.0, `.graphml`** | XML graph interchange, directed edges, graph/node/edge data and extensibility. [Official primer](https://graphml.graphdrawing.org/primer/graphml-primer.html), [normative schema reference](https://graphml.graphdrawing.org/specification/xsd.html) | Useful if interchange with general graph tools matters. More verbose than DOT, while workflow execution meanings would still need custom definitions. Low priority for canonical execution here. |
| **Serverless Workflow DSL v1.0.0** | Task-oriented workflow language with calls, event listening, forks, branching, retries and named-task continuation. [Pinned official DSL reference](https://github.com/open-workflow-specification/specification/blob/v1.0.0/dsl-reference.md) | A real workflow-language alternative despite YAML-shaped examples. Its event/integration/task vocabulary may suit future actions, but adds surface beyond the small Research flow. Secondary candidate, not the recommended first format. The historical repository URL redirects to Open Workflow Specification; this comparison pins v1.0.0 rather than mixing current and older DSLs. |

BPMN has a concrete browser editor/viewer option in [bpmn-js](https://bpmn.io/toolkit/bpmn-js/) (toolkit page reports v18.16.0 on inspection). That is evidence of authoring tooling, not evidence of a compatible embedded Go execution engine. A BPMN task can represent owner work, or a receive task can represent the reply; litefactory must still implement the issue interaction and correlation. XML and standard diagram interchange make it a credible alternative rather than merely a presentation format.

For GraphML, the official primer explicitly identifies itself as non-normative and points to schema definitions. The normative reference appeared in search, but direct retrieval returned an internal error; topology/data findings are supported by the inspected primer. No unsupported conclusion about tool availability follows from that retrieval failure.

### Recommendation and boundaries

**Prefer SCXML for the next Research sketch, with BPMN as the alternative if graphical authoring is a primary goal.** This recommendation assumes text-reviewed files and an embedded litefactory runtime remain the near-term priorities. It is a recommendation to investigate, not owner approval of a format or permission to implement.

Use one mode definition such as `workflows/research.scxml`; keep runtime state outside it. Define a supported SCXML profile explicitly and reject unsupported constructs; do not claim full SCXML conformance. Long-running agent jobs should use asynchronous invocation/events: custom executable content must finish swiftly. SCXML history is state-machine history, not database crash recovery. Parallel regions do not create worker threads or solve story-dependency scheduling. JavaScript is not mandatory; data-model choices are explicit. [SCXML §§3, 4.10, 5.1, 6.4](https://www.w3.org/TR/2015/REC-scxml-20150901/)

Retain the earlier runtime responsibilities: pin definition identity for each instance, record action attempts/results, validate typed agent output, persist owner-question state before reporting, correlate replies, and reconcile uncertain effects after restart. Keep mode selection human-controlled. Implement's dynamic dependency DAG and concurrent worktree ownership require a later design even if the outer mode is a statechart.

Do not maintain a separately edited DOT/GraphML graph alongside the canonical workflow. If diagrams are useful, generate a view from the selected definition so two sources cannot disagree. Choosing BPMN later would change this authoring tradeoff: its standard explicitly includes model and diagram interchange.

### Next bounded investigation and evidence limits

The next useful Research artifact is the same small Research flow expressed in SCXML, with an explicit list of supported constructs and one typed agent-result/reply example. Compare a compact BPMN representation only if that helps assess authoring ergonomics. The example should demonstrate both a clear request bypassing clarification and a question/reply that survives restart. Do not select an engine merely because it parses the file.

Unverified topics remain: suitable maintained Go interpreter or parser, compliance of any candidate library, action invocation bindings, restart correctness, rejection of unsupported elements, and the migration cost from existing contracts. No Go package, external engine, dependencies or executable workflow was installed or created. No source implementation or policy changed, no tests/builds ran, and no performance or token-saving claim was measured. This run read the supplied session and research contract and researched official external sources; earlier repository observations were preserved without re-auditing source. Source bytes and billed tokens were not measured.

No owner question is required to complete this comparison. Choosing the final format remains a future decision; this run supplies a reasoned shortlist rather than silently accepting the provisional YAML.

- 2026-09-13 (run 0004): Accepted optional clarification and Research-first/mode-file direction; compared dedicated statechart, process and graph formats from primary sources; recommended SCXML for the embedded text-reviewed first slice, with BPMN for stronger visual-authoring requirements. Research complete for this bounded pass. No implementation, tests, publication or mode transition.
