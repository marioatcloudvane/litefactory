# Feature Spec: App-level log view (`litefactory logs` with no issue)
# Status: APPROVED
# Date: 2026-09-05

## Goal
Give the operator a way to read and follow litefactory's own daemon log — ticks, dispatch, backoff — from `litefactory logs` with no issue argument, rendered exactly as a foreground `daemon run` prints it, with a level keyword to control both what is shown and what a fresh daemon writes.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an operator, I want `litefactory logs` with no arguments to print litefactory's own log so that I can see the ticks without picking an issue first |
| US-2 | As an operator, I want `litefactory logs -f` to follow that log as it grows so that I can watch the daemon work live |
| US-3 | As an operator, I want that output to look exactly like what `litefactory daemon run` prints on my terminal so that I do not have to learn a second format or read JSON |
| US-4 | As an operator, I want bare `litefactory logs` to stop printing a usage block, with the command's own help still reachable at `litefactory logs --help` so that discoverability is not lost |
| US-5 | As an operator, I want `litefactory logs --level warn` to show only records at or above a level keyword so that I can cut the firehose down |
| US-6 | As an operator, I want `litefactory daemon run --level debug` and `litefactory daemon start --level debug` to run that daemon at a chosen verbosity so that I can get debug detail without hand-editing `config.yaml` |
| US-7 | As an operator, I want `--grep`/`-i`/`--invert` on the app log so that filtering works the same way it already does for issue logs |
| US-8 | As an operator, I want to be told when I ask for a level finer than the daemon was configured to write so that I do not read an empty result as "nothing happened" |
| US-9 | As an operator, I want the default view to show the tail rather than the whole unrotated file so that a months-old `daemon.log` does not flood my terminal |

## Data Model

No database entity, no migration. The model is the on-disk record in `$LF_HOME/logs/daemon.log`, written by `slog.NewJSONHandler` (`internal/logging/logging.go:44-45`), one JSON object per line.

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| AppLogRecord | `time` | string | yes | RFC3339 with nanoseconds, as stdlib `slog.JSONHandler` writes it |
| AppLogRecord | `level` | string | yes | one of `DEBUG`, `INFO`, `WARN`, `ERROR` (stdlib `slog.Level` text form) |
| AppLogRecord | `msg` | string | yes | free text, e.g. `tick complete` |
| AppLogRecord | `version` | string | yes | attached by `cmd/litefactory/main.go:69` via `.With(slog.String("version", …))` |
| AppLogRecord | *(further attrs)* | scalar or object | no | arbitrary key/value pairs; a `WithGroup` group is a **nested JSON object** (e.g. `"git":{"branch":"main"}`) |
| AppLogFile | path | string | yes | `config.Paths.DaemonLogPath()` = `$LF_HOME/logs/daemon.log`; 0600 via process umask; append-only, **never rotated** |
| RenderedLine | text | string | yes | `slog.TextHandler`-equivalent rendering of one AppLogRecord (see Business Rules) |
| LevelFilter | value | string | no | `debug`\|`info`\|`warn`\|`error`, case-insensitive, parsed by the existing `logging.ParseLevel` |

