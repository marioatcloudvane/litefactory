# US-003 scoped unit execution — run 0009

Frozen revision: `4905bbbf909f786e54c46e750cdf06b2302f99c8861ff1494fcec22435f87c2d`; AC-005. Dependency US-001 is satisfied and was not changed.

Command executed:

```sh
go test ./internal/codex -run 'TestMcpOverrides|TestMCPLaunchSpec|TestArgs_.*MCP|TestArgs_IgnoreUserConfig|TestValidateMCPConfig|TestDeliveryArgsForceIsolationAndNativeRoleFiles|TestAllowUserConfigDropsTheIsolationFlag' -short -race -cover -v -timeout=8m
```

Exit status: 0. 14 top-level tests and 38 subtests passed; 0 failed. Output: `run0009-us003-unit.log`. Coverage: 27.2% of package statements (scoped selection).

Added in-memory TOML collision coverage for bare/spaced/quoted/Unicode-escaped table keys, dotted keys, nested inline tables and array tables; retained unrelated-content acceptance. Removed the old test that deliberately accepted a reserved inline server collision. String decoding now independently rejects Go-only escapes and tests control characters, quote/backslash/newline/Unicode values. Exact tool catalogs also assert the minimal empty environment. Added malformed/unterminated, oversized, excessive-depth and invalid-UTF8 rejection checks, plus harmless multiline strings, empty containers and table resets. Encoding decoder checks cover the JSON/TOML shared escape subset; they do not claim to decode all possible TOML string syntax.

Existing selected tests additionally exercise cold/resume placement, absent MCP behavior, configuration isolation and Validate native restrictions. Existing disk-backed collision tests were retained; newly added cases perform no filesystem I/O. No live providers, network, database, aggregate checks, acceptance review or factory submissions were performed by this specialist.

Changed test file: `internal/codex/mcp_test.go`. Relevant production snapshot files: `internal/codex/mcp.go`, `internal/codex/mcp_config.go`, `internal/codex/runner.go`, plus the neutral `internal/agent` launch registration owned by the satisfied dependency. Full engineering snapshot selection belongs to the orchestrator.
