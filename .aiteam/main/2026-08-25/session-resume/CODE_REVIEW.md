# Code Review
# Feature: session-resume
# Reviewed by: go-code-reviewer
# Date: 2026-08-26
# Status: ADVISORY_ONLY

---

## Scope reviewed

Working tree diff (uncommitted) against `IMPLEMENTATION_PLAN.md` and the authoritative
`CLAUDE_CLI_CONTRACT.md`. Files read in full: `internal/harness/{doc,errors,meta,prompt,runner}.go`,
`internal/naming/session.go`, `internal/turn/{harvest,prepare}.go`, plus every changed/added test file
(`internal/harness/{meta_test,prompt_test,runner_test,prompt_history_test}.go`,
`internal/naming/session_test.go`, `internal/turn/{execute_test,fixture_test,harness_test,history_test,resume_test}.go`).
Confirmed `internal/daemon/tick.go` SHA-256 is unchanged
(`9a308a98bbe613d40c0f00fdd4af046102f13403d86ca8d7f5e0f9aec51e3ef7`) and that no migration file was
added under `internal/store/migrations` (only `0001_init.sql` exists).

## Invariant checklist (from the routing brief)

1. `internal/daemon/tick.go` byte-identical — **confirmed** (SHA matches).
2. `--continue` never constructed — **confirmed**; the only occurrences of the string are in comments
   explaining why it must never be used (`internal/harness/runner.go:162,182,371`).
3. `--resume`/`--session-id` mutual exclusivity — **confirmed**; `Runner.args` (`runner.go:388-412`)
   sets a single `sessionFlag` variable, never both.
4. Structural resume-refusal classifier — **confirmed**; `resumeRefused` (`runner.go:599-604`) is
   exactly `resuming && Outcome==OutcomeFailed && Model=="" && ExitCode>0`, matching the contract
   doc's correction (not the plan's incorrect `Events==0`). `TestResumeRefusedClauses`
   (`runner_test.go`) pins every clause, including the `Events==1`/`HasResult` case that would break a
   naive `Events==0` implementation.
5. Fallback fires exactly once, cannot loop, cannot fire on cold/timeout/abandoned/`ErrStartFailed`,
   reuses the same run row/ordinal — **confirmed** by reading `runClaude` (`internal/turn/harvest.go:93-142`)
   and by `TestResumeRefusalRerunsOnceCold`, `TestResumeRefusalDoesNotFireOnOtherFailures`,
   `TestResumeRefusalDoesNotLoop`, `TestResumeRefusalIsMatchedThroughWrapping` (`resume_test.go`). The
   second attempt sets `Path = PathColdAfterResumeRefused`, which is never `resuming()`, so a second
   refusal cannot recurse — it is reported as an ordinary failure.
6. Identical tool boundary on resume/cold — **confirmed**; `args()` builds `--tools`/`--settings`/
   `--allowedTools`/`--strict-mcp-config` identically regardless of `resume`, and
   `TestRunResumeSwapsOnlyTheSessionFlag` asserts the two argvs are equal apart from the session flag.
7. No migration/new table/column; `FirstSegmentOrdinal` stays `1` — **confirmed**.
8. `BuildPrompt` stays pure and the only exported builder; `internal/turn` never assembles markup —
   **confirmed**; `commentHistory`/`historyRole` in `prepare.go` only map store values onto
   `harness.HistoryEntry`/`harness.HistoryRole`, no markup.
9. `internal/harness` does not import `internal/store` — **confirmed** (no such import in any changed
   harness file).
10. Triggering comment not duplicated into history; resumed run carries no history block —
    **confirmed**; `commentHistory` filters `comment.ForgeCommentID == req.Trigger.CommentID`, and
    `BuildPrompt`'s `in.Resume` branch returns before `historyNote` is ever called. Covered by
    `TestColdRunCarriesTheThreadAsHistory`, `TestResumedRunCarriesNoHistory`,
    `TestResumedPromptCarriesNoHistory`.
