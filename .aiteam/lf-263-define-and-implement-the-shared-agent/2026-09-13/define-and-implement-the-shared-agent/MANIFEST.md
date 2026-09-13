# Session manifest

- Branch: lf/263-define-and-implement-the-shared-agent
- Session date: 2026-09-13
- Feature: define-and-implement-the-shared-agent

## Modes

- [x] Research
- [x] Plan
- [ ] Implement
- [ ] Validate
- [ ] Spike

## History

- 2026-09-13: Research run 0001 started; inventory current agent communication and propose a reviewable JSON contract sample.

## Current status — run 0011

Focused Plan traceability correction completed: Story 1 now explicitly links the committed approved Research handoff and all seven schema/example artifacts, maps agreed semantics to its existing criteria and distinguishes historical drafts from recorded Plan refinements. US-001 is submitted as update; US-002–US-006 are unchanged. No story keys, criterion IDs, acceptance obligations or scope changed.

Factory preflight: valid=true, complete=true, no diagnostics/unchecked paths. Durable request `60fa836aeb44ea13bdb2eb7305285fc9`: input valid; Plan pending; engineering/acceptance/effect not_applicable. Revised publication and readiness await factory terminal/save processing. Existing child links remain in the Story Index. No source implementation or tests were needed for this reference-only Plan correction.

## Plan artifacts — run 0010

- `feature-spec.md`: ready requirements, explicit engineering assumptions and evidence obligations.
- `ARCH_REVIEW.md`: ready architectural review, no deviations; additive contract/store/service design and concrete update/progress/evidence policies.
- `plan_brief.md`: shared decisions, dependencies and link-only Story Index awaiting factory-filled child links.
- Completed typed proposal: six stories, 26 explicit criteria, full typed sections/arrays, rostered tasks and criterion evidence inventories; durable content retained through the supplied submission helper. No per-story local Markdown source was created.
- Planned sequence: contract/create; patch/scoped reads; history/receipts; aggregate evidence; advisory progress; finalization guidance/cost comparison and final aggregate checks.
- Evidence limits: offline fixtures and isolated source rendering establish contract behavior only; live agent loading, MCP delivery and billed-token savings are not claimed. Actual engineering checks remain future implementation obligations.
- Recommended next owner command after factory publication/save: `/implement the six planned stories in dependency order`.

## Notes

- A reusable shared library must eventually encapsulate contract complexity.
- Research is retained as design provenance; run 0010 completes Plan only, with no production implementation or routing changes.

- 2026-09-13: Research run 0001 inventory and first draft completed. Research remains incomplete pending owner's JSON shape/order review; no implementation started.

## Research artifacts

- `RESEARCH_BRIEF.md`: source inventory, provisional field schema, ownership/lifecycle/update/progress semantics and review decisions.
- `sample.json`: small first envelope for review and reordering.
- `mode-examples.json`: 15 completed/incomplete/blocked examples across all five modes.
- `exchange-examples.json`: creation, sparse updates, stale/invalid cases, receipts, advisory progress, auxiliary calls and legacy/evidence variants.
- Validation: all three JSON documents parsed; mode/outcome matrix and common-shape checks passed. Illustrations are not production-valid fixtures. No Go code changed or tests executed.

- Run 0002: Verified sample.json exists in local HEAD; corrected issue-facing delivery by including the full JSON inline. Remote publication unverified. Research remains open for owner review.

- Run 0003: Answered metadata and common-versus-mode-specific field questions against focused current sources. Updated all JSON work examples to common optional `result.recommendation` with human-readable `text` and optional advisory `action`; example token `action.moveto.planmode` never authorizes a transition. Existing metadata retained for review, with conditional applicability and simplification options explained. Shared-library requirement retained; no implementation started. Research remains incomplete for owner contract review; no new blocking question is required for this bounded revision.
- Run 0003 validation: all three JSON files parsed; 15 mode/outcome cases and recommendation placement/token consistency checked; creation result matches sample. No production tests executed.

- Run 0004: Common grouping and recommendation text/action accepted; unnecessary fields now omitted. Added `plan-sample.json` and `plan-exchange-examples.json` for rough user stories, criteria enrichment, architecture enrichment and retained revisions. Current full Plan requirements remain completion/publication gates. Future multi-round HITL is recorded as exploratory; no workflow engine, approved Plan, production code or mode transition. Research remains incomplete for remaining contract decisions, with this bounded draft delivered and no blocking question needed.
- Run 0004 validation: all five JSON documents parsed; 15 mode/outcome cases, create/sample equality, optional common arrays, stable identities and snapshot preservation checked. No production tests run.

- Run 0005: Explained input context versus output JSON, current preflight validation versus proposed context retrieval, and shared-library construction with future thin MCP/CLI adapters. Added `implement-sample.json` and `implement-exchange-examples.json`; aligned Implement cases in `mode-examples.json`. Recorded optional `aggregate_checks` as proposed new run-scoped evidence mapping, required only for assigned aggregate obligations and supporting recovery without story events. Research remains incomplete for contract review; no implementation or mode transition.
- Run 0005 validation: all seven JSON files parsed; 15 mode/outcome cases, sample equality, optional common lists, mixed sibling preservation and aggregate-only recovery shapes checked. Examples are illustrative and do not claim actual test execution. No production tests run.

- Run 0006: Owner-requested sanity check of the accumulated brief and all seven JSON artifacts, no new design. Confirmed core common-shape/outcome/recommendation/Plan/Implement claims hold and are internally consistent; found one cosmetic artifact inconsistency (US-1 vs US-001 story-key style across `mode-examples.json` and the other sample files); reconfirmed patch-vs-merge, result lifetime, `aggregate_checks`, Validate/Spike update semantics and MCP/library implementation remain open. Judgement: partially ready for Plan. Research remains incomplete pending the owner's Plan-mode decision.

- Run 0007: Re-evaluated only open health-check findings; distinguished owner choices from technical mapping/fixture work and corrected scope reductions. Added current handoff and MCP_FOLLOWUP.md, normalized US-001 examples in mode/exchange files. Seven JSON files parse; 15 mode/outcome combinations and Research/Implement sample consistency checks pass. No production code changed or Go tests run; Research remains incomplete for recorded choices.

- Run 0008: Explained the same three outstanding choices with concrete consequences and recommendations: explicit stable-ID changes, continuing same-mode result identity, and separate auxiliary advisors initially. No choices settled, new investigation, implementation or scope changes. Research remains incomplete pending owner decisions. Documentation-only clarification; no tests run.

- Run 0009: Owner approved all three recommendations. Recorded patch format, result lifetime and auxiliary scope as settled; marked Research complete and retained technical delivery work in the handoff. No new investigation, code, tests, Plan start or MCP scope change.

- Run 0011: Answered owner schema-provenance concern and submitted explicit Research links/lineage for Story 1 (#266); preserved current criteria and other stories. Full typed preflight passed, revised publication pending as recorded above.
