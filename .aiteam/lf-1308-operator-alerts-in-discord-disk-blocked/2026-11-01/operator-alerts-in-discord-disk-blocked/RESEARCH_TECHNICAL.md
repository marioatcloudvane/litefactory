# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Disk low: the daemon already measures free space each tick against a minimum and holds new runs. It logs a WARN and stores a hold text in memory, cleared every tick. No durable state and no Discord post.

Evidence:

- `internal/daemon/tick.go:3466`
- `internal/daemon/daemon.go:494`

## f2

Pool full: the shared pool refuses a reservation and returns a capacity-deferral text with used/total. The deferral is a returned reason, not a recorded incident, so 'full longer than X' has no timer today.

Evidence:

- `internal/daemon/capacity.go:39`

## f3

Stuck queue: when a comment cannot be consumed the daemon logs 'this task's queue stops here'. That is a log line only; there is no record to read back.

Evidence:

- `internal/daemon/tick.go:3364`

## f4

Service.Announce deduplicates queued notices by key, but that proves only local enqueue deduplication; Discord adapter documentation explicitly warns nonce deduplication is short-lived and uncertain sends after delay or restart can duplicate remotely.

Evidence:

- `internal/messaging/notices.go:538`
- `internal/messaging/discord/adapter.go:266`

## f5

Owner controls: Discord chat is conversational and free-text. Docs say chat cannot start, approve, stop or repair work. 'mute disk for 2h' and 'alerts' would be a new, narrow command type or a conversation tool; none exists.

Evidence:

- `docs/discord.md:5`

## f6

CI / main red: the GitHub webhook handler reads the X-GitHub-Event header, and a test shows workflow_run is accepted but ignored (202, no hint queued). Check-run or workflow_run events are not consumed today. Poll fallback exists for issues.

Evidence:

- `internal/webhook/handler.go:197`
- `internal/webhook/handler_test.go:279`

## f7

Deploy failed: the factory has no project deploy concept. 'deploy' in the code means only the factory's own pre-deploy database backups. A project deploy failure is therefore a new input the factory must be told how to watch. Shaping pass confirmed this by a search of internal/ and cmd/.

Evidence:

- `internal/config/config.go:120`
- `internal/daemon/housekeeping.go:93`

## f8

Provider failure: typed signals exist at the provider boundary. Conversation safety refusal (ErrConversationRefused) and sign-in failure (ErrConversationAuthentication) are distinct errors with plain-word replies; agent runs carry quota Interruption and Transient capacity-refusal evidence; Codex captures the quota message. None is recorded as a durable incident or counted over time.

Evidence:

- `internal/agent/conversation.go:21`
- `internal/messaging/conversation_cause.go:35`
- `internal/agent/result.go:90`
- `internal/codex/runner.go:665`

## f9

Persistence: the store is SQLite with dated migrations (latest 20261031215519). Incident state that survives restart needs a new table and migration. Related precedent: log_incidents and daemon_health exist in the store.

Evidence:

- `internal/store/log_incidents.go:18`
- `internal/store/daemon_health.go`

## f10

MCP: factory_overview is a read-only tool with a fixed output schema served by the connector and built in the overview package. Adding active alerts means extending that schema and service.

Evidence:

- `internal/connector/mcp.go:154`
- `internal/overview/service.go`

## f11

Impact: daemon signal observations, capacity and provider handling, messaging notices/commands and Discord payloads, alerts config/template/docs, SQLite incident state and migration, overview/MCP, doctor and GitHub client polling are affected; owners, MCP clients and existing installations depend on these surfaces, while scheduling, recovery, workflow check reporting and webhooks remain unchanged; no team/skill change is required by this scope and no existing feature is superseded.

Evidence:

- `internal/config/template.go:30`
- `internal/messaging/discord/adapter.go:250`
- `.github/workflows/check.yml:1`
- `internal/github/client.go:83`

## f12

Discord config already supplies a validated numeric owner ID and allowed-channel constraints, but its outgoing payload exposes only an empty mention parse list and no user allowlist, so owner identity alone cannot deliver a ping.

Evidence:

- `internal/config/chat.go:24`
- `internal/config/chat.go:67`
- `internal/messaging/discord/adapter.go:250`
- `internal/messaging/discord/adapter.go:315`

## f13

A fixed Discord phrase is already recognised before the conversation model: 'new conversation' is matched exactly (trimmed, lower-case). This is a working precedent for short alert commands.

Evidence:

- `internal/messaging/natural.go:97`

## f14

GitHub webhook subscriptions register only issues, issue_comment and sub_issues. Receiving CI results by webhook would mean adding an event to every existing subscription; polling needs no subscription change.

Evidence:

- `internal/webhookreg/webhookreg.go:26`
- `internal/webhookreg/subscriptions.go:111`

## f15

Dead dispatches are queryable; report recovery loads durable task-associated records through ListReportRecoveries, and doctor classifies report_deliveries as uncertain when possibly_sent is set without confirmed completion, providing a concrete delivery-stuck source without relying on conversation presentation state.

Evidence:

- `internal/store/graph_dispatches.go:383`
- `internal/daemon/report_recovery.go:41`
- `internal/store/doctor_evidence.go:75`

## f16

The repository's single check workflow runs on main pushes or manual dispatch, cancels superseded in-progress runs and reports failures to GitHub issues; it is the concrete first-version main-red target.

Evidence:

- `.github/workflows/check.yml:1`

## f17

GitHub documents GET repository Actions workflow runs, branch filtering, pagination up to 100 per page and Actions read for fine-grained credentials (classic private-repository tokens require repo); installed credential access was not exercised. Source: https://docs.github.com/en/rest/actions/workflow-runs#list-workflow-runs-for-a-repository

Evidence:

- `internal/github/client.go:83`
- `internal/github/client_test.go:44`

## f18

Discord documents explicit allowed_mentions user allowlists; repository output currently suppresses parsing entirely, so narrow critical-owner mentions require an intentional transport capability and cannot be achieved by text alone. Source: https://docs.discord.com/developers/resources/message#allowed-mentions-object

Evidence:

- `internal/messaging/discord/adapter.go:250`
- `internal/messaging/discord/adapter.go:315`
