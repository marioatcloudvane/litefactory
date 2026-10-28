# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

Baseline is commit 318f954c8f34b0a5a5f43f08fe90c837bb102a18 with a clean worktree. GitHub #1108 is CLOSED and PR #1195 MERGED; #1109 delivery PR #1237 is MERGED. Parent #1106 is closed but #1111 and #1112 remain open and name #1110 as a dependency. This establishes repository delivery history, not live deployment. External sources inspected: https://github.com/marioatcloudvane/litefactory/pull/1195, https://github.com/marioatcloudvane/litefactory/pull/1237, https://github.com/marioatcloudvane/litefactory/issues/1111, https://github.com/marioatcloudvane/litefactory/issues/1112.

Evidence:

- `internal/connector/mcp.go:1`

## F-02

Current remote MCP discovery and dispatch offer overview, task evidence, usage and code read tools. Tools are filtered by consented scopes; known scopes describe read authority only. There is no proposal/confirm/cancel action dispatch in this catalog. Grant coverage now selects repositories and task_ref is chosen per call; older briefs claiming all-repository coverage are superseded by current code.

Evidence:

- `internal/connector/mcp.go:158`
- `internal/connector/server.go:44`
- `internal/connector/consent.go:116`
- `internal/store/connector_types.go:84`

## F-03

Discord conversation proposals resolve exact invocations, compute an internal digest, generate a random 16-character code, expire after 15 minutes and bind confirmation to successful prompt delivery. Plain whole-message yes and cancellation already work; exact details remain available without requiring owners to type a hash.

Evidence:

- `internal/messaging/conversation_actions.go:16`
- `internal/messaging/natural.go:48`
- `internal/messaging/natural_proposal.go:16`
- `internal/messaging/conversation_delivery.go:79`

## F-04

The store atomically consumes a delivered, unexpired exact proposal revision into a journaled execution/inbox invocation. It checks increasing numeric source/confirmation IDs. This is ordering, not a minimum elapsed-time gap. Authorization checks at the conversation layer recheck digest and current target before execution.

Evidence:

- `internal/store/chat_natural_dialogue.go:88`
- `internal/messaging/conversation_actions.go:246`

## F-05

Reuse has transport coupling: current dialogues are keyed by transport, owner and channel, confirmations require numeric message ordering, and execution admission creates chat inbox work. MCP client/grant/request identity is not part of these dialogue fields. Terminal execution payload is cleared and terminal replay records can be purged after 24 hours, so these records alone do not establish the requested durable client/proposal audit.

Evidence:

- `internal/store/chat_natural_dialogue.go:12`
- `internal/store/chat_natural_dialogue.go:102`
- `internal/store/chat_natural_dialogue.go:217`
- `internal/store/chat_natural_dialogue.go:240`

## F-06

Existing standards-author answer, revise, continue, edit, validate, rebase and publish bypass a separate proposal. Publish requires owner words in the current message, then executes directly. This collides with both all-changing-actions-as-proposals and the additional binding-action tap; it is current intended behavior in code, not a reproduced regression.

Evidence:

- `internal/messaging/conversation_actions.go:35`
- `internal/messaging/conversation_actions.go:300`
- `internal/messaging/conversation_actions.go:327`

## F-07

No reaction approval intake was found in the inspected Discord gateway: intents cover guild messages and message content, and dispatch handles READY/RESUMED/MESSAGE_CREATE. A binding-action tap cannot be assumed to exist from current message confirmation support.

Evidence:

- `internal/messaging/discord/gateway.go:25`
- `internal/messaging/discord/gateway.go:350`

## F-08

Connector request audit logs event, outcome, reason, tool, correlation ID and duration; it does not log a proposal or client binding. Existing grant records do carry ClientID. Owner-visible confirmation history needs a dedicated link from authenticated grant/client to the exact proposal and result, rather than assuming current connector logs satisfy it.

Evidence:

