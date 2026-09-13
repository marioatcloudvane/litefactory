# Engineering test report — run 0009

## Result

All three frozen stories have passing scoped engineering checks. Overall Implement remains **partial: aggregate gate failed** in unchanged protected instruction-parity checks. No acceptance assessment was performed. Factory save and remote effects remain separate and unconfirmed.

## Executed scoped checks

| Scope | Command | Result |
|---|---|---|
| US-002 refusal classification | `go test ./internal/harness -run '^TestProvenZeroCostRefusalEvidence$' -short -race -cover` | Passed; 26 cases; 1.595s |
| US-002 SQLite persistence/migration | `go test -race ./internal/store -run '^TestMeterRefusal' -count=1` | Passed; 2 tests; 4.564s |
| US-001 in-memory accounting | `go test ./internal/codex -run 'TestRolloutEvidence' -short -race -cover` | Passed; 14 cases; 1.536s |
| US-001 final parser/filesystem regression | `go test -race ./internal/codex -run 'TestFilesystemEvidence|TestRolloutEvidence' -count=1` | Passed; 3.564s after correcting drain-context regression |
| US-001 historical/transaction/CLI recovery | `go test ./internal/store ./internal/cli -run '^TestMeterRecovery' -count=1 -race` | Passed; store 31.335s, CLI 7.811s; six top-level tests including ten subcases |
| US-003 text/JSON/CSV and run-comment absence | `go test -race ./internal/cli ./internal/report -run 'TestMeterPresentationCoverage|TestRetainedInputsNeverReachTheBody' -count=1` | Passed; CLI 8.897s, report 1.492s; four presentation cases plus absence test |

The retained historical fixture reconciles three children and 63 exclusive responses: **$6.754276 added**, **$11.903026 resulting observed subtotal**, later runs unchanged. Tests establish mutation-free preview, original records/catalog preservation, idempotence after reopening, cancellation/rollback/retry, stale/overlapping/active rejection, source revalidation, safe selectors and incomplete diagnostics.

## Aggregate checks

- `go build ./...` — passed.
- Initial `make check` — format/vet passed; lint stopped on two missing exported API comments. Corrected comments. Initial failure retained in `AGGREGATE_INITIAL_FAILURE.txt`; default lint cache also produced sandbox write warnings.
- `GOLANGCI_LINT_CACHE=/private/tmp/lf211-golangci-cache make check` — **exit 2**. Formatting, vet and pinned golangci-lint v2.13.1 passed with 0 issues. Its full `go test -race -timeout=20m ./...` stage completed: **30 packages passed, 1 package failed**. No timeout occurred. CLI 361.544s, Codex 59.772s, daemon 554.716s, harness 64.226s, store 346.293s and turn 1134.684s passed.

Four top-level failures in `internal/teamsource`:

- `TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly`
- `TestCodexSkills_SharedSourceParity`
- `TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree`
- `TestCodexSkills_AC003GuardClausesPreserved`

`git diff --exit-code HEAD -- .codex .claude teams/source internal/teamsource` exited 0 without output: the failing instruction inputs and tests match the starting commit. The frozen meter scope forbids protected-tree edits. These failures remain unresolved; they are not passes or waived. Full actual output is retained in `AGGREGATE_CHECK_OUTPUT.txt`.

## Evidence limits

Extracted historical counters/windows/source hashes are authentic retained facts; minimal JSONL envelopes, topology/scheduling and negative variations are synthetic. Offline tests do not certify current provider hierarchies, fleet-wide coverage or invoice accuracy. No live repair or paid nested capture occurred.

## Immutable engineering receipts

- US-001 final: `e1811bd1ef9a05515adbb7a307c906a8` — valid input, engineering recorded, effects pending. The earlier snapshot was explicitly invalidated as `f9122c7598895d14f4e65acfc0d325de` before refresh because lint comments changed its bytes; original evidence is retained.
- US-002: `859dfa0f32ffc711954c0301cc5f4ba4` — valid input, engineering recorded, effects pending.
- US-003: `57f0be8008fd1f0d85bec84334cc5541` — valid input, engineering recorded, effects pending.

Recorded scoped engineering does not override the failed aggregate gate or establish acceptance/save.
