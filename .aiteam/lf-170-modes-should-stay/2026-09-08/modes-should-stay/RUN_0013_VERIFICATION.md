# Run 0013 verification — modes-should-stay

Parent: github:marioatcloudvane/litefactory#170. Factory run 290, generation 2.
Starting worktree: clean at 4685737. The supplied generation-2 frozen revisions were the only story specifications; no forge or runtime-state access was performed.

## Changes made in this run

Added internal/daemon/mode_retirement_internal_test.go. Four migrated-SQLite cases exercise actual empty-classifier legacy retirement, covering explicit/inferred completed journals while preserving unfinished work. No production code or protected instruction files changed.

## Fresh story checks

| Story | Result | Fresh scoped race tests | Published event |
| --- | --- | --- | --- |
| US-001 | All four ACs pass; independent review clean | daemon 35.591s, store 6.419s | 000001.json verified |
| US-002 | All six ACs pass; independent review clean | daemon 30.845s, store 2.698s, turn 14.801s; store approval/replan 3.561s | 000002.json verified |
| US-003 | All six ACs pass; independent review clean | planbrief 1.798s, turn 82.008s | 000003.json verified |
| US-004 | AC-004-1, 2, 3, 6 pass; AC-004-4, 5 blocked | turn 26.614s, harness 1.660s, codex 2.042s | 000004.json blocked |

Events are transient factory-owned files in .litefactory-story-events/290, not committed artifacts. Verified events contain full scoped file snapshots, exact AC names and passing review/test evidence bound to manifest digests. Event creation is confirmed; forge closure is owned by the factory and is not claimed here.

## Remaining blocker

Three current Codex copies still contradict or omit the matching Plan discussion and continuation guidance: plan-orchestrator, requirements-engineer and proxy-product-owner under .codex/skills. This run's filesystem boundary makes .codex read-only. AC-004-4 and AC-004-5 therefore remain unsatisfied; no bypass was attempted.

The existing CODEX_SKILLS.patch passed a fresh git apply --check. Apply it in an authorized writable environment, then verify source/copy parity and publish new US-004 evidence against its selected revision. Runtime test success alone does not satisfy the two documentation criteria.

## Aggregate gates

- go build ./...: passed on current production code.
- make check: passed (exit 0), including formatting, vet, pinned lint (0 issues) and full go test -race ./...; turn completed in 529.695s.
- Full race coverage: passed (exit 0), 30 tested packages. Final package result: ok  	github.com/cloudvane/litefactory/internal/turn	464.534s	coverage: 79.3% of statements.
- Aggregate scoped review: BLOCKING only for the protected US-004 copies; see RUN_0013_CODE_REVIEW.md.
- APP_CONTEXT/APP_CONSTRAINTS accuracy check: no semantic update required for this test-only change.

Prior run claims were not carried forward as accomplishments. This report describes this run's observed checks and changes only.

Final snapshot audit: all three verified event manifests, criterion names and file bytes matched the current worktree. No invalidation was required. No commit or push was performed by this run.
