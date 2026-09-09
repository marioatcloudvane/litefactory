# Architect: Go Architect
# Created: 2026-09-09

## Verdict
Approved for planning, with a hard implementation release gate: actual-model attribution and complete descendant coverage for both executable providers must be demonstrated against exact supported versions. No current provider capability is asserted by this review. Full support must not be replaced by a root-only implementation. No standard deviation is required by the proposed boundaries.

## System Overview
Extend `internal/agent`'s existing `Request` / `RunResult` contract. `internal/harness` and `internal/codex` own provider subprocesses and their collection adapters. `internal/turn` owns attempt creation, incremental persistence coordination, frozen pricing selection and end-of-attempt finalization. `internal/store` stays a leaf and owns every SQL statement and migration. `internal/report` renders already prepared summaries; `internal/cli` exposes local read and price-revision commands.

A small new pure `internal/meter` package may own normalization/reconciliation and exact valuation mathematics shared by adapters and turn/CLI. Its imports must not create cycles; it must not own subprocesses, database connections, time or network. Shared exported runner fields remain vendor-neutral and cloned at ownership boundaries. Store-owned DTOs are mapped at the consumer boundary so store does not import agent or meter.

Data flow: turn allocates run attempt and pins price catalog revision → provider adapter emits invocation identities and normalized usage with source provenance → turn persists observations idempotently → attempt finalization reconciles descendant coverage and freezes valuation → report/CLI read durable aggregates and coverage. Provider-inclusive totals are reconciliation evidence, not additive records alongside exclusive descendant measurements.

Raw event logs currently append under one run ordinal (`harness/runner.go`, `codex/runner.go`); therefore ordinal alone cannot distinguish resume refusal and fallback executions. Introduce factory-owned attempt identity before every subprocess, and preserve observed failed-attempt usage before proceeding to fallback. Resumed lifetime counters require explicit baseline handling; an invocation/session identity alone is not a deduplication key. The factory must not recover current attempts by calling a historical import feature.

## Cross-Story Decisions
1. Full v1 support means root plus all causally related descendants, including nested/parallel/failed/resumed work, with source-proven actual model. Configured model remains configuration provenance only. Unknown attribution and unavailable descendants make coverage incomplete and block compatibility/release evidence.
2. Mutually exclusive integer token quantities are the billing basis: uncached input, cache read, cache write (retain known write-duration subdivisions), output. Reasoning tokens and other provider subsets are supplementary, never added twice. Unknown is distinct from measured zero; source types declare increment/cumulative and exclusive/inclusive semantics.
3. Identity hierarchy is task/run/attempt/invocation/measurement; descendant parent linkage and source event identity survive restart/replay. Persist observations during collection, not only after clean result. Missing data survives as partial/unknown diagnostics, not invented zero. Reconciliation mismatches cannot earn complete status.
4. Freeze a whole immutable price-catalog revision before each attempt, including explicit missing-price entries; otherwise later-discovered child models could accidentally use a later edit. Store exact decimal rates and deterministic rounding policy, applied quantities and pricing completeness. Later edits affect only later attempts; no repricing operation or automatic remote prices. Zero prices are valid, absent prices are unpriced.
5. Privacy: collectors only access demonstrably associated sessions and descendants through validated identities and bounded safe paths. No global home-directory transcript sweep; no credentials in measurement tables, prompts or reports. Local raw logs retain existing unredacted policy (SEC-6); committed captured fixtures are sanitized. Current allowlisted environments/permissions remain intact (SEC-1–4), and any new LF_HOME path uses config.Paths (CFG-8).
6. Usage persistence is independent of report delivery and does not advance reporting watermarks. Preserve CONC-4 report transaction ordering, CONC-7 running-state ordering, CONC-8 success-only saving and EXT-3 outcome classification. Coverage errors must not relabel a failed process as succeeded, or fabricate a process failure. Report both statuses explicitly.

## Task Hints
The following bounds follow feature-spec's seven stories; the product owner supplies final task wording and stable AC IDs in the proposal. Each task's engineer is go-implementation-engineer, with go-unit-test-engineer for tests and go-code-reviewer for scoped review. Real SQLite tests use the existing suite (TEST-6), with no integration tags or committed paid-provider execution tests.

