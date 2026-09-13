# Session manifest

- Branch: lf/263-define-and-implement-the-shared-agent
- Session date: 2026-09-13
- Feature: define-and-implement-the-shared-agent

## Modes

- [x] Research
- [x] Plan
- [x] Implement
- [ ] Validate
- [ ] Spike

## History

- 2026-09-13: Research run 0001 started; inventory current agent communication and propose a reviewable JSON contract sample.

## Current status — run 0013

The owner explicitly approved the existing plan and requested publication. Resubmitted the complete six-story typed Plan unchanged, including run 0011 Story 1 Research references. No criteria, story keys, dependencies, scope or existing child links changed. Mode remains Plan; implementation was not started.

Factory preflight returned valid=true and complete=true with no diagnostics or unchecked paths. Durable request `49c3c211d7ee50afc0d6179973576807`: InputState=valid, PlanState=pending, EngineeringState=not_applicable, AcceptanceState=not_applicable, EffectState=not_applicable. This acknowledges retained approved Plan input; publication/readiness still require successful factory terminal/publication/save processing and are not claimed by this receipt. No blocker or owner question remains.

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

- Run 0013: Owner approved the existing plan and requested publication. Complete unchanged proposal passed preflight and was durably submitted under request `49c3c211d7ee50afc0d6179973576807`; publication pending factory terminal/save. No implementation, code changes, commits or pushes.

## Current status — run 0014

Implement started against the complete frozen six-story scope supplied by the factory. US-001 is in development; later stories wait for engineering dependencies. Tests and aggregate checks are pending. No acceptance review, commit, push or publication is claimed.

## Final status — run 0014

Implement incomplete: US-001 through US-005 engineering recorded; US-006 source work and scoped tests delivered but blocked by final make check. No acceptance or mode transition.

- US-001 receipt: c32c621bb6e429c86ffbfe87e61c6f93
- US-002 receipt: 4f561c8df0ee2927892af4be45b85f5b
- US-003 receipt: f6fc5e2929990702c6bb79cd41ec3170
- US-004 receipt: b4a1edf7a2d71fc3258e7bddcb371758
- US-005 receipt: 3af2917d375d203d407cf04470d137f4
- US-006 blocked receipt: 0ce19b664e702c1fafaa756d19bc14ec (valid input; engineering rejected)

Build, formatting, vet and pinned lint passed. Full race suites passed in all packages except four pre-existing installed-instruction parity tests in internal/teamsource. Their inputs are unchanged from HEAD; repair conflicts with US-006 installed-tree exclusion and read-only .codex permission. No aggregate pass is claimed.

Artifacts: TEST_REPORT.md, notes.md, DRIFT_REPORT.md, US-006-aggregate-blocker.md, all six implementation/scoped test reports and logs, per-story contributions/preflights/receipts, aggregate-make-check-final.log, aggregate-build.log, aggregate-gofmt.log, aggregate-final-manifest.json and aggregate-final-snapshot.json. Final source digest: bc8f86a96484ca8ab61a8100d4c187ed25309fb6b7b6f63b737dacbd6c7e79da (140 files; rechecked unchanged).

Next required work is separately scoped installed-instruction/parity repair, then rerun the required aggregate gate and complete US-006. This run does not ask for confirmation, change scope, commit, push or certify factory save/publication.

## Run 0015 — Implement diagnosis, partial

US-001–US-005 remain engineering complete and read-only. US-006 remains blocked on existing aggregate parity failures. Analysis-only findings are retained in BUG_REPORT.md; no tests rerun and no new engineering completion claimed. Separate authorization for installed-instruction repair is required before targeted parity tests, aggregate make check, and final snapshot evidence can complete US-006.


## Run 0017 — Claude status check (2026-09-13)

Status-only request; no repair performed. Fresh targeted instruction tests with race detection and uncached execution failed on the exact structured protocol handoff in all nine Claude role files. No Codex failures were reported. US-006 remains blocked; prior engineering-complete dependencies are unchanged. No aggregate run, acceptance review, or contribution submission occurred. Evidence: `run-0017-claude-status.md`.


## Final status — run 0018

Implement engineering complete. The authorized nine-file Claude repair passed targeted parity tests; focused US-006 checks, build, formatting and native make check passed. Earlier wrapper timeouts are retained honestly. All six stories now have engineering evidence; prior five dependencies were not resubmitted or modified. US-006 request `ca5c340c919238e6a385bad3debf2354`: valid input, engineering recorded, acceptance not_applicable, remote effects pending. No acceptance or successful factory save is claimed.

Final report: `run-0018-completion-report.md`. Final 158-file digest: `de32b0012fac33c3ba532a83ef8f92259352f7953e8ed89ce4b91df07d463ecc`.
