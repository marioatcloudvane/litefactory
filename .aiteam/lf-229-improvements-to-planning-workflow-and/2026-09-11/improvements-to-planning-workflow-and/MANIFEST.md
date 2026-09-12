# Manifest
# Branch: lf/229-improvements-to-planning-workflow-and
# Session: 2026-09-11

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(done)*
- [x] **Implement** — code + tests *(done for S-1, S-2, S-3, S-5; S-4 excluded by owner selection)*
- [ ] **Validate** — verification findings *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

**Validate** returns read-only structured findings through the supplied contract; no
manifest checkbox is written by a Validate run.

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-11 — session created
- 2026-09-11 — Research run: produced RESEARCH_BRIEF.md mapping the GitHub
  run-completion comment renderer (`internal/report`/`internal/turn`), the
  shared-vs-mode-specific boundary, sub-issue and story-progress comment
  paths, and the "partial" status gap. 4 open questions recorded.
- 2026-09-11 — Plan run: owner answered Q-01..Q-04 in the triggering comment
  ("keep it simple, no extra or unnecessary work"). requirements-engineer
  wrote `feature-spec.md` (US-1..US-6, AC-1..AC-19, no open questions). Go
  architect reviewed it, resolved Q-02/D-6 definitively (the live sub-issue
  creation path is `RenderCompleteStory`; the legacy `RenderSubIssue` renderer
  is dormant in production), flagged two deviations against enforced
  constraints (DEV-1: keep the non-success worktree-path Commit arm per
  STYLE-11; DEV-2: keep the Base-branch-sync disclosure per CONC-16, drop only
  Task branch sync) and resolved both by keeping the enforced disclosures,
  confirmed the 5-story split (S-1 slim the run comment, S-2 partial status
  depends on S-1, S-3 sub-issue/progress trim, S-4 `.claude/**` Codex-owned,
  S-5 `.codex/**` Claude-owned) with full task decomposition. Orchestrator
  synthesised both into a typed Plan contribution (26 acceptance criteria
  AC-001..AC-026 across 5 stories) and submitted it via
  `litefactory submission preflight`/`submit`. Preflight was clean
  (`valid: true, complete: true, diagnostics: []`); submit was accepted as
  durable input (`InputState: valid`, request_id
  `f7b37dd4b49190277867aa3af6ee19da`, receipt ID 32, run 344) but its receipt
  still showed `PlanState: pending` (not yet a terminal publication state) on
  two checks after submission. Wrote `plan_brief.md` with a link-only Story
  Index whose sub-issue links are placeholders pending factory publication.

- 2026-09-12 — Implement run (separated workflow): owner selected S-1, S-2,
  S-3 and S-5 (S-4 excluded). Sequenced S-1 before S-2 per dependency; S-3 and
  S-5 ran independently. go-implementation-engineer implemented all four
  stories; go-unit-test-engineer ran per-story checkpoints for the three Go
  stories (S-1, S-2, S-3; S-5 is docs-only) and closed several real coverage
  gaps. Fixed two pre-existing internal/turn test assertions orphaned by S-1's
  approved removal of the Log bullet and success-shaped publication strings
  (not a scope change — see `notes.md`). Aggregate gates green: `make fmt vet
  lint` 0 issues; full-module race test suite green (run split across
  multiple invocations due to tool timeout limits, not test failures — see
  `notes.md`). Submitted all four stories' engineering-complete outcome via
  `submission preflight`/`submit`; all four recorded `EngineeringState:
  recorded` against their correct frozen revisions (S-1 receipt 33, S-2
  receipt 34, S-3 receipt 35, S-5 receipt 36, all under run 345). Wrote
  `notes.md` and `DRIFT_REPORT.md` (verdict: CLEAN for the four selected
  stories). S-4 not started, `.claude/**` untouched. This run's overall
  daemon/turn outcome was later reported as failed with reason "Engineering
  evidence remains incomplete: S-4" even though S-4 was never in this run's
  selected scope.

