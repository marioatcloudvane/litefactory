# Delivery status — run 0008 / 304

This is an engineering status inventory, not acceptance, code, architecture or
approved-plan review. The full frozen approved scope remains unchanged.

| Scope | Current evidence |
|---|---|
| US-001–US-004 | Factory marks engineering complete; read-only dependencies |
| US-005–US-006 | Saved implementation/tests observed; no supplied engineering completion flag |
| US-007–US-010 | No corresponding new feature changes demonstrated in this inspection |
| US-011 | Pending; requires manual Claude harness execution for .codex-only scope |
| US-012 | Pending predecessors, aggregate parity/checks and authentic external delivery evidence |

No scope was added or removed. No implementation was changed by this run.
Current build, formatting, vet and lint pass; aggregate race tests fail in
internal/turn with observed socket-path startup errors. Implementation remains
partial and its aggregate gate is not green. There is no acceptance decision or
recommendation to advance modes.

## Run 0010 / 306 — owner-requested checkpoint

Delivery is intentionally limited to unblocking tests. One fixture-only change
explicitly binds legacy Plan transport to its legacy contract; assertions,
production socket safeguards and completed US-001–US-004 remain unchanged.
Fresh focused Plan and full turn race checks pass. This does not establish
engineering completion of US-005–US-012 or acceptance of any story. No approved
requirements changed. The owner requested stopping before further story work;
full feature implementation remains partial. See TEST_REPORT.md for gates.

The first final gate exposed an additional package-wide timeout under aggregate
load. The Makefile now gives the race suite an explicit bounded 20-minute package
deadline, based on the separately measured 594.698-second passing turn suite and
timeout stacks showing progressing short tests. No test was skipped or assertion
weakened; production operation deadlines remain unchanged. This second change
stays within the owner-requested test-unblocking checkpoint.

Final run 0010 gate: `make check` passed with exit 0, including internal/turn in
672.931 seconds; root build passed. The test-unblocking checkpoint is complete.
The full feature remains partial, and work stops before continued story
implementation exactly as requested. No engineering or acceptance completion
is inferred for US-005–US-012 from these aggregate checks.


## Run 0011 / 307 — selected engineering scope

