# Plan Brief — App-level log view (`litefactory logs` with no issue)
# Branch: lf/89-logging-is-only-on-issues-but-not-on-the
# Date: 2026-09-05
# Parent issue: #89 — "logging is only on issues but not on the app itself"
# Citation base commit: 9fbf54e
# Research brief: .aiteam/lf-89-logging-is-only-on-issues-but-not-on-the/2026-09-05/logging-is-only-on-issues-but-not-on-the/RESEARCH_BRIEF.md

## Shared Decisions

- **The app-log source is a third `logSink`, and it never ends.** `type logSink interface { write(line []byte) (done bool, err error) }` (`internal/cli/cmd_logs_render.go`) is the sole extension point for reading `daemon.log`. Both `rawSink` and `renderSink` set `done` when a *run* ends; the app log has no such concept, and `daemon.log` genuinely contains records whose text reads like a run's terminal event (e.g. `msg=result`). Any sink built for the app-log view returns `done == false` unconditionally, and that must not stop the tail pass or the follow loop. Binds US-2, US-3, US-9.
- **`--level` names two unrelated mechanisms that share only a vocabulary and a parser.** On `logs` it is a *display-time* filter over records already on disk — it can only hide, never reveal. On `daemon run`/`daemon start` it is a *write-time* threshold passed into the logger before it is ever built. Do not introduce one option type spanning both; do not let `logs --level` write anything anywhere. Binds US-5, US-6, US-8.
- **`logging.ParseLevel` is the only level parser used anywhere in this feature.** It already exists, is already case-insensitive over `debug|info|warn|error`, and already returns an error on garbage. No second switch statement, and no direct use of `slog.Level.UnmarshalText` in `internal/cli`. An unparseable value is a usage error, exit 2, at every call site. Binds US-5, US-6.
- **Truncation handling for `daemon.log` belongs to the app-log path, not to `tailLog`.** `tailLog`'s existing doc comment justifies holding a bare offset by stating that run logs are "append-only and never rotated." That premise does not hold for `daemon.log`. The preferred resolution is a separate follow loop that shares `followInterval`/`followReadSize`/the partial-line `pending` technique with `tailLog` but owns its own stat-based size check, re-opening at offset 0 when the file shrinks — leaving `tailLog`'s comment true and its behavior untouched. If a maintainer instead extends `tailLog` itself, updating that comment is part of the same change, not a follow-up. Binds US-2, US-9.
- **US-1 and US-4 ship as one atomic change.** Flipping the `logs` command's `Args.Min` from 1 to 0 without also adding the `len(args) == 0` branch inside the same handler closure turns bare `litefactory logs` from a clean usage error into a panic — the closure today ends with an unconditional `args[0]`. The `Usage`/`Long` help text describing the new mode, and the two pinned `router_test.go` expectations that describe today's "bare logs is an error" contract, are part of that same change, not a follow-up task.

## Workflow

- **Shipping order, with the reason for each position:**
  1. **US-3** (renderer) — no dependency on anything else in this feature and independently golden-testable against real `slog` handlers; everything downstream needs it to produce readable output.
  2. **US-9** (tail-by-default) — needs the renderer to emit the lines it selects.
  3. **US-1 + US-4** (bare `logs` routing, together — see Shared Decisions) — now has a real tail-and-render pipeline to route into.
  4. **US-2** (follow) — consumes the offset US-9's tail computation hands it; building it before US-9 exists would mean re-seeking twice.
  5. **US-5** (level filter) — operates on the decoded record US-3 produces.
  6. **US-7** (grep/-i/--invert) — pure wiring of an existing filter onto the rendered line; has no reason to wait behind US-5 except that it is convenient to land alongside it.
  7. **US-8** (stderr advisory) — consumes the "levels seen" state US-5 owns.
  8. **US-6** (`daemon run`/`daemon start --level`) — structurally independent of the whole app-log-view slice above and may be worked in parallel at any point. Its `daemon start` half is clean and safe to land on its own; its `daemon run` half carries DEV-1 (see its sub-issue) and should be scheduled where a stall on that resolution does not block anything else shipping.

