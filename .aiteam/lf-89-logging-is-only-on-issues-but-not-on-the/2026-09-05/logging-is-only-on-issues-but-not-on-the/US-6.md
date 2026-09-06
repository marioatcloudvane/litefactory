# US-6 — `--level` as a write-time flag on `daemon run`/`daemon start`

> As an operator, I want `litefactory daemon run --level debug` and `litefactory daemon start --level debug` to run that daemon at a chosen verbosity so that I can get debug detail without hand-editing `config.yaml`.

This story adds a `--level` flag to `daemon run` and `daemon start` that overrides the configured log level for that process only.

## Why This Matters

The only way today to get the daemon logging at debug detail is to hand-edit `config.yaml` and restart — awkward for a one-off troubleshooting session, and easy to forget to revert. This gives a way to ask for more detail for a single run, or a single launchd-managed start, without touching the saved configuration — closing the loop with the display-side filter delivered in US-5. Worked through with the architect rather than left as a surprise: litefactory builds its logger before it even knows which command was typed, so honoring this flag on `daemon run` specifically requires one narrow, explicitly-scoped part of the program's own entry point to look at the command line earlier than it normally would — a deliberate, documented exception, not a precedent for parsing flags outside the command layer generally.

## Architecture Notes

- **This story carries a recorded architectural deviation (DEV-1 in `ARCH_REVIEW.md`).** Its `daemon start` half is independent and clean; its `daemon run` half is the part affected.
- `daemon start` half: `daemonProgram()` in `cmd_daemon_agent.go` appends `"--level", <keyword>` to the program slice when the flag is set. `internal/launchd`'s `Manager.NewAgent`, `Agent.Validate`, and `Agent.Render` are unmodified — `Agent.Validate()`'s existing `checkPlistValue` pass already covers every `ProgramArguments` element, and `logging.ParseLevel` already constrains the value to four literal keywords before it reaches that point, so no new sanitisation is added. `writeStartReport` already prints the joined `ProgramArguments`, which already satisfies "reports the effective level in its summary" — do not add a new output row for this.
- `daemon run` half implements the approved resolution to DEV-1: bind `--level` normally on `newDaemonRunCommand`'s flag set (today its `New: func(*flag.FlagSet) Handler` discards the parameter and binds nothing), so help text and unknown-flag handling behave correctly; the handler itself ignores the captured value. Separately, `cmd/litefactory/main.go` calls one new, narrow, named helper in `internal/cli` (e.g. `cli.LogLevelOverride(args []string) (slog.Level, bool, error)`) to resolve the override before `logging.New` runs, used in place of `configuredLevel()` when the flag is present. Add a comment at both sites (the inert handler copy, and the helper) explaining why the flag is read in two places. Record this resolution in `.aiteam/AC_AMENDMENTS.md` as part of this story — that record belongs with the code change taking the deviation.
- The override sits on top of `configuredLevel()`'s existing silent-fallback-to-info behavior: an explicit `--level` beats config, config beats the default; an unparseable `--level` is a usage error, exit 2, before the poll loop starts — never another silent fallback.
- `daemon.Setup{Logger: env.Logger}` is what actually consumes the resolved level; nothing in `internal/daemon` changes.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | Append `--level <keyword>` to `daemonProgram()`'s slice in `cmd_daemon_agent.go` when the flag is set | code | — | 1h |
| T-6.2 | Bind `--level` on `newDaemonRunCommand`'s flag set (name the currently-discarded flag-set parameter); handler ignores the captured value, with an explanatory comment | code | — | 1h |
| T-6.3 | Implement `cli.LogLevelOverride(args)` helper, resolving `--level` from argv via `logging.ParseLevel` | code | T-6.2 | 2h |
| T-6.4 | Call the helper from `cmd/litefactory/main.go`, feeding `logging.New` in place of `configuredLevel()` when the override is present | code | T-6.3 | 2h |
| T-6.5 | Record the DEV-1 resolution in `.aiteam/AC_AMENDMENTS.md` | docs | T-6.2, T-6.3, T-6.4 | 0.5h |
| T-6.6 | Tests: `daemon run --level debug` emits DEBUG records to `daemon.log` and console without modifying `config.yaml`; `daemon run --level loud` exits 2 before the poll loop starts; `daemon start --level debug` produces a plist whose `ProgramArguments` end with `daemon run --level debug` and reports the effective level in its summary | test | T-6.1, T-6.2, T-6.3, T-6.4 | 3h |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-6.1 | `litefactory daemon run --level debug` emits DEBUG records to `daemon.log` and to the console when attached to a terminal, with `log_level: info` still in `config.yaml`, and `config.yaml` is not modified |
| AC-6.2 | `litefactory daemon run --level loud` exits 2 before starting the poll loop |
| AC-6.3 | `litefactory daemon start --level debug` writes a plist whose `ProgramArguments` end with `daemon run --level debug`, and reports the effective level in its summary output |
| AC-6.4 | The DEV-1 resolution is recorded in `.aiteam/AC_AMENDMENTS.md` |
