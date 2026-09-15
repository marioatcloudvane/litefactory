# Session manifest

- Branch: lf/264-adopt-the-shared-communication-contract
- Session date: 2026-09-15
- Feature: adopt-the-shared-communication-contract

## Modes

- [x] Research — prerequisite/status and MCP wiring overview complete; RESEARCH_BRIEF.md, runs 0001–0002
- [x] Plan — run 0006 refined completed typed proposal submitted; factory publication/readiness/save pending
- [ ] Implement
- [ ] Validate
- [ ] Spike

## History

- 2026-09-15: Research run 0001 started; prerequisite status and overview only.
- 2026-09-15: Research run 0001 completed. Checked live issue states/comments and focused source at eb893565687d582bcb79d75edb14023881d0dd73; distinguished landed contract/host infrastructure from #264 adoption/retirement and intentionally outstanding #296 authentic evidence. No implementation or next mode started.

- 2026-09-15: Research run 0002 completed at 4a6c86f6339e6d92fe9e23e1205e818ede9995a2. Extended MCP implementation/caller inventory: existing transport/native wiring, installed versus uninstalled family handlers, missing production intake/recovery/history/activity/final-result connections. No implementation, tests or mode transition.

## Notes

- 2026-09-15: Plan run 0003 saved a simple first draft covering production adoption, MCP wiring, reliable continuation, instruction/test alignment and separately gated legacy retirement. No material questions are needed for this sketch; detailed engineering planning remains unfinished.
- Typed discussion preflight passed with complete validation and no diagnostics. Submission request `5dea52b67a372871f0a7e7d66c9d8c42` returned valid input and PlanState discussion; engineering, acceptance and remote effects are not applicable. No completed proposal or implementation readiness claimed.
- Issues #274 and #296 are deliberately left open as the authentic-host verification track. No implementation requested.

## Plan run 0004

- Owner approved the draft and requested a firm Plan. Requirements and architecture completed without material questions or deviations.
- Artifacts: feature-spec.md, ARCH_REVIEW.md, plan_contribution.json, plan_brief.md; PLAN_CONTEXT.json updated with approved scope and assumptions.
- Proposal: 10 stories, 43 criteria, 38 rostered tasks; every criterion has an evidence inventory. Shared instruction sources precede independent protected-tree stories.
- Adoption and verified legacy retirement remain separate milestones. Missing runtime cohort/drain evidence leaves retirement incomplete; authentic #274/#296 evidence remains a separate unsatisfied track.
- Full typed preflight: valid=true, complete=true, no diagnostics, no unchecked paths.
- Submit request `08f09e5a5fdade31538e1c173f5de150`, receipt 139: InputState valid; PlanState pending; EngineeringState, AcceptanceState and EffectState not_applicable. This acknowledges retained input only. Publication, story links, readiness and successful save require factory post-run success and are not claimed here.
- No implementation, test execution, commit, push, runtime-state edit or mode transition occurred.

## Plan run 0005 — audit pass (no new submission)

