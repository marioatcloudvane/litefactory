# Manifest
# Branch: lf-120-agent-switch-refused-leaves-the-session
# Session: 2026-09-06

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-06 — session created
- 2026-09-06 — Research complete: RESEARCH_BRIEF.md written. Confirmed reporter's
  claims re: agent_switch_refused leaving session state='running'; identified two
  additional instances of the same inaccurate comment and seven additional
  call sites in prepare.go with the same class of bug (plain-error returns after
  the running-state write, with no cleanup at all).

## Notes

