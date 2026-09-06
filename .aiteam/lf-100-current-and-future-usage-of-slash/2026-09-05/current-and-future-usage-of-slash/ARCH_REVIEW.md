# Architect: Go Architect
# Created: 2026-09-05

## Verdict

**Approved for planning, with the proposal-agent boundary made an explicit
workstream.** Scheduling is factory state, not an agent workflow mode. The
feature fits the existing `daemon -> turn/report -> store` path provided that
the daemon remains the clock/dispatch owner and all schedule state is durable
in `internal/store`.

## System Overview

### How This Feature Fits Into the Existing System

An owner comment first travels through the existing `turn.ParseCommand` front
door and the daemon's per-session comment drain. `/schedule` needs recognition
there, but its grammar and state transition are factory concerns: after owner
admission, `internal/daemon` resolves and validates the scheduling request,
persists it through `internal/store`, posts an immediate rendered reply, and
only then consumes the triggering comment. This keeps later comments eligible
and preserves the existing watermark safety rule.

On every normal daemon tick, the daemon additionally reads durable pending
scheduled work and expires pending proposals using its injected clock. It
selects only eligible records in deterministic due-time/durable-ID order,
reserves one before dispatch, and sends only the stored work payload through
the existing normal `turn` execution path. A scheduled item is not a comment
queue entry while it waits, so it cannot block subsequent issue comments.

The design must preserve the package map: SQL and transactions stay in
`internal/store`; outbound wording stays in `internal/report`; posting and
watermark commits stay in `internal/turn.Reporter`; `internal/daemon` owns
when a due item is checked and offered to the existing bounded worker pool.
No change belongs in `cmd/litefactory` beyond dependency wiring.

### New Packages / Components Introduced

- `internal/schedule` (new, leaf): pure schedule-command grammar, civil-time
  resolution/validation, and factory-owned request/proposal value types. It
  must not open the database, post to GitHub, run an agent, or read the clock.
- `internal/store` schedule accessors and one embedded migration: authoritative
  scheduled-work, prerequisite, proposal, and proposal-item persistence.
- `internal/report` schedule reply shapes: factory confirmations and refusals
  rendered from safe, structured data rather than agent prose.
- `internal/daemon` schedule coordinator: consumes scheduling controls,
  performs immediate reply/consumption, sweeps expiry and blocked dependencies,
  reserves due work, counts pending work, and offers it under normal capacity.
- `internal/agent` proposal capability, if needed after repository inspection:
  a narrowly scoped, vendor-neutral factory-controlled structured proposal
  interaction. Concrete adapters remain in `internal/harness` and
  `internal/codex`; it is not a workflow run and must not reuse a normal
  task-run/segment lifecycle merely because both launch a coding agent.

### Key Boundaries

- `turn` recognizes `/schedule` as a non-work command and exposes only safe
  parsed control text to the daemon; it does not decide timing, dependencies,
  proposal state, or eligibility.
- `schedule` parses command syntax and validates local-or-IANA-zone civil time
  before persistence. It receives the daemon-supplied receipt time and local
  location as values, never calls `time.Now` or discovers machine state.
- `daemon` applies owner/session context, invokes store transitions, invokes a
  proposal-capability adapter, and is the sole scheduled dispatcher. It does
  not embed SQL or render Markdown.
- `store` owns all schema, atomic transitions, idempotency keys, prerequisite
  checks, reservation, and durable ordering. Store APIs accept `ctx` first
  (CONC-5); SQLite remains one connection.
- `report` owns user-visible confirmation/refusal text. The posting owner
  stays `turn.Reporter`, so every schedule reply uses the established
  render → post → durable-record/consume ordering (CONC-4, SEC-5, SEC-6).
- The proposal agent returns a strict factory-owned data contract, not an
  executable instruction. The daemon validates every returned item exactly as
  if an owner had supplied it, before store persistence.

### Data Flow

1. Owner comment → existing filter → `ParseCommand` identifies `/schedule`.
2. Daemon resolves a clock request, dependency request, proposal request, or
   proposal decision; pure parsing rejects malformed/nested controls.
3. For a direct schedule, store atomically creates/reuses the source-comment
   result. For accept/reject/propose, store atomically records the lifecycle
   outcome before it becomes visible as successful.
