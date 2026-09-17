# US-004 engineering implementation

Frozen identity: issue #301 / 5463752986, parent 5439211198, revision 74d45a210089e9cf742806b410b67be17201d2dbfbfceca40f7899b61a7dde2b; dependencies US-001 and US-003 engineering recorded by orchestrator. Workflow 2. Base HEAD 16634ed33075ea3acfc449bcb6316163bdc0b807 plus current uncommitted engineering changes. No acceptance asserted.

## Production implementation complete; specialist tests pending

- `internal/submission/family_tools.go`: family catalog v2; only activity_get/activity_update become available to Plan and Spike. Research/Implement/Validate retain existing profile authority.
- `internal/turn/mcp_runtime.go`: retain ActivityService with actual prepared attempt, install both handlers before digest freeze, register guarded activity owner, terminal cleanup on all normal/error preparation and runner exit paths.
- `internal/turn/activity_runtime.go` (new): synchronized registry of at most 32 owners (production at most 16 workers); replacement clears old activity and revokes capability; daemon-supplied time and existing guard detect expired/cancelled/replaced attempts. Explicit immediate terminal display cleanup implements zero retention, an allowed advisory-contract cleanup policy. No timers/goroutines, no acceptance/result completion writes, no durable receipt/effect deletion. Runtime shutdown clears owners independently of runner return and prevents renewed registration.
- `internal/turn/mcp_families.go`: activity_get reads current state on repeated identical requests; it does not recover an old mutation receipt or allocate recursive receipts.
- `internal/daemon/tick.go`: bounded local cleanup runs before remote rate-limit backoff; existing publication recovery stays intact.
- `internal/daemon/setup.go`: shutdown/error cleanup uses runtime Close, which retires advisory owners before authority shutdown.

## Executed checks

Final code state:

`go test ./internal/turn ./internal/daemon -run 'Test(ActivitySocket|ActivitySQLite|MCPRuntimeActualLaunchPaths|MCPRuntimeConcurrentCancellationIsolation|MCPRuntimeReplacementRejectsStaleRegistration|PublicationDaemonIO_)' -count=1 -timeout=4m`

```
ok github.com/cloudvane/litefactory/internal/turn 3.127s
ok github.com/cloudvane/litefactory/internal/daemon 1.376s
```

`go vet ./internal/turn ./internal/submission ./internal/daemon`: exit 0, no output.

`gofmt -l internal/turn/activity_runtime.go internal/turn/mcp_runtime.go internal/turn/mcp_families.go internal/submission/family_tools.go internal/daemon/setup.go internal/daemon/tick.go`: exit 0, no output.

`go test ./internal/submission -run 'TestFamily(Profile|Catalog)' -count=1 -timeout=2m`: exit 1. Existing TestFamilyProfile_ModeMatrix and TestFamilyCatalog_IncrementalInstallation expect both activity tools denied for Plan and Spike. Their stale expected values must change in specialist tests; no other reported failures. No test files edited here.

## Deterministic test handoff

1. Update the two profile/catalog matrices above for Plan/Spike activity-only additions. Assert unrelated family grants remain denied, discovery/call agreement and catalog version change.
2. Production prepareMCP matrix all five supported modes: ActivityService retained; both tools discovered and callable; repeated activity_get observes new updates; catalog digest binds the version and registrations.
3. Controlled runner table: success/failure/cancellation/interruption/missing output all clear activity and revoke old get/update/replay. Preserve original runner outcome and recorded checkpoints.
4. Replacement: two attempts for same task; newer tracking clears/revokes predecessor without affecting newer activity. Other tasks remain isolated.
5. Explicit fake-time CleanActivities at lease expiry, context cancellation and superseded guard: clear display/revoke capability/remove registry owner; live unrelated attempts stay active. No sleeps.
6. Runtime Close clears abandoned owners and refuses subsequent trackActivity. Capacity bound 32 and cleanup error propagation stay deterministic.
7. Retain actual SQLite family receipts and pending comment/creation operations before terminal cleanup; assert they survive and recovery still works. Existing publication IO regressions passed.

These are offline repository checks; no authentic host capture or provider invocation occurred. Full aggregate gate and immutable engineering capture remain orchestrator-owned.
