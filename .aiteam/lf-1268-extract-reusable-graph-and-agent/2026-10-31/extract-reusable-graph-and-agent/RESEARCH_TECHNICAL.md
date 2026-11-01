# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

Current graphexec is already side-effect-free parsing/validation/navigation, but Profile and ActionSpec use agent.Mode/Workflow; Register enforces lf workflow action names; Validate enforces bug policy, mode membership and rights tags. Decide itself only selects an unambiguous declared edge. Generic mechanics can be reused only after separating factory validation.

Evidence:

- `internal/graphexec/doc.go:1`
- `internal/graphexec/graphexec.go:1`
- `internal/graphexec/registry.go:1`
- `internal/graphexec/validate.go:1`
- `internal/graphexec/decide.go:1`

## F-02

agent.Runner is a vendor-neutral interface, but Request includes config/naming dependencies, factory graph/run identities, plan transport and retained Delivery validation. Package documentation claims only naming and references a purity test absent from the inspected file inventory; current imports are stronger evidence than that description.

Evidence:

- `internal/agent/kind.go:1`
- `internal/agent/request.go:1`
- `internal/agent/doc.go:1`

## F-03

Both existing providers combine native execution with factory config, naming and instructionpackage capture. providerprocess has reusable start/kill helpers, but also agent-dependent protocols. Auxiliary classification, conversation, doctor, judge and advisory methods expand the surface beyond Runner.Run. Moving full directories would preserve factory coupling.

Evidence:

- `internal/harness/runner.go:1`
- `internal/codex/runner.go:1`
- `internal/providerprocess/start.go:1`
- `internal/providerprocess/conversation.go:1`

## F-04

Impact: touched graph/agent/harness/codex/providerprocess contracts, tests and docs. Dependents include turn graph dispatch/stops, daemon runner construction/workflow source and retained delivery/capture integration. Durable dispatch, approvals, store and recovery remain factory responsibilities. No team role/skill or team-release edits are necessary in the candidate; existing teams remain compatibility fixtures.

Evidence:

- `internal/turn/graph_node.go:1`
- `internal/daemon/runners.go:1`
- `internal/daemon/workflow_source.go:1`
- `internal/agent/delivery.go:1`

## F-05

Superseded paths are the current in-package generic parsing/navigation and native provider execution portions, not whole factory integration packages. SemanticDigest filters factory lf metadata; keep digest compatibility in factory adapter rather than changing identity during extraction. No state migration is proposed; preservation remains to be proved by implementation checks.

Evidence:

- `internal/graphexec/dot.go:1`
- `internal/graphexec/semantic.go:1`
- `internal/harness/runner.go:1`
- `internal/codex/runner.go:1`

## F-06

Existing purity test explicitly permits agent/config/planbrief imports and checks direct imports. It does not establish the required transitive reusable boundary. DOT scope test rejects arbitrary new .dot files and admits shipped team graphs plus a legacy Implement fixture; demo fixtures must respect or intentionally update that guard.

Evidence:

- `internal/graphexec/purity_test.go:1`

## F-07

Existing coverage separates imperfectly into graph mechanics, factory policy, native execution and integration. Retain policy/approval/recovery/capture tests in factory; relocate neutral tests and characterize mixed translations first. Add library/demo transitive dependency checks and a non-factory consumer check. Detailed ownership matrix and proposed layout/class diagram are in artifact library-boundary.

Evidence:

- `internal/graphexec/graphexec_test.go:1`
- `internal/harness/advisor_capture_test.go:1`
- `internal/harness/mcp_test.go:1`
- `internal/turn/graph_node_test.go:1`
- `internal/turn/graph_stop_recovery_persistent_test.go:1`

## F-08

Executed baseline: go test ./internal/graphexec ./internal/agent ./internal/providerprocess ./internal/harness ./internal/codex — all five passed. This establishes the local baseline only; no whole-repository, race, exhaustive coverage, real-provider or deployed recovery verification was run.

Evidence:

- `go.mod:1`

## F-09

Context checked: current issue #1268 and parent #1251 via gh; related all-state extraction issue search found this issue; all-state graph/agent PR search surfaced historical workflow fixes and neutral base-team PR #1202, not an existing extraction implementation. Recent affected commit 051afef1 changes provider refusal/authentication handling and fresh conversation semantics, which extraction must preserve. Search of retained .aiteam paths found no 1251/1268 brief to reuse.

Evidence:

- `internal/harness/conversation_recorded_test.go:312`

## F-10

