# Implement repair evidence

Tested source commit: `bb83fbc4c604c4a915557a6d972b243a1beded9d`. This run consumes the factory-synchronized main fix for Bug #1340, merge `399fc2da955d8800154053d0aa9a4aa9dcfb7088`. `git merge-base --is-ancestor` returned zero. No production code, graph, or baseline was edited in this repair.

The diff from reviewed commit `dd6a02757fa84a79ee85633c9a6af1ea19a03440` contains only the upstream compatibility-test comment and five Validate identity triples. The upstream correction preserves the 31 baseline keys and does not change graph parsing, navigation, or test logic. No further baseline correction was made here. A direct JSON comparison with the upstream merge confirmed equal 31-key sets, all five Validate entries identical, and only the already changed source Implement entry differing.

## Delegation

Two subagents independently reviewed the consumed graph fix and the routing end-to-end coverage. Both were read-only; the primary worker owns the fresh executions below. Three subagents built the original implementation, as recorded in the supplied predecessor findings.

## Fresh executions

- `go test -race -count=1 -timeout=20m ./internal/graphexec`: passed, 2.146 seconds; see `graphexec-test.log`. Includes `TestExtractionRetainsShippedAndLegacyGraphIdentities`.
- `go test -count=1 -timeout=30m ./internal/turn`: passed, 803.978 seconds; see `turn-test.log`. This is a fresh complete passing package execution on the tested commit, including the Auto routing end-to-end test.
- `go test -race -count=1 -timeout=45m -run <exact Makefile RACE_TESTS_turn selection> ./internal/turn`: passed (246 configured test names selected), 435.382 seconds; selected names are retained in `turn-race-selection.txt`, output in `turn-race-test.log` (exit zero).
- `make lint PKGS='./internal/graphexec ./internal/turn'`: passed, pinned golangci-lint v2.13.1, zero issues; see `lint.log`. Sandbox denied writes to the optional lint cache; analysis completed successfully.
- `go build ./...`: passed; see `build.log`.
- `go run ./cmd/teamrelease -check -source teams/source/go-development -source teams/source/python-fastapi -source teams/source/dotnet-web -source teams/source/swift-ios`: passed; see `generator-check.log` (successful check is silent).
- `gofmt -l internal/graphexec internal/turn`: no unformatted files.
- `git diff --check`: passed.

## Coverage and limits

`TestImplementSizingRoutingEndToEnd` exercises 28 cases across all four shipped new captures, S/M/L, missing and invalid sizing, changed captured role defaults and old unsized approvals. It asserts exactly one selected worker, configured tier/model delivery and retained provenance, selected result and completed local saving. Additional tests cover immutable sizing through repairs/restarts/resumes, original single-worker captures, attempt budgets, blocked/incomplete/failed answers, failed saving, deferred synchronization and selected-worker capacity.

These checks use local fake providers, local forge endpoints, SQLite and local Git repositories. They establish engineering behavior, not live model execution or acceptance. Owner check pending: AC-002, AC-003, AC-004, AC-005, AC-006. The owner's live L-story observation and the supplied architecture finding remain for Validate, unchanged by this repair.

The earlier release deviation remains: four new 1.0.1 team packages preserve immutable 1.0.0 releases. New routing requires the matching runtime and a newly captured 1.0.1 graph. Existing captures retain their original routing. Existing Go instruction prose remains outside this story (`internal/turn/implement_policy.go:17`, `:19`; `internal/turn/research_delivery.go:73`).

## Final engineering state

All scoped checks in this repair passed. No remaining test failures were observed in these executions. The full repository test suite and acceptance review were not run by this worker. Source and baseline files remain unchanged from the factory-prepared commit; only this session’s evidence and handoff files were added.
