# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-1

Request counters are logged per tick and per worker. discoverySummary logs 'discovery request summary' at INFO with 20+ counters, deferred in Tick (scope tick) and in work (scope worker), so it fires several times a minute. Source: internal/daemon/discovery_budget.go:236; internal/daemon/tick.go:134,1177

_No source reference was recorded for this finding._

## F-2

Discovery pause is silent. acquire denies non-probe requests when freshLocked() is false and returns ErrDiscoveryDeferred. Fresh needs a limit, a future reset, and an observation under 2 minutes old. No transition is logged; only quota_fresh=false hints at it. Source: internal/daemon/discovery_budget.go:86,114-172

_No source reference was recorded for this finding._

## F-3

A deferred tick does not update last tick. Tick returns before SetPollCursor, so LastTickAt freezes and 'tick complete' is not logged. Source: internal/daemon/tick.go:214 and the early return before SetPollCursor

_No source reference was recorded for this finding._

## F-4

daemon status shows only launchd state and last tick from the poll cursor. No paused state, last attempt or in-flight count; the daemon persists none of these. Source: internal/cli/cmd_daemon_agent.go:127-150; internal/store/pollcursor.go:12-24

_No source reference was recorded for this finding._

## F-5

Per-comment decisions are scattered across INFO and DEBUG logs (no free slot, first sight, refused runs, comment asks for nothing). No single line per owner comment states its outcome. Source: internal/daemon/tick.go:972-987,1329,1442,1675,1796,2026

_No source reference was recorded for this finding._

## F-6

A GitHub rate-limit refusal logs a WARN, but the backoff-skip tick logs only at DEBUG. This is separate from discovery budget deferral. Source: internal/daemon/tick.go:159,2322

_No source reference was recorded for this finding._

## F-7

litefactory logs already exists with tail and level filters. Source: internal/cli/cmd_logs.go, cmd_logs_app.go, cmd_logs_tail.go

_No source reference was recorded for this finding._
