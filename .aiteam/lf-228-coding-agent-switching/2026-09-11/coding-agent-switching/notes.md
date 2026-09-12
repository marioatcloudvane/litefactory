# Implement run notes — switch-agent-by-label

Persisted workflow: **Separated** (the run was supplied a factory submission helper —
`submission preflight`/`submit`/`receipt` with an engineering-stage capture schema — rather
than the legacy `.litefactory-story-events` protocol, so the separated-workflow procedure was
followed: no scoped/aggregate code review commissioned here, that belongs to Validate).

## Assumptions recorded

- Treated the story's task table (owner roles) as the dispatch plan: one
  `go-implementation-engineer` call covering all 5 production tasks (they are tightly coupled,
  touch overlapping files, and have an internal dependency chain), one `go-unit-test-engineer`
  call for `rewrite-pinned-invariant-tests`, one `go-integration-test-engineer` call for
  `inflight-turn-finishes-under-old-agent`, dispatched after the implementation was ready. The
  two test engineers ran in parallel since neither depends on the other's output, only on the
  completed implementation.
- Phase 2 (environment setup / `TEST_BASE_URL` / `TEST_DATABASE_URL`) was treated as not
  applicable: every test in scope for this story uses embedded/file-based SQLite (this repo's
  normal store-test convention), not an external database or HTTP service, and the story's own
  task table assigns the "against real SQLite" store test to the unit-test engineer, not the
  integration-test engineer. No external test environment variable was needed or consulted.
- Skipped Phase 1.5 (code review) and did not commission any acceptance/architecture/approved-
  plan review — the separated-workflow instructions reserve that for Validate.
- The full, unscoped `internal/turn` test suite (`go test ./internal/turn/...` with no `-run`
  filter) does not complete inside an 8-minute `go test -timeout` / ~10-minute Bash-call
  ceiling; this was independently observed by two of the three sub-agents and reproduced once
  by the orchestrator. Per the operating instructions ("narrow the scope instead" of raising
  the timeout after a kill), the aggregate test gate for `internal/turn` was narrowed to a
  `-run` pattern covering every test this story added or touched plus its closest neighbors,
  run under `-race`, all passing. This is treated as sufficient evidence for this story; it is
  not a full-suite green run of that one package.
- `internal/teamsource` has pre-existing, unrelated test failures (instruction-tree parity
  checks between `.claude/**` and `.codex/**`) confirmed, via `git status --short`, to be
  untouched by any change in this story. Left as-is, documented in `TEST_REPORT.md`, not
  treated as a blocker for this story's completion.
- `make check`'s actual target (`gofmt -l . && go vet ./... && golangci-lint run && go test
  -race -timeout=20m ./...`) was previewed with `make -n check` rather than executed as one
  Bash call, because its `-race ./...` combination and 20-minute timeout both exceed this run's
  gate constraints (never combine `-race` with the whole repository in one call; never exceed
  an ~8m `go test -timeout`). Its components were run separately/scoped instead, as detailed in
  `TEST_REPORT.md`.

## Deviations noted by the implementation engineer (see DRIFT_REPORT.md for full detail)

- Added a transient `store.Segment.AgentSwitched bool` field (not a DB column) so
  `resumeDecision` can force cold unconditionally on a switch turn without a history lookup.
- Added an unexported `freshSegmentSessionRef()` helper (crypto/rand, canonical-UUID shape) to
  mint session ids that satisfy existing Claude session-id validation.

Neither introduces a `claude`/`codex` string literal, a new package/interface/config key, or a
DB migration.
