# US005 implementation handoff

Frozen story US-005, GitHub #279, ID 5442843169, revision `9fc13c1e1822f65bc8c60b5b418fbfacfe8a7d6c42d36f32dd5d53aaf90f280a`; dependencies US-003 and US-004 engineering recorded. #264 production wiring follows this groundwork under the owner's explicit correction.

Changed `docs/mcp-adapter.md`: added the interruption-state matrix, adapter/client/broker worker ownership, same-live reconnect and durable correlation behavior, factory-owned outcome recording after cancellation, guarded receipt versus creation replay rules, Plan unsafe-route refusal through recovery, and exact later consumer obligations. Existing sections already document additive tool policy, true-default automatic registration with no actual host consumer, false-default creation gate, secret-free exact owner approval, original 30-minute TTL and fixed one-batch/five-artifact/twenty-per-repository-day limits.

Existing production code already provides the required local primitives: adapter owns interruptible streams and joins workers; client context closure reaches the bounded broker connection monitor; the attempt guard cancels contexts and stamps transactional admission; shared result journals retain exact request identity; creation reservation/dispatch/outcome records retain ownership and quota independently of transport failure. No additional production code or recovery engine was needed for this assignment. No tests were authored in the implementation role.

Integration specialist still owns combined pipe/private-socket/SQLite fake-creation fault evidence at before reservation, after reservation, before dispatch, after simulated remote create before acknowledgment, and partial batch interruption. Fixtures should use only a deterministic internal handler, preserve the operation ID on reconnect/lost replies, prove one dispatch owner under concurrent retries and restart, and assert cancellation before admission adds no reservation. Existing helper/Validate/Plan refusal tests remain compatibility evidence; actual remote publication and authentic host behavior are unavailable and outside #272.

No acceptance/review, forge action, dist write, daemon restart or commit was performed. Aggregate make check remains the orchestrator's obligation.

Executed existing focused compatibility checks:

`go test ./internal/mcpadapter ./internal/cli ./internal/turn ./internal/store -run 'TestMCPServer_EOFAndBlockedOutputJoin|TestMCPServer_OutputBacklogAndContextJoin|TestMCPCLI_CancellationReachesSharedService|TestMCPTools_PlanUnsafeRouteAndRecovery|TestMCPCreationDB_ConcurrentSingleDispatchAndRecovery|TestMCPCreationDB_ReopenRetainsOperationWithoutAuthority' -count=1`

```
ok github.com/cloudvane/litefactory/internal/mcpadapter 0.605s
ok github.com/cloudvane/litefactory/internal/cli 0.566s
ok github.com/cloudvane/litefactory/internal/turn 1.210s
ok github.com/cloudvane/litefactory/internal/store 1.023s
```
