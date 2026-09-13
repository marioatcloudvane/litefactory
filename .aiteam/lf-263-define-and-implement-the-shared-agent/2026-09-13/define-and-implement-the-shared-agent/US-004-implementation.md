# US-004 engineering report

Identity #269 / issue ID 5440231091. Frozen revision
`58870c177f9a1b0e1532507b58d0414ce20321930b3d793a655a507138275d37`.
Dependencies US-001 and US-003 were supplied as engineering complete. Their
implementation and retained receipts were not modified.

Production tasks and the explicitly authorized unit/real-SQLite/worktree test
phases are complete. The same worker performed these bounded phases due to the
factory's exhausted thread allocation. No acceptance/review was performed.

## Implementation

Additive migration 0041 and store accessors retain first-class aggregate file/check
associations, binding parent/context, exact frozen key/revision set, manifest digest,
source run/attempt and execution/check identities. All relationships retain actual
complete bytes. Scoped story-check identities cannot be relabelled aggregate proof.
Failed/unexecuted facts are retained honestly but cannot satisfy completion.

Pure agent helpers reuse canonical manifest/content hashing and safe-path rules,
serialize copied complete bytes and factual execution claims, and expose a separate
reference constructor. They never execute checks or certify provider provenance.

Turn capture requires current Implement authority and an authorized git worktree.
It checks every changed path (both sides of renames), stable rooted file bytes,
symlink refusal and stable status, then records through store without spanning I/O
with a transaction. Validate capture is rejected. The retained aggregate resolver
requires exact selection/context, all check IDs, passed executed facts and current
worktree bytes. A direct completion helper additionally resolves the selected
current immutable story engineering checkpoints, preserving legacy verified/full
assessment requirements and separated authority. Prior completed historical
acceptance remains intact. Terminal save is a separate factory gate.

Aggregate-only recovery records its own final snapshot/check source while reusing
earlier story checkpoints. It produces no story events, engineering mutations,
acceptance, readiness, closure, publication or runner-routing effects.

## Changed paths

- `internal/agent/aggregate_capture.go`
- `internal/agent/aggregate_capture_test.go`
- `internal/store/migrations/0041_result_aggregate_evidence.sql`
- `internal/store/result_aggregates.go`
- `internal/store/result_aggregates_test.go`
- `internal/turn/agent_result_aggregates.go`
- `internal/turn/agent_result_aggregates_test.go`
- `docs/agent-result-contract.md`

## Executed evidence

`US-004-tests.log` retains the passing race run over all new tests.
`US-004-final-tests.log` retains the final race/coverage run after the historical
checkpoint preservation refinement. `US-004-lint.log` is pinned v2.13.1 scoped lint
(0 issues); changed Go files are gofmt-clean.

The worktree tests execute a real isolated `go test -count=1 ./...` against generated
deterministic source/test files for both workflow versions and again under fresh
recovery attribution. Logs retain command output, exact selected revisions,
manifest digest and run/attempt. These fixture commands are actual executions;
other negative/outcome unit facts are explicitly synthetic. This establishes
offline Go/SQLite/git behavior, not provider provenance or live adapter adoption.

No full-repository aggregate gate, story submission, acceptance/review, daemon
build/restart, installed-tree change or commit was performed here.
