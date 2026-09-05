# Manifest
# Branch: lf/89-logging-is-only-on-issues-but-not-on-the
# Session: 2026-09-05

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(done)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-05 — session created
- 2026-09-05 — Research completed: RESEARCH_BRIEF.md written after codebase-tour of internal/cli (logs command, router/usage handling) and internal/daemon (tick loop) and internal/logging (daemon.log). Headless run, no web research needed.
- 2026-09-05 — Research follow-up: incorporated reporter clarification (bare `logs` must match `daemon run`'s console output exactly; verbosity-level keyword requested). Second codebase-tour confirmed console output is plain `slog.TextHandler` text (distinct from `daemon.log`'s JSON) and found an existing but CLI-invisible `log_level` config feature (`internal/config/config.go`, `internal/logging.ParseLevel`) with no current flag/env override. RESEARCH_BRIEF.md updated in place: Open Question 2 resolved, new Open Questions 10-11 added on verbosity design, Constraints & Risks and Recommended Scope updated accordingly. Headless run, no web research needed.
- 2026-09-05 — Plan completed, headless run (no synchronous user available). requirements-engineer wrote `feature-spec.md` (9 user stories, US-1..US-9) from the research brief. go-architect wrote `ARCH_REVIEW.md`: verdict APPROVED WITH CONDITIONS, cross-story decisions D-1..D-10, two recorded deviations (DEV-1: `daemon run --level` needs a narrow, documented early-argv-read in `main.go` since the logger is built before command dispatch, resolution assigned to US-6; DEV-2: two `router_test.go` cases pinning today's bare-`logs`-is-an-error behavior are deliberately updated as part of US-4, `parse_test.go`'s generic arity fixture is untouched), and per-story architectural hints. Synthesised into `plan_brief.md` (Shared Decisions, Workflow/shipping order, Cross-Story Constraints, Story Index, Out of Scope, Provenance, headless-run assumptions) plus one sub-issue per story (`US-1.md`..`US-9.md`) carrying each story's own Architecture Notes, Tasks and Acceptance Criteria. Per the task's explicit headless-run instruction, every open question the research brief and requirements engineer left unresolved was settled by taking the brief's own stated recommended/lowest-effort default rather than blocking; the two most consequential — bare `litefactory logs` moving from a usage error (exit 2) to a successful app-log view (exit 0), and `daemon.log` being read as the whole process's log rather than something narrower — are called out in `plan_brief.md`'s Headless-run assumptions section. Story Index's Sub-issue links are intentionally left for litefactory's own sub-issue-creation step, which runs after this Plan run from the per-story `.md` files written here.

## Notes

