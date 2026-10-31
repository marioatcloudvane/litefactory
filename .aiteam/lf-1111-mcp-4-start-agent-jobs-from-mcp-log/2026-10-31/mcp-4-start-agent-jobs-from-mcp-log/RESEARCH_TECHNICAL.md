# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

Current remote MCP tools/list and initialize expose server guidance and tools according to availability; action_propose schemas derive variants from the domain catalogue. There is no workflow discovery or job_status tool in this handler. This is distinct from attempt-scoped worker MCP.

Evidence:

- `internal/connector/mcp.go:151`

## F-02

Existing action catalogues already include standards-analysis start and standards-author start/answer/revise/continue and related operations. Generic action_propose therefore already reaches these agents when their adapters and grants are enabled; this request is not starting from zero.

Evidence:

- `internal/messaging/owner_actions.go:364`

## F-03

Author start, answer and revise execute a derived author.continue step when a runner exists. Analysis execution invokes the existing log-analysis service and its retained recovery path. Reuse domain session versions and receipts rather than inventing duplicate author state.

Evidence:

- `internal/daemon/conversation_standards_frozen.go:300`

## F-04

Confirmation returns recorded admission state without running the domain. A daemon-owned Tick worker later executes ready actions sequentially. Step uses ten-minute bounded contexts, current-authority rechecks, durable execution claims and recovery for executing/uncertain records. It is a foundation, not an established general concurrent job scheduler.

Evidence:

- `internal/owneraction/service.go:303`

## F-05

Log analysis already retains invocation state, scope, idempotent start key, evidence, reports, proposal links and filing outcomes. CLI dry-run executes the analysis but files nothing; the normal wall-time ceiling defaults to 1800 seconds. Any new job deadline policy must reconcile that with the existing owner-action ten-minute Step context.

Evidence:

- `internal/cli/cmd_log_analysis.go:24`

## F-06

Authoring exposes retained sessions, questions, optimistic expected versions and separate start/answer/continue/complete/publish steps. The CLI start saves intent, whereas the conversational frozen adapter also runs the next draft. Complete creates an unapproved proposal; drafting must not silently publish a standard.

Evidence:

- `internal/cli/cmd_standards_author.go:23`

## F-07

Existing outcome notifications read owner-action status and summary, validate configured channel access and queue messages using durable identities. Notifications depend on a configured authorized destination; unconditional Discord delivery for every MCP-origin job was not established. The independent owner-action executor can operate without a healthy Discord backend.

Evidence:

- `internal/messaging/conversation_owner_actions.go:299`

## F-08

Impact—touched: remote connector tools, schemas and guidance; owneraction policy/catalogue/execution; messaging standards adapters; loganalysis and standards authoring stores; daemon worker and notification composition. Existing tests include connector action contracts, offline backend duplicate-effect prevention, frozen standards recovery and plain-word action journeys. No code or tests were changed or run during this read-only research.

Evidence:

- `internal/daemon/owner_action_backend_test.go:58`

## F-09

Impact—callers and compatibility: Claude/ChatGPT remote clients and Discord share owneraction domain rules; CLI authoring/log-analysis and existing action inspection/history must remain usable. Existing database records carry proposal, execution and domain identities. A new job store/schema would require additive compatibility and a defined mapping to these records; no common generic job schema was found in the inspected connector/owneraction paths.

Evidence:

- `internal/owneraction/types.go:33`

## F-10

Impact—superseded: no existing workflow needs retirement for a catalogue/job extension. The issue's dedicated tool names overlap the already shipped generic action path. A generic interface versus convenience wrappers remains a shaping choice, not an observed necessity.

Evidence:

- `internal/connector/actions.go:284`

## F-11

Related context: #1110 is closed and merged at HEAD 646ca70a28e7d3c422169ef631b1eb644408670c via PR #1261. Its retained brief explicitly leaves new job orchestration to #1111 and stage steering to #1112. GitHub lookup found #1111 and #1112 open, with parent #1106 closed. Recent connector/owneraction history includes 97c2b6e4 and repository-grant changes. Historical briefs were treated as context, current code as behavior evidence.

Evidence:

- `.aiteam/lf-1110-mcp-3-proposals-and-confirmation-without/2026-10-27/mcp-3-proposals-and-confirmation-without/RESEARCH_BRIEF.md:1`

## F-12

Evidence limits and handoff: repository code and GitHub issue/PR metadata were inspected; no authenticated live remote client, agent job or Discord delivery was exercised. Scheduler concurrency, restart handling, job/session identity, discovery filtering granularity and notifications need shaping decisions. No owner-only missing fact blocks this bounded pass. Advisory schemas and examples are retained as a data artifact.

Evidence:

- `internal/daemon/owner_action_backend.go:27`
