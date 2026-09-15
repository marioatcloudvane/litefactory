# Engineering scope report — run 0005

Status: PARTIAL — selected cross-host obligations remain blocked. This is engineering scope accounting, not acceptance/code/architecture review.

## Delivered as planned

US-001: neutral copied installed catalog, startup-owned runtime authority, per-actual-launch services/broker/cleanup, unmanaged Research/Spike access, disabled/unsupported-provider preservation.

US-002: private structured Claude MCP configuration, exact additive authorization, retained native ceilings/strict isolation, cleanup and scoped provider tests.

US-004: dynamic installed-operation cards, one trusted relevant payload example, receipt/retry/final-output semantics, measured guidance sizes and explicit unavailable provider token telemetry.

US-005 T-001 and available T-002 scope: durable terminal/live Validate reconciliation; real temporary store/broker lifecycle tests using fake hosts.

US-006 T-001: four-scenario external capture handoff, assertion/provenance/sanitization requirements and explicit missing evidence.

## Deferred or blocked

US-003 was expressly deferred by the owner. Codex keeps prior behavior without MCP registration or instructions. This is not a waiver of US-005's cross-host coverage or US-006's Codex captures. US-005 is partial until the deferred dependency and remaining cross-host checks are supplied.

US-006 T-002/T-003 and AC-009–AC-011 are blocked by absent authentic captures from all four current-implementation scenarios. No synthetic capture substitutes, live host acquisition, acceptance assessments or issue closure were performed.

## Scope decisions

Independent US-005 reconciliation and US-006 handoff were completed while their deferred prerequisites remain visible. Existing installed grants are reused; no new comments/creation grants, scheduling, schema, broad #264 workflow changes or protected instruction edits were introduced. Final aggregate gates and snapshot attribution are recorded in TEST_REPORT.md.

## Run 0006 update

Status: US-003 delivered as planned; US-005's previously-blocked cross-host aggregate coverage is now complete.

### Delivered as planned

US-003: internal/codex/mcp.go encodes structured command/args/minimal-env/exact-enabled_tools mcp_servers overrides from the same neutral MCPLaunchSpec registration US-001/US-002 already established, inserted at the exact arg-assembly position the story specified (before resume subcommand/stdin marker), with collision refusal against an existing operator config entry and no persistent config writes — matching the plan's "Provider-owned config serialization consumes neutral immutable registration alongside existing args assembly" architecture exactly. No shell wrapper, no new dependency, no widened broker root. Reviewed ADVISORY_ONLY (three narrow, non-blocking findings, none requiring rework this run).

US-005 T-002/T-003: the cross-host Codex coverage and aggregate `make check` gate that were explicitly blocked by US-003's deferral in run 0005 are now both complete. No architecture change was needed — the existing SubmissionSession reconciliation owner and production-seam lifecycle pattern (real temp SQLite + real broker + fake host subprocess) already anticipated a Codex host and only needed the Codex-side fake wired in, which was done by extending the existing harness/probe helpers rather than inventing a new mechanism.

### Added scope (why)

The engineer running T-003 partitioned the aggregate `go test -race` gate into 21 bounded, sequential invocations (package bundles, plus internal/daemon and internal/turn split by test-name-prefix) instead of the single whole-repo invocation `make check` nominally implies. This was not in the original plan but was required to resolve run 0005's explicit blocker (outer-wrapper and per-package 8-minute timeouts) without lowering rigor, raising any timeout, or skipping any package/test — every test function is still exercised under `-race` with the same `-timeout 8m` bound; only the number of Bash calls used to observe that evidence changed. This is call-shape bounding, not a scope or architecture deviation.

### Dropped or deferred

US-006 (authentic external host captures) remains untouched, per explicit owner instruction for this run — it depends on external operator-supplied captures, not something attemptable in this run. This is not a new deferral; it was already the known state entering this run and is unchanged by it.

### Architectural deviations

None. All US-003 work stayed inside internal/codex as the plan specified; all US-005 work stayed inside internal/turn's existing SubmissionSession/lifecycle test pattern.

### Verdict: CLEAN

No divergence from the approved plan beyond the necessary, rigor-preserving call-shape bounding of the aggregate test gate (documented above), which resolves rather than introduces drift. No follow-up planning cycle is warranted for US-003/US-005. US-006 remains the one open item in the Story Index, already known and explicitly out of scope for this run; it requires externally supplied captures, not further engineering, before it can proceed.
