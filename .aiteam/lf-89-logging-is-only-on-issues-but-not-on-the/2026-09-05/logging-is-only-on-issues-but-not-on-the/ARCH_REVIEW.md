# Architect: Go Architect
# Created: 2026-09-05
# Input: feature-spec.md (APPROVED), issue #89 — "logging is only on issues but not on the app itself"
# Mode: Plan (pre-plan architect review — no `plan_brief.md` or sub-issues exist yet,
#        so hints are organised per user story US-1..US-9 rather than per T-NNN task id)

## Verdict

**APPROVED WITH CONDITIONS.** The feature is structurally a good fit: it adds a
second *source* to a command that already has a clean sink abstraction, and it
needs no new package, no new command registration, no new dependency, no DB
entity and no migration. Nine of the ten cross-story decisions below are
straightforward reuse of machinery that already exists in `internal/cli`.

Two things must be settled before implementation starts, and both are recorded
as deviations at the end of this document:

- **DEV-1 (blocking for US-6 only)** — `daemon run --level` cannot be honoured
  by the handler, because `cmd/litefactory/main.go` builds the logger *before*
  the router dispatches and `runDaemonRun` consumes that already-built
  `env.Logger`. Satisfying the spec requires an argv-aware level resolution in
  `main`, which puts parsing in a package documented as "wiring only" and sits
  next to STYLE-6. A named, narrow boundary is proposed. The rest of US-6
  (`daemon start --level`) is clean and needs no deviation.
- **DEV-2 (advisory)** — three existing tests pin today's "bare `logs` is a
  usage error" design. They are not in the TEST-10 protected family, so changing
  them is legitimate — but they must be changed *as part of the design change*,
  with the reason written down, not quietly edited to green.

Everything else is guidance, not a gate.

---

## System Overview

### How this feature fits into the existing system

`litefactory logs` today is a single-source viewer. `internal/cli/cmd_logs.go`
resolves an issue number to a run directory under
`config.Paths.RunsDir()`, opens that run's JSONL event log, and pushes it
through one of two sinks defined in `internal/cli/cmd_logs_render.go` — the raw
passthrough sink or the human renderer. `internal/logging` is an unrelated leaf
package: it builds the *process* logger (`slog`) that everything else writes
through, with a JSON sink to `config.Paths.DaemonLogPath()` and a text sink to
the console when stdout is a tty. The two have never met. That gap is issue #89:
the app's own log is on disk, in a well-known place, in a well-known format, and
there is no command that reads it.

This feature closes the gap by making `logs` a **two-source** viewer rather than
a one-source one. Nothing moves between packages. `internal/cli` gains the
ability to *read* `daemon.log`, which `internal/logging` already *writes*. There
is no shared type between them and there must not be one: the contract between
writer and reader is the on-disk JSON line format that `slog.JSONHandler`
produces, not a Go struct. `internal/logging` keeps exporting nothing new for
US-1..US-5 and US-7..US-9; only US-6's write-time level plumbing touches it, and
even then only through the existing `logging.Options.Level` and
`logging.ParseLevel` that main already calls.

The dependency direction is unchanged and must stay unchanged:
`cli -> config` for the path, `cli -> logging` only if US-6 needs a level parser
(it already has one — `logging.ParseLevel`), and never `logging -> cli`.
`internal/launchd` is untouched: `Manager.NewAgent` passes its `program []string`
straight into `Agent.ProgramArguments`, so US-6's plist threading is a change to
the slice that `internal/cli/cmd_daemon_agent.go` builds, not to the launchd
package.

### New packages / components introduced

**No new package.** Every change lands in three existing packages plus main.

