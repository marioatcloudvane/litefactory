# US-4 — A resumed run knows its predecessor failed

> As a repository owner, I want a run that resumes a failed predecessor to
> know that its predecessor failed, so that litefactory can tell it so rather
> than letting it read the transcript's last claims as settled fact.

Adds a derived success predicate on the stored run row and threads it, plus
the predecessor's failure reason, into the prompt-building input for a
resumed run.

## Why This Matters

When litefactory picks a failed conversation back up to try again, it hands
the AI agent the entire previous transcript — including whatever the agent
said right before it failed, which often reads like a finished, successful
report. Nothing today tells the resumed run "that wasn't actually true." This
story is the plumbing half of the fix: it works out, from what's already
stored about the previous attempt, whether it genuinely succeeded, and makes
that fact available to be told to the next run (US-5 does the telling).
Resume itself keeps working exactly as it does today — the point is to make
the resumed run better informed, not to make it start over.

## Architecture Notes

- Add `store.Run.Succeeded() bool` to `internal/store/runs.go`: true iff
  `Status == RunSucceeded`, or `Status == RunReported && Error == ""`. This is
  a second, distinct predicate from `agent.RunResult.Succeeded()` (used by
  US-1/US-2) — the two must never be collapsed or converted into each other.
  See plan_brief Shared Decisions.
- No schema change: `Status` and `Error` are already columns; `runs.error` is
  empty for exactly one outcome (success) and survives the move to `reported`
  status, which `status` alone does not. No migration file.
- Add `PreviousFailed bool` and `PreviousReason string` to
  `harness.PromptInput`, doc-commented in the same "read only on the resume
  path" style as the existing `PreviousMode` field. Populate both only when
  `resume` is true, from the `previous` `store.Run` that `prepare` already
  holds — no new field is needed on `turn.preparation`.
- Concurrency note: `SetSessionState(ctx, session.ID, store.SessionRunning)`
  must remain `prepare`'s last statement before a successful return and must
  not be wrapped in `context.WithoutCancel`. The two new field assignments
  belong in the `PromptInput` literal above that call — do not relocate the
  call.
- `resumeDecision`'s signature and logic are unchanged — it still answers only
  "is there a conversation to resume," which stays yes for a failed
  predecessor (plan_brief Shared Decisions). This story adds a second, separate
  signal for whether that conversation's self-report can be trusted; it does
  not gate resume itself.
- Extend `runs.error`'s doc comment to record its new second reader: it is
  prompt input now, not only report/log text.
- Two of this story's acceptance criteria are pinned by test only, not by new
  code — don't "fix" what's already correct: `resumeDecision` already returns
  true for a failed Claude predecessor, and the cold-rerun path already copies
  the whole `PromptInput` and ignores fields it doesn't read.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | Add `store.Run.Succeeded()` to `internal/store/runs.go` with a doc comment stating the two-arm rule and why `RunReported && Error == ""` is the durable success signal | logic | — | S |
| T-4.2 | Extend `runs.error`'s doc comment to record its new second reader — it is now prompt input, not only report/log text | logic | T-4.1 | XS |
| T-4.3 | Add `PreviousFailed` / `PreviousReason` to `harness.PromptInput`, doc-commented in the same "read only on the resume path" style as `PreviousMode` | api | — | S |
| T-4.4 | Populate both fields from `previous` in `prepare`'s `PromptInput` literal, leaving the `SetSessionState` call untouched as `prepare`'s last statement | logic | T-4.1, T-4.3 | S |
| T-4.5 | Table tests for `store.Run.Succeeded()` across all four statuses × empty/non-empty `Error` | test | T-4.1 | S |
| T-4.6 | Tests that `prepare` sets both fields correctly for a succeeded, failed, timed-out, and absent predecessor, and that `resumeDecision` still returns true for a failed Claude predecessor | test | T-4.4 | M |
| T-4.7 | Test that `SetSessionState(..., SessionRunning)` remains `prepare`'s last store write and is not wrapped in `context.WithoutCancel` | test | T-4.4 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | `store.Run.Succeeded()` returns: true for `succeeded`; true for `reported` with an empty error; false for `reported` with a non-empty error; false for `failed`; false for `running`. |
| AC-4.2 | `resumeDecision` still returns true for a failed Claude predecessor on the same segment: resume is not disabled. |