### Relationships
- AppLogFile → AppLogRecord via one JSON object per `\n`-terminated line
- AppLogRecord → RenderedLine via the JSON→text re-renderer (new)
- AppLogRecord → LevelFilter via `level`, compared with `>=`
- `daemon run --level` / `daemon start --level` → `logging.Options.Level` (`internal/logging/logging.go:38`), overriding `Settings.LogLevel` from `config.yaml` for that process only
- `daemon start --level X` → the launchd plist's `ProgramArguments` (`internal/launchd/plist.go:107`), appended after `daemon run`

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| CLI | `litefactory logs` (no positional) | local operator; no auth | **New behavior.** Prints the last N app-log records rendered as text, exit 0. Today this is a usage error, exit 2 |
| CLI | `litefactory logs -f` | local operator | Prints the tail, then follows `daemon.log` until Ctrl-C; exit 0 on cancellation |
| CLI | `litefactory logs --level <keyword>` | local operator | Display-time filter, app-log view only. `debug\|info\|warn\|error` |
| CLI | `litefactory logs -n <count>` | local operator | How many trailing records to print before following. Default 200 |
| CLI | `litefactory logs --all` | local operator | Print the whole file instead of the tail; mutually exclusive with `-n` |
| CLI | `litefactory logs --raw` (no positional) | local operator | The selected `daemon.log` lines byte for byte (JSON), no re-rendering |
| CLI | `litefactory logs --grep <re>` / `-i` / `--invert` | local operator | Same `lineFilter` semantics as today, applied to the rendered (or raw) line |
| CLI | `litefactory logs <issue> …` | local operator | **Unchanged.** Every existing flag, exit code and output byte stays as it is |
| CLI | `litefactory logs --help` / `-h` | — | **Unchanged and already wired** (`parse.go:63` → `errHelpRequested` → `router.go:80-83`), prints to stdout, exit 0 |
| CLI | `litefactory help`, `--help`, bare `litefactory` | — | **Unchanged.** Still `writeGlobalUsage` (`router.go:60-71`) |
| CLI | `litefactory daemon run --level <keyword>` | local operator | New flag. Overrides `Settings.LogLevel` for this process; affects both the console text sink and `daemon.log` |
| CLI | `litefactory daemon start --level <keyword>` | local operator | New flag. Appended to the plist's `ProgramArguments` so the launchd-run daemon starts at that level; echoed in the command's existing summary output |
| Registration | `internal/cli/commands.go` | — | No new command registered; `logs` keeps its single entry (`commands.go:17`) |
| ArgSpec | `newLogsCommand()` | — | `Args: ArgSpec{Names: []string{"issue"}, Min: 0}` (was `Min: 1`) |

## Business Rules

**Command routing**
- `logs` with zero positionals selects the app-log view. `logs` with one positional keeps today's issue/run behavior verbatim.
- The app-log view does **not** open SQLite and does **not** require `litefactory init` to have run. It reads one file. It works with the daemon stopped, and it works before the database exists.
- The app-log view uses no daemon IPC — no socket, no signal, no lock file (AC-5.8 holds unchanged). Following is file polling at the existing 250 ms `followInterval`, reusing the `tailLog` shape (held `io.ReadSeeker` offset, `followReadSize` reads, partial-line `pending` buffer).
- App-log output is **not redacted**, same as issue logs (AC-5.7 / S-7): `$LF_HOME` is 0700 and the operator owns it. Every source file implementing `logs`, including any new file added for this view, must be listed in `logsSourceFiles` (`internal/cli/cmd_logs_test.go:916`) so `TestLogsNeverImportsRedact` covers it.

**Flag applicability**
- App-log-only flags: `--level`, `-n`, `--all`. Supplying any of them together with an issue positional is a usage error, exit 2.
- Issue-only flags: `--run`, `--runs`, `--only`, `--full`, `--repo`. Supplying any of them with no issue positional is a usage error, exit 2, naming the flag.
- Shared flags: `-f`, `--raw`, `--grep`, `-i`, `--invert`.
- `-n` and `--all` together is a usage error, exit 2.
- `-n` must be >= 1; `-n 0` or negative is a usage error, exit 2.

**Rendering — matching the `daemon run` console byte for byte**
- Target format is stock `slog.TextHandler` output, because that is literally what `daemon run` installs (`logging.go:42`) with no `AddSource`, no `ReplaceAttr` and no colorization.
- `time` is re-formatted from the file's RFC3339-nanosecond value to `TextHandler`'s RFC3339-**milliseconds** form, preserving the offset as written.
- Built-in keys are emitted first and in stdlib order: `time=`, `level=`, `msg=`, then the record's own attributes.
- Attribute order on the rendered line is the key order as written in the JSON line. Decoding must preserve that order (a plain `map[string]any` decode loses it and is therefore not acceptable).
- A nested JSON object (produced by `WithGroup`) is flattened to dotted keys: `"git":{"branch":"main"}` renders as `git.branch=main`.
- Values are quoted exactly as `TextHandler` quotes them (whitespace, `=`, quotes, empty string).
- A line that is not valid JSON is emitted verbatim, unparsed, rather than dropped or reported as an error.