### US-1: Contract and compatibility basis
- Contract task: place vendor-neutral identities, optional quantities, scope semantics, model provenance and coverage evidence in existing agent boundary; clone nested mutable data per CONC-1. Keep interfaces at consumers (TEST-5) and preserve purity enforcement deliberately (TEST-10).
- Compatibility task: establish an explicit version/capability record and manual re-verification instructions. Read-only real executions in scratch outside repository must prove root, parallel and nested children and actual models for each supported provider before support is claimed (EXT-1/2). Capture sanitized version-pinned fixtures, never a CI test launching a real paid agent.
- Verification task: demonstrate unknown versus zero, cache subset normalization, overflow/counter regression rejection, cumulative versus incremental semantics and copy isolation. Establish a single authoritative reconciliation contract used by both adapters and reports.
- Dependencies: none; US-2/3/4 build on this. Treat unsuccessful compatibility exploration as a visible release blocker, not permission to silently defer children.

### US-2: Claude collection
- Adapter task: extend harness stream handling and invocation lifecycle ownership, using US-1's proven collection surface. Retain actual root/descendant parent relationships and models, including failed descendants and resumed baselines. Do not assume top-level model totals identify individual children.
- Lifecycle task: thread collection through runner's existing bounded pipe handling. Prefer synchronous collection and existing goroutines; additional concurrency needs explicit bounded lifetime documentation (CONC-2). Daemon owns retry scheduling (CONC-6); no polling/sleep loop in turn. Settle only within existing runner I/O lifetime/deadline and mark incomplete if source finalization cannot be observed.
- Verification task: fixtures cover duplicates, nested parallel roots, model switches, malformed usage, missing terminal output, delayed child completion and parent-versus-descendant reconciliation. Preserve failed process result and observed tokens (CONC-3).
- Dependencies: US-1. Do not redesign environment isolation or collect unrelated sessions to obtain measurements.

### US-3: Codex collection
- Adapter task: extend codex consumer using the proven compatible transport or associated session records selected in US-1. Current RunResult.Model comes from r.model; this is not evidence of actual child model attribution. Never fabricate a model breakdown from it.
- Lifecycle task: bind thread/subagent hierarchy, baseline lifetime counters on resume, deduplicate repeated snapshots, preserve failed attempts and cancellation observations. Collection state must not weaken EXT-3 sticky failure and terminal-event semantics.
- Verification task: root/nested/parallel/failed/resumed fixture cases plus actual-model changes and unknown-model refusal of complete status. Pin provider/source version and evidence provenance; unsupported source shapes produce explicit compatibility diagnostics.
- Dependencies: US-1. No provider-specific names in exported agent contract and no credential access expansion.

### US-4: Durable attempts, observations and rollups
- Schema task: new numbered embedded migrations in internal/store for attempts, invocations, normalized observations and coverage/checkpoints. Preserve one SQLite connection and explicit Migrate; no raw SQL elsewhere and no new migration library.
- Orchestration task: internal/turn allocates attempt before every Run call, connects usage sink/checkpointing, preserves partial result on fallback, and finalizes independently of report delivery. No SQL transaction across subprocess or forge I/O. Crash-recovery records explain unresolved gaps rather than pretending the last checkpoint was complete.
- Query/verification task: store grouping joins existing repository/task/run/mode/agent identities; preserve all executed attempts, count exclusive observations once, and retain incomplete known subtotals. Real SQLite tests cover migration on populated installs, idempotency, replay, rollback, restart and concurrent attempts without conflating sessions.
- Dependencies: US-1; adapter wiring can be verified with fakes before US-2/3 finish. Historical logs are never scanned/imported by migration or startup.

### US-5: Editable pricing and frozen valuations
- Price task: immutable full catalog revisions in store plus existing CLI Command constructor style for meter prices list/set. Use exact validated nonnegative decimal per-million USD rates for each billable dimension. Aggregate independent validation faults; invalid writes are atomic and leave revision unchanged (CFG-5, STYLE-9).
- Valuation task: pure deterministic math belongs in internal/meter, durable snapshots in store and pin/finalize orchestration in turn. Transactionally resolve revision before subprocess; hold no transaction during execution. Define fixed precision and rounding at valuation boundary so aggregate display is reproducible. Freeze absence of price as well as known rates.
- Verification task: completed and in-flight attempts keep original prices after edits, later attempts use revision, newly discovered child models cannot escape pinned catalog, zero versus unknown pricing remain distinct, overflow is surfaced. Retain existing reported cost only as labelled legacy data.
- Dependencies: US-4 (and US-1 transitively). No automatic remote catalog fetch, invoice billing, manual runtime-state edit or history recalculation.

