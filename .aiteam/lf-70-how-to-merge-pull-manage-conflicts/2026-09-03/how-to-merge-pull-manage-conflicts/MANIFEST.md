# Manifest
# Branch: lf/70-how-to-merge-pull-manage-conflicts
# Session: 2026-09-03

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(done)*
- [ ] **Implement** — code + tests *(not started)*
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

## Notes