| Component | Home | Responsibility |
|---|---|---|
| App-log routing branch | `internal/cli/cmd_logs.go` (`newLogsCommand`'s closure) | Decides, on zero positionals, to run the app-log path instead of the issue path |
| App-log view | new file `internal/cli/cmd_logs_app.go` | Resolves the daemon.log path, opens/tails it, applies the level filter, emits the "level never captured" advisory, owns the missing-file and truncation edge cases |
| slog-text re-render sink | new file `internal/cli/cmd_logs_slog.go` (or folded into `cmd_logs_render.go`) | A third `logSink` implementation: decodes one JSON record and re-emits it in stock `slog.TextHandler` form |
| Level flag plumbing | `internal/cli/cmd_daemon.go`, `internal/cli/cmd_daemon_agent.go`, `cmd/litefactory/main.go` | Carries a write-time level from argv into `logging.New`, and into `ProgramArguments` for `daemon start` |

Splitting into two new files rather than one is a judgement call the engineer
may collapse; what is **not** optional is that both files, whatever they are
called, are added to `logsSourceFiles` in `internal/cli/cmd_logs_test.go` (see
Decision D-9).

### Key boundaries

- **`internal/cli` -> `internal/config`.** The daemon.log path is obtained
  *only* through `config.Paths.DaemonLogPath()`. No `filepath.Join` against
  `$LF_HOME` or against `LogsDir()` anywhere in `internal/cli` — that is CFG-8,
  and the accessor already exists (`internal/config/paths.go:77`).
- **`internal/cli` -> the on-disk format, not `internal/logging`.** The renderer
  reads JSON that `slog.JSONHandler` wrote. It must not import
  `internal/logging` to do so, and `internal/logging` must not grow a decoder,
  a "record" type or a rendering helper for the CLI's benefit. The seam is the
  file format. This keeps `logging` the leaf it is in the package map.
- **`internal/cli` -> `internal/launchd`: unchanged.** `Manager.NewAgent(program,
  logsDir, environment)` takes the program slice verbatim
  (`internal/launchd/manager.go:105`). US-6 appends two elements to what
  `daemonProgram()` returns in `cmd_daemon_agent.go` and stops there.
  `Agent.Validate()` already runs `checkPlistValue` over every
  `ProgramArguments` element, so an injected level string is validated against
  non-UTF-8 and control characters before it ever reaches XML — the security
  property is inherited, not rebuilt.
- **`cmd/litefactory/main.go` -> `internal/cli`: one new, narrow, named call.**
  See DEV-1. Whatever shape is approved, main must call *one documented helper*,
  not open-code an argv scan.
- **The `logSink` interface is the extension point.** `type logSink interface {
  write(line []byte) (done bool, err error) }` in `cmd_logs_render.go` already
  abstracts exactly the thing this feature varies. Do not add a parallel
  abstraction beside it.

### Data flow

Issue path (unchanged):

```
argv -> router.dispatch -> parseArgs (peels <issue>) -> runLogs
     -> resolve run dir under config.Paths.RunsDir()
     -> open run JSONL -> logSink (raw | render) -> env.Stdout
```

App-log path (new):

```
argv (no positional) -> router.dispatch -> parseArgs (Min: 0, empty positionals)
     -> app-log branch inside newLogsCommand's closure
     -> config.Paths.DaemonLogPath()
     -> tail: seek back N lines (default 200) or offset 0 with --all
     -> for each line: JSON-decode in key order
          -> level below --level?  drop, remember "seen levels"
          -> --raw?                emit the JSON line verbatim
          -> otherwise             re-render as slog.TextHandler text
          -> --grep/-i/--invert applied to the *rendered* line
     -> env.Stdout
     -> after the initial pass: if --level was requested and no record at or
        above it was seen, one advisory line to env.Stderr
     -> -f? keep polling at followInterval from the held offset, forever,
        until ctx cancellation
```

Write-time path (US-6):

```
argv "daemon run --level debug" -> main resolves the level override
     -> logging.New(Options{Level: override or configuredLevel()})
     -> env.Logger -> cli.Router.Run -> runDaemonRun -> daemon.Setup{Logger: ...}

argv "daemon start --level debug" -> runDaemonStart
     -> daemonProgram() returns [abs, "daemon", "run", "--level", "debug"]
     -> manager.NewAgent -> Agent.Validate -> Agent.Render -> plist on disk
     -> writeStartReport already echoes ProgramArguments in the summary
```

### Concurrency & context notes

**This feature introduces no goroutine.** That is not a preference, it is
CONC-2, whose canonical text goes out of its way to say that an older plan
"names a `logs -f` tailer goroutine that does not exist (`litefactory logs -f`
re-reads the run's JSONL synchronously on the command's own goroutine)". The
app-log follow must be the same shape: a synchronous poll loop on the command's
own goroutine, `select`-ing on `ctx.Done()` and a timer. If an engineer finds
themselves reaching for `go func()`, a watcher, `os/signal`, or `fsnotify`, they
have left the design — and `TestLogsUsesNoDaemonIPC` will catch three of those
four mechanically.

`ctx` flows in from the handler signature and must reach the poll loop as the
first parameter, per CONC-5's spirit. `context.Background()` is banned outside
main by `.golangci.yml` (STYLE-2), so there is no escape hatch.

`tailLog`'s existing follow loop holds an `io.ReadSeeker` and its offset, which
its own doc comment justifies with: *"Run logs are append-only and never
rotated, which is what makes holding the offset sufficient."* That premise does
not hold for `daemon.log` — the spec requires detecting truncation and
re-opening at offset 0. See Decision D-6 for how to resolve that without making
the comment untrue (STYLE-1).

### Shared state and side effects

- **`daemon.log` is written by a live process while `logs` reads it.**
  `logging.FileSink` holds an `O_APPEND` handle under a mutex; appends are
  atomic enough that a reader sees whole lines in practice, but a reader can
  still catch a partial final line. The existing `tailLog` already handles this
  with a `pending` partial-line buffer — reuse that behaviour, do not assume
  every read ends on a newline.
- **The `logs` process logs to the file it is reading.** `main` emits
  `logger.Debug("dispatch", ...)` before dispatch, so `litefactory logs --level
  debug` will show its own dispatch record. The spec accepts this (edge case,
  A-12). Do not add suppression logic; do not special-case the command's own
  records. The one thing to watch is that this record is written *before* the
  tail offset is computed, so it lands in the output naturally.
- **`FileSink` creates `$LF_HOME/logs/` lazily on first write.** A machine that
  has never logged has no directory and no file. The reader must treat "no file"
  as a normal, exit-0 outcome, never as an error, and must not create the file
  as a side effect of reading it.
- **No new file is created, no permission is set, nothing is deleted.** The
  app-log path is read-only. `logs` must never open `daemon.log` for writing,
  and must never `MkdirAll` the logs directory.
