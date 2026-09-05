# Research Brief — current-and-future-usage-of-slash

## Request

[O] Remove the unused slash-command vocabulary, then add a factory-owned
`/schedule` command which postpones execution of the supplied instruction. The
requested forms are `/schedule -by hh:mm:ss` for a delay and `/schedule -to
mm/dd/yyyy hh:mm:ss` for an absolute time. Scheduling must not be delegated to
or interpreted by the selected coding agent. Logging should expose a
`scheduled=<#>` field.

## Codebase Context

[V] GitHub-owner comments are parsed centrally before they enter a run. The
parser recognizes the first non-whitespace token, normalizes its case, keeps the
remaining text as the instruction, and turns it into a closed command/mode
vocabulary (`internal/turn/command.go:139`, `internal/turn/command.go:185`,
`internal/turn/command.go:217`). The present actionable commands are `/spike`,
`/research`, `/plan`, `/implement`, and `/validate`; `/approve`, `/include`,
`/reset`, `/retry`, and `/status` are recognized only to return an
unimplemented-command reply (`internal/turn/command.go:15`,
`internal/turn/command.go:192`, `internal/turn/command.go:204`).

[V] The daemon owns time and dispatch. Its tick discovers work, processes
comments in order, and is the sole sanctioned reader of the clock
(`internal/daemon/tick.go:112`, `internal/daemon/tick.go:236`,
`internal/daemon/tick.go:1151`; `internal/daemon/clock_test.go:20`). A turn
intentionally has no clock; timing therefore belongs in the daemon rather than
the agent or turn executor (`internal/turn/execute.go:357`).

[V] Existing durable state covers issue/comment watermarks, runs, and a
`needs_poll` backlog flag, but records neither a scheduled comment nor a due
instant (`internal/store/migrations/0001_init.sql:44`,
`internal/store/migrations/0002_session_backlog.sql:9`,
`internal/store/migrations/0004_mode_selections.sql:1`). SQLite is the
exclusive store, migrations are embedded numbered SQL, and the one-connection
pool is a correctness requirement (`internal/store/store.go:85`,
`internal/store/migrate.go:35`).

[V] A comment is consumed only after the relevant handling completes. An
unconsumed comment stops processing of later comments on that issue, and the
backlog bit makes the daemon revisit it (`internal/daemon/tick.go:1165`,
`internal/daemon/tick.go:1179`, `internal/daemon/tick.go:1202`). This gives a
scheduled item a significant ordering effect unless the design changes that
queue policy.

[V] Prompt preparation supplies parsed live instruction text to the agent and
frames historical comments as context; agent adapters do not own scheduling or
dispatch (`internal/turn/prepare.go:215`, `internal/turn/prepare.go:234`,
`internal/harness/prompt.go:47`, `internal/codex/runner.go:126`).

## Domain / Problem Space

[V] `/schedule` fits the existing factory-side control-command boundary. Like
the current explicit mode commands, Litefactory should parse and validate it
before a work agent is invoked. A commandless comment alone is eligible for LLM
mode classification, and that classification is constrained to the existing
closed mode vocabulary (`internal/daemon/tick.go:1413`,
`internal/daemon/mode.go:66`, `internal/codex/runner.go:91`). Explicit
slash-command interpretation is deterministic Go behavior, not an LLM choice.

[O] The desired operational outcome is to preserve scarce agent context windows
for work at an appropriate time, such as overnight. This makes a durable,
restart-safe schedule important: in-memory deferral could be lost after a poll
cursor has advanced, whereas the existing backlog migration explicitly treats
durability as necessary for later re-discovery (`internal/store/migrations/0002_session_backlog.sql:9`).

[V] The proposed flag spellings fit the current first-token command grammar,
but the remainder is currently opaque text; neither duration nor timestamp
validation exists (`internal/turn/command.go:139`, `internal/turn/command.go:185`).
The absolute format lacks a timezone while stored timestamps are UTC RFC3339
milliseconds (`internal/store/store.go:202`). Normal daemon polling occurs at
10–60 second intervals, so a tick-driven implementation would start on the
first poll at or after a due time unless a separate wake-up mechanism were
introduced (`internal/config/validate.go:17`).

## Constraints & Risks

- [V] Removing the five reserved commands is a parser/reply-contract change:
  today they are deliberately reported as unimplemented, whereas after removal
  they would normally be unknown slash commands (`internal/turn/command.go:15`,
  `internal/turn/command.go:204`). Existing parser and reply tests need the
  intended behavior specified.
- [V] A scheduled item needs durable identity, due time, and state before a run
  is allocated. Existing run uniqueness prevents duplicate *runs* only after
  allocation; it cannot make pre-run deferral restart-safe
  (`internal/store/runs.go:175`, `internal/store/runs.go:380`).
- [V] If scheduled comments remain unconsumed until due, the existing ordered
  watermark design blocks later comments for the same issue. That may be a
  useful ordering guarantee or an unacceptable delay; it must be an explicit
  product decision (`internal/daemon/tick.go:1151`, `internal/daemon/tick.go:1165`).
- [V] The exact semantics of `scheduled=<#>` are undefined. Existing structured
  logs expose correlated task/repository fields and a separate
  `deferred_sessions` capacity metric, but no schedule field
  (`internal/daemon/logging_test.go:19`, `internal/daemon/tick.go:177`).
- [V] Owner authorization and central input validation already protect command
  dispatch (`internal/turn/classify.go:220`, `internal/turn/classify.go:245`),
  but schedule flags and time values require their own strict validation before
  state is persisted.
- [V] The repository has deterministic injected-clock, migration, queue,
  concurrency, parser, and logging test seams suited to this work
  (`internal/daemon/daemon.go:202`, `internal/daemon/clock_test.go:20`,
  `internal/turn/command_test.go:5`, `internal/store/migrate.go:24`).

## Open Questions

1. Does the instruction follow the time specification in the same `/schedule`
   comment? This is the natural fit for the current parser and is assumed below.
2. Which timezone applies to `-to mm/dd/yyyy hh:mm:ss`? The specified format is
   ambiguous; UTC would align with persistence/logs, while a configured local
   timezone may better match operator expectations.
3. Should `-by` accept only `HH:MM:SS` below 24 hours, or a duration form that
   can represent multi-day delays? The stated "overnight" example fits either.
4. Is due-time precision "first regular poll at or after due," or must the
   daemon wake at the exact instant?
5. Should a scheduled comment block later comments on the same issue, preserving
   current strict ordering, or should later work be allowed to proceed?
6. What does `scheduled=<#>` represent: a count of outstanding schedules, a
   schedule/run ordinal, a comment identifier, or a due timestamp? A count is
   the closest fit to the requested numeric placeholder and existing aggregate
   logging style, but this is an inference.
7. Should scheduling immediately acknowledge the owner, acknowledge only when
   work begins, or do both?

## Recommended scope for Plan mode

Plan a Litefactory-only scheduling lifecycle: remove the five reserved command
names; introduce an explicit schedule command parser and strict flag/time
validation; persist scheduled-comment state and due time using a new embedded
store migration; make daemon ticks identify and execute due items safely across
restarts; define ordering, idempotency, reporting, and a precise
`scheduled=<#>` log meaning; and cover parser, migration, due-time,
restart, queue-order, duplicate, and logging behavior with deterministic tests.

Do not change agent prompts or agent-runner contracts beyond ensuring the agent
receives the original work instruction only when the schedule is due. Do not
introduce a separate external scheduler unless exact wake-up semantics are
explicitly required.
