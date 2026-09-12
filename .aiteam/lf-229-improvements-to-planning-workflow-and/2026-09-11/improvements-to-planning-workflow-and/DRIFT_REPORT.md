# Drift Report — Slimmer run-completion and story comments (S-1, S-2, S-3, S-5)
# Branch: lf/229-improvements-to-planning-workflow-and | Date: 2026-09-12

Scope note: the owner selected S-1, S-2, S-3 and S-5 for this Implement run.
S-4 was explicitly excluded and is not assessed here — it is neither delivered
nor dropped, it was simply not part of this run's authorized scope.

## Delivered as planned

- **S-1** — `internal/report/run.go`'s `fixedSection`/`metadataList`/`runBody`
  ladder shrunk exactly as specified: kept the non-success default Commit arm,
  the `CommitSHA != "" && !Pushed` Publication bullet, the bare Research-brief
  link, and the unchanged `syncSection` (Base branch sync) disclosure; deleted
  `taskSyncSection`, `filesSection`, `outputSection` and every helper/const
  they orphaned (`outputSummary`, `outputOverhead`, `elisionNotice`,
  `droppedOutputNotice`, `fitOutput`, `toolsEntry`, `notGranted`, `codeList`,
  `logsCommand`, `outputText`, `lastLines`, `elideMiddle`, `countLines`,
  `TailLines`, `maxFileEntries`, `filesHeading`). All 17 affected golden
  fixtures regenerated and hand-reviewed. AC-010's repository search performed
  and recorded with a clean result.
- **S-2** — `report.StoryTally{Done, Total int}` and `Run.Stories
  *StoryTally` added; `describeOutcome`'s partial arm applied only after the
  existing failure fold, `succeed: true` retained; `internal/turn`'s
  `storyTally`/`storyDone` helper added beside `briefPath`, gated on
  `result.Run.Mode == string(agent.ModeImplement)`, wired into `runInput`.
  Done-predicate is the exact union specified (`EngineeringComplete || state
  in {verified, completed}`).
- **S-3** — `RenderStoryProgress`'s format string trimmed to story key, state
  sentence, parent-issue link and indented evidence block; every existing
  validation (guard, per-kind digest check, 20,000-byte cap, indentation
  defence) left untouched. Source comments added at the `PlanForge` gate in
  `internal/turn/subissues.go` and at `RenderSubIssue` in
  `internal/report/subissue.go`, recording the traced-not-guessed finding that
  the legacy file-discovery phase is unreachable in production. No code
  change to sub-issue creation; `RenderSubIssue`/`fileStoryIssues` left
  intact, per the plan's explicit "not deleted" constraint.
- **S-5** — `.codex/skills/plan-orchestrator/SKILL.md` and
  `.codex/skills/implement-orchestrator/SKILL.md` hand-edited with the shared
  plain-language rule and each mode's three ordered rules, independently
  authored (S-4 was not read or referenced), adjusted only for each file's
  existing banner voice. No generator run; `.claude/**` and `teams/**`
  untouched.

## Added scope

- Two narrow test-assertion fixes in `internal/turn` (`report_test.go`'s
  `TestReportFailedRunStillPostsAndAdvances`, `task_publication_test.go`'s
  `TestTaskPublicationSurvivesAReplayedTrigger`) that were not named in any
  story's task table. Both are direct, mechanical consequences of S-1's own
  approved "Removed for good: ... Log" and publication-string removals — the
  plan's AC-009 claimed these files needed no changes, but two specific
  assertions did. See `notes.md` for detail. Not a scope change: no new
  behavior, only removing assertions on text the plan itself removed.
- A handful of unit-test-checkpoint additions closing real (non-vacuous)
  coverage gaps: `TestRetainedInputsNeverReachTheBody` and one
  `TestAnUnpushedRunLinksNothing` table case (S-1); `TestRunInputCarriesTheStoryTally`
  (S-2); `TestRenderCompleteStoryOmitsReplyAdvisory` (S-3). All additive,
  all inside the affected packages' existing test files.

## Dropped or deferred

- Nothing within S-1, S-2, S-3 or S-5's approved scope was dropped or reduced.
- S-4 was not attempted in this run (owner selection, not a blocker
  encountered mid-run).

## Architectural deviations

None. `internal/report` remains mode-blind (no `agent.Mode`/`store.*` import);
the mode gate and done-predicate for S-2 live entirely in `internal/turn` as
specified; no goroutines/channels introduced; every new store read takes
`ctx` first; `report.StoryTally`'s only content is the two integers.

## Verdict

CLEAN — delivered matches the approved plan for S-1, S-2, S-3 and S-5 within
acceptable micro-decisions (the two narrow regression fixes and the checkpoint
test additions listed above). S-4 remains fully out of scope for this run.
