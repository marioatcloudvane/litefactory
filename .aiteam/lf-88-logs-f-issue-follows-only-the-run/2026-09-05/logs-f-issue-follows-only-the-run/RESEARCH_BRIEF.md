# Research Brief — logs-f-issue-follows-only-the-run

## Request

Analyze issue #88: an issue-level `logs -f` selected the latest run available at invocation, followed no later runs, and appeared to remain alive after a `turn.failed` terminal event was appended. The requested outcome is a grounded account of the two reported behaviors and the research needed before planning a correction.

## Codebase Context

### Relevant ownership and flow

- [V] The repository is a Go 1.26 single-module daemon. The command entry point dispatches through the CLI router, and the relevant behavior resides in the logs command and renderer (`cmd/litefactory/main.go:30-74`, `internal/cli/commands.go:7-19`, `internal/cli/cmd_logs.go`, `internal/cli/cmd_logs_render.go`).
- [V] A run row owns its ordinal, status, timestamps, outcome information, and JSONL log location (`internal/store/runs.go:69-120`). A run is allocated before the agent executes, the agent appends event records, and harvesting finishes the row as succeeded or failed after the agent returns (`internal/store/runs.go:166-232`, `internal/codex/runner.go:277-328`, `internal/turn/harvest.go:65-85`, `internal/store/runs.go:255-300`).
- [V] Log command tests intentionally use a migrated temporary SQLite database and real JSONL files, so this area is already exercised close to production file/database behavior (`internal/cli/cmd_logs_test.go:25-67`).

### Current selection and follow model

- [V] When `--run` is absent, selection uses ordinal zero as a sentinel and returns the final item of the initially loaded ordinal-sorted run list (`internal/cli/cmd_logs.go:308-315`, `internal/cli/cmd_logs.go:334-350`).
- [V] The command resolves the target run and its log path once, before it starts following. It deliberately closes the database before the long-running follow and does not reopen it within that path (`internal/cli/cmd_logs.go:213-226`, `internal/cli/cmd_logs.go:282-315`).
- [V] The follow state is therefore a fixed path, retained read handle, buffered bytes, and an initial `finished` snapshot derived from the selected row’s status (`internal/cli/cmd_logs.go:644-657`).
- [V] The tail loop drains its retained handle, waits on a 250 ms ticker or cancellation, and exits on an observed terminal event or when the row was already non-running at invocation. It neither refreshes status nor discovers new runs (`internal/cli/cmd_logs.go:668-710`, `internal/cli/cmd_logs.go:713-745`).

## Domain / Problem Space

### Problem 1 — issue-level follow is conclusively pinned

- [O] The owner reported that a follow begun for issue #50 on ordinal 4 showed none of subsequently created ordinals 5–16.
- [V] This follows directly from the current control flow: omitted `--run` selects the final member of one initial list, and the selected log path is fixed before the follow begins (`internal/cli/cmd_logs.go:213-226`, `internal/cli/cmd_logs.go:334-350`). No loop-side database query can see an ordinal created later (`internal/cli/cmd_logs.go:668-710`).
- [V] Therefore the existing command has the same pin-to-one-run behavior for both issue-level and explicitly ordinal-pinned invocations. The distinction proposed by the issue does not presently exist.

### Problem 2 — terminal `turn.failed` non-termination is reported but not root-caused

- [O] The owner observed an open follower still blocked approximately 25 hours later on the same inode after run 4’s well-formed newline-terminated final `turn.failed` record, ruling out rotation, recreation, and an initially completed row.
- [V] All three terminal types are defined as `result`, `turn.completed`, and `turn.failed` (`internal/cli/cmd_logs.go:40-48`). The raw-follow sink parses and recognizes those types, including after filtering (`internal/cli/cmd_logs_render.go:185-201`, `internal/cli/cmd_logs.go:532-553`). The rendered sink marks the same event types done before rendering/filter dispatch (`internal/cli/cmd_logs_render.go:302-321`, `internal/cli/cmd_logs_render.go:362-365`).
- [V] The drain code repeatedly invokes `Read` until EOF, preserves incomplete lines, and routes complete newline-delimited records through the terminal-aware sink (`internal/cli/cmd_logs.go:713-745`, `internal/cli/cmd_logs.go:760-781`). This is consistent with the owner’s expectation that an appended complete event should terminate the follower.
- [V] Existing tests already prove a retained handle can process appended bytes after a prior drain/EOF when a partial line is later completed; those cases terminate with `result` (`internal/cli/cmd_logs_test.go:1147-1218`, `internal/cli/cmd_logs_render_test.go:528-587`).
- [V] Static evidence does not establish why this particular live follow failed to exit. The implementation appears to reach the ticker loop and to recognize `turn.failed` if it reads the event. Root cause remains open pending a deterministic reproduction or instrumentation.

