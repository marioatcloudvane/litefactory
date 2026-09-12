# Manifest
# Branch: lf/253-fresh-coding-agent-per-mode
# Session: 2026-09-12

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(skipped — Plan started with no prior Research run for this feature)*
- [x] **Plan** — `plan_brief.md` + one typed story submitted *(completed this run)*
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
