# Manifest
# Branch: lf/69-we-need-to-make-more-defaults-apply-on
# Session: 2026-09-03

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-03 — session created
- 2026-09-03 — Research mode completed; RESEARCH_BRIEF.md written. Agent
  defaulting confirmed already solved; mode defaulting confirmed only
  partially solved (LLM-classified fallback, no static default, no
  session-state shortcut). Several other onboarding/usage-friction gaps
  identified (silent unassigned-issue path, no agent-tooling detection at
  `init`, no user-facing label docs).
- 2026-09-04 — Research mode follow-up run; RESEARCH_BRIEF.md updated in
  place. Author closed open questions 1, 3, 4, 5 (agent-tool sense
  confirmed; init agent-detection declined; silent unassigned-issue
  behavior confirmed correct-by-design; empty-body doc-comment skipped) and
  confirmed question 2's premise (LLM-classification fallback is the
  intended mode-defaulting design). New Part 4 finding added: confirmed
  true that `CLAUDE.md`'s Classify section unconditionally re-infers mode
  even when the daemon already resolved one via the `- Mode: X` prompt
  header; recommended a documentation-only conditional-trust fix in
  `CLAUDE.md`/`AGENTS.md`. Recommended scope for Plan mode narrowed
  accordingly.

## Notes

