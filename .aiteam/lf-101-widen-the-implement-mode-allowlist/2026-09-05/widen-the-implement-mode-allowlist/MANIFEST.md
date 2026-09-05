# Manifest
# Branch: lf/101-widen-the-implement-mode-allowlist
# Session: 2026-09-05

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-05 — session created
- 2026-09-05 — Plan requested (issue #101 comment "plan this out!"). Gate check
  failed: `RESEARCH_BRIEF.md` does not exist in this session. Per CLAUDE.md
  Gate rules, Plan cannot proceed without it and this run cannot skip the gate
  without explicit human approval (headless run, nobody to ask). No
  plan-orchestrator was invoked. No artifact was produced.
- 2026-09-05 — Research requested (issue #101 comment "ok research it first!").
  research-orchestrator invoked.
- 2026-09-05 — Research complete. `codebase-tour` mapped the tool-permission
  system (`internal/config/policy.go`, `internal/config/config.go`,
  `internal/harness/settings.go`, `internal/harness/runner.go`) and the
  "defence in depth, NOT the boundary" comment's occurrences. No web research
  needed — the design rationale is sourced from litefactory's own prior spike
  measurements (SD-2), not external docs. `RESEARCH_BRIEF.md` written with
  seven open questions, most notably: the exact target tool list and the
  386-denials evidence live only in the GitHub issue itself, not this repo;
  and the writable-scratch-directory ask has no existing implementation to
  extend. Recommended next step: run Plan mode on issue #101.

## Notes

- Issue #101 reads as a well-scoped, evidence-backed ask (log-derived denial
  counts, a concrete allowlist diff, six acceptance criteria) that could
  plausibly go straight to Plan in a synchronous session where a human could
  waive the gate. In this headless run that waiver isn't available, so the
  run stopped rather than silently substituting Research or fabricating a
  brief.
- Research is now complete; `RESEARCH_BRIEF.md` exists in this session. Plan
  mode's gate is satisfied. A human comment is still required to start Plan.
