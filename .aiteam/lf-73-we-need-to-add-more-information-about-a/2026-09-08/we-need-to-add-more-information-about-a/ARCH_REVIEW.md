# Architecture Review: Token and Model Metering

Date: 2026-09-08
Architect: Go Architect
Verdict: APPROVED WITH MANDATORY IMPLEMENTATION RELEASE GATES

The feature is structurally compatible with this repository. Complete Claude and Codex descendant attribution is mandatory in V1. Actual deployed-version collection semantics remain unverified; approval of this design is not certification of either upstream format. Both provider stories must demonstrate full collection before the combined release gate passes. An aggregate-only implementation does not fulfill this plan. No production implementation or binary invocation experiments were performed in this review.

## System Overview

Extend `internal/agent`'s existing Request/RunResult boundary with measurement, agent-execution identity, capability and coverage values. Vendor parsing and process ownership remain in `internal/harness` and `internal/codex`. `internal/turn` owns each process attempt, persists measurements through `internal/store`, and passes summary values to `internal/report`. `internal/cli` reads store aggregates and accepts price revision changes. No second Runner abstraction, vendor SDK, telemetry service or new module dependency is required by the selected architecture.

SQL, migrations, immutable rate revisions and durable valuation records belong exclusively in `internal/store`. Preserve its leaf status with store-owned persistence DTOs mapped at the caller boundary; do not import `agent` into store. If pure normalization/pricing arithmetic merits a package, a small stdlib-only `internal/meter` may own arithmetic and summary values; never give it database access, subprocess execution, wall-clock scheduling or vendor-specific exported names. Keep rate arithmetic exact using stdlib integer/rational arithmetic and canonical decimal strings, not float64.

Data flow: allocated logical run -> durable attempt and pinned rate-set revision -> provider process and bounded collector -> authoritative numeric observations plus reconciliation views -> idempotent measurement/valuation transaction -> stored summary -> CLI/report. A failed resume and its cold fallback have separate attempt IDs; the first attempt is persisted before the second starts. Reporting still posts before its final reporting-state transaction (CONC-4). Measurement journaling is execution persistence, not reporting-state mutation.

There is no database transaction across process, filesystem, git or forge I/O. The store retains one SQLite connection. The daemon owns scheduling/backoff; collection is event-driven and must not introduce sleep/retry loops below it (CONC-6). Reuse pipe reader lifetimes and bounded synchronous collection whenever possible; any added worker has a documented fixed bound, owner and cancellation exit (CONC-2). Clone nested result slices/maps across concurrent boundaries (CONC-1).

## Shared Decisions

1. Account for tracked mode runs, all attempts and every descendant. Helper classification/proposal calls are excluded and explicitly labelled in reports. No historical importer, backfill script, automatic old-log scan or repricing path is delivered.
2. Store execution identity independently from display names. Record source session/thread, child and parent IDs, observed model and configured model separately, source schema/binary version and collector revision. Persist request/event identity when available; do not use model text or timestamps alone as deduplication keys.
3. Distinguish unknown, unsupported, partial and complete coverage from measured numeric zero. Completion requires a validated collection boundary and every discovered descendant closed/accounted for; successfully parsing known events is insufficient proof that no children were missed.
4. Select an exclusive accounting frontier: request deltas or disjoint child/root-exclusive intervals. Inclusive model/root totals are reconciliation evidence only. Never charge both inclusive parents and descendants. Missing children may leave a known inclusive total and an incomplete breakdown; label these different views rather than adding the known total to known child rows.
5. Normalize disjoint ordinary input/cache-read/cache-write/output only when native semantics permit. Preserve native counters and category availability, TTL distinctions and reasoning as an output subcount. Reject negative, overflowing or impossible subsets and retain a coverage reason. Unknown cache partitioning can coexist with known total tokens and partial valuation.
6. Cumulative counters need explicit scope/reset identities and persisted baselines. Snapshot replacement is not addition. A resumed session's pre-attempt baseline must be obtained before new work or established by validated invocation-local semantics. Never treat the first lifetime cumulative snapshot as a fresh attempt delta by default.
7. Freeze a price-set revision at attempt allocation. Select the eligible version for its start instant, with immutable rates and immutable membership, including an explicit empty set. Future edits append new revisions; they cannot change old valuations or cause a previously unpriced attempt to become priced. Effective-from is constrained to new/future attempts; an edit during execution does not affect that attempt.
8. Persist frozen line valuations together with accepted measurements, so late-arriving observations use the attempt's pinned rate set. Keep currencies separate. Preserve exact amount precision and specify one display rounding policy. Missing model/category/rate means unpriced, never zero or fallback to vendor cost. Token reporting works with no rates.
9. Incremental numeric journaling protects measurements already observed when a process crashes. A daemon crash may lose not-yet-observed source data; its incomplete attempt stays visibly partial after existing orphan reconciliation. No promise of recovering unobserved consumption. Existing terminal result and save-success semantics remain unchanged.
10. Read only collector-owned, session-bound metadata paths. Refuse symlink escapes, unrelated sessions and arbitrary event-supplied paths. Parse only numeric/provenance fields; do not persist prompts, response/tool bodies or credentials in the ledger. Existing raw run logging remains governed by current policy.

