# Implement run 0006

Workflow version 2. Frozen scope is the factory-provided US-001 through US-005 revisions; prior Plan artifacts provide context only. Implement owns engineering and executed checks. Validate remains separate.

Execution order: US-001, US-002, US-003, US-004, US-005. US-004 depends on US-001 and US-002; US-005 depends on US-003 and US-004. All stories require the landed #263/#264 production lifecycle prerequisite.

The implementation engineer is checking that prerequisite before any MCP code. No provider host wiring or tool-family publication belongs to this run. No source checks have run yet. No questions or approval waits are planned; missing prerequisite implementation is outside authorized scope.

## Prerequisite result

US-001 is blocked. The engineer reports internal/turn/agent_results.go:15 deliberately excludes runner wiring, BindAgentResultService has no production callers, internal/turn/submission_runner.go:46 still starts StartSubmissionSession, and docs/agent-result-contract.md:217 describes the result service as unwired. See US-001-IMPLEMENTATION.md.

The delivered shared communication contract is present, but its required production lifecycle adoption is absent. No source code or tests were changed. No tests were run. Five blocked outcomes were durably submitted; receipts report valid input, rejected engineering, not-applicable acceptance and delivered blocked-status effects. See IMPLEMENT_HANDOFF.json for correlation.

Required next condition: bring completed #264 adoption into this worktree, then resume the authorized dependency order. No clarification or approval request is needed for this run; the prerequisite is an explicit frozen requirement.

## Implement run 0007 — owner dependency correction

The owner explicitly corrects prior prerequisite wording: #272 precedes #264. The delivered #263 JSON communication groundwork is the only external implementation prerequisite. This run builds the reusable adapter, live binding, profiles, grants, neutral registration producer and recovery primitives against that groundwork. Production lifecycle adoption is deferred to #264; native host consumption remains #274 and actual creation families remain #273. Old blocked history is preserved as history, not current authority. Frozen story scope otherwise remains selected unchanged.

Execution order: US-001, US-002, US-003 and US-004 (serializing overlapping writers), US-005, aggregate checks. Implement owns engineering and tests only; no acceptance review or issue closure.

### Run 0007 US-001 checkpoint

Production and real temporary SQLite/private-socket authority tests passed; race coverage passed. Factory request `0f0e50a2c2f89ca5ee9fdf2f35a48fb3`: input valid, engineering recorded, acceptance not_applicable, effects pending. Snapshot `run0007-us001-v1`, digest `54a07b467e97c296baf582cdda234b717a5a5cd24be34abdd5ae662222e9012e`. Unit specialist found no meaningful separate pure seam because concrete store state is required; real dependency tests establish behavior. Discovery currently exercises the live guard; actual MCP list/call follows US002/US003. Production launch and host integration remain explicitly outside this run.

### US-002 safe deferred-effect route

Before #264 adopts final publication wiring, MCP refuses valid completed Plan/proposal-story routes after invoking the existing shared validator; safe discussion communication remains available. This uses the frozen story’s explicit unsafe-route refusal alternative. Existing terminal/helper Plan publication policy stays unchanged. Tests must cover retained/recovered proposal scenarios as well as new input, so no MCP-origin proposal is admitted for later unguarded creation.

### Run 0007 US-002 checkpoint

Catalog/profile unit checks and six real SQLite/socket test functions passed, including race execution. Exact bytes and call counts, all mode dispatch, receipts/replay, live authority/selector denial, Plan publication/recovery refusal and Validate communication covered. Factory request `1c603ccb194f792a9bb0e5594b77b1bc` records valid input and engineering; acceptance not_applicable and remote effects pending. Snapshot run0007-us002-v1. US003 may extend private transport files, requiring fresh aggregate evidence for affected snapshots later.

### Run 0007 US-003 checkpoint

Bounded stdio protocol, explicit CLI startup, strict true-default auto-start configuration and neutral immutable launch producer implemented. Fourteen unit test functions passed with race detection; five real CLI/pipe/socket/SQLite integration functions and fifteen subtests passed normally and with race detection. A discovered NUL-path acceptance defect was fixed and regression-tested. Factory request `f1aadd8cf88a923f4b3d2b5409764295` records input valid and engineering recorded, acceptance not_applicable, effects pending. Snapshot run0007-us003-v1. Existing private socket frame ceiling expanded to 16MiB to fit full bounded shared receipts; adapter output ceiling 64MiB accounts for structured plus compatible text representation. Actual host consumption remains unavailable until #274.

### Run 0007 US-004 checkpoint

Creation intent/grant/operation primitive and strict false-default gate implemented. Eight unit functions and nine real SQLite/intake test functions with all subcases pass, including race checks. Tests cover immutable owner approval, exact TTL/clamp, next launch, gate/mode/foreign/replaced expiry refusals, concurrent single dispatch, quota/rolling unresolved charges, release rules and physical database reopen. Factory request `7427cd8c8c8842c77a2d2744bd1da5f6` records engineering, input valid, acceptance not_applicable, effects pending. Snapshot run0007-us004-v1. Migration0043 adds existing lease binding timestamp plus private creation records. Production control/launch wiring stays #264-owned; no production creation handler or external publication added.

### Aggregate fix history

First make check stopped at 46 lint findings; exported comments, naming, equivalent switch control flow and test serialization assertions were corrected without exclusions. Default linter cache was outside writable roots; 2848 repetitive cache warnings are summarized in the retained log. Subsequent checks use GOLANGCI_LINT_CACHE under /private/tmp with the same pinned v2.13.1 linter.

First aggregate retry passed lint and all reported packages except turn. TestTurnImposesNoAmbientDeadline rejected WithDeadline in AttemptAuthority.Bind, and the full turn race package later exceeded the repository target’s existing 20-minute timeout. Its cause remains uncertain; stack traces mostly showed original tests and parallel-slot waits, not evidence of a specific MCP deadlock. No timeout was raised. US001 and US005 engineering were explicitly invalidated while this finding was corrected; receipt IDs are retained in AGGREGATE_INVALIDATIONS_RUN0007.json.

Second fix cycle changes turn authority to derive WithCancel from its caller. Immutable lease expiry remains checked in store before each operation/admission; explicit close/replacement/revocation and caller cancellation retain context cancellation. The architecture gate, authority/recovery race suite and narrow exact-expiry/synchronized-admission race regressions all passed with timeout8m. Final build and gofmt -l pass. Final make check is running with GOFLAGS=-p=1 to serialize packages, retaining every test and the existing target timeout; this changes test execution scheduling only.

### Run 0007 complete

The final serialized-package make check exited0; all reported packages passed, including turn1166.347s. Build and gofmt -l also pass. Five final 44-file captures were submitted and read once: every receipt reports valid input, recorded engineering, acceptance not_applicable, effects delivered. Final refresh explicitly invalidated stale unaccepted snapshots before replacement. No further test or source work remains in Implement. Save/commit/push and any later Validate remain separate factory/owner actions.