- Owner requested a sanity check of the run-0004 proposal: completeness against issue #264's full scope, drift of story architecture/task references against current repository state, and any scope silently dropped between feature-spec.md and the final plan_contribution.json. No requirements conversation was re-run and no new typed proposal was submitted.
- Delegated to `requirements-engineer` (completeness audit) and `go-architect` (drift audit) in parallel, read-only; neither modified feature-spec.md, ARCH_REVIEW.md, plan_brief.md or plan_contribution.json.
- Completeness verdict: no material scope loss. All 16 issue-#264 scope clauses map to explicit story ACs/tasks; all 12 feature-spec ACs map to final story ACs; every story AC has at least one task. All seven retirement requirements (Plan v1 file handoff, Implement event-file ingress, Validate v1 assessment parser, unreachable Markdown discovery, StoryProgress preservation, v2 internal Plan handoffs/publication, historical reads/migrations) have explicit typed representation, not just plan_brief prose.
- Three minor completeness weaknesses found, none blocking: (A) `activity_get` registration and comment/creation wiring are split across US-003 and US-004 at the same runtime registration site (`internal/turn/mcp_runtime.go`) with no declared cross-story dependency; (B) the RESEARCH_BRIEF-flagged stale doc `docs/agent-evidence-capture.md` has no explicit AC/task home under US-005's canonical-docs update; (C) US-001 AC-004 asserts post-exit rejection stays available but doesn't explicitly forbid fabricated automatic correction, weakening feature-spec's stated edge case.
- Drift verdict: none verified. Provenance is clean — plan_brief.md's cited base `0403d5c` to current HEAD `b169999` only touches session artifacts, zero source files changed. All ~28 cited file paths and named types/functions/interfaces across the ten stories exist in the current tree with the claimed shape (spot-checked including `internal/turn/submission_runner.go`, `mcp_runtime.go`, `agent_result_aggregates.go`, `internal/report/subissue.go`, CLI/MCP tool registrations, event-directory ingress in `story_progress.go`/`approved_context.go`, legacy workflow-v1 branches in `store/result_aggregates.go` and `turn/submissions.go`).
- One drift-adjacent advisory (not proposal drift, an execution-order note): `docs/story-contract-instruction-manifest.md` still numbers stories US-009–US-012 from a prior feature; #264's own group renumbers overlapping content as US-005/US-006/US-007/US-010. Already in scope under the story that updates canonical instructions — needs the renumbering handled during implementation of that story, not a new AC.
- Decision: none of the findings rise to the "significant enough to warrant revising and resubmitting the typed proposal" bar set for this audit — no story, AC or task is missing scope; the gaps are a missing cross-story dependency declaration, one doc reference, one wording precision, and a documentation renumbering collision. No resubmission made. The run-0004 typed proposal (10 stories/43 criteria/38 tasks) stands unchanged, still pending factory publication/readiness/save from that run. Findings are handed to the owner as recommended follow-ups (see final report) — implementable either as pre-Implement story amendments via the normal owner-comment/inline-edit path once the stories are published, or as noted assumptions carried into Implement/Validate.
- No implementation, test execution, commit, push, runtime-state edit or mode transition occurred in this run.

## Plan run 0006 — readiness submission

- Owner requested “Make the plan ready” after the prior audit attempt lacked its required completion handoff and implementation admission was refused. Reused the settled run-0004 requirements/architecture and run-0005 audit; no material questions remain.
- Incorporated all four audit refinements in the typed proposal: US-004 depends on US-003 for runtime registration sequencing; US-005 T-001 explicitly updates docs/agent-evidence-capture.md; US-001 AC-004 explicitly forbids fabricated automatic post-exit correction or acceptance; US-005 T-003 explicitly updates the instruction manifest's ownership mapping to US-005/006/007/010.
- Updated plan_brief.md, feature-spec.md and PLAN_CONTEXT.json consistently. Recorded the owner's ascending sequential implementation preference, retaining independent installed-tree engineering/acceptance gates and the manual Codex-to-Claude switch for US-006/007.
- Specialist structural check: 10 stories, 43 stable criterion IDs, 38 rostered tasks, complete criterion task coverage, unchanged evidence inventories and acyclic dependencies compatible with ascending order. Prior completeness and no-drift findings remain applicable.
- Initial preflight rejected the saved create intent because US-001 already has a factory-known identity. Changed all existing stable-key contributions to update; the factory retains and supplies identities/base revisions. Corrected full preflight passed: valid=true, complete=true, diagnostics=[], unchecked_paths=[].
- Fresh completed submission request `aab8b7c061aa994904054ddec0f9e070`, receipt 140: InputState valid; PlanState pending; EngineeringState, AcceptanceState and EffectState not_applicable. Durable input is retained; publication, readiness and successful save remain factory post-run obligations and are not claimed before terminal success.
- No legacy outcome-file workaround, direct forge action, implementation, test execution, commit, push, runtime-state edit or mode transition. Compatibility/drain evidence remains required for retirement; #274/#296 retain the separate authentic-host verification obligations.
- After successful factory publication/save, recommended owner command: `/implement implement all stories in sequential order`.