**Level**
- `--level` filters records with `record.level >= filter`. Comparison uses the existing `logging.ParseLevel` vocabulary; the value is case-insensitive; an unparseable value is a usage error, exit 2, listing `debug, info, warn, error`.
- A record whose `level` is absent or unrecognised is always shown, never hidden by `--level`.
- `--level` is display-time only. It cannot reveal records the daemon never wrote.
- When `--level` names a level finer than `Settings.LogLevel` in `config.yaml`, one advisory line is written to **stderr** before the output (so stdout stays pipeable), naming the configured level and pointing at `litefactory daemon start --level <keyword>`. It is advice, not an error: exit code is unaffected.
- `daemon run --level` / `daemon start --level` override `Settings.LogLevel` for that process only. Neither writes to `config.yaml`. An unparseable value is a usage error, exit 2, reusing the same vocabulary message.
- The override reaches `logging.New` before the router dispatches, so it governs both sinks. There is no live/mutable level: a running daemon's level cannot be changed.

**Tail and follow**
- Default view is the last 200 records of the file after filtering, oldest first.
- `-f` prints that same tail, then follows. `--all -f` prints the whole file, then follows.
- The app-log follow has **no terminal event**. The `result` / `turn.completed` / `turn.failed` stop conditions used for run logs must not apply here — a `daemon.log` record whose `msg` is `result` must not end the follow.
- A follow ends only on context cancellation (Ctrl-C / SIGTERM), and that is exit 0, not a failure — matching today's `tailLog` behavior.
- `--raw` emits the selected lines byte for byte with no re-rendering; `--grep` still applies line by line when given.

## Edge Cases & Error Handling

