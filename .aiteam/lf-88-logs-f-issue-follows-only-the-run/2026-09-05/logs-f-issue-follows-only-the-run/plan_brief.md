# Plan Brief — logs-f-issue-follows-only-the-run
# Branch: lf/88-logs-f-issue-follows-only-the-run
# Date: 2026-09-05
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/88
# Citation base: 8fa0a08e426e38ef584e14793eb9a582ead52ab5
# Research brief: RESEARCH_BRIEF.md

## Shared Decisions
- Omitted `--run` with `-f` follows successive ordinals; an explicitly supplied ordinal remains pinned. Non-follow selection is unchanged.
- Each completed ordinal starts its own successor deadline, bounded by the configured timeout for that run's persisted agent; unknown historical agent configuration is actionable and has no fallback.
- Every refresh uses the existing 250 ms cadence and a short-lived store access. A terminal database observation receives one final file drain before lifecycle transition.
- Terminal events are `result`, `turn.completed`, and `turn.failed`. Rollover separators bypass every display filter; rendered output uses stdout and raw output uses stderr while raw stdout remains source bytes only.

## Workflow
- Shipping order: US-001 implementation first, then its regression coverage; they ship together because the state-machine change is not complete without deterministic real-file evidence.
- Dependency graph / hard couplings: T-001.2 depends on T-001.1. There are no cross-story dependencies because this is one cohesive lifecycle capability.

## Cross-Story Constraints
- Platform: Keep the synchronous ticker-driven command in `internal/cli`; use concrete config and store contracts; add no package, goroutine, watcher, IPC path, schema change, or dependency.
- Security: Treat unexpected store, file, and agent-configuration failures as contextual actionable errors. Do not retain a database connection during a follow.
- Tests: Preserve co-located, non-parallel real SQLite/real-file CLI tests with bounded contexts and channel synchronization; `gofmt`, `go vet`, pinned lint, and race-enabled tests must remain green.
- Ring-fenced: daemon timeout/kill/reconciliation, allocation, migrations, runner protocols, log rotation, persistent follower state, and an unproven historical-hang root cause.

## Story Index
| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|------------|-----------|
| US-001 | Continuous and bounded issue log following | L | — | <!-- forge-subissue: US-001 --> |

## Out of Scope
- Non-follow log selection and output behavior, except the documented follow rollover separator.

## Provenance
- Citation base: 8fa0a08e426e38ef584e14793eb9a582ead52ab5.
- Known-stale areas: the reported 2026-09-03 live `turn.failed` non-termination has no deterministic root cause; this plan verifies behavior without claiming one.
- Re-pinning requirements: before merge, re-verify current logs command/store accessor names and configured Claude/Codex timeout fields against the target base.
