# Implementation notes — run 230

- Scope: factory-frozen generation 1, US-1 through US-5 (#161–#165). Frozen inline bodies override older local-file skill directions.
- Verify and publish each story immediately, then retain aggregate review/build/check/coverage gates.
- Writers: US-1 config/options/docs; US-2 neutral agent metadata/adapters/fixtures. Dependent work starts after both contracts are verified.
- Test infrastructure: real temporary SQLite, fake CLIs and httptest; no integration tags or external provisioning.
- Progress: US-1 and US-2 implementation active; US-3/4/5 pending dependencies.

## Incremental verification
- US-1 verified in event 000001 after constructor retention fix, independent clean review and scoped race tests.
- US-2 verified in event 000002 after stale Claude rejection fix, independent clean review and scoped race tests.
- US-3 now implementing against both verified contracts. Shared snapshot writers must request invalidation before edits.
- Baseline-plus-US-1/US-2 `go build ./...` passed.
- US-3 verified in event 000003 after adding watermark-independent daemon announcement replay (CR-B-301), independent clean review, scoped race suite and final added regressions. US-4 active; US-5 pending.
- Before US-4 shared edits, events 000004/000005 invalidate US-1/US-3 snapshots. US-4 authorized overlap: turn/execute.go, turn/harvest.go, store/runs.go, daemon/tick.go. Reverification attempt 2 required for US-1/US-3; US-2 remains valid.
- US1 reverified attempt2 event000006; US3 reverified attempt2 event000007 after explicitly enabling its announcement fixture. US4 verified event000008 after durable cancellation notice fix and direct automatic frozen-scope regression.
- US4 broader turn race timeout5m was below observed baseline; rerun15m passed. Daemon obsolete disabled fixture corrected and all affected scoped regressions pass.
- US5 active for restart/chain settlement; AC5.6 includes final aggregate gates before its evidence publication.
- US5 lifecycle edits invalidated US3 attempt2 / US4 attempt1 in events000009/000010 before touching store completion/dispatch. US1 attempt2 and US2 attempt1 remain valid.
- US5 independent production review CLEAN; real SQLite restart/settlement matrix and both actual fake-CLI provider recovery paths pass. Final exhaustion case pending.
- Coverage contract: Makefile/APP_CONSTRAINTS/CI have no numerical threshold or separate coverage target; unit-test skill requires race+cover visibility, supplied by scoped tests. Final make check still required.
- App context/constraints updated; aggregate independent review active.
- Aggregate review CLEAN and final build passed. First make check stopped at lint: nine missing exported store API comments, one nested else-if style and one wasted initial test assignment. Assigned narrowly scoped fixes; no behavior changes or US1/US2 snapshot edits.

## Final outcome
- All five stories verified with matching immutable final snapshots. Latest events: US1=000006 attempt2; US2=000002 attempt1; US3=000011 attempt3; US4=000012 attempt2; US5=000013 attempt1. Factory owns publication and closure.
- Final build, complete make check (29 race-test packages; zero lint issues), aggregate independent review and required scoped coverage checks passed. Details: VALIDATION.md.
- DRIFT_REPORT.md: CLEAN; no selected scope dropped or added. App context and constraints updated.
- No commits/pushes, live daemon changes, runtime edits, forge requests or external provider quota experiments.
