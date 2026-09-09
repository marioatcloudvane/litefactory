# Drift Report — a-finished-turn-can-stay-in-the-in
# Branch: lf/187-a-finished-turn-can-stay-in-the-in | Date: 2026-09-09

## Delivered as planned

- US-1 (#192): ordered, duplicate-free discovery source attribution and exactly one admitted-worker DEBUG dispatch event across ordinary, scheduled and continuation paths; refusal diagnostics and regression coverage verified.
- US-2 (#193): private worker disposition/request accounting; body cleanup then in-flight release then terminal logging then pool release; cancellation and early-return coverage; existing per-trigger/error diagnostics preserved.
- US-3 (#194): additive summary aliases, operator/package explanations, repeated two-parent quiet polling, unchanged-parent-timestamp child regressions, and successful aggregate `make check`.
- Each story received independent scoped review and executed criterion-named evidence. Overlapping snapshots were invalidated before writes and reverified on stable bytes. Final aggregate review is CLEAN.

## Added scope

- None. Private `worker.go`, focused test files, and validation artifacts implement the approved diagnostic contracts without adding packages, schemas, configuration or exported APIs.

## Dropped or deferred

- None within approved implementation scope. Production session identification/deployed-revision confirmation and deployment remain explicitly out of scope.

## Architectural deviations

- No production deviation. The current ordinary drain yields after one spawning comment; multi-request accounting is tested through a deliberately constructed private worker using two real `d.run` executor invocations and sequential task copies, while normal yielding remains covered separately.
- README lacked a dedicated logging section, so the approved operator guidance was added as a focused polling diagnostics section.
- Explicit managed-story instructions superseded stale skill wording about local story Markdown and externally configured integration environments. Frozen inline revisions and local HTTP/SQLite fixtures were used throughout.

## Verdict

CLEAN — delivery matches the approved frozen plan. All eight acceptance criteria, scoped reviews, and final aggregate gates passed. No follow-up planning cycle is needed for this implementation.
