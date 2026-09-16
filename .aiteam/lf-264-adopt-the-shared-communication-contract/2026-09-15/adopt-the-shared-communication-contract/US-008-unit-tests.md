# US-008 unit test evidence

Frozen story: US-008 / #305, issue ID 5463755356, parent ID 5439211198,
revision `53dc9a6dc1096297d1f5196dceac1376b5b901e5600820e81f4fa6ab3b323902`,
dependencies none, workflow 2. Execution state: base HEAD
`16634ed33075ea3acfc449bcb6316163bdc0b807` with this run's uncommitted
engineering and test changes. The orchestrator owns immutable capture of the
combined implementation state. This report is agent-reported local execution,
not acceptance or authentic-host evidence.

## Removed exclusive tests and preserved helpers

- Deleted `internal/planbrief/storyfile_test.go` (obsolete Markdown story parser).
- Deleted `internal/report/subissue_test.go` and its exclusive
  `internal/report/testdata/subissue.golden` fixture.
- Moved the shared `testStory` fixture to `complete_story_test.go`, retaining
  only Ref, ID, Title and Excerpt. Both existing complete-renderer tests remain.
- Deleted `internal/turn/subissues_test.go` discovery tests and fakes. Moved
  `lockIndex` (still used by plan_outcome_test.go), `req2`, `originFile` and
  `commitCount` into `internal/turn/harvest_recovery_test.go`. Preserved the
  general failed-predecessor save regression under the accurate name
  `TestCommitAndPushIncludesPredecessorLeftoversOnSucceedingRun`.
- Adapted `internal/github/subissues_test.go`'s
  `TestARenderedSubIssueIsAcceptedAsASpec` to RenderCompleteStory/Excerpt.
- Updated gate-test inventory to the two surviving sections, prepareClone and
  commitAndPush, preserving mutation/acquisition assertions and removing stale
  discovery comments.
- Added complete-renderer regressions for publication marker/title identity,
  invalid identity rejection with nonpostable output, and body/title redaction.

## Executed unit checks

Command:

```text
go test ./internal/report ./internal/planbrief -run '^(TestCompleteStoryPreservesDetailOrRefuses|TestRenderCompleteStory.*|TestParseStoryIndex.*|TestRewriteSubIssueCell.*)$' -short -race -cover -timeout 8m
```

Final output, exit 0:

```text
ok  github.com/cloudvane/litefactory/internal/report     2.354s  coverage: 4.3% of statements
ok  github.com/cloudvane/litefactory/internal/planbrief   (cached) coverage: 8.3% of statements
```

14 selected top-level functions passed, 0 failed: five complete-renderer tests
and nine retained index parsing/rewrite tests. Coverage is scoped package
coverage, not a whole-package/full-suite claim. The planbrief result was reused
from the first identical invocation in this worker's execution; that invocation
ran and passed planbrief in 1.524s with 8.3% coverage.

Initial new assertions incorrectly expected the retired renderer's ID-prefixed
title and one universal error sentinel. The run failed those two assertions;
the complete renderer preserves exact title and returns a parent-validation
error for missing parent identity. Test expectations were corrected to the live
contract (exact title, rejection, no postable output), with no production edits.
Both corrected tests passed in the final execution above.

## Evidence boundaries and handoff

AC-001 reachability inventory is owned by the engineer's US-008-engineering.md;
this worker read that report after the deletion handoff. AC-002 exclusive test
removal and retained fixtures are represented in this diff. AC-003 pure renderer
and index regressions pass. Real Git, disk-based gate assertions, managed
PublishPlan and controlled HTTP rendered-spec tests were handed to the I/O
specialist; this unit-only worker did not execute them. The I/O specialist also
owns the production no-discovery regression in a separate file.

No live cohort/drain proof, legacy-reader retirement, acceptance, external host
observation, full suite or make check is claimed. No TodoWrite tool was available;
planned work was tracked through this report and status messages.
