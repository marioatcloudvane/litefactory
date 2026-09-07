# Manifest
# Branch: lf/70-how-to-merge-pull-manage-conflicts
# Session: 2026-09-03

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(done)*
- [x] **Implement** — code + tests *(done — all 12 of 12 stories delivered: US-1 through US-12)*
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
- 2026-09-07 — Implement mode run (scoped to US-7 only, by explicit
  instruction "Implement US-7," deliberately excluding US-1 through US-6 and
  US-9 through US-12; US-8 — already implemented and committed — was left
  untouched, and the uncommitted partial US-1/US-2 work already in the
  worktree from the earlier stopped run was left untouched throughout). US-7
  (`lf:sync=` label opt-out) implemented in full: `internal/daemon/sync.go`
  (new) adds `syncLabelPrefix`, `syncRefusal`, `syncDecision`, and
  `resolveSync(labels []string) syncDecision`, copied rung-for-rung from the
  live `agentLabelPrefix`/`modeLabelPrefix` ladder (not the dead
  `lf:tools=`/`legacyToolsLabel` pattern); `internal/turn/execute.go` gained
  `Request.SyncOverride *bool`; `internal/daemon/tick.go` resolves the label
  per-tick and threads the decision into the existing `turn.Request{...}`
  literal. No raw label text is carried by any field, satisfying SEC-5/S-9.
  `go-code-reviewer` returned CLEAN with zero findings. Phase 2 environment
  setup skipped — US-7 touches no real dependency (pure label-string parsing
  plus a struct field addition). `go-unit-test-engineer` added two test files
  (7 test functions, ~21 subtests) covering every ladder rung from T-7.3
  (absent, on, off, miscased prefix, duplicated label, invalid value), plus
  sibling-label independence and a structural `reflect`-based SEC-5/S-9 check.
  All tests passed; no bug loop needed. `go build ./...`, `go vet ./...`,
  `gofmt -l .`, and `go test -race ./...` were all re-run directly by the
  orchestrator across the whole module and are green (every package `ok`, no
  race findings). `DRIFT_REPORT.md` gained a second dated section for US-7,
  verdict CLEAN, appended after the existing US-8 section rather than
  overwriting it. No APP_CONTEXT/APP_CONSTRAINTS update was made, per the
  retirement of that step. US-1 through US-6 and US-9 through US-12 remain
  for future runs, each needing its own comment; `syncBase` (US-2, not yet
  built) is the next consumer of `turn.Request.SyncOverride`.
- 2026-09-07 — Implement mode run (scoped to US-1 only, by explicit
  instruction, deliberately excluding US-2 through US-12; US-7 and US-8 —
  already implemented and committed — were left untouched). This run's
  premise was unusual: US-1's production code (`Divergence`,
  `parseDivergenceCounts`, `divergenceTimeout` in `internal/git/merge.go` and
  `internal/git/exec.go`, wired into `internal/turn/prepare.go`'s
  `syncBase`) already existed, written by an earlier run stopped by the
  operator at 49 minutes before reaching review or tests — so this run's job
  was verification, not construction. The one named trap — `git rev-list
  --left-right --count base...branch`'s `<left> <right>` output order, and
  which side means "ahead" vs "behind" — was checked empirically against
  real git behavior in a throwaway repo, independently, by both the
  implementation engineer and the code reviewer (tracing from source rather
  than trusting the first report): both confirmed the existing code has this
  correct, not swapped. `checkName` ref-name screening (S-1), range assembly
  inside `internal/git`, `--end-of-options` usage, the 30s
  `divergenceTimeout`, the missing-base-ref `ErrRevisionNotFound`
  classification, and the `prepare.go` call site (comparing against
  `origin/<Repo.BaseBranch>` after fetch, inside the existing `CloneGate`)
  were all verified correct; no production code was changed.
  `internal/git/merge.go`'s US-2 partial work (`MergePreview`, `MergeBase`,
  `recoverFromFailedMerge`) was read only for pattern comparison and left
  completely untouched throughout. `go-code-reviewer` returned CLEAN with
  zero BLOCKING or ADVISORY findings, scoped strictly to the
  `Divergence`-related code. T-1.4 — the story's only real gap, with zero
  existing tests anywhere in the repo — was delivered in full: new file
  `internal/git/merge_test.go`, 4 test functions / 9 subtests covering all
  four ahead/behind combinations, a side-order regression test with
  deliberately asymmetric counts pinning AC-1.1's exact trap, the
  missing-base-ref error path, and the `checkName`-screening error path, all
  built on the existing `internal/gittest` real-git-in-temp-directory harness.
  Phase 2 environment setup was skipped — US-1 touches no database, HTTP
  call, or message queue, and this codebase's own convention already tests
  `internal/git` with real git in temp directories as unit tests. `go test
  -race ./...` green across all 21 module packages (no race findings);
  `go vet ./...` and `gofmt -l .` clean, re-verified directly by the
  orchestrator. No bug loop needed — zero test failures. `DRIFT_REPORT.md`
  gained a third dated section (US-1), verdict CLEAN. No APP_CONTEXT/
  APP_CONSTRAINTS update was made, per the retirement of that step. US-2 is
  now unblocked — its dependencies (US-1, US-7, US-8) are all complete;
  US-2's own partial, unreviewed code already sits in `internal/git/merge.go`
  from the earlier stopped run and awaits its own comment before anyone acts
  on it. US-3 through US-6 and US-9 through US-12 remain for future runs,
  each needing its own comment.
