# US-007 implementation report

Frozen issue #287, issue ID 5448834188, revision f5c585b1b0c766b8b531c8b0422d09af398a84ffa5fdb8ed0faf656a32c1dcef; dependencies US-001 and US-002 supplied engineering-complete. Implementation task complete; criterion-derived tests and engineering capture remain orchestrator/test-specialist work. No acceptance assessment.

Created `internal/turn/activity.go` only. No shared source or previously captured file changed.

`NewActivityService(ActivityOptions{Receipts, Now})` requires the existing bound ReceiptService and initial factory time. Register `activity_update` with `FamilyRegistration{Name: "activity_update", Owner: service, Handle: service.HandleUpdate}` and likewise `activity_get` with HandleGet. The existing family dispatcher supplies live AttemptGuard/profile enforcement, durable request identity and first-invocation election; a duplicate observes its durable receipt without calling the activity handler again. PermissionMaterial contributes the immutable binding and advisory contract version.

Update strictly decodes the existing `agentresult.AdvisoryProgress` shape `{sequence,message}`, invokes ReduceProgress under a mutex and returns `{disposition,code,activity}`. It records rejected or confirmed ledger disposition before responding. Invalid fields also record rejection. No authored text is stored in the receipt. Existing reducer sequence/replay/conflict/stale, UTF-8, 2 KiB, 1,024 update and 32 identity bounds remain unchanged. Get accepts only an empty object and returns bounded copied latest display plus counters. Initial/process-restarted display is `unavailable` even if a durable update receipt exists.

`Lifecycle(ActivityLifecycle{Now, Terminal, Clear})` is a factory-only input and returns a copied ActivityView. Factory time advances monotonically; terminal marking preserves the earliest supplied timestamp using MarkProgressTerminal, and cleanup delegates to CleanTerminalProgress. Explicit or expired cleanup returns `cleared`. Terminal/clear cannot reactivate this service, preventing cleanup from resetting an active attempt's update budget. No tool argument supplies lifecycle/time, and no method changes workflow/evidence, grants, scopes, leases or mode. No timers, goroutines, wall-clock reads or production scheduling were introduced. Production scheduling remains #274.

Actual checks:

```
go test ./internal/agentresult ./internal/turn -run 'TestProgress_' -count=1
ok github.com/cloudvane/litefactory/internal/agentresult 0.353s
ok github.com/cloudvane/litefactory/internal/turn 0.490s [no tests to run]
```

This executes the existing reducer tests and compiles turn; it does not establish the new facade's criteria. `go vet ./internal/turn` passed, exit 0, no diagnostics. Owned source was gofmt formatted. Initial compile/vet attempts failed on a concurrent comment-store constant collision; its owner corrected the collision before these successful reruns.

Test handoff: compose the actual family dispatcher and ReceiptService; exercise sequence responses and persisted rejected/confirmed disposition, concurrent updates, malformed fields, scoped current-writer denial, terminal cleanup/expiry with injected time, copied pointer isolation, unchanged workflow/lease/grant deadlines and a fresh ActivityService over retained receipts. A retry's response is FamilyReceipt, not replayed display; a new activity_get request sees unavailable after service replacement. No tests authored by this implementation specialist; no forge, full-suite, submission, commit or daemon effects.
