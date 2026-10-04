# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Revision inspected: 04f38894eb74e6e82955f9dbca6565439da0e4c9. Research has two stages, investigation and shaping, with explicit generated briefs and owner review. Plan has four stages: story set, story, technical and acceptance. Preserve stage behavior separately from simplifying the execution core.

Evidence:

- `internal/teamsource/plan/plan.dot:1` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f2

Queue refusal remains conditional: Research pre-question replies are visibly refused and consumed; other claim failures call leave. Bug additionally consumes ErrBugReplyNotApplied. General turn errors call leave, whose log explicitly says the comment remains unconsumed and the task queue stops. The reported retry totals were not independently verified.

Evidence:

- `internal/daemon/tick.go:2094` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f3

Owner reply routing depends on graph_owner_waits, publication journal completion and instance status. Claim requires waiting state, exact task/workflow/question identity, no running attempt and a later comment ID. It stores reply body and author, then allocation requires the origin node. This is concrete duplicated conversation state and a source of refusals.

Evidence:

- `internal/store/graph_owner_waits.go:75` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f4

graph_comment_intents stores exact authored candidate bytes, not simply a cache of the final rendered GitHub comment. It binds source attempt and publication identity, creates owner waits, and participates in report validation. Deletion must account for uncertain/lost publication recovery, not merely reply routing.

Evidence:

- `internal/store/graph_comments.go:11` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f5

The common AgentActivity discards failures before accepting retained output for Research and Plan. Bug alone tries to accept a submission despite process failure. Afterwards the common path revalidates trusted result context, authorizes output, checks artifacts, serializes and enforces 1 MiB. Therefore the issue's accepted-result-after-exit concern is confirmed for both modes, including Plan through shared registration.

Evidence:

- `internal/turn/research_agent.go:184` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f6

Submit checks agentresult.MaxBytes, which is the 4 MiB contribution ceiling. The retained graph envelope is separately limited to 1 MiB. The same accepted result can fit submission and fail later retention; the common submit boundary is the simplification point for one effective serialized-size contract.

Evidence:

- `internal/turn/agent_results.go:263` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f7

Current agent-facing communication schemas expose create or expected_revision/operations updates. Those are shared across modes, and submission has durable replay, authority and receipt handling. Per-step fields need coordinated changes in schemas, broker/service decoding, mode validation and instructions, preserving request replay.

Evidence:

- `internal/submission/tools.go:81` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f8

Result identity is task/mode/context based, not inherently a fresh object per run. Graph work context is distinguished by instance. retainedCurrent requires the latest accepted revision to belong to this run/attempt, and regression tests reject an earlier attempt's result. This is partial existing protection, not the requested simple run-local replacement protocol. A comment-only create also fails today's required outcome/payload contract rather than behaving as the proposed typed submit.

Evidence:

- `internal/turn/agent_results.go:379` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f9

Dispatch cancellation after bounded failed starts still exists, but it now records a reason and the daemon reports a durable stop. Tests cover one notice and an owner reply resuming a cancelled saved graph. This issue should reuse those fixes rather than claim dispatch exhaustion is wholly silent.

Evidence:

- `internal/daemon/graph_dispatch.go:295` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f10

Generic origin parking and retrying a factory effect are already implemented, but their admission/validation is Bug-only. Research/Plan DOT lacks owner parking declarations. Exhausted nodes can be marked failed. Extending the shared parking contract could retry a cheap stopped step without rerunning a successful agent, while preserving its retained output.

Evidence:

- `internal/graphexec/owner_stops.go:46` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f11

Research dispatch compares the shipped digest with the pinned definition and refuses changed captures. Publication recovery also refuses changed captures with owed work. Semantic topology/successor machinery already exists, so topology migration and resource-only change behavior require tracing separately; a raw DOT edit should not be assumed uniformly safe or uniformly stranded.

Evidence:

- `internal/turn/research_admission.go:142` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f12

Current admission differs from stale documentation: Research default activation is false, with explicit graph/legacy selection; new Plan sessions default to graph and persisted routes remain frozen. Supported legacy Plan readers/publication remain relevant and must not be counted as removable merely because graph Plan exists.

Evidence:

- `internal/store/plan_workflow.go:13` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f13

Scoped Go inventory at inspected revision: Research 20 production files, 3,667 code / 4,309 physical lines; 41 tests, 5,117 / 5,580. Plan 60 production files, 9,618 / 11,159; 76 tests, 15,849 / 17,514. Shared graph 34 production files, 3,603 / 4,297; 32 tests, 4,560 / 4,861. Shared result 42 production files, 6,232 / 7,259; 63 tests, 11,735 / 12,510. Shared production sum 9,835; shared tests 16,295. Graph resources additionally have 70 Research and 151 Plan physical lines.

Evidence:

- `.aiteam/lf-473-research-and-plan-guide-don-t-gate/2026-10-04/research-and-plan-guide-don-t-gate/code-inventory.csv:1` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f14

Counting method: enumerate internal/**/*.go; first bucket filenames containing research, then filenames containing plan or all internal/planbrief, then remaining internal/graphexec or graph-prefixed files, then remaining internal/agentresult, internal/submission or agent_result/mcp_/workflow_ prefixed files. Test files end _test.go. Strip // and /* */ comments with a lexical regex preserving quoted and raw strings, then count nonblank lines. Physical lines retain blanks/comments. Inventory excludes SQL, Markdown, DOT, external dependencies and generic packages not matched; includes supported legacy paths and some shared functions in mode-named files. It is a scoped baseline, not an exhaustive call-graph attribution or deletion estimate.

Evidence:

- `.aiteam/lf-473-research-and-plan-guide-don-t-gate/2026-10-04/research-and-plan-guide-don-t-gate/code-inventory.csv:1` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f15

Advisory simplification order: normalize permanent refusals into visible consumed replies; move complete result/size validation to submit and retain accepted answers independently from process outcome; replace agent-facing patches with step-specific run-local fields; extend existing stop/recovery behavior; then replace conversation routing with GitHub markers/history while preserving unconfirmed effects. Do not collapse the well-working stage content or remove owner decision semantics just to reduce line count.

Evidence:

- `internal/turn/research_agent.go:184` @ `04f38894eb74e6e82955f9dbca6565439da0e4c9`

## f16

Focused reinspection confirms comment intents retain exact authored candidate bytes and original publication identity, so removing the records without equivalent recovery can lose unconfirmed publication input.

Evidence:

- `internal/store/graph_comments.go:11` @ `01c7717883c30b160a9e926bc2285d08846b1b8a`

## f17

Focused reinspection confirms artifacts are checked after agent execution and owner-stop admission is Bug-only; applying the proposed simplification affects artifact timing and factory-stop admission, not only error-message wording.

Evidence:

- `internal/turn/research_agent.go:234` @ `01c7717883c30b160a9e926bc2285d08846b1b8a`
- `internal/graphexec/owner_stops.go:48` @ `01c7717883c30b160a9e926bc2285d08846b1b8a`

## f18

Adversarial analysis identifies potential regressions in queue/delivery coupling, replacement replay, artifact mutation, uncertain effects, GitHub history completeness and definition compatibility; these are inferred failure scenarios, not observed or reproduced incidents.

Evidence:

- `internal/store/graph_comments.go:21` @ `01c7717883c30b160a9e926bc2285d08846b1b8a`
- `internal/turn/research_agent.go:240` @ `01c7717883c30b160a9e926bc2285d08846b1b8a`