- **Dependency graph / hard couplings:**
  - US-1 -> US-3, US-9 (needs both to have real content to route into)
  - US-4 -> US-1 (one atomic change; see Shared Decisions)
  - US-2 -> US-3, US-9 (renders through US-3; starts from US-9's offset)
  - US-5 -> US-3 (filters the decoded record)
  - US-7 -> US-3 (filters the rendered line)
  - US-8 -> US-5 ("levels seen" state)
  - US-6 -> none (independent track)

## Cross-Story Constraints

- **Platform:** No new package, no new command registration, no new third-party dependency — everything needed is stdlib (`encoding/json`, `log/slog`, `os`, `time`, `regexp`). No goroutine, channel, or mutex is introduced anywhere in this feature; every poll loop (tail, follow) runs synchronously on the command's own goroutine and selects on `ctx.Done()`. `internal/logging` gains no new exported decoder, record type, or rendering helper for the CLI's benefit — the contract between writer and reader is the on-disk JSON line format, not a shared Go type, and stays a leaf package except for US-6's use of its existing `Options.Level`/`ParseLevel`. New `os.Open` calls against `daemon.log` need the same `#nosec G304` justification already used for the three existing ones in `cmd_logs.go` ("litefactory's own artefact path, from `config.Paths`").
- **Security:** `daemon.log` is deliberately unredacted, same posture as issue logs — no source file this feature adds may import `internal/redact`, and rendered output must never be posted, uploaded, or written anywhere but the terminal. No daemon IPC of any kind — no socket, no signal, no lock file, and no `fsnotify`/`net/rpc`/`os/signal` import anywhere the feature touches. The `daemon.log` path is obtained only through `config.Paths.DaemonLogPath()`; no `filepath.Join` against `$LF_HOME` anywhere in `internal/cli`. The value `daemon start --level` writes into the launchd plist is already covered by `Agent.Validate()`'s existing `checkPlistValue` pass over every `ProgramArguments` element, and is additionally constrained to four literal keywords by `logging.ParseLevel` before it gets that far — no new escaping or sanitisation is to be added.
- **Tests:** Every new source file this feature adds joins `logsSourceFiles` in `internal/cli/cmd_logs_test.go` as part of the story that creates it, not a follow-up — that slice is what `TestLogsNeverImportsRedact` and `TestLogsUsesNoDaemonIPC` iterate. `internal/cli/router_test.go`'s "missing argument: <issue>" case and its top-level `--help` case (which asserts the literal `"logs <issue>"`) change deliberately as part of the design change, with the new contract stated in the test — never silently edited to green. `internal/cli/parse_test.go`'s local-fixture arity test is unrelated generic-parser behavior and must keep passing unchanged, with `Min: 1` retained in its own fixture. Every existing flag, output byte, and exit code on the issue path (`litefactory logs <issue> ...`) stays unchanged; the existing `cmd_logs_test.go` suite passes with no behavioral edits. `make check` (fmt, vet, lint, test with `-race`) must pass throughout.
- **Ring-fenced:** `internal/launchd` is untouched by this plan. `Manager.NewAgent`, `Agent.Validate`, and `Agent.Render` are correct as they stand; US-6's plist half is entirely a change to the argument slice `cmd_daemon_agent.go` builds before handing it to `NewAgent`. Log rotation/retention/truncation *policy* for `daemon.log` stays out of scope — pre-existing, named accepted M0 debt in `internal/launchd/plist.go`; this plan only makes the reader tolerate truncation defensively, it does not fix the underlying gap. Live mutation of a *running* daemon's log level (no `slog.LevelVar`, no IPC) is out of reach for this plan.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Bare `litefactory logs` shows litefactory's own log | M | US-3, US-9 | https://github.com/marioatcloudvane/litefactory/issues/91 |
| US-2 | `-f` follow on the app log | M | US-3, US-9 | https://github.com/marioatcloudvane/litefactory/issues/92 |
| US-3 | Render re-parsed JSON as stock `slog.TextHandler` text | L | — | https://github.com/marioatcloudvane/litefactory/issues/93 |
| US-4 | Bare invocation stops printing a usage block; help stays at `logs --help` | S | US-1 | https://github.com/marioatcloudvane/litefactory/issues/94 |
| US-5 | `--level` as a display-time filter on `logs` | S | US-3 | https://github.com/marioatcloudvane/litefactory/issues/95 |
| US-6 | `--level` as a write-time flag on `daemon run`/`daemon start` | M | — | https://github.com/marioatcloudvane/litefactory/issues/96 |
| US-7 | Carry `--grep`/`-i`/`--invert` to the app log | S | US-3 | https://github.com/marioatcloudvane/litefactory/issues/97 |
| US-8 | Stderr advisory when the requested level was never captured | S | US-5 | https://github.com/marioatcloudvane/litefactory/issues/98 |
| US-9 | Tail by default | M | US-3 | https://github.com/marioatcloudvane/litefactory/issues/99 |

## Out of Scope

- Log rotation, truncation, or retention policy for `daemon.log` (pre-existing gap; already named accepted M0 debt in `internal/launchd/plist.go`). Raise separately.
- Changing a *running* daemon's log level without restarting it (no `slog.LevelVar`, no IPC).
- A global `--log-level` flag usable across every command; the write-time override lives on `daemon run`/`daemon start` only.
- Persisting `--level` back into `config.yaml`.
- Any change to what the daemon logs, or to tick/dispatch logic — `tick.go` already emits everything this feature needs.
- Any new IPC between the CLI and the daemon.
- A separate `litefactory logs daemon` or `litefactory daemon logs` command; bare `logs` is the single entry point.
- Merging issue-run logs and the app log into one interleaved stream.
- Structured/JSON output modes beyond the existing `--raw` pass-through.
- Colorization of the app-log view (`daemon run` is not colorized either).
- `--only` (event-kind) filtering for the app log; those kinds are coding-agent event types with no meaning for slog records.
- Any change to `litefactory help`, `--help`, `-h`, or the global usage table.

## Provenance

- Citation base: 9fbf54e (branch tip at research completion; two research runs and this plan run have elapsed since).
- Known-stale areas: none flagged beyond what the research brief already carries. The architect independently re-verified two of the brief's open items directly against code this run rather than trusting the brief's own uncertainty on them (where bare `logs --help` is wired; where the redaction-guard test lives).
- Re-pinning requirements: line-number citations throughout the feature spec and architect review (e.g. `cmd_logs.go`, `logging.go`, `router_test.go`, `parse_test.go`) should be re-verified against the branch tip at implementation start.

## Headless-run assumptions

This plan was produced without a live user available to answer clarifying questions. Every open question the research brief carried was resolved by taking the brief's own stated recommendation rather than blocking; the requirements-engineer recorded thirteen such assumptions (A-1 through A-13) in `feature-spec.md`'s final section, with the code citation behind each. The two most consequential for implementation:

- Bare `litefactory logs` moves from exit 2 (usage error) to exit 0 (log output) — a CLI contract change with two pinned tests behind it (see US-4).
- `daemon.log` is a whole-process log, not a daemon-only one: every `litefactory` invocation constructs the same `FileSink`, so a `logs` invocation can see its own `dispatch` record at debug level. This is accepted as correct rather than filtered (see US-3/US-9 edge cases).

Two further judgment calls were made by the architect, not the brief, and are called out in the relevant sub-issues rather than here: DEV-1 (US-6's `daemon run --level` plumbing) and DEV-2 (US-4's deliberate test changes).