- **No lock is taken.** `TestLogsUsesNoDaemonIPC` asserts that running `logs`
  never creates `config.NewPaths(home).LockPath()`. The app-log reader has no
  business coordinating with the daemon.

---

## Cross-story decisions

These bind two or more stories. They are decisions, not suggestions; a story
that departs from one needs a note saying why.

**D-1 — `logs` stays one command; the branch lives inside the closure.**
No new command is registered. `Commands()` keeps its nine entries.
`Args` becomes `ArgSpec{Names: []string{"issue"}, Min: 0}` and the routing
decision is `len(args) == 0` *inside* the handler closure. This is the single
most important structural point in the feature, because the current closure is:

```go
return func(ctx context.Context, env *Env, args []string) error {
    return runLogs(ctx, env, cmd, opts, args[0])
}
```

`args[0]` is an unconditional index. Flipping `Min: 1` to `Min: 0` without
restructuring this line turns bare `litefactory logs` from a clean exit-2 into a
**panic**. The `len(args) == 0` check must be added in the same change as the
`Min` change, never in a later task.

**D-2 — the app log is a third `logSink`, and its `done` is always false.**
`rawSink.write` returns `isTerminalEvent(line)`; `renderSink.write` sets `done`
when the event `type` is `"result"`. Both exist because a *run* ends. The app
log does not end — and `daemon.log` genuinely contains records whose `msg` is
`result`-ish text. An app-log sink must return `done == false` unconditionally,
and that must be stated in a comment (STYLE-1: say what would break if changed
the obvious way). `--only` does not apply to the app log and is out of scope.

**D-3 — `--level` means two different things and must never be conflated.**
On `logs` it is a *display-time* filter over records already on disk: it can
only hide, never reveal. On `daemon run` / `daemon start` it is a *write-time*
threshold passed to `logging.Options.Level`. They share a keyword vocabulary
(`debug|info|warn|error`) and a parser (`logging.ParseLevel`, which already
delegates to `slog.Level.UnmarshalText`) and nothing else. Do not introduce a
shared "level option" struct spanning the two commands; do not let `logs
--level` write anything anywhere. The stderr advisory (US-8) exists precisely
because users will confuse the two — it is a UX apology for an unavoidable
asymmetry, not a bug workaround.

**D-4 — `logging.ParseLevel` is the only level parser.** It exists
(`internal/logging/logging.go:99`), it returns `slog.LevelInfo` on empty, it
returns an error on garbage. Both `logs --level` and `daemon run --level` use
it. Do not write a second switch statement over level keywords, and do not use
`slog.Level.UnmarshalText` directly in `internal/cli`. An unparseable value is a
`*UsageError` (exit 2) at both sites.

**D-5 — no change inside `internal/launchd`.** US-6's plist half is entirely a
change to what `daemonProgram()` in `cmd_daemon_agent.go` returns.
`Manager.NewAgent`, `Agent.Validate` and `Agent.Render` are correct as they
stand and must not be modified; `Render` is byte-stable and its tests pin that.
`writeStartReport` already prints `strings.Join(agent.ProgramArguments, " ")`,
so the acceptance criterion "reports the effective level in its summary output"
is satisfied by the existing row — **do not add a new output row for it.**

**D-6 — truncation handling belongs to the app-log path, not to `tailLog`.**
`tailLog`'s doc comment states that run logs are append-only and never rotated,
"which is what makes holding the offset sufficient". Two acceptable resolutions,
in order of preference:

1. *Preferred:* the app-log follow gets its own loop, sharing `followInterval`,
   `followReadSize` and the partial-line `pending` technique but owning a
   stat-based size check that re-opens at offset 0 when the file shrinks. This
   leaves `tailLog`'s comment true and its behaviour untouched.
2. *Acceptable:* `tailLog` grows a truncation check applied to both sources —
   but then its doc comment is now false and **updating it is part of the
   change** (STYLE-1), as is re-justifying why holding the offset is still
   correct for runs.

What is not acceptable is duplicating the whole follow loop by copy-paste, or
silently leaving the comment as-is.

**D-7 — byte-identical `TextHandler` rendering requires an order-preserving
decode.** The spec promises the rendered line matches what
`slog.NewTextHandler` would have written. That promise is only keepable because
`logging.New` builds its handlers with `&slog.HandlerOptions{Level: opts.Level}`
and **nothing else** — no `AddSource`, no `ReplaceAttr`. Two consequences the
engineer must internalise:

- Decoding into `map[string]any` destroys attribute order and will break the
  promise on any record with two or more attributes. Use an order-preserving
  token-stream decode (`json.Decoder` with `Token()`), or an equivalent that
  preserves source order.
- `WithGroup` produces a nested JSON object; `TextHandler` produces
  dotted keys. Flattening nested objects to dotted keys is required, and it is
  the one place where the JSON and text forms are not a trivial 1:1.
- Timestamps differ between the two handlers (JSON carries nanosecond
  precision, text carries milliseconds). Re-rendering means re-formatting the
  timestamp, not copying the string through.
- `TEST-7` (determinism) applies to any map the renderer introduces — but note
  the *correct* order here is **source order**, not sorted order.
  `compactFields`' `sort.Strings(keys)` in `cmd_logs_render.go` is the
  determinism precedent to cite, **not** the ordering to copy.

