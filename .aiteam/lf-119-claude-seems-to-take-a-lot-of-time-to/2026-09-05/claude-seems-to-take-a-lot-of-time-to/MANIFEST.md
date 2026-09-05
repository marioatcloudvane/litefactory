# Manifest
# Branch: lf/119-claude-seems-to-take-a-lot-of-time-to
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
- 2026-09-05 — Research complete: RESEARCH_BRIEF.md written. codebase-tour traced litefactory's resume-decision and claude-invocation path (internal/harness/runner.go, internal/turn/prepare.go, internal/naming/session.go) and found it is pure in-memory/DB-driven with no cost scaling on Claude session age or size; surfaced a candidate litefactory-side cause (harness/settings.go:114 denies claude read access to ~/.claude/**, which is untested against --resume behavior). web-researcher found upstream Claude Code CLI issues describing slow/hung --resume tied primarily to transcript size, with a documented cache-expiry mechanism requiring both idle time (~1hr) and large transcripts (>100k tokens) together — the reporter's staleness-alone hypothesis is only partially supported.

## Notes