4. Reporter renders and posts the immediate reply, then records the reply and
   advances the source watermark. A post failure leaves the comment unconsumed
   and the durable result available for idempotent retry.
5. A later tick reads pending state at the daemon clock, transitions failed-
   prerequisite dependents to `blocked`, expires proposals, counts pending
   scheduled work, and reserves eligible work deterministically.
6. The daemon offers a reserved scheduled record to the existing worker-pool
   path; the execution request uses its stored payload only. Completion feeds
   back to the scheduled-work lifecycle so dependents become eligible or
   blocked, with required factory replies posted through the Reporter.

### Concurrency & Context Notes

Do not create a scheduler goroutine or timer per item. The existing tick and
worker-pool budget are the feature's timing and capacity mechanism (CONC-2).
The daemon's injected clock is the only source of “now”; schedule parsing and
expiry are direct testable calls. Any database or proposal-agent operation
carries the tick/worker context. Scheduled-work selection and the pending →
running reservation must be one durable compare-and-transition so duplicate
ticks/restarts cannot launch it twice. The store's one SQLite connection is a
correctness constraint, not a tuning target.

### Shared State and Side Effects

Durable shared state comprises scheduled-work records, prerequisite edges,
proposal records/items, source-comment idempotency keys, and schedule reply
recording. GitHub side effects are immediate factory replies and existing
reactions; work execution has its usual run report. Logs gain
`scheduled=<count>`, where count is only pending scheduled work (both
clock-waiting and prerequisite-waiting), excluding running and terminal rows.

## Cross-Story Decisions

1. **Clock ownership:** default `-to` resolution uses `time.Local` supplied by
   the daemon at command receipt; a trailing explicit `UTC` or IANA location
   overrides it. Persist the resolved UTC instant only. Reject DST folds/gaps;
   never silently choose one.
2. **Batch syntax:** `/schedule --after <id[,id...]> -by HH:MM:SS <payload>`
   is the single chaining form. Its delay starts at the maximum successful
   prerequisite completion time. It is not a second timer layered in a worker.
3. **Non-blocking queue:** successful direct schedule/proposal decision replies
   consume the control comment. Waiting schedule records are outside the
   watermark queue, allowing later ordinary comments and later schedules.
4. **Atomic idempotency:** source comment identity is unique for every direct
   control result and proposal creation; proposal acceptance is a single store
   transaction that transitions exactly one pending proposal and creates all
   child schedules. Replays return the durable prior result, never another
   reply, schedule, or run.
5. **Proposal trust boundary:** a proposal is advisory agent output. Only
   validated concrete items get persisted after explicit owner `--accept <id>`;
   an agent cannot schedule, choose lifecycle status, or dispatch work.
6. **Reporting:** direct controls, proposal decisions, expiry/blocked outcomes,
   and scheduled-run terminal transitions all render through `report` and post
   through `turn.Reporter`, preserving redaction and post-first persistence.

## Task Decomposition and Architectural Hints

### T-100.1: Remove obsolete command vocabulary and introduce the schedule control boundary

**Agent:** Go implementation engineer

**Architectural Hints:**

- **Pattern to follow:** Retain `turn.ParseCommand` as the pure first-token
  classifier. Remove the reserved unsupported vocabulary and recognize
  `/schedule` distinctly from actionable work commands; unknown commands keep
  the existing unknown-command reply behavior.
- **Package placement:** Parser recognition belongs in `internal/turn`; detailed
  schedule grammar belongs in new `internal/schedule`, not in daemon tick code
  and not in `internal/report`.
- **Key interfaces:** Preserve `Command` as a value consumed by daemon and
  reply paths; introduce a schedule-control classification that cannot be
  mistaken for a work-spawning command.
- **Integration points:** Update the `turn` → `report` reply-kind mapping and
  command/parser tests together. Do not change agent mode parsing; `/implement
  <storyid>` remains an opaque payload once schedule parsing strips its flags.
- **Watch out for:** The command parser never fails. Malformed schedule syntax
  must become a factory refusal with no state change, not free text passed to
  a coding agent. Preserve `lf: ask` removal semantics.
