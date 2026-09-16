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

## Implement run 0007 — partial / blocked

Implemented substantial US-001–006 and US-008 work and executed scoped unit/local-persistence/race checks. All changed stories were subsequently invalidated pending current-state repair/verification after aggregate failures. US-007 requires Claude/protected Codex maintenance; US-009 retains three legacy readers pending factual factory cohort/drain evidence; US-010 blocked by these obligations and failing aggregate checks.

Build/gofmt/vet/pinned lint pass; full CLI/daemon plain suites pass. Aggregate instruction and native lifecycle/family catalog tests fail; full turn and make-check race execution do not complete within bounded gates. Two fix cycles used. No acceptance reviewed or changed. No commits/pushes performed.

See TEST_REPORT.md, BUG_ESCALATION.md, DRIFT_REPORT.md, notes.md, US-007-harness-handoff.md, US-009-retirement-disposition.md, US-009-retirement-followup.md and FINAL_IMPLEMENTATION_STATE.json. Final implementation digest 25986981b6304e1bd99c25a4ade586bf7f3164bf359ad24768bcda2ee97d1db8.

## Implement run 0008 — US-007 only (owner-scoped)

Owner instruction scoped this run to US-007 exclusively; US-001–006, US-008, US-009
and US-010's partial/invalidated/blocked state from run 0007 was left exactly as
found, not touched or re-verified.

Ran under Claude, satisfying US-007's required execution harness (Codex cannot
write `.codex/**`; run 0007 correctly left it blocked with an exact handoff in
US-007-harness-handoff.md). Verified the US-005 dependency's shared-source and
manifest output is present, coherent and usable in the current worktree despite
US-005's own checkpoint being invalidated for an unrelated stale-fixture reason
(see US-007-engineering.md for the check); treated it as a usable, not a blocking,
prerequisite.

Integrated the adopted shared/finalization semantics into all nine `.codex/skills/**`
files that were still stale (workflow, review-evidence, plan-orchestrator,
proxy-product-owner, requirements-engineer, go-architect, implement-orchestrator,
go-code-reviewer, research-orchestrator), preserving every native Codex wrapper,
role/tool-restriction note and the mode-header contract. The three engineer skills
(go-implementation-engineer, go-unit-test-engineer, go-integration-test-engineer)
were already in sync and needed no edit. No `.claude/**`, shared-source or other
story's file was touched.