## Provider Acquisition and Certification

### Claude

The default strategy preserves print/stream-json invocation and existing permission/tool settings. Extend the stream parser for per-model totals, parent tool IDs and supported terminal usage deltas. Combine that with read-only metadata extraction from the active session's root and descendant transcripts, discovered from the factory-owned Claude configuration/session root and verified agent/session relationships. Join Agent and legacy Task spawning IDs to child IDs; traverse the complete session-owned tree rather than assuming main stdout copies all children. Capture pre-attempt baselines for resumed transcripts, deduplicate stable message/request identities, and prefer documented terminal output usage over placeholder assistant counters. Refresh discovery at stream/lifecycle boundaries and perform a final bounded closure inventory after the process exits; do not install a wall-clock polling loop in the harness.

A metadata collector is not a historical rollup: it reads only the current attempt's identified session and uses pre-attempt positions/baselines to exclude prior work. It must not sweep old run logs. Supported-version probes must establish that nested/concurrent descendants, failures and multiple models are discoverable and that final output counters are authoritative. If the deployed transcript/stream combination does not expose sufficient information, this story remains incomplete. Root modelUsage cannot substitute for per-child evidence.

Enhanced request tracing is an alternative only if an explicitly approved security/architecture amendment is needed and granted. Do not silently inherit telemetry variables, expand environment forwarding or add an exporter/listener as a shortcut. Existing `env.go` already has documented amendments for a factory-owned CLAUDE_CONFIG_DIR and specific settings; those do not authorize arbitrary environment expansion. The selected default does not require a new external dependency or listener.

### Codex

Use a stdio app-server transport inside the existing `internal/codex.Runner` for metered main runs. Generate schemas from the exact selected binary during the implementation compatibility probe. The adapter owns initialization, root thread start/resume and turn start, recursive child-thread discovery/subscription, terminal events, token-usage snapshots and model/rerouted observations. Preserve stdin prompt delivery, configured worktree, sandbox/approval/tool policy, credential isolation, cancellation/process-tree shutdown, final text and managed frozen-story context. Helpers may retain their present exec path because their usage is explicitly outside scope.

Do not assume root-thread subscriptions receive all child events. Demonstrate explicit subscription/read access for each child and a closure boundary identifying descendants that start/finish between notifications. Pair usage increments with observed model intervals only when a validated schema associates them unambiguously. A turn total spanning a model reroute cannot be partitioned by guessing; use request-level local session metadata, scoped to the selected root/child threads with validated identities, if needed. If neither app-server nor its validated metadata provides exact partitions or complete child discovery, the story and V1 gate fail for that binary. Do not silently retain exec aggregate-only metering as a successful substitute.

Version changes require recertification. Unsupported versions can retain observed token fragments with an unsupported/partial diagnostic but must not claim full support. A fixture-only decoder test does not establish deployed binary behavior.

### Required evidence for each provider

