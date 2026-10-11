# Scheduling

Schedule an owner instruction for later eligibility on a registered issue. The daemon must be running when it can dispatch; busy workers, repository serialization, polling and remote backoff can delay the actual start.

```text
/schedule -by 00:30:00 /research Check the current retry behavior.
```

Expect a factory scheduling acknowledgment, then a separate run when eligible. `/schedule` itself does not start the requested work immediately.

Use `-to MM/DD/YYYY HH:MM:SS [timezone] <instruction>` for a civil time. An omitted zone uses the daemon's local timezone; use `UTC` or an IANA name such as `Europe/Vienna` to be explicit. Stored due times are UTC. Skipped or repeated local times around daylight-saving transitions are refused rather than guessed.

`--after <schedule-id[,schedule-id...]> -by HH:MM:SS <instruction>` waits for prerequisite schedules to complete successfully, then starts its delay. These numbers are schedule IDs, not issue numbers. A failed prerequisite does not establish completion. IDs must be positive and unique.

`--propose <instruction>` requests a scheduling proposal; review the reported proposal before sending `--accept <proposal-id>` or `--reject <proposal-id>`. An accepted proposal supplies its reviewed schedule; a proposal alone is not a dispatch promise. This requires a proposal generator; the current CLI daemon wiring does not install one, so `--propose` reports proposal failure there. Where a generator is installed, proposals contain 1–16 items, pending proposals older than seven days expire, and new advice supersedes older pending advice for the same issue/owner. Rejection cannot cancel already accepted work.

Durations use exactly two-digit hours/minutes/seconds, must be positive, and minutes/seconds are at most 59. Every work request needs a payload. Nested `/schedule` payloads are refused. Invalid dates, zones, dependencies or stale/ineligible proposal decisions produce refusal feedback; correct them in a new comment. Check pending work and daemon health if a due time passes without a run.

See the [complete syntax](issue-command-reference.md), [daemon operations](../operations/running-the-daemon.md) and [recovery](../operations/recovery.md).

[Using the factory](README.md)
