# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Current daemon Forge and Classifier interfaces take concrete GitHub issues/comments and repository types. Executor and Reporter are separate, but this is not a neutral messaging interface. A Discord adapter cannot implement this API faithfully without inventing issue semantics.

Evidence:

- `internal/daemon/daemon.go:92`

## f2

Inbound classification uses numeric owner and bot IDs, bot flags and output markers. Free-text owner comments can start a mode-resolved run; slash commands include workflow controls and confirmations. Directly feeding generic chat into this parser could unexpectedly start work. Cross-platform identity and explicit intent admission are required before reuse.

Evidence:

- `internal/turn/classify.go:99`

## f3

Top-level config contains settings, validate, webhook, providers, agents, tiers and tool profiles. Providers contains only GitHub. Strict YAML rejects unknown keys; credentials are indirect runtime references. A communication section requires typed configuration, defaults, validation, template and secret-scan consideration.

Evidence:

- `internal/config/config.go:1`

## f4

Registered repository rows hold forge identity, local path, base branch and worktree settings. They have no messaging bindings. Project routing can reuse stable repository identity, but binding storage and team-versus-project precedence are new decisions; an agent team catalogue is not automatically a chat tenant.

Evidence:

- `internal/store/repos.go:16`

## f5

GitHub webhooks only deliver authenticated wake-up hints. The daemon re-reads authoritative issue state before acting. This handler is not a command inbox and uses GitHub HMAC verification; Discord HTTP interactions require their own signature and admission path.

Evidence:

- `internal/webhook/doc.go:1`

## f6

Doctor Service accepts trusted explicit issue target, instructions and source ID, shares durable CLI/forge history, redacts input, locks conversations, and retains failed attempts. It requires authentic provider isolation evidence before invocation. This is a reusable issue-diagnosis service, not a ready factory-wide chat service.

Evidence:

- `internal/doctor/service.go:33`

## f7

Doctor inbox state is immutable and durable, with deduplication and stop fencing. Its concrete admission requires GitHub issue/comment/actor integers and its source ID has a GitHub prefix. Reuse durability semantics, but avoid fake GitHub IDs for Discord. GitHub doctor reporting also retains redacted multipart replies and reconciles uncertain effects; its transport details need adaptation.

Evidence:

- `internal/store/doctor_inbox.go:73`

## f8

Research continuation consumes authenticated replies allocated to a precise wait/attempt and prevents later stages from reusing approval. Messaging answers and buttons must bind the exact presented review, wait, target and revision; display text or a role name alone cannot grant authority.

Evidence:

- `internal/turn/research_owner_resume.go:23`

## f9

Official Discord documentation supports Gateway or HTTP interaction delivery, with mutually exclusive interaction intake. Gateway avoids a public listener; HTTP requires PING handling and Ed25519 request verification. Gateway adds heartbeat, reconnect/resume and replay handling. Slash commands supply explicit intent; arbitrary message content introduces privileged-intent considerations. Sources checked 2026-10-09: https://docs.discord.com/developers/interactions/overview and https://docs.discord.com/developers/events/gateway.

_No source reference was recorded for this finding._

## f10

Discord interaction callbacks must occur within 3 seconds and tokens last 15 minutes; Gateway callbacks still use HTTP. API limits require honoring bucket/global state and Retry-After. Inference: acknowledge/defer separately from work completion, persist destination and delivery state, and use bot-authorized messages for results beyond token lifetime. Sources checked 2026-10-09: https://docs.discord.com/developers/interactions/receiving-and-responding and https://docs.discord.com/developers/topics/rate-limits.

_No source reference was recorded for this finding._

## f11

Existing meter supports factory, repository and task summaries with mode, coding-agent and model filters; its report distinguishes measured totals, known subtotals, frozen USD estimates and missing coverage.

Evidence:

- `internal/cli/cmd_meter.go:28`
- `internal/report/meter.go:23`

## f12

Owner reply expands the first release to project questions, in-progress information, status notifications, meter reporting and chat-created issues; it settles owner-only access and channel-owned history with minimal factory storage.

_No source reference was recorded for this finding._

## f13

Official Discord message documentation requires history-read permissions and restricts arbitrary message content by intent; shaping uses bot-authored accepted-turn records and explicit commands instead of a passive message reader. Source checked 2026-10-10: https://github.com/discord/discord-api-docs/blob/main/developers/resources/message.mdx

_No source reference was recorded for this finding._