- `internal/connector/transport.go:92`
- `internal/store/connector_types.go:84`

## F-09

Impact — touched: connector transport/catalog/consent/guidance, messaging proposal and standards execution policy, Discord gateway/intake, daemon domain adapters and store/migrations/tests. Depends on it: remote clients, existing Discord conversations and standards author, plus upcoming #1111 jobs and #1112 stage steering. Superseded: duplicate transport-specific confirmation policy and selected direct standards paths; whether legacy CLI/GitHub hash commands are retired is not settled by this issue. Data/compatibility: pending dialogues, journal recovery, read-only OAuth grants and audit retention need explicit treatment. No new team roles or skills are required by the supplied request; existing remote guidance must explain the resulting protocol. External sources inspected: https://github.com/marioatcloudvane/litefactory/issues/1111, https://github.com/marioatcloudvane/litefactory/issues/1112.

Evidence:

- `internal/connector/server.go:1`
- `internal/messaging/conversation_actions.go:16`
- `internal/store/chat_natural_dialogue.go:12`
- `internal/daemon/remote_connector.go:1`
- `docs/guides/mcp.md:27`

## F-10

Adjacent history was checked: merged PRs #670 (natural commands), #844 (persistent conversation), #1195 (remote endpoint), #1237 (discussion reads), #1238 (consent) and current commit #1239 (repository grants). Prior #1108/#1109 briefs describe foundations but are not stronger than current code, especially for grant scopes and coverage. No prior #1110 research brief was found in the session-artifact filename scan. External sources inspected: https://github.com/marioatcloudvane/litefactory/pull/670, https://github.com/marioatcloudvane/litefactory/pull/844, https://github.com/marioatcloudvane/litefactory/pull/1239.

Evidence:

- `.aiteam/lf-1108-mcp-1-remote-mcp-endpoint-with-owner/2026-10-25/mcp-1-remote-mcp-endpoint-with-owner/RESEARCH_BRIEF.md:1`
- `.aiteam/lf-1109-mcp-2-read-tools-to-discuss-tasks-task/2026-10-26/mcp-2-read-tools-to-discuss-tasks-task/RESEARCH_BRIEF.md:1`

## F-11

Executed existing tests passed: go test ./internal/store -run 'TestNatural(AuthorizationFencesAndCleanup|ConfirmationCancellationRace|CorrectionAndExpiryCleanup)$' -count=1; go test ./internal/messaging -run 'TestPlainYes(ConfirmsTheOneOpenProposal|NeverConfirmsMoreThanAYesOrAnEarlierProposal)$' -count=1. These establish selected existing lifecycle behavior only. No new code, full-suite check, live OAuth client, minimum-delay experiment or Discord tap test was performed.

Evidence:

- `internal/store/chat_natural_test.go:59`
- `internal/messaging/conversation_plain_test.go:58`

## F-12

Applicable Go guidance favors existing interfaces, explicit failure boundaries and cancellation. Repository rules place all SQL in internal/store, require new timestamped migrations for schema changes and prohibit writing the live daemon binary or editing runtime state. Existing proposal/execution uncertainty must remain distinct from confirmed external effect.

Evidence:

- `teams/source/go-development/implement/skills/litefactory-rules/litefactory.md:7`
- `teams/source/go-development/implement/skills/litefactory-rules/litefactory.md:39`
- `internal/store/chat_natural_dialogue.go:216`

## F-13

Run 0002 carries forward F-01–F-12 as predecessor observations, without re-investigation or test reruns. Current git status shows only the untracked generated session artifact directory. Preparation reports base synchronization deferred at 19 commits behind; the retained findings therefore describe the earlier inspected baseline, not a fresh assessment of origin/main.

Evidence:

- `.aiteam/lf-1110-mcp-3-proposals-and-confirmation-without/2026-10-27/mcp-3-proposals-and-confirmation-without/RESEARCH_TECHNICAL.md:1`