Executed `go test ./internal/teamsource -run TestCodexSkills -race -count=1 -v`
(AC-003's owned-tree assertion): 5 test groups, 43 subtests total, all PASS,
1.634s — 6 subtests failed on this exact command before the edits. Full detail,
before/after evidence and the informational (non-blocking, out-of-scope)
observations on the still-failing cross-tree and Research-neutral-contract tests
are in US-007-engineering.md.

Submitted stage `engineering`, story `US-007`, outcome `engineering_complete` with
a 9-file capture (implementation_digest `da922466378d4c21d1c26c3b5cc533524e0910530c5524e46b8212f78dcf3214`)
and one executed check. Preflight valid/complete, no diagnostics. Submit request
`39e3eeafdb0cc83b44b0534fecbc0cec`: InputState valid, EngineeringState recorded,
AcceptanceState not_applicable, EffectState delivered (confirmed on receipt
lookup). Bound to frozen revision `ccf38a88e6da8a298283a93b33970e15197aa1edb4426675c4ba96070fcda4b3`.
This supersedes run 0007's `blocked` contribution for US-007. No acceptance
claimed, no commit or push performed. US-001–006, US-008, US-009 and US-010
remain exactly as run 0007 left them: not evaluated by this run.

## Implement run 0009 — diagnostic only, no code change, no submission

Owner asked what currently blocks US-009 (#306) and how to continue. Treated run
0007/0008 artifacts as unverified history and independently re-checked the
current worktree; made no assumption that US-001/US-002 (factory-listed
`state: blocked`) satisfy US-009's dependency.

Verified directly against current source (not against prior claims):

- `internal/turn/plan_handoff.go` (`preparePlanHandoff`/`publishPlanHandoff`),
  `internal/turn/story_progress.go` (`StoryProgress.Register`/`ingest`/event-file
  transport) and `internal/turn/validation.go` (`harvestValidation` calling
  `agent.ParseValidationOutput`) — all three legacy ingress paths US-009 must
  retire still exist unchanged in the current worktree. US-009-retirement-
  disposition.md's per-reader claims (retain-all-three, AC-003/004/005
  incomplete) match the code as it stands now.
- `go build ./...` on the current worktree: clean, no errors.
- Re-ran the exact 20-test US-009 compatibility subset (`internal/store` +
  `internal/turn`, includes `StoryProgress*`, `ManagedValidate*`,
  `TypedPlan*`/`PlanFailureRetainsTransportForRetry`, contract-upgrade/migration
  tests) independently: still all pass, plain and confirmed again without
  `-race` re-verification this run (prior run's plain+race logs stand,
  unchanged files). This does not establish retirement — only that retaining
  the three readers causes no regression today.
- Re-ran the specific tests BUG_ESCALATION.md listed as failing against
  US-001/US-002's own changed files (`mcp_runtime.go`, `submission_runner.go`,
  `submissions.go`, `result_finalization.go`, `agent_results.go` for US-001;
  `agent_result_baseline.go`/`agent_result_context.go`/`agent_result_capture.go`
  for US-002): `TestCodexMCPLifecycleEnabledDisabledAbsentSetup`,
  `TestCodexMCPLifecycleMalformedQuotedValuesSurvive`,
  `TestCodexMCPLifecycleServerCollisionRefusesSafely`,
  `TestCodexMCPLifecycleConcurrentCancellationIsolation`,
  `TestMCPFamilyCompleteCatalogPipeSocketHTTP` — **all still fail today**,
  same shape as run 0007 reported (finalization rejects live Codex results for
  missing `$.result`; family-catalog outcomes go missing; Plan/Spike catalogs
  still expose `activity_get`/`activity_update`). This is independent,
  freshly-executed evidence, not a re-read of old logs.

Diagnosis: US-009 is blocked on two independent axes, neither a trivial in-
scope fix:

1. Its declared dependencies US-001 and US-002 are not actually in a working
   state right now — their own finalization/family-catalog tests fail on the
   current worktree, matching the factory's `state: blocked` for both (not
   `engineering_complete`). Building retirement work on top of that
   unfinished/unverified foundation would not be sound engineering.
2. Independent of (1), US-009's own AC-001/AC-002 require a factory/operator-
   produced, read-only live cohort-and-effect-drain inventory and authorized
   upgrade/completion receipts proving no supported consumer still needs the
   three legacy readers. No such evidence exists anywhere in this repository
   or session, and none was fabricated. This is the same conclusion prior
   runs reached and it still holds — it is an evidentiary gap, not a code
   defect, and nothing in US-009's own scope is fixable without that evidence.

No code, test or documentation change was made this run: nothing found was a
trivial fix squarely inside US-009's own acceptance criteria. No factory
submission was made — no verifiable work occurred that changes any story's
engineering/acceptance state.

Recommended path, in order:
1. Finish and re-verify US-001/US-002 engineering (fix the five failing tests
   above) before resuming US-009 — do this as their own scoped implementation
   work, not inside US-009.
