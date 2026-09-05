# Drift Report — logs-f-issue-follows-only-the-run
# Branch: lf/88-logs-f-issue-follows-only-the-run | Date: 2026-09-05

## Delivered as planned

- US-001 implemented the issue-level follow lifecycle: implicit follows advance through later ordinals, while explicit ordinal selection remains pinned.
- Per-run successor waiting uses the completed run's persisted agent timeout; store observations remain short-lived and cancellation exits successfully.
- Terminal database state performs a final drain before exit or successor waiting; raw rollover notices use stderr and rendered notices use stdout.
- Co-located real SQLite/JSONL tests cover rollover routing, explicit pinning, and all three terminal events after initial EOF. Legacy fixtures were aligned with the explicit configuration-error behavior.
- Review blockers on missing configuration and cancellation handling were fixed and re-reviewed cleanly.

## Added scope

- Repaired the unrelated macOS process-tree harness checks discovered by full-suite verification: unavailable process enumeration is an explicit test capability condition, and BSD `pgrep -P` receives its required pattern argument. This was necessary to restore the project-wide verification gate and did not change logs-follow behavior.

## Dropped or deferred

- None.

## Architectural deviations

- None. The follow lifecycle remains a synchronous `internal/cli` ticker-based flow with no schema change, watcher, daemon coordination, goroutine, or dependency.

## Verdict

DRIFTED — the planned logs-follow delivery is complete; one small unrelated Darwin harness portability repair was added after it blocked full-suite verification.
