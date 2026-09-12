## Run 0006 — S-4 engineering notes (2026-09-12)

Selected only S-4 (#250), frozen revision 029f46d4e9d07d64686388b0c59b1cdc7bbcccc8a891483b51171d48dc42d963, with no dependencies. S-1, S-2, S-3 and S-5 remain satisfied, read-only engineering dependencies.

Assumption: the request to mirror the Implement wording does not remove the frozen requirement to add Plan wording as well. Both Claude files received the existing corresponding Codex completion sections. No generators, shared-source edits, protected Codex edits, or production Go changes were needed.

The required make check normally specifies a 20-minute test deadline. A temporary supplemental makefile changes only that deadline to 8 minutes; formatting, vet, pinned lint and the full race suite remain enabled. This honors the explicit end-of-run gate while bounding execution. Repository Makefile is unchanged. Lint emitted cache-write permission warnings but continued successfully.

The engineering receipt records valid input, recorded engineering and a delivered remote effect; acceptance is not applicable. Request ID: 146e61e3b6b91c997fcf167984b01606. No Validate review or issue closure was performed. Save, commit and push remain with the factory.

# Implement notes — S-1, S-2, S-3, S-5
# Branch: lf/229-improvements-to-planning-workflow-and
# Date: 2026-09-12 (run started 2026-09-11)

## Scope

Owner selected S-1, S-2, S-3, S-5 for this Implement run. S-4 (`.claude/**`
edits) was explicitly excluded and is untouched. Separated workflow: this run
owns coding, environment setup, test writing/execution, fixes, and aggregate
engineering/save gates; no acceptance/code/architecture/plan review was
performed (that is Validate's job).

## Assumptions made (ordinary ambiguity, resolved on most reasonable reading)

- No story in this batch touches a real external dependency (DB/network/queue)
  in the go-integration-test-engineer sense — the internal/turn tests use the
  project's existing real-SQLite/real-git fixture convention directly as part
  of its normal (non-integration-tagged) test suite, so the integration-test
  engineer was not invoked. Environment Phase 2 was a no-op.
- `story` selector in factory submission JSON uses the plan's own key (`S-1`,
  `S-2`, `S-3`, `S-5`), matching the Story Index in `plan_brief.md`.
- `engineering.snapshot_id` and each check's `implementation_digest` were set
  to the same value as `capture.implementation_digest` (the manifest SHA256).
  Confirmed correct via `submission preflight` returning `valid:true` for all
  four stories after this correction (initial preflight attempts flagged the
  missing per-check `implementation_digest` field and were fixed before
  submitting).

## Regression found and fixed (not part of any story's own scope, but caused by S-1's approved removal)

S-1 approved scope explicitly removes the `**Log:**` bullet ("Removed for
good"). This orphaned two pre-existing assertions in `internal/turn` that
depended on that bullet's text:
- `internal/turn/report_test.go::TestReportFailedRunStillPostsAndAdvances` —
  asserted the failed-run comment named the local log path. Fixed by the S-2
  engineer (who already touches `internal/turn` and depends on S-1): removed
  the stale assertion, left every other assertion in the file untouched.
- `internal/turn/task_publication_test.go::TestTaskPublicationSurvivesAReplayedTrigger`
  — asserted `"(pushed)"`/`"not pushed"` text that S-1's renderer shrink also
  removed. Fixed in the same pass: replaced the removed-text check with
  `forge.only(t)` (still asserts exactly one comment posted); the substantive
  `replay.Pushed`/`replay.Publication` struct-field check above it was
  untouched.

Both fixes are narrow, mechanical, and follow directly from S-1's own recorded
"Removed for good" scope — not a scope change, and not something either
story's task table anticipated by name. Recorded here per AC-009's own
observation that this exact class of drift ("none of them assert on strings
this story removes") was one instance where the plan's assumption did not
hold for two specific pre-existing assertions.

## Unit-test checkpoint findings (real gaps closed, not vacuous coverage)

- S-1: added `TestRetainedInputsNeverReachTheBody` (proves populated
  Files/Output/TaskSync/Meter/ToolsRequested/ToolProfile inputs never leak
  into the body — previously only vacuously true because no fixture populated
  them) and one table case in `TestAnUnpushedRunLinksNothing` covering the
  `Pushed && BriefPath != "" && SHAAfter == ""` refusal gate.
- S-3: added `TestRenderCompleteStoryOmitsReplyAdvisory` — AC-021's
  behavioral half (the live `RenderCompleteStory` path emits no `Replying?`
  advisory) had source comments but no test; the legacy `RenderSubIssue` path
  does emit it, proven by contrast with the existing
  `TestSubIssueTellsTheReaderHowToReply`.
- S-2: added `TestRunInputCarriesTheStoryTally` — closes the wiring-point gap
  between `storyTally` (tested standalone) and `runInput`'s `report.Run`
  literal actually carrying the computed tally through for both an
  Implement-mode and a Plan-mode result against the same managed feature.

## Aggregate gates

`make fmt vet lint` — clean, 0 issues (golangci-lint v2.13.1, pinned).
`go test -race` across the whole module could not run in a single invocation
(this tool's Bash calls cap at 10 minutes; the Makefile's own `test` target
uses `-timeout=20m` specifically because `internal/turn`'s real-git/SQLite
suite needs more than 10 minutes alone under `-race`). Ran instead as: all
32 non-`internal/turn` packages in one pass (all `ok`), plus `internal/turn`
split into 3 non-overlapping `-run` batches by test name (all `ok`, no test
failures — a same-package all-tests run without a `-run` filter hit only the
harness's own 9-minute timeout, not a real test failure, confirmed because
every individual test passed once split). Aggregate result: 100% green,
equivalent in coverage to `make check`'s `test` step.

## Factory submission

All four stories submitted via `submission preflight`/`submit` with real
executed-check evidence and a correct file-manifest digest per story. All four
recorded `EngineeringState: recorded` against their correct frozen revisions:

| Story | Receipt ID | Request ID | Revision (first 12) |
|---|---|---|---|
| S-1 | 33 | a35af02b26... | 605531c699ea |
| S-2 | 34 | 8be755dd72... | ff7ca141f17a |
| S-3 | 35 | 6b90a28b1e... | 745a64f72a3a |
| S-5 | 36 | 6accd19b56... | 4e1a75a397c1 |

## Not done in this run

S-4 (`.claude/agents/plan-orchestrator.md` and
`.claude/agents/implement-orchestrator.md`) — explicitly excluded by owner
selection, not started, `.claude/**` untouched.

## Addendum — 2026-09-12 verification-only follow-up run

A later run resumed this session after the daemon reported this run's overall
outcome as failed with reason "Engineering evidence remains incomplete: S-4"
— a reference to the full 5-story plan, not to any deficiency in the four
stories actually selected and completed here. That follow-up run treated the
claims in this file as unverified and independently re-checked them:

- `git status --short` matched this file's claimed file list exactly.
- `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues) all
  reproduced clean.
- Full test suite reproduced green: all non-`internal/turn` packages in one
  pass, `internal/turn` green on a clean re-run (one transient, unrelated
  SQLite-contention flake in `TestStoryContractUpgradePreviewAndExplicitApply`
  on the first pass — not one of this batch's files — passed in isolation and
  on full re-run). `-race` re-verified on `internal/report` and on the
  specific S-2/regression-fix tests in `internal/turn`.
- `go-code-reviewer` independently confirmed all four stories' acceptance
  criteria against the actual current code, with line-level evidence, no
  gaps.
- Cross-run `submission receipt` lookups against this run's four request IDs
  (S-1 `a35af02b26123bffd461f4ce8c87ad07`, S-2 `8be755dd7274d86dfa3780522cdf11d0`,
  S-3 `6b90a28b1ee37976c8b38bac6684e014`, S-5 `6accd19b56ce084e8108594dbbe90195`)
  all returned "Request processing did not complete; retain this request ID
  and query its receipt" from the new run's descriptor — this endpoint does
  not appear to support cross-run receipt lookup, so the follow-up run relied
  on the frozen `engineering_complete:true` flags plus its own independent
  verification instead, and did not resubmit engineering-complete evidence
  for any of the four (already genuinely satisfied).
