# Pre-extraction safety coverage

Inventory of the starting implementation, prepared before replacing its bodies. Existing tests below are evidence locations, not claims that their complete suites were run in this task. Research F-08 records its earlier five-package baseline separately. The extraction must retain these ownership boundaries.

| Invariant | Existing characterization / owner | New gap coverage |
|---|---|---|
| K-01 observable graph behavior | `internal/graphexec/graphexec_test.go` covers defaults, escaping, exact byte digest, duplicate declarations, missing endpoints, malformed scalars, validation diagnostics, cycle bounds and navigation; `owner_stops_test.go` covers parked zero-edge outcomes; workflow-specific suites cover shipped/legacy profiles | `extraction_characterization_test.go` characterizes unsupported DOT forms, chained edge defaults and attribute independence, ambiguous/empty/terminal/undeclared navigation |
| K-01 approvals, receipts, worktrees | `internal/turn/feature_delivery_test.go`, `graph_node_test.go`, `agent_result_persistence_test.go`, `execute_test.go`, `execution_connections_test.go`; durable approval/receipt tests remain factory-owned | No new neutral-library test should replace these integration checks |
| K-02 immutable captures | `internal/agent/delivery_verify_test.go`, both providers' `verified_delivery_test.go`, `advisor_capture_test.go` | Existing capture integrity tests stay in factory adapters |
| K-03 syntax, scalar faults, graph IDs | `internal/graphexec/slot_cost_test.go` rejects invalid raw declarations even when overwritten; `semantic_test.go` separates presentation changes from semantic identities; `graphexec_test.go` preserves wire/profile interpretation | New unsupported DOT and chained-default checks above. Scalar declaration policy stays factory-owned |
| K-04 provider argv, native events/errors/usage | Claude `runner_test.go`, `stream_test.go`, `usage_test.go`; Codex `runner_test.go`, `recovery_test.go`, `usage_test.go`, `usage_evidence_test.go`, `usage_filesystem_evidence_test.go`; both `doctor_transport_test.go`, `repair_transport_test.go`, `judge_transport_test.go` | Keep fixture protocol data with relocated neutral transport tests; attempt attribution remains factory-owned |
| K-04 cold/resume/missing session/auth/refusal | Both `conversation_recorded_test.go`; Codex `recovery_test.go` and `quota_test.go`; Claude ordinary runner fixtures | Existing exact session callback/cancellation cases must survive integration |
| K-04 bounded output, environment, cleanup/cancel | `internal/providerprocess/doctor_test.go` covers output bounds/fragments, judge oversize discard, parent cancellation and secret-free environment; `kill_test.go`; both runner cancellation tests and recorded interruption suites | New process characterization verifies exact PID journal before exec and prevents exec on journal persistence failure |
| K-04 ownership/PID barrier/session ack | `internal/providerprocess/conversation_test.go` covers live PID refusal, stale launch fence, started/no-process failure facts; both recorded conversation suites cover session callbacks, resumed identity and interrupted state | `internal/providerprocess/extraction_characterization_test.go` supplies the previously missing direct barrier/journal-failure checks |
| K-05 permission presence/ceiling/recheck | Both `verified_delivery_test.go`, `validation_test.go`, `bug_analyst_permissions_test.go`; Claude `workflow_role_test.go`; Codex `TestRunnerRecoveryRechecksDelivery`; Claude `TestClaudeDeliveryReverifiedEveryLaunchPath`; `internal/providerprocess/start_test.go` covers eligible retry, bounded absence, cancellation and unrelated failures | Presence values and capture checks belong to factory adapters; neutral contracts must carry explicit presence unchanged |
| K-06 same-state retained recovery | `internal/turn/graph_stop_recovery_persistent_test.go`, `graph_delivery_test.go`, `activity_durable_test.go`; recorded conversation native-state tests | No state conversion is introduced by this task |
| K-07 intermediate usable integration | Existing adapter entrypoint/constructor tests and provider fixtures listed above | Each cutover must run its focused fixtures before replacing the next provider |
| K-08 bindings/outcomes/attempt atomicity | `internal/turn/graph_node_test.go`: registry, dispatch/persistence/cloning, failure outcomes, completion atomicity, mismatched pins, dispatch limits; `graph_composition_test.go` | Activities and durable dispatch remain in core |

## Executed checks

- `go test -race -timeout=2m ./internal/graphexec ./internal/providerprocess -run TestExtraction`: graph passed; initial process assertion expected a newline despite the existing line observer stripping it. Corrected only that assertion.
- `go test -race -timeout=2m ./internal/providerprocess -run TestExtraction`: passed after the assertion correction.
- New files were formatted with `gofmt`.

## Limits

This is a coverage inventory and focused pre-extraction characterization, not exhaustive proof. It does not establish live provider availability, exactly-once execution, complete usage accounting, cross-version native sandbox support or final integration correctness. Root must run final changed/importer package suites and lint. No shipped agent/skill wording is pinned by the added tests. No behavior defect was repaired.
