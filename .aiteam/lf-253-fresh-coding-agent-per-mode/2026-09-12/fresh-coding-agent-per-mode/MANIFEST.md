# Manifest
# Branch: lf/253-fresh-coding-agent-per-mode
# Session: 2026-09-12

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(completed this run — see history; an
  earlier Plan run had proceeded with Research recorded as skipped)*
- [x] **Plan** — `plan_brief.md` + one typed story submitted *(completed earlier run)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Validate** — verification findings *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

**Validate** returns read-only structured findings through the supplied contract; no
manifest checkbox is written by a Validate run.

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-12 — session created for issue #253 ("Fresh coding agent per mode").
  Supersedes #227 and its seven-story plan; independent replacement, no
  imported stories.
- 2026-09-12 — Plan run completed: `feature-spec.md` written by
  requirements-engineer (no material questions), `ARCH_REVIEW.md` written by
  go-architect (verdict: APPROVED, no owner-facing deviation), `plan_brief.md`
  synthesized, and one typed story ("fresh-coding-agent-per-mode", 3 tasks, 15
  acceptance criteria) submitted via `submission preflight` + `submission submit`
  against descriptor `ec09ba98295eed3d`. Preflight and submit both reported
  `valid=true, complete=true, diagnostics=[]` (request_id
  `1192b533f27a6ee0394672a1ba24b4f7`, contribution ID 44, run 358). A
  post-submit `submission receipt` check showed `InputState: valid` but
  `PlanState: pending` with no story issue link yet recorded — publication
  (issue creation, link-fill, readiness) is an asynchronous factory-side step
  not observed as complete within this run. Readiness for Implement is not yet
  established; do not assume it until a later check shows `PlanState`
  advanced past `pending` with a story link present.
- 2026-09-12 (run 0002) — Research run completed at owner request ("Do it")
  after the Plan run above. `RESEARCH_BRIEF.md` written: independently
  re-derived the mode/segment/prompt mechanism directly from source at commit
  `576208a2e541f9f2b44ce6a812ecddf2dd87c957` (`internal/turn/delivery.go`
  `prepareDeliverySegment`, `internal/turn/prepare.go` `previousRun` /
  `resumeDecision` / `allocateRun`, `internal/harness/prompt.go` `BuildPrompt` /
  `PromptInput`, `internal/turn/sessiondir.go` `findSessionDir`,
  `internal/naming`, `internal/planbrief`) and confirmed it matches, without
  contradiction, the design already frozen into `feature-spec.md` (BR-1..BR-18)
  and `ARCH_REVIEW.md` (D-1..D-10). No materially different design surfaced; no
  Plan decision was reopened. Carried forward, not newly found: the two
  unsatisfied evidence obligations (live conversation-lifecycle behavior and
  agent comprehension of the artifact pointer are unverifiable under
  `docs/decisions/nested-agent-evidence.md`'s sandbox boundary). No material
  owner questions; the pending `PlanState` from the prior entry is restated as
  a process note, not a Research finding.
- 2026-09-12 (run 0003) — Research run triggered by an owner comment ("Start
  it, very wasy job" — typo for "easy", characterizing scope as small, not
  new scope). Checked whether anything invalidates the run 0002
  `RESEARCH_BRIEF.md` rather than re-running the investigation: `git log
  576208a2e541f9f2b44ce6a812ecddf2dd87c957..HEAD` shows no commits touching
  `internal/turn`, `internal/harness`, `internal/naming`, `internal/planbrief`,
  `internal/store`, `internal/daemon`, or
  `docs/decisions/nested-agent-evidence.md`; working tree is clean and matches
  `origin/lf/253-fresh-coding-agent-per-mode`. Confirmed-current, not
  re-derived: `RESEARCH_BRIEF.md` is unchanged, no new investigation was
  performed, and no new owner questions were raised.

## Notes

- Instruction requested one small story, roughly three tasks: (1) consistent
  mode-change resets, (2) existing-artifact pointer in the prompt, (3) focused
  regression tests. Delivered exactly as one story with tasks T-1, T-2, T-3
  matching that breakdown.
- Explicitly out of scope (carried into `plan_brief.md` and the story):
  new database schema, artifact-binding subsystem, generalized handoff system,
  instruction-tree rollout, separate live-agent evidence project.
- Two acceptance criteria carry explicitly unsatisfied evidence obligations per
  `docs/decisions/nested-agent-evidence.md`: AC-005's live-behavior half (that a
  real Claude/Codex conversation actually resets) and AC-009's comprehension
  half (that a reset agent actually reads the named brief). Both are retained,
  not weakened, and must not be marked proven by an offline test suite alone.
- Research (run 0002) independently confirmed the same mechanism and the same
  two unsatisfied evidence obligations from source directly; see
  `RESEARCH_BRIEF.md`. Research (run 0003) confirmed that brief remains
  current with no repository changes since; no re-derivation was needed.
