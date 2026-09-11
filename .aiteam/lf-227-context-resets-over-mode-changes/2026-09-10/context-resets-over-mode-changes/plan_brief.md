# Plan Brief — context-resets-over-mode-changes
# Branch: lf/227-context-resets-over-mode-changes
# Date: 2026-09-11
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/227
# Citation base: a702393d80927e8ac2ca756a35b81d7c35c8f907
# Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)

## Shared Decisions

- Run 0003 “Ok looks good, plan it out!” adopts the three research recommendations: reset on every admitted mode change, keep existing bounded recorded comments, and permit incomplete handoffs within existing workflow gates.
- Reset native conversation identity independently of task/worktree/artifact identity. Same-mode continuation retains existing recovery and independent delivery-reset behavior. Returning to an earlier mode does not resurrect its older conversation.
- Explicitly deliver and consume applicable saved handoffs across cold, resumed and fallback runs. Preserve provenance and omissions; saved artifacts and comments remain background rather than authority over tools, modes, frozen requirements or evidence.
- Reuse existing frozen story bundles, immutable engineering evidence, attributed validation findings and shared runner inputs. Persisted workflow/content versions and approval gates remain unchanged.
- Shared instruction sources precede independent installed updates. Codex owns Claude-tree maintenance; Claude owns Codex-tree maintenance. Aggregate parity follows both, and temporary drift does not block either independent slice.

## Workflow

Conversation selection and artifact binding can be engineered independently. Receiving-mode handoffs depend on both. Shared instructions then describe the delivered contract and release two independent installed-tree updates. Aggregate engineering/parity and bounded external observation follow all slices.

Dependency graph: US-001 + US-002 → US-003 → US-004 → US-005 + US-006 → US-007. US-007 explicitly depends on all six preceding stories. US-006 needs manual Claude execution; a Codex run leaves it available for that handoff while completing other ready work.

Use the factory's persisted workflow. In separated workflow, engineering plus retained executed tests releases dependent work while acceptance stays open; Validate later performs read-only acceptance/code/architecture/approved-plan review. Preserve legacy behavior when factory-bound. No automatic mode chain. Factory publication, save and readiness must succeed before implementation admission; a typed submission acknowledgment alone is insufficient.

Authentic feature-specific lifecycle/handoff capture from both actual adapters is currently unavailable. Its final acceptance obligation remains unverified until separately authorized operator evidence is supplied and assessed. Offline engineering can proceed independently. The agent must not attempt live nested capture or call missing proof successful.

## Cross-Story Constraints

- Keep orchestration/file reads in `turn`, SQL and additive embedded migrations in `store`, neutral adapter values in `agent`, pure prompt rendering in its existing owner, and clocks in `daemon`. No new service, public CLI, configuration or dependency is planned.
- Preserve one SQLite connection, context-first accessors, task locking, idempotent recovery, rooted/pinned artifact reads and final successful preparation ordering. Never let background content alter authority; never silently truncate authoritative input.
- Use existing deterministic Go fixtures and ordinary SQLite/local-worktree tests. No integration tags, live-agent CI, new sleeps, runtime-state editing or writes to `dist/`. Implementation retains appropriate executed checks and final `make check`; planning executes none.
- Preserve read-only Validate without artifact scaffolding or fresh checks. Failed evidence stays failed; missing/stale/misattributed evidence stays unverified. Historical acceptance does not reopen after later changes.
- Retain criterion-named proof inventories with source availability, exact context bounds and missing prerequisites in factory-owned stories. Synthetic fixtures establish offline behavior only.
- Maintain installed trees independently; compare contracts in memory or temporary output, never generate into both trees. Only the factory commits/pushes, publishes stories and projects acceptance.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Start fresh conversations on admitted mode changes | [#240](https://github.com/marioatcloudvane/litefactory/issues/240) |
| US-002 | Keep one validated artifact session across fresh starts | [#241](https://github.com/marioatcloudvane/litefactory/issues/241) |
| US-003 | Deliver explicit handoffs and corrective findings to the receiving mode | [#242](https://github.com/marioatcloudvane/litefactory/issues/242) |
| US-004 | Document the shared handoff contract and evidence procedure | [#243](https://github.com/marioatcloudvane/litefactory/issues/243) |
| US-005 | Apply the handoff contract to Claude instructions using Codex | [#244](https://github.com/marioatcloudvane/litefactory/issues/244) |
| US-006 | Apply the handoff contract to Codex instructions using Claude | [#245](https://github.com/marioatcloudvane/litefactory/issues/245) |
| US-007 | Retain aggregate checks and authentic transition evidence | [#246](https://github.com/marioatcloudvane/litefactory/issues/246) |

## Out of Scope

- Deleting conversation records, comments, artifacts or worktrees; guaranteeing recovery of unwritten reasoning; automatic transcript summaries.
- Automatic phase chaining, new reset opt-outs, weaker approval/acceptance gates or implicit workflow/content upgrades.
- Live nested agent launches, sandbox expansion, fallback-agent introduction, paid-agent capture in CI, unrelated historical evidence recovery or team catalog releases.

## Provenance

This proposal uses the current session's approved `feature-spec.md` and `ARCH_REVIEW.md`, grounded in `RESEARCH_BRIEF.md` from runs 0001/0002. The architecture verdict is APPROVED; no owner question or standards deviation remains. The owner approval is interpreted as adoption of D1–D3, not permission to implement within Plan.

Citation base `a702393d80927e8ac2ca756a35b81d7c35c8f907` is the current HEAD verified by the root agent. Repository path references in architecture are current inspected planning evidence; research line citations are historical and must be rechecked before use. Implement must inspect current frozen scope/current source and reconcile changes through the factory when material. Authentic external observations must record their own exact code revision and adapter/environment bounds; no historical capture is claimed to prove this feature.

The Story Index contains discovery metadata only. Full typed stories are submitted through the factory; no per-story Markdown or second editable story source is retained. Pending issue links are filled only by the factory after publication.
