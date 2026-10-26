# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The existing MCP entry point is stdio and requires a live attempt descriptor before handshake. The backing submission client checks private socket identity. It is a private attempt capability, not a general owner connector.

Evidence:

- `internal/cli/cmd_mcp.go:14`
- `internal/submission/client.go:47`
- `internal/submission/broker.go:116`

## f2

The MCP adapter owns bounded newline-delimited streams, connection initialization, cancellation and worker limits. Its Backend interface lists and calls attempt-bound submission tools. Reuse of framing or validation must preserve the separation from owner-facing tool authority.

Evidence:

- `internal/mcpadapter/server.go:16`
- `internal/mcpadapter/server.go:70`
- `docs/guides/mcp.md:9`

## f3

The existing webhook listener binds separately to fail early, serves HTTP, and has explicit shutdown. It uses 3-second header, 5-second read/write and 10-second idle timeouts. Its webhook handler caps bodies at 1 MiB; those values are evidence of webhook hardening, not settled MCP limits.

Evidence:

- `internal/webhook/server.go:13`
- `internal/webhook/server.go:30`
- `internal/webhook/handler.go:21`
- `internal/daemon/webhook.go:310`

## f4

Webhook configuration defaults to loopback 127.0.0.1:8787 and allows disabling the listener. The proxy guide forwards exact webhook and Discord paths through an existing HTTPS server but contains no rate-limit or body-size directives in that example. Actual host configuration was not inspected.

Evidence:

- `internal/config/webhook.go:10`
- `docs/getting-started/github-setup.md:227`

## f5

Owner identity is resolved through local gh auth status and gh api user against github.com. Numeric UserID is authoritative; Login is only a label. This does not implement browser GitHub sign-in or a connector authorization server.

Evidence:

- `internal/identity/owner.go:18`
- `internal/identity/owner.go:26`
- `internal/identity/owner.go:46`

## f6

Configuration rejects unknown YAML keys and embedded credentials; credentials use indirect environment/keychain references resolved at runtime. OAuth settings and secrets must fit that existing contract. The current top-level configuration has no remote MCP/OAuth stanza.

Evidence:

- `internal/config/config.go:3`
- `internal/config/config.go:10`
- `internal/config/config.go:46`

## f7

FactoryStatus reads the persisted health snapshot, including in-flight sessions, worker occupancy, holds and heartbeat age. It treats Running as a snapshot no more than five minutes old. The task reader already bounds project tasks at 100, distinguishes active runs from stale session bookkeeping, and projects owner waits and parked work.

Evidence:

- `internal/messaging/service.go:87`
- `internal/messaging/service.go:112`
- `internal/messaging/ask/store_reader.go:15`
- `internal/messaging/ask/store_reader.go:67`

## f8

The existing owner inbox has transport/conversation authorization and navigation state. Its public chat handler is not a neutral read API to expose directly. Store graph wait/session reads and task projections are reusable sources for the overview.

Evidence:

- `internal/messaging/owner_inbox.go:23`
- `internal/messaging/owner_inbox.go:77`
- `internal/store/graph_instances.go:167`

## f9

Report deliveries distinguish retained intent, possibly-sent effects and confirmed completion, and execution snapshot reads include completed history. Recent delivery must not be inferred merely from a finished agent run.

Evidence:

- `internal/store/report_deliveries.go:10`
- `internal/store/report_deliveries.go:37`
- `internal/store/execution_snapshots.go:322`

## f10

The existing redactor can register connection credentials as literal secrets; logging can reuse protection mechanisms, but new OAuth token/code and request logging paths require explicit treatment.

Evidence:

- `internal/redact/redact.go:329`

## f11

Official MCP authorization documentation describes resource metadata discovery, authorization-server metadata, PKCE and resource-bound tokens. OAuth discovery and sign-in endpoints must be reachable before the connector has an MCP access token; interpret the issue's unauthenticated restriction as protecting MCP operations, not preventing login/discovery.

Evidence:

- `.aiteam/lf-1108-mcp-1-remote-mcp-endpoint-with-owner/2026-10-25/mcp-1-remote-mcp-endpoint-with-owner/protocol-sources.md:5`

## f12

Official Streamable HTTP documentation reports transport behavior changes in revision 2026-07-28, including removal of GET streams and protocol sessions. A transport version and actual client support must be pinned rather than assuming all clients have identical behavior.

Evidence:

- `.aiteam/lf-1108-mcp-1-remote-mcp-endpoint-with-owner/2026-10-25/mcp-1-remote-mcp-endpoint-with-owner/protocol-sources.md:9`
