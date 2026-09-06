# US-2 — `-f` follow on the app log

> As an operator, I want `litefactory logs -f` to follow that log as it grows so that I can watch the daemon work live.

This story makes `-f` keep printing new app-log records as the daemon writes them, indefinitely, until the operator stops it.

## Why This Matters

An operator watching the daemon work in real time needs the view to behave like tailing any other live log — new lines keep appearing, and it doesn't stop just because a line happens to read like something finished (the daemon writes plenty of records that look like a completed task without being one). It also has to keep working if the log file gets replaced out from under it, since nothing in litefactory currently rotates or caps this file — that's a known, already-accepted gap elsewhere in the system, and this story's job is to not fall over because of it, not to fix it.

## Architecture Notes

- Pattern to follow: `tailLog`'s technique — a held `io.ReadSeeker` with an explicit offset, `followInterval` (250ms) between polls, `followReadSize` (64 KiB) reads, and a `pending` buffer for a partial trailing line. Reuse the technique, not necessarily the function.
- Platform: no goroutine. The loop runs synchronously on the command's own goroutine and selects on `ctx.Done()`; `context.Background()` is banned outside `main`.
- Risk: truncation. Preferred: a separate follow loop that shares the polling constants and the `pending` technique with `tailLog` but owns its own stat-based size check, re-opening at offset 0 when the file shrinks — this leaves `tailLog`'s own doc comment (which assumes the file it follows is append-only and never rotated) true and untouched. If `tailLog` itself is extended instead, its doc comment must be corrected in the same change.
- The app-log source has no terminal event: a record whose `msg` is `result` must not stop the follow — this differs from the issue-log sinks, which do stop on that condition.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Implement the app-log follow loop: synchronous, `ctx`-aware, reusing the `followInterval`/`followReadSize`/`pending` technique | code | US-1's `cmd_logs_app.go`, US-9's computed tail offset | 4h |
| T-2.2 | Add stat-based truncation detection: reopen at offset 0 when the file shrinks | code | T-2.1 | 2h |
| T-2.3 | Wire `-f` to the app-log path | code | T-2.1 | 1h |
| T-2.4 | Add any new follow-path file to `logsSourceFiles` | test | T-2.1 | 0.5h |
| T-2.5 | Tests: streaming within one poll interval and exit 0 on cancellation; no stop on `msg=result`; waits for a not-yet-existing file; resumes after truncation | test | T-2.1, T-2.2, T-2.3 | 3h |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | `litefactory logs -f` streams records appended after the command started, within one poll interval, and exits 0 on context cancellation |
| AC-2.2 | `litefactory logs -f` does not stop when a record with `msg=result` is appended |
| AC-2.3 | `litefactory logs -f` against a not-yet-existing `daemon.log` waits and starts streaming once the file appears |
| AC-2.4 | `litefactory logs -f` resumes correctly after the file is truncated beneath it |
| AC-2.5 | No goroutine, channel, `fsnotify`, `net/rpc`, or `os/signal` import is introduced by the follow implementation |
