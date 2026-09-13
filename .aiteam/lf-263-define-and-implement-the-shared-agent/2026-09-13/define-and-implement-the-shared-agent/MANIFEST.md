# Session manifest

- Branch: lf/263-define-and-implement-the-shared-agent
- Session date: 2026-09-13
- Feature: define-and-implement-the-shared-agent

## Modes

- [ ] Research
- [ ] Plan
- [ ] Implement
- [ ] Validate
- [ ] Spike

## History

- 2026-09-13: Research run 0001 started; inventory current agent communication and propose a reviewable JSON contract sample.

## Notes

- A reusable shared library must eventually encapsulate contract complexity.
- Current scope is research and initial schema/sample design for owner review; no production implementation or routing changes.

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