- 2026-09-12 — Implement run (verification-only, resuming after the prior
  run's "Engineering evidence remains incomplete: S-4" failure reason):
  treated the prior run's self-report as unverified per this run's
  instructions and independently re-checked everything from the actual
  worktree rather than trusting it. `git status --short` matches the prior
  run's claimed file list exactly (all `internal/report`/`internal/turn`
  files plus both `.codex/skills/*/SKILL.md`; `.claude/**` untouched, S-4
  confirmed not started). Ran the gates myself: `go build ./...` and
  `go vet ./...` clean; `golangci-lint run ./...` — 0 issues; full test suite
  across all 32 non-`internal/turn` packages green in one pass; `internal/turn`
  green on a clean re-run after one transient `-race`-unrelated flake
  (`TestStoryContractUpgradePreviewAndExplicitApply` hit a pre-existing
  "story progress parent is busy" SQLite-contention flake on the first
  full-suite pass, unrelated to any of S-1/S-2/S-3/S-5's files, passed in
  isolation and on a full clean re-run); additionally ran `-race` on
  `internal/report` (clean) and on the specific S-2/regression-fix tests in
  `internal/turn` (`TestStoryTally*`, `TestRunInputCarriesTheStoryTally`,
  `TestReportFailedRunStillPostsAndAdvances`,
  `TestTaskPublicationSurvivesAReplayedTrigger`) — all pass under `-race`.
  Dispatched `go-code-reviewer` in a read-only verification pass (not an
  acceptance/Validate review) against each story's stated acceptance
  criteria; it returned CONFIRMED with quoted line-level evidence for all
  four stories (S-1's `run.go` section ladder and metadata trimming, S-2's
  `StoryTally`/`describeOutcome` partial arm/`internal/turn` tally wiring,
  S-3's `RenderStoryProgress` trimmed format plus the two dormant-path source
  comments, S-5's two `.codex/skills/*/SKILL.md` completion-message rules) —
  no gaps found. Attempted independent cross-run receipt confirmation via
  `submission receipt` against the prior run's four known request IDs (run
  345, receipts 33-36); all four returned "Request processing did not
  complete; retain this request ID and query its receipt" for every ID —
  this endpoint does not appear to support cross-run receipt lookup from a
  new run's descriptor, so this run relied instead on the frozen
  `engineering_complete:true` flags already supplied for S-1/S-2/S-3/S-5 in
  this run's story data, combined with the independent file/test/review
  verification above. Since verification found no gaps for any of the four
  stories, this run did **not** resubmit engineering-complete evidence for
  any of them (resubmitting an already-genuinely-satisfied story would
  violate "do not reimplement"). S-4 was not started and `.claude/**` was not
  touched, per the owner's explicit continued exclusion of S-4 from this
  run's selection — flagging for the owner that the prior run's failure
  reason ("Engineering evidence remains incomplete: S-4") appears to reflect
  the overall 5-story plan not being fully engineering-complete, not any
  deficiency in S-1/S-2/S-3/S-5's own work; whether to select S-4 in a future
  run is left to the owner.

## Notes

- Typed Plan submission `request_id`: `f7b37dd4b49190277867aa3af6ee19da`
  (receipt ID 32, run 344). Publication (story issue creation/links, save,
  readiness) is owned entirely by the factory and was still pending as of this
  run; re-check the receipt or the parent issue before relying on the Story
  Index links in `plan_brief.md`.
- Implement engineering submissions (run 345): S-1 request_id
  `a35af02b26123bffd461f4ce8c87ad07` (receipt 33), S-2 request_id
  `8be755dd7274d86dfa3780522cdf11d0` (receipt 34), S-3 request_id
  `6b90a28b1ee37976c8b38bac6684e014` (receipt 35), S-5 request_id
  `6accd19b56ce084e8108594dbbe90195` (receipt 36). All four: `InputState:
  valid`, `EngineeringState: recorded`. Issues/acceptance boxes remain open —
  Validate owns acceptance/code/architecture review next, on explicit
  request.
