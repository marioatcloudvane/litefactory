# Research Brief — lf-89-logging-is-only-on-issues-but-not-on-the

## Request

From GitHub issue #89 (opened by @marioatcloudvane), verbatim:

> when i do "logs" i have no possibility to check the logs i have in litefactory itself - e.g the ticks, ...!
>
> i need a log that is independent of an issue; i also want to follow and see the ticks litefactory works on. implication here: when saying "litefactory logs" it currently shows me all commands as help but i think this should then rather show the logs of litefactory itself! so the help needs to move. also, we need to keep the -f here as well to follow!

Restated: the reporter wants (a) a way to view litefactory's own operational log — specifically the daemon's poll/dispatch cycles ("ticks") — independent of any single GitHub issue; (b) `litefactory logs` invoked bare/without an issue argument to show that app-level log rather than a help screen; (c) the existing `-f`/follow behavior preserved for whatever `logs` ends up showing; (d) command help relocated elsewhere so it no longer occupies the bare-`logs` slot.

This is a headless run (no clarifying answers available). Assumptions taken below are marked `[Assumption]` and are also collected in Open Questions/Recommended scope.

## Codebase Context

All findings in this section are `[V]` (verified this run via `codebase-tour`, single Go module, no cobra/zap/logrus — hand-rolled CLI router + stdlib `log/slog`).

### The `logs` command today

- `internal/cli/cmd_logs.go` (988 lines) implements the whole `logs` command. `newLogsCommand()` declares `Args: ArgSpec{Names: []string{"issue"}, Min: 1}` — the issue is a **required** positional argument. `[V]`
- Flags (`logsOptions.bind`): `--run N`, `--runs`, `--raw`, `--only`, `--grep`, and **`-f`/`--follow`** ("follow the run as events are written"). `[V]`
- `runLogs` → `loadLogTarget` opens the SQLite store, reads the session + its runs for the given issue, then **closes the DB** before branching into `writeRunList` / `tailLog` / `copyLog` / `writeRun`. `[V]`
- `tailLog(ctx, sink, path, followInterval=250ms, finished)`: pure polling via `time.NewTicker`, no fsnotify — documented as deliberate ("daemon is a separate process appending, a watcher buys nothing over a ticker"). Stops on a terminal event (`"result"`, `"turn.completed"`, `"turn.failed"`) or `ctx.Done()`. Reads via `bufio.Reader.ReadBytes('\n')` (not `Scanner`, to avoid the 64KB token limit). `[V]`
- Explicit invariant in the file: `cmd_logs.go` must **never** import `internal/redact` — a build-breaking test (`TestLogsNeverImportsRedact`, referenced by comment; exact location not directly confirmed this run — see Open Questions) enforces that `logs` output is always unredacted by design (S-7). Also documented: no daemon IPC of any kind (AC-5.8) — no socket, no signal, no lock file. `[V]`
- Per-issue run logs live at `naming.RunLogPath(home, ref, ordinal)` / `naming.RunMetaPath(...)`, under `$LF_HOME/logs/runs/...`, one JSONL file per run. `internal/turn` is commented as the writer of the run + meta files (not independently confirmed by reading `internal/turn`). `[V]` / `[W]` for the writer attribution specifically.

### What bare `litefactory logs` actually does today

- `internal/cli/parse.go:52-78` (`parseArgs`): when `len(pos) < cmd.Args.Min`, returns `*UsageError{Cmd: cmd, Usage: ""}` with message `"missing argument: <issue>"`. `[V]`
- `internal/cli/router.go:44-51` (`Router.Run`): when `Usage == ""` and `Cmd != nil`, it prints `writeCommandUsage(cmd)` — i.e. **only the `logs` command's own scoped usage/flags**, not the full command table. `[V]`
- The global command table (`writeGlobalUsage`) only fires for a truly bare `litefactory` (zero args) or `help` / `--help` / `-h` (`router.go:60-71`), where `parseArgs` instead returns `Usagef(nil, "no command given")` with `Cmd == nil`. `[V]`
- Confirmed by `internal/cli/router_test.go:95`: `wantStderrHas: []string{"missing argument: <issue>", "logs <issue>"}`.
- **Discrepancy with the reported symptom**: the issue text says bare `logs` "shows me all commands as help." As coded today it shows only the `logs` command's own usage block (its `Long`/`Usage`/flag list), not the full command table. This is flagged explicitly in Open Questions — it may reflect the reporter's imprecise recollection, an earlier code path, or a UX complaint that the *command-scoped* help is still unwanted friction (i.e. "I typed logs and got help text instead of logs, regardless of which help text it was").