### Independent stranded-follower failure

- [V] A run that becomes terminal in the database after follow begins cannot end the follower unless a terminal event arrives, because status is only captured at invocation (`internal/cli/cmd_logs.go:644-657`, `internal/cli/cmd_logs.go:695-700`).
- [V] Startup reconciliation can eventually mark abandoned running rows failed, but it is a daemon-startup operation rather than a signal observed by a live follower (`internal/store/runs.go:460-504`, `internal/daemon/daemon.go:415-440`). A killed writer can consequently leave a live follower waiting indefinitely under the current model.

## Constraints & Risks

- [V] The log command currently avoids retaining a SQLite connection across a long follow (`internal/cli/cmd_logs.go:282-315`). Any status-refresh design must preserve this resource/concurrency intent and use the store through its existing ownership boundary.
- [V] Runs use an ordered ordinal and append-only JSONL path per run, so issue-level rollover needs an explicit state transition between a completed ordinal and a newly discovered ordinal; it cannot emerge from a single open file handle (`internal/store/runs.go:69-120`, `internal/cli/cmd_logs.go:213-226`).
- [V] The terminal-event paths are shared but not identical between raw and rendered output. Regression coverage must prove both modes continue to terminate correctly if behavior is refactored (`internal/cli/cmd_logs_render.go:185-201`, `internal/cli/cmd_logs_render.go:302-321`).
- [V] Follow tests share `LF_HOME` and are deliberately not parallel; new asynchronous tests should retain the current writer/follower handshakes and deadlines rather than use timing-only sleeps (`internal/cli/cmd_logs_test.go:25-32`, `internal/cli/cmd_logs_test.go:1042-1072`).
- [O] A blocked file notification/read state looks like a healthy idle tail to users. The design should make mode-specific lifecycle behavior clear, particularly during ordinal transitions.

## Open Questions

1. [V] What exact raw/render mode and filters were active during the reported `turn.failed` incident? Both paths look terminal-aware statically, but reproduction should cover the actual invocation shape.
2. [V] Can a minimal real-file test append each terminal type only after the follower has demonstrably completed an initial EOF drain and reproduce the `turn.failed` failure? Existing after-EOF coverage uses only `result`.
3. [V] At what cadence and by what store accessor should a running follow observe terminal database state, while preserving the command’s short-lived database-access model?
4. [V] For issue-level following, should a completed current run wait indefinitely for the next ordinal, or is there a bounded/interruptible lifecycle policy? The issue asks to continue into the next run but does not define the no-next-run end condition.
5. [V] Should a terminal database state take precedence immediately over unread final bytes, or should the command make one final drain before ending so it does not omit an event written shortly before status update?
6. [V] What exact separator text and raw/render placement best make ordinal rollover visible without corrupting raw JSONL expectations? The issue requires a visible separator but does not specify format or whether raw output remains pure JSONL.

## Recommended scope for Plan mode

Plan one cohesive logs-follow behavior change, with these boundaries:

1. Preserve explicit `--run N -f` as a pin to one ordinal, while making an issue-level follow re-query and transition to newer ordinals after its current run becomes terminal.
2. Define a visible, mode-appropriate transition indicator and a clear cancellation/no-next-run policy for issue-level waits.
3. Add database-status observation during an active follow so a writer that dies without a terminal log event cannot strand the command. Specify final-drain ordering.
4. Add deterministic real-file regression tests that force an initial EOF before appending `result`, `turn.completed`, and `turn.failed`; cover raw and rendered behavior as appropriate, the database-terminal-without-event case, explicit ordinal pinning, issue ordinal rollover, and transition visibility.
5. Treat the reported `turn.failed` hang as an investigation target, not a proven code defect with a claimed root cause. Do not broaden scope into daemon restart reconciliation or log rotation unless reproduction demonstrates a need.