### US-6: Meter CLI and completion reporting
- CLI task: one registered constructor per established command family, bound flag closures (STYLE-6), output through command writer, no database in cmd/main. Global/repository/task selectors and mode/ca/model filters compose; details expose attempt/invocation/model and ancestry.
- Projection task: report consumes prepared values and never imports turn/store. Text and JSON distinguish known subtotal, unknown total, unpriced tokens, unsupported coverage and complete known zero. Filtered views retain denominator/coverage meaning and cannot present partial model attribution as complete.
- Verification task: fixture-backed CLI/store scope combinations, invalid selectors/flags, stable ordering (TEST-7), exact local-estimate report provenance, and legacy costs labelled separately. Explicitly label separate daemon helper overhead as excluded.
- Dependencies: US-4, US-5. Full support release waits US-2/3 and US-7, even if CLI is independently testable.

### US-7: Integrated full-support release gate
- Scenario task: tie version-pinned captured evidence from both providers to end-to-end adapter → persistence → CLI/report scenarios. Prove root plus every nested/parallel/failed descendant, actual model switching, resume/fallback baselines, crash/replay deduplication, cancellation and immutable in-flight prices.
- Review/test task: scoped review and tests per story, then aggregate make check and safe go build ./...; do not run make build or touch dist. Synchronize race tests using channels/Quiesce instead of sleep (TEST-8). Manual EXT-1 probes remain scratch-only and documented, while committed tests are fixture-driven.
- Release task: verify no historical recovery code/script entered repository and no unsupported version is presented as full support; document supported versions and re-verification. A missing real capability proof or reconciliation failure blocks release, regardless of aggregate test pass.
- Dependencies: US-2, US-3, US-4, US-5, US-6.

## Platform Notes
- Platform: Go package layout/interfaces/error handling apply through existing internal owners; no new modules, framework, metrics collector, server, HTTP router, tenant machinery or infrastructure is needed. HTTP/server deployment constraints are not applicable (CLI).
- Platform: use existing modernc SQLite migration/pool contract, not generic platform suggestions for migration libraries (TEST-2).
- Platform: bounded context-aware collector lifetimes, exact arithmetic and immutable snapshots are the main cross-cutting implementation risks. No second recover or detached background contexts (SEC-11, STYLE-2).

## Managed Transport and Publication Verification
Inspected current source, not previous-run claims: internal/planbrief/proposal.go, acceptance.go, turn/plan_handoff.go, plan_publication.go, plan_index.go and sessiondir.go.

- Exact transient path is worktree root `.litefactory-plan-proposal.json`. Strict JSON rejects unknown fields, trailing input and over 8 MiB. Envelope is version=1, parent=github:marioatcloudvane/litefactory#73, generation=1, nonempty stories (at most 100).
- Story fields are key, intent, issue_number, issue_id, expected_base, title, body, dependencies. Keys match `[A-Za-z0-9][A-Za-z0-9._-]{0,39}`. Create requires numeric zero issue_number and issue_id and empty-string expected_base. No supplied existing identities means create proposals only; no fabricated IDs/hashes. Titles are nonempty single lines at most 256 bytes; bodies at most 65536 bytes. Dependencies refer to keys in same envelope, unique, non-self, acyclic.
- Every body needs nonempty exact level-2 User Story, Acceptance Criteria, Tasks, Architecture, Dependencies. Use one leading unique AC identifier per checklist criterion and exact same evidence identifier later. Body dependency prose must agree with JSON edges.
- Factory first validates destination before forge effects, then publishes full stories and rewrites index. It alone knows numeric identities and verifies current bases/markers. No claim of publication or live repository verification can be made by this planning agent.
- Session discovery derives branch/slug, tries run date first, then newest valid date with regular plan_brief.md. The assigned 2026-09-09 session with expected branch and feature slug is discoverable. The brief must contain literal `## Story Index`; an empty header/table is safe before factory supplies links. The factory replaces the entire section through next `\n## ` with columns `ID | Title | Sub-issue` and verified GitHub links. Keep shared content outside that section; do not invent links or use local per-story paths.
- No `<key>.md` file may exist in selected session for a proposal story; otherwise handoff explicitly fails. Factory removes transient JSON only after successful publication/index replacement; Git excludes it from status harvesting and unstages it before commits. The agent must leave the JSON present for the factory and must not commit it.
- Local ParseProposal validation proves structure, not factory-side feature state, credentials, publication or readiness. Those checks remain pending factory execution after this run.
