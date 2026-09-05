# Research Brief — current-and-future-usage-of-slash

## Request

[O] Remove the unused slash-command vocabulary, then add a factory-owned
`/schedule` command which postpones execution of the supplied instruction. The
requested forms are `/schedule -by hh:mm:ss` for a delay and `/schedule -to
mm/dd/yyyy hh:mm:ss` for an absolute time. Scheduling must not be delegated to
or interpreted by the selected coding agent. Logging should expose a
`scheduled=<#>` field. The owner clarified that this count means outstanding
scheduled items; scheduled work must not block later comments; absolute times
default to the daemon device's local timezone but can state an explicit zone
such as `UTC`; and `/schedule --propose` should ask a coding agent for a
schedule that `/schedule --accept` can arm. The owner also asked whether
`/implement <storyid>` already works.

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

## Follow-up findings and owner decisions

[V] There is no Litefactory timezone setting. Therefore the stated device
default means the daemon process's OS-local timezone, rather than a project
configuration value. An explicit zone needs a grammar and validation policy;
UTC is a valid explicit case.

[O] A scheduled comment must not block later work. [V] The existing
unconsumed-comment strategy cannot satisfy that rule because its watermark
stops at the first outstanding comment (`internal/daemon/tick.go:1151`,
`internal/daemon/tick.go:1165`). A schedule command must be durably recorded
and consumed immediately, with due work dispatched later through a separate
path. The existing `needs_poll` state is session-level rediscovery, not a
per-comment schedule (`internal/store/migrations/0002_session_backlog.sql:4`).

[V] The factory has a narrow agent-call precedent in free-text mode
classification: it is stateless, tool-free, and accepts one exact mode token
(`internal/daemon/mode.go:55`, `internal/daemon/mode.go:66`,
`internal/harness/runner.go:20`, `internal/codex/runner.go:100`). It cannot
carry a multi-item schedule. A normal work run allocates a run/worktree and has
mode permissions. Consequently `--propose` needs its own bounded agent-call
contract, factory-owned proposal format/report, and durable proposal record for
later acceptance and restart safety.

[V] `/implement <storyid>` does work as a mode/instruction handoff: the parser
retains `T-001.2` as opaque instruction text (`internal/turn/command.go:201`,
`internal/turn/command.go:217`; `internal/turn/command_test.go`), and prompt
preparation supplies it to the agent (`internal/turn/prepare.go:228`,
`internal/turn/prepare.go:248`). Litefactory does not validate the story ID,
read the plan, or enforce plan-artifact existence. Those responsibilities are
left to the selected agent's Implement-mode workflow (`CLAUDE.md:44`).

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
- [V] Proposal/acceptance is an additional lifecycle: each proposal needs an
  issue/owner association and a terminal accepted, replaced, expired, or
  rejected state. Factory comments are self-filtered, so `--accept` must be a
  new owner comment (`internal/report/marker.go:45`, `internal/report/marker.go:80`).
- [V] Due work is naturally eligible on the first poll at/after its due time and
  can still await capacity. The existing `in_flight` and `deferred_sessions`
  metrics are distinct from the requested outstanding `scheduled` count
  (`internal/daemon/tick.go:77`, `internal/daemon/tick.go:177`).

## Open Questions

1. Does the work instruction follow the time specification in the same
   `/schedule` comment? This remains the natural parser fit and is assumed.
2. What exact explicit-zone syntax should be accepted in addition to the
   OS-local default: a trailing IANA zone/`UTC`, or a named flag?
3. Should `-by` accept only `HH:MM:SS` below 24 hours, or a duration form that
   can represent multi-day delays? The stated "overnight" example fits either.
4. Should `--accept` apply to the latest proposal for the issue, or name a
   proposal identifier?
5. What should a proposed schedule contain: due times only, ordering only, or
   one concrete scheduled work item per story?
6. Should scheduling, proposal, and acceptance each have immediate factory
   replies in addition to normal due-work reporting?

## Recommended scope for Plan mode

Plan a Litefactory-only scheduling lifecycle: remove the five reserved command
names; introduce an explicit schedule command parser and strict flag/time
validation; persist scheduled-comment state and due time using a new embedded
store migration; consume scheduling control comments immediately so later work
can continue; make daemon ticks identify and execute due items safely across
restarts; define local-default and explicit-zone parsing, idempotency,
reporting, and the outstanding-count `scheduled=<#>` meaning; and cover parser,
migration, due-time, restart, non-blocking queue, duplicate, timezone, and
logging behavior with deterministic tests.

Do not change agent prompts or agent-runner contracts beyond ensuring the agent
receives the original work instruction only when the schedule is due. Do not
introduce a separate external scheduler unless exact wake-up semantics are
explicitly required. Plan a separate bounded factory-controlled proposal call
and durable proposal/acceptance lifecycle, rather than reusing normal work runs
or the one-word mode classifier.

## Post-implementation audit

[V] The completed scheduling implementation is present in commit `c6fb438`,
not in the final commit `9f6bd43`. Relative to `ae84e43`, `c6fb438` changes 34
non-artifact production/test files (+2,520/-94 lines); `9f6bd43` changes only
two session artifact files. The working tree is clean because both commits are
already on `lf/100-current-and-future-usage-of-slash` and its tracked origin.

[V] The implementation has deterministic `/schedule` recognition in
`internal/turn/command.go:13-17,195-205`, strict control grammar in
`internal/schedule/schedule.go:42-235`, durable schedule/proposal tables in
`internal/store/migrations/0005_schedules.sql:1-52`, direct control handling
and replies in `internal/daemon/tick.go:1348-1405`, and tick-driven stored
payload dispatch in `internal/daemon/schedules.go:17-161`.

[V] The configured coding agents do not infer schedule syntax. Only
`--propose` invokes a bounded structured advisor; the Claude route is tool-free
and the Codex route is read-only (`internal/agent/proposal.go:13-22,55-72`,
`internal/harness/runner.go:70-112`, `internal/codex/runner.go:143-185`).

[V] The previous run report was inaccurate in saying no commit or push occurred.
This audit corrects that record. The source audit did not rerun the full test
suite; it verified committed code and tests only. A single integrated test from
inbound scheduling comment through reply, persistence, and later due dispatch
was not evident in the focused test sources inspected.
