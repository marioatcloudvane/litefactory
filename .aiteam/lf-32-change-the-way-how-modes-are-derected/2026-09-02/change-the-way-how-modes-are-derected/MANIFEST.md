# Manifest
# Branch: lf-32-change-the-way-how-modes-are-derected
# Session: 2026-09-02

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(completed 2026-09-02)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(completed 2026-09-02)*
- [x] **Implement** — code + tests *(completed 2026-09-02)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-02 — session created; Research completed
- 2026-09-02 — Plan completed with five implementation stories
- 2026-09-02 — Implement completed: daemon-owned routing, default-agent
  classification, durable one-shot mode labels, and cleanup recovery

## Notes

- The issue title's spelling is retained in the supplied feature slug.
- The full check reached the race suite successfully, where two existing
  macOS process-tree fixtures could not observe child processes in this
  sandbox. All other packages passed, and the pinned lint reported zero issues.
