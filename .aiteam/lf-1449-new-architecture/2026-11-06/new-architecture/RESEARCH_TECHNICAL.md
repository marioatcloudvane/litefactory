# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Parent brainstorm #1251 split the app into four stories: execution library (#1268), factory core plus CLI (#1269), Discord separation (#1270), remote owner MCP separation (#1271). All four are closed. Stated assumption: one repo, one Go module, coordinated releases; separate processes, multi-host and independent versioning are follow-up scope.

Evidence:

- `docs/factory-core.md:1`

## f2

pkg/execution holds graph (parse, validate, registry), agent (contracts, usage, tokens) and Claude and Codex providers plus an internal process package, about 5.1k production lines. A demo consumer under examples/execution runs a small non-factory workflow and has its own dependency-boundary tests.

Evidence:

- `pkg/execution/graph/graph.go`
- `examples/execution/main.go:1`
- `examples/execution/boundary_test.go:18`

## f3

Factory core is organised as internal/factory with a contracts package and focused services: health, localadmin, localprojects, localrights, localteams, localstandards, localusage, localdoctor, localanalysis, maintenance, owneractions, featuredraft, researchanswers, retainedinbox, conversationdiagnosis, readtargets, ticketbridge, runtime. Production size is about 11.4k lines, versus none before the extraction.

Evidence:

- `docs/factory-core.md:10`
- `internal/factory/boundary_test.go:16`

## f4

Discord now lives in internal/discordcomponent. It owns token handling, HTTP and Gateway intake, and conversation and delivery workers. It reaches storage only through messagingstorage.Port and factory projections. A boundary test checks it cannot reach store, daemon or live execution, and a recovery rehearsal script covers upgrade and rollback.

Evidence:

- `internal/discordcomponent/boundary_test.go:21`
- `scripts/discord-component-recovery.py`
- `internal/messagingstorage/storage.go`

## f5

The remote MCP connector uses connectoraccess for credential, scope and repository checks, and connectorstorage as the one trusted binding of persistence and the core reader. Remote reads go through factory/readtargets. A boundary test bars the store, workers and execution from the connector.

Evidence:

- `internal/connectoraccess/access.go`
- `internal/connectorstorage/persistence.go`
- `internal/connector/boundary_test.go:13`
- `docs/factory-core.md:120`

## f6

Residual coupling: the messaging package still imports internal/store in 18 production files, and the connector in 4 (internal/connector/oauth.go, brainstorm.go, task_advisory.go, stage_decisions.go). The messaging test allows only store error identity and context fences. The factory services import store 60 times and turn 5 times, which is expected for the core. The boundaries are test-enforced conventions, not module or process separation.

Evidence:

- `internal/messaging/boundary_test.go:13`
- `internal/messaging/service.go:252`

## f7

Size change from commit 436cb605 to HEAD, production Go lines: daemon 28.2k to 30.1k, turn 57.2k to 61.9k, messaging 13.1k to 12.5k, connector 3.6k to 6.3k, cli 10.4k to 9.0k, graphexec 3.6k to 4.1k, agent 4.0k to 4.2k. The extraction added structure and did not shrink the codebase. The two largest packages are turn (about 62k) and store (about 56k).

Evidence:

- `internal/turn/doc.go`

## f8

Compatibility: docs state no schema, migration, capture-digest, grant-generation or pending-delivery encoding changes. Compound store transactions stay as they were. Live upgrade and recovery on a deployed daemon is described as the owner's check at deploy. Superseded in part by f10: the package tests have now been run.

Evidence:

- `docs/factory-core.md:88`

## f9

Impact analysis. Touched: pkg/execution, internal/factory/*, internal/discordcomponent, internal/connector*, internal/messaging*, internal/cli, docs/factory-core.md and scripts. Depends on it: daemon composition, the CLI, the closed #1268-#1271 stories. Superseded: direct store and daemon paths in Discord, connector and CLI (stated as retired). Data and compatibility: none found for schema changes, per the docs. Team releases: none found in this area. For this request itself (a report), nothing in the repository is touched; the owner chose an issue comment only.

_No source reference was recorded for this finding._

## f10

At commit c05ebd29, go test passed for pkg/execution/..., examples/execution/..., internal/factory/..., internal/discordcomponent/..., internal/connector/... and internal/messaging/..., including all boundary tests. The full repository suite and a live daemon upgrade were not run.

Evidence:

- `internal/factory/boundary_test.go:16`
- `internal/discordcomponent/boundary_test.go:21`
- `internal/connector/boundary_test.go:13`
- `examples/execution/boundary_test.go:18`

## f11

Thin coverage: 12 of the 18 internal/factory service packages have no test files of their own (conversationdiagnosis, localadmin, localanalysis, localdoctor, localrights, localstandards, localteams, localusage, maintenance, readtargets, researchanswers, ticketbridge); the contracts package, types only, has none either. They are covered only by the shared factory boundary test and by callers' tests.

Evidence:

- `internal/factory/boundary_test.go:16`

## f12

Package directories at 436cb605 had no pkg/execution, internal/factory, internal/discordcomponent, internal/connectoraccess, internal/connectorstorage or internal/messagingstorage; graph execution, agent and provider process code lived under internal/graphexec, internal/agent and internal/providerprocess. All of these new boundary packages exist at c05ebd29. The before diagram is based on this listing and on the docs naming the direct store and daemon paths that were retired.

Evidence:

- `docs/factory-core.md:1`
