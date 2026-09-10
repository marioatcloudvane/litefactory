# Implementation Plan
# Feature: acceptance-criteria-should-be-structured
# Source: feature-spec.md and ARCH_REVIEW.md
# Created: 2026-09-10
# Status: COMPLETE PROPOSAL; factory publication/readiness pending

## Outcome and scope

Agents currently reconstruct full Markdown and identity-bearing event payloads from examples. The resulting parser/type errors can invalidate already completed work one field and one run at a time. Deliver typed contributions for all five story sections and covered Plan/Implement/Validate outcomes, deterministic factory rendering and envelopes, shared preflight and durable live receipts. GitHub remains the sole editable story authority.

Owner-approved decisions: full story shape, evidence list B, canonical criterion ID rules, both preflight and authoritative receipts (including Plan), comment-preferred editing with inline edits supported, and explicit CLI-only legacy upgrades. No product questions remain. This document is shared context only. Full editable story authority is the factory-published GitHub issues; no local per-story Markdown is created.

## Shared architecture and contracts

Extend existing owners: planbrief for pure story schema/reconciliation, report for Markdown, agent for neutral stage contributions, turn for trusted binding/publication/freeze/evidence, store for all SQL and durable history/receipts, daemon for admission/retry/time, cli for helper/upgrade. A small stdlib-only submission package owns bounded local Unix-socket framing through consumer handlers; it imports neither store nor turn. Both runner adapters consume the same bound capability.

The helper receives JSON on stdin and an owner-only rooted attempt descriptor; agents supply authorized selectors/outcomes/evidence, never identity envelopes/counters. Preflight and submit share validation, aggregate independent errors and disclose incomplete traversal. Receipt dimensions separate valid input, durable engineering record, acceptance and remote effects; Plan readiness remains pending until successful terminal publication/save. Retry the same request/content idempotently; corrections use a new request. Cancellation and bounded workers are joined at run end; durable pending state survives failure. No forge credentials, permissive environment, sandbox widening or new agent fallback.

Content/submission versions are separate from persisted workflow. New feature scopes use the new contract after rollout; existing scopes remain legacy until explicit owner CLI apply. Disallow mixed content/event versions within one active generation and preserve historical revisions/evidence. New evidence is an array of check_ids/summary objects; execution proof still requires retained immutable checks and exact snapshots. Validate reviews retained evidence only.

Issue projections are revision-keyed derivations, never competing authority. Comments use existing authenticated-owner refinement/Plan admission; inline edits across every section remain supported. Stable IDs survive reorder/clarification; material replacement retires its ID; clear additions allocate unused IDs; ambiguity remains pending in ordinary language. Check source revision before writes/freeze and confirm resulting bytes, preserving unrelated edits. No atomic forge compare-and-swap guarantee is assumed. The approved short comment-preference footer belongs outside acceptance obligations.

Shared budgets: 1–256 active canonical AC identifiers (AC-001 through AC-999999 without excess zero padding); 4,096 UTF-8 bytes per criterion; 1–16 evidence explanations of at most 8,192 UTF-8 bytes each; 65,536 rendered story bytes; 8 MiB escaped Plan transport; 4 MiB submission/assessment metadata; separately bounded immutable snapshots within existing 64 MiB per-run retention. Aggregate maxima need not coexist; a 256-short-criterion story with references must fit every consumer. Fail before publication when escaped/rendered totals or redaction would change the approved contract.

## Delivery order and ownership

Ship coherent version-gated increments: schema and validation, durable bound live submission, then typed publication/reconciliation and explicit upgrade; documentation and each independently owned instruction tree follow, with aggregate checks last. Dependencies in the index express engineering prerequisites under separated workflow, not premature acceptance. An independently valuable feedback improvement may ship before full content rollout without upgrading legacy contracts implicitly.

