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

Deploy failed: found no project deploy pipeline concept in the factory code. 'deploy' appears only in config text. A deploy failure is therefore a new probe, probably fed by the project's own GitHub workflow result, not something the factory observes.

Evidence:

- `internal/config/config.go`

## f8

Provider failure: provider and refusal handling exists in providerprocess and agent/codex runners, and conversation capability code. Whether it records a durable, typed 'safety refusal' or 'sign-in expired' event was not traced in this pass.

Evidence:

- `internal/providerprocess/conversation_capability.go`

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

Impact. Touched: daemon tick, pool/capacity, messaging notices, config (new alerts block plus template and docs), store (table plus migration), overview and connector (optional), webhook handler (CI events). Superseded: nothing; disk WARN log stays. Compatibility: new config block must default safely for existing installs. Tests: daemon, messaging, store, config.

Evidence:

- `internal/config/template.go:30`