**D-8 — a non-JSON or undecodable line is passed through verbatim and is not an
error.** `daemon.log` can contain a partially written final line, and a future
writer could put something else there. One bad line must never abort the view,
must never produce a Go error, and must never be silently dropped. This rule
applies identically to the tail pass and the follow pass.

**D-9 — every new source file joins `logsSourceFiles`.**
`internal/cli/cmd_logs_test.go` declares `var logsSourceFiles = []string{
"cmd_logs.go", "cmd_logs_render.go"}` with a doc comment saying it is "every
file the `logs` command is implemented in ... splitting the renderer out must
not be a way around them." Two structural guards iterate it:
`TestLogsNeverImportsRedact` (SEC-6: the local log copy is deliberately
unredacted, and the viewer must not start redacting it) and
`TestLogsUsesNoDaemonIPC` (bans `fsnotify`, `net/rpc`, `os/signal` imports and
asserts the lock file is never created). Adding the new files to that slice is
part of the story that creates them, not a follow-up.

**D-10 — the three tests that pin today's bare-`logs` behaviour change
deliberately.** See DEV-2. They are `TestRouterExitCodes`'s
`"missing required positional exits 2"` case and its `"--help"` case (which
asserts the literal `"logs <issue>"` appears in the top-level command list), and
`TestParseArgsArity`'s `"missing required positional"` case. Note the third one
runs against a *local* `logsFixture` in `parse_test.go`, not against the real
`newLogsCommand()` — it is testing `parseArgs`' arity logic generically, so
**it should keep `Min: 1` in its fixture and keep passing unchanged.** Only the
two `router_test.go` cases actually describe the design being changed.
`TestCommandsAreWellFormed` requires each `Args.Names` entry to appear in
`Usage` as `<issue>`; `logs [<issue>] ...` contains that substring, so it stays
green.

---

## Deviations

### DEV-1 — `daemon run --level` requires argv-aware logger construction in `main`

**Status:** proposed resolution below; engineer should implement the proposal
and record it in `.aiteam/AC_AMENDMENTS.md` as part of US-6. Not a blocker for
US-1..US-5 or US-7..US-9, which can proceed independently.

**The conflict.** `cmd/litefactory/main.go` builds the logger before the router
runs:

```go
logger := logging.New(logging.Options{Level: configuredLevel(), ...})
...
return cli.NewRouter(programName, env, cli.Commands()).Run(ctx, args)
```

`configuredLevel()` reads `config.Locate()` -> `config.Load()` ->
`logging.ParseLevel(cfg.Settings.LogLevel)`. By the time `runDaemonRun` is
reached, `env.Logger` is fixed, and `runDaemonRun` hands that exact logger to
`daemon.Setup{..., Logger: env.Logger, ...}`. Worse, `newDaemonRunCommand`'s
`New` is `func(*flag.FlagSet) Handler` — **the flag set parameter is unnamed and
discarded**, so today the command binds no flags at all.

The spec requires the override to reach `logging.New` before dispatch. There is
no way to do that from inside a handler. Something in `main` has to know about
`--level` before the router exists. That pushes command-line knowledge into a
package APP_CONTEXT documents as wiring-only, and it creates a second parse site
for one flag, which is uncomfortably adjacent to STYLE-6's "handlers MUST NOT
read the flag set back" (though it does not literally violate it — main is not a
handler).

**The impact.** US-6 only. If it is not resolved, `daemon run --level` either
does nothing (silently wrong — unacceptable) or must be dropped from the
feature, leaving `daemon start --level` working and `daemon run --level` absent,
which is an inconsistent surface.

**Options.**

- **(A) Recommended — declare in the command, resolve in main, through one named
  helper.** `newDaemonRunCommand` binds `--level` on its flag set as normal, so
  `daemon run --help` documents it and an unknown-flag error still behaves
  correctly; the handler ignores the captured value. Separately, `main` calls
  **one** narrow exported helper — e.g. `cli.LogLevelOverride(args []string)
  (slog.Level, bool, error)` living in `internal/cli` next to the command that
  declares the flag — and passes the result to `logging.New` in place of
  `configuredLevel()` when present. Cost: one flag is read in two places, which
  must be documented in a comment at both sites (STYLE-1) saying that the
  handler's copy is deliberately inert and why. Benefit: `cmd/` gains one
  function call, not a parser; the knowledge of the flag's name stays in
  `internal/cli`; help output and usage errors are unaffected.
- **(B) Rebuild the logger inside the handler.** `runDaemonRun` builds a fresh
  logger at the requested level and passes it into `daemon.Setup`. Cost: the
  pre-dispatch `logger.Debug("dispatch", ...)` record and anything else logged
  before the handler still use the config level; `env.Logger` and the daemon's
  logger diverge, which is a genuine footgun for the next person; the
  `logging.FileSink` would either be shared (fine) or duplicated (a second
  `O_APPEND` handle on the same file — avoid). Contradicts the spec's explicit
  "before the router dispatches".
- **(C) Drop `daemon run --level`, keep `daemon start --level`.** Cost: the
  surface is inconsistent and the foreground-debug use case — the main reason to
  want this at all — is unserved.

**Recommendation: (A).** Until someone says otherwise, (A) is what US-6 should
implement, with the AC_AMENDMENTS entry written in the same change.

### DEV-2 — the pinned bare-`logs` tests must change as a design change

