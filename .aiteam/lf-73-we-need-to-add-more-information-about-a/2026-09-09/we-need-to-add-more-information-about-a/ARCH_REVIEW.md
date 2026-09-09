# Architect: Go Architect
# Created: 2026-09-09 — run 0010

## Verdict

APPROVED for Plan publication with full descendant support still a capability gate, not a verified implementation claim. No standards deviation required. Reviewed current feature-spec, factory-supplied US-1..US-7, APP_CONTEXT, all APP_CONSTRAINTS and Go platform constraints against HEAD 0d548269a37536f4557221b2a26b5ee51821ae43. This document describes proposed structures; it creates no migrations or implementation.

## System Overview

Existing repos → sessions → runs remain the repository/task/run identity authority. Extend internal/agent with immutable-by-ownership usage records. internal/harness and internal/codex own decoding and associated descendant sources; internal/turn allocates attempts and bridges agent records to store-owned DTOs. internal/store remains the only SQL/database owner. internal/meter is a new pure component for normalization, reconciliation and exact valuation. internal/cli owns command validation and CSV/text/JSON presentation; report accepts prepared values and never imports turn or store.

Data flow: start attempt and pin catalog atomically → execute root/children → incremental source observations → atomic checkpoints/exclusive measurement replacement/valuation → final coverage → store snapshot → CLI/export or prepared completion report. Provider-inclusive observations are retained only as reconciliation evidence. The configured model is not actual-model proof.

Keep copied slices/maps at boundaries (CONC-1), context-first accessors (CONC-5), tiny bounded collector lifetimes (CONC-2), existing runner failure classification (EXT-3), report delivery ordering (CONC-4) and one SQLite connection. No SQL transaction spans subprocess, forge or stdout I/O. No HTTP service, new dependency, global transcript scan or runtime config edit is introduced. CLI uses command writers and constructor-bound flags (STYLE-2/6); interfaces belong to consumers (TEST-5). Safe source association preserves SEC-4/20. Real probes stay manual scratch work with exact-version evidence (EXT-1/2); transaction promises use real SQLite (TEST-6).

## Proposed physical schema

Notation: every column below is NOT NULL unless marked `NULL`; PK means primary key. All counters are SQLite INTEGER checked >=0 and handled with checked int64 arithmetic. All IDs are INTEGER unless explicitly TEXT. Timestamps are TEXT UTC RFC3339 with fixed millisecond precision following store.FormatTime. No REAL money fields are added. JSON fields contain bounded canonical machine metadata only, never prompts, source transcripts, arbitrary paths or credentials. Every referenced table below uses the meter_ prefix; existing `runs(id)` remains the only external FK. Deletion follows existing run retention through CASCADE for attempt children; catalog deletion is RESTRICT. New numbered embedded migrations are assigned at implementation against the then-current tip; store.Open does not migrate.

### Migration/story sequencing

US-4 creates meter_price_revisions with an immutable empty baseline catalog alongside attempts, invocations, observations, checkpoints and measurements. It pins that baseline before launch, so its attempt FK is valid without depending on US-5. US-4 implements atomic observation/checkpoint/measurement persistence and snapshot measurement/coverage projection; pricing remains explicitly unpriced. US-5 adds meter_price_rates, meter_valuations and meter_valuation_dimensions, atomic catalog editing, active-revision selection and valuation writes/projections. US-4 rows are not retroactively priced: the pinned empty baseline remains empty. The baseline is a real revision with no rate entries, not a nullable or mutable placeholder. US-5 extends the transaction boundary to include valuation alongside subsequent measurement revisions and does not introduce a reverse story dependency.

### meter_price_revisions — immutable full-catalog header

Columns: `id INTEGER PK`, `created_at TEXT`, `currency TEXT` (CHECK USD), `rate_scale INTEGER` (CHECK 9), `valuation_scale INTEGER` (CHECK 12), `rounding_policy TEXT` (CHECK half_even_per_measurement_dimension_v1).

