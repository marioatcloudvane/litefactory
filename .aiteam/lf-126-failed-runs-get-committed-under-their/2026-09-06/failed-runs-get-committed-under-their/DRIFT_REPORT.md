# Drift Report — failed-runs-get-committed-under-their
# Branch: lf/126-failed-runs-get-committed-under-their | Date: 2026-09-07

## Delivered as planned

- **US-1** — `save`'s commit gate in `internal/turn/harvest.go` widened from
  `Outcome == OutcomeAbandoned` to `!runResult.Succeeded()`, covering Failed,
  TimedOut, Abandoned, NoResult in one gate. `commitAndPush` untouched, still
  the sole commit/push site, still reachable only on success. `save`'s doc
  comment rewritten; `internal/turn/doc.go` checked and found to need no edit
  (already described success-gated behaviour).
- **US-2** — `fileStoryIssues`'s guard in `internal/turn/subissues.go` widened
  from `runResult == nil || Outcome == OutcomeAbandoned` to
  `runResult == nil || !runResult.Succeeded()`, log-and-early-return doctrine
  preserved, comment updated to state the real reason.
- **US-3** — the "none — this run committed nothing" arm in
  `internal/report/run.go`'s `metadataList` now distinguishes success (old
  wording) from non-success (names the worktree path). `describeRunOutcome`'s
  Failed/TimedOut/NoResult arms state the work is still in the worktree.
  Delivered exactly to plan, with one implementation-discipline correction
  applied in the bug loop (see Dropped/Deferred — no, see below; this was a
  defect introduced by two uncoordinated passes both adding the mention, not
  a scope change, and is recorded under Architectural Deviations instead).
- **US-4** — `store.Run.Succeeded() bool` added to `internal/store/runs.go`
  with the exact two-arm rule from the plan. `PreviousFailed`/`PreviousReason`
  added to `harness.PromptInput`, populated only on the resume path in
  `prepare`, `SetSessionState(..., SessionRunning)` left as `prepare`'s last
  statement, not wrapped in `context.WithoutCancel`. `runs.error`'s doc
  comment extended for its second reader. The two `Succeeded()` predicates
  kept fully separate, as bound by plan_brief Shared Decisions — verified
  independently by the code reviewer.
- **US-5** — corrective-note constant added beside `askingRule`/
  `askingRevokedRule` in `internal/harness/prompt.go`; emitted in
  `writeResumeHeader` after the `- Feature slug:` line, gated on
  `PreviousFailed`, without touching the `"- Mode: %s\n"` literal. Security
  guardrail implemented via the **preferred** option: `PreviousReason`
  rendered inside `<note>` framing, passed through `capText`
  (which calls `neutraliseNoteTags`), with an additional independent
  `MaxPreviousReasonBytes` cap layered on top of the store's own 200-byte
  excerpt bound. `internal/harness/doc.go` updated to describe both US-4's
  new fields and US-5's corrective note in one section.
- **US-6** — `metadataList`'s Cost branch replaced `res.CostUSD > 0` with a
  `HasCost`/`HasResult`-driven three-way, matching the existing Exit-status/
  Duration sibling patterns. Storage layer untouched, as specified.

## Added scope

- **Bug-loop fixes (BT-001, BT-002)** — not in the original plan, but a
  direct, necessary consequence of parallel implementation of US-3-adjacent
  wording by more than one engineer pass on the same file
  (`internal/report/run.go`) without visibility into each other's edits.
  BT-001 removed a duplicate worktree-path mention from
  `describeRunOutcome`'s three non-success arms (keeping `metadataList`'s
  Commit-line arm as the single canonical site). BT-002 guarded
  `outcomeView.withFailure`'s own worktree-path mention on
  `run.CommitSHA != ""`, making it mutually exclusive with `metadataList`'s
  arm. Both are pure bug fixes restoring AC-3.1's literal requirement
  ("names the worktree path... " — implicitly, exactly once); neither adds
  behavior beyond what US-3 already specified.
- **Two new test files/functions surfacing the above**:
  `TestNonSuccessReportNamesTheWorktree` and
  `TestFailureAroundTheRunWithNoCommitNamesTheWorktreeOnce`, both in
  `internal/report/report_test.go`, not explicitly named in any story's Task
  table but directly derived from AC-3.1 and T-3.3, as the test-engineer role
  requires.

## Dropped or deferred

None. All six stories' Acceptance Criteria are implemented and covered by
tests; nothing was descoped.

## Architectural deviations

- **US-3's worktree-path mention landed twice, briefly, before the bug
  loop caught it.** This wasn't an architectural choice — it was a
  coordination gap between the batch-1 US-6 pass and the (interrupted,
  then recovery-verified) US-3/US-5 passes, all editing
  `internal/report/run.go` in the same run without seeing each other's
  diffs. The final state matches the plan's intent (AC-3.1) exactly; the
  detour is recorded here for visibility, not because the delivered code
  deviates from what was planned.
- No other deviation from `plan_brief.md`'s Architecture Notes was found by
  the code reviewer (verdict: CLEAN) or introduced during the bug loop.

## Verdict

CLEAN — delivered matches plan within acceptable micro-decisions. The one
notable event (a real double-mention defect, found and fixed over two bug-loop
cycles) was a self-inflicted coordination gap between independent engineer
passes on the same file, not a plan misread or scope change; the delivered
behaviour matches every story's Acceptance Criteria as written, and the whole
repository's `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` are green.
