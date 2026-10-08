# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Diagnosis explicitly denies filesystem, shell, network and search. BuildPrompt limits prepared JSON to 262144 bytes; invocation uses a private scratch directory and bounded memory rather than evidence source paths.

Evidence:

- `internal/doctor/prompt.go:12`
- `internal/doctor/prompt.go:31`
- `internal/doctor/invoke.go:49`

## f2

Claude diagnosis isolation returns a tool ceiling with no native tools. Codex diagnosis returns ErrDoctorIsolationUnverified; its transport also disables shell and multi-agent features. A prompt edit alone cannot grant direct reads.

Evidence:

- `internal/harness/doctor.go:18`
- `internal/codex/doctor.go:13`
- `internal/codex/doctor.go:87`

## f3

Evidence collection reads selected database records and cached comment bodies; it explicitly records fresh remote comments as a gap. Local collection observes worktree state and scans at most the latest 1 MiB of daemon log. Paths already centralize litefactory.db and logs/daemon.log.

Evidence:

- `internal/store/doctor_evidence.go:42`
- `internal/doctor/files.go:55`
- `internal/doctor/files.go:86`
- `internal/config/paths.go:77`

## f4

Repair host capabilities are installed by CLI and conditionally by daemon setup. Apply requires environment, runner and restart route plus verified provider isolation. Both native repair providers currently return ErrDoctorIsolationUnverified, which is a concrete production blocker beyond the stale comment about no production environment.

Evidence:

- `internal/cli/doctor_native.go:48`
- `internal/daemon/setup.go:540`
- `internal/doctor/repair.go:63`
- `internal/harness/repair.go:13`
- `internal/codex/repair.go:13`

## f5

Repair is not file-only: Backup/Write dispatch non-file kinds to row repair methods. Existing catalog includes poll_flag, run_interrupt, session_unlock, report_completion, watermark_rewind, graph_dispatch_cancel, graph_wait_cancel, graph_attempt_cancel and graph_fail. Approved manifests, issue ownership, drain acknowledgment, before-state backups and conditional updates already exist.

Evidence:

- `internal/doctor/repair.go:319`
- `internal/store/doctor_repair_rows.go:14`
- `internal/store/doctor_repair_rows.go:90`
- `internal/store/doctor_repair_rows.go:125`
- `internal/store/doctor_repairs.go:60`

## f6

graph_fail rejects active node attempts, pending/reserved/running dispatches and active owner waits. It changes status/finished_at/next_due_at but does not itself launch retry work. Implement discovery treats failed instances as retryable subject to its failure cap; generic workflow startup recovery instead records explicit owner continuation as required.

Evidence:

- `internal/store/doctor_repair_rows.go:185`
- `internal/daemon/implement_story_task.go:35`
- `internal/store/graph_recovery.go:25`

## f7

Reuse points include RequestOwnerStop and the daemon-consumed stop intent, revision-checked TransitionImplementStory with parked-to-waiting owner guidance, RequeueSchedule for running reservations that could not acquire a lock, and task worktree MergePreview/MergeBase/CompleteMerge. RequeueSchedule is narrower than reopening wrongly settled work. Existing field guide prohibits watermark advancement; consume_comment requires a new explicit contract.

Evidence:

- `internal/store/runs.go:39`
- `internal/store/runs.go:76`
- `internal/store/implement_stories.go:177`
- `internal/store/schedules.go:179`
- `internal/git/merge.go:334`
- `internal/git/worktree_apply.go:316`
- `internal/doctor/prompt.go:16`
