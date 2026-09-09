# Drift Report — Token and Model Metering
# Branch: lf/73-we-need-to-add-more-information-about-a | Run: 0017

## Delivered as planned

- Reused the existing vendor-neutral agent usage contract and checked accounting primitives.
- Added migrations 0019/0020 for attempts, invocation ancestry, immutable evidence/checkpoints, exclusive measurements, pinned complete price catalogs and frozen dimensional valuations.
- Wired each execution/fallback attempt to durable usage capture, cancellation/failure finalization and prepared completion reports.
- Added exact user-editable local pricing with immutable revisions and per-attempt pins, including absence of prices. Historical/in-flight price edits do not revalue previous quantities.
- Added global/repository/issue metering, model/mode/agent filters, JSON/details, price list/set, and required-project CSV export with all five filters, fixed columns, reversible text escaping, bounded coherent snapshots and released database ownership during output.
- Added automated tests for accounting, persistence, migration/reopen, concurrency, immutable pricing, export and synthetic provider transport; scoped independent review corrected identified issues.

## Added scope

No product scope added. Review hardening and explicit partial-coverage diagnostics implement the approved accounting rules.

## Dropped or deferred

The owner explicitly deferred manual real-provider captures and authorized continuation of dependent stories. Authentic exact-version root/parallel/nested/failed-child/model-switch proof remains deferred; no passing evidence is fabricated for AC-1.4 or related provider release criteria.

US-2/US-3 collectors are implemented conservative subsets, not full provider support. Claude fresh assistant events can produce exclusive usage; resumed events remain nonadditive without a verified freshness/baseline. Current stream child forwarding is not proven and no associated-child transcript collector is present. Codex observes documented child topology and normalized summary quantities, but cumulative totals and unknown model attribution remain nonadditive; per-child quantities are unresolved. Therefore full descendant consumption is not yet delivered, beyond the manual test deferral itself. Public reports retain unknown/partial coverage rather than claiming complete totals.

US-7 authentic provider certification remains deferred; synthetic executable tests prove local transport/accounting boundaries only. The owner capture guide in docs/meter.md describes the required handoff.

## Architectural deviations

Provider implementation relies on current stream-only observation and conservatively declines unsafe inferred deltas. It does not claim that the approved complete provider surface has been established. SQL/store ownership, single connection, explicit migrations, immutable exact pricing, pure accounting and report dependency direction are preserved.

## Verdict

DRIFTED — durable metering/pricing/report/export functionality is implemented, while complete provider descendant/model collection and real-execution certification remain unresolved. The owner authorized continuing this work without waiting for those captures; full feature release must still disclose these gaps. Final aggregate gate results are recorded in the run manifest.
