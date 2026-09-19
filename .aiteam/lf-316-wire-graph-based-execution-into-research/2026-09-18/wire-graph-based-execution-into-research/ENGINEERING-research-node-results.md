# Engineering work: research-node-results (#338)

Frozen revision: e8f7819424297244c5dde17bbe03af44808bafac01aaaa2a44f32dce42c29507.

Status: implementation provided; story completion BLOCKED pending dedicated scoped tests and authentic externally authorized AC-407 observations. No engineering-complete checkpoint or acceptance claim is made by this report.

## Changes

- `internal/agent/graph.go`: factory-owned private Research invocation kind and instance/node/attempt/run attribution.
- `internal/agent/request.go`: refuse incompatible kind/mode or missing/invalid graph identities.
- `internal/agentresult/research_node.go`: strict version-1 response/result envelope; 1 MiB ceiling checked before decoding; existing structural/application validators reject duplicate/unknown/null/missing members, invalid output and disallowed questions. `ask` requires incomplete and nonempty questions; `done` requires completed and no questions.
- `internal/turn/research_agent.go`: `RegisterResearchAgent` declares `lf_research_agent` with `success` outcome. `ResearchAgentActivity` checks a reserved attempt, active run and persisted provider segment, uses the existing runner, joins supplied helper transports, validates terminal node data and reuses current-attempt shared-result receipt/correction validation. It returns private JSON for foundation atomic output/attempt/transition completion, without whole-turn projection, comments, git saving or retry. Process evidence remains separate on `ActivityResult.ProcessResult`.
- `internal/turn/research_artifacts.go`: exact admitted-session containment, clean relative path grammar, regular-file and symlink-ancestry checks using a confined `os.Root`.
- `internal/turn/graph_activity.go`: independent process-result facts on activity return.
- `internal/store/graph_outputs.go`: bounded saved-output reader checks total bytes and row count transactionally before allocating payloads; `LoadResearchNodeOutput` additionally requires a successful attempt and exactly one valid JSON node result.

Foundation files `graph_activity.go` and `graph_outputs.go` were already integrated but untracked before this story; preserve their full foundation contents with these additions.

## Integration contract

Factory composition supplies `ResearchAgentActivity.Prepare`, returning a `ResearchInvocation` containing a prepared host request, runner kind, exact worktree-relative session path, matching bound `AgentResultService`, and optional transport-close function. Preparation does not launch or publish. The factory continues to own run settlement and provider resume-token persistence. Later admission/host stories wire this seam into their authorized launch path; this story does not select an execution path or silently replace legacy execution.

The node always returns the versioned terminal envelope. The existing helper still submits the ordinary application result and preserves preflight/correction/receipt semantics; final node output must agree with the current candidate. Host-native observation and actual production admission composition are not established by offline foundation tests.

## Executed checks

- `go test ./internal/agent ./internal/agentresult ./internal/turn ./internal/store -run 'TestGraph|TestResult' -count=1`: passed. Actual selected tests ran in turn (34.117s) and store (4.805s); agent and agentresult reported no matching tests.
- `go vet ./internal/agent ./internal/agentresult ./internal/store ./internal/turn`: passed.
- `go test ./internal/turn ./internal/store -run 'TestGraph' -count=1`: passed after final helper additions (turn 13.279s; store 2.400s).
- `go test ./internal/agentresult -run 'TestCreate|TestUpdate' -count=1`: passed (0.264s).

No full-repository gate was run by this specialist. Test engineers must add direct parser/artifact, fake-runner and real-store coverage for the new contract before an engineering capture.

## Unverified obligations

AC-407 requires authentic separately authorized Claude and Codex helper invocation, same-run correction and authoritative receipt capture, bound to exact delivered snapshots and CLI versions. None was supplied or performed. Existing offline tests are not substitutes. No paid/nested host launch, forge effect, commit, push or protected installed instruction-tree edit was performed.
