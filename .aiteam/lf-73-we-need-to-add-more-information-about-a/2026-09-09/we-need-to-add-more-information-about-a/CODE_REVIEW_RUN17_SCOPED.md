# Run 17 scoped reviews

## US-2 and US-3 conservative collector changes

Reviewed runner wiring and provider usage collectors against the selected US-2 revision 7af4d756615b42e52e93e7e76b6555e237519432faf84cdf7bd050fcaebee9ea and US-3 revision e2b3d77fd0b65257b0bc9de438bdea0741b3afae11342aa3632a1cbe0a044d9c.

The review found that resumed Claude messages could initially enter additive totals without a verified baseline. The owner corrected this by retaining resumed observations as non-additive evidence. Re-review found no additional correctness or security defect in the conservative stream-only implementation. Independent `go test -race ./internal/harness ./internal/codex` passed (50.825s and 41.165s).

This does not verify full provider collection. Claude child forwarding/associated-child collection remains unproven, and all resumed Claude observations are non-additive. Codex retains cumulative summaries as non-additive evidence with unknown actual model: it currently produces coverage rather than exclusive token measurements. Real provider provenance is explicitly deferred by the owner. These functional limitations must remain visible; these stories cannot be represented as fully accepted merely because parser tests pass.

## US-4 base storage review

Reviewed migration 0019, meter store/validation/snapshot accessors, run orphan recovery, and turn preparation/harvest/meter wiring against selected revision bc0bb2b7afae5918280eca87092ef36cfd2423068f55c7a610bf09e74f86e15f. The transaction structure allocates attempts before each actual runner call, retains deduplicated evidence, prevents ancestry reassignment/cycles, and closes snapshot cursors before output. Millisecond bound ceiling plus exact instant filtering preserves the requested half-open interval. Pricing projection is deliberately pending US-5 and is not accepted yet.

Changes required before acceptance:

- Merge controlled diagnostic codes instead of replacing or retaining only an older array in attempt, invocation, rejection, and finalization updates. Later persistence/descendant gaps must not disappear.
- Preserve canonical mixed model provenance across measurement updates.
- Use a collision-free filtered invocation identity instead of slash concatenation.
- Move the bounded finalization timeout into the store-owned operation; turn must not introduce its own ambient timeout.
- Resolve reported lint findings and test the diagnostic/provenance regressions.

The snapshot byte-accounting callback must be documented as pure and free of I/O because it executes within the materialization transaction. This review is provisional pending fixes, pricing integration, and scoped tests; it is not a verified story event.

## US-6 base command and report review

Reviewed cmd_meter.go, meter_csv.go, command registration, report meter/run presentation, and docs/meter.md against selected revision 51612341ad8783cbdd9a71d2f20a495c8a7f4898a9535e18f23099073c7e7548. The initial review found explicit --issue=-1 accepted as omission, undetected short header writes, and homogeneous unknown/unpriced states collapsed into partial. The owner corrected these cases. Export validation, reversible formula escaping, page draining after cursor closure, and report dependency direction look sound.

Before acceptance, text details must include each row's usage and frozen amounts; complete-total fields must not present partial collection as complete, and the report-owned accumulator must independently suppress all-in estimates for incomplete pricing. Pricing wiring and monetary documentation remain pending US-5. Automated scoped tests and integrated tests remain required after these corrections. No complete US-6 verification is claimed by this provisional review.

## Corrected scope and pricing integration re-review

US-4 fixes were re-reviewed: attempt/invocation/measurement diagnostics now merge, repeated diagnostic lists are deduplicated before the bounded unique-code check, orphan recovery preserves prior diagnostics, model provenance is a canonical set, and filtered identities use a two-field tuple. Finalization deadlines belong to store; runner errors take precedence over a success-shaped result when classifying an attempt. Independent focused race tests for TestMeter and TestTurnImposesNoAmbientDeadline passed in store (13.471s) and turn (4.962s) before the final diagnostic cleanup; the integrated scope must retain its final post-change tests separately.

US-6 re-review confirmed explicit positive issue validation, short-write detection, preserved homogeneous unknown/unpriced status, complete-total suppression when collection is incomplete, independently suppressed all-in estimates when pricing is incomplete, and per-invocation text usage/amount/revision/diagnostics. Independent `go test -race ./internal/cli ./internal/report -run TestMeter -count=1` passed (7.893s and 1.831s) before the subsequent pricing integration.

US-5 selected revision 07886a68613013266e4b8add7de58400ba18749f22ca109125a0c35eebf7a9b7 was reviewed across pure pricing, catalog editing, transaction-held valuation validation, snapshot projection, command parsing and prepared completion-report mapping. Exact math separates write subsets, represents total fallback once, applies half-even rounding per dimension, and treats unknown quantities distinctly. Independent `go test -race ./internal/meter -count=1` passed (1.597s).

The pricing review required stronger storage validation: callback-provided valuations could omit dimensions or mismatch frozen quantities. The correction checks required dimensions, exact measured quantities and disjoint write representation, pinned rates, pricing statuses, bounded monetary strings, half-even rounding intervals and subtotal equality. These corrections were re-reviewed with no additional code findings. Snapshot valuation lookup matches measurement id/revision and the attempt-pinned price revision within the same materialization transaction. Report preparation reads stored values and does not query current prices or reprice historical observations.

The reviewed production implementation for US-4, US-5 and US-6 has no outstanding scoped code-review findings. Story completion remains conditional on final tests against the frozen integrated file set and criterion-keyed evidence. The initial findings above describe the review history, not outstanding defects. This does not resolve US-1/US-2/US-3 provider proof or functional limitations, or certify full US-7 release readiness. Aggregate checks and any later changes require separate evidence/re-review.
