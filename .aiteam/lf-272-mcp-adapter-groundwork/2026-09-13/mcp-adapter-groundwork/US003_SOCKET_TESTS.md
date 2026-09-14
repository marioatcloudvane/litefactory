# US-003 CLI/pipe/socket/SQLite evidence — run 0007

Story #277 / 5442842741; revision 278cf863a7800a6cfee5bb38f4a88a674d1cb5daba61917e516d6493878da24b; dependency US-002 recorded.

New files: internal/cli/cmd_mcp_test.go; internal/turn/mcp_launch_test.go. Five top-level test functions with fifteen table subtests. No production edits in this assignment.

The registered mcp serve command runs in-process with real os.Pipe stdin/stdout, real private Unix broker sockets and real temporary migrated SQLite. Its backend is the actual ToolDispatcher/AttemptGuard/AgentResultService. Config is decoded with mcp_auto_start:false: automatic launch preparation yields nil while explicit serving successfully handshakes, lists all three tools and submits shared JSON. Exact application bytes survive all boundaries, changing MCP IDs does not allocate a new application request, and receipts retain separate pending engineering/acceptance/publication states. A new connection with the same live descriptor requires another handshake and recovers the same durable receipt.

Cancelled, closed and restarted authority refuses explicit serve before producing a handshake or any stdout. An actual shared-service resolver held at a deterministic cancellation barrier verifies notification and EOF propagation from stdio through the client socket/broker/guard into its context. The cancellation leaves no revision, joins the resolver, and does not claim success. CLI workers and real pipes are closed/joined by owned test cleanup.

The bound neutral producer is exercised for cold, resume, failed-resume fallback and cold fallback existing factory attempts. All descriptor paths/references are fresh; replaced guards refuse reconstruction while old brokers remain alive. Enabled/disabled/no-attempt, missing broker, mismatched broker attempt, relative executable and closed broker cases are covered. Copied argument slices and optional agent.Request values preserve existing native and unrelated-server permissions. Byte/metadata snapshots of the real private directory plus an unrelated configuration fixture establish that construction creates/modifies no files. These are neutral producer fixtures, not authentic host startup or inventory proof.

Executed final plain command:
```
go test ./internal/cli ./internal/turn -run 'TestMCPCLI_|TestMCPLaunch_' -count=1
ok github.com/cloudvane/litefactory/internal/cli 1.490s
ok github.com/cloudvane/litefactory/internal/turn 0.608s
```

Race command: `go test ./internal/cli ./internal/turn -run 'TestMCPCLI_|TestMCPLaunch_' -count=1 -race`.

Prior unit command for immutable engineering capture:
`go test ./internal/mcpadapter ./internal/config ./internal/agent -run 'TestMCPServer_|TestMCPAutoStart_|TestMCPLaunchSpec_' -short -race -cover`.

No external environment/service, subprocess provider, host configuration, daemon restart, dist writes, integration tag, new module, forge effect or acceptance review. Native #274 host consumption remains unavailable and unverified. Exact expiry boundary evidence remains in US-001, while blocked-output ownership and broader malformed/protocol concurrency matrices are in US003 unit evidence.

Race output:
```
ok github.com/cloudvane/litefactory/internal/cli 7.612s
ok github.com/cloudvane/litefactory/internal/turn 4.990s
```
All selected integration tests passed; zero failures.
