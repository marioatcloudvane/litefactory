# Code Review — US-2
# Feature: a-finished-turn-can-stay-in-the-in
# Reviewed by: go-code-reviewer
# Date: 2026-09-09
# Status: CLEAN

## Scope
Frozen US-2, issue #193 (5399585045), revision `0c8dd256dc446ed6032df84f38ecd5f491df3d2ae7f78b8d24e5ddf986befc0c`, dependency US-1. Reviewed new `internal/daemon/worker.go` and production changes in `tick.go`, `schedules.go`, and `continuation_dispatch.go`, including immediate branch context and the shared plan. Independent test implementation is being handled separately; this report does not claim test execution.

## BLOCKING findings
None.

## ADVISORY findings
None outstanding.

### CR-A-002: Quiesce noncompletion check relies on a short observation window — resolved
**File:** `internal/daemon/worker_lifecycle_internal_test.go`, `TestWorkerLifecycleAC3CleanupBoundary`
**Category:** Test determinism
**Finding:** The 20ms select after starting the Quiesce goroutine can pass merely because that goroutine has not been scheduled. Other white-box assertions and production review establish the correct pool ordering, so this is a proof-strength concern rather than an observed product defect.
**Suggestion:** While the logger remains blocked, call Quiesce synchronously with a bounded deadline and require the deadline result; then release logging and require a fresh Quiesce to complete. This demonstrates the join boundary without relying on scheduling within a short negative observation window.
**Resolution:** Scoped re-review confirms synchronous Quiesce now requires `context.DeadlineExceeded` while the terminal logger is held, followed by release and a successful fresh bounded join. The defer still unblocks the logger on assertion failure. The post-join direct pool reuse check was removed; external real-Tick coverage remains. No new findings.


## Acceptance-oriented review
- AC-3: Ordinary, scheduled, and continuation launch sites all invoke `runWorker` exactly once after the existing attributed dispatch event. The wrapper's inner call waits for the body and its defers, then releases in-flight ownership. It emits one DEBUG terminal record with the original immutable admission logger before the outer deferred `pool.done`. Therefore Quiesce joins terminal logging, while the event deliberately does not claim the pool slot is already reusable. A panic unwinds admission without claiming normal completion; no recovery wrapper was added.
- Quiet drains and harmless bootstrap refusals complete. Refinement early return and unconsumed drains explicitly defer. Ordinary error paths record failed/cancelled status through `stopped` and `leave`, including errors intentionally swallowed by `leave`. Clean-drain backlog write failure also records failure while preserving its existing warning.
- Schedule lock contention and suspended recovery explicitly defer; outcome lookup, settlement, and descendant-reporting errors record failure/cancellation. Sequential task copies retain the same worker-state pointer, so settlement/refinement bookkeeping contributes to the original worker's terminal result. The status describes worker disposition, not whether the recorded agent run succeeded.
- Continuation final-observation failures mark deferred when successfully requeued, cancellation when observed, and failed when requeue fails. A successful decision to cancel ineligible continuation work is completed control work. No-run lock contention defers; other no-run failures mark failed, with durable-operation errors recorded. All existing error diagnostics remain.
- Cancellation dominates status when either the observed error/result identifies cancellation or the worker context is done at exit. Failed dominates deferred, preventing a swallowed failure followed by drain deferral from appearing successful. Status values are bounded and private.
- AC-4: The sole executor invocation is incremented immediately before `d.turn.Execute`, after mode/tool/permission decisions. Failed calls and locked dispositions count; early refusals do not. A shared worker-local pointer covers multiple sequential trigger calls and copied refinement tasks. Zero is reported numerically, without claiming idle or no useful work. Existing per-trigger `turn complete` logs remain distinct.
- Duration uses private `time.Now`/`time.Since` monotonic elapsed measurement, independent of the injected scheduling clock and tick grouping timestamp. No scheduling deadline/cadence/admission calculation was changed.

## Dependency and concurrency review
US-1 compatibility remains CLEAN: dispatch event placement and attributes, cloned source ownership, candidate deduplication/order, and refusal diagnostics are preserved. `runWorker` changes the launch target, not eligibility or counted handoffs. Worker result state is initialized and used on its owning goroutine; admission metadata remains immutable. Existing pool reservation/join and context propagation remain intact. No exported API, new persistent state, database query, extra forge request, or agent contract was introduced.

## Final test review
Reviewed `worker_lifecycle_test.go` and `worker_lifecycle_internal_test.go`. Coverage meaningfully exercises all three launch paths, zero-request bootstrap/control work, ordinary and durable deferred/failed outcomes, cancellation result/context, immutable matching terminal identity/source/tick context, nonnegative duration, pre-execution bootstrap and continuation observation failures, and preserved error/per-trigger diagnostics. A cleanup observer checks body cleanup and released in-flight ownership while logging still occupies the pool lifetime. Real subsequent ticks in the ordinary suite demonstrate capacity reuse; the tests do not run Tick inside a logger.

The test-only multiple-request bridge traverses actual private `d.run` with sequential copied tasks and an injected second-call failure. This usefully verifies shared accounting without claiming normal drain now batches spawning comments; the external ordinary test explicitly preserves one spawning request per worker. Shared observations use channels, atomics and the synchronized log buffer. The timing-based proof-strength advisory recorded above is resolved; no production change was required.

## Verdict
CLEAN — production, scoped tests, and US-1 dependency review have no outstanding findings. Bind the actual tester execution and snapshot before verification. Final aggregate checks remain required.
