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

Discord output exists: Service.Announce queues a keyed message to a channel; the same key queued twice changes nothing, which gives free per-incident dedup. Notices go to the configured notice channel, with a text-protection step.

Evidence:

- `internal/messaging/notices.go:538`
- `internal/config/chat.go:26`
- `internal/daemon/chat.go:237`

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

Impact. Touched: daemon tick, pool/capacity, provider result handling, messaging notices and inbound chat, config (new alerts block plus template and docs), store (table plus migration), overview and connector, GitHub client or webhook handler (CI results). Depends on it: owner via Discord, MCP clients via factory_overview, doctor. Superseded: nothing; the disk WARN log stays. Compatibility: new config block must be optional and default safely for existing installs; existing webhook subscriptions keep their events. Tests: daemon, messaging, store, config, overview.

Evidence:

- `internal/config/template.go:30`

## f12

Discord already knows the owner (chat.discord.owner_id, numeric, validated), so 'mention the owner' and 'only the owner may mute' need no new identity setting. The notice channel is optional and must be one of the allowed channels.

Evidence:

- `internal/config/chat.go:24`
- `internal/config/chat.go:67`

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

Dead dispatches are already queryable in the store (graph_dispatches dead query) and the daemon iterates them, so the dead-dispatch part of 'task stuck' has a readable source. Report delivery 'uncertain' states also exist in the store (e.g. chat presentation, graph attempts); which one maps to #1240 was not traced.

Evidence:

- `internal/store/graph_dispatches.go:383`
- `internal/daemon/graph_dispatch.go:609`
- `internal/store/chat_presentation.go`
