# Test report — run 26

The owner requested temporary deactivation of the SQLite-loss recovery scenario and completion of the remaining tests. Only `TestStoppedResultSQLiteOutageRecoversWithoutRelaunch` was changed: an explicit `t.Skip` records the owner deferral under #430. The original body is preserved. Production repair-hold guards are unchanged. This scenario is deferred, not fixed or passed; post-outage recovery covered only by that test remains unverified.

## Executed checks

- `go build ./...`: passed.
- `make -f Makefile -f /tmp/lf430-run26-gates/gate.mk check`: passed. The temporary, external Makefile override replaces only the test recipe with `go test -timeout=8m ./...`; formatting, vet and pinned golangci-lint v2.13.1 use the normal repository targets. No repository build configuration changed.
- Complete fresh race matrix: **48 batches passed, 0 failed, 0 unexecuted**. This supplies the race coverage separately from the bounded make-check invocation. All commands used `-race -timeout=8m -count=1`. Seven groups cover all 39 nonheavy packages; nine daemon, eight store and 24 turn shards cover their complete freshly enumerated test/example/fuzz-seed inventories. One original 40-test turn shard exhausted the cumulative eight-minute package budget while its active test had run 11 seconds. Its timeout output is retained under `superseded_execution_failures`; the exact 40 tests subsequently passed in four 10-test shards at the unchanged timeout. Names are matched by exact anchored expressions. See `AGGREGATE_RUN_0026.json` for every command, output and duration.
- Focused stopped-result/report-recovery race check: **12 top-level tests passed, 0 failed, 1 owner-deferred test skipped**; daemon package coverage 27.2%. Command: `go test ./internal/daemon -run 'TestStoppedResult|TestReportRecovery' -race -cover -count=1 -timeout=8m -v`.
- Fresh scoped doctor/repair unit checks: **10 functions and 106 subtests passed**, zero failures. Doctor package coverage 5.9%; store package coverage 0.2%. These are scoped coverage percentages, not whole-feature coverage. Commands and full output are in `SCOPED_CHECKS_RUN_0026.json`.

## Evidence and limits

All code checks describe final implementation digest `d681a93b396931c35b5d8dd3378a959c612e77daf2fe64d68935bc1ccd3fae19`. `ENGINEERING_MANIFEST_RUN_0026.json` inventories 154 implementation/test/documentation files. Historical run24 failures remain in their original artifacts; this run supersedes the unfinished aggregate status with fresh execution.

Authentic #406 capture, native-provider isolation, target process drain and restart/launchd observations remain unverified. Controlled SQLite, HTTP, process and provider fixtures do not establish those host observations. Native providers remain disabled behind their isolation gates; no live repair or daemon restart was performed. The SQLite-loss test deferral does not waive other criteria or imply acceptance.

All three engineering checkpoints are recorded. Acceptance remains for an explicitly requested Validate run; no acceptance/code/architecture review or issue closure was performed. Factory save, publication and pending remote effects are separate from executed checks.

The aggregate transport also captures the frozen session artifacts alongside those implementation files. Its broader transport digest is separate from the implementation-only manifest above.
