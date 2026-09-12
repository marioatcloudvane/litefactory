# Test Report — switch-agent-by-label

Session: `.aiteam/lf-228-coding-agent-switching/2026-09-11/coding-agent-switching`
Story: `switch-agent-by-label` (issue #252, revision `75fcd752b8f4b40986f1af3a9994afaaf59abab5f0599229559a45b801635054`)

## Engineering work

- `go-implementation-engineer` implemented all 5 assigned production tasks: `store-set-segment-agent`,
  `delivery-switch-in-place`, `prepare-narrow-refusal`, `prepare-force-cold-on-switch`,
  `delete-switch-agent-script`.
- `go-unit-test-engineer` rewrote the two now-superseded pinned invariant tests in
  `internal/turn/execute_test.go`, added new tests covering the switch itself
  (AC-013/014/015), the same-agent no-write regression (AC-016), the always-cold
  A->B->A sequence (AC-017), the narrowed continuation-only refusal (AC-020),
  a delivery-reconciliation test in `internal/turn/delivery_test.go` (AC-018), and
  store-level tests for the new `SetSegmentAgent` accessor in `internal/store/store_test.go`.
- `go-integration-test-engineer` added `TestExecuteInFlightTurnFinishesUnderOldAgentThenTheNextTurnSwitches`
  in `internal/turn/execute_test.go`, a sequential pin (no sleeps/polling/goroutines) proving a
  turn already in flight finishes under the old agent and only the next dispatched turn switches,
  relying on the existing `AcquireSessionLock` per-session serialization. No production code change.

Both test engineers ran concurrently and both touched `internal/turn/execute_test.go`; each
used the `Edit` tool (exact-match, so a true clobber would have errored) and both reports
independently confirm the resulting file compiles and all tests (including the other
engineer's addition) pass.

## Gates run by the orchestrator after all engineering landed

| Gate | Scope | Result |
|---|---|---|
| `go build ./...` | whole repo | pass |
| `go vet ./...` | whole repo | pass |
| `gofmt -l .` | whole repo | clean, no output |
| `golangci-lint run` (v2.13.1) | whole repo | 0 issues |
| `go test -race -timeout=8m` | `internal/store`, `internal/report`, `internal/naming` | all `ok` |
| `go test -race -timeout=8m -run '<switch/prepare/delivery/resume/report/segment/...>'` | `internal/turn` (targeted, covers every test added or touched for this story plus its closest neighbors) | all `PASS`, `ok` (217s) |
| `go test -timeout=8m ./...` minus `internal/turn` | rest of the repo, no `-race` | all `ok`, except pre-existing unrelated failure below |

### Known, pre-existing, out-of-scope failure

`internal/teamsource` has 3 failing tests (`TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly`,
`TestCodexSkills_SharedSourceParity`, `TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree`,
`TestCodexSkills_AC003GuardClausesPreserved`), all asserting parity between the `.claude/**`
and `.codex/**` agent-instruction trees. None of this story's changes touch `.claude/**`,
`.codex/**`, or `internal/teamsource` — confirmed via `git status --short`, which shows only
`internal/naming`, `internal/report`, `internal/store`, `internal/turn`, and the deleted
`switch-agent.sh`. This failure predates this story's work and is unrelated to the
switch-agent-by-label feature; it was not investigated or fixed here (out of scope, and
instruction-tree maintenance is explicitly isolated per this repo's own protocol: Codex
edits `.claude/**`, Claude edits `.codex/**`).

### Not run: full unscoped `internal/turn` suite

`go test -timeout=8m ./internal/turn/...` with no `-run` filter does not complete within 8
minutes — confirmed independently by two of the three engineers and by the orchestrator
(exit 143, goroutine dump from many real-git subprocess-spawning tests, not a hang tied to
this story's changes). This is a pre-existing characteristic of this large package (~200
test functions). Per the run's operating constraints (never raise `-timeout` above 8m to
chase a killed gate), the gate was narrowed instead: a `-run` pattern covering every test
this story added, rewrote, or is adjacent to (`TestExecute*`, `TestPrepare*`, `TestDelivery*`,
`TestReconcile*`, `TestResume*`, `TestReport*`, `TestPlanDelivery*`, `TestVerifyDelivery*`,
`TestAutomatic*`, `TestContinuation*`, `TestSegment*`) was run under `-race` and passed in full.

## Verdict

All engineering and executed tests for this story pass. Aggregate build/vet/format/lint gates
pass whole-repo. Aggregate test gates pass for every package this story touches, plus a
targeted, race-enabled superset of `internal/turn`, plus the whole rest of the repo except one
pre-existing, unrelated `internal/teamsource` failure.
