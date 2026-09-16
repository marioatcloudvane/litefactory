# US-002 SQLite/private-socket evidence — run 0007

Identity: #276 / 5442842524; revision ac14c8f3efc72a7b4126389cc58165aef50d57f4567c9f22e13bf4dc0375523a; dependency US-001.

Changed file: internal/turn/mcp_tools_test.go. Six test functions, 11 table subtests.

Real temporary migrated SQLite, real private Unix sockets, existing Client/Broker, AttemptGuard, ToolDispatcher and AgentResultService are exercised. A synchronized counting wrapper delegates to the actual service, recording input bytes and method call counts without replacing validation/storage. No external service, provider subprocess, integration tags or new module.

Checks cover all five mode catalogs; server direct-call denial without backend invocation; equivalent shared preflight; no preflight revision; exactly one backend delegation per request; exact contribution bytes including whitespace; duplicate journal/request/receipt identity and one accepted revision; independent pending engineering/acceptance/publication states; explicit authority-selector and edit-target refusal; complete permission-digest mismatch refusal; real discovery/preflight/submit/receipt denial after cancellation, close, replacement and fresh broker authority; unaffected internal historical reads.

Plan tests prove valid story-bearing draft and valid completed proposals are refused through MCP before any accepted snapshot; ordinary helper submissions still accept them. Valid edits against an already retained proposal are refused both before and after a fresh runtime/launch recovery. Internal receipt recovery remains usable. Safe Plan discussion succeeds in the all-mode table. Validate tests exercise retained-assessment communication and fresh-capture refusal, with acceptance still pending. Retained assessment facts in that resolver are explicit synthetic trusted fixture metadata; this does not certify provider execution or authentic retained evidence delivery.

The first development run failed because its recovery fixture attempted to rebind the same run/attempt after restart, and SQLite correctly refused its unique launch constraint. The test was corrected to allocate a fresh recovery launch; no production change was required.

Executed final plain check:
`go test ./internal/turn -run 'TestMCPTools_' -count=1`
Output: `ok github.com/cloudvane/litefactory/internal/turn 2.164s`.

Race check command: `go test ./internal/turn -run 'TestMCPTools_' -count=1 -race`.

Unit evidence command, retained unchanged:
`go test ./internal/submission ./internal/agentresult -run 'TestToolProfile_|TestDecodeToolCall_|TestMCPSharedContract_' -short -race -cover`

Coverage boundaries: explicit stdio adapter/initialization/cancellation joins belong to US003/US005; exact expiry boundaries have US001 real-store evidence; native host startup/inventory is unavailable here and remains #274. No live GitHub publication or acceptance/architecture/code review was performed.

Race output: `ok github.com/cloudvane/litefactory/internal/turn 16.290s`. Final six test functions and 11 subtests passed; zero remaining failures.
