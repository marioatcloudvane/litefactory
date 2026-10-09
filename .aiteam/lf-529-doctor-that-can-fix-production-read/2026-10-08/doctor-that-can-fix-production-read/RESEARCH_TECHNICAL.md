# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Diagnosis explicitly denies filesystem, shell, network and search. Prepared JSON is limited to 262144 bytes.

Evidence:

- `internal/doctor/prompt.go:12`
- `internal/doctor/prompt.go:31`

## f2

Claude diagnosis has no native tools. Codex diagnosis returns ErrDoctorIsolationUnverified. A prompt edit alone cannot grant reads.

Evidence:

- `internal/harness/doctor.go:18`
- `internal/codex/doctor.go:17`

## f3

Evidence collection records fresh remote comments as a gap and scans at most 1 MiB of daemon log.

Evidence:

- `internal/store/doctor_evidence.go:42`
- `internal/doctor/files.go:86`

## f4

Both native repair providers unconditionally return ErrDoctorIsolationUnverified, so repair stays blocked regardless of environment wiring.

Evidence:

- `internal/harness/repair.go:14`
- `internal/codex/repair.go:14`
- `internal/agent/doctor.go:12`

## f5

Repair is not file-only: row repairs exist for poll_flag, run_interrupt, session_unlock, report_completion, watermark_rewind, graph dispatch/wait/attempt cancel and graph_fail, with approved manifests, backups and conditional updates.

Evidence:

- `internal/doctor/repair.go:319`
- `internal/store/doctor_repair_rows.go:14`
- `internal/store/doctor_repairs.go:60`

## f6

graph_fail rejects active attempts, live dispatches and active owner waits, and launches no retry itself. Implement treats failed instances as retryable up to a cap.

Evidence:

- `internal/store/doctor_repair_rows.go:185`
- `internal/daemon/implement_story_task.go:35`

## f7

Reusable: RequestOwnerStop, TransitionImplementStory, RequeueSchedule (narrow), task worktree merge helpers. No existing consume_comment; field guide forbids watermark advancement.

Evidence:

- `internal/store/runs.go:39`
- `internal/store/implement_stories.go:177`
- `internal/store/schedules.go:179`
- `internal/git/merge.go:334`
- `internal/doctor/prompt.go:16`

## f8

Implement infrastructure-failure allowance counts failed graph instances since the story state timestamp, independent of the attempts field; resetting attempts alone leaves it exhausted.

Evidence:

- `internal/store/implement_stories.go:365`
- `internal/daemon/implement_story_task.go:58`