Record exact version, schema/fixture provenance and re-verification commands in the existing binary contract documentation where applicable (EXT-1, EXT-2). Run read-only probes in a scratch directory outside the repository; no live-agent test is committed. Capture minimal sanitized numeric fixtures for root-only, two concurrent children, nested child, resumed child, different child model, reroute/mixed-model turn, repeated events, failed child, absent child completion, interrupted stream and resumed-root baseline. Compare authoritative exclusive records with available aggregate reconciliation totals under documented inclusion semantics. Tests use replay fixtures and process fakes after the one-time probe. A mismatch or unavailable scenario is a blocking capability finding, not a waived acceptance criterion.

## Story and Task Structure

The orchestrator may preserve the feature-spec US identifiers while using these managed keys. Every task below has a concrete architectural owner; acceptance IDs must lead each criterion and be used verbatim as evidence names.

### MTR-1 — Shared usage contracts and normalization (US-1)

Dependencies: none.

- T-1.1, Go implementation engineer: Extend `agent` value contracts for attempts, executions, source identity/scope, native numeric counters, normalized categories, model attribution and coverage; extend deep Clone behavior. Keep exported names vendor-neutral and preserve existing Runner interface semantics.
- T-1.2, Go implementation engineer: Add deterministic normalization, deduplication/reset handling and exclusive-view reconciliation in provider-neutral pure logic; vendor field mapping stays in adapters. No store or binary calls.
- T-1.3, Go unit-test engineer: Table-driven fixtures for zero/absent values, cache subsets, reasoning overlap, cumulative replay/reset, malformed values, mixed parent/child scopes and nested clone isolation. Tests verify quantities and coverage, not just field copying.

### MTR-2 — Complete Claude descendant collection (US-2)

Dependencies: MTR-1.

- T-2.1, Go implementation engineer: Perform and document exact-version acquisition probe before locking the parser; establish authoritative output counters, identity joins and full child closure inventory using the Claude strategy above.
- T-2.2, Go implementation engineer: Implement metadata/stream acquisition in `harness`, rooted path validation, baseline capture and lifecycle-aware final collection. Populate shared values and retain aggregate totals only as reconciliation. Preserve existing invocation/environment security and failure reporting.
- T-2.3, Go unit-test engineer: Replay all supported-version scenarios, including malicious paths and missing child closure. Certify complete tree and per-model quantities; aggregate-only outcomes cannot pass.

### MTR-3 — Complete Codex descendant collection (US-3)

Dependencies: MTR-1.

- T-3.1, Go implementation engineer: Generate exact-binary schemas and prove child subscription/closure and usage/model correlation in scratch probes. Capture unsupported boundaries explicitly.
- T-3.2, Go implementation engineer: Implement the selected stdio transport within `codex.Runner`, preserving execution policy and lifecycle; recursively collect child usage and use validated per-request local metadata only where needed for exact attribution. Bound buffers/process lifetime and reject ambiguous reroute allocation.
- T-3.3, Go unit-test engineer: Fake stdio server/process and metadata fixtures cover protocol sequencing, concurrent/nested/resumed children, model changes, permission refusal, cancellation, malformed input and missing terminal messages. No upstream/network calls in committed tests.

### MTR-4 — Durable attempts and usage ledger (US-4)

Dependencies: MTR-1, MTR-2, MTR-3.

- T-4.1, Go implementation engineer: Add numbered embedded migration and store-owned DTOs/accessors for attempts, execution tree, immutable measurements, coverage and price-set pin references. Preserve one connection and explicit migration.
- T-4.2, Go implementation engineer: Wire attempt allocation, incremental numeric sink and completion into `turn/harvest.go`, before result overwrite/fallback. Persist separately from report delivery state; failure to durably record usage must be observable. No new retry loops or changed save-success predicates.
- T-4.3, Go unit-test engineer: Real migrated SQLite and existing turn fixtures verify transaction atomicity, idempotence, failed resume plus fallback, cancellation, restarted orphan attempts and old rows remaining unmetered. No historical log reads.

### MTR-5 — Editable rates and frozen valuations (US-5)

Dependencies: MTR-4.

