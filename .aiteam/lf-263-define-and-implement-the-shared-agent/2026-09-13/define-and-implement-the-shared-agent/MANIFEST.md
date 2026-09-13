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
