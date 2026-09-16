# US-008 engineering — Remove unreachable story discovery

Frozen identity: #305, issue ID 5463755356, parent ID 5439211198, revision
`53dc9a6dc1096297d1f5196dceac1376b5b901e5600820e81f4fa6ab3b323902`, dependencies none,
workflow 2. Inspected HEAD: `16634ed33075ea3acfc449bcb6316163bdc0b807` plus the
current run's uncommitted US-001–006 engineering changes. Source inspection and
local builds establish repository behavior only, not authentic host capability.

## Production ownership proof before deletion

- `internal/daemon/setup.go` supplies `client` to `Options.SubIssueCreator` and
  `renderer` to `Options.SubIssueRenderer`. Its concrete GitHub client implements
  `PlanForge` (the methods used by `plan_publication.go`).
- `runAndHarvest` in `internal/turn/harvest.go` was the sole production caller of
  `fileStoryIssues`. That function first returned for an explicit Plan proposal;
  its next gate returned whenever `SubIssueCreator` implemented `PlanForge`.
  Every production construction therefore returned before Markdown discovery.
- Below that gate the exclusively owned chain was `fileStoryIssues` ->
  `buildSubIssueSpecs` -> `buildSubIssueSpec` -> `planbrief.ParseStoryFile` and
  `report.Reporter.RenderSubIssue`, plus `rewriteStoryIndex` -> `subIssueURL`
  and the second-save `commitStoryIndex` with `subIssueCommitSummary`.
- Search across internal Go sources found no other production callers of these
  helpers. `ParseStoryFile` and its struct/constants/error/heading helper were
  exclusive to this discovery chain. The legacy renderer and title/body/detail
  trimming helpers were exclusive too. This justified deleting `storyfile.go`
  in full, but **not** deleting `turn/subissues.go` or `report/subissue.go` in full.

## Removed and preserved

Removed the discovery entrypoint and harvest call, exclusive publication/index
rewrite/second-commit helpers, obsolete renderer interface, legacy renderer and
its exclusive body/title/detail helpers and Story fields, per-story parser and
its exclusive error. Updated affected package/owner comments.

Preserved `locateSessionDir` for managed `plan_handoff.go` and `planbriefFileMode`
for rooted `plan_index.go`. Preserved the shared `SubIssueCreator` option and
interface because managed scope, approval and refinement consumers still use
that dependency. `SubIssueRenderer` now requires existing `CompleteStoryRenderer`
instead of the removed legacy renderer interface. Production setup remains valid
with the same dependencies. Preserved report `Story`, `SubIssue`, `storyIDPattern`,
`StoryMarker`, complete rendering and shared link/sanitization helpers.

`PublishPlan`, complete proposal publication, typed/internal Plan handoffs,
link-only index writing and common session lookup remain in place. Existing
GitHub publication APIs remain available. No legacy Plan input, Implement event
reader, Validate parser, historical read/migration or daemon state was removed.

## Files changed by this worker

- internal/turn/subissues.go
- internal/turn/execute.go
- internal/turn/harvest.go
- internal/turn/doc.go
- internal/report/subissue.go
- internal/report/marker.go
- internal/planbrief/storyfile.go (deleted)
- internal/planbrief/errors.go
- internal/planbrief/doc.go
- this report

## Executed checks

`go build ./internal/turn ./internal/report ./internal/planbrief ./internal/daemon`
completed with exit code 0 and empty output after these production edits.
`gofmt -w` applied to the eight retained edited Go files. No full suite, make
check, vet or targeted test is claimed by this worker: test compilation requires
the separately owned cleanup below. The orchestrator/test specialist must retain
executed tests and immutable capture before engineering completion.

## Exact test-owner handoff

1. Remove exclusive `internal/planbrief/storyfile_test.go`.
2. Remove legacy `internal/report/subissue_test.go`, first moving `testStory` into
   `complete_story_test.go` and retaining only Ref, ID, Title and full Excerpt.
   That fixture serves both existing complete-story tests. Update the stale
   complete-renderer test comment referencing removed legacy tests.
3. Adapt `internal/github/subissues_test.go`'s
   `TestARenderedSubIssueIsAcceptedAsASpec` to `RenderCompleteStory` and `Excerpt`.
   Preserve its live redaction/marker/publication seam assertions.
4. Remove discovery-only `TestFileStoryIssues*` cases and fakes in
   `internal/turn/subissues_test.go`, but preserve/move `lockIndex` (called by
   `plan_outcome_test.go`). Preserve the general save-recovery regression
   `TestFileStoryIssuesCommitsPredecessorLeftoversOnSucceedingRun`, rename it
   for its actual save behavior, and retain its req2/originFile/commitCount helpers.
5. Update `internal/turn/gate_test.go`'s explicit inventory to remove
   `commitStoryIndex`; live mutation sections are prepareClone and commitAndPush.
   Remove stale discovery/third-gate comments, retaining all live gate assertions.
6. Add a focused constructor/no-Markdown-discovery assertion; execute focused
   managed PublishPlan, complete-renderer, link-only-index, gate and save-recovery
   checks after cleanup. No tests were edited by the implementation worker.

## Milestone disposition

The dead discovery path is removed based on its production gate and exclusive
caller evidence. Story completion still needs the test-owner cleanup and retained
passing regressions. This is independent of US-009's still-gated Plan v1 file,
Implement event-file and Validate v1 reader retirement. No live cohort was
inspected or declared drained here. No acceptance or authentic-host claim is made.
