# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

Current deployment is one Go module and the main litefactory binary, with subcommands for daemon, CLI administration and MCP; teamrelease is a separate build-time command. There is no independently versioned application suite in the inspected go.mod and Makefile.

Evidence:

- `go.mod:1`
- `Makefile:1`
- `cmd/litefactory/main.go:1`
- `cmd/teamrelease/main.go:1`

## F-02

Largest production Go package trees by lines, excluding *_test.go: turn 57,212; store 47,407; daemon 28,154; messaging 13,135; cli 10,413; git 7,251; harness 6,694. These are source-size indicators, not runtime or maintenance measurements. Counted recursively under internal at the inspected commit.

Evidence:

- `internal/turn/doc.go:1`
- `internal/store/store.go:1`
- `internal/daemon/doc.go:1`
- `internal/messaging/service.go:1`

## F-03

Daemon owns discovery, scheduling, concurrency, backoff and recovery. Turn owns task execution, worktrees, workflow actions, result handling and forge publication. They already have an explicit when/what boundary, but daemon imports many domain packages and turn imports both provider implementations, store and workflow tooling.

Evidence:

- `internal/daemon/doc.go:1`
- `internal/daemon/daemon.go:1`
- `internal/turn/doc.go:1`
- `internal/turn/mcp_runtime.go:1`

## F-04

The reusable framework candidate already has vendor-neutral agent types, provider implementations in harness and codex, process support, graph navigation and instruction packaging. However graphexec imports agent, config and planbrief and validates factory-specific graph policies; it is not a clean standalone generic framework today.

Evidence:

- `internal/agent/doc.go:1`
- `internal/harness/doc.go:1`
- `internal/graphexec/doc.go:1`
- `internal/graphexec/plan_control_test.go:1`

## F-05

Agent MCP and remote owner MCP are separate surfaces. mcpadapter is a stdio protocol edge with no SQL or workflow decisions; submission is a local Unix-socket broker whose handler and lifetime are owned by the orchestrator. Turn binds each attempt to authority and durable result handling. This is already a useful transport/service boundary.

Evidence:

- `internal/mcpadapter/doc.go:1`
- `internal/submission/doc.go:1`
- `internal/submission/broker.go:1`
- `internal/turn/mcp_runtime.go:1`
- `internal/cli/cmd_mcp.go:1`

## F-06

Remote MCP connector runs as an optional daemon-owned HTTP server, with its own owner authentication, grants and token lifecycle. Its Options includes shared store plus injected overview/task/usage/code providers. Separating its process requires replacing both local provider calls and direct persistence access, while preserving revocation and owner identity checks.

Evidence:

- `internal/connector/server.go:1`
- `internal/daemon/remote_connector.go:1`

## F-07

Discord has a concrete adapter under messaging/discord and a transport interface in messaging.Service. Messaging also owns conversations and directly uses store; daemon chat files bind it to research answers, diagnoses and other factory operations. Extracting only the adapter is easy conceptually but leaves much of the communication application in daemon.

Evidence:

- `internal/messaging/service.go:1`
- `internal/daemon/chat.go:1`
- `internal/daemon/chat_research.go:1`
- `internal/daemon/chat_gateway_internal_test.go:1`

## F-08

The shared SQLite store is a major coupling point across workflow state, results, chat and connector authorization. Its single-connection policy and transaction semantics are deliberate. Turn documents post-before-record delivery and watermark behavior. Several applications opening the same database would retain schema coupling; splitting database ownership requires explicit command/query and recovery contracts.

Evidence:

- `internal/store/store.go:1`
- `internal/turn/doc.go:1`
- `internal/store/report_delivery_external_test.go:1`

## F-09

Team content is already versioned separately from executable code. Four stack teams inherit base-team workflow graphs, roles and skills. cmd/teamrelease generates releases and catalogue; only the owner releases and links versions. Existing captures must remain immutable. Preserve this content boundary rather than folding team content into a generic engine.

Evidence:

- `teams/README.md:1`
- `cmd/teamrelease/main.go:1`
- `internal/instructionpackage/doc.go:1`

## F-10

Impact: likely extraction touches daemon setup, turn orchestration, shared store/config, CLI and launchd lifecycle, chat, connector, graph/provider contracts, team capture and documentation. Dependents include issue-triggered workflows, owner Discord conversations, agent result clients, remote MCP clients and operator CLI. No behavior is superseded by this research pass and no migration is performed. A later process split must account for existing databases, descriptors, grants, captures and interrupted runs.

Evidence:

- `internal/daemon/daemon.go:1`
- `internal/daemon/remote_connector.go:1`
- `internal/turn/mcp_runtime.go:1`
- `internal/store/store.go:1`
- `teams/README.md:1`
- `internal/cli/cmd_mcp.go:1`

## F-11

Existing safety net includes graph topology tests, MCP CLI/adapter tests, connector authentication/transport tests, daemon gateway tests, and store delivery failure-window tests. Makefile defines full checks and targeted race coverage. This was static research: no test suite or live deployment verification was run, and extraction safety is not established.

Evidence:

- `Makefile:1`
- `internal/graphexec/plan_control_test.go:1`
- `internal/cli/cmd_mcp_test.go:1`
- `internal/daemon/chat_gateway_internal_test.go:1`
- `internal/store/report_delivery_external_test.go:1`

## F-12

Related context: prior Research for #1108 establishes remote connector authentication/transport intent; #984 documents graph-declared gate direction. Current code takes precedence over those briefs. Open #1112 concerns MCP stage steering and open #1249 plans shared MCP/Discord action confirmation; these overlap any new core API and should be coordinated. Recent PR #1115 added Discord Gateway intake. Issue #1251 has no owner clarifications.

