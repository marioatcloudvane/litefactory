# Manifest
# Branch: main
# Session: 2026-08-25

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — promoted from a spike, not a fresh Research pass)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done — 3 stories, 20 tasks)*
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
- 2026-08-25 — Plan done. Both of the brief's unknowns were answered empirically during planning, so
  the plan has no branch points. Headline finding: **reusing `--session-id` exits 1 ("already in
  use"), so the second run of any task is broken today** — it has never fired only because no task
  has ever had a second run. `--resume` composes with the whole existing flag set. Transcripts live
  outside the worktree and survive its deletion. Three stories, 20 tasks, no migration, `tick.go`
  untouched.

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
