# US-001 implementation handoff

Frozen identity: github:marioatcloudvane/litefactory#275, issue ID 5442842332, revision a79dc7fcafd48ad0e0f7fd4cf2180f63c24fec8b5670e94b538446cf3d76c27c; dependencies none.

Production implementation complete; engineering completion awaits required specialist evidence and aggregate checks. Owner correction removes #264 as prerequisite. This is a reusable foundation over delivered #263 ResultBinding, without production lifecycle wiring.

Files:
- internal/store/migrations/0042_mcp_authority.sql (new)
- internal/store/mcp_authority.go (new)
- internal/store/result_bindings.go (modified)
- internal/turn/mcp_attempt.go (new)

AttemptAuthority creates a fresh runtime instance and extends an existing live ResultBinding once per launch. MCPLease keys the original run/attempt; no alternate launch identity or universal timeout is introduced. Deadlines come from the factory. Scope comprises the full comparable result binding, including frozen-context hash, plus a factory-computed SHA256 permission digest. US002 must compute this digest over its complete immutable permitted tools and target selectors.

Every US002 List/Call must obtain guard.Context(ctx), defer its release, and pass that context through all shared result service operations. Store rechecks the lease inside existing result transactions before accessing historical/replay or mutation state. The unstamped internal service retains historical-reader semantics. guard.Check also handles discovery authorization. Guard.Revoke cancels outstanding contexts immediately and durably revokes admissions; already-recorded work remains retained. Replacement binding cancels the prior guard; runtime restart invalidates all prior instance identities without deleting history. Broker owners must join their own cancelled workers.

Executed compatibility check: go test ./internal/store ./internal/turn -run 'TestResultBinding_|TestAgentResultService_' -count=1. Passed: store 1.355s, turn 2.563s. This establishes existing result compatibility only, not the required new MCP authorization matrix. Integration specialist owns real SQLite/socket, expiry and synchronized race evidence; unit specialist evidence and repository aggregate gate remain outstanding.