- **NOT in scope for this task:** Durable data model, tick eligibility, or an
  agent proposal implementation.

### T-100.2: Add durable scheduled-work and dependency persistence

**Agent:** Go implementation engineer

**Architectural Hints:**

- **Pattern to follow:** Add one hand-written embedded migration and schedule
  accessors under `internal/store`, following `runs`/`watermarks` transaction
  patterns. All SQL stays there; every accessor takes `ctx` first (CONC-5).
- **Package placement:** New schedule entity/accessor files and migration are
  `internal/store`; dependency validation/service orchestration belongs above
  it in daemon.
- **Key interfaces:** Expose concrete store value types and narrow operations
  for create-or-return-by-source, prerequisite lookup/validation support,
  deterministic eligible listing, atomic reservation, terminal transition,
  pending count, and block propagation. Keep transaction-shaped operations
  inside the store rather than composing several public writes in daemon.
- **Concurrency/context:** Make pending → running reservation conditional and
  durable. It is the sole cross-restart at-most-once dispatch barrier.
- **Integration points:** Refer to registered repository/session identity and
  existing run identity without duplicating repo/task tables. Prerequisite rows
  must enforce same-session ownership structurally where SQLite can, and be
  rechecked in the atomic write path.
- **Watch out for:** `store.Open` does not migrate; migration test coverage and
  real SQLite transaction tests are mandatory (TEST-6). Do not raise the
  one-connection cap or introduce a migration library.
- **NOT in scope for this task:** Posting GitHub replies or interpreting agent
  output.

### T-100.3: Implement direct scheduling controls and immediate replies

**Agent:** Go implementation engineer

**Architectural Hints:**

- **Pattern to follow:** Extend daemon comment handling with a factory-control
  branch after authorization and before normal run dispatch. Parse against the
  daemon-provided receipt time/local location, persist idempotently, then use
  the reporter to render/post/consume.
- **Package placement:** Command orchestration and timezone source are in
  `internal/daemon`; safe response wording and reply data are in
  `internal/report`; no SQL outside `internal/store`.
- **Key interfaces:** Evolve the existing daemon Reporter seam only as needed
  to support schedule confirmations/refusals while retaining concrete
  `turn.Reporter` transaction ownership. New reply shapes must remain
  structurally unable to echo unsafe input (SEC-5).
- **Integration points:** Use the normal owner admission filter, existing
  acknowledgement behavior, watermark advancement, redactor, and source
  comment identity. A schedule confirmation includes durable ID, resolved
  UTC/due presentation or dependency rule, and pending count.
- **Watch out for:** A successful state write without a successful post must
  remain replayable, and a post must happen before consuming the comment
  (CONC-4). Repeated delivery must use the stored prior outcome and avoid
  duplicate factory comments.
- **NOT in scope for this task:** Due-work polling or proposal acceptance.

### T-100.4: Dispatch due work, dependency outcomes, and scheduled metric

**Agent:** Go implementation engineer

**Architectural Hints:**

- **Pattern to follow:** Add scheduled maintenance and dispatch to the ordinary
  daemon tick, using the existing injected clock, pool, in-flight accounting,
  and fair offer mechanism. There is no background scheduler and no sleep
  beneath daemon (CONC-2, CONC-6).
- **Package placement:** Eligibility/expiry/blocking policy belongs in
  `internal/daemon`; query/order/reservation/count belongs in `internal/store`;
  execution continues through existing `turn` contracts.
- **Key interfaces:** Convert a reserved scheduled item into a normal turn
  request without passing schedule flags or metadata to the agent. Associate
  the resulting work run with the scheduled record for terminalization.
- **Concurrency/context:** Deterministic due-UTC then schedule-ID selection is
  required before offers. Capacity refusal leaves a record pending/reservable
  for a later tick; never mark it running before it has an allocated execution
  path. Blocking propagation must be idempotent across ticks.
- **Integration points:** Tick completion logging adds `scheduled` beside
  existing `in_flight` and `deferred_sessions`. Factory-generated blocked and
  no-longer-eligible replies use the same Reporter redaction/post-first path.
