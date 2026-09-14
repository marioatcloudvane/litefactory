# Aggregate fix cycle 2

The previous aggregate run passed lint and other packages but failed `TestTurnImposesNoAmbientDeadline`: `AttemptAuthority.Bind` called `context.WithDeadline`, violating the existing turn boundary (AC-PAR-3.6). That invocation also exhausted the repository's 20-minute turn-package deadline.

Changed only `internal/turn/mcp_attempt.go`: derive the guard context with `context.WithCancel(ctx)` instead of imposing the supplied lease expiry as an ambient context deadline. A comment states ownership explicitly. Caller-supplied immutable lease expiry remains in the persisted lease and is checked using fresh store time on discovery, reads/replay and transactional mutation admission. Parent cancellation and explicit revocation/replacement/close still cancel active contexts. Existing transport/client/broker owners retain their own bounded request lifetimes. No timer or forbidden deadline was relocated or hidden, and no test gate was weakened.

Timeout observation: the trace lists original non-MCP tests active for 0–14 seconds when the package alarm fired and many goroutines waiting in `testing.waitParallel`; the searched trace contains no MCP-specific goroutine frame. This is insufficient to determine whether throughput, contention or another cause exhausted the package budget. No test scheduling or timeout modification was made by this specialist, and no full suite was rerun.

Focused verification result follows after completion. The orchestrator owns the final required aggregate check and preservation of the earlier failure/timeout evidence.

`go test ./internal/turn ./internal/store -run 'TestTurnImposesNoAmbientDeadline|TestMCPAttempt_|TestMCPLease_|TestMCPRecovery_' -count=1 -race -timeout=8m`

```
ok github.com/cloudvane/litefactory/internal/turn 35.288s
ok github.com/cloudvane/litefactory/internal/store 16.971s
```

The existing structural gate, live-attempt revocation/admission, exact lease expiry and combined recovery tests all pass, with no detected data races.

## Integration specialist verification

Confirmed both actual test names, then ran only synchronized revocation/admission and exact lease expiry/selector regressions:

`go test ./internal/turn ./internal/store -run '^(TestMCPAttempt_RevokeBeforeAdmissionAndAfterRecordedWork|TestMCPLease_ExactExpiryAndImmutableSelectors)$' -count=1 -race -timeout=8m`

```
ok github.com/cloudvane/litefactory/internal/turn 4.541s
ok github.com/cloudvane/litefactory/internal/store 3.599s
```

Real migrated SQLite assertions retain admission refusal after revocation, cancellation of outstanding context with admitted history preserved, and expiry acceptance one nanosecond before versus refusal at/after the exact deadline. Both tests and subcases pass without races. No source changes were made during this verification assignment; final aggregate remains with the orchestrator.