Evidence:

- `.aiteam/lf-1108-mcp-1-remote-mcp-endpoint-with-owner/2026-10-25/mcp-1-remote-mcp-endpoint-with-owner/RESEARCH_BRIEF.md:1`
- `.aiteam/lf-984-graph-topology-still-decided-in-go/2026-10-19/graph-topology-still-decided-in-go/RESEARCH_BRIEF.md:1`
- `internal/daemon/chat_gateway_internal_test.go:1`

## F-13

At shaping inspection commit 436cb605fa217839e7bab176a3861dc5946dcaf0, daemon documentation separates scheduling from turn execution, including restart reconciliation and delivery recovery; turn documentation specifies task locking, worktree isolation and post-before-record delivery; MCP composition binds durable result/receipt services and an attempt-scoped broker; graphexec documents navigation without execution or persistence. These support the proposed six-responsibility core view, but do not establish that these are isolated modules today; no runtime tests were run.

Evidence:

- `internal/daemon/doc.go:1`
- `internal/turn/doc.go:1`
- `internal/turn/mcp_runtime.go:329`
- `internal/turn/mcp_runtime.go:425`
- `internal/graphexec/doc.go:1`
- `internal/submission/doc.go:1`

## F-14

Ticketing inspected at 41cfc1222a7f2644a59e6d8c4f2ccf0eb0ce4cd1: provider-neutral ticket values qualify identities by provider, canonical API origin and immutable remote ID; projectconnection.TicketClient exposes common ticket operations implemented by GitHub Tickets and a wrapper embedding Linear Client. This is Go interfaces/composition, not Linear inheriting from GitHub; provider dispatch remains explicit in the client factory.

Evidence:

- `internal/ticket/doc.go:1`
- `internal/ticket/identity.go:68`
- `internal/projectconnection/tickets.go:17`
- `internal/projectconnection/factory.go:123`
- `internal/projectconnection/linear.go:60`
- `internal/github/tickets.go:19`

## F-15

Projects retain distinct ticket and code connection roles; ticket providers may be GitHub or Linear, while store validation restricts code connections to GitHub. Admission verifies both destinations and saves an execution snapshot; routing reuses its ticket connection version and actor, rejecting ambiguous context. Linear credentials do not substitute for code-host credentials. This extends the earlier impact analysis to projectconnection, ticket, linear, connection versions and project approver authority.

Evidence:

- `internal/store/project_connections.go:106`
- `internal/daemon/project_connections.go:20`
- `internal/daemon/project_connections.go:169`
- `internal/projectconnection/factory.go:47`
- `internal/projectconnection/linear.go:75`

## F-16

Ticket support is integrated beyond notifications: shared contracts cover discovery, reads, comments, creation/content edits, parent/child links, lifecycle state, labels, subscriptions and recovery pages; Linear implements native GraphQL operations, content normalization and configured mode/state mapping. Recovery and verified webhook hints feed shared daemon admission; no claim of complete provider feature parity or live deployment health is established.

Evidence:

- `internal/projectconnection/tickets.go:17`
- `internal/linear/tickets.go:93`
- `internal/linear/recovery.go:91`
- `internal/daemon/ticket_recovery.go:77`
- `internal/daemon/webhook_linear.go:23`
- `internal/webhook/linear.go:35`

## F-17

The abstraction remains partial: daemon and turn compatibility paths use github.Issue, github.Comment and github.Repo values, translating Linear remote IDs to database-backed local numeric slots while retaining qualified snapshots/replies; WorkflowTickets and routed bridges keep code delivery separate. Daemon setup still loads global GitHub identities/client and a GitHub default bridge before per-project routing, so application startup is not GitHub-independent. Earlier GitHub-only intake diagrams were incomplete and are superseded by this provider-aware view, without superseding their scheduling/recovery findings.

Evidence:

- `internal/turn/ticket_compatibility.go:14`
- `internal/turn/ticket_compatibility.go:104`
- `internal/daemon/linear_compat.go:32`
- `internal/turn/ticket_workflow_bridge.go:17`
- `internal/daemon/setup.go:260`
- `internal/daemon/setup.go:286`

## F-18

Linear-backed code delivery preserves separate retained GitHub code authority: CLI requests verify the retained human code owner and the daemon polls only bound GitHub delivery PR controls, while ticket replies do not enter that control path; do not equate ticket approval with code-delivery approval during extraction.

Evidence:

- `internal/daemon/linear_delivery.go:31`
- `internal/daemon/linear_delivery.go:94`
- `internal/turn/linear_delivery.go:58`

## F-19

Executed successfully during this shaping pass: go test ./internal/ticket ./internal/projectconnection ./internal/github ./internal/linear; go test ./internal/daemon -run '^(TestLinearTicketWorkflow|TestGraphDispatchObservesLinearSessionAtItsTicketContainer|TestLinearPlanShellDiscoveryAndManagedReview)$' -count=1. These package and controlled-server integration checks support adapter behavior, Linear workflow wiring, retained code/ticket separation, managed-story discovery and graph dispatch; they do not verify live service credentials/configuration, every workflow or the full suite, and do not establish extraction safety.

Evidence:

- `internal/github/tickets_test.go:37`
- `internal/projectconnection/factory_test.go:29`
- `internal/daemon/linear_workflow_test.go:265`
- `internal/daemon/graph_dispatch_linear_test.go:15`
- `internal/daemon/linear_plan_story_test.go:21`
