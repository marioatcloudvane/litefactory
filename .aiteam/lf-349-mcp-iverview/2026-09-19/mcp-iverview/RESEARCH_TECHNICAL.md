# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The adapter implements a tools-only MCP stdio edge. It advertises protocol version 2025-11-25 and accepts tools/list and tools/call after initialization; resources and prompts are not exposed by this dispatch.

Evidence:

- `internal/mcpadapter/server.go:203` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f2

The catalog includes communication preflight, submission and receipt, plus feedback, issue lookup/publication/creation, evidence, activity and receipt families. Installed handlers, mode and permissions control exposure.

Evidence:

- `internal/submission/family_tools.go:9` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f3

Issue lookup returns repository-local summaries, not full bodies or comment history.

Evidence:

- `internal/turn/issue_query.go:123` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f4

Runtime assembly conditionally registers handlers. Publication and creation have additional gates; dispatch binds permissions to an immutable attempt lease and uses durable receipts.

Evidence:

- `internal/turn/mcp_families.go:119` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f5

Claude uses private per-attempt stdio registration and reserved-name collision checks.

Evidence:

- `internal/harness/mcp.go:30` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f6

Codex uses per-invocation overrides, exact enabled_tools, required startup and reserved-name collision checks.

Evidence:

- `internal/codex/mcp.go:24` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f7

Existing catalog tests cover pipe, socket and HTTP fixtures. Their existence does not establish passing tests or authentic provider compatibility in this investigation.

Evidence:

- `internal/turn/mcp_family_catalog_test.go:119` @ `7d5799834b5031c84286080708aaff6091ea63ee`
