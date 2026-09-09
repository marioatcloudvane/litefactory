# Implementation Notes — transient-codex-reconnect-notices

## Run and scope

Run 0004 (219), generation 1, implements frozen US-1, issue #156 (ID 5387500996), revision `cac8f61bbd84134987aa8a8cd59dc1bb374cbd62dacc555abaa1ea5766d062c8`, with no dependencies. The factory-supplied exact story body is the implementation authority. Local planning artifacts provide shared context only.

## Decisions and assumptions

- Generic top-level error events are diagnostics; explicit turn.failed remains sticky. Existing logs retain notices, so no warning API is needed.
- A private outcome-classification helper is permitted to test context precedence deterministically without live processes or sleeps.
- No infrastructure setup or integration suite is required: scoped behavior uses deterministic unit/fake-process tests and existing local harvest fixtures.
- TLS troubleshooting and recovery of historical work remain excluded.
- No commits, pushes, forge access, runtime-state changes, or dist writes are part of this run.
- The stale specialist instruction to consume local story Markdown is superseded by the user's frozen managed-story handoff. No local story files are created.

## Progress

- T-1 completed in `internal/codex/runner.go`. Existing `go test ./internal/codex`, `go vet ./internal/codex` and repository-wide `go build ./...` passed.
- Phase 1.5 production review: CLEAN, with no production findings. Full regressions and aggregate gate remain pending.
- T-2 completed: `internal/codex/recovery_test.go` adds 97 deterministic leaf cases. `go test -race -cover ./internal/codex` passed (74.8% coverage).
- Existing harvest safeguards passed: `go test -race -cover ./internal/turn -run '^(TestSaveOnlyCommitsASucceededRun|TestSaveDoesNotCommitAbandonedIsUnchanged)$'` (16.9% package coverage for this focused selection).
- Final production, regression and documentation review: CLEAN. No bug-loop cycles or unresolved findings.
- APP_CONTEXT and APP_CONSTRAINTS were synchronized with the shared consumer ownership and EXT-3 diagnostic/terminal contract; enforcement references the new regression file.
- Aggregate `make check` reached the full race suite after formatting, vet and pinned golangci-lint v2.13.1 (0 issues). Nonfatal linter cache-write warnings reflect the sandbox's unwritable default cache directory; no code or configuration workaround was needed.
- Aggregate `make check` completed with exit zero, including the full race suite. This final stable implementation passed both the single-story and parent aggregate gates.
- Published transient verified event `.litefactory-story-events/219/000001.json` immediately after aggregate success. It binds AC-1 through AC-8, scoped review and tests to 14 immutable files with implementation digest `ed5605a2885d3a507582c6e9551ac06bd973d8cd648b4f2dd4c842935e3cb7ea`. Factory acceptance or issue closure is not claimed. No snapshot files changed after publication.

## Protocol provenance

The regression fixture comment records upstream `rust-v0.104.0` source provenance from research and instructions to recheck event semantics on upgrades. No installed or historical incident Codex binary version was verified in this implementation run, and tests use deterministic synthetic streams rather than a live provider.
