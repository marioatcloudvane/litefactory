# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

The parent request explicitly separates fast discussion in Claude/ChatGPT using read tools from slower stage-agent and judge jobs. #1112 adds ask, judge, revise and approve, with only decision summaries on GitHub. #1109 and #1110 are closed with merged PRs #1237 and #1261; #1111 remains open. Source: authenticated issue lookups for #1106, #1109, #1110, #1111, #1112 and PR lookups during this pass.

Evidence:

- `docs/guides/remote-connector.md:285`

## F-02

The delivered read interface is task_evidence_read(task_ref, section, cursor?, limit?), rather than the issue's illustrative task_context/task_details names. Sections are status, results, questions, research, plan and approvals. Repository tools supply committed code evidence. The connector checks repository-scoped consent on every call and cannot mutate task state through these reads.

Evidence:

- `internal/connector/discussion.go:27`
- `internal/connector/discussion.go:142`
- `docs/guides/remote-connector.md:285`

## F-03

Context is retrieved on demand: task evidence pages carry revisions, sources, gaps and protected continuation cursors. Maximum 50 items and 64 KiB evidence page; the complete discussion response ceiling is 128 KiB. Paging restarts if evidence changes. Sections are independently observed, not an atomic task snapshot. These byte ceilings do not measure tokens or guarantee low voice latency.

Evidence:

- `internal/connector/discussion.go:19`
- `internal/connector/discussion.go:142`
- `docs/guides/remote-connector.md:296`

## F-04

An oversized individual saved item is marked unavailable, not split into readable fragments. The evidence service rejects source units above 256 KiB and also replaces a first unit exceeding its combined serialized page budget with a gap. Thus reading all continuation pages cannot recover every large architecture document. Whether selective section/field retrieval is needed is an open shaping point.

Evidence:

- `internal/taskevidence/service.go:43`
- `internal/taskevidence/service.go:77`
- `internal/taskevidence/service_test.go:62`

## F-05

Existing Plan discussion routes an owner's ordinary text to the stage agent with retained Plan state. Inspected tests assert that answers preserve stage, waits, readiness and exact approval state, and that replay does not duplicate the answer. The same tests expect an answer posted on the relevant GitHub issue. Reusing this path unchanged would violate #1112's intermediate-chat-only requirement. These are inspected tests, not newly executed verification.

Evidence:

- `internal/turn/plan_graph.go:126`
- `internal/turn/plan_graph_discussion_test.go:18`
- `internal/turn/plan_graph_discussion_test.go:35`
- `internal/turn/plan_graph_discussion_test.go:72`

## F-06

The existing judge service is reusable beneath its GitHub handler. The handler authenticates owner admission, captures discussion up to a cutoff, calls JudgeService.Execute and publishes through a durable report journal. Judge context pins redacted saved result, constituent Plan revisions and repository commit, with named gaps. MCP should not call the publishing wrapper blindly when its result belongs in chat.

Evidence:

- `internal/daemon/judge.go:18`
- `internal/turn/judge_context.go:28`
- `internal/turn/judge_context.go:95`

## F-07

Plain-word proposal foundation already supplies action_propose, action_confirm, action_cancel, action_inspect and action_history. Targets and revisions are immutable review facts; a separate confirmation has a minimum five-second gap and fifteen-minute expiry, and stale facts require a new proposal. The current allowlist does not classify Plan revision/approval, stage-agent or judge task operations; task steering must deliberately extend admission rather than assume generic action tools expose it.

Evidence:

- `internal/connector/actions.go:20`
- `internal/owneraction/registry.go:17`
- `docs/guides/remote-connector.md:535`

## F-08

Plan state already binds displayed candidates and dependency/shared revisions. Owner approval is resolved against exact current review evidence. The MCP surface can hide identifiers from speech but must preserve those internal bindings and reject changes between proposal and confirmation. A stage approval can advance downstream workflow; it is not merely a chat acknowledgment.

Evidence:

- `internal/planbrief/plan_workflow.go:58`
- `internal/turn/plan_graph.go:539`
- `internal/turn/plan_graph.go:896`

## F-09

Native agent requests distinguish cold, resume and cold-after-resume-refused paths. A stage-agent question can use factory-held context without exporting all of it to the chat client, but resuming the original agent's entire conversation is not guaranteed. Session reuse versus a fresh isolated advisory run, bounded conversational follow-up context, and behavior while a stage advances remain shaping decisions.

Evidence:

- `internal/agent/request.go:15`
- `internal/agent/request.go:47`
- `internal/agent/request.go:101`

## F-10

Impact — touched areas: connector tool catalog/guidance and consent, task evidence projection, shared owner-action admission, daemon job orchestration, Plan discussion/approval, judge service and durable reporting/store. Consumers: Claude/ChatGPT MCP clients, GitHub owner replies, Discord notifications/confirmation, Research and Plan workflows. Existing connector, Plan, judge and owner-action tests are relevant regression suites. No replacement of GitHub commands is requested. Durable job, discussion-summary and recovery records may require schema changes; no specific migration is established in this research pass. Existing read grants must remain read-only and existing installations retain their pending owner decisions.

Evidence:

- `internal/connector/mcp.go:171`
- `internal/owneraction/registry.go:17`
- `internal/turn/plan_graph.go:470`
- `internal/daemon/judge.go:18`
- `docs/guides/remote-connector.md:535`

## F-11

Prior #1109 and #1110 briefs confirm the read-only discussion and confirmation foundations intentionally excluded new stage steering and job suites. Current code and remote connector documentation supersede their historical tool/scope wording. No applicable AGENTS.md was found in the worktree/root-parent search. Research used read-only inspection and GitHub lookups; no live hosted-client walkthrough, performance measurement, implementation or new test run was performed.

Evidence:

- `.aiteam/lf-1110-mcp-3-proposals-and-confirmation-without/2026-10-27/mcp-3-proposals-and-confirmation-without/RESEARCH_BRIEF.md:1`
- `.aiteam/lf-1109-mcp-2-read-tools-to-discuss-tasks-task/2026-10-26/mcp-2-read-tools-to-discuss-tasks-task/RESEARCH_BRIEF.md:1`
- `docs/guides/remote-connector.md:535`

## F-12

Shaping inspection confirms judge source selection already covers saved Research results, Plans and Bug analyses, excludes Bug repair results, and refuses unavailable sources; this supports including Bug judging without inventing a new judge. No tests were executed in this shaping pass.

Evidence:

- `internal/store/judge_sources.go:263`
- `internal/store/judge_bug_sources_test.go:44`
- `internal/store/judge_bug_sources_test.go:97`

## F-13

Shaping impact: F-10 remains applicable; durable job/conversation/proposal/delivery records and connector documentation are in scope, with additive compatibility for old installations; no existing public tool or GitHub workflow is retired, and no team-role or skill replacement was identified in the connector, owner-action and judge surfaces inspected. Exact storage migration design belongs to Plan, while preservation requirements are K-02 and K-04.

Evidence:

- `internal/owneraction/registry.go:17`
- `internal/turn/judge_context.go:28`
- `docs/guides/remote-connector.md:285`