**Status:** advisory, not blocking.

`TestRouterExitCodes` asserts that `litefactory logs` with no arguments exits 2
with `"missing argument: <issue>"`, and that top-level `--help` lists
`"logs <issue>"`. Both become false by design in US-1/US-4. These tests are
**not** in the TEST-10 protected family (`surface_test.go` / `policy_test.go` /
`purity_test.go` live in `config`, `git`, `github`, `gittest` and `identity`;
`internal/cli` has none), so editing them is legitimate. The deviation being
flagged is procedural: the edits must be made *because the design changed*, with
the new expectation stating the new contract ("bare `logs` shows the app log and
exits 0"), and not by deleting the case. Do not weaken
`TestCommandsAreWellFormed` or `TestParseArgsArity` to accommodate this — they
are testing generic parser behaviour and should keep passing untouched.

---

## Per-story architectural hints

### US-1 — `issue` becomes optional; bare `litefactory logs` shows the app log

**Architectural hints**

- **Pattern to follow:** the existing command-construction shape in
  `newLogsCommand()` (`internal/cli/cmd_logs.go:104`). `Command.New(fs)` binds
  flags and returns a closure capturing them (STYLE-6). The routing branch goes
  *inside* that closure.
- **Package placement:** the branch in `cmd_logs.go`; the app-log implementation
  in a new `internal/cli/cmd_logs_app.go`. Nothing goes in `internal/logging`,
  nothing goes in `pkg/`, and no new package is created.
- **Key interfaces:** `ArgSpec{Names: []string{"issue"}, Min: 0}`. With `Min: 0`
  the `missing argument: <issue>` branch in `parseArgs` becomes dead for this
  command — that is the point.
- **Watch out for:** **the panic.** The current closure ends
  `return runLogs(ctx, env, cmd, opts, args[0])`. `args[0]` on an empty slice
  panics. The `len(args) == 0` guard and the `Min` change are one atomic change.
  Also update `Usage` to `logs [<issue>] ...` — `TestCommandsAreWellFormed`
  requires `<issue>` to still appear as a substring, which `[<issue>]`
  satisfies.
- **Integration points:** D-1, D-10. The app-log path obtains its file from
  `config.Paths.DaemonLogPath()` and nowhere else (CFG-8).
- **NOT in scope:** a `logs daemon` subcommand (explicitly out of scope in the
  spec), any change to `Commands()`, any change to run-log resolution.

### US-2 — `-f` follow on the app log

**Architectural hints**

- **Pattern to follow:** `tailLog(ctx, sink, path, interval, finished)` in
  `cmd_logs.go` — specifically its *technique*: a held `io.ReadSeeker` with an
  explicit offset, `followInterval` (250ms) between polls, `followReadSize`
  (64 KiB) reads, and a `pending` buffer for a partial trailing line. It
  deliberately does not use `bufio.Scanner`; keep that.
- **Concurrency/context:** **no goroutine** (CONC-2 — its canonical text
  explicitly denies that a `logs -f` tailer goroutine exists). The loop runs on
  the command's own goroutine and selects on `ctx.Done()`. `context.Background()`
  is banned outside main (STYLE-2), so take `ctx` from the handler.
- **Key interfaces:** the app-log follow has **no terminal event**. `tailLog`'s
  `finished` concept and `isTerminalEvent` do not apply; a record whose `msg` is
  `result` must not stop the follow (D-2).
- **Watch out for:** truncation. See D-6 — prefer a separate loop that owns a
  stat-based size check and re-opens at offset 0, rather than falsifying
  `tailLog`'s "append-only and never rotated" justification.
- **Integration points:** shares `followInterval`/`followReadSize` constants with
  the issue path; shares the sink (US-3) and the filters (US-5, US-7).
- **NOT in scope:** file-system watching. `fsnotify`, `os/signal` and `net/rpc`
  are all banned in `logsSourceFiles` by `TestLogsUsesNoDaemonIPC`, and the new
  file joins that list (D-9). Polling is the design, not a compromise.

### US-3 — render re-parsed JSON records as stock `slog.TextHandler` text

**Architectural hints**

- **Pattern to follow:** `logSink` in `cmd_logs_render.go` is the extension
  point — a third implementation beside `rawSink` and `renderSink`. Follow
  `renderSink`'s division of labour: `write` decodes and decides, an `emit`-like
  helper produces the final line and is where filtering happens.
- **Package placement:** new file `internal/cli/cmd_logs_slog.go`, or appended
  to `cmd_logs_render.go`. Either way it joins `logsSourceFiles` (D-9).
- **Key interfaces:** `write(line []byte) (done bool, err error)` with `done`
  always false (D-2).
- **Watch out for:** all of D-7 — order-preserving decode (not
  `map[string]any`), group-to-dotted-key flattening, timestamp reformatting
  (RFC3339 nanos in JSON vs millis in text), and the fact that this promise is
  only keepable because `logging.New` sets no `AddSource` and no `ReplaceAttr`.
  If anyone ever adds `ReplaceAttr` to `logging.Options`, this renderer breaks —
  say so in a comment (STYLE-1).
- **Integration points:** consumed by both the tail pass (US-9) and the follow
  loop (US-2); filtered by US-5 before rendering and by US-7 after.
- **NOT in scope:** colorization (explicitly out of scope), `--only` kind
  filtering (issue-path only), any new rendering option flags.

### US-4 — bare invocation no longer prints usage; help stays at `logs --help`

**Architectural hints**

- **Pattern to follow:** the router already does the right thing —
  `errHelpRequested` causes `r.writeCommandUsage(r.env.Stdout, cmd)` and a nil
  return, i.e. help on **stdout**, exit 0 (`internal/cli/router.go`). Nothing
  about help routing changes; only the *no-argument* case changes.
- **Package placement:** `Usage` and `Long` strings in `newLogsCommand()`.
  `Long` needs a new paragraph describing the app-log mode — this story is the
  discoverability story, and the help text is its deliverable.
- **Watch out for:** `writeCommandUsage` rebuilds the flag set purely to
  enumerate flags, "which is why `Command.New` must have no effect beyond
  binding them". Any new flag (US-5, US-9) must be bound with no side effects —
  no file access, no path resolution, no `config.Locate()` inside `New`.
- **Integration points:** DEV-2 / D-10 — the two `router_test.go` expectations
  change here.
- **NOT in scope:** a global `--log-level` flag on the root command (explicitly
  out of scope).

### US-5 — `--level` as a display-time filter on `logs`

**Architectural hints**

- **Pattern to follow:** bind it in `func (o *logsOptions) bind(fs *flag.FlagSet)`
  alongside `run`, `raw`, `follow`, `only`, `grep`, `ignoreCase`, `invert`,
  `full`, `repo`. That method is the single place `logs` flags are declared.
- **Key interfaces:** parse with `logging.ParseLevel` (D-4); an unparseable
  value is a `*UsageError` -> exit 2. Compare the decoded record's level against
  the threshold; a record whose level is missing or unrecognised should be
  **shown**, not dropped (D-8's spirit: never silently lose a line).
- **Watch out for:** D-3. This flag cannot reveal anything that was never
  written. It must not touch config, must not write anything, and must not be
  persisted (persisting it is explicitly out of scope). Combined with `--raw`,
  the filter still applies — `--raw` changes the rendering, not the selection.
- **Integration points:** feeds US-8's advisory (the filter is what knows which
  levels were actually seen). Misusing `--level` on the *issue* path is a usage
  error, exit 2 — mirror the app-log-only/issue-only flag misuse rule
  symmetrically.
- **NOT in scope:** live mutation of the daemon's level, and any interaction
  with `settings.log_level`.

### US-6 — `--level` as a write-time flag on `daemon run` / `daemon start`

**This is the story carrying DEV-1. Read it before starting.**

**Architectural hints**

- **`daemon start` half (clean, do this first):** `daemonProgram()` in
  `internal/cli/cmd_daemon_agent.go` returns `[]string{abs, "daemon", "run"}`.
  Append `"--level", <keyword>` when the flag is set. `Manager.NewAgent` passes
  the slice through untouched (`internal/launchd/manager.go:105`), and
  `Agent.Validate()` already `checkPlistValue`s every element — the level string
  is validated for non-UTF-8 and control characters before it reaches XML, so no
  new sanitisation is needed. `writeStartReport` already prints the joined
  `ProgramArguments`, satisfying "reports the effective level in its summary"
  with **no new output row** (D-5).
- **`daemon run` half:** implement DEV-1 option (A). `newDaemonRunCommand`'s
  `New: func(*flag.FlagSet) Handler` currently discards its parameter — name it
  and bind `--level` so help and unknown-flag handling are correct, then have
  the handler ignore the value with a comment explaining that `main` resolved it
  before the logger existed (STYLE-1). Add the AC_AMENDMENTS entry.
- **Package placement:** `internal/cli/cmd_daemon.go` (flag binding),
  `internal/cli/cmd_daemon_agent.go` (program slice), `cmd/litefactory/main.go`
  (one call to the named helper). **No change in `internal/launchd`** (D-5).
- **Watch out for:** `configuredLevel()` in main falls back to `slog.LevelInfo`
  on *every* failure, silently. The override must sit *on top* of that
  fallback — an explicit `--level` beats config, config beats the default — and
  an unparseable `--level` must be a usage error, not another silent fallback.
- **Integration points:** `daemon.Setup{Logger: env.Logger}` is what actually
  consumes the level; nothing in `internal/daemon` changes.
- **NOT in scope:** persisting the level to `config.yaml`, a global root
  `--log-level`, changing the daemon's level while it runs.

### US-7 — carry `--grep` / `-i` / `--invert` over to the app log

**Architectural hints**

- **Pattern to follow:** reuse `lineFilter` and `newLineFilter(pattern string,
  ignoreCase, invert bool) (*lineFilter, error)` from `cmd_logs_render.go`
  verbatim. It returns `nil, nil` for an empty pattern, prefixes `(?i)` for
  case-insensitivity, and `(*lineFilter).match` is nil-safe. There is nothing to
  build here — this story is wiring, not construction.
- **Key interfaces:** the filter is applied to the **rendered** line, not to the
  raw JSON — that is `renderSink.emit`'s existing rule and it must hold for the
  app log too, or `--grep` would match JSON key names in one mode and human text
  in the other.
- **Watch out for:** with `--raw`, the "rendered line" *is* the JSON line;
  that is consistent, not a special case. An invalid regexp is a `*UsageError`
  (exit 2), same as today.
- **Integration points:** composes with US-5's level filter — level first (cheap,
  structural), grep second (on the rendered text).
