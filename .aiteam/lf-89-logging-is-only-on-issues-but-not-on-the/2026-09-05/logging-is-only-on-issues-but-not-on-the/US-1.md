# US-1 — Bare `litefactory logs` shows litefactory's own log

> As an operator, I want `litefactory logs` with no arguments to print litefactory's own log so that I can see the ticks without picking an issue first.

This story makes `litefactory logs` with zero arguments route to a new app-log view instead of failing with a usage error.

## Why This Matters

Today, typing `litefactory logs` with no issue number in hand — just to check on the tool itself — produces an error asking you to supply one. There is no way to see what litefactory has been doing without already knowing an issue exists. This story fixes that by routing the bare command to litefactory's own record of its activity, which is the most literal reading of the original complaint. What is worth knowing going in: this is a breaking change to today's contract — bare `litefactory logs` moves from a usage error (exit 2) to a real, successful result (exit 0) — accepted deliberately rather than discovered as a surprise later.

## Architecture Notes

- Pattern to follow: the existing command-construction shape in `newLogsCommand()` (`internal/cli/cmd_logs.go`). `Args` becomes `ArgSpec{Names: []string{"issue"}, Min: 0}`; the routing decision (`len(args) == 0`) goes inside the closure that currently ends with an unconditional `args[0]`.
- Platform: the `Min: 0` change and the `len(args) == 0` guard must land in the same change — flipping one without the other turns bare `litefactory logs` into a panic, not a usage error.
- Risk: flag-applicability errors must be symmetric and must name the offending flag — an issue-only flag given with no issue, and an app-log-only flag given with an issue, are both usage errors, exit 2.
- New file `internal/cli/cmd_logs_app.go` owns path resolution (`config.Paths.DaemonLogPath()` only — never a raw `filepath.Join` against `$LF_HOME`), the missing-file message, and the updated "issue must be a positive number" error text. It must be added to `logsSourceFiles` in `cmd_logs_test.go` as part of this story, not a follow-up.
- The app-log view opens no SQLite database and requires no prior `litefactory init` — it reads exactly one file.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Flip `ArgSpec.Min` to 0 and add the `len(args) == 0` branch in one change | code | — | 2h |
| T-1.2 | Create `cmd_logs_app.go`: resolve `DaemonLogPath()`, detect a missing file, route into the app-log flow | code | T-1.1 | 3h |
| T-1.3 | Update the "issue must be a positive number" error message to also mention that bare `litefactory logs` shows litefactory's own log | code | T-1.1 | 0.5h |
| T-1.4 | Add flag-applicability checks: issue-only flags error (naming the flag) with no issue given; app-log-only flags error (naming the flag) with an issue given | code | T-1.1 | 2h |
| T-1.5 | Add `cmd_logs_app.go` to `logsSourceFiles` in `cmd_logs_test.go` | test | T-1.2 | 0.5h |
| T-1.6 | Tests: bare `logs` exits 0, no SQLite/init precondition required, `logs daemon` still exits 2 with the updated message | test | T-1.1, T-1.2, T-1.3 | 2h |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | `litefactory logs` with no arguments exits 0 and prints app-log lines; it prints no usage block and no command help |
| AC-1.2 | `litefactory logs` succeeds against a `$LF_HOME` that has a `daemon.log` but no SQLite database (no `init` precondition error) |
| AC-1.3 | `litefactory logs daemon` exits 2 with a message that both rejects `daemon` as an issue and mentions that bare `litefactory logs` shows litefactory's own log |
| AC-1.4 | `litefactory logs --run 2` with no issue exits 2 and names `--run`; `litefactory logs 123 --level debug` exits 2 and names `--level` |
| AC-1.5 | `litefactory logs` with no `daemon.log` prints a sentence naming the path and exits 0 |
| AC-1.6 | `cmd_logs_app.go` is listed in `logsSourceFiles` and is covered by `TestLogsNeverImportsRedact` and `TestLogsUsesNoDaemonIPC` |
