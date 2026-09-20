# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Observed at revision 86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c: Research already composes investigation and shaping roles from Markdown in one DOT graph. Controls and comments use lf_source to read each stage's own agent output. The workflow executes at most one agent stage per run and parks subsequent work in a durable dispatch. Thus adding another role within Research already reuses handlers; adding another mode does not yet reuse the complete lifecycle.

Evidence:

- `internal/teamsource/research/research.dot:15` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_workflow.go:69` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_shaping_graph_test.go:33` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f2

The graph foundation is substantially reusable: registry specs declare mode, outcomes and attribute vocabularies; validation checks reachability, deterministic outcome edges and bounded cycles; node execution reserves attempts and retains transitions/output. However action names must currently use lf_<mode>_<capability>, control validation is called specifically for Research, and failed-activity terminalization is Research-specific. Sharing implementation does not inherently require changing action spelling; naming remains a separate compatibility choice.

Evidence:

- `internal/graphexec/registry.go:19` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/graphexec/registry.go:33` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/graphexec/validate.go:101` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/graph_node.go:133` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f3

Loop and if already share ResearchControlActivity. It verifies the pinned definition and attempt identity, reads the latest selected source attempt, revalidates retained output, and compares response. Initial defaults are unavailable after an attempt. Response vocabulary, decoding and path validation are hard-coded to Research. The path validator enforces question/review waits, prevents direct agent re-entry and constrains failure publications; these are runtime policy obligations, not merely prompt wording.

Evidence:

- `internal/turn/research_control.go:23` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_control.go:51` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/graphexec/research_control.go:18` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/graphexec/research_control.go:62` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/graphexec/research_control.go:89` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f4

The agent handler combines reusable run/attempt/provider binding, runner dispatch, one cold fallback after refused resume, helper shutdown, private output retention and commit narrative with Research-only result decoding and brief rendering. Request validation permits only the Research graph invocation kind. Delivery selects Markdown roles, skills, model tiers and bounded resource closures, intersects role tools with the factory ceiling, and computes instruction identities, but its entry points and bootstrap are Research-specific. Generalization must reach delivery and request/result authority, not just Execute methods.

Evidence:

- `internal/turn/research_agent.go:71` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_agent.go:160` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/agent/request.go:143` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_delivery.go:33` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/teamsource/research_delivery.go:15` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f5

The package loader embeds only research, opens research.dot, requires builtin/research metadata and recognizes lf_research_agent nodes. It already supports node-over-role-over-graph task/tier inheritance, selected skills, resource validation and content hashing. Launch admission is Research-only and default-off; persistence also has Research-specific execution-path and launch-capture tables and queries. Owner reply admission rejects modes other than Research. A generic package loader/catalog and mode-aware lifecycle are therefore necessary beyond shared activity bodies.

Evidence:

- `internal/teamsource/research.go:22` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/teamsource/research.go:106` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/teamsource/research.go:164` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_admission.go:20` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_admission.go:43` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/store/research_admission.go:13` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/store/research_admission.go:51` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/store/graph_owner_waits.go:60` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f6

Comment publication already separates retained candidate identity from delivery and recovery. Its current checks and renderer require Research results; delivery purpose includes a research-comment prefix. Comment kinds and waiting behavior are also fixed in the store, and SQL constrains kinds to question/review/final/failure. Consequently configurable per-mode kinds cannot simply bypass persistence contracts. Existing effect identities must survive refactoring so lost acknowledgments recover the original operation.

Evidence:

- `internal/turn/research_comments.go:121` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_comments.go:174` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_comments.go:188` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/store/graph_comments.go:118` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/store/migrations/0055_graph_comment_review_kind.sql:29` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f7

Plan is an existing domain workflow, not just another response enum. Shared application results already support Plan stories and planning context, with complete typed validation on completion. Whole-turn finalization adapts these results to the existing Plan checkpoint owner. Managed publication validates parent/generation and the complete story batch, retains replayable effects and returns a link-only index that must be saved before readiness. Research private-node finalization explicitly avoids that whole-turn projection. A Plan graph must deliberately connect its final authorized result to these owners while keeping intermediate stage results private.

Evidence:

- `internal/agentresult/types.go:166` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/result_finalization.go:294` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_agent.go:181` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/plan_handoff.go:113` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/plan_publication.go:49` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f8

The private Research envelope couples ask to incomplete results with questions and done/approved to completed results without questions. The approved branch checks result shape; it does not itself verify an owner approval record. Owner-reply admission separately authenticates and correlates a reply. Inference for shaping: a shared contract must distinguish stage completion, whole-mode completion and authorization for effects. Reusing this decoder unchanged would neither admit Plan payloads nor settle those Plan authority boundaries.

Evidence:

- `internal/agentresult/research_node.go:57` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/agentresult/research_node.go:69` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/store/graph_owner_waits.go:60` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/result_finalization.go:294` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f9

Assessment, not an implemented design: the evidence supports three reusable boundaries—workflow package loading and node delivery; graph lifecycle and standard activities; typed mode-result validation and domain completion adapters. Markdown and DOT can describe roles, resources, stages, sources, branches and limits. Factory code should retain permissions, identity checks, effect recovery and existing Plan publication authority. A smaller handler-only refactor leaves Plan-specific copies in loading/admission/continuation. Moving all semantics into unrestricted DOT would exceed today's closed vocabulary and authority model.

Evidence:

- `internal/teamsource/research.go:106` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_workflow.go:23` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/graphexec/registry.go:19` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/plan_publication.go:49` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f10

Compatibility requires explicit treatment: semantic digests include actions and execution attributes; launch admission retains captures and creates linked successors when topology changes, while parked dispatch resume rejects changed shipped definitions. Research remains opt-in with live-host activation evidence documented as outstanding. No claim of zero digest changes, migration-free adoption or authentic host compatibility is established by this pass.

Evidence:

- `internal/graphexec/semantic.go:12` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/store/research_admission.go:116` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_admission.go:150` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `docs/workflow/research-graph.md:3` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f11

Validation performed on darwin/arm64 with go1.27.0 at revision 86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c: go test ./internal/graphexec ./internal/teamsource ./internal/agentresult ./internal/turn -run 'Test(ResearchCondition|ResearchControl|ValidateResearchControlGraph|CaptureResearch|ResearchAgentRealStorePrivateRetention|ShapingGraph)' -count=1 exited successfully. Matching tests passed in graphexec, teamsource and turn; agentresult reported no matching tests, so no decoder test coverage is claimed from this command. This was a bounded source assessment with focused offline checks, not full make check or a live provider probe. Git status was clean; no implementation files were changed.

Evidence:

- `internal/graphexec/research_control_test.go:21` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/teamsource/research_test.go:46` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_boundaries_test.go:116` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
- `internal/turn/research_shaping_graph_test.go:33` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
