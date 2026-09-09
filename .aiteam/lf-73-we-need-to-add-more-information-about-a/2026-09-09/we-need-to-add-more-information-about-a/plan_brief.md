# Plan Brief — Token and Model Metering
# Feature: we-need-to-add-more-information-about-a
# Branch: lf/73-we-need-to-add-more-information-about-a
# Date: 2026-09-09 — run 0010
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/73
# Citation base: 0d548269a37536f4557221b2a26b5ee51821ae43
# Research brief: none in this session; current owner scope and factory-supplied stories

## Shared Decisions

- Full managed-root and every causally related descendant invocation with actual-model provenance for Claude and Codex is mandatory in v1. Exact supported versions need real-execution proof; unknown models or missing children prevent complete status and do not satisfy the release gate.
- Extend internal/agent. Provider collectors live in harness/codex, attempt orchestration in turn, durable DTOs/SQL in store, presentation in cli/report, and pure normalization/reconciliation/valuation in internal/meter. Mutable payloads are copied across ownership boundaries.
- Reuse repos → sessions → runs as project/issue/run identity; add attempts → invocation ancestry → exclusive per-model measurements. Source observations/checkpoints retain replay and cumulative baselines. Inclusive totals reconcile, never add to exclusive rows. No new tasks table or duplicated repository identity.
- Nonnegative nullable int64 quantities separate uncached input, cache reads, cache writes and output. Cache-duration and reasoning fields are subsets. Zero is measured zero; null means unknown. Partial known subtotals remain qualified by collection status and controlled diagnostic codes.
- Full immutable price catalogs are pinned before attempt launch, including absent entries. Prices are exact USD/million decimals; maximum 9 fractional and 18 integral digits. Valuation uses half-even rounding at 12 USD decimals per measurement/dimension and sums stored amounts. Price edits never revalue completed or already observed in-flight work; new consumption extends the in-flight estimate using the original catalog. Only later attempts use new catalogs.
- Storage and presentation share measurement-revision-matched frozen valuations. Partial/unknown collection or pricing leaves the public all-in estimate null/blank; priced_subtotal_usd remains the known priced measured subtotal. Legacy provider-reported cost stays separately labelled. No historical repricing path is introduced.
- New command family is meter. Export requires an explicit registered `github:OWNER/REPO`; --coding_agent, --issue, --mode, --time_from and --time_to combine with AND. Time bounds are offset-bearing RFC3339 selecting complete attempts by start instant in [from,to); no per-token timestamp precision is implied.
- Export and reports share exclusive invocation/model grain, ancestry and coverage-only rows for absent measurements. They include all selected attempts and descendants, preserve unresolved attribution and exclude separately labelled daemon helper overhead. CSV field contract and command syntax belong to US-6; pricing command behavior belongs to US-5.
- Export snapshots matching measurements, coverage and frozen valuations before output. Store owns the temporary snapshot; no transaction/cursor or second database connection spans a slow writer. Preparation limits are 1,000,000 rows, 256 MiB serialized CSV and 30 seconds (or earlier caller deadline); exceeding a limit fails before the header with a narrower-scope diagnostic. No silent truncation.

## Workflow

- US-1 proves shared contracts and provider collection capabilities. US-2, US-3 and US-4 follow independently. US-4 establishes the immutable empty price-revision header baseline and pins it, so its schema does not depend on US-5.
- US-5 follows US-4, adds rates/frozen valuations and extends the usage snapshot with matching pricing. US-6 follows US-4/5 and supplies presentations/export. US-7 follows US-2 through US-6 and verifies the complete release.
- All seven stories form one coordinated v1. US-1..3 are unchanged planning specifications in this revision; this does not claim they are implemented. US-4..7 are updated from the supplied GitHub bases. No root-only provider support may ship in place of full descendants.
- During Implement, verify each selected story immediately with exact AC identifiers, scoped review and appropriate tests; publish only the supplied per-run evidence event. Aggregate gates remain required. Factory alone reads/updates forge state and verifies proposal publication.

## Cross-Story Constraints

- Store remains a leaf with context-first accessors, one SQLite connection, numbered embedded migrations and explicit migration. No transaction spans subprocess, forge or output I/O. Reuse current identity and retention conventions; preserve nullable historical cost.
- Preserve config purity, consumer-owned interfaces, report dependency direction, daemon/turn time ownership, subprocess classification and report watermark/transaction ordering. Metering completeness is independent of process success.
- Collector paths and source sessions must be validated as associated with this managed execution. No global transcript scans, expanded credentials, permission bypasses, prompts/raw transcripts in metadata/export, manual LF_HOME editing, daemon restart or dist writes.
- Use fixture/unit and real SQLite tests with race-safe synchronization. Exact-version provider probes stay manual and scratch-only; no committed tests launch paid agents. Implement must pass make check and safe go build ./...; never make build or invent integration tags.
- Follow shared exact decimal/unknown/subset semantics across store, pricing, CLI, CSV and reports. CSV escaping is reversible and spreadsheet-safe as specified in US-6; stdout contains only CSV and output errors are failures.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-1 | Define a shared usage contract and prove provider collection capabilities | [#172](https://github.com/marioatcloudvane/litefactory/issues/172) |
| US-2 | Collect complete Claude invocation and model usage | [#173](https://github.com/marioatcloudvane/litefactory/issues/173) |
| US-3 | Collect complete Codex invocation and model usage | [#174](https://github.com/marioatcloudvane/litefactory/issues/174) |
| US-4 | Persist attempts and deduplicated invocation measurements | [#175](https://github.com/marioatcloudvane/litefactory/issues/175) |
| US-5 | Manage editable prices and immutable attempt valuations | [#176](https://github.com/marioatcloudvane/litefactory/issues/176) |
| US-6 | Expose metering scopes, project CSV export and run summaries | [#177](https://github.com/marioatcloudvane/litefactory/issues/177) |
| US-7 | Verify full descendant metering and release readiness | [#178](https://github.com/marioatcloudvane/litefactory/issues/178) |

## Out of Scope

- Historical-log recovery/import/backfill/rollup code, scripts, commands or migrations in this repository. An external script is separate work, not a deliverable of this plan.
- Historical/in-flight repricing, remote automatic prices, invoice reconciliation, subscriptions/credits/taxes, and additional coding-agent implementations.
- Separate daemon helper overhead within managed-run totals. All managed-root descendants remain mandatory.
- Global CSV export, an import command or a dedicated output-file command. Export writes stdout; shell redirection selects the destination.

## Provenance

- Citation base: 0d548269a37536f4557221b2a26b5ee51821ae43. Run 0010 used the owner's new requirements, factory-supplied US-1..7 identities/bases/bodies and fresh requirements/architecture review. No live forge reads or inferred issue identities were used.
- Complete proposed table/field/key/type/index inventory and command inventory: [ARCH_REVIEW.md](ARCH_REVIEW.md). Managed story bodies own implementation contracts; US-4 owns usage schema and US-5 pricing schema. The review is supporting design context, not an editable story source.
- [feature-spec.md](feature-spec.md) records scope and headless assumptions: stdout CSV, explicit GitHub repository and whole-attempt start-time selection. Current-session proposal transports full bodies and supplied revision hashes; factory publishes and verifies it, then removes transient transport.
- Local validation establishes transport/criterion shape only; publication and story-state readiness remain factory-owned. Re-pin repository citations and exact provider/source versions before implementation if they change.
