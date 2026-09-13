# Architect: Go Architect
# Created: 2026-09-12

Verdict: APPROVED for a bounded implementation plan. No standards deviation required.
Source citation HEAD: `c5ebc85688e6348a4f54cd69071e9dc55296f892`.
Reviewed feature-spec.md, APP_CONTEXT.md, full APP_CONSTRAINTS.md, platform constraints, security scanner, task decomposer, workflow contract, story contract manifest and nested-agent evidence decision. This is architecture review, not executed engineering verification.

## System Overview

### How This Feature Fits

Keep three independently useful delivery stories: historical recovery plus collector regressions (spec US-1/US-2, AC-001–004 and recovery/provenance portion of AC-007); evidenced refusal (spec US-3, AC-005); presentation (spec US-4, AC-006 and presentation documentation portion of AC-007). The presentation story consumes the refusal facts, so its final integration depends on refusal. Recovery and refusal have no behavioral dependency; serialize overlapping shared-file edits if needed.

`internal/codex` owns rollout parsing, safe discovery and ancestry; `internal/harness` owns Claude terminal evidence. Both produce neutral `internal/agent` values. `internal/meter` retains pure normalization/reconciliation/valuation; `internal/store` exclusively owns SQL, transaction boundaries and frozen catalogs. `internal/cli` selects the requested task/run and renders the recovery preview/apply result. `internal/report` remains a pure prepared-value renderer. `internal/turn/meter.go` continues live persistence and attempt finalization; use it as the established mapping reference, not a second recovery runner.

### New Packages / Components

No new package, service, scheduler, dependency or general repair framework. Add a narrow recovery command and provider-owned read-only recovery entrypoint. A small store-owned historical append operation is necessary: current `meterOpen` in `internal/store/meter.go` rejects finalized attempts, and `meter_test.go` explicitly protects that fence. Do not remove that fence from live observation APIs. Recovery gets its own finalized-attempt-only validation path, using the existing ledger reconciliation/valuation machinery. New schema, if needed to retain source provenance or explicit refusal facts, must be an additive numbered embedded migration; do not rewrite migrations or assume widening free-form statuses is harmless.

### Key Boundaries

- CLI → provider adapter: explicit selected attempt/root/time context and bounded source access; adapter returns candidate neutral observations, invocation ancestry, source digests and unresolved reasons. No SQL or database callback in the adapter.
- CLI → store: preview reads current selected ledger and pinned catalog; apply presents validated candidates and expected current state. Store revalidates eligibility and deduplication inside its transaction.
- Store → pure meter computation: no filesystem reads, output, subprocess or nested database access inside reconciliation callbacks (single-connection safety, CONC-18–20).
- Harness → agent/turn/store: affirmative refusal evidence is an additive fact, separate from actual model, token quantities and pricing completeness. Existing process failure/fallback semantics remain authoritative.
- Store snapshot → CLI/report: retain old nullable fields; add explanatory refusal data where required. No report-to-turn dependency.

### Data Flow

Recovery: existing selector validation → selected finalized attempt and retained source context → bounded source extraction and exact candidate valuation → read-only preview. Apply repeats source/state validation → atomic candidate batch with stable response identities and original price revision → refreshed snapshot and applied/no-op/unresolved result. Existing root and later attempts are preserved. A repeated or interrupted operation may replay identical observations but must never rebill them. If candidate evidence overlaps existing cumulative measurement and exclusivity cannot be established, report unresolved instead of appending or replacing totals.

Live collection: prelaunch baseline → shared Codex discovery/ancestry → exclusive own-attempt deltas → existing sink. Recovery must share source interpretation and identity logic, but must not treat live prelaunch baselines as historical attempt boundaries. Retained UTC event times and attempt windows establish historical ownership; filenames or parent thread alone do not.

Refusal: existing structural resume refusal plus affirmative terminal zero turns/zero cost and absence of contrary activity → persisted provider-reported refusal fact → explicit summary/detail explanation. Never manufacture a model, zero token vector or token-derived valuation. Missing/nonzero/conflicting evidence remains unknown. The fallback is a separate durable attempt and retains its independent diagnostics.

### Concurrency & Context

No new goroutines. Carry caller context through bounded discovery and store calls; retain existing source limits and bounded final drain. Do not add polling below the daemon. Prepare bounded evidence outside transactions, compare stable digests/state before mutation, and commit a selected candidate batch atomically. Release snapshots/transactions before any output. Preserve the single SQLite connection and ctx-first accessors (CONC-2, CONC-5, CONC-18–19).

### Shared State and Side Effects

Recovery may append previously absent measurements on finalized attempts only through the explicit recovery operation. It must preserve attempt identity/outcome/timestamps, existing measurement revisions and existing valuations; new measurements use that attempt's retained catalog. Active attempts refuse. No synthetic replacement attempt or current-catalog repricing. A preview must not create attempts, checkpoints, migrations, coverage writes or repair journals; schema setup must not turn preview into mutation. Runtime application is an operator action after delivery, never a Plan/Implement step.

