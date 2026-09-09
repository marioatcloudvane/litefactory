# Code Review

Feature: a-failed-run-s-uncommitted-work
Reviewed by: go-code-reviewer
Date: 2026-09-09
Status: CLEAN

## Blocking findings

None.

## Advisory findings

None.

## Cross-story consistency

US-1, US-2 and US-3 implement the supplied frozen scope coherently. Normal dirty recovery defers synchronization only after existing policy gates; store and report share the new outcome, and both agent adapters receive bounded context on every prompt path. Confirmed resolution retains numeric authorization, fresh revisions and dirty-work barriers, while its failure disclosure provides the ordinary-comment recovery sequence. Reserved transport remains excluded from ordinary status/save; explicit pre-merge guards protect staged/tracked changes and incoming collisions from raw Git mutation. Success-only saving and publication-before-save semantics remain intact.

The new guards use caller context, retain the clone critical section and wrap underlying inspection failures. No new goroutines, retry clocks, configuration keys, migrations, package dependencies or external services were introduced. SQL stays in store; Git execution stays in git; prompt/report formatting remains shared and pure. The probe documents the observed index mutation that justified the narrow additional merge guard.

Settled context documents describe these behaviors and add CONC-15/16 with relevant enforcement references. Duplicate lookup/enforcement rows were consolidated without deleting unique obligations; the stored-success lookup preserves the newer semantic outcome contract. No skill/agent mirror files changed.

## Validation reviewed

- Renewed combined US-1/US-2 scoped race/coverage suite: PASS, **78.534s**, **44.2%** turn coverage, run by reviewer against final shared code.
- US-3 collision/save suite: PASS, **38.964s**, **25.5%** git coverage; Plan retry suite: PASS, **14.136s**, **24.1%** turn coverage, reported by specialist.
- Adapter stdin, raw-index preservation and store round-trip suites passed as detailed in `CODE_REVIEW_US1.md`.
- `git diff --check` passed. The original US-1 evidence was invalidated before the shared merge guard changed; renewed US-1 and US-2 review cover that final code.

Coverage figures describe scoped commands only. This code-review gate does not claim the final `make check` or aggregate build passed; the orchestrator must record those results separately.

## Final changed-file scope

- `.aiteam/APP_CONSTRAINTS.md`
- `.aiteam/APP_CONTEXT.md`
- `.aiteam/lf-171-a-failed-run-s-uncommitted-work/2026-09-09/a-failed-run-s-uncommitted-work/CODE_REVIEW_US1.md`
- `.aiteam/lf-171-a-failed-run-s-uncommitted-work/2026-09-09/a-failed-run-s-uncommitted-work/CODE_REVIEW_US2.md`
- `.aiteam/lf-171-a-failed-run-s-uncommitted-work/2026-09-09/a-failed-run-s-uncommitted-work/CODE_REVIEW_US3.md`
- `.aiteam/lf-171-a-failed-run-s-uncommitted-work/2026-09-09/a-failed-run-s-uncommitted-work/GIT_TRANSPORT_PROBE.md`
- `internal/codex/deferred_sync_test.go`
- `internal/git/merge.go`
- `internal/git/reserved_sync_test.go`
- `internal/git/status.go`
- `internal/git/status_preservation_test.go`
- `internal/harness/deferred_sync_test.go`
- `internal/harness/prompt.go`
- `internal/report/run.go`
- `internal/store/deferred_sync_test.go`
- `internal/store/runs.go`
- `internal/turn/apply.go`
- `internal/turn/deferred_dirty_test.go`
- `internal/turn/dirty_confirmation_recovery_test.go`
- `internal/turn/plan_retry_transport_test.go`
- `internal/turn/prepare.go`

## Verdict

CLEAN — no findings across final implementation, tests and context documentation. Proceed to final aggregate build/check gates; preserve already published scoped evidence unless subsequent changes affect its snapshots or validation.