Shared/root instruction sources are one scope. US-010 runs under Codex and owns only .claude/**. US-011 runs under Claude and owns only .codex/**. Both become independently selectable after shared sources; neither waits for sibling-tree parity. Use temporary/in-memory rendering; report interim drift and the manual Claude handoff. Aggregate parity is US-012. Do not generate both installed trees from one story.

## Evidence and completion

Every full story body carries a criterion-by-criterion Evidence Inventory with proof method, availability, version/context bounds and missing prerequisite. Offline synthetic fixtures prove implemented source behavior, not actual harness capability. Supplied #170/#204 incident reports and runs 12–14 are narrative evidence; raw historical payloads were not independently verified here.

Authentic evidence for BOTH actual agents invoking the new helper, receiving diagnostics, correcting in-run and observing a durable decision before exit is currently unavailable. It remains US-012 AC-003, explicitly UNSATISFIED until separately authorized operator-controlled external capture with sanitized version/host/sandbox/run/request provenance is supplied. Neither fake adapters nor a historical unrelated capture discharge it. No managed nested paid-agent launch, permission expansion or fresh checks in Validate are authorized. A reasoned scoped numeric-owner exception may waive the blocker only while retaining the unverified facts.

Implement records scoped engineering and executed checks against exact snapshots and completes aggregate make check/save gates. In separated workflow it releases engineering dependencies while acceptance stays open. Validate is read-only acceptance/code/architecture/approved-plan review using retained outcomes; missing or failed evidence remains factual. Legacy scope keeps existing immediate scoped acceptance/review/test gates. No new integration tags or live-provider tests are planned. Only the factory projects decisions and closes stories. Parent closure additionally requires explicit approval of a concrete PR/current head/main and confirmed integration; mode transitions never occur automatically.

## Assumptions and risks

No existing child identities were supplied, so all 12 entries are creates with zero numeric identities and empty bases. Current transport for THIS Plan remains unchanged v1 full-body JSON; the future typed transport is the deliverable. Factory publication fills verified issue links and establishes readiness, not this local document. No scope repair for historical runs is included.

Transport reachability in actual agent sandboxes is an unresolved evidence prerequisite, not permission to change the boundary. Semantic free-text inference cannot guarantee intent; uncertain edits stay pending. Cross-process revision races require preserved diagnostics/reconciliation rather than claims of atomic remote writes. Instruction delivery requires a manual Claude switch for US-011. These constraints do not require another product question before authoring; they must remain visible in implementation and acceptance reports.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Versioned typed story contract and deterministic rendering | [#215](https://github.com/marioatcloudvane/litefactory/issues/215) |
| US-002 | Strict stage contributions, evidence list B and diagnostic preflight | [#216](https://github.com/marioatcloudvane/litefactory/issues/216) |
| US-003 | Durable contract bindings, evidence references and submission receipts | [#217](https://github.com/marioatcloudvane/litefactory/issues/217) |
| US-004 | Agent-callable local preflight, submit and receipt interface | [#218](https://github.com/marioatcloudvane/litefactory/issues/218) |
| US-005 | Connect submissions to engineering, Validate and recovery | [#219](https://github.com/marioatcloudvane/litefactory/issues/219) |
| US-006 | Publish typed Plans and freeze issue-derived typed requirements | [#220](https://github.com/marioatcloudvane/litefactory/issues/220) |
| US-007 | Reconcile comment and inline story edits without losing authority | [#221](https://github.com/marioatcloudvane/litefactory/issues/221) |
| US-008 | Explicit preview/apply legacy contract upgrade | [#222](https://github.com/marioatcloudvane/litefactory/issues/222) |
| US-009 | Shared protocol instructions and operator documentation | [#223](https://github.com/marioatcloudvane/litefactory/issues/223) |
| US-010 | Claude instruction artifacts maintained by Codex | [#224](https://github.com/marioatcloudvane/litefactory/issues/224) |
| US-011 | Codex instruction artifacts maintained by Claude | [#225](https://github.com/marioatcloudvane/litefactory/issues/225) |
| US-012 | Aggregate compatibility and retained external delivery evidence | [#226](https://github.com/marioatcloudvane/litefactory/issues/226) |

## Next action

After the factory successfully publishes and verifies the complete proposal, the owner may select `/implement build the approved structured story and submission contracts`. Plan remains selected until an explicit owner command or canonical label change passes managed gates.