The proposed presentation wording applies to incomplete collection or pricing with a known subtotal: “observed lower bound at recorded rates; total unknown.” Wholly unknown is not zero. Confirmed refusal is visibly provider-reported zero and does not certify unrelated measurements or change the rule that all-in estimates require complete collection and pricing (CONC-20). Keep meter metadata absent from run comments.

## Per-task hints

Task identifiers below are architectural handles for story authoring; carry the matching hints into the typed tasks without creating per-story Markdown.

### T-001.1: Bounded historical source collection and preview
**Agent:** go-implementation-engineer

- **Pattern:** Reuse `internal/codex/usage_rollout.go` source parsing, safe file opens, model provenance and identities; explicit selected-attempt context distinguishes recovery from live baselines.
- **Package placement:** provider logic in `internal/codex`; command orchestration in `internal/cli/cmd_meter_recover.go` following `cmd_meter.go` selectors and registration conventions.
- **Key interfaces:** `agent.UsageObservation`, `UsageInvocation`, existing meter filter and snapshot values; a narrow concrete adapter result with candidates, provenance and unresolved reasons.
- **Watch out for:** run-5 children are not run-8 consumption; unknown ownership/model is a refusal to add usage. Bounded metadata-only ancestry can traverse an unchanged intermediate parent without charging it. Never read arbitrary transcript paths selected by provider text.
- **Not in scope:** daemon repair, blanket historical replay, root repricing, live capture.

### T-001.2: Atomic explicit apply with historical deduplication
**Agent:** go-implementation-engineer

- **Pattern:** Narrow append-only finalized-attempt operation with original catalog and stable response identities; preserve normal finalized-attempt rejection.
- **Package placement:** `internal/store` owns all SQL; `internal/meter` remains pure; CLI coordinates prepared evidence and output.
- **Integration points:** reuse `ApplyMeterObservation` validation/reconciliation internals without blindly relaxing `meterOpen`; explicit candidate batch validates before commit.
- **Watch out for:** repeated apply, interruption, concurrent state changes, prior inclusive/cumulative evidence and conflicting source digests. Preserve original values/revisions and pin new child valuations to the original catalog. Never hold SQL transaction across source reads.
- **Not in scope:** generic repair infrastructure, direct runtime DB mutation during development.

### T-001.3: Collector corrections and offline regression coverage
**Agent:** go-implementation-engineer for demonstrated corrections; go-unit-test-engineer for collector tests

- **Pattern:** Shared collector path for all modes, no Implement-specific collector branch. Separate ancestry visibility from measurable own-attempt activity.
- **Package placement:** `internal/codex/usage_rollout.go`, existing collector tests and sanitized testdata.
- **Watch out for:** parallel/nested children, unchanged parent with active grandchild, resume deltas, inherited/foreign records, children discovered within final drain, exhausted scan limits and later unavailable activity. Test deterministic input/discovery sequencing, not unbounded sleeps. A passing synthetic topology is not live compatibility certification (EXT-4–5).

### T-001.4: Recovery transaction and command verification
**Agent:** go-integration-test-engineer

- **Pattern:** Existing ordinary Go tests over a real migrated temporary SQLite store and real isolated fixture files (TEST-6); no integration build tag or naming convention.
- **Package placement:** store/CLI recovery tests beside owners.
- **Integration points:** preview/apply/replay, all-or-nothing interruption, changed-source/current-state refusal and unaffected later attempts.
- **Watch out for:** assert exact strings $6.754276 increment and $11.903026 subtotal, one root/three parented children, plus runs 6/7/8 unchanged. Retain provenance and identify reconstructed source envelopes as synthetic. Do not execute on the daemon DB.

### T-002.1: Persist affirmative zero-cost refusal evidence
**Agent:** go-implementation-engineer

- **Pattern:** Extend existing terminal evidence handling, not a configured-model fallback or error-text cost heuristic.
- **Package placement:** `internal/harness`, neutral `internal/agent` facts, `internal/turn/meter.go`, `internal/store` as needed.
- **Integration points:** structural refusal classification already feeding fallback; carry exact presence-aware zero-turn/zero-cost evidence through attempt persistence and snapshot output.
- **Watch out for:** numeric zero must be distinguished from absent fields. Do not infer the failed process version from the successful fallback. Keep pricing unknown when actual model/rate evidence is absent; explain the separate known provider cost rather than lying about token pricing.

### T-002.2: Refusal classification and persistence tests
**Agent:** go-unit-test-engineer; go-integration-test-engineer for any new store transaction

- **Pattern:** Retained affirmative refusal facts for the positive case; synthetic missing/nonzero/interrupted/actual-work/conflicting negatives.
- **Package placement:** harness/turn tests, store tests when persistence changes.
- **Watch out for:** actual-model remains unknown, fallback remains separate, non-refusal diagnostics survive. Run no paid providers.

