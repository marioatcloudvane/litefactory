# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-1

Request counters are logged per tick and per worker at INFO, so they fire several times a minute.

Evidence:

- `internal/daemon/discovery_budget.go:241`

## F-2

Discovery pause is silent; only quota_fresh=false hints at it.

Evidence:

- `internal/daemon/discovery_budget.go:114`

## F-3

A deferred tick does not update last tick, so status freezes.

Evidence:

- `internal/daemon/tick.go:214`

## F-4

daemon status reads only last tick from the poll cursor; no paused state, last attempt or in-flight count is stored.

Evidence:

- `internal/store/pollcursor.go:22`

## F-5

Per-comment decisions are scattered across INFO and DEBUG lines; none states the outcome in one line.

Evidence:

- `internal/daemon/tick.go:972`

## F-6

A rate-limit refusal logs WARN but the backoff-skip tick logs DEBUG only.

Evidence:

- `internal/daemon/tick.go:159`

## F-7

litefactory logs already exists with tail and level filter.

Evidence:

- `internal/cli/cmd_logs.go`

## F-8

An existing test asserts the 'tick complete' INFO record and its alias fields, so those names must stay.

Evidence:

- `internal/daemon/quiet_polling_test.go:62`

## F-20

The daemon writes its own JSONL daemon.log in the logs folder, opened append-only through a file sink; launchd output goes to separate launchd.out.log and launchd.err.log. daemon status reads only the poll-cursor last-tick time from the database.

Evidence:

- `internal/config/paths.go:106`
- `internal/logging/filesink.go:63`
- `internal/cli/cmd_daemon_agent.go:187`

## F-21

On the owner's machine daemon.log is about 99 MB, unrotated since 25 August (roughly 2 to 3 MB a day on average). Size taken from a directory listing; line counts were not read.

_No source reference was recorded for this finding._

## F-22

The daemon log path is used by the file sink at startup, by litefactory logs (tail and read) and by the doctor check, so per-day files touch all three.

Evidence:

- `cmd/litefactory/main.go:111`
- `internal/cli/cmd_logs_app.go:85`
- `internal/doctor/files.go:87`
