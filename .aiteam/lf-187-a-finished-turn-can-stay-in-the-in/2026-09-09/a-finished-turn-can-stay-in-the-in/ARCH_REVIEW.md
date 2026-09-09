# Architect: Go Architect
# Created: 2026-09-09

## Verdict

Approved for story authoring with the AC-3 boundary refinement already applied to feature-spec.md before freeze. No standard deviation requested. This is daemon observability work; the withdrawn leak diagnosis does not justify changing admission or polling policy.

## System Overview

The existing flow is forge discovery plus managed-parent registry plus durable backlog → ordinary candidate union → bounded shared admission → worker → per-comment/control processing → existing turn executor and reporter. Scheduled and automatic-continuation workers enter through separate admission paths and share the same pool, task model and executor boundary. Candidate provenance belongs to daemon, as does worker lifetime; turn continues to own the meaning and preparation of individual execution requests.

No new package, public API, table, configuration, dependency or goroutine is necessary. Private daemon metadata can carry immutable candidate sources and worker-local outcome accounting. Structured logging uses the existing injected slog.Logger. Existing INFO preparation reports remain owned by turn.

### Key Boundaries and Data Flow

Discovery records categories when each source encounters an existing candidate, even when candidate deduplication would otherwise skip it. The union is stable and duplicate-free; it does not change source eligibility, materialization, queue ordering or rotation. Materialization copies diagnostic context into the worker task. Admission logs the handoff only after every current validation and reservation succeeds and immediately before goroutine launch. Failed admission is not a dispatched worker.

Worker-local accounting counts each request immediately at the existing daemon-to-executor invocation boundary, including calls returning errors or lock refusals. One ordinary worker may drain multiple comments. The counter must not count classifications, controls or preparation heuristics. A zero count says only that the worker requested no execution; managed-child discovery, acknowledgements and control responses can still be useful.

Private worker outcome values summarize completed, deferred, failed or shutdown-cancelled work from the actual branch and context. They are not agent success values. Preserve errors and reporting, store settlement, refinement consumption and backlog mutation behavior. Existing helpers that log and discard a backlog-clear error cannot support a claim that all durable cleanup succeeded.

### Concurrency and Context

Mutable source collections must be cloned across candidate/task boundaries (CONC-1); keep outcome counters worker-local. Every worker remains bounded by the current shared pool and Quiesce (CONC-2). Do not move admission/reservation, add polling goroutines, or derive concurrency from log output.

Terminal order is: worker body returns after its internal cleanup; in-flight ownership is released; terminal diagnostic is emitted; final pool release/join action runs. Existing pool.done releases semaphore then decrements the WaitGroup. Logging after pool.done would permit Quiesce to return before the diagnostic and allow worker activity to outlive the join. Consequently AC-3 now promises ownership release before the log and pool/join release afterward. This is an architecture refinement to unapproved planning language, not a frozen-contract change. TEST-9 remains intact: use a subsequent real Tick to prove a slot can be reused, never make a completion log into a slot-release observable.

Use the supplied tickAt solely as tick grouping context. Never read or pass d.now below Tick; clock_test.go pins the deterministic scheduling seam. If retaining diagnostic duration, measure only monotonic elapsed worker lifetime through a private diagnostic timer; no public clock option or scheduling decisions may depend on it. Test duration presence/nonnegative bounds rather than elapsed wall-clock exactness. Tests rendezvous via channels and Quiesce, with named timeout bounds, never sleeps/eventual polling (TEST-8).

### Shared State and Side Effects

No new store accessor or SQL is required. Do not reconcile inflight with sessions.lock_owner: legitimate worker ownership extends beyond durable execution locks. Preserve scheduling/continuation reservation and settlement, cursor/backoff timing, closure retirement, ordinary backlog clean-drain behavior, and managed-child approval/freeze rules (CONC-9, CONC-14, CONC-15). GitHub reads and all publication remain factory-owned (SEC-20, CONC-12).

## Proposed Story Decomposition

| Key | Title | Size | Depends on | Criteria |
| --- | --- | --- | --- | --- |
| US-1 | Attribute daemon worker dispatches to sessions and discovery sources | M | None | AC-1, AC-2 |
| US-2 | Report terminal worker outcomes and execution requests | M | US-1 | AC-3, AC-4 |
| US-3 | Clarify polling counters and verify quiet managed-parent polling | M | US-1, US-2 | AC-5, AC-6, AC-7, AC-8 |

