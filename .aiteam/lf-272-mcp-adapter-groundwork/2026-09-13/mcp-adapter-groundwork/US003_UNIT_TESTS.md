# US-003 unit evidence — run 0007

Story #277 / 5442842741; revision 278cf863a7800a6cfee5bb38f4a88a674d1cb5daba61917e516d6493878da24b; dependency US-002 recorded.

Added test files:
- internal/mcpadapter/server_test.go
- internal/config/mcp_test.go
- internal/agent/mcp_test.go

Fourteen top-level test functions passed. Tests use owned in-memory io.Pipe streams, a handwritten Backend and in-memory config/source fixtures; no database, filesystem or provider is required.

Adapter cases cover supported and alternate-version negotiation; only tools capability; required initialization notification; invalid initialization fields; duplicate keys; invalid UTF-8/JSON, methods, parameter selectors and string/number/null/object IDs; excessive nesting; duplicate active IDs and four-worker admission limit; exact argument bytes; separate connection/application correlation; structured and compatible text results with pending receipt dimensions and domain isError; string-versus-numeric ID distinction; matching, unknown and completed cancellation; cross-connection isolation; admitted-worker EOF joins; EOF/context interruption of blocked output; response-backlog shutdown; excessive input/output frames; full 4 MiB contribution plus envelope acceptance. Every observed stdout frame must parse as JSON-RPC.

Config cases cover absent/default true, true, preserved false, quoted string, integer, null, yes, array, mapping and empty invalid forms, plus template parity. Immutable launch tests cover copied argument slices, cloned values, invalid paths/attempts, permission before/after snapshots with permitted native/unrelated-server data and source boundary assertions prohibiting host/process/config effects. Real broker/guard producer construction remains for integration tests.

Initial test development expected parse-error -32700 for invalid UTF-8. The server correctly refused it with invalid-request -32600 before backend forwarding; the test was corrected because the criterion requires bounded refusal, not that particular error code.

A concrete production defect was discovered: NewMCPLaunchSpec accepted NUL bytes in executable and descriptor paths. The two NUL-path cases failed. The orchestrator explicitly assigned a narrow implementation fix; internal/agent/mcp.go now refuses NUL in both fields. No other production change occurred in this test assignment.

Executed fix regression:
```
go test ./internal/agent -run 'TestMCPLaunchSpec_RejectsInvalidValues' -short -race -cover
ok github.com/cloudvane/litefactory/internal/agent 1.722s coverage: 0.6% of statements
```

Executed final affected unit set:
```
go test ./internal/mcpadapter ./internal/config ./internal/agent -run 'TestMCPServer_|TestMCPAutoStart_|TestMCPLaunchSpec_' -short -race -cover
ok github.com/cloudvane/litefactory/internal/mcpadapter 2.781s coverage: 90.7% of statements
ok github.com/cloudvane/litefactory/internal/config (cached) coverage: 65.3% of statements
ok github.com/cloudvane/litefactory/internal/agent 1.423s coverage: 1.7% of statements
```
The unchanged config tests initially executed in 1.662s with the same 65.3% coverage. Coverage percentages are package-wide for this scoped selection, not full-suite claims. No remaining failed tests.

Separate evidence still required: real stdio-to-private-socket/SQLite behavior, explicit serve with auto-start false, stale descriptor refusal and real bound launch producer/CLI integration. Native provider startup/teardown/inventory remains unavailable until #274; no synthetic fixture claims authentic host delivery.
