# Implement notes — run 0008

Resumed frozen US-002 on 2026-09-11 at clean HEAD 08f488c, the factory merge over retained d376d08. US-001 remains the supplied read-only engineering-complete dependency.

The fresh reproduction exposed overlapping 100-request background list reservations against capacity 120. One empty history could defer another before refunds, leaving bootstrap incomplete. The retained timeout stack also showed the backoff test waiting for a worker signal that this deferral prevented.

The implementation serializes only concurrent paginated discovery calls with context cancellation, settling/refunding before releasing the next waiter. It does not wait for quota refill and leaves scalar accounting and active traffic outside that serialization. This preserves the frozen requirement that parent-comment discovery remains budgeted.

Test additions cover deterministic overlap/refund/cancellation and foreground reads while discovery is occupied. The backoff fixture now emits an explicit failure after five seconds if its rendezvous never arrives, preserving ordering assertions. No production timeout was extended.

All five named regressions passed freshly and in repeated/race runs. A complete daemon package run terminated naturally in 32.080s. Final aggregate result is recorded in TEST_REPORT.md. No live endpoint or production savings measurement, runtime administration, acceptance review, or git writes occurred.

The first aggregate run passed the complete daemon race package naturally (427.440s), but exposed a separate instruction-parity omission in `.claude/agents/research-orchestrator.md`. Restored its standard shared-workflow handoff in the owned tree; focused `TestBothInstructionTrees` race check passed (2.057s). The original failing aggregate output remains retained.
