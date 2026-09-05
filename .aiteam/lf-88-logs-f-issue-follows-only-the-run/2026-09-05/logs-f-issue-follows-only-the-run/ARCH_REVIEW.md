# Architect: Go Architect
# Created: 2026-09-05

## Verdict

**Approved for planning.** This is a cohesive CLI follow-lifecycle change. It
belongs entirely in the existing `internal/cli` command boundary, consuming
the existing `internal/store` and `internal/config` contracts. No schema,
daemon, agent-runner, IPC, watcher, package, or third-party dependency is
needed.

## System Overview

### How This Feature Fits Into the Existing System

`litefactory logs` is a local, synchronous CLI reader. Its command handler
resolves a task/session and run from SQLite, then renders or copies that run's
append-only JSONL file. Today its follow state is intentionally one open file
and one command goroutine. This feature changes the lifecycle above that file
tail: an implicit issue follow becomes a sequence of run follows; an explicit
ordinal follow remains one such follow only.

The CLI remains the consumer of the database and files; `internal/store`
continues to own every SQL statement and `internal/config` continues to own
the validated per-agent timeout. The daemon neither coordinates the follower
nor gains new responsibilities. Each refresh must open, use, and close its
own store handle so the follower never monopolizes the SQLite connection while
waiting.

The recorded run agent selects the successor-wait bound. That connects the
run lifecycle to the configured hard execution timeout without introducing a
new setting: after terminal handling for ordinal N, an implicit follower waits
at most N's recorded agent timeout for an ordinal greater than N. If a newer
run arrives, its own recorded agent supplies the bound for the next transition.

### New Packages / Components Introduced

None. The implementation should add a small follow-lifecycle coordination
layer within `internal/cli`, alongside the existing logs selection/tailing
code, rather than exporting a reusable package for one command.

### Key Boundaries

- `internal/cli` owns follow policy, output routing, cancellation treatment,
  file-handle lifetime, and deciding whether selection is implicit or pinned.
- `internal/store` owns current-run and ordinal-ordered session queries. CLI
  must consume store accessors; it must not add SQL or hold `*sql.DB`.
- `internal/config` owns typed Claude/Codex timeout configuration. CLI may map
  the persisted agent discriminator to its matching configured stanza but must
  not substitute the default agent or invent a fallback timeout.
- JSONL remains append-only and source-owned by the runner. Raw stdout stays
  source bytes only; the rollover notice belongs on stderr in raw mode and on
  the rendered output stream otherwise.

### Data Flow

`logs -f <issue>` resolves one session and its initial newest run -> the CLI
tails that run's JSONL -> on every existing poll cadence it observes file
growth and, while active, refreshed run status -> a terminal event or terminal
database state triggers one final drain -> explicit `--run` exits; implicit
selection polls the same session for the earliest higher ordinal until the
completed run's configured timeout expires -> it announces and tails the next
run -> repeats. Context cancellation exits the command directly from every
state.

### Concurrency & Context Notes

Keep this synchronous on the command goroutine. The existing ticker/select is
the lifecycle owner and must continue to observe `ctx.Done()` while waiting for
files, status, or a successor. Do not add a goroutine, filesystem watcher, or
daemon signal path; doing so would change the documented small goroutine budget
(CONC-2) and violate the specified polling design. Every store refresh takes
the caller context first (CONC-5); no detached `context.Background()` may be
created (STYLE-2).

The final-drain transition is important: database terminality permits flushing
an unterminated tail and ends waiting, but does not authorize dropping complete
bytes written immediately before the state update. A terminal JSONL record is
still a terminal observation even if display filtering suppresses its normal
rendered/raw output.

### Shared State and Side Effects

The follower reads shared SQLite state and append-only log files only. It
creates no durable state and does not alter daemon scheduling. The only new
observable side effect is a rollover separator before every subsequently
entered ordinal: rendered to normal output in rendered mode, and to stderr in
raw mode. It must bypass all event filters.

## Cross-Story Decisions

1. **Selection mode is captured from flag presence, not ordinal value.** The
   omitted `--run` sentinel may still select the newest initial run, but only
   omitted `--run` combined with `-f` enters the successor lifecycle. An
   explicit ordinal is permanently pinned, even if later ordinals exist.

2. **One shared per-run state machine serves both lifecycle modes.** It owns
   missing-active-log polling, retained-handle draining, terminal event
   recognition, refreshed database status, exactly one final drain, and partial
   tail flushing. The outer implicit-follow coordinator alone discovers and
   transitions to new ordinals. This avoids raw and rendered terminal behavior
   drifting.

3. **The successor deadline is per completed run and agent-specific.** Start
   it only after terminal handling of that run. The bounded interval is the
   configured hard timeout for the run's persisted `Agent`; do not use the
   command start time, the current default agent, or a global fixed duration.
   An absent/unrecognized historical agent/configuration is an actionable
   configuration error, never a silent fallback.

4. **Refreshes use the existing 250 ms cadence and short-lived store access.**
   Reopen and close around each observation/discovery operation. Do not retain
   a database handle or SQLite connection over the command lifetime; this
   preserves the current reader/daemon coexistence intent and the one-connection
   correctness constraint.

5. **Terminal database state is authoritative for liveness but not for byte
   loss.** On detecting it, drain once more before deciding to exit or begin
   successor waiting. A partial line is only emitted once terminality proves no
   further data is expected. The terminal event set remains `result`,
   `turn.completed`, and `turn.failed` in all output paths.

