# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Server instructions are composed at initialize from embedded markdown: overview/factory.md, overview/brainstorm.md, per-tool guidance/<tool>.md for each tool the grant can call, and a missing-scopes notice. Failure refuses initialize (fail closed). Files are capped at 8192 bytes.

Evidence:

- `internal/connector/prompts.go:56`
- `internal/connector/mcp.go:159`

## f2

Which tools a grant can call is decided by one function, availableTools(grant). tools/list, tools/call and instructions all use it, so a guide built on it cannot name an ungranted tool. missingScopes(grant) (from #1414) already names reconnect-worthy scopes.

Evidence:

- `internal/connector/discussion.go:316`
- `internal/connector/discussion.go:332`

## f3

Three MCP prompts exist (brainstorm_feature, whats_waiting, review_task), listed per grant from a small table; the project template is built from the same files. These are the 'surfaces some clients ignore' that the guide tool backs up.

Evidence:

- `internal/connector/prompts.go:25`
- `internal/connector/prompts.go:158`

## f4

Adding a tool touches several hand-listed places: tools/list block, tools/call dispatch and argument checks, the name allow-list in loadGuidance, and the isKnown-tool check near the end of mcp.go. A new always-available tool needs a gating rule (which scope lists it) because factory_overview is gated by the factory:overview scope and others by discussion, actions, brainstorm or advisory scopes.

Evidence:

- `internal/connector/mcp.go:182`
- `internal/connector/mcp.go:283`
- `internal/connector/guidance.go:18`
- `internal/connector/mcp.go:637`

## f5

Filing paths today: action_propose/action_confirm (with the Discord check mark for binding actions) and, under the repository:brainstorm scope, brainstorm_draft then brainstorm_tasks_get to create issues from drafts. The brainstorm guide says creating issues needs a normal proposal and a separate owner yes. No tool in the guidance is named for bug filing; #1422 is a separate feature and was not found in the repository code.

Evidence:

- `internal/connector/guidance/overview/brainstorm.md:1`
- `internal/connector/brainstorm.go:41`
- `internal/connector/actions.go:20`

## f6

Existing guarantees tests must keep: 'review_task' prompt requires task_evidence_read; read-only annotations are set per tool; the docs describe the connector in docs/guides/remote-connector.md and mcp.md. Existing drift tests exist for guidance sets (overview_contract_test.go, prompts_test.go, discussion_boundary_test.go) and are the natural place to extend.

Evidence:

- `internal/connector/overview_contract_test.go`
- `internal/connector/prompts_test.go`
- `docs/guides/remote-connector.md`

## f7

Impact: touched = internal/connector (mcp.go, guidance.go, prompts.go, discussion.go, new guidance files, tests) plus docs/guides. Depended on by ChatGPT/Claude clients through tools/list, and by the project template (ComposeTemplate). Superseded = nothing. Data = no migrations or stored-state change; grants are read only. Looked in internal/connector, internal/connectorstate, docs/guides.

_No source reference was recorded for this finding._
