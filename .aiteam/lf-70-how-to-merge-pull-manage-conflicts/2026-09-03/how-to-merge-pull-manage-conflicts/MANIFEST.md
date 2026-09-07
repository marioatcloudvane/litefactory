# Manifest
# Branch: lf/70-how-to-merge-pull-manage-conflicts
# Session: 2026-09-03

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(done)*
- [ ] **Implement** — code + tests *(in progress — US-8 of 12 stories delivered)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-03 — session created
- 2026-09-03 — Research mode completed: `RESEARCH_BRIEF.md` written. Codebase
  tour found litefactory has no merge/pull/conflict/PR-creation capability at
  all today (deliberate design). `web-researcher` could not run — issue is not
  labelled `lf:tools=web` — so six web-research questions on branching
  strategy, merge UI patterns, and prior art from other AI coding platforms
  are logged as open questions instead of answered.
- 2026-09-03 — Research mode re-run: `lf:tools=web` label now present.
  `web-researcher` invoked with the six previously-blocked questions;
  `RESEARCH_BRIEF.md` updated with a new "Prior Art & External Findings"
  section (branching strategy, squash/merge/rebase tradeoffs, GitHub web-UI
  conflict-resolution limits, merge queue mechanics, and how five comparable
  AI coding platforms — Copilot coding agent, Devin, Cursor, Sweep, Aider —
  handle branch/merge). Open Questions trimmed to the four narrower residual
  points web research could not settle; Constraints & Risks and Recommended
  scope for Plan mode updated accordingly. Research mode is now complete with
  no outstanding blocked questions.
