# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Work survived. HEAD 2c10f40 contains SPIKE.md, the discovery budget edit and its regression test. The initial worktree was clean. observeLocked now accepts earlier future resets, takes minimum counts, adopts the earlier reset and refreshes observedAt; it still rejects resets at or before now. The test exercises freshness recovery after three minutes and expired-window rejection.

Evidence:

- `internal/daemon/discovery_budget.go:95`
- `internal/daemon/discovery_budget_internal_test.go:249`
- `.aiteam/lf-435-discovery-stalls-for-up-to-an-hour-after/2026-09-30/discovery-stalls-for-up-to-an-hour-after/SPIKE.md:27`

## f2

Historical first Spike instance 165 succeeded as a workflow with fix_unverified. Investigator run 1 took 42.492 seconds and fixer run 2 took 38.840 seconds. Fixer exited at 19:07:28Z; workflow completed at 19:55:46Z. Retained checks show go vet and golangci-lint passed; go test -race -timeout=0 ./... failed with exit 1. Daemon package passed in 521.290s; turn package passed in 2813.259s. Thus long checks explain most of the apparent first-run stall, not a continuously running editing agent.

Evidence:

- `.aiteam/lf-435-discovery-stalls-for-up-to-an-hour-after/2026-09-30/discovery-stalls-for-up-to-an-hour-after/spike-failure-evidence.json:1`
- `.litefactory/bug-checks.yaml:9`
- `internal/turn/spike_check.go:264`

## f3

Failure evidence is lossy by design: spike checks retain only the last 2048 bytes of command output. The real run's retained tail is marked output_truncated and contains passing package summaries followed by FAIL, without the failed test/package detail. renderSpikeCheck exposes only phase names and generic outcome, not command output. Full-suite root cause therefore remains unresolved from this retained record.

Evidence:

- `internal/turn/spike_check.go:23`
- `internal/turn/spike_check.go:287`
- `internal/turn/spike_workflow.go:258`
- `.aiteam/lf-435-discovery-stalls-for-up-to-an-hour-after/2026-09-30/discovery-stalls-for-up-to-an-hour-after/spike-failure-evidence.json:1`

## f4

Continuation root cause: researchWorkflow preparation passes the incoming route.scope to predecessorContext. That function scans the previous instance's successful agent attempts newest first and loads the retained fixer output with that same scope. Spike starts read-only, while validateSpikeFixReport forbids any fix_report when WriteAccess is false. This conflates historical evidence validation with current authoring permission. The actual instance 170 failed at probe_agent with this exact diagnostic; run 3 has no agent start time, duration 0 and no result text. The misleading 'prepare research invocation' prefix belongs to the generic shared activity and does not establish that the failed attempt changed to Research.

Evidence:

- `internal/turn/research_workflow.go:233`
- `internal/turn/research_workflow.go:257`
- `internal/turn/workflow_policy.go:128`
- `internal/turn/spike_workflow.go:45`
- `internal/agentresult/validate.go:380`
- `internal/turn/research_agent.go:108`
- `.aiteam/lf-435-discovery-stalls-for-up-to-an-hour-after/2026-09-30/discovery-stalls-for-up-to-an-hour-after/spike-failure-evidence.json:1`

## f5

Existing boundaries to preserve: Spike grants write access only for its write node; results are per node, and historical findings are explicitly supplied as untrusted data. A repair should distinguish source authority for historical reports from permissions for newly authored reports. Merely granting the investigator write access would bypass the intended boundary. Existing shared workflow loader, Spike result validator, check recorder and renderer are the affected owners.

Evidence:

- `internal/turn/mcp_runtime.go:188`
- `internal/turn/spike_workflow.go:59`
- `internal/turn/research_workflow.go:278`
- `internal/agentresult/validate.go:380`

## f6

Reporting mixes temporal states: the preserved fixer narrative says 'Nothing was run; the factory checks will do that', whereas the generated Spike note records focused/full checks and failure. The daemon log records commit-and-push of 2c10f40 at 2026-09-30T21:55:50+02:00. This is historical logged remote success, not fresh remote verification. Current Research makes no engineering acceptance or publication claim.

Evidence:

- `.aiteam/lf-435-discovery-stalls-for-up-to-an-hour-after/2026-09-30/discovery-stalls-for-up-to-an-hour-after/spike-failure-evidence.json:1`
- `.aiteam/lf-435-discovery-stalls-for-up-to-an-hour-after/2026-09-30/discovery-stalls-for-up-to-an-hour-after/SPIKE.md:37`
- `internal/turn/spike_workflow.go:258`
