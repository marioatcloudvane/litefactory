# US-004 real-I/O engineering checks

Frozen identity: #301 / 5463752986; parent 5439211198; revision `74d45a210089e9cf742806b410b67be17201d2dbfbfceca40f7899b61a7dde2b`; dependencies US-001 and US-003. Workflow 2. No acceptance decision.

Implementation tested: HEAD `16634ed33075ea3acfc449bcb6316163bdc0b807` plus current uncommitted US-001–004 production changes and `internal/turn/activity_runtime_io_test.go`. Orchestrator owns immutable aggregate capture. Environment: `go version go1.27.0 darwin/arm64`; temporary migrated SQLite, Unix-domain broker and controlled loopback HTTP. No external forge/provider calls, live daemon state, new tags or dependencies.

## Added checks

- IT-US-004-1 / `TestActivityRuntimeIO_ProductionModes`: five actual prepareMCP constructions; retained owners; broker discovery/call agreement and catalog-v2 digest presence; activity_get observes successive updates using identical arguments; Plan/Spike refuse unrelated tools; activity changes neither run status nor application revisions; terminal cleanup refuses get, mutation and replay.
- IT-US-004-2 / `TestActivityRuntimeIO_ReplacementExpiryAndShutdown`: replacement clears/revokes predecessor; late predecessor exit does not clear successor; explicit factory expiry removes owner; SQLite reopen retains durable receipt; Close clears abandoned owner and rejects renewed registration.
- IT-US-004-3 / `TestActivityRuntimeIO_RunnerTermination`: success, failure, cancellation, interruption, missing and malformed output all clear activity and revoke obsolete authority; original failure/interruption causes survive; fresh authorized attempt reads historical durable activity receipts.
- IT-US-004-4 / `TestActivityRuntimeIO_BoundedOwnersAndCancelledCleanup`: 32 real owners admitted, 33rd refused; cancelled cleanup performs no owner changes; cancellation of one attempt clears only that owner; other task guards remain live; Close empties registry. No sleeps.
- IT-US-004-5 / `TestActivityRuntimeIO_CleanupPreservesUncertainPublication`: actual lost HTTP comment reply retains pending operation across runtime Close and database reopen; exact observation confirms without redispatch.

## Executed results

`go test ./internal/turn -run '^TestActivityRuntimeIO_' -count=1 -timeout=4m`

```
ok  github.com/cloudvane/litefactory/internal/turn 4.266s
```

`go test -race ./internal/turn ./internal/daemon -run 'TestActivityRuntimeIO_|TestMCPRuntimeConcurrentCancellationIsolation|TestPublicationDaemonIO_' -count=1 -timeout=4m -v`

Full exact output: `US-004-io-race.log` in this session.

```
ok  github.com/cloudvane/litefactory/internal/turn 36.640s
ok  github.com/cloudvane/litefactory/internal/daemon 7.609s
```

9 top-level tests passed, 0 failed in retained race run (five new runtime tests, existing concurrent cancellation isolation and three existing real daemon publication recovery/intake tests). No race reports.

`gofmt -l internal/turn/activity_runtime_io_test.go`: exit 0, no output.

Initial test-development failure: after runner exit the old ReceiptService correctly rejected read authority. The assertion was corrected to read durable history from a newly authorized preparation. This was a fixture error, not a production defect; retained final run passes.

## Bounds

Deterministic runtime cleanup is exercised directly with factory-supplied time. Existing daemon recovery tests verify bounded scheduling/restart publication behavior; no new end-to-end daemon-expiry-during-backoff test was added. Pure catalog/profile and display-lifecycle coverage belongs to the unit specialist. These are offline checks, not authentic Claude/Codex host observations; #274/#296 obligations remain external.