A revision contains a complete catalog; absent rate rows mean unknown, including a deliberately empty initial catalog. The current catalog is the greatest committed id. Revision header and all rates publish atomically. Readers can never see a partially populated catalog. No update/delete API exists.

### meter_price_rates — immutable model/dimension entries

Columns: `revision_id INTEGER`, `coding_agent TEXT`, `model TEXT`, `dimension TEXT`, `usd_per_million TEXT`.

PK `(revision_id,coding_agent,model,dimension)`; FK revision_id → meter_price_revisions(id) RESTRICT. Model/agent are exact validated selectors, no implicit configured-model alias. Dimension enum: input_uncached, cache_read, cache_write, cache_write_5m, cache_write_1h, output. Rate is canonical nonnegative decimal USD/million, maximum 9 fractional digits, maximum 18 integral digits; reject excess precision rather than silently round. TEXT preserves exact decimal values; SQL numeric casts/sums are prohibited. PK supports rate lookup; no extra index needed.

### meter_attempts — one factory subprocess attempt

Columns: `id TEXT PK` (factory stable opaque identity), `run_id INTEGER`, `ordinal INTEGER` (positive within run), `coding_agent TEXT`, `agent_version TEXT NULL`, `source_format TEXT NULL`, `started_at TEXT`, `finished_at TEXT NULL`, `outcome TEXT`, `collection_status TEXT`, `diagnostic_codes_json TEXT`, `price_revision_id INTEGER`.

FK run_id → runs(id) CASCADE; price_revision_id → meter_price_revisions(id) RESTRICT; UNIQUE `(run_id,ordinal)`; indexes `(started_at,id)`, `(coding_agent,started_at,id)`, `(price_revision_id)`. outcome records launch/running/success/failure/cancelled/abandoned independently of collection_status (unknown/partial/complete). Include failed launch attempts with unknown measurement, not synthetic zero. runs.mode and sessions.issue_number/repo_id are joined, never copied into attempts. Agent is attempt-local because execution provenance belongs to the subprocess. Pin revision in the same transaction as allocation before launch.

### meter_invocations — root and descendant ancestry

Columns: `attempt_id TEXT`, `id TEXT`, `parent_id TEXT NULL`, `source_id TEXT`, `source_invocation_id TEXT`, `started_at TEXT NULL`, `finished_at TEXT NULL`, `lifecycle TEXT`, `collection_status TEXT`, `diagnostic_codes_json TEXT`.

PK `(attempt_id,id)`; FK attempt_id → meter_attempts(id) CASCADE; composite self-FK `(attempt_id,parent_id)` → meter_invocations(attempt_id,id), deferred; UNIQUE `(attempt_id,source_id,source_invocation_id)`; index `(attempt_id,parent_id,id)`. A partial unique index on attempt_id WHERE parent_id IS NULL permits only one root. Unresolved children require identified placeholder parents or attempt diagnostics, never extra roots. Validate acyclic ancestry in the store contract; deferred FK permits out-of-order registration in one transaction.

### meter_observations — deduplicated source evidence, not report totals

Columns: `id INTEGER PK`, `attempt_id TEXT`, `source_id TEXT`, `event_key TEXT`, `invocation_id TEXT NULL`, `observed_at TEXT NULL`, `recorded_at TEXT`, `semantics TEXT`, `counter_epoch TEXT`, `source_sequence TEXT NULL`, `actual_model TEXT NULL`, `model_provenance TEXT`, `dimensions_json TEXT`, `payload_sha256 TEXT`, `validation_status TEXT`, `diagnostic_codes_json TEXT`.

FK attempt_id → meter_attempts(id) CASCADE; composite FK `(attempt_id,invocation_id)` → meter_invocations(attempt_id,id); UNIQUE `(attempt_id,source_id,event_key)`; indexes `(attempt_id,invocation_id,id)` and `(attempt_id,source_id,counter_epoch,id)`. semantics explicitly identifies incremental/cumulative and exclusive/descendant-inclusive. dimensions_json preserves canonical nullable integer source dimensions and subset semantics, not raw provider JSON. Conflicting bytes for the same key produce diagnostics, never overwrite accepted evidence. Invocation/model may be absent for provider summary evidence. Negative or regressing input records are diagnostic evidence and never additive usage.

