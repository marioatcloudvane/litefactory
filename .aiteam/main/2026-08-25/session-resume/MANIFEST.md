# Manifest
# Branch: main
# Session: 2026-08-25

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — promoted from a spike, not a fresh Research pass)*
- [ ] **Plan** — `IMPLEMENTATION_PLAN.md` *(in progress)*
- [ ] **Implement** — code + tests *(not started)*

## History

- 2026-08-25 — Session created. Grew out of a spike asking whether a run should
  see the whole comment thread rather than only the comment that triggered it.
- 2026-08-25 — Spike found there is no `--resume` anywhere in the tree: every run
  is a cold `claude -p`, and every run of a task reuses the session id derived
  from `store.FirstSegmentOrdinal` (= 1) without ever resuming it.
- 2026-08-25 — Owner chose Plan mode directly over Research, with the explicit
  instruction "don't over-complicate this". Research gate satisfied by recording
  the spike findings as `RESEARCH_BRIEF.md`.

## Notes

- Owner directives: resume between runs; relabel prior owner comments
  (`old_owner`) so historic commands do not read as live ones; **prompt injection
  explicitly deferred**; compact the history with the latest comment verbatim.
- Design ordering established in the brief: **resume primary, compaction
  fallback.** Reversing it means paying for a summarizer on every run.
- Two unknowns are untested and gate the design: whether `claude -p` resumes on a
  reused `--session-id`, and whether the session file survives between runs.
- Sibling session `fix-comments-pickup` (same branch and date) is a separate
  defect; it also owns `daemon/tick.go`, so watch for collisions if both reach
  Implement.
