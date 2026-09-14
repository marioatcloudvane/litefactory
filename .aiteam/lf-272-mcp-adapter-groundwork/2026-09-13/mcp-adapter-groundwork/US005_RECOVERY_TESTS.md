# US005 combined recovery evidence

Frozen US-005, GitHub #279, ID 5442843169, revision `9fc13c1e1822f65bc8c60b5b418fbfacfe8a7d6c42d36f32dd5d53aaf90f280a`; dependencies US-003 and US-004 engineering recorded. Owner correction keeps #264 wiring after #272.

Added `internal/turn/mcp_recovery_test.go`. Three top-level tests use actual MCP Server.Run over io.Pipe, the real submission client/private Unix socket/broker, actual AttemptGuard and CreationAuthorizer, and migrated temporary SQLite. A handler registered only inside the fixture simulates creation after acquiring the real factory-only permit. Production tool profiles and source files remain unchanged.

`TestMCPRecovery_FaultBoundaryMatrix` uses synchronized barriers at before reservation, after reservation, before dispatch, after simulated remote creation before acknowledgment, and partial-batch interruption. Faults exercise MCP cancellation notification, EOF, attempt close/replacement and fresh runtime identity. Before admission no durable operation exists. Later interruption preserves reserved or uncertain state, original operation ID and artifact units. Same-live reconnect performs a new handshake and returns that operation without another permit. Dead authority refuses exact replay, and dispatched uncertainty cannot release quota.

`TestMCPRecovery_ConcurrentRetriesLostReply` sends additional MCP calls while the first simulated effect awaits acknowledgment. All return the same durable operation; EOF loses the first reply, a fresh connection reauthenticates and resolves the retained operation, and counters prove one dispatch of the simulated two-artifact batch. No second operation exists.

`TestMCPRecovery_ReplacementReceiptCannotDispatch` allocates a fresh actual result launch after a partial outcome and exposes a test-only narrowly scoped receipt handler. The fresh guard can inspect its retained operation; foreign task selection and creation invocation refuse, the replaced descriptor cannot replay creation, and simulated effect counts remain unchanged. This establishes the groundwork's separate live read/recovery possibility, not a production creation-receipt family (owned by #273).

Existing US002 evidence covers shared helper/result receipt compatibility and retained Validate communication. US004 evidence covers exact expired-grant replay, physical SQLite reopen and detailed quota/rolling boundaries. These unchanged suites were not duplicated here. This is local deterministic fault evidence only: no real GitHub publication, authentic host, automatic production registration, live daemon restart or remote exactly-once claim.

Plain command:

`go test ./internal/turn -run TestMCPRecovery_ -count=1`

```
ok github.com/cloudvane/litefactory/internal/turn 1.679s
```

No production defects were observed and no fixes were needed. Final race result follows after execution completes. Aggregate checks remain the orchestrator's responsibility.

`go test ./internal/turn -run TestMCPRecovery_ -count=1 -race`

```
ok github.com/cloudvane/litefactory/internal/turn 16.485s
```

All three top-level tests and six fault-matrix subcases pass; zero failures and zero detected data races.
