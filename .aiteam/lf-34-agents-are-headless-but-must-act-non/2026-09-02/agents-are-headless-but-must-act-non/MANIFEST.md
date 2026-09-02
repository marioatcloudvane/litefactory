# Manifest
# Branch: lf/34-agents-are-headless-but-must-act-non
# Session: 2026-09-02

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-02 — session created, run started from issue #34 comment ("do it"),
  no command named, classified as Research mode.
- 2026-09-02 — Research mode completed. codebase-tour traced the headless/asking
  mechanism through internal/harness/prompt.go, internal/turn/ask.go,
  internal/report/run.go, and internal/github/comments.go. RESEARCH_BRIEF.md
  written with findings, constraints, and 6 open questions for Plan mode
  (chiefly: is this a documentation fix or a behavior change to the
  opt-in/off-by-default asking gate).
- 2026-09-02 — Follow-up Research run: owner answered all 6 open questions on
  the issue. Resolved as a documentation/prompt-clarity fix (no change to
  modeMayAsk / lf: ask gating). A second, full-file codebase-tour pass audited
  all 15 .claude/agents+modes files for headless/asking self-awareness;
  confirmed requirements-engineer.md and implement-orchestrator.md's Phase 2
  read as live-dialogue scripts rather than stop-and-surface instructions.
  RESEARCH_BRIEF.md rewritten in place: prior open questions 5 and 6 (tool
  labels, ARCHITECTURE.md) dropped as out of scope per owner; 5 new, narrower
  open questions from the audit carried forward for Plan mode. Manifest not
  re-ticked — Research was already marked done; this is an update within the
  same completed mode, not a new mode entry.

## Notes

