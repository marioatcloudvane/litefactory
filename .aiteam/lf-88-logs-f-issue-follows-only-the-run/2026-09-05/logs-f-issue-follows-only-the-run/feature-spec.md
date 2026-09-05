# Feature Spec: logs-f-issue-follows-only-the-run
# Status: APPROVED
# Date: 2026-09-05

## Goal

- Make `litefactory logs -f <issue>` continue across successive runs for the issue, while guaranteeing that an explicit run follow and a terminal or abandoned writer cannot leave a follower stranded.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an issue observer, I want `logs -f <issue>` to move from a completed run to the next run created for that issue so that one follower remains useful across repeated turns. |
| US-2 | As an issue observer, I want the follower to state when it starts following a new ordinal so that output from separate runs is unambiguous. |
| US-3 | As a user following one chosen ordinal, I want `logs -f <issue> --run N` to stop when that run ends so that explicit selection remains pinned. |
| US-4 | As a user following an active run, I want the command to finish when the run becomes terminal in the database even if its writer never emits a terminal event so that an interrupted writer cannot leave the command waiting forever. |

## Data Model

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Run | ID, SessionID, Ordinal, Agent, Status, LogPath | existing persisted fields | yes | Use the existing ordinal-sorted session runs, agent discriminator, lifecycle status, and recorded/derived log path; no schema change. |
| Agent configuration | `agents.claude.timeout_seconds` / `agents.codex.timeout_seconds` | existing integer seconds | yes | Select the timeout for the agent recorded on the completed run; values remain subject to existing configuration validation. |

### Relationships

- Session → Run via `Run.SessionID`; ordinal identifies each run within its session.
- Run → agent configuration via `Run.Agent`; the matching configured agent timeout bounds the issue-level wait for a successor.

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| CLI | `litefactory logs -f <issue>` without `--run` | local CLI access | Follow the initially newest run, then discover and follow newer ordinals for the same resolved session. |
| CLI | `litefactory logs -f <issue> --run N` | local CLI access | Follow only ordinal `N`; do not discover or enter later runs. |
| CLI output | Rendered issue-level rollover | local CLI access | Emit an unfiltered visible separator naming the newly followed zero-padded ordinal before its events. |
| CLI output | Raw issue-level rollover | local CLI access | Keep stdout exclusively concatenated source JSONL bytes; emit the visible rollover separator to stderr. |

## Business Rules

- Treat omitted `--run` and explicit `--run N` as distinct follow lifecycles only when `-f` is set; non-follow selection behavior remains unchanged.
- For an issue-level follow, drain the current log through the terminal event when present; after the current run is terminal, re-query the resolved session for an ordinal greater than the just-followed ordinal.
- Follow the earliest available newer ordinal, announce that ordinal, and repeat the lifecycle for each later ordinal.
- When no newer ordinal exists, wait no longer than the configured hard timeout for the agent recorded on the completed run; exit successfully if no successor is created before that deadline.
- Use the existing 250 ms follow polling cadence for log and database observation; do not introduce daemon IPC or a filesystem watcher.
- Reopen and close store access for refreshes rather than retaining a SQLite connection during the long-running follow.
- During an active run, observe its current database status. On a terminal database status, perform one final log drain before ending or transitioning, so log bytes written immediately before the status update are not omitted.
- A terminal event is any of `result`, `turn.completed`, or `turn.failed`; each ends an explicitly pinned follow after its event is emitted.
- A terminal database state without a terminal event ends an explicit follow after the final drain; for issue-level follow, it begins the bounded successor wait.
- Rendered separators are not subject to `--only`, `--grep`, `-i`, or `--invert`; raw separators never enter stdout.
- Cancelled context ends any follow successfully and immediately; it does not wait for a successor deadline.

## Edge Cases & Error Handling

- Current running run has no log file yet → continue polling until the file appears, the database becomes terminal, the successor-wait lifecycle applies, or the context is cancelled.
- Current run was terminal when invocation began → drain its existing log once, then: explicit `--run` exits; issue-level follow waits for a newer ordinal for that run's configured timeout.
- Terminal database state arrives before a final JSONL terminal record → make one final drain; retain a newline-terminated final event if present, then apply terminal handling.
- Writer leaves a partial final line → preserve current behavior: do not emit it while an active run may append; flush it only once database terminal handling proves no further bytes are expected.
- Later run is allocated before it has a log file → enter it and wait for its file/events under the normal active-follow lifecycle.
- Multiple newer runs exist at refresh → follow them in ascending ordinal order, emitting one separator before each entered run.
- Store refresh or log open/read failure other than an expected missing active-run file → return a non-zero actionable error; do not silently treat it as completion.
- Configured agent for a historical run is unavailable or unrecognized → return a non-zero actionable configuration error rather than choosing an unrelated timeout.

## Out of Scope

- Changing daemon timeout, process-kill, startup-reconciliation, or run-allocation behavior.
- Adding database migrations or persistent follower state.
- Log rotation/recreation support or a filesystem watcher.
- Claiming a root cause for the reported historical `turn.failed` hang without deterministic reproduction evidence.
- Changing non-follow `logs`, `logs --runs`, log rendering, filtering, redaction, or raw byte semantics except for the documented follow rollover separator destination.

## Acceptance Criteria

- [ ] `logs -f <issue>` without `--run` follows the newest initial run, then follows each subsequently created higher ordinal for the same session in ascending order.
- [ ] Each issue-level rendered rollover prints a visible separator that identifies the new zero-padded ordinal before that run's events.
- [ ] Each issue-level raw rollover keeps stdout as the exact concatenation of source JSONL bytes and prints the ordinal separator on stderr.
- [ ] `logs -f <issue> --run N` never opens or emits a later ordinal and exits after ordinal `N` reaches terminal handling.
- [ ] After a completed issue-level run has no newer ordinal, the follower exits successfully when the timeout configured for that run's recorded agent elapses; creation of a newer ordinal before that deadline is followed.
- [ ] The active-run follower checks current database status without holding one database connection for the duration of the follow.
- [ ] If a followed run becomes database-terminal without a terminal event, the follower makes a final log drain and then exits (explicit run) or waits for a successor (issue-level run).
- [ ] Deterministic real-file tests prove that a follower that has already drained EOF exits when each newline-terminated terminal type (`result`, `turn.completed`, `turn.failed`) is appended, in both raw and rendered follow paths where applicable.
- [ ] Tests cover terminal-event emission after an initial EOF, terminal database status without a terminal event, explicit-run pinning, multi-ordinal issue rollover, rendered/raw separator behavior, successor timeout expiry, cancellation, and a later run whose log file is not yet present.
