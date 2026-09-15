# Adopt the shared communication contract

- Feature: adopt-the-shared-communication-contract
- Branch: lf/264-adopt-the-shared-communication-contract
- Date: 2026-09-15
- Parent: https://github.com/marioatcloudvane/litefactory/issues/264
- Citation base: `0403d5c02d54ea798a9605d87316611cf53c1037`
- Scope authority: owner approved the run 0003 draft, requested a firm Plan in run 0004, and requested readiness after the run 0005 audit. The settled 10-story scope remains unchanged.

## Shared Decisions

- Reuse existing shared result, submission, publication, creation and activity services. MCP, helper and applicable structured terminal adapters converge on shared semantic validation and durable receipts.
- Bind finalization independently of optional MCP startup. Every supported launch has an explicit final route; preserving native auto-start configuration cannot bypass required completion validation.
- Shared business results do not replace typed Plan publication, engineering evidence, frozen acceptance scope or existing workflow authority. Preserve factory-owned identities and separate input, engineering, acceptance and remote-effect receipt states.
- Supply explicit authoritative prior state with stable IDs and observed revisions. A provider session or earlier prose is not a baseline. Reuse immutable checkpoint/aggregate resolution when validating completion.
- Keep attempt transitions in turn and clocks, bounded effect reconciliation and cleanup scheduling in daemon. Durable pending effects survive the end of advisory activity.
- Preserve Validate's read-only structured assessment route. Progress remains advisory and cannot accept or complete work.
- Shared instruction sources precede independent installed-tree changes. Codex handles the Claude tree; Claude handles the Codex tree. Each uses scoped checks, with both-tree consistency checked only at the aggregate gate.

## Workflow

Deliver two explicit milestones:

1. **Production adoption:** shared completion and adapters, explicit prior state/evidence, authorized MCP publication/recovery, advisory progress, auxiliary conventions and canonical/installed guidance. Verify all supported modes and failure paths.
2. **Verified retirement:** remove unreachable discovery separately; remove compatibility ingress only after actual supported-cohort and pending-effect prerequisites are evidenced. Retain a separate disposition for each old input and do not count deprecation documentation as removal.

The shared completion seam precedes prior-state/evidence wiring and the other attempt-bound services. Canonical guidance follows settled production routes; the two installed-tree stories can then run independently under their respective harnesses. Full aggregate verification follows all implementation and retirement work. Adoption can be reported separately if retirement is blocked, but that does not complete the feature.

### Execution order and audit refinements

The recorded owner preference is sequential implementation in ascending order, US-001 through US-010. US-004 explicitly follows US-003 engineering completion because activity and publication extend the same runtime registration. This scheduling preference does not add sibling acceptance dependencies: US-006 and US-007 each depend only on US-005 shared-source work and pass their own engineering checks independently. Use Codex for US-006 (.claude/**), then a manual switch to Claude for US-007 (.codex/**); if the required harness is unavailable, retain the exact handoff for the operator. Aggregate parity remains US-010.

The audit refinements are included in the typed stories: US-001 AC-004 explicitly forbids fabricated automatic post-exit correction or acceptance; US-005 T-001 explicitly updates docs/agent-evidence-capture.md; US-005 T-003 resolves stale instruction-manifest story numbering with this feature's US-005/006/007/010 ownership mapping. Existing criterion IDs, evidence inventories and retirement prerequisites remain intact.

## Cross-Story Constraints

- Preserve current workflow/content/submission versions through restart and supported upgrades; content/submission upgrade does not retire workflow-v1 combined verification.
- Preserve shared StoryProgress records/recovery/delivery, live v2 Plan handoffs/publication, required historical reads and existing database migrations.
- Keep all SQL in internal/store, the single SQLite connection, contextual I/O and existing package direction. Use an additive embedded migration only for a demonstrated missing durable-state requirement.
- Do not edit daemon runtime state or manually rewrite persisted versions. Supported operator-controlled upgrade/completion may be an external prerequisite; unavailable evidence leaves retirement incomplete.
- Do not delete a required reader before its compatibility prerequisite is met. If a supported cohort cannot drain, retain the reader and present the concrete focused retirement follow-up and scope revision for owner decision.
- Retain executed offline production-composition, persistence/recovery and compatibility evidence. Run project make check and formatting checks before declaring implementation finished; Validate only assesses retained evidence.
- Authentic host observation remains the separate intentionally open #274/#296 track. Synthetic fixtures establish tested source behavior only, and unavailable authentic observations cannot satisfy host claims. No managed nested capture or permission expansion is authorized.
- Do not alter immutable historical team releases. Preserve mode-header authority, provider wrappers and scoped tool restrictions when updating current instructions.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Use shared results to finish every mode | [#298](https://github.com/marioatcloudvane/litefactory/issues/298) |
| US-002 | Resume from authoritative results and evidence | [#299](https://github.com/marioatcloudvane/litefactory/issues/299) |
| US-003 | Connect authorized publication and recovery | [#300](https://github.com/marioatcloudvane/litefactory/issues/300) |
| US-004 | Keep progress advisory and bounded to a run | [#301](https://github.com/marioatcloudvane/litefactory/issues/301) |
| US-005 | Align shared instructions and auxiliary conventions | [#302](https://github.com/marioatcloudvane/litefactory/issues/302) |
| US-006 | Update installed Claude instructions | [#303](https://github.com/marioatcloudvane/litefactory/issues/303) |
| US-007 | Update installed Codex instructions | [#304](https://github.com/marioatcloudvane/litefactory/issues/304) |
| US-008 | Remove unreachable story discovery | [#305](https://github.com/marioatcloudvane/litefactory/issues/305) |
| US-009 | Retire legacy inputs after compatibility checks | [#306](https://github.com/marioatcloudvane/litefactory/issues/306) |
| US-010 | Verify adoption and retirement together | [#307](https://github.com/marioatcloudvane/litefactory/issues/307) |

## Out of Scope

- A new MCP server, replacement provider integration or MCP-specific business validation.
- Forcing code/artifact contents, vendor stdout, usage transcripts or process diagnostics into business-result content.
- Implicit workflow migration or retirement of combined workflow-v1 verification.
- Authentic provider capture work owned by #274/#296, live nested agents, daemon restarts or broader permissions.
- Unapproved issue creation/attachment, acceptance waiver, mode transition or merge.

## Provenance

Requirements: [feature-spec.md](feature-spec.md). Architecture: [ARCH_REVIEW.md](ARCH_REVIEW.md). Earlier findings: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md). PLAN_CONTEXT.json preserves the approved scope and recorded assumptions from the continuing Plan conversation. The typed proposal is retained in plan_contribution.json; only factory completion can establish publication and readiness.

Architecture inspected current owners at the citation base. No implementation checks, live legacy-cohort/effect inventory or authentic provider captures were executed in Plan. Recheck caller reachability and actual supported cohorts before retirement. Criterion-specific evidence methods, source availability, version limits and outstanding prerequisites live in the typed stories, not this shared brief.

Plan submission acknowledgment establishes valid retained input only. Publication, issue-link replacement, readiness and successful save remain factory outcomes after this subprocess finishes.