- T-5.1, Go implementation engineer: Add rate-set/rate/valuation schema and store transactions; map missing rates explicitly and use exact arithmetic at the owning pure/application boundary. Insertions only; no update-history path.
- T-5.2, Go implementation engineer: Implement `meter prices list/set` through CLI/store accessors, including provider/model/category/unit/currency/effective-from validation and deterministic conflict errors. Rate revisions use supported application writes, never manual runtime config edits.
- T-5.3, Go unit-test engineer: Real SQLite proves completed and in-flight attempts retain original revision/amount/unpriced state after edits, including empty initial table and late observations. Cover rounding, precision, missing cache dimensions and mixed currencies.

### MTR-6 — Meter views and run report integration (US-6)

Dependencies: MTR-4, MTR-5.

- T-6.1, Go implementation engineer: Add store aggregation/query DTOs for all/repository/task scopes and filters; canonical task resolution stays in existing CLI/naming patterns. Aggregate only the selected accounting frontier and keep uncertain attribution visible.
- T-6.2, Go implementation engineer: Register meter command through existing constructors. Support documented filters, attempts/agent tree, numeric JSON, explicit unmetered/partial counts, separate currencies and empty result behavior. Preserve STYLE-6 flag closures.
- T-6.3, Go implementation engineer: Pass computed values into report rendering, with tokens/coverage/user-priced estimates and labelled main-run scope. Renderer never reads DB/logs. Preserve local validation/redaction and save/report workflow.
- T-6.4, Go unit-test engineer: CLI/report fixtures exercise selector ambiguity, all filters, model-unknown bucket, mixed coverage, currencies, tree totals and empty rates. Compare CLI and report values with store results.

### MTR-7 — Full-support release verification and documentation

Dependencies: MTR-2, MTR-3, MTR-4, MTR-5, MTR-6.

- T-7.1, Go code reviewer: Verify full descendant capability evidence for both agents and cross-story ownership/security consistency. Confirm no configuration guesses stand in for observed models and no inclusive/exclusive overlap reaches cost totals.
- T-7.2, Go unit-test engineer: Run end-to-end offline replay through runner -> attempts/store -> CLI/report for each provider, with retries, nested/concurrent descendants and immutable pricing. Final `make check`, `go build ./...` and required coverage checks must pass; no integration tags or real-agent tests.
- T-7.3, Go implementation engineer: Document supported exact versions, recertification, rates and coverage semantics, excluded helper overhead and unavailable historical consumption. Update APP_CONTEXT/APP_CONSTRAINTS if actual package boundaries change, using the app-context-updater role where instructed. No backfill or historical repricing artifact is included.

## Platform Constraints

- Go version/dependencies: Go 1.26, stdlib-only new logic; no added module or assertion framework (TEST-2).
- Database: real SQLite tests, embedded numbered migrations, all SQL in store, ctx first, one connection; no migration library (TEST-6, CONC-5).
- HTTP/new service: not applicable to the selected stdio and metadata strategy.
- Concurrency: process-owned bounded readers, cancellation propagated; channel rendezvous for tests, no sleeps or polling retries (CONC-2, CONC-6, TEST-8).
- Logging: injected slog and existing CLI writers, no copied model text in numeric ledger (STYLE-2).
- Validation: deterministic joined errors and strict config defaults/template if new keys are necessary (CFG-1 through CFG-6, STYLE-9).
- Build/test/lint: make check and go build ./...; never write dist or restart the daemon.

## Managed Publication Contract

`internal/planbrief/proposal.go` requires `version: 1`, supplied parent and positive generation. New stories use `intent: create`, numeric `issue_number: 0`, numeric `issue_id: 0`, `expected_base: ""`. Dependencies are story keys in the same acyclic envelope, not guessed issue numbers. Every full body requires nonempty exact H2 User Story, Acceptance Criteria, Tasks, Architecture and Dependencies sections. A leading AC identifier is the evidence key; test/review evidence must cover every parsed criterion.

The factory owns GitHub access and publication. `internal/turn/plan_publication.go` builds actual issue links only after publication and `plan_index.go` replaces the Story Index section. Before publication, keep a header-only index `| ID | Title | Sub-issue |` and explain pending identities outside it; do not fabricate story URLs or create story Markdown files. A temporary Go validation tool within the module can read the proposal, invoke `planbrief.ParseProposal` and enumerate `AcceptanceCriteria`; remove the validation source afterwards. The transport file is transient and excluded from commits by factory handling.