6. **Rollover output is mode-specific and filter-independent.** Rendered
   follow writes a visible zero-padded ordinal separator to stdout before that
   run's events. Raw follow writes source JSONL exclusively to stdout and the
   separator to stderr. `--only`, grep, case folding, and inversion do not
   suppress or transform the separator.

## Plan Integration Guidance

The Product Owner should keep this as one cohesive story unless task
assignment needs separate ownership. The minimum implementation task boundary
is the follow lifecycle/output integration in `internal/cli`; the regression
test task is co-located in the existing CLI logs test files. No integration-test
story is needed: those existing tests already use real SQLite and real files,
which is the required fidelity for this behavior.

## Architectural Hints for Anticipated Tasks

### Follow lifecycle and rollover behavior

**Agent:** Go implementation engineer

**Architectural Hints:**

- **Pattern to follow:** Evolve the existing `runLogs` -> selection -> `tailLog`
  arrangement into an outer issue-follow lifecycle plus a reusable per-run
  follower. Preserve the existing direct-file, ticker-driven design; do not
  route this through `daemon`.
- **Package placement:** Keep all behavior in `internal/cli`'s logs command and
  renderer files. `cmd/litefactory/main.go` remains wiring-only.
- **Key interfaces:** Consume concrete `config` values and concrete
  `store.DB` accessors. Do not introduce an interface merely to wrap one store
  implementation (TEST-5).
- **Concurrency/context:** Carry the command context through every open/query
  and waiting state. No new goroutine is justified (CONC-2, CONC-5).
- **Integration points:** Use the session ID and ordinal ordering already owned
  by `store`; use the persisted run agent to choose the corresponding configured
  timeout. Rollover writes to `Env.Stdout` or `Env.Stderr` according to raw
  semantics.
- **Watch out for:** Do not retain a store connection across the wait; do not
  make an implicit run selection behave like explicit `--run`; do not treat a
  missing log file for an active/newly allocated run as fatal; and wrap
  unexpected store/file failures with operation context while retaining their
  cause.
- **NOT in scope for this task:** daemon timeout changes, migrations, runner
  protocol changes, rotation handling, watchers, or persistent follower state.

### Follow regression coverage

**Agent:** Go unit-test engineer

**Architectural Hints:**

- **Pattern to follow:** Extend the existing in-package `internal/cli` logs
  tests and their real-file writer/follower rendezvous pattern. Table-drive the
  three terminal event types and run the raw/render paths that have distinct
  sinks (TEST-1).
- **Package placement:** Keep tests co-located with `cmd_logs.go` and
  `cmd_logs_render.go`; no new test package, mock framework, test module, or
  integration-test naming is appropriate (TEST-2, TEST-4).
- **Integration points:** Exercise a migrated temporary SQLite store and real
  JSONL files, including reopening status refreshes. This validates the exact
  store/file boundary rather than a fake approximation.
- **Concurrency/context:** Synchronize the writer only after the follower has
  demonstrably reached initial EOF; use channels and bounded test contexts, not
  timing-only sleeps. Follow tests must remain non-parallel because they share
  `LF_HOME` (TEST-8 and existing CLI test convention).
- **Watch out for:** Assert raw stdout is byte-exact source JSONL, separator is
  stderr only in raw mode, and separators survive all display filters. Cover a
  terminal DB row with no terminal event, final drain ordering, explicit-run
  pinning, ascending multi-run rollover, missing later-run file, successor
  deadline expiry, and cancellation.
- **NOT in scope for this task:** diagnosing the historical live `turn.failed`
  incident beyond deterministic regression coverage; the plan must not claim a
  root cause without reproduction evidence.

## Applicable Platform Constraints

- **Platform: Project layout** — applicable: all behavior stays in the existing
  private `internal/cli` package; no new top-level package is warranted.
- **Platform: Error handling** — applicable: unexpected store, file, and
  historical-agent configuration failures need contextual `%w` wrapping; the
  existing typed not-found/usage conventions remain the branch mechanism.
- **Platform: Concurrency** — applicable: the synchronous ticker state machine
  must honor command cancellation and must add no goroutine or unsynchronized
  shared state.
- **Platform: Interfaces** — applicable: consume concrete config/store types;
  no premature abstraction for one command.
- **Platform: Dependency management** — not applicable: no dependency needed.
- **Platform: HTTP/API layer** — not applicable: local CLI only.
- **Platform: Database access** — applicable: all SQL remains in
  `internal/store`, queries receive `ctx`, and the SQLite pool configuration is
  unchanged; no migration is required.
- **Platform: Testing conventions** — applicable: table-driven, stdlib,
  co-located real-file/SQLite regression tests; `go test -race` remains the
  relevant verification.
- **Platform: Logging and observability** — not applicable: user-requested
  command output is not application logging; use command output writers, not a
  logging library.
- **Platform: Linting and formatting** — applicable: plan verification must
  include `gofmt`, `go vet`, the pinned linter, and the race-enabled test suite.

## Standards and Deviations

No deviation is requested. The plan is compatible with CFG-6 (reuse validated
timeouts), TEST-1/TEST-2/TEST-4/TEST-5, CONC-2/CONC-5/CONC-6, and
STYLE-2/STYLE-6/STYLE-7. In particular, CLI polling is part of this command's
existing behavior rather than a daemon retry, and no configuration surface is
added.
