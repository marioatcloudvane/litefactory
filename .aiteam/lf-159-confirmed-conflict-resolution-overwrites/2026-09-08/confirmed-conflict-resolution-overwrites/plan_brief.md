# Implementation Plan
# Feature: confirmed-conflict-resolution-overwrites
# Parent: github:marioatcloudvane/litefactory#159
# Generation: 1
# Created: 2026-09-08
# Status: IMPLEMENTED — FROZEN GENERATION 1 VERIFIED

## Goal and Source

Prevent confirmed conflict resolutions from replacing entire files with fragments, refuse unsafe or legacy payloads before commit, and show owners the extent of changes. This plan follows [feature-spec.md](feature-spec.md), approved under the headless instruction, and [ARCH_REVIEW.md](ARCH_REVIEW.md), whose verdict is proceed with no standards deviations. [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md) records current-code evidence; historical branch damage remains issue-provided evidence.

## Shared Decisions

Choose factory-identified region responses and deterministic splicing. Preserve every byte outside conflict spans. Complete conflicted path and region coverage is mandatory: any unsupported/excluded path blocks the whole automatic proposal. Keep the 4,096-byte per-file conflict-content cap and configured total cap, enforce 1 MiB original reads per file and 1 MiB aggregate advisor output, and retain the existing 100-file limit. Full originals remain bounded transient memory.

Version stored region proposals explicitly; legacy/missing and unknown formats cannot be confirmed. Regeneration requires a new owner confirmation. Bind canonical original/result digests, complete region sets, recorded branch/base identities and raw metrics. Canonical merge-tree hashes are checked against regenerated canonical bytes; actual git merge permits only opening/closing marker text-label differences, with exact other marker metadata, ordered side content and outside bytes. Reconstructed actual output must match the proposed result digest.

Validate all candidates before persistence and again under the existing clone gate before the first replacement. Recheck written/staged bytes before commit. Preserve authorization, freshness, expiry, containment, exact staging, abort/restoration and postcommit push-failure behavior. Full .go results must parse with stdlib go/parser. Refuse a result strictly below half the task-branch baseline bytes OR logical lines; exact-half and zero-baseline boundaries follow the spec. No unsafe-confirm override or automatic repository validation commands.

Compute metrics on raw bytes against the named task-branch SHA, with deterministic diff/stat behavior and no external diff/text conversion. Report consumes prepared values and sanitizes only display. Budget all per-file metadata before diff; omit diff with notice, or refuse if complete metadata cannot fit. State Go syntax coverage and sanitized/incomplete display accurately.

## Architecture and Ownership

internal/agent owns the common tool-free advisor contract. internal/git owns bounded Git reads, pure region spans, reconstruction, content policy, raw statistics and the final mutation barrier. internal/turn owns collection, explicit value mapping, lifecycle and clone gate; advisor invocation stays outside the gate. internal/store owns additive migration and persistence as a leaf. internal/report owns safe rendering only. No new dependency, service, clock or goroutine is needed. Do not normalize source, persist complete originals, or move SQL/Git execution outside their designated owners.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-1 | Resolve identified conflict regions while preserving surrounding file bytes | [#166](https://github.com/marioatcloudvane/litefactory/issues/166) |
| US-2 | Persist and confirm only validated region proposals with rollback on refusal | [#167](https://github.com/marioatcloudvane/litefactory/issues/167) |
| US-3 | Show complete change statistics and a bounded safe confirmation diff | [#168](https://github.com/marioatcloudvane/litefactory/issues/168) |

## Delivery Order and Scope

US-1 (L): identified-region contract, bounded collection, deterministic reconstruction and reusable policy. US-2 (L), after US-1: versioned persistence and safe confirmed application with rollback. US-3 (M), after US-2: complete metrics, bounded preview and final checks/documentation. Shared groundwork in US-1 is unavoidable; confirmed application is verified end-to-end in US-2. Execute sequentially because all stories touch the same conflict flow. Detailed tasks and acceptance obligations for Implement came solely from factory-selected immutable GitHub revisions for issues #166–168; links remain a provenance-only Story Index.

## Evidence and Definition of Done

Each story body uses unique leading AC identifiers; evidence names must match them exactly. Each story requires its own focused acceptance checks, review and tests, then the factory-supplied per-run evidence event immediately upon completion. Local real-Git/SQLite fixtures and handwritten fakes cover supported success as well as refusal and rollback; no integration build tags. Final make check and go build ./... are required after scoped verification. Never make build, write dist/, alter runtime state, or commit/push from the agent. Factory alone publishes/verifies the proposal, mutates forge state and closes stories.

All story criteria verified; no unresolved blocking review findings or standards deviations; final checks pass; safety/legacy/preview behavior documented. No production code or tests were changed or executed by this Plan run.

## Handoff and Assumptions

The complete generation-1 version-1 proposal is transient root .litefactory-plan-proposal.json, left for factory consumption and never to be committed. No existing factory-supplied GitHub story context was provided, so all three stories have create intent, zero numeric identities and empty expected_base. Publication/readiness is pending factory verification. The factory replaces the Story Index with real links and removes transient transport before saving.

Conservative headless policy choices are recorded in feature-spec.md: region splicing, fixed bounds, strict-half refusal and unsupported legacy rejection. Historical repair, universal parsing, semantic correctness, threshold UI and equal-size malicious-edit guarantees remain out of scope.

## Implement result

Run 0003 / 231 delivered all selected generation-1 stories. [DRIFT_REPORT.md](DRIFT_REPORT.md) records CLEAN delivery; [CODE_REVIEW.md](CODE_REVIEW.md) records no blocking findings. Final make check and go build ./... passed. Final transient events 000007–000009 carry scoped snapshots and all acceptance evidence; factory owns ingestion and forge state. Earlier Plan handoff paragraphs above describe run-0002 history, not a substitute for frozen Implement scope.
