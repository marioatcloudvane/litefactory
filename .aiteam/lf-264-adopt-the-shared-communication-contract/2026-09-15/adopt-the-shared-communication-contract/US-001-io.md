# US-001 real-I/O engineering checks

Frozen story: US-001 / #298 / issue ID 5463751081 / parent ID 5439211198 / revision `9a738f696ae27b0a265127f1dea85f0fb0d30d1773b1ca211b165a5c057b5487`; dependencies none; workflow 2. Implement only, no acceptance review or submission performed.

## Changed tests

- `internal/turn/shared_finalization_test.go`: eleven focused test functions covering finalization and production adapter seams.
- `internal/daemon/shared_result_setup_test.go`: actual `daemon.Open` constructor with native auto-start on/off, local HTTP fixture and SQLite; asserts obsolete runtime authority is invalidated in both configurations.
- `internal/turn/mcp_runtime_unit_test.go`: updated the obsolete unsupported-provider expectation. A configured shared runtime must be valid even when native MCP is unsupported.

No production source was edited by this specialist.

## Coverage

- AC-001/AC-005: all five mode adapters; MCP on/off; cold/resume/fallback request preservation; local helper broker and terminal final routes; unmanaged/legacy Implement/Validate and managed legacy Plan compatibility messaging; unsupported native provider still receives shared completion.
- AC-002/AC-006: 90 SQLite-backed combinations (five modes, six subprocess outcomes, valid/missing/malformed output), preserved cause/resume/exit metadata, input receipts independent of engineering/acceptance/publication, current-attempt requirement, atomic stale/conflicting replacement rejection, exact duplicate reuse, explicit revision update, completion resolver called during preflight/submit/final enforcement, cancelled/deadline contexts and runner errors/nil results.
- AC-003: two-story complete reference-only Validate terminal findings; duplicate live findings preserve correlation; conflicts/invalid later findings/missing coverage cannot partially accept earlier stories. Interrupted process preserves checkpoint without final workflow projection. Validate requests retain exact supplied permissions and receive no new helper filesystem grant.
- AC-002: real typed Plan callbacks retain the existing internal handoff; exact checkpoint duplicates reuse authority, conflicting checkpoint refuses, failed process never forwards Plan. Input acceptance does not publish stories or mutate readiness.

## Executed commands and evidence

Base revision: `16634ed33075ea3acfc449bcb6316163bdc0b807` plus uncommitted implementation/tests. `US-001-io-state.json` records SHA256s and a manifest digest for the production/test inputs used here. No full-package/repository suite was run.

1. `go test ./internal/turn -run 'TestShared(Finalization|RunnerHelper|ValidationMultiple|ManagedRunner|RunnerExplicit|PlanFinalization|UnsupportedNative)|TestRunMCPBound_Disabled|TestValidationCheckpointReconcilesTerminal|TestSubmissionTerminalValidationRequiresRecordedReceipt' -count=1 -timeout=8m` — PASS, 45.106s, retained `US-001-io-plain.log`.
2. Same selection with `go test -race` — PASS, 336.310s, retained `US-001-io-race.log`.
3. `go test ./internal/daemon -run TestOpenSharedResultAuthorityIndependentOfNativeAutoStart -count=1 -timeout=8m` — PASS, 1.724s, retained `US-001-io-daemon-plain.log`.
4. Same daemon selection with `go test -race` — PASS, 4.940s, retained `US-001-io-daemon-race.log`.
5. `gofmt -l internal/turn/shared_finalization_test.go internal/turn/mcp_runtime_unit_test.go internal/daemon/shared_result_setup_test.go` — PASS, empty output.
6. `git diff --check` — PASS, empty output.

Intermediate test-authoring compile failures were corrected (receipt wrapper type, expired-context fixture, worktree type). The Plan fixture initially omitted explicit workflow version and used an overly long Unix socket path; both fixture errors were corrected. No production failure was uncovered by these checks.

## Bounds and remaining proof

All dependencies are isolated local SQLite databases, Unix sockets, temporary git worktrees or controlled local HTTP. Tests use the existing production helper client directly, without launching a CLI executable or actual paid provider. They do not establish authentic Claude/Codex invocation, correction or receipt observation; #274/#296 remain external obligations. The managed runner matrix covers structured terminal routing; native family implementations and helper transport are exercised through the existing broker/service seams rather than authentic host adapters. Authoritative aggregate enrichment is US-002 and remains required before Implement completion/adoption claims. Full pinned aggregate checks belong to the orchestrator. No live forge, daemon state, protected trees, dist files, commits or pushes were touched.

Additional native broker check: `go test ./internal/turn -run TestSharedCompletedResearchAndSpikeNativeTool -count=1 -timeout=8m` passed in 0.988s. It exercises completed Research/Spike submission, authoritative receipt lookup and final success through the actual local family dispatcher. It was added after the first combined selection; a separate race invocation retains its result in `US-001-io-native-race.log`. Production source remained unchanged.

The additional native broker race command passed in 5.566s. Final scoped formatting and diff whitespace checks also pass after that addition.

## Final result

15 selected top-level test functions passed, 0 failed, including the 11 newly added turn tests, one new daemon test, updated runtime gate and two retained reconciliation tests. All selected checks passed both plain and race-enabled execution. The large race selection completed in 336.310s within its unchanged eight-minute test timeout. No remaining failed tests or production defects were identified. This report is engineering execution evidence only; the full story/aggregate completeness decision remains with the orchestrator and authoritative completion enrichment remains assigned to US-002.
