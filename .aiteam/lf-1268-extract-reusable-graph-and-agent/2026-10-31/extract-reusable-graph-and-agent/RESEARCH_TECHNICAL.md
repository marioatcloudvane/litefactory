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