Shaping inventory at 4dc8a8063e172df61bb7619fcfdecbe3dcba3448 confirms auxiliary provider surfaces include schedule/conflict proposals, chat, doctor/repair, judge, drift and log analysis as well as classification/conversation; Permissions documents omitted-versus-empty grant semantics, and providerprocess.Start requires authorization rechecks on fresh retry attempts. These constrain extraction beyond Runner.Run. No code changed or tests rerun in shaping; F-08 remains the earlier baseline only.

Evidence:

- `internal/agent/permissions.go:1`
- `internal/providerprocess/start.go:11`
- `internal/harness/runner.go:29`
- `internal/codex/runner.go:118`
- `internal/harness/drift.go:35`
- `internal/harness/log_analyst.go:45`
- `internal/codex/chat.go:11`

## F-11

Read issue #1269 via gh issue view: Establish factory core contracts and include the operator CLI; it owns intake, scheduling, gates, workspaces, attempts, durable results and publication/recovery, defines commands/read views and CLI ownership, includes qualified GitHub/Linear ticket identity cleanup and logical state ownership, and explicitly consumes the execution-library boundary. This establishes complementary scope, not a second execution library.

Evidence:

- `.aiteam/lf-1268-extract-reusable-graph-and-agent/2026-10-31/extract-reusable-graph-and-agent/ISSUE_1269.json:1`

## F-12

Counted top-level production .go files (excluding *_test.go, including comments) in existing internal packages: graphexec 25 files/3783 lines, agent 37/4047, harness 28/6723, codex 16/3212, providerprocess 7/977, totaling 18742 candidate source lines; turn 217/58572, daemon 115/29680, store 241/49528, instructionpackage 11/1540 and teampackage 6/1625 total 140945 factory-owned source lines. These describe current source pools and do not estimate moved lines, effort, or total repository proportions.

Evidence:

- `internal/graphexec/graphexec.go:1`
- `internal/agent/request.go:1`
- `internal/harness/runner.go:1`
- `internal/codex/runner.go:1`
- `internal/providerprocess/start.go:1`
- `internal/turn/graph_node.go:1`
- `internal/daemon/runners.go:1`

## F-13

ActionSpec in graphexec is declarative membership/outcome/attribute data; turn.ActivityRegistry separately maps names to ActivityHandler.Execute implementations using factory Options and identity-bearing ActivityRequest. ExecuteNodeAction resolves node.Action, invokes the handler after admission/reservation and selects the next edge using ActivityResult.Outcome. RegisterAgent, RegisterControl, RegisterComment and Plan effect registration pair declarations with factory handlers. These existing handlers and durable dispatch remain factory-owned; no reusable executable action framework is implied by extraction.

Evidence:

- `internal/graphexec/registry.go:25`
- `internal/turn/graph_activity.go:43`
- `internal/turn/graph_node.go:155`
- `internal/turn/graph_node.go:259`
- `internal/turn/graph_node.go:329`
- `internal/turn/research_agent.go:69`
- `internal/turn/research_control.go:25`
- `internal/turn/research_comments.go:56`
- `internal/turn/plan_graph.go:610`

## F-14

At 6d86064d7b30fae45f6a5a1e12093882e5eeb876, source inventory across production internal Go files found 40 Execute receiver types taking ActivityRequest: 6 shared/Research, 4 Plan, 4 Spike, 6 Bug, 5 Implement and 15 Validate including wrappers/adapters. All are listed in activity-inventory and the owner-facing class panels. Non-activity Execute services and test handlers are excluded; terminals bypass handlers. Prior package line counts in F-12 describe its earlier inspected baseline, not a refreshed count.

Evidence:

- `.aiteam/lf-1268-extract-reusable-graph-and-agent/2026-10-31/extract-reusable-graph-and-agent/ACTIVITY_INVENTORY.json:1`
- `internal/turn/graph_activity.go:43`
- `internal/turn/graph_node.go:130`

## F-15

Production registration confirms many-to-many action/handler binding: shared Agent/Control/Comment/Judge/Sync/Brief; Plan runtime uses planGraphHandler and story/approval effects; Bug role dispatcher selects analyst/fixer; Implement's implementStepFunc binds selectPath, await, anyParked, ask, reportDone, resolved, budget, workerResult, storyDone and parkStory; Validate has 14 activity types plus admitted-suite wrapper. These handlers consume factory Options and identity-bearing requests, so importability later requires capability boundaries rather than directory relocation alone.

Evidence:

- `internal/turn/research_workflow.go:70`
- `internal/turn/plan_graph.go:622`
- `internal/turn/bug_conversation.go:44`
- `internal/turn/implement_steps.go:56`
- `internal/turn/validate_flow.go:157`
- `internal/turn/validate_suite.go:479`