- 2026-09-07 — Research mode re-run (seventh pass, targeted check, no
  sub-agent invocation): request was "Continue where it was stopped when the
  rate limit happened," worded as an implement-continuation request but
  carrying an explicit `Mode: research` header, so this ran as a Research
  check rather than an Implement continuation of the rate-limited US-2
  attempt. Checked `RESEARCH_BRIEF.md`'s Open Questions plus `US-2.md`,
  `US-3.md`, and `US-4.md` directly for any genuine unresolved research
  question blocking those three stories specifically. Found none requiring
  new research: Open Question 17 (over-cap-skip vs. deliberate-opt-out
  disclosure loudness) is already answered by `US-3.md`'s own Architecture
  Notes and AC-3.2 — every `skipped` outcome discloses its specific reason
  (cap, config, or label), so the two cases are not in fact rendered
  identically. Open Question 21 (`prepareClone` step-order wording) is a
  already-resolved wording fix with no effect on US-2's integration point,
  confirmed unaffected by `US-2.md`'s own Architecture Notes. Open Question
  19 (old-git-version probe message prominence) names no task in US-2, US-3,
  or US-4's own task tables — the git-version floor check is not currently
  assigned to any of these three sub-issues, so it is an unresolved plan
  *scoping* gap, not a blocked research question these three stories are
  waiting on; it is left as-is in Open Questions for whoever scopes it.
  `RESEARCH_BRIEF.md` was not modified — nothing new to add. Neither
  `plan_brief.md` nor any `US-*.md` file was modified. No implementation work
  was performed, and the interrupted run's uncommitted partial code in
  `internal/git/merge.go` (`MergePreview`, `MergeBase`,
  `recoverFromFailedMerge`) was not read, evaluated, or acted on. US-2 (with
  its own partial, unreviewed code already sitting in the worktree),
  US-3, and US-4 remain ready for Implement mode, each pending its own
  comment.
