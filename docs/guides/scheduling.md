# Scheduling

[Documentation](../README.md) · [Using the factory](README.md)

Use `/schedule` to ask the factory to do owner work later on a registered issue. The daemon must be running when the work is due. Busy workers, one-run-per-repository rules, polling and GitHub rate limits can delay the actual start.

```text
/schedule -by 00:30:00 /research Check the current retry behavior.
```

You get an acknowledgment from the factory first and a separate run when the work is due. `/schedule` never starts the work straight away.

## Forms

- `-by HH:MM:SS <instruction>` runs the instruction after a delay from when the factory received the comment.
- `-to MM/DD/YYYY HH:MM:SS [zone] <instruction>` runs it at a clock time. Without a zone, the daemon's local time zone is used. Give `UTC` or an IANA name such as `Europe/Vienna` to be exact. Times are stored in UTC. A time that does not exist or happens twice around a daylight-saving change is refused, not guessed.
- `--after <id[,id...]> -by HH:MM:SS <instruction>` waits until the listed schedules have completed successfully, then starts the delay. The numbers are schedule IDs, not issue numbers. A failed schedule does not count as completed.
- `--propose <instruction>` asks the factory to suggest a schedule. Read the proposal, then send `--accept <proposal-id>` or `--reject <proposal-id>`. A proposal contains 1 to 16 items, a pending proposal expires after seven days, and a newer proposal for the same issue replaces an older pending one. Rejecting cannot cancel work you already accepted.

Rules: delays use two-digit hours, minutes and seconds, must be positive, and minutes and seconds are at most 59. Every request needs an instruction. A `/schedule` inside a `/schedule` is refused. Bad dates, zones, IDs or stale proposals get a refusal reply; correct them in a new comment.

If a due time passes without a run, check that the daemon is running and look for pending work and warnings (see [running the daemon](../operations/running-the-daemon.md)).

The exact syntax is in [owner commands](owner-commands.md). See also [recovery](../operations/recovery.md): when automatic recovery after a provider session limit is on, the factory can create a schedule for you.