### meter_checkpoints — baseline and replay cursor

Columns: `attempt_id TEXT`, `source_id TEXT`, `counter_key TEXT`, `counter_epoch TEXT`, `invocation_id TEXT NULL`, `model_key TEXT`, `baseline_dimensions_json TEXT`, `last_dimensions_json TEXT`, `source_cursor TEXT NULL`, `last_observation_id INTEGER NULL`, `updated_at TEXT`.

PK `(attempt_id,source_id,counter_key,counter_epoch)`; FK attempt_id → meter_attempts(id) CASCADE; composite FK invocation → meter_invocations; FK last_observation_id → meter_observations(id); index `(last_observation_id)`. model_key is a canonical encoded tuple distinguishing known model from unknown, not a user model name sentinel. Counter keys partition model and metric scopes. Missing baselines remain null values in JSON and prohibit fabricated deltas. Adapter supplies verified resume lifetime baselines; epoch is source-proven, not inferred from any regression. Checkpoint and accepted observation plus affected measurement/valuation update atomically.

### meter_measurements — current exclusive invocation/model aggregate

Columns: `id INTEGER PK`, `attempt_id TEXT`, `invocation_id TEXT`, `model_key TEXT`, `actual_model TEXT NULL`, `model_provenance TEXT`, `input_uncached_tokens INTEGER NULL`, `cache_read_tokens INTEGER NULL`, `cache_write_tokens INTEGER NULL`, `cache_write_5m_tokens INTEGER NULL`, `cache_write_1h_tokens INTEGER NULL`, `output_tokens INTEGER NULL`, `reasoning_tokens INTEGER NULL`, `collection_status TEXT`, `diagnostic_codes_json TEXT`, `last_observation_id INTEGER`, `revision INTEGER` (positive), `updated_at TEXT`.

FK attempt_id → meter_attempts(id) CASCADE; composite FK `(attempt_id,invocation_id)` → meter_invocations; FK last_observation_id → meter_observations(id); UNIQUE `(attempt_id,invocation_id,model_key)`; indexes `(actual_model,attempt_id,id)`, `(last_observation_id)`. Rollups update only as genuinely new evidence arrives, never on price edits. A model_key encodes model-knownness collision-free; NULL actual_model does not allow duplicate unknown rows through SQL NULL uniqueness. Individual nonnull token fields are known subtotals when coverage is partial. Cache duration and reasoning fields are subsets, not additional total tokens. Provenance records mixed verified sources canonically when needed. No provider-inclusive observations enter this table.

### meter_valuations — immutable value snapshots

Columns: `id INTEGER PK`, `measurement_id INTEGER`, `measurement_revision INTEGER`, `price_revision_id INTEGER`, `pricing_status TEXT`, `estimated_usd TEXT NULL`, `priced_subtotal_usd TEXT`, `created_at TEXT`.

FK measurement_id → meter_measurements(id) CASCADE; price_revision_id → meter_price_revisions(id) RESTRICT; UNIQUE `(measurement_id,measurement_revision)`; index `(price_revision_id)`. pricing_status is priced/partial/unpriced/unknown; estimated_usd is NULL unless all observed billable quantities have prices, independently of collection completeness. A stored estimated_usd can describe the fully priced observed measurement even when collection is partial. Public text/JSON/CSV all-in estimated_usd MUST nevertheless be null/blank whenever relevant collection coverage is partial/unknown; priced_subtotal_usd remains visible and qualified as observed consumption. This distinction prevents a priced observation from masquerading as a complete attempt estimate. Select the valuation matching measurement.revision; never sum historical snapshots. New measurements create new snapshots under the attempt's pinned revision; edits never recalculate old or in-flight consumption. Completed measurement revisions are terminal after bounded collection finalization; unresolved late evidence is diagnosed rather than silently rewritten.