- 2026-09-07 — Implement mode run (scoped to US-2, US-3, and US-4, per
  explicit instruction to continue where a prior rate-limited run had been
  cut off before writing anything to disk — confirmed via `git status` clean
  at this run's start, so this was a fresh start on that scope, not a resume).
  US-1, US-7, and US-8 (already implemented, reviewed, and committed) were
  left untouched; US-5, US-6, and US-9 through US-12 remain out of scope.
  Sequenced per `plan_brief.md`'s explicit dependency chain: US-2 alone
  first, gated to DONE, then US-3 and US-4 in parallel (disjoint files).
  US-2's pre-existing, unreviewed partial code in `internal/git/merge.go`
  (`MergePreview`, `MergeBase`, `recoverFromFailedMerge`, plus
  `internal/turn/prepare.go`'s `syncBase` wiring and
  `internal/turn/gate_test.go`'s registry edit, all left over from an
  earlier, separate interrupted run) was verified against US-2's actual
  acceptance criteria rather than assumed correct: most of it matched spec
  and was kept; one real bug (`parseMergePreview`'s wrong section-boundary
  parsing, corrupting `TreeSHA`/`Conflicts` on every real conflict) was found
  and fixed before code review via a new real-git test. `go-code-reviewer`
  found one BLOCKING finding (CR-B-001: `MergePreview`'s `merge-tree`
  invocation omitted `-z`, so non-ASCII/special-character conflict paths came
  back C-quoted rather than literal, breaking AC-2.2 for real-world repos)
  and two advisories (CR-A-001: `report.Sync.Reason`, a judgment-call field
  addition for AC-3.2; CR-A-002: a coverage gap around `ReplyRefusesSync`
  end-to-end). CR-B-001 was fixed (added `-z`, rewrote the parser for
  NUL-terminated output, verified the new wire format empirically via git
  plumbing commands in a throwaway repo, added a `café.md` regression test)
  and re-reviewed to ADVISORY_ONLY. Phase 2 environment setup was skipped —
  none of the three stories touches a real dependency. `go-unit-test-engineer`
  cross-checked every AC in US-2/3/4 against existing tests, closed two real
  gaps (AC-2.3's "in every outcome" origin-untouched proof, previously only
  checked for the successful-merge path; CR-A-002's end-to-end
  `ReplyRefusesSync` flow, including a posted-comment-body assertion) with
  five new/extended test files, found zero production bugs. `go build ./...`,
  `go vet ./...`, `gofmt -l .` clean; `go test -race ./...` green across all
  22 packages, re-verified directly by the orchestrator both before code
  review and as the final gate. No bug loop needed — zero test failures at
  any phase. `DRIFT_REPORT.md` gained a fourth dated section (US-2/US-3/US-4),
  verdict CLEAN, noting two accepted micro-decisions (the `Reason` field, the
  `turn.Options.BlockRunOnConflict` field) and one correctly-recognized
  deferral (US-3's live wiring from `internal/turn` into a rendered comment,
  blocked on US-9's not-yet-built persistence, exactly as the task table's
  own dependency column implies). US-4's architecture notes were found to
  describe `replyKind()` incorrectly (claimed a new branch there was needed;
  actual code correctly follows the existing `ReplyAgentSwitchRefused`
  precedent instead, verified independently by the code reviewer) — a stale
  sub-issue detail, not a functional deviation. No APP_CONTEXT/APP_CONSTRAINTS
  update was made, per the retirement of that step. US-5, US-6, and US-9
  through US-12 remain for future runs, each needing its own comment; US-5
  (conflict-path sanitization) is now unblocked since US-4 exists, and US-9's
  persistence work is the natural next story to complete US-3's deferred live
  wiring.
- 2026-09-07 — Implement mode run (scoped to US-5 and US-9 together, per
  explicit instruction — a genuine parallel fan-out since both were unblocked
  (US-5 depends on US-4, US-9 on US-1/US-2, all three already done) and land
  in disjoint packages. US-6, US-10, US-11, US-12 deliberately excluded, US-6
  in particular held back because its concurrency test (T-6.1, sized L) would
  have blocked both finished stories at Gate A. US-9's stale migration-number
  citation (`0005_run_sync.sql` on `0004`) was corrected before implementation
  started, per the run's own instructions — the actual, independently
  re-verified next migration is `0007_run_sync.sql`, since `0005` and `0006`
  were taken by an intervening scheduling feature. US-5 (`internal/report`)
  delivered a local reject-don't-salvage denylist (`conflictPathPattern`/
  `conflictPathSpan`) for conflict paths containing markdown-significant
  characters, HTML markers, or bidi-override runes, built on US-3/US-4's
  existing `conflictsBlock` rather than duplicating it; backtick was
  deliberately left off the denylist since the existing `fenceFor` dynamic
  fence-width mechanism already defeats backtick-based breakout, a claim the
  code reviewer independently re-verified against source rather than trusted.
  US-9 (`internal/store` + one call site in `internal/turn/prepare.go`)
  delivered migration `0007_run_sync.sql` and `DB.RecordRunSync`, with one
  deviation from the sub-issue's stale line-number citation: the write-back
  lands immediately after `allocateRun` (not at the `syncBase` call site
  itself, where no `runs` row yet exists) — independently verified correct by
  the code reviewer against current source. `go-code-reviewer` returned
  ADVISORY_ONLY with one new advisory (CR-A-003: `SyncFailed` is a supported,
  tested outcome value currently unreachable from production code, a
  pre-existing property of `prepareClone`'s ordering, not a defect either
  story introduced) and zero BLOCKING findings — no bug loop needed. Phase 2
  environment setup was skipped — neither story touches an external HTTP call,
  message queue, or provisioned database (US-9's SQLite work follows this
  codebase's existing unit-testable-migration convention). `go-unit-test-
  engineer` cross-checked every AC in US-5/US-9 against existing tests and
  closed three real gaps: two for US-5 (a positive test proving ordinary
  markdown is accepted-and-safely-fenced rather than falsely over-rejected;
  a test proving whole-path rejection specifically, using a marker string
  that would survive a truncate-to-prefix regression but not genuine
  reject-whole behavior) and one for US-9 (a genuine `internal/turn`-level
  wiring test, `TestExecuteRecordsTheSyncOutcomeOnTheRunRow`, proving the real
  call site fires during a real `Execute`/`prepare()` flow with a real git
  merge and a real persisted commit SHA — closing a gap the store-level
  round-trip tests alone could not catch). `go build ./...`, `go vet ./...`,
  `gofmt -l .` all clean; `go test -race ./...` green across all 22 packages,
  re-verified directly by the orchestrator as the final gate (no failures at
  any phase). `DRIFT_REPORT.md` gained a sixth dated section (US-5/US-9),
  verdict CLEAN. No APP_CONTEXT/APP_CONSTRAINTS update was made, per the
  retirement of that step. US-6, US-10, then US-11 (needs US-5 + US-9, now
  both satisfied), then US-12 (needs US-11 + US-10), remain for future runs,
  each needing its own comment.
- 2026-09-07 — Implement mode run (scoped to US-6 and US-10, in parallel) was
  cut off by an API rate limit mid-flight, after code review but before unit
  testing. litefactory committed the in-flight work anyway (commit `cfa2a9f`):
  `internal/daemon/gate.go`, `internal/git/errors.go`, `internal/git/merge.go`,
  new `internal/git/merge_timeout_internal_test.go`, new
  `internal/turn/syncconcurrency_test.go`. `go-code-reviewer` had already
  returned ADVISORY_ONLY (CR-A-004, CR-A-005, zero BLOCKING) before the cutoff.
- 2026-09-07 — Implement mode resumed (same scope, US-6 and US-10 only), per
  explicit instruction to continue from where the interrupted run left off
  without redoing completed work. Verified the worktree was clean at `cfa2a9f`
  and the prior implementation/review stood before proceeding. Resumed at
  Phase 3 (unit testing): `go-unit-test-engineer` read the existing
  implementation and every existing test file first and found all four
  acceptance criteria (AC-6.1, AC-6.2, AC-10.1, AC-10.2) already fully proven
  by tests already on disk — `internal/turn/gate_test.go` and
  `syncconcurrency_test.go` for US-6; `internal/git/merge_test.go` (pre-
  existing, from US-2) plus the new `merge_timeout_internal_test.go` for
  US-10, including the T-10.3 timeout/SHA-identity proof the interrupted
  run's code review had flagged as the outstanding gap — that gap had in fact
  already been closed by the same interrupted run, in a file the review's
  file list predated. No new test files were written (writing more would have
  duplicated existing coverage); no production code was touched (no bug
  found). `go build ./...`, `go vet ./...`, `gofmt -l .` clean; `go test -race
  -count=1 ./...` green across all 22 packages, re-run by the orchestrator
  directly as the final gate — no bug loop needed. `DRIFT_REPORT.md` gained a
  seventh dated section (US-6/US-10), verdict CLEAN. No APP_CONTEXT/
  APP_CONSTRAINTS update was made, per the retirement of that step. US-11
  (needs US-5 + US-9, both done) and US-12 (needs US-11 + US-10, US-10 now
  done) remain for future runs, each needing its own comment.
- 2026-09-07 — Implement mode run (scoped to US-11 only, per explicit
  instruction "implement US-11 now"; US-1 through US-10 — already
  implemented, reviewed, and committed — were left untouched; US-12 remains
  out of scope, still blocked on this story's own comment landing before it
  can start). Read `US-11.md` and `plan_brief.md` directly rather than
  trusting the request's own summary, per its own instruction, including the
  question of whether `plan_brief.md`'s Phase 7.5 (a blocking, non-story
  security review, scoped to this story's repository-content-to-model
  exposure, naming `go-code-reviewer` + `.claude/skills/shared/security-scanner.md`
  as "the nearest available instrument" and the human owner as final
  approver) is a formal prerequisite this run must complete separately.
  Resolved as ordinary uncertainty rather than a severe blocker: implemented
  US-11 in full against its own acceptance criteria with
  `suggest_conflict_resolution` defaulting `false` exactly as D-3 specifies,
  and folded the review's automated instrument into this run's own Phase 1.5
  code-review gate by explicitly instructing `go-code-reviewer` to run
  `security-scanner.md` against every new/changed file. The human-approval
  half of Phase 7.5 (flipping the default to `true`) was not performed and
  is not performable by a headless run — recorded as an open item, not
  silently skipped. Migration number `0008_conflict_proposals.sql` was
  re-confirmed by directly listing `internal/store/migrations/` before
  implementation (head was `0007_run_sync.sql` from the completed US-9); no
  collision. `go-code-reviewer` found one BLOCKING finding (CR-B-002: the
  first implementation pass built the `ConflictAdvisor` narrowing interface
  and its wiring, fully covered by stub-based tests, but left no concrete
  `Runner` — `harness.Runner`/`codex.Runner` — actually implementing it, so
  `conflictAdvisorFor` could never find a real advisor in production
  regardless of the config flag, materially failing AC-11.1's real
  posted-comment requirement) and two advisories (CR-A-006: local
  revalidation checks a model-returned path against the full
  `MergePreview.Conflicts` list rather than the narrower set of files
  actually shown to the model, a defence-in-depth gap that doesn't violate
  AC-11.4 as written; CR-A-007: `collectConflictContent` doesn't
  short-circuit once the byte budget is exhausted, a performance note). CR-B-002
  was fixed by adding `ProposeConflictResolution` to both
  `internal/harness/runner.go` and `internal/codex/runner.go`, mirroring the
  existing `ProposeSchedule` precedent's tool-free/bounded-call shape
  exactly in each file, and re-reviewed to CLEAR (interface satisfaction
  verified via a temporary compile-time probe, tool-free/deadline discipline
  independently re-traced, one observed `internal/harness` test failure
  confirmed via 14 combined full-suite/isolated `-race` re-runs to be a
  genuine pre-existing flake unrelated to the diff). Phase 2 environment
  setup was skipped — the story touches only SQLite via this codebase's
  existing embedded-migration unit-test convention, and every model call in
  every test uses a stubbed advisor per T-11.13's own instruction, never a
  real network call. `go-unit-test-engineer` cross-checked all nine
  acceptance criteria against tests already on disk and closed two genuine
  gaps: AC-11.6 (no existing test directly asserted the terminal-state/
  no-sleep-or-poll guarantee — closed with a `Result` state assertion plus a
  new AST-based structural test forbidding `time.Sleep`/`After`/`Tick`/
  `NewTicker`/`NewTimer` in the new file, following this codebase's own
  established AST-test pattern) and CR-A-006 (closed with a regression test
  documenting, not fixing, current behavior). `go build ./...`,
  `go vet ./...`, `gofmt -l .` clean; `go test -race -count=1 ./...` green
  across all 22 packages, re-verified directly by the orchestrator as the
  final gate. No bug loop needed — the one BLOCKING finding was resolved
  within Phase 1.5's own re-review cycle, a distinct mechanism from the bug
  loop. `DRIFT_REPORT.md` gained a ninth dated section (US-11), verdict
  CLEAN — the one genuine deferral (Phase 7.5's human-approval half) is a
  decision only a human can make on the issue thread, not a scope miss this
  run left undone. No `APP_CONTEXT.md`/`APP_CONSTRAINTS.md` update was made,
  per the retirement of that step. US-12 is now unblocked (both dependencies,
  US-11 and US-10, are done) but remains gated by the plan's own Phase 9
  observation gate — "at least one real blocked run has produced a
  suggestion an owner has actually read" — which cannot be satisfied while
  `suggest_conflict_resolution` ships disabled by default; needs its own
  comment once that gate has genuinely opened.
- 2026-09-07 — Implement mode run (resumed US-12, the final story, per
  explicit instruction "Resume us-12" and a reported explicit human waiver of
  the Phase 9 observation gate specifically, "I accept the risk of shipping
  US-12" — the waiver was held to cover only the missing observation, not any
  loosening of US-12's own acceptance criteria, and every AC was verified to
  hold in full). This run's own briefing claimed a prior rate-limited attempt
  had written nothing to disk and that this was "effectively a fresh start" —
  checked, not assumed, and found materially wrong: `git log`/`git show
  --stat` showed the tip commit (`8be7251`) already contained ~2,300 lines
  across 19 files implementing most of US-12 (confirmation grammar in a new
  `internal/confirm` package, `*Worktree` mutators in
  `internal/git/worktree_apply.go`, the agentless apply turn in
  `internal/turn/apply.go`, report/store extensions, migration
  `0009_conflict_proposal_confirmation.sql`) — a clean `git status` meant
  litefactory had already committed the interrupted run's work, not that the
  work didn't exist. Corrected and recorded in `notes.md`; this run proceeded
  as a resume-and-verify (the same pattern already used for US-2 and
  US-6/US-10), not a rewrite. Verification found and fixed two real defects
  (a security-relevant ownership-check direction bug in
  `ValidateConfirmation` — it compared the confirming commenter's login
  against the issue's own author instead of the daemon's single numeric owner
  ID, backwards for the ordinary case; and a stale golden-text fixture in
  `internal/report/testdata/`) and one genuinely missing task, T-12.5 (daemon
  dispatch), built in full this run as `internal/daemon/confirm.go` plus
  wiring, modeled on `internal/daemon/proposals.go`'s existing lifecycle
  shape. `go-code-reviewer`'s pass (with an explicit security-scanner
  instruction, standing in for Phase 7.5 against the single riskiest story in
  the feature) found one BLOCKING finding, CR-B-003 (`prepareClone` called
  `syncBase` unconditionally even on the confirmed-apply turn, risking an
  extra unconfirmed merge commit and a spurious `superseded` result via S-15's
  TOCTOU check) — fixed by gating the call behind the same `confirmed == nil`
  branch already used elsewhere in the same function, with a new regression
  test confirmed to fail pre-fix and pass post-fix; re-review verdict CLEAR,
  five non-blocking advisories remain (CR-A-008 through CR-A-013). `go-unit-
  test-engineer` cross-checked all ten ACs, closed several genuine coverage
  gaps (rendered-comment assertions for all 8 new `ReplyKind` values, the
  enum-exhaustiveness test, T-12.5's dispatch layer, AC-12.7's "unlisted path"
  case, AC-12.9's store-level race guard, AC-12.10's terminal-run guarantee)
  with zero production-code changes — 497 test cases, 0 failed. Phase 2
  environment setup skipped, consistent with every prior story — no external
  HTTP call or message queue is introduced. `go build ./...`, `go vet ./...`,
  `gofmt -l .` clean; `go test -race -count=1 ./...` green across all 24
  packages, re-verified directly by the orchestrator both before code review
  and as the final gate. No bug loop needed — the one BLOCKING finding was
  resolved within Phase 1.5's own re-review cycle. `DRIFT_REPORT.md` gained a
  twelfth dated section (US-12), verdict CLEAN. No `APP_CONTEXT.md`/
  `APP_CONSTRAINTS.md` update was made, per the retirement of that step. This
  closes the feature: all twelve user stories (US-1 through US-12) are now
  implemented, reviewed (CLEAN or ADVISORY_ONLY, zero open BLOCKING findings
  anywhere), tested, and green. The one deliberately open item is the
  human-approval half of Phase 7.5/Phase 9 (flipping
  `suggest_conflict_resolution`'s default and formally recording the security
  sign-off), which remains a decision only a human can make on the issue
  thread — not performable by any headless run.

## Notes
