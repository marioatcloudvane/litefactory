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