- 2026-09-05 — Research mode re-run (third pass): issue owner raised three
  follow-up questions — pre-run staleness/drift detection, conditional
  auto-sync of base into a task branch, and centralizing git handling in
  litefactory itself rather than in agents. `codebase-tour` and
  `web-researcher` invoked concurrently (headless run, no clarifying
  questions asked). `RESEARCH_BRIEF.md` updated with a new "Follow-up
  Research" section: a concrete drift-check placement at the existing
  `prepareClone` fetch checkpoint, an "if A then B" auto-sync decision tree
  (fast-forward/clean-merge → sync and disclose, conflict → stop and flag)
  drawing on Dependabot/Renovate prior art, explicit risk-asymmetry framing
  (per-issue branch sync vs. the categorically separate, out-of-scope case of
  merging into shared main), and confirmation that git-exec is already
  centralized in `internal/git` with one named, documented exception (Claude
  agents' leaky `--allowedTools` Bash surface). Original Open Question 5
  (backoff.go retry semantics) resolved: it does not retry git operations.
  Six new open questions added (thresholds, comment placement, Codex sandbox
  network-block status, the Bash loophole's disposition, per-project
  base-branch config gap, and conflict-fallback scope). Recommended scope for
  Plan mode extended accordingly. Research mode remains complete.
- 2026-09-05 — Plan mode completed (headless run, no clarifying questions
  asked; the run's one ambiguity — citation base commit not independently
  determinable from feature-spec.md or ARCH_REVIEW.md — was resolved by
  noting it under Provenance rather than guessing a SHA).
  `requirements-engineer` produced `feature-spec.md` from `RESEARCH_BRIEF.md`,
  covering 10 user stories (US-1 through US-10: divergence measurement,
  auto-merge on clean diff, disclosure, refusal on conflict, safe rendering
  of conflict paths, concurrency verification, `lf:sync=` label opt-out,
  config defaults, persistence, and the rollback guarantee) with 16 flat
  acceptance criteria. `go-architect` reviewed it against
  `.claude/skills/go/platform-constraints.md` and
  `.claude/skills/shared/security-scanner.md`, returning APPROVE WITH CHANGES
  in `ARCH_REVIEW.md`: five deviations (DEV-1 merge ordering inside
  `prepareClone`, DEV-2 git methods live on `*git.Worktree` not `*git.Clone`,
  DEV-3 reshapes US-6 from lock-construction to lock-verification, DEV-4
  models the `lf:sync=` label on the live `agentLabelPrefix`/`modeLabelPrefix`
  ladder rather than the dead `lf:tools=` precedent, DEV-5 the `gate_test.go`
  registry edit), two flagged-assumption resolutions (A-7 keeps
  `auto_sync_max_behind: 100` as a documented judgment call, not a
  measurement; A-14 confirms `block_run_on_conflict: true` on the hard
  condition that US-7's label opt-out ships no later than US-2/US-4), and
  nine forward-looking security findings (S-1 through S-9, spanning ref-name
  screening, untrusted conflict-path content, the `merge-tree --write-tree`
  object-write side effect, commit identity/signing, the abort-then-verify
  rollback pattern, single-`CloneGate` scope, prompt-injection surface,
  and parse-time conflict-list capping). Synthesised into `plan_brief.md`
  (shared decisions, 7-phase shipping order, cross-story constraints,
  10-row Story Index, global out-of-scope list, provenance) plus ten
  per-story sub-issue files (`US-1.md` through `US-10.md`), each with a
  business-register opening, Why This Matters, Architecture Notes, Tasks, and
  Acceptance Criteria scoped to that story alone. Feature-spec.md's flat
  AC-1..AC-16 numbering was reassigned into per-story-scoped IDs
  (e.g. AC-11 → US-6's AC-6.1) to satisfy the sub-issue contract; US-5 and
  US-6 received derived ACs with no direct feature-spec precursor. Story
  Index rows are complete except for the sub-issue link column, which
  litefactory fills in when it creates the sub-issues from these bodies after
  this run ends.
- 2026-09-06 — Research mode re-run (fourth pass, headless, no clarifying
  questions asked): issue owner asked for a full enumeration of the distinct
  scenarios/tracks this feature creates — trigger, conflict state, outcome,
  automatic-vs-manual — plus an explicit capabilities-vs-non-goals accounting
  and a check for anything missed, now that Plan mode's `plan_brief.md` and
  ten sub-issues exist. No new sub-agent invocation was needed; this pass
  synthesized the existing brief together with `plan_brief.md` and two
  sub-issues read directly for precision (`US-2.md` for the exact `syncBase`
  decision tree, `US-8.md` for the behind-cap and config-default semantics).
  `RESEARCH_BRIEF.md` gained two new sections: "Scenario / Track Matrix" (a
  9-row enables table and an 8-row does-not-enable table, covering
  `up_to_date`/`synced`/`blocked`/`skipped` outcomes, the malformed-label
  case, the old-git-version degrade path, the rollback-on-failure path, and
  concurrency independence) and "Capabilities vs. Non-Goals" (an explicit
  enable/non-goal list plus a "what may have been missed" callout on four
  points: the over-cap-skip vs. deliberate-opt-out disclosure being
  identical, the malformed-label-is-refused-not-defaulted trade-off, the
  old-git-version probe's message prominence, and the base-branch-hierarchy
  assumption the original issue phrasing implied but this plan does not
  build). Three new open questions (17-19) added, tracking those four points.
  Research mode remains complete; no new brief content invalidates anything
  Plan mode already delivered.
- 2026-09-06 — Research mode re-run (fifth pass, narrow targeted top-up,
  headless, no clarifying questions asked): after two intervening feature
  merges (lf/50 config hierarchy, lf/69 router-trusts-daemon-mode) plus
  further changes pulled from `main`, the issue owner asked for (1)
  re-validation of seven specific `plan_brief.md` claims against current HEAD
  and (2) a feasibility assessment for a new, not-yet-designed user story:
  calling the default reasoning model for a suggested conflict resolution on
  a genuine merge conflict, posting it for review, and applying it only after
  explicit human confirmation (not the already-rejected auto-resolve pattern).
  This was an explicit top-up, not a re-derivation — no existing brief content
  was revisited or altered. Two `codebase-tour` agents ran concurrently.
  Re-validation found 5 of 7 claims unchanged and 2 concrete drifts: the
  proposed `0005_run_sync.sql` migration collides with two migrations that
  landed since the plan was written (real next-free number is `0007`, to be
  re-checked at implementation time), and `prepareClone`'s actual current step
  order is fetch → freezeSlug → ensureWorktree, not the order some plan prose
  states (the proposed "insert sync last" integration point is unaffected).
  One wording-only nit: `legacyToolsLabel`'s refusal function is still called
  live on every tick, only the label itself is obsolete. Feasibility findings
  for the new story: a narrower "prompt-in, text/JSON-out, no tools" call
  pattern already exists (`agent.Classifier`, `agent.ProposeSchedule` plus the
  `daemon.scheduleAdvisor` narrowing interface) as direct precedent; no
  mid-run pause/resume-on-confirmation mechanism exists anywhere in
  litefactory today — the nearest analogue (schedule proposals) uses two
  separate complete turns joined by persisted DB state and an
  author-ownership check, not an in-flight pause; applying a confirmed
  resolution mixes clean reuse (identity, commit, push all map onto existing
  primitives) with genuinely new surface (no scoped index-resolution
  primitive exists in `internal/git` today); and feeding actual
  conflicting-content (not just file names, which the existing plan already
  covers safely) into a model prompt is a materially larger prompt-injection
  surface with no existing pipeline to extend, only a sibling
  text-capping/neutralising convention to model it on. `RESEARCH_BRIEF.md`
  gained one new dated section covering both questions in full technical
  depth with citations; all 943 lines of prior content left untouched. Nine
  new open questions (20-28) added. This pass makes no architectural verdict
  on the new user story — that is explicitly left to Plan mode's architect.
- 2026-09-06 — Plan mode amendment round (headless, no clarifying question
  asked; a prior asking window was judged already closed and the task's own
  mandate to update the artifacts and report back was treated as sufficient
  authorisation to proceed and disclose rather than pause a second time). The
  issue owner posted a new comment on issue #70 asking three things: (1)
  confirmation of whether every run now pulls in and checks against the
  latest `main`; (2) whether the `lf:sync=` opt-out label (US-7) still makes
  sense, having questioned its value; (3) a new user story calling the
  default reasoning model to suggest a conflict resolution, applying it only
  after explicit human confirmation. The owner also reported having merged
  new changes into the branch from `main` (confirmed by `git log`:
  `2196855 Merge branch 'main' into lf/70-...`). `requirements-engineer` and
  `go-architect` both re-ran against the current repository state.
  `go-architect` verified and corrected two stale plan facts (migration head
  is `0006`, not `0004`/`0005` as earlier drafts said, so US-9 becomes
  `0007_run_sync.sql` and the new story's table becomes
  `0008_conflict_proposals.sql`; `prepareClone`'s real step order is
  fetch → freezeSlug → ensureWorktree, with `syncBase` last, confirmed against
  `internal/turn/prepare.go:376-410`), endorsed keeping US-7 unchanged (the
  label is now also the only per-issue kill switch that keeps repository
  content out of a model prompt, since `lf:sync=off` issues never reach
  `blocked` and therefore never trigger the new stories), and decomposed the
  owner's third request into two new stories rather than one: US-11
  ("Suggest a resolution when a run is refused for a conflict" — propose and
  post only, no branch/index/commit writes) and US-12 ("Apply a confirmed
  resolution and complete the merge" — the only story in this feature where
  model-produced text can become committed bytes, gated by a structurally
  non-bypassable confirmation type). The architect flagged a sequencing
  deviation (D-2): neither new story fits the previously-approved Phase 6/7
  shipping order, so a new Phase 7.5 (blocking security review, scope: content
  outbound to a model and model text inbound, `go-code-reviewer` plus
  `security-scanner.md` as the nearest available instrument, human owner as
  approver), Phase 8 (US-11, shipping with `suggest_conflict_resolution`
  defaulting `false` until the review signs off — D-3, authorised by the
  spec's own escape clause), and Phase 9 (US-12, opening only after Phase 8
  has shipped and at least one real blocked run has produced a suggestion the
  owner has read — an observation gate, not a date) were inserted after the
  original Phase 7. Eight new forward-looking security findings were recorded
  (S-10 through S-17; S-17 explicitly amends and reverses S-9's "conflict-path
  list is never persisted" claim, since a stored proposal's file list and
  resolution must persist for a later run to apply them). `plan_brief.md` was
  rewritten in full to carry the corrected migration numbers, the Phase
  7.5/8/9 workflow addition (flagged as deviation D-2 for the owner's
  attention), the new Security findings, two new Story Index rows (US-11,
  US-12), an Out-of-Scope addition (no live pause-and-wait mechanism, no
  suggestion-quality tuning, no bypass of US-12's confirmation gate in any
  form), and a refreshed Provenance section describing the 2026-09-06 merge
  and the re-verification this amendment required. `US-7.md` gained a
  "Reconsidered and kept (2026-09-06)" section recording the owner's
  challenge and the decision to keep the label unchanged, plus an update to
  its hard-sequencing bullet noting it must now also ship no later than
  US-11. Two new sub-issue files were written in full, `US-11.md` and
  `US-12.md`, each with a business-register opening, Why This Matters,
  Architecture Notes, Tasks, and Acceptance Criteria scoped to that story
  alone, following the architect's fully-drafted content in `ARCH_REVIEW.md`'s
  amendment section. Plan mode's checkbox remains ticked done — this is an
  amendment to an already-completed artifact, not a fresh Plan run.

- 2026-09-07 — Implement mode run (scoped to US-8 only, by explicit
  instruction, deliberately excluding US-1 through US-7 and US-9 through
  US-12; the uncommitted partial US-1/US-2 work already in the worktree from
  the run-9 stop at 49 minutes was left untouched throughout). US-8
  ("Config-governed defaults and size cap") implemented in full:
  `AutoSyncBase`/`AutoSyncMaxBehind`/`BlockRunOnConflict` added to
  `internal/config`'s `Settings` with load-time defaults
  (`true`/`100`/`true`); `Config.Validate()` rejects a negative
  `AutoSyncMaxBehind` as a hard config-load error, never clamped (CFG-6); the
  three keys documented (including the "100 is a judgment call, not a
  measurement" note) in `internal/config/template.go`'s generated
  `config.yaml`. `go-code-reviewer` returned CLEAN with one non-blocking
  advisory (error-message wording nit, CR-A-001). Phase 2 environment setup
  skipped — US-8 touches no real dependency. `go-unit-test-engineer` reviewed
  the implementer's five tests, added two more closing real gaps (explicit
  zero surviving the YAML-decode-onto-defaults merge path; sibling-field
  independence), for seven total. `go test -race ./...` green across all 22
  packages (56 subtests/tests passing in `internal/config` alone, 86.3%
  coverage), `go vet ./...` and `gofmt -l .` clean. No bug loop needed — zero
  test failures. `DRIFT_REPORT.md` verdict: CLEAN. No APP_CONTEXT/
  APP_CONSTRAINTS update was made, per the retirement of that step. US-7 (the
  other US-2 prerequisite), then US-2, then US-3/4/5, remain for future runs,
  each needing its own comment.
- 2026-09-07 — Research mode re-run (sixth pass, targeted check, no sub-agent
  invocation): request was "continue with US-7," worded as an implement
  request but carrying an explicit `Mode: research` header, so this ran as a
  Research check rather than an Implement continuation. Checked
  `RESEARCH_BRIEF.md`'s Open Questions and `US-7.md` directly for any
  genuine unresolved research question blocking US-7 specifically. Found
  none: Open Question 18 (whether a malformed/ambiguous `lf:sync=` label
  should fall back to the config default instead of being refused outright)
  is the only open question that names US-7, and it is already answered by
  `US-7.md`'s own concrete Architecture Notes and AC-7.1 — a malformed,
  miscased, or duplicated label is refused (rejected) and the config default
  applies, which is a softer outcome than the "refused" wording in this
  brief's earlier Scenario/Track Matrix section implied. No codebase or web
  research question remains open for US-7; its sub-issue is fully specified,
  including the exact existing code pattern to copy
  (`internal/daemon/agent.go`'s `agentLabelPrefix`, `internal/daemon/mode.go`'s
  `modeLabelPrefix`). `RESEARCH_BRIEF.md` was not modified — nothing new to
  add. Neither `plan_brief.md` nor any `US-*.md` file was modified. No
  implementation work was performed. US-7 remains ready for Implement mode,
  pending its own comment.

## Notes