### meter_valuation_dimensions — frozen quantities and applied rates

Columns: `valuation_id INTEGER`, `dimension TEXT`, `quantity INTEGER NULL`, `usd_per_million TEXT NULL`, `amount_usd TEXT NULL`, `pricing_status TEXT`.

PK `(valuation_id,dimension)`; FK valuation_id → meter_valuations(id) CASCADE. Dimensions include input_uncached, cache_read, cache_write_5m, cache_write_1h, cache_write_other, output. Known duration buckets use their exact rates, falling back to the generic cache_write rate from the same revision when absent; cache_write_other is the known nonnegative remainder. Unknown duration decomposition can use the generic rate on total writes, represented once as cache_write_other with documented total-fallback provenance in dimension status; never price both total and subsets. If duration is required for pricing and unknown with no generic rate, retain unpriced writes. reasoning stays an output subset and has no separate billable dimension. All frozen quantities and rates are exact. Arithmetic uses stdlib arbitrary-precision integers/rationals; round half-even to 12 USD decimal places per measurement/dimension; sum stored rounded dimension amounts for row and aggregate subtotals without additional rounding. Reject invalid/overflowed counters; monetary TEXT avoids SQLite int64 sum overflow and binary floating-point drift.

### Keys, queries and migration invariants

Join meter_attempts.run_id → runs.session_id → sessions.repo_id → repos.id. Reuse sessions UNIQUE(repo_id,issue_number) and runs UNIQUE(session_id,ordinal); indexes above support project-first joins, attempt-start ranges and agent/model filters. An additional runs(mode,session_id,id) index supports mode filtering; validate query plans with realistic fixture cardinalities before adding redundant indexes. Do not introduce a tasks table or duplicate repo/task identity columns. Store accessors enforce same-attempt observation/checkpoint references, same-revision valuations and model-key consistency transactionally where cross-table CHECKs cannot. Document and test each invariant against real SQLite.

## CSV snapshot and presentation contract

Required command is `litefactory meter export github:OWNER/REPO [--coding_agent <agent>] [--issue <positive-integer>] [--mode <research|plan|implement|spike>] [--time_from <RFC3339>] [--time_to <RFC3339>]`. A registered GitHub project is mandatory; no cwd/default/global fallback. AND filters; issue scoped by repo; time selection is [from,to) on attempt.started_at and includes the full selected attempt, including descendants finishing later. Parse offset-bearing RFC3339, normalize UTC, compare instants at stored precision without incorrectly rounding query boundaries outward; reject equal/reversed bounds.

Store owns one consistent snapshot of selected rows, ancestry, coverage and matching frozen valuations. A store-owned SQLite TEMP relation may materialize that projection in one bounded-context transaction; finish the transaction before CSV/stdout writes, then page the immutable temporary rows with bounded buffers and close cursors between pages. Temp relation is connection-local and dropped on completion/error; preserve the one-connection pool. Snapshot creation is capped at 1,000,000 projected rows, 256 MiB of serialized UTF-8 CSV payload including header and escaping, and 30 seconds of elapsed snapshot preparation; use the earlier caller deadline when shorter. Exceeding any cap returns an actionable narrower-scope error before the header, never silently truncates. Enforce row/byte ceilings while materializing bounded rows, not after allocating the entire result. These v1 limits are documented product constraints, not hidden truncation or tunable guesses; output draining remains caller-cancellable and does not retain the snapshot transaction. No permanent export tables, extra database connection or transaction held during a slow output writer. Do not keyset-page mutable live totals without a snapshot. Exporting does not recompute valuation. Capture running-attempt partial coverage as of that snapshot; later observations belong to later exports.

Fixed columns in order: `project,issue,run,attempt_id,attempt_started_at,attempt_finished_at,coding_agent,mode,invocation_id,parent_invocation_id,actual_model,model_provenance,input_uncached_tokens,cache_read_tokens,cache_write_tokens,cache_write_5m_tokens,cache_write_1h_tokens,output_tokens,reasoning_tokens,price_revision_id,estimated_usd,priced_subtotal_usd,pricing_status,collection_status,diagnostic_codes,row_kind`.

