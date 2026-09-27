# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

A daily background sweep already exists for a different resource: it processes one registered task at a time, and once every candidate has been visited in a pass, it marks the sweep complete and schedules the next pass 24 hours later — the same daily cadence the requested archive job needs.

Evidence:

- `internal/daemon/retirement_sweep.go:70`

## f2

Closed-issue detection this sweep relies on is a fresh GitHub API read that checks issue number, state and API URL identity, not a cached or webhook-driven flag — the same check a new archive job would need to reuse or duplicate.

Evidence:

- `internal/daemon/retirement.go:80`

## f3

That sweep only removes a resource once the owning task has no active run and no pending effects, and only after a configured number of days has passed — the same 'closed + settled + past retention window' shape the requested feature describes, applied to git worktrees and branches rather than database rows, and grouped by a task's whole session family (parent run plus any child story runs), not a single session.

Evidence:

- `internal/daemon/retirement.go:94`
- `internal/store/retirement_owners.go:93`

## f4

The retention window for that existing sweep is an operator-configured config.yaml value with its own default (14 days) and validated bounds (1 to 3650 days) — the established pattern a new archive-retention key would follow, but it is a distinct setting already shipping with its own default for an unrelated resource.

Evidence:

- `internal/config/config.go:450`
- `internal/config/validate.go:108`

## f5

There is already a manual, read-only preview command for the existing sweep that lists per-resource decisions without deleting anything — evidence that a small read-only look-up command is a cheap, precedented addition if wanted, separate from any heavier restore/dry-run machinery.

Evidence:

- `internal/cli/cmd_task_retire.go:16`

## f6

The large tables the original ticket names as the database's actual space problem are real, current tables, confirmed in the schema, and nothing archive-related has been added to the schema since.

Evidence:

- `internal/store/migrations/0014_story_progress.sql:9`
- `internal/store/migrations/0028_submission_contracts.sql:35`

## f7

No archive-related table, CLI command, or config key exists anywhere in the codebase today; every other 'archive' hit in the code is an unrelated word (document ingestion, sub-issue handling, test fixtures).

Evidence:

- `internal/attach/ingest.go`

## f8

Several tables, including the Plan workflow history table the original ticket named as part of the space problem, have their own database trigger that refuses any DELETE (and any UPDATE) against them, by design, to keep that history permanent — this blocks treating them the same as the other bloat tables without a separate, deliberate decision.

Evidence:

- `internal/store/migrations/0058_plan_workflow.sql:21`

## f9

The three tables the original ticket measured as the largest contributors (story progress events, submission receipts, submission file snapshots) carry no delete-blocking trigger, and are only read by code paths tied to an in-progress run's recovery/state resolution, not by any user-facing history or show command — so a task that is fully settled (closed, no active run, no pending effect) has no live code path left that reads them.

Evidence:

- `internal/store/submission_recovery.go`
- `internal/store/story_progress.go`
- `internal/store/engineering.go`

## f10

The application-result tables are heavily cross-referenced by foreign keys to each other and to submission receipts, which makes them a harder, separate unit to detach safely than the three simpler evidence tables.

Evidence:

- `internal/store/migrations/0040_application_results.sql:32`
