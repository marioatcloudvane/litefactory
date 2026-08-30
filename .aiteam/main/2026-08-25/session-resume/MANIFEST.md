# Manifest
# Branch: main
# Session: 2026-08-25

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — promoted from a spike, not a fresh Research pass)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done — 3 stories, 20 tasks)*
- [x] **Implement** — code + tests *(done — 3 stories, 20 tasks, green; committed and pushed as `edf82e7`)*

Artifacts: `CLAUDE_CLI_CONTRACT.md`, `CODE_REVIEW.md`, `DRIFT_REPORT.md`, `notes.md`.

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

- 2026-08-26 — Implement done. Three stories landed strictly serially (S1 → S2 → S3). Code review
  verdict **ADVISORY_ONLY**, no blocking findings; three advisories recorded in `notes.md`, one (a
  doc-comment typo) fixed. An independent AC-coverage pass added zero tests and mutation-verified the
  AC-SR-2.2 regression goes red against the pre-change behaviour. **Test results: all 18 packages
  pass under `-race`, 0 failures, no escalation.** `make check` exit 0 — gofmt clean, `go vet` clean,
  golangci-lint v2.13.1 `0 issues`. Drift verdict **CLEAN**.
- 2026-08-26 — One correction to the plan: its resume-refusal rule (`Events == 0`) would never have
  fired — a refusal emits one terminal `result` event. Implemented as
  `resuming && Outcome == OutcomeFailed && Model == "" && ExitCode > 0`. The empirical contract that
  S1 exists to pin is what caught it. Contract re-verified against `claude 2.1.246` (plan probed
  `2.1.245`; behaviour identical).
- 2026-08-26 — `internal/daemon/tick.go` ends byte-identical, so **no collision with the sibling
  `fix-comments-pickup` session**. Committed and pushed by the owner as `edf82e7` on 2026-08-26, fast-forward onto `origin/main`.

## Notes

- Owner directives: resume between runs; relabel prior owner comments
  (`old_owner`) so historic commands do not read as live ones; **prompt injection
  explicitly deferred**; compact the history with the latest comment verbatim.
- Design ordering established in the brief: **resume primary, compaction
  fallback.** Reversing it means paying for a summarizer on every run.
- ~~Two unknowns are untested and gate the design~~ — both answered empirically during Plan and
  re-verified during Implement. See `CLAUDE_CLI_CONTRACT.md`.
- Sibling session `fix-comments-pickup` (same branch and date) is a separate
  defect; it also owns `daemon/tick.go`. **No collision occurred** — `tick.go` ends byte-identical.
- `.aiteam/APP_CONTEXT.md` still does not exist for this repo. The `app-context-updater` agent named
  in the roster was not available in the Implement runtime, so it was not created. Left for the
  owner; it is not a prerequisite for anything delivered here.
