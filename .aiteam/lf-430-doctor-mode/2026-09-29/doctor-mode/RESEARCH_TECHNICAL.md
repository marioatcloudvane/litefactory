# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

No doctor command exists. The only reference is a comment saying three hard checks run inline instead of in a doctor command.

Evidence:

- `internal/cli/cmd_daemon_agent.go:327`

## f2

The CLI has a single registration slice; a new command is a constructor plus one line. Existing repair-like commands are task clean, task retire (preview only), meter recover (preview, then --apply) and story contract upgrade.

Evidence:

- `internal/cli/commands.go:8`
- `internal/cli/cmd_meter_recover.go:16`
- `internal/cli/cmd_task_retire.go:17`

## f3

Comment commands are parsed in one function. Known names are /spike, /research, /plan, /implement, /validate, /schedule, /confirm and plan controls; anything else becomes KindUnknown. /schedule and /confirm are factory-only controls that never start an agent run, so a non-mode control command has precedent.

Evidence:

- `internal/turn/command.go:227`

## f4

Owner comments are consumed through the per-task queue and watermark. An earlier incident showed a malformed owner command wedging a task, so a /doctor comment sent through the normal path could be blocked by the failure it should repair.

Evidence:

- `.aiteam/lf-314-a-malformed-owner-command-wedges-a-task/2026-09-22/a-malformed-owner-command-wedges-a-task/RESEARCH_BRIEF.md`

## f5

Recorded past stuck states: graph plan wedging a parent (#366); a posted report failing to record and looping, with running rows stranded by lost processes (#210); a closed issue keeping needs_poll and being re-dispatched forever (#169); a failed run's uncommitted work blocking later runs (#171).

Evidence:

- `.aiteam/lf-366-graph-plan-wedges-the-whole-parent/2026-09-23/graph-plan-wedges-the-whole-parent/RESEARCH_BRIEF.md`
- `.aiteam/lf-210-a-report-that-posts-but-fails-to-record/2026-09-09/a-report-that-posts-but-fails-to-record/RESEARCH_BRIEF.md`
- `.aiteam/lf-169-a-closed-issue-keeps-its-needs-poll-flag/2026-09-09/a-closed-issue-keeps-its-needs-poll-flag/RESEARCH_BRIEF.md`
- `.aiteam/lf-171-a-failed-run-s-uncommitted-work/2026-09-09/a-failed-run-s-uncommitted-work/RESEARCH_BRIEF.md`

## f6

State lives in SQLite (sessions, runs, comments, watermarks, schedules, proposals, story progress, meter) with migrations up to 0078, so a doctor can inspect and repair rows.

Evidence:

- `internal/store/migrations/0001_init.sql:50`
- `internal/store/migrations/0078_creation_owner_publications.sql`
