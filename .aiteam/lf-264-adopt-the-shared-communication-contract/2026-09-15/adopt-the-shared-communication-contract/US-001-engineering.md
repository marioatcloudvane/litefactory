# US-001 implementation handoff

Frozen identity: issue 298 / issue_id 5463751081 / parent_id 5439211198 / revision `9a738f696ae27b0a265127f1dea85f0fb0d30d1773b1ca211b165a5c057b5487`; dependencies: none. Source: https://github.com/marioatcloudvane/litefactory/issues/298. Workflow 2, Implement only.

Implementation is ready for specialist test writing/execution. This report does not mark engineering, acceptance, adoption or retirement complete. No acceptance review was performed and no factory submission was made by this specialist.

## Implemented

- Production daemon constructs shared result lifetime even with MCP auto-start disabled; `DisableMCP` suppresses native registration only. Unsupported native providers retain the shared helper/terminal route.
- Shared helper broker uses the existing transport, stable request journal and receipt APIs. A factory-owned `AllowResult` scope delegates authored validation to the result service. Where checkpoint and result helpers coexist, the latter is nested under the existing checkpoint directory so working profiles need no wider root.
- Terminal create/update inputs use `AgentResultService`; current-attempt accepted live input can satisfy finalization. Equal decoded terminal/live results reuse the original revision. Conflicting complete replacement input must use explicit revision-bound updates.
- Missing/malformed current-attempt required output rejects nominal process success with actionable retained diagnostics. Earlier attempts' accepted snapshots do not excuse missing current input. Live transports are joined before final validation. Failed/cancelled/interrupted process causes remain authoritative; valid application input can survive without triggering success-only workflow projection.
- Validate's existing complete reference-only contributions object adapts to a shared application result. All terminal contributions and conflicts are checked before the first terminal decision, under the session lock; lost-reply recovery runs before reconciliation. No shell/capture/edit/check/delegation permission is added.
- Shared Plan values adapt to the existing typed contribution callbacks and internal Plan handoff/publication owner. Exact earlier Plan checkpoint values are reused; mismatches reject. Publication remains at its existing success/save boundary.
- `AgentResultServiceOptions.ValidateCompletion` is invoked during preflight, submit and final enforcement. Implement currently calls `ValidateAgentResultCompletion` with no supplied checkpoints; this intentionally fails closed until US-002 enriches trusted context and resolves authoritative retained checkpoints/aggregates. `RequireAggregate` is not relaxed.
- Application receipts observe engineering/acceptance only after the corresponding authoritative final checks/decisions. Publication remains independent.

## Explicit compatibility

Unmanaged or submission-v1 Implement/Validate and managed v1 Plan retain their existing required workflow inputs and receive an explicit compatibility/upgrade prompt. No generation or workflow versions are changed. Nil runtime remains available to existing test/injected callers; production construction installs the runtime independently of MCP settings.

## Changed production files

- `internal/daemon/setup.go`
- `internal/submission/broker.go`
- `internal/turn/agent_results.go`
- `internal/turn/mcp_runtime.go`
- `internal/turn/result_finalization.go` (new)
- `internal/turn/submission_runner.go`
- `internal/turn/submissions.go`
- `internal/turn/typed_plan.go`

## Executed checks

Base revision: `16634ed33075ea3acfc449bcb6316163bdc0b807`, with the eight uncommitted source files above. Sorted path/NUL/SHA256/NUL/false/newline manifest digest of those source files: `adb66f9ba91404e262ad5b8116dd28ff8dac229bd53bf72a4e688494f8adc1e4`.

- Final `go test ./internal/turn -run 'TestValidationCheckpointReconcilesTerminal|TestSubmissionTerminalValidationRequiresRecordedReceipt|TestMCPRuntimeUnmanagedPreparation|TestTypedPlan' -count=1`: PASS, `ok github.com/cloudvane/litefactory/internal/turn 16.221s`.
- `go test ./internal/submission -run 'TestBroker|TestCall' -count=1`: PASS, `ok github.com/cloudvane/litefactory/internal/submission 0.592s` (broker source unchanged since execution).
- `go vet ./internal/turn ./internal/submission ./internal/daemon`: PASS, no output. Executed before the final receipt-observation-only addition; aggregate gate must run vet on final state.
- `git diff --check`: PASS, no output.
- `gofmt -l` on all eight listed files: PASS, no output.

Earlier focused reconciliation execution exposed a lost-reply recovery ordering error. Recovery was moved ahead of terminal conflict prevalidation and the passing reruns above include that fix. An intermediate compile failure from an unavailable store accessor was corrected by retaining copied canonical Plan contribution bytes in the existing typed Plan state, without a new accessor.

## Specialist handoff / remaining verification

No test files were edited, in accordance with the implementation engineer role. Required new tests should cover:

1. Production constructor and actual helper/terminal runners for all five modes, native enabled/disabled/unsupported, cold/resume/fallback and the explicit compatibility branches.
2. Required missing/malformed output, earlier-attempt-only snapshot, duplicate/conflicting full terminal result, successful live result plus ordinary terminal prose, and explicit revision-bound update.
3. Failure/cancellation/interruption with valid checkpoints, valid/invalid/missing final input, original process cause and resumable intent retained; no workflow projection on failure.
4. Multi-story Validate conflict in a later story before new earlier terminal acceptance; identical live/terminal receipt recovery; no extra restricted-profile grants.
5. Plan helper/terminal exact duplicate and mismatch handling, callback validation before application acceptance, and retained owner handoff/publication boundaries.
6. Completion callback invocation through preflight, submission and final enforcement, failing closed without resolver facts. Full authoritative aggregate/checkpoint enrichment belongs to US-002 before adoption.

This is offline source/execution evidence only. Authentic host observations on #274/#296 remain outside this implementation handoff. No full package/repository suite, make check, dist build, provider process, forge action, commit or push was performed.