These are independently verifiable after listed dependencies. US-1 owns discovery metadata and launch events; US-2 extends that context into terminal outcomes; US-3 owns summary aliases, operator/package documentation and the composed quiet-polling regression. This serial dependency avoids competing edits to the central task/admission lifecycle. Carry shared decisions here into plan_brief.md; keep complete story-specific tasks, criteria and inventories in proposal bodies.

## Task Hints

### US-1 / discovery provenance
**Agent:** go-implementation-engineer.
Pattern: enrich the current deduplication data flow in internal/daemon/tick.go and story_refinements.go rather than introducing a second scheduler. The forge category includes existing issue/comment-driven discovery; managed_registry is enrollment evidence, not evidence of a pending child update. Ensure already-seen candidates collect later source categories. Preserve lazily resolved issue values and the existing queue sort/rotation. No extra forge reads.

### US-1 / admission diagnostics
**Agent:** go-implementation-engineer.
Placement: daemon ordinary offer, schedules.go and continuation_dispatch.go. Reuse a consistent child logger with repo, canonical task, session_id and applicable schedule_id/continuation_id exactly once. Capture bounded source categories in canonical order: forge_discovery, managed_registry, backlog, due_schedule, automatic_continuation. Emit worker dispatched before launch and only once per Dispatched increment. Continuation duplicate refusal uses existing DEBUG refusal convention with complete identity. Keep admission errors and refusal behavior unchanged.

### US-1 / acceptance tests and review
**Agent:** go-unit-test-engineer; go-code-reviewer.
Use existing HTTP/SQLite fixture and synchronized fake executor. Cover each launch path, multi-source deduplication and rejection branches. Assert structured attributes and event ordering instead of exact prose of legacy errors. Scoped review checks unchanged queue ordering, copies across struct boundaries, and no duplicate logger attributes. Evidence names AC-1 and AC-2 exactly.

### US-2 / lifecycle accounting
**Agent:** go-implementation-engineer.
Placement: daemon task/worker and shared executor invocation path. Internal worker-return values or worker-local result state are sufficient; do not extend turn.Result, agent contracts or exported daemon API. Ensure ordinary early bootstrap/refinement/drain exits, scheduled settlement exits and continuation requeue/failure paths provide truthful status. Count executor requests, not d.run entries; permission/mode refusal before Execute is zero. Preserve failure diagnostics and durable behavior.

### US-2 / terminal wrapper
**Agent:** go-implementation-engineer.
Follow the ordering and clock boundaries above. Capture immutable admission context for matching terminal logs. Cancellation is determined from observed result/context on the worker's exit, not retroactively from the next tick. Keep final pool.done last. No recovery wrapper: SEC-11 forbids adding recover, and process death/panics are outside the terminal guarantee. Existing per-trigger turn complete remains per-trigger evidence.

### US-2 / acceptance tests and review
**Agent:** go-unit-test-engineer; go-code-reviewer.
Cover zero, multiple, failed and lock-refused executor calls; useful control-only work; error, deferral and shutdown cancellation. Quiesce must join terminal diagnostics. Use subsequent ticks for pool reuse and an appropriate white-box inspection only for in-flight release at diagnostic time. Never use a logger callback that launches concurrent Tick during another Tick or WaitGroup wait. Evidence names AC-3 and AC-4 exactly.

### US-3 / tick summaries and documentation
**Agent:** go-implementation-engineer.
Placement: daemon tick summary and internal/daemon/doc.go, plus existing operator-facing README logging guidance. Keep TickResult fields and legacy dispatched/in_flight/issues attributes; add workers_dispatched/workers_in_flight/forge_issues aliases from those same values, without recomputation. Explain that forge_issues includes forge discovery paths and excludes local managed/backlog enrollment. Update package discovery documentation, which currently omits managed registry. Do not imply dispatch-completion snapshots know eventual executions or that aliases eliminate all numeric polling activity.