Selected work is US-005 (#219) and US-006 (#220), using the supplied immutable revisions. This entry records implementation task reports and executed checks; it is not acceptance, code, architecture or approved-plan review.

- US-005 T-005.1–T-005.3: inherited envelope/dispatch, receipts and recovery paths completed with current-byte reference checks, honest terminal decision handling, compatible rejection inserts, and offline same-run correction/concurrency/restart/cancellation coverage.
- US-006 T-006.1–T-006.3: inherited typed Plan/render/freeze paths completed with explicit new-feature workflow binding, frozen-ID acceptance consumers, deterministic typed checkbox projection, preserved completed dependencies and immutable projection replay, plus publication/approval/save failure coverage.
- Both story engineering events were published immediately after passing scoped checks. Aggregate/save gates remain separate; no acceptance boxes or issues are changed by this run.
- No added feature scope. The interpretation for checkbox projection is canonical rendered rows with exact frozen IDs/text. Broader semantic inline reconciliation remains US-007.
- US-001–US-004 remain factory-completed dependencies. US-007–US-012 were not selected; no implementation or acceptance claims are made for them. Opposite-harness instructions and authentic dual-harness evidence remain their later story obligations.
- No protected-tree edits, shared context rewrites, forge operations, commit/push or automatic mode transition. Factory saving and publication confirmation remain external to this run.

Engineering scope accounting: selected tasks implemented and scoped checks passed; no material architectural deviation was reported by the engineers. Final aggregate result is in TEST_REPORT.md.

Run 0011 aggregate outcome: `make check` and `go build ./...` passed; engineering events remain byte-current. The selected two-story engineering scope is complete. The overall twelve-story feature remains partial because US-007–US-012 are outside this run.


## Run 0012 / 308 — engineering scope checkpoint

US-007 production implementation and scoped unit/local-boundary tests complete; event 308/000001.json published. US-008/009 and final aggregate gates remain pending. No acceptance/code/architecture/approved-plan review performed. Established clarifications use an explicit authenticated ordinary-text form to avoid trusting an agent claim of equivalence. No selected requirement is intentionally dropped. US-010–012 are outside the owner-selected work.


US-008 delivered default preview and explicit revision-bound recoverable apply with tests. Compatibility required a narrow adapter allowing factory-owned completed legacy children as opaque unchanged dependencies in future typed Plans; no legacy obligations are invented. US-007 engineering snapshot replaced after this shared-file change. No selected requirement intentionally dropped; US-009/aggregate remain pending.


US-009 shared/root delivery complete with temporary render/manifest checks; installed trees intentionally unchanged per frozen scope. All selected story engineering checkpoints have been published, aggregate build/check pending. Remaining US-010–012 are outside selected scope, including manual Claude handoff and authentic evidence prerequisite. No acceptance or code/architecture/plan review was commissioned; this is an engineering delivery accounting record.


## Run 0012 final delivery accounting

### Delivered

- US-007: all-section recognizable reconciliation, preserved unrelated bytes/footer, permanent criterion IDs and explicit authenticated clarification, exact revision/semantic approval handling; pure and offline boundary regressions passed.
- US-008: default CLI preview and explicit recoverable revision-bound upgrade, admission guards, historical/completed legacy preservation and unchanged workflow; pure, CLI/local HTTP and real SQLite/fake-forge regressions passed.
- US-009: shared/root/operator protocol, exact source-to-target manifest and temporary-render checks, independent harness handoffs and honest missing authentic evidence. No installed-tree writes.

### Scope and assumptions

No selected engineering work intentionally dropped. Legacy conversion/inline semantic handling are conservative; ambiguous input stays pending rather than inventing obligations. Explicit `Clarify AC-001: <replacement text>` provides established identity-preserving clarification. Completed legacy dependencies remain opaque factory-supplied identities through subsequent typed Plans. These implementation choices preserve the approved ownership and authority boundaries.

US-010–012 remain outside the owner's selected scope. Installed instruction parity requires the independent Codex/Claude stories, and authentic dual-harness capture is still unavailable. No statement here supplies acceptance/code/architecture/approved-plan review.

### Engineering outcome

All selected engineering checkpoints and final aggregate build/check passed; no reported engineering scope deviation remains. Factory save/commit/push is still required. Feature-wide Implement remains partial because US-010–012 are not part of this run.


## Run 0013 / 309 — engineering scope disposition

Persisted workflow 2; requirements are the supplied frozen issue revisions. US-001–009 remain read-only satisfied engineering dependencies. This is Codex, so US-010 is independently executable. US-011 explicitly requires manual Claude execution of the `.codex/**` targets in `docs/story-contract-instruction-manifest.md`; no permission retry, harness launch or shared-source substitute is authorized by that requirement. US-012 remains dependency-blocked until US-011 engineering completes. Treating “do stories 10, 11 and 12” as selection preserves their explicit harness/dependency constraints.

Blocked events 309/000001.json (US-011) and 309/000002.json (US-012) preserve these facts. No authentic external two-harness helper/diagnostic/correction/decision capture was supplied in this run. No exception or waiver was supplied. Synthetic checks cannot supply that observation. No automatic Validate, acceptance/review findings, forge action, commit or push.


Run 0013 checkpoint: US-010 engineering complete, event 309/000003.json; 16 Claude manifest targets, snapshot `b1945927ccba057b85a2b5ba8d93f3b73012891cd2139e5d2d691ed26cd21e95`. Scoped assertion/render/whitespace checks and build passed. Final make check running. US-011/012 remain blocked as recorded above; temporary installed-tree parity drift is intentional pending manual Claude handoff.


Run 0013 final: `go build ./...` and the single final `make check` passed, exit 0. Format, vet, pinned golangci-lint v2.13.1 (zero issues) and full race suite passed; turn 730.400s, daemon 391.349s, CLI 259.218s. Linter emitted nonfatal cache-write permission warnings; no permission changes or check retries occurred. Published US-010 event 309/000003.json was verified against all 16 final worktree files. No `.codex` diff exists.

Selected scope remains partial: US-010 engineering complete; US-011 blocked for manual Claude execution of the exact Codex targets in docs/story-contract-instruction-manifest.md; US-012 blocked on US-011 and cannot claim aggregate installed-tree parity. Authentic external evidence for both actual harnesses remains missing/unverified without a waiver. Existing US-001–009 work was preserved. Session artifacts record progress outside the exclusively `.claude/**` US-010 implementation snapshot. No acceptance/review, issue closure, forge actions, commit/push or automatic mode transition. Factory saving remains pending.
