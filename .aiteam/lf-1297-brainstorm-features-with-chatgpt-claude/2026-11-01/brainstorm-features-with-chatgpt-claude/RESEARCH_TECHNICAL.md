# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Server instructions are the concatenation of per-tool guidance markdown files for the tools the grant allows. There is no general factory overview; the file named factory_overview.md only describes the factory_overview tool. loadGuidance accepts only known tool names and caps each file at 8192 bytes.

Evidence:

- `internal/connector/guidance.go:13`
- `internal/connector/guidance/factory_overview.md:1`

## f2

The MCP initialize reply advertises only the tools capability. No prompts capability, prompts/list or prompts/get exists.

Evidence:

- `internal/connector/mcp.go:146`

## f3

Owner actions go through a fixed allowlist. Unknown commands are refused. Current commands: workflow-analysis, workflow-author, draft, diagnose, answer, revise_task (conversational); confirm, approve_stage (binding); standards-* operations. A create-and-start action would need a new classified entry and a tier decision.

Evidence:

- `internal/owneraction/registry.go:16`

## f4

Proposal flow: propose, then a separate conversational yes at least five seconds later and within fifteen minutes; binding actions also need the owner's Discord check-mark reaction. Proposals are bound to the original client and grant.

Evidence:

- `internal/connector/guidance/action_propose.md:1`

## f5

discussion_open is task-scoped. It resolves an existing task (session) by task_ref and repository grant, and cannot create a session. A brainstorm without a task needs new storage and a new grant scope or reuse of an existing one.

Evidence:

- `internal/store/connector_discussion.go:13`
- `internal/connector/guidance/discussion_open.md:1`

## f6

Read-only code tools (repo_grep, repo_read, repo_log, repo_diff, standards_read) and ask_task_agent exist. Issue list/get tooling for the connector was not found by grep as a connector tool, so related and duplicate issue search looks new on the connector side.

Evidence:

- `internal/connector/guidance.go:17`

## f7

Related work: #1110 (proposals and confirmation by impact tier) and #1112 (discuss and steer tasks) are both closed and delivered. No other open or closed issue covers brainstorming; the only search hit was this issue.

_No source reference was recorded for this finding._

## f8

Impact: touched are internal/connector (mcp.go, guidance.go, guidance/*.md, a new prompts handler), internal/owneraction (registry, catalog), internal/store (new brainstorm tables and a migration), and docs. Depends on it: ChatGPT/Claude connector clients, which see new instructions and prompts after reconnect. Superseded: nothing found. Compatibility: new tables need a migration; existing grants do not cover a new scope until the owner consents.

Evidence:

- `internal/connector/mcp.go:141`
- `internal/owneraction/registry.go:16`

## f9

Discord docs describe one configured owner for binding approvals. Nothing in the docs mentions a second user (Nico) sharing a connector brainstorm.

Evidence:

- `docs/discord.md:162`
