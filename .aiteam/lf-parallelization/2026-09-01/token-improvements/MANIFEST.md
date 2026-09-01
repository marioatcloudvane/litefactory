# Manifest
# Branch: lf-parallelization
# Session: 2026-09-01

## Modes

- [ ] **Research** — `RESEARCH_BRIEF.md` *(not started)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [x] **Spike** — `SPIKE.md` *(done)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-01 — session created
- 2026-09-01 — Spike: measured token spend across the 24 recorded runs in
  `~/.litefactory/logs/runs/`. Wrote `SPIKE.md`. No code changed.

## Notes

Spike recommends five fixes. Three are self-contained edits (archive out of the
worktree, split the two large context files, trim agent definitions). Two —
capping the research agents' tool budget, and the subagent context window —
change how runs behave and want a Plan run first.
