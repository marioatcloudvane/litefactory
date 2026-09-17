# Session manifest

- Branch: lf/259-a-duplicate-check-reference-aborts-a
- Session date: 2026-09-13
- Feature: a-duplicate-check-reference-aborts-a

## Modes

- [x] Research
- [ ] Plan
- [ ] Implement
- [ ] Validate
- [ ] Spike

## History

- 2026-09-13: Research session initialized for issue #259.

- 2026-09-13: Research complete; saved RESEARCH_BRIEF.md with source findings, correction of the fatal Close/recovery path, recommended fixes, regression gaps and passing focused tests. No material unresolved questions.

## Notes

- Run 0013 final delivery: narrowed Research result accepted through MCP at revision 5, matching request `c6990c6d5dd768a10f8ec5fcbb54cfaf`; preflight valid/complete, engineering/acceptance/publication pending.

- Run 0012 delivery: preflight valid/complete; MCP submission and matching receipt accepted input at revision 4, request `3e12805ce9a3ab8d56e7966ce0f6de09`. Engineering/acceptance/publication pending. Advisory research complete; implementation outstanding.

- 2026-09-17, run 0011 final delivery: all three MCP communication calls succeeded once, matching request `4cb8e614737e629881546c6b047da4b7`; input accepted at revision 3, engineering/acceptance/publication pending. Exact call evidence in CODEX_RUN_0011_MCP_DIAGNOSTIC.md; no fallback. Research complete; post-exit factory validation remains outstanding.

- Research reported check-reference collisions, cross-run reconciliation failures, error wording, and Plan receipt recovery.

- 2026-09-16, run 0003: Research complete. Verified run/task and snapshot/story/issue binding at revision 5c4bf0b9ce3b32ea66f633280556d57420e42893; fetched #258 body and six comments, including the latest owner JSON/meta/result proposal. Extended RESEARCH_BRIEF.md with check-key sufficiency versus trusted correlation, schema authority/versioning, scoped errors and stage receipts, Plan change requests, token efficiency and first-use contract loading. No material unresolved questions; future #258 design choices identified as recommendations. No implementation or repeated tests; prior test baseline retained with revision limits.

- 2026-09-16, run 0003: Research complete after fresh inspection at aaa8b9ee60e29c9fb75a9f22b0b6a8c95c1b5143. All original defects remain; shared completion is implemented but preserves affected checkpoint backend. Current focused store/turn and agentresult tests passed; temporary Go overlay reproduced valid preflight followed by duplicate-check conflict and repeated global recovery failure. Saved updated standalone RESEARCH_BRIEF.md and DUPLICATE_CHECK_PROBE.go.txt. No material unanswered decisions, no implementation, runtime edits or remote effects.

- 2026-09-16, run 0004: Independent revalidation of run 0003 at the owner's explicit request ("revalidate, i don't trust the other assessment"). Verified `792f5f6edb892d924e5ace59c1b6f796cc4876e9` is source-identical to run 0003's `aaa8b9ee` revision outside `.aiteam/` (empty `git diff`). Re-derived all file:line citations independently from current source; wrote and ran a fresh, independently-authored duplicate-check-reference probe via Go overlay (not the retained run-0003 probe file) and independently re-ran run 0003's reported test commands rather than trusting reported results. All five claims (check-identity collision keyed by `(run_id,reference)`; global `RecoverSubmissions`/`StoryProgress.Finish` cross-run contamination; harvest mislabeling successful-but-poisoned results as "coding agent did not start"; orphaned Plan receipts excluded from recovery/abandonment by the engineering/acceptance-pending guard; #258's shared contract sitting on the same unfixed backend) were independently confirmed with fresh evidence. No claim refuted or found overstated. Additional finding: the new #258 shared-result reader (`internal/turn/agent_result_context.go:84`) itself reuses the unscoped check lookup, directly inheriting claim 1's defect. Two immaterial citation line-number drifts corrected. Research complete; no material unanswered owner decision; no production code, runtime state, issue state or workflow mode changed; no commit/push.

- 2026-09-16, run 0007: Bounded research assessment complete at `3a19dfd16760431cdc86c28dd61ba6b71fbb8d63`; all five findings reconfirmed by current source reads. Re-executed retained duplicate-check/recovery probe plus focused correlation, normal Plan publication, immutable evidence and all agentresult tests (PASS). Cross-run harvest and orphan Plan failure remain source-traced. Requested MCP delivery is blocked/incomplete: parent inventory finds no litefactory communication tools, zero calls, no rejection, no request ID/receipt, and no CLI/terminal-JSON fallback. See run-0007 brief section and CODEX_RUN_0007_TOOL_INVENTORY.md. No unresolved owner scope decisions and no production/runtime changes.

- 2026-09-17, run 0010: Bounded research complete at unchanged `3a19dfd16760431cdc86c28dd61ba6b71fbb8d63`; current focused source reconfirms all five findings and fresh execution of retained duplicate-check/recovery probe PASS reproduces both errors (1.145s). No implementation or full suite. MCP delivery remains incomplete: ten litefactory tools now visible, but activity_get and communication_preflight are denied with `MCP tool call requires approval, but approval policy is never`; exact args/errors in CODEX_RUN_0010_MCP_DIAGNOSTIC.md. No submit, matching receipt, request ID, helper/terminal fallback, runtime edit, commit or push. No material unresolved owner scope decision.

- 2026-09-17, run 0011: Bounded research complete at unchanged `3a19dfd16760431cdc86c28dd61ba6b71fbb8d63`. Current source reconfirms five findings; fresh retained probe execution reproduced duplicate check conflict and repeated global recovery failure (PASS, 1.477s). Cross-run harvest and orphan Plan behavior remain source-traced. No material unresolved scope decisions or production/runtime changes. Parent reports activity_get now succeeds; final MCP application chain and receipt status are recorded separately in CODEX_RUN_0011_MCP_DIAGNOSTIC.md and final report.

- 2026-09-17, run 0012: Research complete at `1ef699a99a7ee097a1f5bc217e01f341ef1037db`: produced advisory implementation sequence for scoped check-key migration/readers, foreground recovery isolation, truthful harvest and stage-aware Plan orphan handling. Preserves post-broker-close/succeeded-run publication window, immutable history and aggregate attribution. Added upgrade/replay/isolation/harvest/Plan regression matrix plus operator-owned release activities. No new owner questions, tests, implementation, formal Plan stories, runtime changes or mode switch. Parent handles shared-result delivery.

- 2026-09-17, run 0013: Research complete at `c3b461b8b486adcf647272d057e307cc2bcd3e45`. Owner narrowed #259 repair to items 1–3; Plan orphan recovery deferred (no-open-Plans statement is owner-reported, not independently verified or a permanent fix). Focused trace confirms communication protocol guards trusted identity, authority, revision/replay and receipt attribution, while internal global recovery still joins foreign failures into callers. Existing binding supports scoped backend recovery without protocol redesign. No tests, production/runtime edits, Plan stories or mode change. Parent owns shared-result delivery.
