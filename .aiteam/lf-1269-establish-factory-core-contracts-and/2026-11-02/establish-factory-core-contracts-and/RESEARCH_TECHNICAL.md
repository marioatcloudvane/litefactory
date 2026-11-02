# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

The reusable execution dependency has landed: pkg/execution owns neutral graph/agent/provider mechanics, while factory policy, action bindings, approvals, attempts and durable dispatch remain factory responsibilities. The documented rollback uses unchanged state. #1268 is closed and PR #1328 merged; the earlier research explicitly assigns CLI/core contracts and ticket cleanup to #1269.

Evidence:

- `docs/execution.md:1`
- `.aiteam/lf-1268-extract-reusable-graph-and-agent/2026-10-31/extract-reusable-graph-and-agent/RESEARCH_BRIEF.md:1`

## F-02

Current core responsibilities span daemon and turn rather than one application facade. Turn owns comment execution, cross-process task locks, worktree preparation, harness execution and result harvesting; daemon owns scheduling, in-flight worker coordination and deferred retries. Clone gates and transactional report/watermark recording are existing invariants, not generic execution-library features.

Evidence:

- `internal/turn/doc.go:1`
- `internal/daemon/daemon.go:1`
- `internal/daemon/schedules.go:1`

## F-03

CLI is a broad application consumer and composition root, not only argument parsing. Its single Commands registration includes configuration, standards, teams, submission/MCP helpers, repository connections, daemon lifecycle, recovery, cleanup, rights and metering. Task clean performs DB lookup, Git mutation and state update inside the handler. Daemon run opens storage and composes the daemon. Local lifecycle/install/config operations cannot all become daemon RPC calls without changing behavior.

Evidence:

- `internal/cli/commands.go:1`
- `internal/cli/cmd_task.go:1`
- `internal/cli/cmd_daemon.go:1`
- `internal/cli/cmd_init.go:1`

## F-04

Ticket primitives already qualify identity by provider, trusted API origin and immutable remote ID; descriptive locators confer no authority. Projectconnection constructs isolated clients by immutable connection version and verified publisher, with separate Tickets and Code surfaces and request admission callbacks. Reuse these foundations rather than inventing a new identity or ticket service.

Evidence:

- `internal/ticket/doc.go:1`
- `internal/ticket/identity.go:1`
- `internal/projectconnection/factory.go:1`

## F-05

The existing TicketClient is broad and still accepts github.Comment in QualifyReply. Daemon discovery/read paths convert qualified snapshots/replies back into GitHub-shaped workflow values, including Linear local slots and retained session binding. Legacy forge fallback also exists. Necessary cleanup therefore touches shared interfaces plus bridges; removing all legacy representation would exceed the stated narrow cleanup.

Evidence:

- `internal/projectconnection/tickets.go:1`
- `internal/daemon/ticket_reads.go:1`
- `internal/daemon/ticket_ingress.go:1`

## F-06

Discord already has neutral Message, StatusReader and Transport values, but messaging.FactoryStatus directly maps store health rows, and daemon chat wiring owns adapter composition and action integration. Connector construction also accepts *store.DB. These are concrete existing direct paths the core contracts must replace or place behind owned implementations; adapter extraction itself belongs to sibling issues #1270/#1271.

Evidence:

- `internal/messaging/service.go:1`
- `internal/daemon/chat.go:1`
- `internal/connector/server.go:1`
- `internal/daemon/remote_connector.go:1`

## F-07

The current connector exceeds the sibling issue's read-only description: it advertises repository actions, standards management, private advisory execution and discussion publication scopes. Action tools include propose/confirm/cancel/inspect/history, and daemon wires a shared owneraction service with Discord approvals. Core contracts limited to reads would not represent current consumers. This is observed scope drift, not permission to add capabilities.

Evidence:

- `internal/connector/server.go:1`
- `internal/connector/actions.go:1`
- `internal/daemon/owner_actions.go:1`

## F-08

Attempt-scoped MCP is a distinct authority path: stdio adapter owns protocol, while submission broker owns factory-supplied method/story scope and private live socket capability. Client validates descriptor/socket identity and journals correlation before transmission for lost-reply recovery. CLI submission and mcp serve use this descriptor; remote connector explicitly excludes agent/submission backends.

Evidence:

- `internal/mcpadapter/doc.go:1`
- `internal/submission/broker.go:1`
- `internal/submission/client.go:1`
- `internal/cli/cmd_submission.go:1`
- `internal/cli/cmd_mcp.go:1`
- `internal/connector/server.go:1`

## F-09

Disabling optional transports is supported structurally today: prepareChat returns nil when disabled and unresolved chat credentials leave only chat unavailable; prepareRemoteConnector returns nil when disabled. This establishes existing startup branches, not proof that every core workflow has been exercised with both disabled.

Evidence:

- `internal/daemon/chat.go:1`
- `internal/daemon/remote_connector.go:1`

## F-10

Impact/data: store currently contains workflow schedules and deliveries, conversations and conversation deliveries, connector authorization and connector actions in the same package. Logical state ownership can be separated without forcing new databases. The schema, durable workflow serializers, connection versions, authorization generations and pending deliveries are compatibility-sensitive; this pass did not establish a necessary conversion.

Evidence:

- `internal/store/schedules.go:1`
- `internal/store/delivery.go:1`
- `internal/store/conversations.go:1`
- `internal/store/conversation_delivery.go:1`
- `internal/store/connector.go:1`
- `internal/store/connector_actions.go:1`

## F-11

Existing safety net includes task clean dirty/no-op/missing-path tests, immutable connection/approver tests, Linear code-delivery authority separation, interrupted dispatch recovery, report recovery, broker scope/correlation checks and connector grant/revocation checks. These are available characterization assets, not a complete acceptance matrix for the new boundary. New checks will be needed for dependency restrictions, service contract behavior and a combined disabled-adapter core path.

Evidence:

- `internal/cli/cmd_task_test.go:1`
- `internal/projectconnection/approver_test.go:1`
- `internal/daemon/linear_delivery_request_test.go:1`
- `internal/daemon/daemon_test.go:1`
- `internal/daemon/report_recovery_test.go:1`
- `internal/submission/broker_test.go:1`
- `internal/connector/connector_security_test.go:1`

## F-12

Executed go test ./internal/ticket ./internal/projectconnection ./internal/submission ./examples/execution: all four packages passed. Repository CI runs make check (fmt, vet, lint, tests); full check was not executed in this research pass. No code changes were made.

Evidence:

- `.github/workflows/check.yml:1`
- `Makefile:1`

## F-13

Superseded/dependents: direct CLI domain logic, daemon chat factory bindings and adapter factory-table access are the replacement candidates. Existing CLI commands, ticket workflows, Discord service, remote connector and attempt helpers depend on these behaviors. Preserve their current paths until each replacement is integrated; do not leave duplicate policy owners afterward. Documentation affected includes execution, MCP/remote connector, scheduling and operator usage. No team graph/skill changes were found necessary from the inspected boundary; immutable capture semantics remain a core dependency.

Evidence:

- `internal/cli/commands.go:1`
- `internal/daemon/owner_actions.go:1`
- `internal/messaging/service.go:1`
- `internal/connector/server.go:1`
- `docs/execution.md:1`
- `docs/guides/remote-connector.md:1`
- `docs/guides/scheduling.md:1`