### T-003.1: Clear partial-cost presentation and operator instructions
**Agent:** go-implementation-engineer

- **Pattern:** Render prepared snapshot facts; preserve machine-readable field semantics and add only necessary explanatory fields.
- **Package placement:** `internal/cli/cmd_meter.go`, `meter_csv.go`, `internal/report/meter.go` where shared rendering applies; `docs/meter.md` and provider-source documentation.
- **Integration points:** refusal fields from story 2, existing summary/details, JSON and CSV output.
- **Watch out for:** lower bound qualified by recorded rates; no invoice/fleet promise. Documentation distinguishes installing the feature from explicitly applying recovery. No restoration of removed run-comment metadata.

### T-003.2: Output compatibility verification
**Agent:** go-unit-test-engineer

- **Pattern:** Existing CLI/report fixtures for complete, partial, wholly unknown and confirmed-refusal states.
- **Package placement:** existing CLI/report tests.
- **Watch out for:** additive refusal fields, exact amounts, null all-in totals on incomplete evidence and independently retained fallback diagnostics. Include absence of meter metadata in run comments.

## Evidence Inventory Review

The feature-spec inventory is suitable for these bounded criteria. Carry its rows into each affected typed story, using final criterion IDs. AC-001/002 proof is offline execution over retained extracted historical facts, not a claim that reconstructed JSONL envelopes are authentic complete rollouts. Source hashes, timestamps, versions and sanitization notes must accompany fixtures. AC-004 proves enumerated collector behavior under fixture scheduling; no authentic current nested capture exists, so full current hierarchy certification remains unverified and outside this proposal. AC-005 positive evidence establishes the particular historical refusal; its process version remains unknown. No external nested capture or invoice evidence is a prerequisite for the promised offline regression/repair behavior. Do not close old #73/#125/#178 obligations from these checks.

## Architecture Notes

- Platform: layout/dependencies — existing internal packages only; no external libraries or new HTTP/service topology.
- Platform: errors — typed invalid-operation errors and explicit unresolved candidate results; preserve source causes and controlled diagnostics (CONC-3, STYLE-5).
- Platform: database — all SQL in store, ctx-first, one connection, embedded migrations only if necessary; no transaction spanning file or output I/O.
- Platform: testing — stdlib and existing fakes, real SQLite for atomicity, no paid-provider tests; implementation executes required `make check` and retains actual check evidence.
- Platform: interfaces — consumers own any needed small interface (TEST-5); prefer concrete existing APIs over speculative abstraction.

### Security Findings

Scope: prospective recovery/refusal/presentation changes only. Rulebook: APP_CONSTRAINTS.md SEC/CFG rules.

| Area | Status | Finding |
|---|---|---|
| Input validation | Concern addressed by design | Rooted no-follow bounded source access, selector validation and evidence/state revalidation are mandatory for recovery. |
| Authorization | Compliant design | Operator explicit apply; no background mutation or forge credentials. |
| Secrets/configuration | Concern addressed by design | Sanitize fixture content, retain provenance, never commit full private provider transcripts; paths under LF_HOME use config.Paths (CFG-8). |
| Data handling | Compliant design | Parameterized SQL exclusively in store; output controlled explanations, not raw provider prose. |
| Dependencies | Compliant design | No new dependency; no supply-chain audit is claimed. |
| Error handling | Compliant design | Unresolved evidence is visible without fabricated cost/model or sensitive source dumps. |

## Deviations

None. The normal finalized-attempt mutation fence remains intact; a narrowly scoped explicit historical append operation is the required deliberate store API extension, not a blanket relaxation. Architectural task grouping does not remove any feature-spec criterion.

## Run 0008 revalidation — 2026-09-13

Retained architecture reviewed against the saved feature spec and current source; APPROVED remains valid without scope changes or standards deviations. Worktree HEAD is still `c5ebc85688e6348a4f54cd69071e9dc55296f892`. The locally available `origin/main` is `60187208d9a9bcadcca8f01edb39ec11cac23c3e`; a scoped comparison finds no differences in `internal/codex`, `internal/harness`, `internal/agent`, `internal/meter`, `internal/store`, `internal/cli`, `internal/report`, `internal/turn/meter.go`, `docs/meter.md` or `docs/meter-provider-sources.md`. This checks the local remote-tracking reference, not a fresh authenticated remote observation or publication status.

Direct source review reconfirmed the finalized-attempt fence in `internal/store/meter.go`, unchanged-baseline skip in `internal/codex/usage_rollout.go`, and structural refusal versus terminal-cost handling in `internal/harness/runner.go`. The review and feature spec remain untracked retained artifacts; their presence does not establish earlier run success or published stories. `RUNTIME_EVIDENCE.json` is present and parseable with rollout, run metadata, Claude refusal and database sections. This retry did not re-extract private runtime records or execute tests; historical facts retain the provenance and limitations stated above. No implementation landed in the current worktree diff. Factory submission/publication verification is owned by the Plan orchestrator.
