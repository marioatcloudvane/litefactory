# US-002 offline HTTP and SQLite executed scenarios

Frozen story: github:marioatcloudvane/litefactory#233, US-002, revision `9c1be5ccfdc83bb4e2fa29b7ab59ccee742892744c704e8616c88c630aa8957e`, depends on US-001. Task T-002.3.

Executed passing command:

```
go test -race ./internal/daemon -run 'TestDiscoveryHTTP|TestDiscoveryBudgetPendingRefinementDeliveryBypassesReserve|TestRateLimitBacksOffAndResumes' -count=1 -v
```

Result: PASS, daemon package 13.712s. Full output: `us002-final-http-race.log`.

Eight top-level cases exercise actual HTTP client decoding/cache/observation and daemon admission with migrated SQLite, plus existing full worker fixture for active delivery and genuine backoff:

- Cold/warm/changed two-parent discovery: respectively 6x200; 6x304; 1x200 + 5x304, two comment pages per phase. These are OFFLINE fixture measurements, not live support or savings evidence.
- Complete 100-page history, short history deferral across repositories, queue priority, next-minute resume and complete warm 100-page fetch.
- Later-page 502 and genuine 403 leave discovery cursor and pending admission unchanged; reservations released.
- Cancelled real-client fetch returns cancellation and releases reservation.
- Same-parent refinement rotation retains the successful 100-page child's cursor, leaves deferred next child's cursor unchanged, and resumes it on an injected subsequent minute.
- Channel-synchronized concurrent actual scalar requests hold two independent pessimistic reservations, then settle the three actual charged requests including startup probe.
- Durable queued refinement executes and reports despite the 4000 reserve floor; discovery cursor and parent watermark remain unchanged.
- Actual GitHub 403 backoff preserves poll cursor/watermark, skips network during the remote hold, and resumes after a fresh quota observation. Probe cadence remains bounded independently of Retry-After.

Fixture corrections: common HTTP responses now provide valid core quota fields, including a configurable stable reset. The old backoff fixture returned Remaining=0 with a one-hour reset but expected discovery after 45 seconds. Its initial failure is retained in `us002-http-with-initial-backoff-failure.log`. The corrected case uses an explicit expiring quota window, renewed response metadata, and respects the one-minute probe cadence after the 45-second remote hold. Standalone passing correction output is in `us002-backoff-corrected-race.log`.

A separate earlier `go test ./internal/daemon -count=1` remained running when the specialist returned, session 84242, output `/tmp/us002-daemon-tests.log`. It does not establish passing aggregate evidence and predates the backoff fixture correction. Orchestrator owns completion and final make check.

## Cycle 2 aggregate-fixture corrections

The earlier outstanding full daemon process was explicitly cancelled via its exact exec session; it produced no passing evidence.

Quota fixture reset windows now roll forward only when the injected clock reaches their expiry; a non-advancing clock peek avoids adding fixture HTTP requests to daemon time. This supports existing long-expiry and provider lifecycle scenarios without expired metadata. The backlog-specific issue spy delegates probe issue 1 directly to the actual client, so probe observation is not misclassified as a task lookup or subject to candidate-only failure hooks.

Bootstrap helpers wait for actual durable watermarks through bounded subsequent ticks before new comments are introduced. Cross-repository concurrency and pool refill tests continue discovery through admitted ticks while preserving their execution barriers: both repositories overlap, three pool slots become concurrently active, and the fourth/fifth start after individual releases while the remaining original executions stay held.

Executed passing checks after these corrections:

```
go test -race ./internal/daemon -run 'TestClosedBacklog|TestUncertainBacklog|TestFailedClosure|TestTwoIssuesInTwoReposRunConcurrently|TestFourthStartsWhenFirstFinishesNotAfterAllThreeComplete' -count=1 -timeout=2m -v
```

PASS 19.112s; `us002-cycle2-fixtures-race.log`.

```
go test -race ./internal/daemon -run 'TestDiscoveryHTTP|TestDiscoveryBudgetPendingRefinementDeliveryBypassesReserve|TestRateLimitBacksOffAndResumes' -count=1 -timeout=2m -v
```

PASS 15.967s; `us002-cycle2-http-race.log`.

No production code or admission bypass added by this specialist. Aggregate make check remains owned by the orchestrator.