2. Separately, obtain the operator-supplied cohort/drain evidence
   US-009-retirement-disposition.md and US-009-retirement-followup.md specify
   (source links unchanged: parent #264, story #306, aggregate #307,
   independent dead-discovery removal #305, authentic-host tracks #274/#296).
   This cannot be produced by an Implement run; it needs a factory/operator
   action outside this repository.
3. Only after both (1) and (2) land can US-009 begin the actual AC-003/004/005
   deletions of the three retained readers, per the retention criteria already
   recorded in US-009-retirement-disposition.md.
4. The previously drafted scope-revision option in US-009-retirement-
   followup.md (split adoption from the three-reader retirement into a
   separate follow-up issue) remains an available owner decision if (1)/(2)
   are judged too costly to pursue inline; it was not applied this run.

## Implement run 0010 — US-001 and US-002, engineering complete

Owner instruction: follow run 0009's recommended path step (1) only — fix
US-001/US-002's failing tests as their own scoped engineering work. Explicitly
did not fabricate US-009 cohort/drain evidence, did not touch US-009's
retirement targets (`internal/turn/plan_handoff.go`, `story_progress.go`,
`validation.go`'s `harvestValidation` — confirmed via `git diff --stat`
unchanged by this run's own edits), and did not start US-003 onward.

Re-verified run 0009's findings independently against the current worktree
before trusting them, per AGENTS.md/CLAUDE.md's dependency rule (neither
US-001 nor US-002 counted as a satisfied prerequisite going in).

**US-001 "Use shared results to finish every mode" (#298).** Found and fixed
two real production defects:
- `internal/turn/mcp_families.go`: `activity_get` bypassed the receipted
  `Begin`/`Finish` dispatch flow entirely, so a `receipts_get` lookup for it
  always returned a lost/"missing" outcome — this was the reported MCP
  family-catalog outcome loss for research/implement/validate. Removed the
  bypass; `activity_get` is now durably receipted like every other family
  tool while still re-invoking live on retry (no stale replay).
- `internal/submission/family_tools.go`: `familyAllowed` special-cased
  `activity_get`/`activity_update` to bypass the research/implement/validate
  gate for `plan` and `spike` — this was the reported stray-tool exposure.
  Removed the 3-line carve-out; Plan/Spike catalogs no longer expose those
  two tools.
- The reported Codex `$.result` rejection was investigated and found to be a
  **test-fixture** defect, not a production one: the fake `codex` lifecycle
  test fixtures never actually called `communication_submit` or emitted valid
  JSON, so `result_finalization.go` correctly rejected them per AC-004 (no
  fabricated acceptance of absent/invalid output) — confirmed against the
  already-passing `TestSharedFinalizationTerminationMatrix`. Fixed by adding
  a real `communication_submit` call to the fixtures
  (`internal/turn/mcp_codex_lifecycle_test.go`), mirroring the existing
  passing pattern in `shared_finalization_test.go`. Also updated three
  pre-existing tests whose assertions encoded the old, incorrect plan/spike
  catalog contents (`internal/turn/activity_runtime_io_test.go`,
  `internal/submission/family_catalog_test.go`,
  `internal/submission/family_tools_test.go` — the latter two ended up
  byte-identical to the last commit once corrected).
- `internal/turn/plan_handoff.go`, `story_progress.go` and `validation.go`
  were read-only reference only; not modified by this run's US-001 work
  (AC-002). Note: `plan_handoff.go` carries a pre-existing uncommitted diff
  from an earlier (run 0007) session that this run did not create, touch or
  resolve — flagged for the owner/a future run, not addressed here since it
  predates and is outside US-001's own scope.
- Verified: `go build ./...`, `go vet ./...` clean; `gofmt -l .` empty;
  pinned `golangci-lint v2.13.1 run ./...` reported 0 issues.
  `go test ./internal/turn/... -run <targeted finalization/MCP-family/Codex-
  lifecycle names> -v` (and again with `-race -cover -timeout 8m`): all pass.
  `go test ./internal/turn/... -run <broader family/activity/shared/MCP-
  runtime names> -timeout 6m`: 50 top-level tests, 0 failures. `go test
  ./internal/submission/... -v -timeout 5m`: 24 tests, 0 failures. Full
  untargeted `go test ./internal/turn/...` does not complete inside an 8-
  minute bounded gate on this machine (large real-subprocess package,
  matching run 0007's documented finding) — targeted coverage above is the
  executed evidence used, not the untargeted run.
- Submitted stage `engineering`, story `US-001`, outcome `engineering_complete`
  bound to frozen revision `9a738f696ae27b0a265127f1dea85f0fb0d30d1773b1ca211b165a5c057b5487`.
  Preflight valid/complete, no diagnostics. Submit request
  `7b359a6c7a676ee9bf5f867f80954403`, receipt ID 159, StoryAttempt 1:
  InputState valid, EngineeringState recorded, AcceptanceState not_applicable,
  EffectState pending — confirmed by an independent receipt lookup after
  submit. 16-file capture, implementation digest
  `266861294dd2e95f1c1ea53189c415d8890b600c636fbe06785eefba6acc126a`, 3
  executed checks (build/vet/fmt/lint; targeted turn tests; full submission
  package tests).

**US-002 "Resume from authoritative results and evidence" (#299).**
Investigated the full resume/baseline/rebase/reply-loss/independent-state
surface (`internal/turn/mcp_guidance.go`, `approved_context.go`,
`family_receipts.go`, `agent_result_aggregates.go`, `agent_result_baseline.go`,
`agent_result_context.go`, `agent_result_capture.go`, `internal/agentresult`,
relevant `internal/store` accessors) against the real current code and real
test runs, not old notes. Found it already correctly implemented — cold vs.
resume/restart/fallback baselines, read-only-reader mutation refusal, complete-
result revalidation on sparse updates, stale-revision rebase without retired-ID
reuse, exact-byte reply-loss recovery, and independently-tracked input/
engineering/acceptance/effect states are all genuinely exercised by existing
passing tests. No production code changes were needed or made.

Independent test-coverage review found one genuine gap:
`store.DB.ResultRequestReferences` (the bounded recent-correlation inventory
embedded verbatim in the production launch prompt telling the agent how to
recover from reply loss) had zero direct test coverage. Closed it with two new
tests: `TestResultRequestReferences_OrderedBoundedAndScoped`
(internal/store/result_history_test.go) and
`TestAgentResultService_BaselineRequestInventory`
(internal/turn/agent_results_test.go). No production defect was found.

- Verified: `go build ./...`, `go vet ./...` clean; `gofmt -l .` empty;
  pinned `golangci-lint v2.13.1 run ./...` reported 0 issues (run after both
  US-001 and US-002 changes together). `go test
  ./internal/agentresult/... ./internal/store/... -timeout 6m`: both `ok`.
  `go test ./internal/turn/... -run <targeted resume/baseline/reply-loss/
  receipt-isolation names> -v -timeout 120s`: all pass.
- Submitted stage `engineering`, story `US-002`, outcome `engineering_complete`
  bound to frozen revision `02ab327e5ff1f1b431f024ee7dba52f6b1a55e40879e55a2164b369ceb3a3d6f`.
  Preflight valid/complete, no diagnostics. Submit request
  `e5ffa1e409f59a91402e7aadc566ef79`, receipt ID 160, StoryAttempt 1:
  InputState valid, EngineeringState recorded, AcceptanceState not_applicable,
  EffectState pending — confirmed by an independent receipt lookup after
  submit. 11-file capture, implementation digest
  `1fb5e1fc56537dbc06faba3f34759f6c529cc71ae0a01d66f024222f84ec9ba8`, 3
  executed checks (build/vet/fmt/lint; targeted turn tests; agentresult+store
  tests).

Both stories' engineering contribution JSON, preflight and receipt bytes are
retained in this session directory (`US-001-contribution-run0010.json`,
`US-001-submit-run0010.json`, `US-001-receipt-run0010.json` and the `US-002-*`
equivalents) alongside run 0007's now-superseded/invalidated artifacts of the
same names without the `-run0010` suffix.

Not touched or claimed complete by this run: US-003 through US-010, US-009's
cohort/drain evidence gap (still an external operator prerequisite, unchanged),
`.codex/**`, `.claude/**`. No commit, push, or acceptance/Validate action was
performed — Implement does not claim acceptance under the separated workflow.

## Implement run 0011 — diagnostic only, no code change, no submission

Owner asked what is necessary to finish US-009, given the factory now lists
both US-001 (#298) and US-002 (#299) as `engineering_complete:true`/`frozen`.
Treated run 0010's claims as unverified until independently checked against
the current worktree, per this run's own instruction.

Independently re-verified, this run, against the current tree (not against
prior notes):

- `go build ./...`, `go vet ./...`: clean. `gofmt -l .`: empty.
- `go test ./internal/turn/... -run 'ToolFamil|Catalog|Codex|LiveResult' -v`:
  `TestMCPFamilyCompleteCatalogPipeSocketHTTP` and
  `TestPublicationRuntimeCatalogFromProduction` (all mode/gate subtests) and
  the family-catalog unit tests all PASS.
- `go test ./internal/agentresult/... -v`: all PASS (versioned corpus, schema
  parity, retirement rollback, immutable-target tests included).
- `go test ./internal/store/... -v`: all PASS (submission, conflict-proposal,
  segment, migration and story-reconciliation tests included).
- The five tests run 0009 found failing and run 0010 claimed to have fixed —
  `TestCodexMCPLifecycleEnabledDisabledAbsentSetup`,
  `TestCodexMCPLifecycleMalformedQuotedValuesSurvive`,
  `TestCodexMCPLifecycleServerCollisionRefusesSafely`,
  `TestCodexMCPLifecycleConcurrentCancellationIsolation`,
  `TestMCPFamilyCompleteCatalogPipeSocketHTTP` — all PASS today, freshly
  executed, not a re-read of old logs.

**Conclusion: run 0010's fix holds.** US-001/US-002 are genuinely in a working
state on the current worktree (build/vet/fmt clean, their own targeted tests
green), independent of the factory's `engineering_complete`/`frozen` label —
this run reached the same conclusion by its own execution, not by trusting
that label.

Re-read the three legacy readers US-009 must retire directly from source
(not from notes): `internal/turn/plan_handoff.go`
(`preparePlanHandoff`/`publishPlanHandoff`/`markManagedPlanReady`),
`internal/turn/story_progress.go` (`StoryProgress.Register`/`Poll`/`Finish`/
`ingest`/`deliverParent`) and `internal/turn/validation.go`
(`prepareValidationContext`/`harvestValidation` calling
`agent.ParseValidationOutput`). All three are present, unchanged in shape
and logic from US-009-retirement-disposition.md's description, and their
documented live callers (`prepare.go`, `harvest.go`, `approved_context.go`)
match what the code shows. `git status --short` confirms
`internal/turn/plan_handoff.go` still carries the same pre-existing
uncommitted local diff run 0010 flagged (predates and is unrelated to
US-001/US-002/US-009 work by any run so far; not investigated further as
out of scope for this diagnostic).

Re-read `US-009-retirement-disposition.md` and `US-009-retirement-followup.md`
and checked their claims against the repo: no factory/operator-produced
read-only cohort/drain inventory or authorized upgrade/completion receipt
exists anywhere in the repository or this session directory now (checked the
full session file listing for anything cohort/drain/inventory-shaped beyond
per-story engineering submission receipts — none found). The disposition's
AC-001/AC-002 gap is unchanged from run 0009's finding.

**Diagnosis: with US-001/US-002 verified solid, US-009 is now blocked purely
on the missing operator/factory-supplied cohort-and-drain evidence** (AC-001/
AC-002), not on its dependencies and not on any other code-level defect found
in its own scope this run. Nothing squarely within US-009's scope was found
to be a trivial in-scope fix, so no code, test or documentation change was
made and no factory submission was made.

Concrete ordered list of what remains, separating what can happen before the
evidence exists from what strictly cannot:

Cannot start before evidence exists (US-009's AC-001/AC-002 gate everything
below):
1. A factory/operator-produced, read-only inventory of the live cohort:
   supported generations/attempts (including cold/restart/continuation),
   observation time, environment/cohort identity, parent/generation/run IDs,
   workflow/content/submission versions, pending upgrade intent and every
   pending operation/receipt. The disposition document already established
   that no existing CLI/store route (`daemon status`, `cmd_story_contract.go`,
   `store.Open`) safely produces this within factory-only authority — this is
   an operator/factory action outside what an Implement run can produce, not
   a missing line of code.
2. Presenting the resulting unfinished cohorts through the supported
   upgrade/admission controls, with explicit authenticated operator
   authority, completing or upgrading eligible work through supported flows,
   and retaining immutable, revision-bound receipts proving no supported
   consumer still needs each of the three readers.

Can only happen once (1)-(2) land, but are otherwise ready to execute
immediately once they do (T-003/T-004/T-005 per US-009's own tasks):
3. Remove exclusive Plan v1 file ingress (`preparePlanHandoff`,
   `publishPlanHandoff`'s legacy-transport branch) once Plan v1 consumers are
   proven settled — preserving v2 internal handoffs/publication and the
   rooted artifact helpers that remain shared.
4. Remove exclusive Implement event-file ingress (`StoryProgress`'s file
   transport creation/`ingest`) once all streams are reliably drained and
   pending effects settled — preserving `StoryProgress` records, checkpoints,
   history, recovery, delivery, acceptance and follow-up owners, which are
   not exclusive to the file-ingress path.
5. Remove exclusive Validate v1 structured-output parsing
   (`harvestValidation`'s `agent.ParseValidationOutput` branch) once a
   compatible reference-only terminal route is proven for every supported
   scope — preserving the shared assessment/acceptance data model.
6. Re-run the already-identified 20-test compatibility subset
   (`US-009-compatibility-tests.md`) plus aggregate build/vet/test gates
   against the post-removal snapshot as the offline regression evidence
   (T-006); this is evidence for the removal, not a substitute for (1)-(2).

Not a code-level prerequisite, but a standing owner decision available at any
time and independent of the above: apply the scope split already drafted in
`US-009-retirement-followup.md` — move the three-reader retirement into a
separate follow-up issue, keep #264/US-009 to adoption plus US-008's proven
dead-discovery removal, and revise US-010's completion criteria accordingly.
This was not applied by this or any prior run; it remains a draft requiring
explicit owner/factory approval.

No code, test or documentation change was made this run. No factory
submission was made — no verifiable work occurred that changes any story's
engineering/acceptance state.

## Implement run 0014 — US-009 explanation

Answered how to implement the inventory and carry out supported upgrades/drain before reader retirement. Retained proposal: `US-009-inventory-approach.md`. Proposed inventory command is explicitly not implemented. Existing upgrade syntax is attributed to run-0013 source verification. No production changes, new tests, runtime inventory, migration, forge action or engineering-complete submission. US-009 remains incomplete; completed dependencies and other stories are unchanged.


## Implement run 0015 — one-off script

Current owner correction implemented: session-local legacy_inventory.go and legacy_inventory_test.go, guide US-009-script-guide.md, live report US-009-live-inventory-run-0015.json, engineering report US-009-engineering-run-0015.md, and focused test report US-009-script-test-report.md. Four focused tests pass in plain/race runs. US-009 remains blocked on existing unfinished legacy cohorts, missing runtime ledgers, and required admission/drain evidence. Receipt 163 records valid blocked input; remote effect is pending. No product feature, runtime mutation, reader deletion, commit or push.


Run 0015 aggregate gate: make check exceeded its 480-second outer bound (recorded exit 124). Vet and pinned golangci-lint v2.13.1 passed; full race test suite did not complete. No aggregate pass is claimed. See run-0015-make-check.log and run-0015-make-check.exit. The current script-only scope provides no evidence justifying unrelated product repairs.

## Implement run 0017 — #230/#259 scripts

Delivered upgrade-230.sh, upgrade-259.sh, upgrade_pending.py, test_upgrade_pending.py and US-009-scripts-230-259.md in this session. Mock behavioral checks: 10 passed, 0 failed. No live migration performed. US-009 blocked checkpoint submitted as request cca32cf26fad478fe49dd2b9520bb64a, receipt 165; confirmed input valid, engineering rejected for blocked outcome, acceptance not_applicable, remote effect pending at observation. No completion claim. Full retirement and aggregate completion remain separate from delivered scripts.

Aggregate run 0017: make check passed formatting, go vet and pinned golangci-lint v2.13.1 (0 issues), then its full race suite exceeded the 360-second bound and was terminated (process return -15). Therefore make check did not pass; no aggregate engineering completion is claimed. Logs: run-0017-make-check.log and run-0017-make-check.exit. No retry or unrelated code fixes were attempted for this narrowed scripting deliverable.