### The daemon / "ticks"

- `internal/daemon/daemon.go`: `Daemon.Run` → `Start` → `loop` → `Tick`, once per poll interval. `[V]`
- `internal/daemon/tick.go:120-186` (`Daemon.Tick`): two phases, `discover` then `dispatch`. Ends with a structured Info-level log line: `"tick complete"` carrying `repos`, `issues`, `dispatched`, `in_flight`, `deferred_sessions`, `cursor`, `duration` (`tick.go:177-184`). Many Debug/Info/Warn/Error lines throughout discover/dispatch/work for skipped repos, backoff, rate limits, per-task first-sight/drain/comment events, each decorated with `task=`/`repo=`/`session_id=` via child loggers (per `internal/daemon/logging_test.go`, `TestEveryLineBelowPollNamesItsTask`, AC-PAR-7.1). `[V]`
- This "tick complete" logging **already happens on every tick, regardless of whether anyone is watching** — it is not gated behind any viewer being attached. `[V]`
- `internal/store/pollcursor.go:22-23`: `PollCursor.LastTickAt` is persisted to SQLite and described as "when the last tick completed; `daemon status` reports it." `[V]`
- `internal/cli/cmd_daemon_agent.go:121-209` (`runDaemonStatus` / `lastTickAt`): `daemon status` already prints a `last tick` timestamp, read from the DB as a point-in-time snapshot (opens DB, reads `GetPollCursor`, closes, formats RFC3339 or a "never ticked" sentinel). This is the **closest existing prior art** to "see the ticks," but it is a snapshot, not a stream/follow. `[V]`

### Existing logging infrastructure

- `internal/logging/logging.go` (122 lines): one process-wide `*slog.Logger`, fanned out to two handlers — a text handler to the console (only when the console is a terminal) and a JSON handler that **always** writes every record to a file. No package-level global logger; passed by struct field. `[V]`
- `internal/config/paths.go:20-22,73-83`: `LogsDir() = $LF_HOME/logs`, `RunsDir() = $LF_HOME/logs/runs`, `DaemonLogPath() = $LF_HOME/logs/daemon.log`. `daemon.log` is a **pre-existing, distinct, app-level log file that already exists on disk today** — it is simply never read by any `logs`-family CLI code. Only `RunsDir()`/`naming.RunLogPath` and the DB are read by `cmd_logs.go`. `[V]`
- `internal/logging/filesink.go` (86 lines): `FileSink` opens `daemon.log` lazily on first write (append mode), relies on process umask `0o077` for 0700/0600 perms (no explicit `Chmod`). Thread-safe via mutex. **No rotation logic observed** — `daemon.log` grows unbounded as currently implemented. `[V]`
- `daemon.log` (JSON, via `slog.NewJSONHandler`) and per-issue run logs (JSONL, rendered by `cmd_logs.go` with a `--raw`/formatted split) are **structurally separate today with no shared reader abstraction**. Any feature that surfaces both through one command needs to bridge two different formats and two different locations. `[V]`

### Convention for adding this command

- One `*Command` per `cmd_<group>.go` file, registered with one line in `internal/cli/commands.go:8-20` (9 commands today: `init`, `repo add/list`, `daemon run/start/stop/status`, `logs`, `task clean`). This is the documented, established pattern for adding or restructuring a command. `[V]`
- `ArgSpec{Min: 0}` is already a supported mechanism in `internal/cli/command.go` — nothing structurally prevents making the `issue` positional optional on `logs` (this is a seam that exists, not a recommendation either way). `[V]`
- `tailLog`'s poll-based follow implementation is directly reusable in shape (same 250ms-ticker + offset-tracking pattern) for a "follow the daemon log" feature — it is the one existing "follow a growing file" pattern in the codebase. `[V]`

## Domain / Problem Space

litefactory currently has two kinds of "log" that are conceptually and physically separate:

1. **Issue/run-scoped logs** — one JSONL file per run, keyed to a GitHub issue, fully exposed today via `litefactory logs <issue>` with rich options (`--run`, `--raw`, `--grep`, `--only`, `-f`).
2. **Daemon/system-scoped logs** — `daemon.log`, a single JSON-line file capturing every daemon tick and every structured event across all repos/issues, already written continuously by the running daemon, but with **zero CLI exposure**. The only surfaced fragment of this today is a single timestamp (`daemon status`'s "last tick").

The reporter's need is squarely for visibility into category 2, streamed, and wants the ergonomic `-f` follow behavior that already exists for category 1. The complaint about "the help needs to move" is a consequence of how the `logs` command's argument parsing currently behaves: since `issue` is a required positional, running `logs` with no arguments is treated as a usage error and short-circuits into printing that command's help/usage block instead of doing anything log-related. `[V]`

There is no evidence of a "tick" concept meaning anything other than the daemon's poll-and-dispatch loop (`internal/daemon/tick.go`) — `[V]` this matches the reporter's usage ("the ticks litefactory works on").

## Constraints & Risks

- **Redaction invariant.** `cmd_logs.go` is under a build-breaking guard preventing it from importing `internal/redact`, because per-issue log output is unredacted by design (S-7). If daemon-log content is ever surfaced through the same command/file, whoever implements this must confirm whether `daemon.log` content carries the same "always unredacted, operator already owns `$LF_HOME`" trust assumption, or whether it needs different handling. This wasn't contradicted by anything found, but wasn't explicitly confirmed for daemon-level output either. `[V]` for the guard's existence; `[Assumption]` that the same trust model extends to daemon.log content.
- **No IPC with the daemon.** `cmd_logs.go` documents "no daemon IPC of any kind (AC-5.8) — no socket, no signal, no lock." Any new follow-the-daemon-log feature should stick to the same file-polling pattern used by `tailLog`, not attempt to talk to a running daemon process directly. `[V]`
- **Format mismatch.** `daemon.log` is JSON-per-line (raw slog JSON handler output); per-issue logs go through a formatted/`--raw` rendering split. A unified `logs` experience needs an explicit decision on how daemon.log lines get rendered for a human reading them live (pass through raw JSON, or build a formatter, mirroring the per-issue `--raw` split).
- **No log rotation.** `daemon.log` has no observed rotation logic — it will grow unbounded. Not new risk introduced by this feature, but worth flagging since a "follow the daemon log" feature makes this file more visible/relied-upon than it is today.
- **Backward compatibility of `logs <issue>`.** Two existing tests pin today's exact behavior and error strings: `internal/cli/router_test.go:95` and `internal/cli/parse_test.go:222` (bare-`logs` usage error). Any change to `logs`'s argument requirements (e.g. making `issue` optional so bare `logs` means something else) will need these updated, and the ~870 unread lines of `internal/cli/cmd_logs_test.go` likely contain more follow-mode (`-f`) test cases that would need auditing for regressions. `[V]`
- **Where "help needs to move" lands.** The reporter's phrasing implies command help should be relocated so it's not what `logs` shows by default. The codebase already has a `help` command/flag path (`litefactory help`, `--help`, `-h`) and a `writeGlobalUsage` for the fully-bare `litefactory` invocation — these are unaffected by anything in `cmd_logs.go` and already exist as the "true" help entrypoints. What would change, if this request is implemented as described, is only the *specific* case of `logs` with no issue argument no longer falling through to that command's own usage block.

## Open Questions

1. **Does "shows me all commands as help" match reality, or is the reporter describing the command-scoped usage block that `logs` (with no args) actually prints today?** The code prints only `logs`'s own usage (`internal/cli/router.go:44-51`, confirmed by `router_test.go:95`), not the full command table (`writeGlobalUsage` is reserved for a truly bare `litefactory`). Plan mode should clarify with the reporter whether the ask is "bare `logs` should never show *any* help text, full stop" (in which case the current command-scoped usage block is already the smaller of two possible "shows help" behaviors and just needs replacing) — this brief assumes the intent is the former.
2. **What should `litefactory logs` (bare, no issue argument) actually show?** Candidate designs, none confirmed with the reporter:
   - (a) Tail/follow `daemon.log` directly (all ticks, all repos, all issues, everything the daemon logs).
   - (b) A curated "system events" view (e.g. only tick-complete lines and dispatch decisions, filtering out per-repo Debug noise).
   - (c) Something else entirely (e.g. a merged view across daemon.log + all active runs).
   Design (a) is the lowest-effort/most literal reading of the issue text ("a log that is independent of an issue... follow and see the ticks") and reuses `tailLog`'s existing pattern most directly. This brief recommends Plan mode default to (a) unless the reporter indicates otherwise, since it requires no new filtering/curation logic and directly answers "see the ticks."
3. **Should the daemon-log-follow feature live under bare `litefactory logs` (making `issue` optional), or under a new/renamed command (e.g. `litefactory logs daemon`, `litefactory daemon logs`)?** Both are structurally supported by the existing command-registration convention. Making `issue` optional on the existing `logs` command most literally satisfies "when saying 'litefactory logs' it currently shows me all commands as help but I think this should then rather show the logs of litefactory itself" — but it is a breaking change to `logs`'s current contract (bare invocation currently errors with exit code 2; it would need to instead succeed and start following). Plan mode must decide and record this as an explicit design decision.
4. **Where does help move to, concretely?** `litefactory help`, `litefactory --help`, `litefactory -h`, and bare `litefactory` (zero args) already route to `writeGlobalUsage` today and are untouched by this issue's literal ask. Per-command help (`litefactory logs --help`, if that flag exists — not confirmed this run) may need to be the new discoverability path for `logs`'s own argument/flag reference once bare `logs` stops showing it automatically. Confirm during planning whether `--help` is already wired per-command or needs adding.
5. **Redaction and trust model for daemon.log content surfaced via `logs`.** Confirm whether the S-7 "logs output is always unredacted" design intent is meant to extend to daemon-level log content, or whether daemon.log (which may span multiple repos/issues at once, unlike a single-issue view) warrants different treatment.
6. **Exact location and content of `TestLogsNeverImportsRedact`** — referenced only via a comment in `cmd_logs.go`, not independently located this run. Needs confirming before any implementation touches `cmd_logs.go`'s import graph.
7. **Confirm `internal/turn` as the writer of per-run log/meta files** — inferred from a comment in `cmd_logs.go`, not verified by reading `internal/turn` directly. Low risk to this feature (daemon.log is the relevant file, not per-run logs) but worth a footnote for Plan mode's file-ownership map.
8. **Does `daemon.log` ever get rotated/truncated?** No rotation logic was found in `internal/logging/filesink.go`. If a long-running `-f` follow attaches to a growing, unrotated file, that's consistent with today's behavior for per-issue logs too, but worth confirming there's no silent-truncation edge case Plan mode should design around.
9. **Filtering for a multi-repo daemon.** `daemon.log` interleaves ticks and events across all configured repos. Does the reporter want to filter by repo (mirroring `--only`/`--grep` on the existing `logs` command), or is an unfiltered firehose acceptable for v1? Not stated in the issue; recommend Plan mode consider carrying over `--grep` at minimum since the pattern already exists.

No web-research questions were identified for this issue — it concerns only this codebase's existing CLI/daemon/logging architecture, not any external library, protocol, or specification. `web-researcher` was not invoked.

## Recommended scope for Plan mode

**In scope:**
- Decide and specify the concrete shape of an app-level/daemon-level log view exposed via the CLI (see Open Question 2 and 3), including whether it lives under the existing `logs` command (with `issue` made optional) or a new command/subcommand.
- Specify follow (`-f`) behavior for that view, reusing the existing `tailLog` polling pattern (250ms ticker, offset tracking, `bufio.Reader.ReadBytes`) as the reference implementation.
- Specify where per-command help/usage moves to (or confirm it already lives at `litefactory logs --help`/`litefactory help logs` if such a path exists) so that bare `logs` (if repurposed) no longer needs to fall back to a usage block.
- Specify rendering of `daemon.log`'s JSON lines for human/live reading (raw pass-through vs. a formatter), and whether `--raw`/`--grep`/`--only`-style flags carry over.
- Update/extend the two pinned tests (`internal/cli/router_test.go:95`, `internal/cli/parse_test.go:222`) and audit `internal/cli/cmd_logs_test.go` for any follow-mode assertions affected by a changed `logs` contract.
- Decide the redaction/trust posture for daemon-log content surfaced via the CLI (Open Question 5).

**Out of scope (unless the reporter says otherwise in a follow-up):**
- Any change to the daemon's actual tick/dispatch logic, log content, or log levels — the daemon already logs everything needed (`tick.go:177-184` and throughout); this is purely a CLI-exposure and command-routing problem, not a daemon-logging-completeness problem.
- Log rotation/retention for `daemon.log` — flagged as a pre-existing gap (Open Question 8) but not part of the reported issue; raise separately if Plan mode judges it a blocker.
- Any new IPC mechanism between the CLI and a running daemon process — the existing "no daemon IPC" constraint (AC-5.8) should continue to hold; file-polling is the established and sufficient pattern.
