# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

At revision 21b40106fdbc51bbf5fade401067e98b1c76c8f9, successful approval calls AdmitCreationApproval then Consume and returns without reporting or launching work. Recovery likewise only consumes. Consume explicitly posts nothing. The restart test asserts zero posts.

Evidence:

- `internal/daemon/mcp_creation.go:60`
- `internal/daemon/mcp_creation.go:94`
- `internal/turn/report.go:669`
- `internal/daemon/creation_success_io_test.go:110`

## f2

Approval lifetime remains 30 minutes from trusted comment creation, not intake time. Admission and grant binding enforce expiry. The proposal reply still describes approval for the next Research attempt without deadline, acceptance acknowledgment, or launch instructions. A fresh approval can reference the retained proposal provided it remains current and eligible; replaying the old comment does not renew authority.

Evidence:

- `internal/store/mcp_creation.go:15`
- `internal/store/mcp_creation.go:153`
- `internal/store/mcp_creation.go:198`
- `internal/store/mcp_creation.go:220`
- `internal/report/reply.go:747`

## f3

Rejections now use durable reporting with bounded reason categories and correction guidance. Malformed-command queue tests verify delivery, consumption, no granted intent, and subsequent work progressing. Transient infrastructure errors still remain pending rather than becoming policy rejections; delivery outages are not claimed fixed.

Evidence:

- `internal/daemon/mcp_creation.go:68`
- `internal/report/creation_rejection.go:6`
- `internal/daemon/creation_rejection_queue_test.go:45`

## f4

The newer issues_create_prepare tool is installed for both legacy and graph Research. Retained proposals receive a factory presentation that reuses the existing proposal renderer. This improves drafting/presentation but does not replace the silent approval path. Existing durable delivery and recovery mechanisms are relevant reusable infrastructure for an eventual acknowledgment.

Evidence:

- `internal/turn/mcp_runtime.go:323`
- `internal/turn/creation_presentation.go:31`
- `internal/turn/creation_presentation.go:97`
- `internal/daemon/creation_presentation.go:83`
- `internal/daemon/creation_presentation_test.go:195`

## f5

The next-attempt claim must distinguish invocation types. MCP runtime excludes graph Research nodes from creation publication and grant binding; a test explicitly proves a graph launch retains the eligible intent and lacks issues_create_batch, while a legacy Research launch consumes it and receives the tool. Approval commands return before normal comment execution. Therefore the original promise that another ordinary Research comment will publish is not universally supported by current code.

Evidence:

- `internal/turn/mcp_runtime.go:305`
- `internal/turn/mcp_runtime.go:350`
- `internal/turn/creation_proposal_catalog_test.go:181`
- `internal/daemon/tick.go:1616`

## f6

Verification on 2026-09-23, darwin/arm64, Go 1.27.0, revision 21b40106fdbc51bbf5fade401067e98b1c76c8f9: go test ./internal/daemon ./internal/turn ./internal/store -run 'TestCreationSuccessIO_ApprovalConsumptionRestart|TestCreationRejectionQueueIO_|TestCreationPresentationGraphRunPostsOnce|TestMCPCreationDB_ExpiryScopeAndNextLaunch|TestCreationProposalCatalog' -count=1 passed in all three packages. These are offline fixture tests, not live host observations. No source changes were made.

Evidence:

- `internal/daemon/creation_success_io_test.go:110`
- `internal/daemon/creation_rejection_queue_test.go:12`
- `internal/daemon/creation_presentation_test.go:195`
- `internal/store/mcp_creation_test.go:128`
- `internal/turn/creation_proposal_catalog_test.go:181`
