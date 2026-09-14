# US-003 production implementation — run 0007

Story #277 / 5442842741; revision 278cf863a7800a6cfee5bb38f4a88a674d1cb5daba61917e516d6493878da24b; dependency US-002 recorded. Owner explicitly places this foundation before #264 production adoption.

New files: internal/mcpadapter/{doc,protocol,server}.go; internal/agent/mcp.go; internal/turn/mcp_launch.go; internal/cli/cmd_mcp.go; docs/mcp-adapter.md.
Modified files: internal/submission/{broker,protocol}.go; internal/agent/request.go; internal/cli/commands.go; internal/config/{config,template}.go.

Production API: mcpadapter.Server{}.Run(ctx, io.ReadCloser, io.WriteCloser, Backend). Backend has List(ctx) and Call(ctx,name,json.RawMessage), returning submission.Response. submission.Client implements it. Stream Close must interrupt Read/Write. Run joins one reader, one serialized writer and at most four tool workers; eight queued response ceiling; active IDs release after output. EOF/context/cancel notifications propagate through private client cancellation. No detached workers or noninterruptible writer wrapper.

MCP 2025-11-25 tools-only initialize/initialized/list/call with supported-version negotiation, strict UTF-8/duplicate key/depth/field parsing, string/integer-literal IDs, invalid-state errors. Input limit 4 MiB + 64 KiB, private frame 16 MiB accommodates 4 MiB input plus base64 receipt/snapshot output, stdio output 64 MiB accommodates structured response plus compatible text. Conservative preallocation bounds precede response encoding; no HTML escape inflation; compact newline framing. Correlation and independent receipt dimensions retained, domain rejection is isError tool output.

Neutral launch: agent.MCPLaunchSpec has private immutable executable/descriptor/attempt values, validated constructor, getters, Arguments() allocating a fresh fixed slice, Clone(). agent.Request.MCPLaunch remains separate from Permissions. BuildMCPLaunchSpec(ctx,enabled,executable,broker,guard) checks guard, private broker files and broker's factory AttemptReference; disabled or absent guard yields nil. Caller supplies the resolved absolute executable. Broker Options.AttemptReference carries the existing factory attempt reference, no alternate identity is allocated. Producer performs no subprocess/config writes.

settings.mcp_auto_start is true by default, false preserved, strict direct boolean and documented template parity; loaded once. Explicit CLI mcp serve checks live descriptor regardless of convenience setting. Actual automatic host consumer/startup remains unavailable in #272 and belongs to #274; #264 supplies future production lifecycle adoption.

Sources: official https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle ; basic/transports ; server/tools (full direct links in docs/mcp-adapter.md).

Executed after final changes:
```
go test ./internal/mcpadapter ./internal/agent ./internal/config ./internal/cli ./internal/submission ./internal/turn -run 'TestDecodeAutoResumeOnSessionLimit|TestDefaultFileAutoResumeOnSessionLimit|TestToolProfile_|TestMCPTools_|TestAgentResultService' -count=1
? internal/mcpadapter [no test files]
ok internal/agent 0.391s [no tests to run]
ok internal/config 0.604s
ok internal/cli 0.482s [no tests to run]
ok internal/submission 0.614s
ok internal/turn 3.093s

go vet ./internal/mcpadapter ./internal/agent ./internal/config ./internal/cli ./internal/submission ./internal/turn
```
Vet passed without output. New protocol/config/launch tests are required from the test specialists; no claim of full story completion or acceptance yet. No forge, host wiring, live provider, dist, runtime config, commit or push operation.
