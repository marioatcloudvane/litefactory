# Run 0007 — US-006 engineering evidence readiness

Date: 2026-09-16. Mode: Implement. Frozen story: US-006 / #296, revision
`b2f580dd26b9e0a57baabcaadf03056358d92c2f538886296f2c8e66dcf75f45`.
Dependencies: US-002, US-003, US-004, US-005. This is an engineering prerequisite
inventory, not an acceptance/code/architecture review or an issue closure.

## Result

US-006 remains **blocked**: no authentic external MCP capture for any of the four
required scenarios was supplied or found in the inspected evidence locations.
The Codex implementation prerequisite is now present, but source and automated
checks cannot supply the missing host observations. T-001's capture handoff
already exists; T-002 cannot integrate absent captures, and T-003 cannot replay
them. No acceptance box can be supported by this inventory alone.

## What is now present

- `internal/codex/mcp.go` implements `SupportsMCP` and `mcpOverrides`, encoding
  command, args, an explicit environment and permitted tools. The runner consumes
  it at `internal/codex/runner.go:418`; Validate feature restrictions remain at
  line 414. This establishes source availability, not actual host interpretation.
- Claude's `internal/harness/mcp.go` and runner call at
  `internal/harness/runner.go:529` provide generated registration; strict MCP
  configuration remains in the runner.
- `internal/turn/mcp_codex_lifecycle_test.go` contains enabled/disabled/absent,
  quoted-value, collision, run-path and concurrency/cancellation cases. Their
  existence is not a fresh execution result or authentic host evidence.
- `docs/agent-evidence-capture.md`, section “Native MCP host verification
  (#274, US-006)” beginning at line 297, already supplies the four-scenario
  procedure, exact call chains, assertion/provenance matrix, sanitization and
  offline replay handoff. Its statements that US-003 was deferred describe run
  0005; current Codex source exists as noted above. The external capture
  requirement remains unchanged.

## Four-cell inventory

| Scenario | Frozen obligation | Available authentic MCP source | Missing observations |
|---|---|---|---|
| Claude working | AC-009 | None supplied/found | Permitted discovery and actual native tool use; same-attempt preflight diagnostic, repaired input, submission and matching durable decision receipt observed before exit. |
| Codex working | AC-009 | None supplied/found | The same sequence through native Codex MCP, with actual version and sandbox/configuration context. |
| Claude Validate | AC-010 | None supplied/found | Effective native/MCP inventory; allowed live checkpoint and receipt before exit; observable shell and delegation refusal or authoritative unavailability; required terminal findings using retained evidence. |
| Codex Validate | AC-010 | None supplied/found | The same Validate observations under the effective Codex profile, including both negative enforcement observations. |

AC-011 also remains unavailable: no four-cell source/provenance set on one
identifiable implementation revision, no reviewed sanitized exact call/event
mapping, no preserved request/attempt correlation and decision-before-exit
ordering, and no authentic capture checksum/count/replay results. No capture
hashes or host versions are invented. Historical version probes and ordinary
helper receipts do not satisfy MCP observation requirements.

## Inspection bounds

Read the supplied session's `plan_brief.md`, relevant feature-spec sections, the
existing capture handoff and `docs/story-contract.md` evidence boundary. Used
`rg --files` and targeted content searches over `docs/` and `internal/`, including
their `testdata` layouts, plus the supplied session's file inventory. No other
session archive or daemon runtime directory was searched.

The only `*.jsonl` / provenance capture pairs discovered under `docs/` and
`internal/` were `internal/codex/testdata/quota-authentic.jsonl` with
`QUOTA_PROVENANCE.md` and the equivalent pair under `internal/harness/testdata/`.
Their provenance identifies quota diagnostic examples, not the #274 MCP
discovery/repair/receipt or Validate enforcement scenarios. The current session
contains engineering logs, contribution/receipt JSON and planning/report files;
none is an identified four-scenario authentic MCP capture handoff.

This bounded inventory does not assert that no operator-controlled source exists
elsewhere. None was supplied with a locator and provenance for this task. No live
provider was launched, no runtime state was inspected or edited, no forge action
was performed, and no tests were executed by this evidence task. Aggregate check
execution and US-003/US-005 contribution handling belong to the orchestrator.

## Required next input and engineering disposition

A separately authorized external operator must supply the four sanitized captures
and provenance specified in `docs/agent-evidence-capture.md` on the same
implemented revision. T-002 can then integrate those actual sources and retain
missing observations explicitly; T-003 can execute the bounded offline integrity,
correlation, ordering and enforcement assertions. A managed nested agent run,
configuration assertion, shell-helper receipt or synthetic fixture is not an
authorized substitute.

Recommended contribution: `stage=engineering`, `story=US-006`,
`outcome=blocked`, reason: all four authentic external native MCP captures and
their provenance remain unavailable; AC-009/AC-010 observations and AC-011
integration/replay cannot be completed from the landed source prerequisites.
