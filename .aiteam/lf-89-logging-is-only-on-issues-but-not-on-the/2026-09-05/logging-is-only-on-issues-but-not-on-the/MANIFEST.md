# Manifest
# Branch: lf/89-logging-is-only-on-issues-but-not-on-the
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
- 2026-09-05 — Research completed: RESEARCH_BRIEF.md written after codebase-tour of internal/cli (logs command, router/usage handling) and internal/daemon (tick loop) and internal/logging (daemon.log). Headless run, no web research needed.
- 2026-09-05 — Research follow-up: incorporated reporter clarification (bare `logs` must match `daemon run`'s console output exactly; verbosity-level keyword requested). Second codebase-tour confirmed console output is plain `slog.TextHandler` text (distinct from `daemon.log`'s JSON) and found an existing but CLI-invisible `log_level` config feature (`internal/config/config.go`, `internal/logging.ParseLevel`) with no current flag/env override. RESEARCH_BRIEF.md updated in place: Open Question 2 resolved, new Open Questions 10-11 added on verbosity design, Constraints & Risks and Recommended Scope updated accordingly. Headless run, no web research needed.

## Notes