- **Watch out for:** A scheduled item must not join the per-session comment
  queue while waiting; it must not bypass daemon capacity or overlap a session
  that is already in flight. Tests rendezvous and use `Quiesce`, never sleeps
  (TEST-8, TEST-9).
- **NOT in scope for this task:** Natural-language scheduling or exact
  wall-clock wakeups.

### T-100.5: Add durable proposal generation, acceptance, and rejection

**Agent:** Go implementation engineer

**Architectural Hints:**

- **Pattern to follow:** Treat `--propose`, `--accept <proposal-id>`, and
  `--reject <proposal-id>` as a finite store-owned state machine. A new pending
  proposal replaces earlier pending proposals for the same owner/issue; seven-
  day expiry is evaluated by the daemon clock.
- **Package placement:** Proposal records/items and atomic accept/reject are in
  `internal/store`; lifecycle orchestration is in daemon; response rendering is
  in report. Agent capability contracts live in `internal/agent` with adapters
  only in `harness`/`codex`.
- **Key interfaces:** Add a narrowly consumed proposal-generator interface at
  the daemon boundary, declared by the consuming package (TEST-5). Its result
  is a structured value contract, never a raw schedule string or arbitrary
  prose. The default configured agent is the only candidate invoked.
- **Concurrency/context:** Proposal generation is bounded by the caller
  context and existing agent timeout policy; it must not create an unbounded
  goroutine. Acceptance is one transaction and only one caller can change a
  pending proposal to accepted.
- **Integration points:** Before inserting a proposal, daemon validates every
  returned mode/payload/time/dependency graph through the same factory rules as
  direct scheduling. Acceptance writes all ScheduledWork rows atomically and
  returns their IDs/count in the immediate reply.
- **Watch out for:** Do not model proposal generation as a normal task run:
  that would allocate task run ordinals, contaminate a coding conversation, and
  make a non-work factory interaction look like work. No new external binary
  behavior is needed unless adapter analysis proves it; if invocation changes,
  follow EXT-1/EXT-2.
- **NOT in scope for this task:** Allowing the agent to persist schedules or
  dispatch them.

### T-100.6: Verify behavior across parser, store, reporter, daemon, and agent seams

**Agent:** Go unit-test engineer

**Architectural Hints:**

- **Pattern to follow:** Table-driven parser/timezone/dependency/proposal-state
  tests; existing daemon and turn fixtures for end-to-end control comments;
  real SQLite for all atomicity/migration claims (TEST-1, TEST-3, TEST-6).
- **Integration points:** Cover restart/replay, later-comment non-blocking,
  due-time ordering, capacity delay, successful/failing prerequisites, and
  owner/issue-scoped proposal decisions. Assert agents see only payload.
- **Concurrency/context:** Use fake injected clocks and channel rendezvous,
  `Daemon.Quiesce`, and repeated real ticks; do not use wall-clock sleeps or
  invent a worker-slot observable (TEST-8, TEST-9).
- **Watch out for:** Surface tests may deliberately pin package APIs and daemon
  clock reads (TEST-10). Update such tests only as an intentional documented
  design change.
- **NOT in scope for this task:** Live agent-process tests or a new mocking
  framework (TEST-2, EXT-1).

## Platform Constraints Applied

- **Platform: project layout** — all new application code stays under
  `internal/`; `cmd/litefactory` remains wiring-only.
- **Platform: database access** — use embedded hand-rolled migrations,
  parameterized context-aware SQLite accessors only; one connection remains.
- **Platform: error handling** — typed/sentinel store conditions support
  branching; external/reportable bad outcomes remain values (CONC-3).
- **Platform: concurrency** — no scheduler goroutine; the daemon tick owns
  clock/capacity and every blocking call carries context.
- **Platform: interfaces** — interfaces are declared at consumption points;
  do not add an abstraction with no real test/adaptor consumer.
- **Platform: logging** — use the injected `slog.Logger`; add only the
  structured `scheduled` attribute, not metrics/tracing infrastructure.
- **Platform: testing** — stdlib tests, hand-written fakes, deterministic
  ordering, real SQLite transaction coverage, and `-race` suite.

## Deviations

None. The plan can meet the approved specification and the applicable SEC,
CFG, TEST, CONC, STYLE, and EXT constraints without a standards amendment.
