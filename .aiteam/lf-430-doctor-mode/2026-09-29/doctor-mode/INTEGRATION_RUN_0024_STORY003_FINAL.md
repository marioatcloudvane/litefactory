# Story 003 final local boundary execution

Frozen story: #434 / story-003, revision `36627c9d83e9ce38219bd9db4574f85c58e431bdd6d0260b2835b49bfd81f8c2`, dependencies story-001/story-002. Engineering tests only; no acceptance or source review was performed.

New ordinary Go tests (repository policy overrides the generic skill's integration-tag convention):

- `internal/doctor/repair_boundary_io_test.go`: `TestDoctorStopJoinsAdmittedFileEffectAcrossStoreHandles` uses a real advisory repair-effect lock, two independently opened SQLite handles and separate services. A bounded stop cannot acknowledge while the admitted effect lock is held. The real file effect completes under that lock; stop then settles, retains the partial hold and original backup, refuses late writes, and leaves the normal session unchanged. This tests local service/descriptor concurrency; it does not claim a killed process or authentic host drain.
- Same file: `TestDoctorAbsentFileCrashReopenPreservesUncertainHoldAndBackup`, with no-write and third-party-create cases. It durably records backup/write intent, closes the original database, reopens it and reconciles through the real service. Recovery preserves absence or the unrelated creator's exact bytes, retains the absent-original backup and uncertain hold, refuses further writes and creates no restart intent. Interrupted execution is deliberately injected at the durable intent boundary; no actual process was killed.
- `internal/store/doctor_repair_publication_io_test.go`: `TestDoctorRepairOutcomeReopenCompletesWithoutWorkflowCursorEffects` completes a controlled row repair and exact recovery acknowledgment, closes/reopens the database, retains idempotent multipart outcome delivery, confirms/completes it, and checks unchanged target/sibling sessions and watermarks. It also rejects another restart claim and removes completed outcome recovery debt. The source is an explicit factory-authenticated fixture; exact GitHub admission is exercised by existing tests. This test does not publish externally.

Targeted commands already passed:

```
go test -race ./internal/doctor -run 'TestDoctorStopJoins|TestDoctorAbsentFileCrash' -count=1
ok github.com/cloudvane/litefactory/internal/doctor 3.774s

go test -race ./internal/store -run TestDoctorRepairOutcomeReopen -count=1
ok github.com/cloudvane/litefactory/internal/store 2.489s
```

No new production defect was observed in these cases. Only new test files and this report were written. Real dependencies are isolated temporary SQLite/filesystem/advisory locks; there were no forge, provider, live launchctl, runtime-state, or daemon lifecycle actions. Authentic provider isolation, live process drain and real restart observations remain unverified and are not replaced by these fixtures. Aggregate checks and immutable result capture belong to the orchestrator.

Final combined scoped execution:

```
go test -race ./internal/doctor ./internal/store ./internal/daemon -run Doctor -count=1
ok github.com/cloudvane/litefactory/internal/doctor 14.077s
ok github.com/cloudvane/litefactory/internal/store 13.369s
ok github.com/cloudvane/litefactory/internal/daemon 5.824s
```

Pinned scoped lint:

```
GOLANGCI_LINT_CACHE=/private/tmp/litefactory-doctor-lint /Users/mariomh/.local/bin/golangci-lint run ./internal/doctor ./internal/store
0 issues.
```

All three new test functions passed (four distinct cases including the two crash-boundary subtests); zero failures. Existing scoped Doctor tests also passed. Configured authentic external observations remain missing as stated above.