- **NOT in scope:** any new filter flag, and any change to how `--grep` behaves
  on the issue path.

### US-8 — stderr advisory when the requested level was never captured

**Architectural hints**

- **Pattern to follow:** advisories go to `env.Stderr` while log content goes to
  `env.Stdout`, so piping `litefactory logs` stays clean. `fmt.Print*` is banned
  (STYLE-2) — write to the `env.Stderr` writer.
- **Key interfaces:** exit code stays `ExitOK`. This is an advisory, not a
  `*UsageError` and not a `*PreconditionError`. A user who filters to `debug` on
  an info-level log has done nothing wrong.
- **Watch out for:** the advisory is emitted **once**, after the initial tail
  pass, and must not fire repeatedly inside the follow loop. Phrase it so it
  points at the actual cause — the daemon wrote at a higher threshold — and at
  the fix (`daemon start --level debug`), which is the whole reason US-6 exists.
- **Integration points:** needs the "levels actually seen" observation from
  US-5's filter. That is a small piece of state owned by the app-log view, not a
  package-level variable.
- **NOT in scope:** inspecting `config.yaml` to *predict* what the daemon is
  writing, or contacting the daemon to ask. No IPC (D-9).

### US-9 — tail by default, because `daemon.log` is unrotated

**Architectural hints**

