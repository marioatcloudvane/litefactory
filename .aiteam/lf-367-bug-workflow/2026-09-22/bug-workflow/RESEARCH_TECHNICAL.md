# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Owner request: add a workflow for handling bugs, rather than fix a particular reported defect. Analysis must locate the problem when ambiguous and cover origin, trigger, preconditions, consequences, severity, solution and complexity. Repair must change code, update unit tests and rerun the suite. No concrete failing behavior or reproduction is supplied because the subject is the workflow itself.

Evidence:

- `.aiteam/lf-367-bug-workflow/2026-09-22/bug-workflow/external-evidence.md:3`

## f2

Observed at repository revision 40fa12605a4c5bb5b366a6e2806add039c7cab40: supported modes are research, plan, implement, validate and spike. Execute admits Plan and Research graph routes explicitly. There is no Bug value in the closed mode vocabulary. A new entry path must therefore be designed rather than inferred from a bug label. The ordinary Implement path also refuses unready plans, so bypassing feature planning requires a defined bug authorization contract.

Evidence:

- `internal/agent/mode.go:11` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/execute.go:631` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/execute.go:826` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`

## f3

Reusable mechanisms exist: lf_agent, lf_if, lf_loop, lf_comment and lf_ask are generic registered actions, bound to a valid admitted mode. WorkflowPolicy supplies domain-specific decode, result, artifact, publication and effect contracts; ExecuteWorkflow rejects mismatched admitted modes. Research's DOT demonstrates clarification loops and durable review waits, with document generation as a Research-specific effect. These are reuse points, not authority to reuse Research permissions for repair.

Evidence:

- `internal/graphexec/registry.go:37` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/workflow_policy.go:13` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/research_workflow.go:27` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/teamsource/research/research.dot:18` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`

## f4

The graph engine supports outcome-directed traversal, bounded cycles and durable execution records. DOT profile version 1 explicitly rejects subgraphs and ports. Composition tests exercise sequential nodes and persisted continuation using a fake future activity; they do not establish a production child-graph invocation or test-runner activity. Interpretation of 'also a graph itself' must distinguish graph-shaped test steps from nested graph execution.

Evidence:

- `internal/graphexec/doc.go:9` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/graphexec/dot.go:68` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/graph_composition_test.go:14` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/daemon/graph_dispatch.go:20` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`

## f5

Live GitHub observation on 2026-09-22: #319, 'Wire graph-based execution into Implement mode', is open and labeled Mode: Research. Its latest shaping comment calls lf_implement_prepare, lf_implement_next, lf_implement_recipe, lf_implement_checks, lf_implement_repair, lf_implement_record and lf_implement_handoff proposed additions, not delivered behavior. Proposed contracts include source-bound execution evidence, repair-budget reservation and honest complete/partial handoff. This is a dependency inventory, not a stable interface to build against yet.

Evidence:

- `.aiteam/lf-367-bug-workflow/2026-09-22/bug-workflow/external-evidence.md:10`
- `.aiteam/lf-367-bug-workflow/2026-09-22/bug-workflow/external-evidence.md:24`

## f6

Dependency context: the owner answered #319 with serial execution, continuing independent work after exhaustion and an activity-configured repair limit defaulting to two; the owner also placed acceptance criteria in Validate. The subsequent shaping proposal interprets this as scoped engineering checks in Implement and full-suite/acceptance execution in Validate, gated on a coordinated versioned Validate path. Those #319 choices do not settle #367's lifecycle boundary. #367 explicitly wants a suite rerun, creating a test-ownership issue for shaping.

Evidence:

- `.aiteam/lf-367-bug-workflow/2026-09-22/bug-workflow/external-evidence.md:18`
- `.aiteam/lf-367-bug-workflow/2026-09-22/bug-workflow/external-evidence.md:24`
- `.aiteam/lf-367-bug-workflow/2026-09-22/bug-workflow/external-evidence.md:3`

## f7

Existing lifecycle constraints distinguish engineering completion, acceptance, child closure and delivery. CONC-11 prohibits automatic mode chaining, requires concrete PR/current-head/main approval for merge and confirmed integration before parent closure. CONC-21 retains mode selection; CONC-23 grants default question permission only to Research and Plan. A lightweight end-to-end bug path must explicitly reconcile these contracts; analysis completion or passing unit tests currently provides neither acceptance nor merge authority.

Evidence:

- `.aiteam/APP_CONSTRAINTS.md:207` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `.aiteam/APP_CONSTRAINTS.md:221` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `.aiteam/APP_CONSTRAINTS.md:223` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/ask.go:12` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/feature_delivery.go:237` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`

## f8

Local test baseline: make test executes go test with the race detector across all packages. make check sequences formatting, vet, pinned lint and race tests. These are existing command targets to account for when defining suite execution for this repository; this Research pass inspected them but did not execute them. Existing graph traversal, persistence and bounded-loop tests offer implementation verification patterns.

Evidence:

- `Makefile:21` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/graph_composition_test.go:25` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/graph_composition_test.go:134` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`

## f9

Current code is ahead of an older Research document statement: docs/workflow/research-graph.md says no production Plan graph is registered, but Execute now calls admitPlanExecution, Plan captures shipped graph resources, and AdmitPlanPath selects graph for new cohorts while retaining legacy routes. Use the current Plan policy and admission code as the second concrete reuse example rather than treating that older document sentence as current behavior.

Evidence:

- `docs/workflow/research-graph.md:17` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/execute.go:631` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/turn/plan_graph.go:55` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
- `internal/store/plan_workflow.go:13` @ `40fa12605a4c5bb5b366a6e2806add039c7cab40`
