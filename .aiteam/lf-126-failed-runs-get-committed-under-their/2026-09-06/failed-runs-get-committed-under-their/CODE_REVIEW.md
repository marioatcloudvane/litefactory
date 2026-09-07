# Code Review
# Feature: failed runs get committed under their failure text (#126)
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: CLEAN

---

## Scope reviewed

Read every file the six stories touched, on disk as it currently stands:

- `internal/turn/harvest.go` (US-1)
- `internal/turn/subissues.go` (US-2)
- `internal/store/runs.go` (US-4)
- `internal/turn/prepare.go` (US-4)
- `internal/harness/prompt.go` (US-4, US-5)
- `internal/harness/doc.go` (US-5)
- `internal/report/run.go` (US-3, US-6)
- `internal/turn/doc.go` (checked for the commit-on-failure passage US-1 was
  supposed to remove — confirmed there was none to remove)

`internal/report/report_test.go` and the golden files under
`internal/report/testdata/` were touched by US-6/US-3 but are test
files/fixtures, out of this review's scope per the reviewer brief; their
correctness is covered by `go test`, which is green.

`go build ./...`, `go vet ./...`, `gofmt -l .`, and `go test ./...` (full
tree) all ran clean with no findings.

---

## BLOCKING findings

None.

---

## ADVISORY findings

None.

---

## Verification notes

- **US-1/US-2 gate widening**: `save`'s gate in `harvest.go` is
  `!runResult.Succeeded()` (nil-safe `agent.RunResult.Succeeded()` via the
  `harness.RunResult = agent.RunResult` alias, confirmed at
  `internal/agent/result.go:171`), and `fileStoryIssues`'s guard in
  `subissues.go` is `runResult == nil || !runResult.Succeeded()`. Both cover
  Failed/TimedOut/Abandoned/NoResult in one predicate, exactly as specified.
  `commitAndPush` is untouched and reachable only through `save` on success.
  `turn.Result.Succeeded()` (`result.go:236`) — the ring-fenced method — was
  not touched by either file. `internal/turn/doc.go` was checked in full and
  contains no passage arguing commit-on-failure, confirming the US-1
  engineer's report that no edit was needed there.
- **Shipped together**: both US-1 and US-2's gates are present in the same
  worktree state, satisfying the "must ship together, never alone" cross-story
  constraint.
- **`gate_test.go` untouched**: `git log` shows no commit touching
  `internal/turn/gate_test.go` on this branch, and it still asserts three
  `CloneGate.Acquire` call sites — the pinned count the plan brief requires to
  stay unchanged.
- **US-4 two predicates kept distinct**: `store.Run.Succeeded()`
  (`internal/store/runs.go:156`) reads `Status`/`Error` on the stored row;
  `agent.RunResult.Succeeded()` reads a live outcome. Neither calls into the
  other, and no conversion helper was introduced between them anywhere in the
  touched files. `internal/turn/prepare.go` gates `previousFailed`/
  `previousReason` strictly inside `if resume { ... }` and negates
  `previous.Succeeded()` — never reading the live-run predicate for a stored
  row. `SetSessionState(ctx, session.ID, store.SessionRunning)` remains
  `prepare`'s last statement before a successful return, not wrapped in
  `context.WithoutCancel`, with the two new field assignments sitting in the
  `PromptInput` literal above it, as required.
- **No schema change**: confirmed via `internal/store/migrations/` — no new
  migration file; `Run.Error`'s existing column backs `Succeeded()`.
- **US-5 security guardrail**: `writePreviousFailedNote` routes
  `PreviousReason` through `capText` (which calls `neutraliseNoteTags` then
  `truncateWithMarker`) and frames it inside the same `<note>`/`noteFraming`/
  `Source:` element every other untrusted body in the prompt uses, via the
  shared `note()` helper, with an independent `MaxPreviousReasonBytes` cap on
  top of the store's own 200-byte `excerpt` bound. This is the "Preferred"
  option the plan brief called for, not the fallback. The `"- Mode: %s\n"`
  literal is untouched in both `writeHeader` and `writeResumeHeader`. The
  insertion point in `writeResumeHeader` is immediately after the "- Feature
  slug:" line and before the infer-guidance/asking blocks, gated strictly on
  `in.PreviousFailed`, which is the only point that keeps both of those
  blocks byte-for-byte unchanged when the predecessor succeeded (empty-reason
  case handled by `writePreviousFailedNote`'s own `TrimSpace` check).
  `BuildPrompt` remains pure — no clock, filesystem, or env access in the new
  branch.
- **US-3**: `metadataList`'s Commit branch and `describeRunOutcome`'s
  Failed/TimedOut/NoResult arms (plus `withFailure`) all render
  `worktreeNote`/`inWorktreeAt`, which quotes the path exclusively through
  `codeSpan`. The success-with-no-changes wording ("none — this run committed
  nothing") and the non-success wording ("none — this run did not succeed;
  if it produced anything, it is still in the worktree...") are textually
  distinct, satisfying AC-3.1's "must not use the same words" requirement.
- **US-6**: the Cost branch in `metadataList` (`internal/report/run.go:620`)
  is now `res.HasCost`-driven with an explicit "unknown" default, matching
  the sibling Exit-status/Duration patterns in the same function, rather than
  the old `res.CostUSD > 0` comparison that conflated "unknown" and "exactly
  zero".
- **No dead code / TODOs**: grepped all seven touched files for
  `TODO`/`FIXME` — none found.
- **Full suite green**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
  `go test ./...` (whole tree) all pass with no findings, including
  `internal/turn`, `internal/store`, `internal/harness`, and `internal/report`
  in isolation.

---

## Cross-story summary

The six stories integrate cleanly at every boundary this review checked. US-1
and US-2 share one predicate and one wording pattern for "did not succeed,"
and both landed together as required. US-4 introduces a second,
independently-documented `Succeeded()` predicate on `store.Run` without ever
touching or converting through the pre-existing `agent.RunResult.Succeeded()`
— the one collapse the plan brief explicitly forbade. US-4's new
`PromptInput` fields flow into US-5's prompt rendering through a single,
narrow gate (`resume`), and US-5's security guardrail around
agent-authored `PreviousReason` text is implemented via the preferred
`<note>`-framed option, consistent with every other untrusted body already
in the prompt. US-3 and US-6 are independent, read-only changes to the report
renderer that sit in adjacent, non-overlapping branches of the same function
with no interference. No new import edge appeared between `internal/report`
and `internal/turn`, and nothing outside the specified `internal/` packages
was touched.

---

## Verdict

CLEAN — No findings. Proceeding to environment setup.