- **Pattern to follow:** bind `-n` (default 200) and `--all` in
  `logsOptions.bind` alongside the rest. `--all` and `-n` together, and either
  on the issue path, are usage errors (exit 2) — mirror the app-log-only flag
  misuse rule.
- **Key interfaces:** the tail computation reads backwards from the end of the
  file to find the Nth-from-last newline, then streams forward from there. It
  must never load the whole file to count lines — `daemon.log` is unrotated by
  design, and `internal/launchd/plist.go`'s `keepAlive = false` comment names
  "how an unrotated daemon.log (accepted M0 debt) fills a disk" as a known
  hazard. Assume it can be large.
- **Concurrency/context:** the backward scan is bounded I/O on the command's
  goroutine; check `ctx` between chunks if the scan can be long.
- **Watch out for:** the interaction with `-f`. The tail offset becomes the
  follow loop's starting offset — compute it once, hand it to the loop, do not
  re-seek. A file shorter than N lines starts at offset 0 with no error. A
  missing file prints `no litefactory log at <path> yet` and exits 0; with `-f`
  it waits for the file to appear rather than exiting (the file is created
  lazily by `FileSink` on first write, so "not there yet" is a normal state).
  **The reader must never create the file or its directory.**
- **Integration points:** US-2's follow loop, US-5/US-7's filters. Note the tail
  window is over *lines on disk*, applied before filtering — so `-n 200
  --level error` can legitimately show fewer than 200 lines. That is correct;
  do not try to fill the window post-filter.
- **NOT in scope:** log rotation, size caps, and any pruning of `daemon.log`
  (all explicitly out of scope — rotation remains accepted M0 debt).

---

## Platform lens (`.claude/skills/go/platform-constraints.md`)

- **Project layout** — applies. No new top-level package; everything lands in
  existing `internal/` packages. `pkg/` is not involved. No code is shared
  across `cmd/` binaries (there is one binary).
- **Error handling** — applies. New failure conditions are: unparseable level
  (`*UsageError`, exit 2), incompatible flag combination (`*UsageError`), and
  missing log file (**not an error** — exit 0 with a message). Wrap file I/O
  errors with `%w` and the path. No new sentinel error is needed; the existing
  `*UsageError` / `*PreconditionError` kinds cover everything (STYLE-5, STYLE-6).
- **Concurrency** — applies, and the answer is "none". No goroutine, no channel,
  no mutex is introduced. `ctx` is the first parameter of every blocking helper
  and the follow loop selects on `ctx.Done()`. CONC-2's inventory stays at four
  fixed goroutines and needs no amendment.