- `daemon.log` does not exist, no `-f` → print `no litefactory log at <path> yet` to stdout, exit 0 (mirrors today's `has no runs yet`).
- `daemon.log` does not exist, with `-f` → wait for it to appear and start streaming when it does; do not fail. `FileSink` creates the file lazily on first write (`filesink.go:45-50`), so this is the normal cold-start case.
- `daemon.log` exists but is empty → no output, exit 0 (or wait, under `-f`).
- `$LF_HOME` cannot be resolved (`config.Locate()` fails) → today's error, unchanged.
- `daemon.log` is unreadable (permissions) → report the path and the OS error, exit 1.
- File shrinks below the follower's offset mid-follow (external truncation or a manual `rm`+recreate; nothing in litefactory rotates it) → re-open and resume from offset 0 rather than emitting garbage or spinning.
- Writer is mid-line when a tick reads → the partial line is buffered, not printed, and completed by a later read. On cancellation the partial stays unprinted.
- Line is not JSON, or is JSON that is not an object → emitted verbatim.
- Record has no `msg`, or no `time` → render the fields that are present, in file order; do not fail the whole view for one malformed record.
- Extremely long single line → read via the same 64 KiB-chunk `pending` accumulation used today; no 64 KiB per-line ceiling (`bufio.Scanner` is not used, deliberately).
- The `logs` process's own records appear in the output when `log_level: debug` (every invocation logs `dispatch` at Debug, `main.go:78`). This is one record, not a feedback loop, and is accepted — the app log is a whole-process log, not a daemon-only log.
- `litefactory logs daemon` / `logs foo` → still `issue must be a positive number, got "daemon"`, exit 2; the message additionally states that bare `litefactory logs` shows litefactory's own log.
- Requested `-n 5000` on a file with 12 records → prints all 12, exit 0.
- Launchd-run daemon (`daemon start`) never installs the console sink (stdout is not a tty; `logging.IsTerminal` is false), so `daemon.log` is the only record of what would have printed. The app-log view is therefore the *only* way to see that output — this is the primary case the feature serves.
- Pinned tests that must be updated, not worked around: `internal/cli/router_test.go:95` and `internal/cli/parse_test.go:222` both assert today's `missing argument: <issue>` for bare `logs`.

## Out of Scope
- Log rotation, truncation or retention for `daemon.log` (pre-existing gap; the launchd plist comment at `internal/launchd/plist.go:62` already names it accepted M0 debt). Raise separately.
- Changing a **running** daemon's log level without restarting it (no `slog.LevelVar`, no IPC).
- A global `--log-level` flag usable across every command; the override lives on `daemon run` / `daemon start` only.
- Persisting `--level` back into `config.yaml`.
- Any change to what the daemon logs, or to tick/dispatch logic. `tick.go:177-184` already emits everything needed.
- Any new IPC between the CLI and the daemon.
- A separate `litefactory logs daemon` or `litefactory daemon logs` command; bare `logs` is the single entry point.
- Merging issue-run logs and the app log into one interleaved stream.
- Structured/JSON output modes beyond the existing `--raw` pass-through.
- Colorization of the app-log view (`daemon run` is not colorized either).
- `--only` (event-kind) filtering for the app log; those kinds are coding-agent event types and have no meaning for slog records.
- Changing `litefactory help`, `--help`, `-h` or the global usage table.

## Acceptance Criteria
- [ ] `litefactory logs` with no arguments exits 0 and prints app-log lines; it prints no usage block and no command help.
- [ ] `litefactory logs --help` and `litefactory logs -h` print the `logs` command's description, usage and flags to stdout, exit 0.
- [ ] `litefactory help` and bare `litefactory` still print the global command table, byte-identically to today.
- [ ] Given a `daemon.log` line produced by `slog.NewJSONHandler` from a record, the rendered app-log line is byte-identical to what `slog.NewTextHandler` produces for that same record — asserted by a test that logs through both handlers and diffs the re-rendered JSON against the text output.
- [ ] A record written through `WithGroup("git")` with attr `branch=main` renders as `git.branch=main`, not as a JSON object.
- [ ] Attribute order in the rendered line matches the order in the `daemon.log` line.
- [ ] Timestamps render in RFC3339-milliseconds form, matching `TextHandler`.
- [ ] A non-JSON line in `daemon.log` is emitted verbatim and does not abort the view.
- [ ] `litefactory logs -f` streams records appended after the command started, within one poll interval, and exits 0 on context cancellation.
- [ ] `litefactory logs -f` does not stop when a record with `msg=result` is appended.
- [ ] `litefactory logs -f` against a not-yet-existing `daemon.log` waits and starts streaming once the file appears.
- [ ] `litefactory logs -f` resumes correctly after the file is truncated beneath it.
- [ ] `litefactory logs --level warn` prints WARN and ERROR records and no INFO or DEBUG records.
- [ ] `litefactory logs --level DEBUG` is accepted (case-insensitive); `litefactory logs --level loud` exits 2 and names `debug, info, warn, error`.
- [ ] `litefactory logs --level debug` with `log_level: info` in config writes one advisory line to stderr naming the configured level and `daemon start --level`, while stdout carries only log output; exit code is unaffected.
- [ ] `litefactory logs` with no `daemon.log` prints a sentence naming the path and exits 0.
- [ ] `litefactory logs` prints the last 200 records by default; `-n 20` prints 20; `--all` prints every record; `-n` with `--all` exits 2; `-n 0` exits 2.
- [ ] `litefactory logs --raw` with no issue writes the selected `daemon.log` lines byte for byte.
- [ ] `litefactory logs --grep tick`, `-i` and `--invert` filter app-log lines with the same semantics they have for issue logs.
- [ ] `litefactory logs --run 2` with no issue exits 2 and names `--run`; `litefactory logs 123 --level debug` exits 2 and names `--level`.
- [ ] `litefactory logs 123` output, exit codes and every existing flag are unchanged — the existing `cmd_logs_test.go` suite passes without behavioral edits.
- [ ] `litefactory logs` succeeds against a `$LF_HOME` that has a `daemon.log` but no SQLite database (no `init` precondition error).
- [ ] `litefactory logs daemon` exits 2 with a message that both rejects `daemon` as an issue and mentions that bare `litefactory logs` shows litefactory's own log.
- [ ] `litefactory daemon run --level debug` emits DEBUG records to `daemon.log` and to the console when attached to a terminal, with `log_level: info` still in `config.yaml`, and `config.yaml` is not modified.
- [ ] `litefactory daemon run --level loud` exits 2 before starting the poll loop.
- [ ] `litefactory daemon start --level debug` writes a plist whose `ProgramArguments` end with `daemon run --level debug`, and reports the effective level in its summary output.
- [ ] `TestLogsNeverImportsRedact` covers every source file the app-log view is implemented in (`logsSourceFiles` updated).
- [ ] `internal/cli/router_test.go:95` and `internal/cli/parse_test.go:222` are updated to the new bare-`logs` contract.
- [ ] `make check` passes (`fmt vet lint test`, tests with `-race`).

## Assumptions Taken (in place of live clarification)

This was a headless run. Each item below was decided from the research brief's stated recommendation, the code, or `APP_CONTEXT.md`, and is recorded here as an assumption rather than a confirmed requirement.

| # | Open question (brief) | Decision taken | Basis |
|---|---|---|---|
| A-1 | OQ1 — does bare `logs` really "show all commands as help"? | It does not; it shows only the `logs` command's own usage block (`router.go:44-51`). Read the complaint as "I typed `logs` and got help instead of logs", regardless of which help. Bare `logs` now shows the app log and no help at all. | Brief's stated assumption; verified in `router.go`/`parse.go`/`router_test.go:95` |
| A-2 | OQ3 — bare `logs` vs. a new command | Make `issue` optional on the existing `logs` command (`ArgSpec.Min: 0`). No new command, no `logs daemon` subcommand. Accepted breaking change: bare `logs` goes from exit 2 to exit 0. | Reporter's literal words ("when saying 'litefactory logs' … this should then rather show the logs of litefactory itself"); brief names this the most literal satisfaction |
| A-3 | OQ2 — what bare `logs` shows | Resolved by the reporter's follow-up: the `daemon run` console output, i.e. stock `slog.TextHandler` text, re-rendered from `daemon.log`'s JSON. Not raw JSON, not a curated subset. | Reporter's comment on #89; `logging.go:42-45`, `logging_test.go:12-30` |
| A-4 | OQ4 — where help moves to | Nowhere. `logs --help` / `-h` is already wired per-command (`parse.go:63` → `router.go:80-83`) and needs no work. Global help is untouched. | Verified in code this run; the brief left this unconfirmed |
| A-5 | OQ5 — redaction posture for app-log content | Same as issue logs: unredacted. `daemon.log` lives under a 0700 `$LF_HOME` the operator owns, and S-7 redaction is outbound-only. The `never import redact` guard is extended to the new file rather than relaxed. | `cmd_logs.go:151-159`, `main.go:36`, `filesink.go:10-19` |
| A-6 | OQ6 — location of `TestLogsNeverImportsRedact` | Confirmed: `internal/cli/cmd_logs_test.go:901`, with `logsSourceFiles` at `:916`. No longer an open question. | Read this run |
| A-7 | OQ8 — rotation / truncation | No rotation exists and none is added. Truncation is handled defensively at follow time (re-open at offset 0 when the file shrinks). Rotation stays out of scope. | `filesink.go:56-69`; `plist.go:62` names unrotated `daemon.log` as accepted M0 debt |
| A-8 | OQ9 — multi-repo filtering | Carry over `--grep`/`-i`/`--invert` only. No repo-specific filter; `--grep 'repo=acme/api'` covers it because the daemon already decorates lines with `repo=`/`task=`. No `--only`. | Brief's recommendation ("carry over `--grep` at minimum"); `internal/daemon/logging_test.go` AC-PAR-7.1 |
| A-9 | OQ10 — what the verbosity keyword controls | Option **(c)**, the brief's stated most-complete reading: a display-time `--level` filter on `logs` **and** a write-time `--level` override on `daemon run` / `daemon start`. Live mutation of a running daemon is excluded. Mitigation for (a)'s trap: the stderr advisory when the requested level is finer than what was configured. | Brief OQ10 recommendation; `main.go:103-117`, `logging.go:38` |
| A-10 | OQ11 — where the write-time flag lives | On `daemon run` and `daemon start` only, not as a global flag. `daemon start` threads it into the plist's `ProgramArguments`. | Brief calls this the option consistent with `configuredLevel()`'s single call site; `plist.go:104-107`, `manager.go:108` |
| A-11 | Not raised in the brief — "keyword" form | A `--level <keyword>` flag, not a bare positional keyword. A positional would be ambiguous against the (now optional) `issue` positional. | `parse.go:30-41` `splitPositionals`; the vocabulary matches `config.yaml`'s `log_level` exactly |
| A-12 | Not raised in the brief — default output volume | Tail the last 200 records by default, with `-n` and `--all`. `daemon.log` is unrotated and can be arbitrarily large, and the reporter's own framing (`-f`) is `tail -f` ergonomics. | `filesink.go` (no rotation); reporter's `-f` request |
| A-13 | Not raised in the brief — is this daemon-only? | `daemon.log` is a whole-process log: every `litefactory` invocation constructs the same `FileSink` (`main.go:45,86-93`). The view is named "litefactory's own log", and a `logs` invocation's own `dispatch` record showing up at debug level is accepted, not filtered out. | `main.go:45-93`, `main.go:78` |
