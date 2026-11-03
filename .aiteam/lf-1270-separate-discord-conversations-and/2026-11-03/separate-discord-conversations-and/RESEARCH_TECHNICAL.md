# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

The request is a behavior-preserving in-process component extraction, not a new provider, process or release. The core prerequisite #1269 is CLOSED and PR #1365 merged on 2026-11-03; current HEAD is 8bac77dd77ac2da83234b4eba9c11b3a9c01fc10. Earlier #1269 research explicitly leaves complete Discord extraction to this sibling.

Evidence:

- `docs/factory-core.md:1`
- `.aiteam/lf-1269-establish-factory-core-contracts-and/2026-11-02/establish-factory-core-contracts-and/RESEARCH_BRIEF.md:23`

## F-02

Already reusable: messaging.Service takes messagingstorage.Port and factory.Messaging; messagingstate owns conversation/delivery values; factory contracts supply health, project, inbox and task views, with owner-action domains and feature draft services. messagingstorage keeps the database private while forwarding existing compound transactions. Passing *store.DB at trusted composition to a narrow interface is not by itself general database access by the consumer.

Evidence:

- `internal/messaging/service.go:103`
- `internal/messagingstorage/storage.go:1`
- `internal/messagingstate/types.go:1`
- `internal/factory/messaging.go:14`

## F-03

Touched: Discord gateway, HTTP adapter, intake/history/reactions; conversation execution/presentation/delivery; messaging state/storage; daemon chat lifecycle and core effect hosts; config.DiscordChat and operator docs. Daemon still owns token resolution/retry, redactor setup, adapter construction, gateway/polling loops and conversation worker startup. Moving only internal/messaging/discord would leave this ownership mixed.

Evidence:

- `internal/daemon/chat.go:31`
- `internal/daemon/chat_natural.go:43`
- `internal/daemon/chat_conversations.go:9`
- `internal/config/chat.go:1`

## F-04

Remaining factory-facing paths are concentrated in trusted daemon hosts: chatResearch directly reads graph/session/wait/result/report rows, locks sessions, records owner answers and reconciles publication; ChatIssueCreator directly claims previews and reconciles issue creation; ownerInboxReader rechecks retained/current ticket connections around provider I/O and redacts returned fields. These contain authority and recovery behavior that must remain core-owned behind bounded operations, not move wholesale into the Discord component.

Evidence:

- `internal/daemon/chat_research.go:59`
- `internal/daemon/chat_research.go:170`
- `internal/daemon/chat_research.go:498`
- `internal/daemon/chat_issue.go:27`
- `internal/daemon/chat_owner_inbox.go:18`

## F-05

Depends on it: daemon startup/webhook routing, ordinary owner conversations, /details, notices, operator alerts, standards/analysis/diagnosis hosts, feature creation and research answers. Approval service composition is shared with remote connector when enabled; Discord supplies approval delivery and reactions, while current owner identity/generation and action policy are checked centrally. Sibling remote extraction must not introduce a second approval coordinator.

Evidence:

- `internal/daemon/owner_actions.go:17`
- `internal/daemon/chat.go:135`
- `internal/daemon/conversation_standards.go:26`
- `internal/messaging/service.go:180`

## F-06

Superseded paths: daemon-owned transport lifecycle/intake/delivery coordination and chat-specific direct factory-state operations are the remaining boundary targets. Existing factory services, narrow messaging storage port and unchanged serializers are reuse points. The current import guard scans messaging production source, forbids daemon/turn/submission/SQL and general store symbols, but deliberately permits store error/context/time/identity compatibility symbols and does not assess daemon host extraction.

Evidence:

- `internal/messaging/boundary_test.go:15`
- `internal/messaging/ask/boundary_test.go:14`
- `docs/factory-core.md:88`

## F-07

Data/compatibility: existing SQLite transactions coordinate capture creation, provider replacement, ordered acknowledgments and outcome-plus-notice updates. No schema conversion is needed merely to change logical ownership. Delivery retries use saved head reply bytes without invoking a model or expiry, recheck authorization, retain paused records on permanent refusal, and honor Retry-After. Adapter uses stable enforced nonces; bounded provider deduplication does not establish indefinite exactly-once delivery.

Evidence:

- `docs/factory-core.md:72`
- `internal/messaging/conversation_delivery.go:14`
- `internal/messaging/discord/adapter.go:268`
- `docs/conversation-evidence.md:48`

## F-08

Availability: disabled chat returns no ChatOptions. Unresolvable optional token leaves chat unavailable and retries later while the daemon remains usable. This is distinct from malformed enabled configuration, which can still reject startup. Actions requiring Discord approval must remain pending/unavailable when Discord is disabled rather than acquire an alternate approval channel.

Evidence:

- `internal/daemon/chat.go:71`
- `internal/daemon/chat.go:111`
- `docs/factory-core.md:103`
- `internal/daemon/chat_test.go:94`

## F-09

Checks executed at this baseline: focused messaging boundary, owner-action authority/uncertain-restart characterization, resolution authorization and Discord heartbeat tests passed; daemon gateway reconnect/fallback, nonblocking registration and token recovery tests passed; saved-byte ordered delivery/access throttling and two factory pending-state restart/pre-change fixture tests passed; Ask bounded-view and selected factory tests passed. These were local non-race checks, not full suites, live provider tests or production upgrade evidence. The GitHub and Linear controlled-provider main-path test with optional transports absent also passed in internal/daemon; it covers result receipt/restart/publication recovery within the fixture's documented limits.

Evidence:

- `internal/messaging/owner_action_characterization_test.go:10`
- `internal/daemon/chat_gateway_internal_test.go:202`
- `internal/messaging/conversation_delivery_test.go:46`
- `internal/store/factory_prechange_state_test.go:17`
- `internal/store/factory_pending_state_test.go:13`
- `internal/daemon/factory_core_e2e_internal_test.go:44`

## F-10

Standards and recovery evidence: make check is the repository full formatting/vet/lint/test gate with selected race coverage. docs/factory-core.md documents unchanged-state rollback by stopping admission/draining and restarting reconciliation; a pre-change database fixture and baseline/current/baseline storage rehearsal exist. Their stated coverage does not establish every historical conversation shape or a live process drain. Existing Discord/conversation docs and setup must be updated with final ownership/operator guidance; no team-role or skill runtime change is indicated by this extraction.

Evidence:

- `Makefile:508`
- `.github/workflows/check.yml:1`
- `docs/factory-core.md:120`
- `internal/store/testdata/factory-pending-state/README.md:1`
- `docs/discord.md:1`