- **Interfaces** — applies. `logSink` already exists at the point of
  consumption with two implementations plus test doubles; adding a third is the
  opposite of premature abstraction. **Do not** define a new interface for the
  app-log source (TEST-5, and the skill's "avoid premature abstraction").
- **Dependency management** — applies negatively: **no new third-party
  dependency.** Everything needed is stdlib (`encoding/json`, `log/slog`,
  `os`, `time`, `regexp`). A JSON-to-text library, a tailing library, or a
  colour library would all be wrong here.
- **HTTP / API layer** — not applicable (CLI).
- **Database access** — not applicable (no DB entity, no migration; the spec
  says so explicitly).
- **Testing conventions** — applies. Table-driven, stdlib `testing`, no new
  assertion library and no mocking framework (TEST-1, TEST-2). The renderer is
  the highest-value unit-test target: feed it JSON lines produced by a real
  `slog.JSONHandler` and assert the output equals what a real
  `slog.TextHandler` produced from the same records — that is a golden test the
  stdlib generates for you, and it is the only honest way to test D-7's
  byte-identity promise. Determinism is required (TEST-7).
- **Logging & observability** — applies with a twist: this feature *reads* the
  log rather than writing one. `fmt.Print*` and `log.*` are banned (STYLE-2);
  command output goes to `env.Stdout` / `env.Stderr`. No metrics or tracing.
- **Linting & formatting** — applies. New files need `gofmt`; `gosec` will flag
  the new `os.Open` of `daemon.log` as G304, which needs a `#nosec G304` comment
  justifying it the same way the three existing ones in `cmd_logs.go` do
  ("litefactory's own artefact path, from `config.Paths`").

## Security lens

- **SEC-6 is the load-bearing rule.** "Outbound bodies MUST be redacted; the
  local `$LF_HOME/logs` copy deliberately MUST NOT be." `daemon.log` may contain
  material that would be redacted on its way to GitHub. Rendering it to a local
  terminal is exactly what that carve-out is for. **The viewer must not import
  `internal/redact`** — `TestLogsNeverImportsRedact` enforces this over
  `logsSourceFiles`, and the new files join that list (D-9). Equally: the
  rendered output must never be posted, uploaded or written anywhere.
- **SEC-10 unaffected.** `syscall.Umask(0o077)` stays the first statement of
  `main`. DEV-1's helper call goes *after* it. The app-log reader creates no
  file, so no permission decision is made.
- **CFG-8 enforced by construction.** `config.Paths.DaemonLogPath()` is the only
  route to the path. No `filepath.Join` against `$LF_HOME` in `internal/cli`.
- **Path traversal: none.** The path is derived, not user-supplied; there is no
  flag that names a log file.
- **Plist injection: already covered.** `Agent.Validate()` runs
  `checkPlistValue` over every `ProgramArguments` element, rejecting non-UTF-8
  and control characters, before `Render()`. Since the level is additionally
  validated by `logging.ParseLevel` at the CLI boundary (exit 2 on anything not
  in the keyword set), only four literal strings can ever reach the plist. No
  new escaping is needed and none should be added.
- **No secret is newly exposed.** The file already exists at 0600 under the
  umask; the reader shows the invoking user their own file. Reading a file the
  user could `cat` is not a new disclosure.
- **No IPC, no lock.** `TestLogsUsesNoDaemonIPC` asserts the lock file is never
  created by `logs` and that `fsnotify` / `net/rpc` / `os/signal` are never
  imported. Honour that as a security property, not just a test.

## Task-decomposition lens

A sequencing suggestion for the plan-orchestrator. **This is input to
decomposition, not a plan** — adding or removing tasks is the proxy product
owner's call, and I have not invented any work the spec does not contain.

- **US-1 and US-4 are one unit of work.** Splitting `Min: 0` from the
  `len(args) == 0` guard leaves a panic on `main` between two commits (D-1).
  The `Usage`/`Long` text and the two `router_test.go` expectations belong in the
  same change, because they describe the same design decision.
- **US-3 (the renderer) is the deepest and most independently testable piece.**
  It has no dependency on US-1's routing — it can be built and golden-tested
  against `slog` handlers before anything is wired to it. If parallelism is
  wanted, this is the piece to split off first.
- **US-9 then US-2** — tail before follow, because the follow loop consumes the
  tail's computed offset, not the other way round.
- **US-5 then US-8** — the advisory needs the filter's "levels seen"
  observation.
- **US-7 is small** (wiring an existing `lineFilter`) and can ride with US-5.
- **US-6 is independent of all the above** and can be worked in parallel — but
  it carries DEV-1, so it should be scheduled where a stall does not block the
  rest. Its `daemon start` half is safe to land on its own; its `daemon run`
  half is the part that waits on DEV-1's resolution.
- **D-9 (adding the new files to `logsSourceFiles`) belongs to whichever story
  creates each file**, not to a cleanup task at the end. A file that exists for
  even one commit without those guards is a file the guards did not check.

## Risks

- **R-1 (medium) — the byte-identity promise is stricter than it looks.**
  D-7 lists four ways to break it (attribute order, group flattening, timestamp
  precision, `HandlerOptions` drift). The mitigation is a golden test driven by
  real `slog` handlers rather than hand-written expected strings; if that test is
  awkward to write, the promise should be renegotiated rather than the test
  weakened.
- **R-2 (medium) — `daemon.log` is unrotated and can be large.** US-9's backward
  tail scan is the only thing standing between the user and reading a
  multi-gigabyte file into memory. Rotation stays out of scope, so this
  mitigation is the whole defence.
- **R-3 (low) — DEV-1's two parse sites can drift.** If someone later renames
  `--level` on `daemon run`, the helper in main must change with it. Mitigated by
  a comment at both sites (STYLE-1) and by the fact that they live in the same
  package.
- **R-4 (low) — the `logs` process logging into the file it reads** can look
  like a bug to a user running `logs -f --level debug`, since their own dispatch
  record appears. Accepted by the spec (A-12); the risk is a future engineer
  "fixing" it with suppression logic that would then hide real records.
- **R-5 (low) — `tailLog`'s comment.** If D-6 option 2 is chosen and the comment
  is not updated, the codebase acquires a false comment in exactly the place
  STYLE-1 warns about.

## Domain gaps

None. Every story in this spec is Go CLI, `log/slog`, file I/O and launchd
argument plumbing — all inside Go backend architecture. No frontend, infra or
data-modelling guidance is required, so no gap needs flagging to a specialist
this team does not have.