One row per exclusive invocation/model aggregate, plus coverage rows for attempts with no measurements and known invocations with none. Avoid duplicate attempt-root coverage rows for the same missing evidence. Coverage row numeric cells are blank, never additive placeholders; diagnostic_codes is a stable sorted machine-code list. measurement collection_status combines attempt and invocation/model status conservatively. Root/parent identifiers are metadata, no inclusive usage rows. Unknown fields blank; known zero 0. Use UTF-8, standard CSV quoting, UTC times, exact decimals and stable sort project/issue/run/attempt-start/id/invocation/model. Header only is a valid empty result. Stdout CSV only; stderr diagnostics; nonzero on writer error, which may leave partial stdout. Shell redirection owns filenames. Reconcile token sums from exclusive fields, never subsets; estimates are summed only from current frozen valuation snapshots, and unpriced/unknown coverage remains visible.

Spreadsheet text escaping applies before CSV quoting to every nonempty text cell, including source-derived identifiers, model/provenance and diagnostic cells. Prefix exactly one ASCII apostrophe when a cell starts with an apostrophe, contains leading tab/CR/LF, or its first non-Unicode-whitespace character is one of `=`, `+`, `-`, `@`. Leave all other text unchanged. Since original leading apostrophes are doubled and unchanged cells never start with an apostrophe, a consumer reverses escaping by removing exactly one leading apostrophe if present. Unknown empty cells stay empty. Typed validated integer/decimal and timestamp cells use their canonical serializers, never arbitrary provider text. Standard CSV quoting follows escaping; quoting alone is not spreadsheet formula protection. Test apostrophes, whitespace-prefixed formulas, tabs/newlines and a reversal round trip; document this escaping as part of CSV v1.

In-flight estimates may grow when additional measured work arrives. Immutability means price edits cannot change valuation of already captured work or the catalog used by any later child in that attempt; it does not freeze a running token counter.

## Complete new CLI inventory

- `litefactory meter [repository [task-id]] [--mode <mode>] [--ca <agent>] [--model <model>] [--json] [--details]`
- `litefactory meter prices list [--json]`
- `litefactory meter prices set <model> --coding_agent <agent> --input <rate> --output <rate> [--cache_read <rate>] [--cache_write <rate>] [--cache_write_5m <rate>] [--cache_write_1h <rate>]`
- `litefactory meter export github:OWNER/REPO [--coding_agent <agent>] [--issue <number>] [--mode <mode>] [--time_from <RFC3339>] [--time_to <RFC3339>]`

`meter prices` is a help grouping. Rates are USD/million; set copies catalog and replaces that exact model entry; omitted optional rates become unknown. No import, reprice, export-all or dedicated output-file flag is planned. The new explicit export names retain the requested underscores; existing planned summary --ca remains documented. No historical recovery utility enters the repository.

## Task Hints

### US-1-T1: Shared contract

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/agent. Use existing runner boundary and copied payloads, model-knownness and explicit source semantics; no provider names in exports. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-1-T2: Pure accounting

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/meter. Separate normalization, identities and subset arithmetic; never own I/O or time. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-1-T3: Capability proof

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: scratch probes. Require real version-pinned complete descendants and actual models before selecting adapters; no credentials in fixtures. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-1-T4: Contract verification

**Agent:** go-implementation-engineer with go-unit-test-engineer and go-code-reviewer

**Architectural hints:** Package placement: agent/meter tests. Exercise ownership, nullable zeros, regression and source compatibility; failures remain release blockers. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-2-T1: Claude collection

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/harness. Decode only source surfaces proven by US-1; retain process outcomes. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-2-T2: Identity and resume

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/harness. Connect parent IDs and verified model epochs to neutral sink; inclusive counters are evidence only. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-2-T3: Bounded finalization

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/harness. Use existing context/pipe lifetime, validated associated sources and bounded reads; no global sweep. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-2-T4: Claude verification

