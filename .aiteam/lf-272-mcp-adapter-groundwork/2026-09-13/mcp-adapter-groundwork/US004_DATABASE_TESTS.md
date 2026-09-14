# US004 executed SQLite and trusted intake tests

Frozen identity: US-004, GitHub #278, numeric ID 5442842977, revision `7fe2d1453415e2988eb510de1e1b48d47e6130544a6ab30da74b744d5eb07b2c`, dependencies US-001 and US-002 engineering recorded. Owner correction applies: #272 groundwork precedes #264 production wiring.

New files:
- `internal/store/mcp_creation_test.go`
- `internal/turn/mcp_creation_db_test.go`
- `internal/daemon/mcp_creation_test.go`

Nine top-level tests use real migrated temporary SQLite, actual factory attempt bindings/guards, the real creation store/authorizer, and trusted daemon intake. No external server, forge call, provider subprocess, integration tag, new module, or production changes were needed.

Coverage includes retained proposal existence and timestamp; numeric owner and bot provenance; copied, quoted, claimed-identity, wrong-source/repository/mode/relationship commands; missing/changed proposals; immutable comment replay and changed-then-restored proposal revisions; missing/foreign/mismatched/expired/replaced/restarted/cancelled authority; no transfer to resume/fallback; grant consumption only after approval; exact original 30-minute deadline and earlier attempt clamp; gate off/gate only/all-five-mode guarded facade dispatch; concurrent 16-way reservation yielding one durable operation and exactly one private permit; duplicate dispatch refusal; delayed dispatch expiry/revocation/context cancellation; 5-unit batches and 20-unit isolated repository quotas; one-nanosecond-before and exact rolling-window boundaries; indefinitely charged pending/uncertain reservations; never-dispatched release without reopening the grant; physical database close/reopen preserving uncertain outcomes without restoring prior authority; factory reconciliation retaining operation identity.

The injected clock controls creation bookkeeping. Store-only fixtures align persisted lease bound timestamps with that deterministic clock for future rolling-window scenarios. Actual production lease identity, digest, runtime, expiry, result access and SQL transaction checks remain active. Daemon fixtures supply synthetic authenticated GitHub metadata to the actual intake seam; this is not proof of live GitHub ingestion. Native host isolation, production lifecycle/control consumption, actual publication and remote exactly-once effects remain outside this story.

Initial test compilation exposed test-authoring API mistakes (outcome methods return two values, service construction is `BindAgentResultService`); corrected without production edits. No production defect was observed.

Executed plain command:

`go test ./internal/store ./internal/turn ./internal/daemon -run 'TestMCPCreationDB_|TestMCPOwnerIntake_|TestCreationAuthorizerDB_' -count=1`

```
ok github.com/cloudvane/litefactory/internal/store 2.631s
ok github.com/cloudvane/litefactory/internal/turn 1.544s
ok github.com/cloudvane/litefactory/internal/daemon 1.324s
```

Final race result appended below after command completion. Aggregate checks are owned by the orchestrator; no acceptance or review was performed.

`go test ./internal/store ./internal/turn ./internal/daemon -run 'TestMCPCreationDB_|TestMCPOwnerIntake_|TestCreationAuthorizerDB_' -count=1 -race`

```
ok github.com/cloudvane/litefactory/internal/store 52.182s
ok github.com/cloudvane/litefactory/internal/turn 10.912s
ok github.com/cloudvane/litefactory/internal/daemon 4.116s
```

Final outcome: all nine top-level tests and their subcases pass; zero test failures and zero detected data races.
