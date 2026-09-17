# US-003 local I/O execution report

Frozen story #300 / issue ID 5463752385, parent 5439211198, revision `5f7bc6777358833a0b5af6923475849980c79ce1555b8644a7b0cc8153a5fef5`; dependencies US-001/US-002; workflow 2. Tests only, no acceptance review.

## Added coverage

`internal/turn/publication_runtime_io_test.go` adds three groups:

- `TestPublicationRuntimeCatalogFromProduction`: actual prepareMCP, real local broker, temporary SQLite and controlled GitHub HTTP. Research/Implement/Validate can publish the explicitly authorized current-task comment with creation disabled; unrelated target calls fail. Plan/Spike exclude comment publication. Creation is unavailable without owner authority. Implement and Validate use their own supported submission session bindings.
- `TestPublicationRuntimePreparedOwnerIntentNativeLifetime`: real owner proposal preparation and stable replay; retained title/source links; preparation creates no grant; exact approval survives disabled MCP, then binds its exact envelope on eligible native Research preparation. Consumed intent is no longer eligible; replacement cannot inherit it and invalidates obsolete guard.
- `TestPublicationRuntimeCreationRestartObservation`: malformed HTTP reply after remote creation success leaves durable pending effect. Reopened SQLite enumerates with keyset bounds; runtime observation with new creation disabled confirms the exact remote issue without new POST/attachment. Confirmed effect leaves pending enumeration.

Existing executed HTTP cases additionally cover comment/creation exact content, numeric identity, ambiguous provenance, lost reply/reopen, concurrent replay, cancellation, expiry, envelope mutation, partial attachment, and redaction.

## Executed checks

Environment: Go 1.27.0, Darwin arm64. Base HEAD `16634ed33075ea3acfc449bcb6316163bdc0b807` plus uncommitted shared implementation and these tests. Orchestrator must capture final immutable implementation digest; this base SHA alone does not identify uncommitted contents.

1. `go test ./internal/turn -run '^(TestPublicationRuntime|TestCreationHTTP|TestCommentHTTP)' -count=1 -timeout=8m -v` — PASS, 14 top-level groups / 44 including subtests, 0 failed, package 5.417s. Full output: `US-003-io-tests.log`.
2. Same command with `-race` — PASS, same 14 groups / 44 cases, 0 failed, package 83.007s. Full output: `US-003-io-race.log`.
3. After import grouping cleanup: `go test ./internal/turn -run '^TestPublicationRuntime' -count=1 -timeout=8m -v` — PASS. Full output: `US-003-io-final.log`.

Initial development runs exposed fixture errors (missing managed Validate session, incorrect worktree construction, and binding an Implement run as Validate); fixtures corrected before the retained passing runs. No production defect found.

## Bounds and remaining handoff

This report establishes offline repository behavior only. No external forge, actual daemon runtime state, paid/nested provider, build tags, protected instruction writes, dist writes, commits, or acceptance operations occurred. Authentic host observations #274/#296 remain unsatisfied.

Daemon handleComment owner intake and minute/page scheduling are assigned separately by the orchestrator. This report does not establish daemon scheduling/restart guarantees or trusted intake end to end. It also does not replace separate unit authority/refusal coverage or aggregate make check.