**Agent:** go-implementation-engineer with go-unit-test-engineer and go-code-reviewer

**Architectural hints:** Package placement: harness fixtures. Cover nested/parallel/failed/delayed children, duplicate/resume and complete coverage evidence. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-3-T1: Codex collection

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/codex. Use US-1 proven source, not configured-model inference. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-3-T2: Child models and counters

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/codex. Map thread ancestry and counter epochs through neutral normalization; do not count cached/reasoning subsets twice. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-3-T3: Incremental persistence

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/codex. Sink receives evidence before terminal output; missing final events preserve known usage and sticky failure. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-3-T4: Codex verification

**Agent:** go-implementation-engineer with go-unit-test-engineer and go-code-reviewer

**Architectural hints:** Package placement: codex fixtures. Version-pinned replay covers every required descendant/model and unsupported versions. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-4-T1: Durable entities

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/store. Own schema/checks/DTOs and context-first accessors; reuse runs/session/repo FK hierarchy. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-4-T2: Attempt lifecycle

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/turn. Allocate unique attempt and pinned revision before each subprocess including fallback; no transaction spans execution. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-4-T3: Idempotence and reads

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/store. Atomically record evidence/checkpoint/measurement and valuation; provide consistent bounded export snapshot and conservative coverage projection. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-4-T4: Store verification

**Agent:** go-implementation-engineer with go-unit-test-engineer and go-code-reviewer

**Architectural hints:** Package placement: store and turn tests. Real SQLite for populated migration, rollback, replay/restart and snapshot isolation; no time sleeps. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-5-T1: Catalog storage

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/store. Immutable copied full catalogs; pin absence and protect references, revision publication atomic. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-5-T2: Exact valuation

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/meter. Use decimal exact arithmetic and frozen dimension values with defined subset fallback and rounding. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-5-T3: Pin and finalize

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/turn. Attempt catalog never changes; accepted new work appends valuation snapshots, price edits do not touch them. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-5-T4: Pricing CLI

**Agent:** go-implementation-engineer with go-unit-test-engineer and go-code-reviewer

**Architectural hints:** Package placement: internal/cli. Constructor-bound flags, writer output and atomic validation; fixture tests cover in-flight edits and unknown prices. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-6-T1: Meter commands

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/cli. Required project export and validated AND filters; match complete documented inventory. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-6-T2: Text JSON CSV projections

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/cli and store. Use bounded immutable snapshot, stable ordering, standard CSV writer and null/zero/coverage semantics; no SQL in CLI. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-6-T3: Completion reporting

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: internal/turn and report. Pass prepared frozen values with revision and completeness; preserve render-before-ack and excluded helper label. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-6-T4: Presentation verification

**Agent:** go-implementation-engineer with go-unit-test-engineer and go-code-reviewer

**Architectural hints:** Package placement: cli/report fixtures. Test all five filters, cross-project issue identities, offset boundaries, empty results, quote/newline fields, writer errors and row-total reconciliation. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-7-T1: Integrated fixture scenarios

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: existing test packages. Link both adapters through store/prices/presentation/export, keeping current values distinct from valuation history. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-7-T2: Compatibility proof

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: scratch provenance. Pin real supported binary versions and reproduce probes only when capability evidence requires. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-7-T3: Acceptance and aggregate gates

**Agent:** go-implementation-engineer

**Architectural hints:** Package placement: existing verification workflow. Per-story evidence exact AC keys, immediate scoped review/tests then make check and safe go build ./.... Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

### US-7-T4: Release documentation

**Agent:** go-implementation-engineer with go-unit-test-engineer and go-code-reviewer

**Architectural hints:** Package placement: repository docs. Document CLI/schema/export bounds, rate precision, snapshot semantics, supported versions and exclusions; no historical recovery code. Follow APP_CONSTRAINTS; no work outside this story’s factory contract.

