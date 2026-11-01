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

Read-only code tools (repo_grep, repo_read, repo_log, repo_diff, standards_read) and ask_task_agent exist. No connector tool lists or searches issues; the GitHub client already has SearchIssues and ListIssuesPage (see f10), so related and duplicate search is a new connector tool over an existing client call.

Evidence:

- `internal/connector/guidance.go:17`

## f7

Related work: #1110 (proposals and confirmation by impact tier) and #1112 (discuss and steer tasks) are both closed and delivered. No other open or closed issue covers brainstorming; the only search hit was this issue.

_No source reference was recorded for this finding._

## f8

Impact: touched are internal/connector (mcp.go, guidance.go, guidance/*.md, a new prompts handler, consent scopes), internal/owneraction (registry, catalog), internal/store (new brainstorm tables and a migration), the workflow launch path, and docs (remote-connector guide). Depends on it: ChatGPT/Claude connector clients, which see new instructions and prompts after reconnect. Superseded: nothing found. Compatibility: new tables need a migration; existing grants do not cover a new scope until the owner consents.

Evidence:

- `internal/connector/mcp.go:141`
- `internal/owneraction/registry.go:16`
- `internal/connector/server.go:64`

## f9

Discord docs describe one configured owner for binding approvals. Nothing in the docs mentions a second user sharing a connector brainstorm.

Evidence:

- `docs/discord.md:162`

## f10

The GitHub client already offers SearchIssues(repo, text, limit), ListIssuesPage and CreateIssue.

Evidence:

- `internal/github/search.go:15`
- `internal/github/issue_page.go:20`
- `internal/github/issues.go:378`

## f11

An idempotent issue publisher exists: PublishApprovedCreation publishes an owner-approved creation proposal, skips confirmed artifacts on resume and stops as uncertain rather than creating a duplicate. It is bound to a source task and agent-drafted proposals today.

Evidence:

- `internal/turn/creation_owner_publication.go:17`

## f12

The factory drops its own comments before reading them as instructions (bot author or a line starting with the lf marker), and only runs an initial issue body written by the verified project approver. So a factory-posted /research comment or a factory-authored issue body cannot start a task; the start must be a direct workflow launch under the owner's confirmed action.

Evidence:

- `internal/turn/classify.go:236`
- `internal/github/issues.go:62`
- `internal/store/research_admission.go:72`

## f13

Long-running advisory work already uses an async job pattern: ask_task_agent and judge_task return a job, read later with advisory_job_get and stopped with advisory_job_cancel, under the task:advisory scope with explicit repository selection and a cost note on the consent page.

Evidence:

- `internal/connector/task_advisory.go:19`
- `internal/connector/consent.go:262`

## f14

Nico is the name of the Discord conversation assistant (inferred from a test comment; no doc defines it). Discord proposals use the same five-second and fifteen-minute confirmation rules.

Evidence:

- `internal/messaging/conversation_standards_analysis_test.go:127`
- `docs/discord-conversations.md:44`

## f15

Connector answers are already bounded: summaries at most 1,000 Unicode code points and complete responses at most 128 KiB, with redaction and host-path protection.

Evidence:

- `docs/guides/remote-connector.md:257`
