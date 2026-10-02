# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

No webhook or HTTP listener code exists in the repository; discovery is timer polling only.

Evidence:

- `internal/daemon/daemon.go:804`

## f2

Poll interval is poll_interval_seconds, default 30 s, valid range 10-60 s, validated in config and re-checked in daemon Open. The requested 20-180 minute range conflicts with these bounds and unit.

Evidence:

- `internal/config/validate.go:20`
- `internal/daemon/setup.go:139`

## f3

The daemon loop consumes ticks from a channel (injectable) and calls Tick; a webhook can feed the same channel so discovery stays the single source of truth.

Evidence:

- `internal/daemon/daemon.go:774`

## f4

A secrets resolver with keychain support exists and is used for the bot token, reusable for a webhook secret.

Evidence:

- `internal/secrets/secrets_test.go:65`
