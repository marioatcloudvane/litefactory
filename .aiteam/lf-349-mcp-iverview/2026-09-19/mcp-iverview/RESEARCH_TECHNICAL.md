# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The adapter implements a tools-only MCP stdio edge. It advertises protocol version 2025-11-25 and accepts tools/list and tools/call after initialization; resources and prompts are not exposed by this dispatch.

Evidence:

- `internal/mcpadapter/server.go:203` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f2

The existing catalog includes communication preflight, submission and receipt; family tools include feedback preflight, issue list/get/comment/create-batch, evidence submission, activity update/get and receipt lookup. Catalog membership is not blanket availability: installed handlers, mode and creation permission control exposure.

Evidence:

- `internal/submission/family_tools.go:9` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f3

Issue lookup deliberately returns repository-local summaries. Full bodies and comment history are not the contract of issues_get or issues_list. Extending issue context would touch this projection and its privacy/bounds tests.

Evidence:

- `internal/turn/issue_query.go:123` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f4

Runtime assembly registers activity and conditionally issue handlers. Publication and creation require additional dependencies and gates. Family dispatch binds permissions to an immutable attempt lease and uses durable receipts to avoid repeating effects.

Evidence:

- `internal/turn/mcp_families.go:119` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f5

Claude registration uses private per-attempt stdio configuration and rejects collisions with the reserved litefactory server name.

Evidence:

- `internal/harness/mcp.go:30` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f6

Codex registration uses per-invocation configuration overrides, exact enabled_tools, required startup and reserved-name collision checks. Existing provider registration should be reused if adding tools.

Evidence:

- `internal/codex/mcp.go:24` @ `7d5799834b5031c84286080708aaff6091ea63ee`

## f7

Existing tests exercise the family catalog over pipe, socket and HTTP fixture paths. Their presence is evidence of test coverage design, not a claim that tests or authentic provider sessions passed in this investigation.

Evidence:

- `internal/turn/mcp_family_catalog_test.go:119` @ `7d5799834b5031c84286080708aaff6091ea63ee`
