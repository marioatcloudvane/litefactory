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