### US-3 / composed polling regression and aggregate gates
**Agent:** go-unit-test-engineer; go-code-reviewer.
Use synthetic local HTTP responses and migrated SQLite under daemon's existing fixture conventions (TEST-1–6). Two open managed parents, established watermarks, no actionable child or parent input, and empty forge discovery should repeatedly dispatch exactly two workers without executor requests/prepared records. Quiesce, inspect ordinary backlog and durable runs, then use a real following tick to prove redispatch. Separately hold workers for deterministic in-flight summary checks; do not require fast quiet workers to remain in flight at sample time. Re-run existing managed-child/closed-child/post-promotion tests and fill meaningful gaps. Aggregate make check is required; no integration tags or live agent captures. Evidence names AC-5 through AC-8 exactly.

## Managed Proposal Schema and Publication Directions

Inspected internal/planbrief/proposal.go, acceptance.go; internal/turn/plan_handoff.go, plan_index.go, plan_publication.go; .gitignore and internal/git/commit.go.

- Envelope: version 1, parent github:marioatcloudvane/litefactory#187, generation 1, nonempty stories array. Unknown fields and trailing JSON are rejected. Maximum 100 stories and 8 MiB envelope.
- No existing factory-supplied story identity/base context is present. Every story is create with issue_number 0, issue_id 0, expected_base "". Do not derive identities from research links, local prior files or forge queries.
- Every story uses exactly key, intent, issue_number, issue_id, expected_base, title, body, dependencies. Key regex is ^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$. Dependencies are other proposal keys, unique, non-self and acyclic; no dependencies is [].
- Nonblank single-line titles must be at most 256 bytes; full bodies at most 65536 bytes. Each body needs nonempty level-2 User Story, Acceptance Criteria, Tasks, Architecture, Dependencies sections. Evidence Inventory is a separate level-2 section. Keep acceptance criteria one criterion per line with explicit leading AC identifiers; extra explanatory lines inside that section become independent obligations. The shared parser extracts AC-1 etc., not the entire line after it.
- Existing-story update/unchanged would require supplied positive numeric issue number/ID and a 64-character SHA256(title + NUL + body); none may be invented for this proposal.
- There is no special pending-link URL scheme in the managed publication path. Before publication use an empty link-only Story Index table (ID, Title, Sub-issue) under the exact heading, followed by shared context sections. Do not fabricate issue URLs, local story links or guessed child numbers. readPlanIndex only requires the heading; PublishPlan replaces that section with ID/title/verified numeric GitHub issue links. Keep decomposition/order in a separate shared planning section if desired.
- .gitignore already excludes /.litefactory-plan-proposal.json. Factory publication reads it using rooted safety checks, validates it, publishes complete issues, replaces Story Index, then deletes the transport before successful save. Git save additionally unstages reserved proposal/event transport. Leave proposal for factory consumption; do not commit it or manually delete it now.
- Do not create per-story Markdown files; plan_handoff explicitly refuses <story-key>.md next to the brief. Only the factory reads/mutates forge state.

## Evidence Review Before Freeze

Read docs/decisions/nested-agent-evidence.md before reviewing acceptance. All eight feature-spec inventory rows are sufficient for these bounded repository contracts, with AC-3 updated to the corrected terminal boundary. Story authoring must retain each assigned row, including method, available synthetic/repository source, implementation-revision bounds and outstanding execution prerequisite. AC-8 also requires documentation review and actual aggregate check evidence.

No live nested agent launch, authentic stream, production session identity or historical recovery is promised. Therefore no external authentic capture is a prerequisite for these criteria; the production explanation remains unverified operational attribution. All implementation/tests/reviews remain unsatisfied until run. Completed stories must publish exact-criterion scoped evidence plus review/tests through the factory-supplied event interface, without waiting for siblings; final aggregate evidence remains additional (SEC-21, CONC-11).

## Platform Constraint Application

Go placement, structured logging, bounded concurrency, context propagation, error preservation, fake-based offline tests and pinned format/vet/lint/race gates apply. Existing standard-library slog and hand-written fixtures suffice; new library/framework wiring is unnecessary (STYLE-2, TEST-2). HTTP entry-point, RPC, database schema, tenant, dependency and frontend architecture changes are not applicable to this feature. No outside-specialism architecture gap was identified.
