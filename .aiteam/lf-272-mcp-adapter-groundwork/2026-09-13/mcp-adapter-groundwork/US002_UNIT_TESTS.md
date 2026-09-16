# US-002 unit evidence — run 0007

Identity: #276 / 5442842524; revision ac14c8f3efc72a7b4126389cc58165aef50d57f4567c9f22e13bf4dc0375523a; dependency US-001.

Files: internal/submission/tools_test.go; internal/agentresult/mcp_profile_contract_test.go.

Six top-level tests, 23 table subtests; all passed, zero failures. All tests are offline and pure; source boundary checks parse compile-time embedded source, requiring no runtime filesystem.

Coverage:
- Every mode discovers exactly three executable communication tools with explicit read/input effects; direct invocation eligibility denies shell, execution, creation aliases, dispatch, waiver, mode change and unknown names.
- Unknown/zero profiles deny all; modifying names, effects and schema bytes in returned catalog cannot alter subsequent server policy.
- Tool decoding retains exact internal whitespace, escaped Unicode and number lexemes; duplicate selectors/names, authority envelope injection, invalid names, excess depth and trailing documents fail.
- Compile-time source boundary assertions prohibit application/host/process imports and permission/process mutation; immutable agent.Request before/after snapshots retain existing native tools and unrelated server payloads while separate profiles are constructed and modified.
- Existing pure shared validator accepts a retained Validate assessment and rejects execution capture, mode override, foreign story and unretained snapshot. These tests do not claim dispatcher or host integration evidence.

Command: go test ./internal/submission ./internal/agentresult -run 'TestToolProfile_|TestDecodeToolCall_|TestMCPSharedContract_' -short -race -cover

Unavoidable integration boundary: ToolDispatcher requires a concrete AttemptGuard backed by real SQLite; no injectable pure guard exists. Handler invocation counts, raw argument bytes across the socket, guarded selector denial, authority refresh, durable receipt dimensions and deferred Plan publication refusal must be covered by the separately assigned real-store/private-socket tests. Authentic host permissions and inventory remain #274's evidence obligation.

Actual final output:
```
ok github.com/cloudvane/litefactory/internal/submission 1.581s coverage: 11.2% of statements
ok github.com/cloudvane/litefactory/internal/agentresult (cached) coverage: 18.9% of statements
```
The previous executed agentresult run passed in 1.643s with the same 18.9% package coverage. Coverage is package-wide for this narrow selected test set, not a claim of full package verification.