11. A historic `/spike …` never appears under `## Instruction` — **confirmed**;
    `TestHistoricCommandIsNotTheLiveInstruction` cuts the prompt at `## Context` and asserts the
    historic string is absent from the instruction half and present exactly once, in the history note.

No violation found on any of the eleven invariants.

## Cross-story consistency

The three stories share `prompt.go`, `runner.go` and `prepare.go` cleanly. `Request.ClaudeSessionID`
(S1) is read by `Runner.args` (S2) without re-derivation; `PromptInput.Resume`/`History` (S2/S3) are
populated once in `prepare.go` and consumed only inside `BuildPrompt`. The one seam worth calling out —
`runClaude`'s fallback rebuilding `prep.promptInput` with `Resume` flipped — is exercised end to end by
`TestFallbackColdRerunCarriesTheHistory`, which is exactly the seam AC-SR-2.8 depends on. No dead code,
no contradictory doc comments found in the touched files themselves; `doc.go` was updated in the same
commit as the flag change it documents.

`TestSecondRunResumesInsteadOfReusingTheSessionID` (`internal/turn/harness_test.go:240`) does exercise
the regression through a real `*harness.Runner` and a fake `claude` on `PATH`, asserting on the child's
actual argv (`--resume` present with the stored id, `--session-id` absent, tool-boundary flags present,
`meta.Resumed` true). It is not vacuous — it would fail against the pre-change tree, where the runner
re-derives the session id and always emits `--session-id`.

## ADVISORY findings

### CR-A-001: Doc-comment typo
**File:** `internal/harness/runner.go:596`
**Story:** US-002 / T-SR-2.1
**Category:** Idiom
**Finding:** `resumeRefused`'s doc comment reads "the SD-7 missing---verbose failure" (triple hyphen).
**Suggestion:** `missing --verbose failure`.

### CR-A-002: Invocation contract doc left describing only the cold path
**File:** `internal/config/config.go:92-96`, `internal/store/segments.go:26-29`
**Story:** US-002 (cross-cutting)
**Category:** Standards / Consistency
**Finding:** Both doc comments still show only `--session-id <uuid>` as *the* invocation and describe
`ClaudeSessionID` as "the value passed to `claude --session-id`" without noting that every run after
the first passes it to `--resume` instead. `internal/harness/doc.go` was correctly updated in this
change; these two files, which describe the same contract from the config/store side, were not, and
now read as slightly incomplete next to it.
**Suggestion:** A one-line addition to each, pointing at `internal/harness/doc.go` or
`Runner.args`'s doc as the fuller account, would keep the three descriptions from drifting further
apart. Not required for this change since neither file is on the story's task list, but worth a
follow-up.

### CR-A-003: Two full-table reads per prepared turn
**File:** `internal/turn/prepare.go:135` (`previousRun`), `internal/turn/prepare.go:149` (`commentHistory`)
**Story:** US-002 / US-003
**Category:** Performance
**Finding:** `previousRun` calls `ListRuns` and scans every run row of the session in Go to find the
last terminal one; `commentHistory` calls `ListComments` and reads every comment row on every prepared
turn, including on the resume path where the result is only used if the fallback fires. Both are
architecturally deliberate (documented in `prepare.go`'s comments and in the plan's Architecture Notes
— "a read and a filter in Go", "read on every path even though only a cold prompt renders it") and are
not schema changes, so this is not a defect, but on a task with a long run/comment history the
per-prepare cost grows linearly with the task's whole lifetime rather than being bounded.
**Suggestion:** No action needed at this scale (SQLite, single-digit-to-low-hundreds rows per task);
worth revisiting only if a task's run or comment count grows large enough for it to show up in
practice.

## Verdict

ADVISORY_ONLY — 3 suggestions noted, none blocking. All eleven routing-brief invariants hold, the
regression test is non-vacuous, and the resume/fallback/history mechanics match the verified CLI
contract rather than the plan's one incorrect detail (the resume-refusal classifier). Proceeding to
environment setup is safe from this review's perspective.
