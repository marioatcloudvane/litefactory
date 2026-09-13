# US-001 production implementation

Frozen identity: #260, revision 62b25689d99f55c6a162c9f02cfaa9a6200c2e2609e077d0917bc17f2a8c1a6c, dependencies none.

Production implementation is ready for the assigned test engineers. This is not acceptance or a claim that the required historical fixture/transaction regressions have passed.

## Files

- internal/codex/usage_historical.go: bounded historical descendant collection via shared source interpretation, timestamp attribution, source digests and replacement/content revalidation.
- internal/codex/usage_rollout.go: ancestry metadata remains visible for unchanged ancestors; timestamp-aware historical filtering, metadata conflict diagnostics, full-byte source attestation.
- internal/codex/usage.go: bounded final drain continues to discover delayed children rather than ending when only known nodes look finalized.
- internal/store/meter_recovery.go: truly read-only open without migrations, selector validation, original finalized window/catalog/root context, ledger fingerprint, conservative overlap rejection, preview simulation and atomic append/replay.
- internal/store/meter.go: extracted transaction-local observation helper; the ordinary observation API retains its finalized-attempt fence.
- internal/cli/cmd_meter_recover.go: meter recover repository task-id --attempt ID defaults to preview; --apply explicitly revalidates sources and ledger before append.
- internal/cli/meter_recovery_reconcile.go: neutral/store adaptation reusing meter.Reconcile and exact meter.Value; original catalog only.
- internal/cli/commands.go: recovery registration.

## Executed checks

- `go test ./internal/cli ./internal/store ./internal/codex -run '^TestUsageSyntheticTopologyAndLifetimeSummary$'` — passed; CLI/store compiled with no selected tests, Codex selected test passed.
- `go test ./internal/cli ./internal/store ./internal/codex -run '^TestUsageSynthetic'` — passed; CLI/store compiled with no selected tests, Codex existing synthetic tests passed.
- gofmt applied to changed production files.

## Decisions and limits

Recovery appends previously unmeasured descendants, preserving root measurements and attempt lifecycle metadata. An already partially measured descendant is refused unless every proposed response is an exact accepted replay; this avoids pretending an opaque preexisting total is exclusive. Historical synthetic roots retain their existing invocation identity; the provider thread comes from the persisted segment binding. Missing binding, overlap, malformed/unsupported sources and ambiguous attribution stay unresolved.

Sources are read and revalidated outside the atomic ledger transaction. Private transcripts and runtime state were neither modified nor copied to repository fixtures. Historical offline assertions still need extracted counters/provenance and reconstructed source envelopes. No present-day provider hierarchy or invoice certification is established.

## Required test handoff

Use prepareMeterRecovery(ctx, db, isolatedHome, attemptID) for CLI end-to-end tests; seed migrated temporary SQLite with original catalog/window, retained segment resume token, synthetic historic root and root measurement. Exact target is 6.754276 additional / 11.903026 resulting subtotal, preserving later runs. Exercise preview row/schema/checkpoint immutability, repeat/retried append, stale fingerprint, foreign event identity, overlapping/active attempts, prior descendant measurement conflict, source replacement/mutation and invalid selectors. Collector tests need parallel/nested/resumed/inherited/foreign/unchanged-ancestor/delayed/scan-limit cases. Test engineers own test files and comprehensive execution.
