# Plan Brief — agent_switch_refused leaves the session stuck at state=running
# Branch: lf/120-agent-switch-refused-leaves-the-session
# Date: 2026-09-06
# Parent issue: GitHub #120
# Citation base: ad15980
# Research brief: ./RESEARCH_BRIEF.md

## Shared Decisions

- **RQ-1 (state target on refusal): no session-state write is added at all.** An
  `agentSwitchRefusal` leaves `sessions.state` at whatever it already was — `idle`
  stays `idle`, `failed` stays `failed`. This is not a `failed` write and not an
  explicit `idle` write; it is the absence of any `SetSessionState` call on the
  refusal path. The precedent is the daemon's own label-driven refusals
  (`refuseMode`, `refuseTools`, `refuseAgent` in `internal/daemon`), which have
  **zero** `SetSessionState` call sites between them — not the `prepareFailed`
  pattern, which answers a different question (a turn that was supposed to run and
  didn't). Binds US-1, US-2, US-4.
- **RQ-2 (fix mechanism): one statement moves.** The single
  `SetSessionState(ctx, session.ID, store.SessionRunning)` call in
  `internal/turn/prepare.go` moves from its current position (line 111, immediately
  after the worktree is materialised) to the last statement before `prepare`
  returns its `*preparation` — after `permissions` is built, before the
  `logger.Info("run prepared", …)` line. `permissions` cannot fail, so this leaves
  **zero** error returns after the write. That single move is what closes both
  US-1 (the reported bug) and US-2 (the sibling bug class) — they are not two
  separate code changes. Binds US-1, US-2, and the regression tests in US-4.
- **RQ-2 addendum — the sibling-site count is 8, not 7.** The research brief's
  count of seven post-write error returns in `prepare.go` missed one:
  `opts.Runners.runner(kind)` at `prepare.go:159-169`. It is a `*failure`, routed
  to `prepareFailed`, so after the move it transitions `idle → failed` instead of
  `running → failed` — correct, unchanged behaviour, already covered by
  `TestExecuteFetchFailureAbortsWithNoWorktree`. This is a count correction only;
  no extra code. Binds US-2's scope statement and acceptance criteria.
- **RQ-3 (comment corrections): yes, and the corrected wording must be the same
  narrow invariant everywhere it appears.** "Nothing was created" is false both
  before and after this fix — the worktree is materialised and
  `sessions.worktree_path` is written before the refusal check runs, unconditionally.
  The true, narrower claim every corrected comment must state is: **no
  session-state transition has occurred and no `runs` row has been allocated; the
  worktree exists on disk and `sessions.worktree_path` records it, and both are
  idempotent and reused by the next turn.** Binds all three US-3 comment-fix tasks
  — write the same truth three times, not three different truths.
- **Accepted, self-healing consistency window.** Moving the write later means
  `allocateRun` (which inserts a `runs` row with `status='running'`) now happens
  *before* the session is marked `running`, creating a narrow window where a `runs`
  row reads `running` while its session still reads `idle`. This is accepted, not
  mitigated by new code: `ReconcileOrphans` already fails every orphaned `running`
  run *and* its session, in the same transaction, at daemon start, and nothing in
  the codebase gates control flow on `session.state`. No fault-injection seam, no
  extra transaction, and no new compensating logic are to be added for this.
  Binds US-1 and US-2 (the two stories that together constitute the write-move).

## Workflow

- **Shipping order:** US-1 first — it is the statement move itself and every other
  story depends on it landing before its own work is meaningful. US-2 (verification
  that the move closes all 8 sibling sites) and US-3 (comment corrections) and US-4
  (regression tests) can all proceed in parallel once US-1 lands; none of the three
  depends on the others.
- **Dependency graph:** US-1 → {US-2, US-3, US-4}. US-3 and US-4 are independent of
  each other. Nothing in this plan is a hard-coupled "ships together" pair beyond
  that fan-out — US-1 alone is a complete, mergeable fix; US-2/US-3/US-4 hardening
  and closing the loop around it.

## Cross-Story Constraints

- **Platform:** the entire change is confined to `internal/turn` (`prepare.go`,
  `execute.go`, `report.go`, their doc comments, and `execute_test.go`). No new
  package, no `go.mod` change, no schema change, no migration, no new
  `store.SessionState` value, no new `Disposition`, no new store accessor —
  `SetSessionState` is reused exactly as it exists today, only its call site moves.
  Binds US-1, US-2, US-3.
- **Platform:** the moved call keeps `ctx` as its first argument and must **not** be
  wrapped in `context.WithoutCancel` — that idiom is reserved for the post-cancellation
  cleanup paths (`execute.go:474-479`, `prepareFailed`) and using it on this write
  would make it survive a cancelled turn, which is wrong. Binds US-1 only in
  execution, but the constraint is what US-4's tests must not contradict.
- **Security:** the accepted consistency window (see Shared Decisions) is the one
  forward-looking risk this plan carries. No new input surface, no new SQL, no new
  concurrency exposure — `AcquireSessionLock` plus the daemon's in-flight set
  already exclude concurrent access to the same session, and the SQLite pool is
  capped at one connection so the single `UPDATE` cannot partially apply. Binds
  US-1, US-2.
- **Tests:** `go build ./...`, `go vet ./...`, `gofmt -l .`, and `make check`
  (which includes `-race`) must stay green through every story. New tests live
  in-package in `internal/turn/execute_test.go` (package `turn`) — this package
  forbids the `_internal_test.go` suffix. No `time.Sleep`/polling; use the existing
  `newFixture` real-SQLite fixture, never a fake store, for anything asserting on
  persisted state. Binds US-3 (its edits must not break existing tests) and US-4
  (which writes the new ones).
- **Ring-fenced (do not touch in this plan):** `lock_owner`, `AcquireSessionLock`,
  `ReleaseSessionLock`, `ReconcileOrphans`'s run-row/lock-based detection;
  `Execute`'s error routing (`execute.go:492-508`) and its `Disposition` set;
  `report.ReplyAgentSwitchRefused` and the issue-body skip behaviour; the
  daemon-level label-ladder refusals (`refuseMode`, `refuseTools`, `refuseAgent`),
  already confirmed correct; `internal/turn/doc.go` (its three-critical-section
  count is unchanged and needs no edit).

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Move the `running` state write so a refused turn leaves session state untouched | S | — | US-1.md |
| US-2 | Close the sibling bug class: 8 post-write error paths in `prepare` get the same guarantee | XS | US-1 | US-2.md |
| US-3 | Correct the inaccurate "nothing was touched" comments | S | US-1 | US-3.md |
| US-4 | Regression coverage for session-state-after-refusal and state-during-dispatch | M | US-1 | US-4.md |

## Out of Scope

- Adding an interface/fake seam for `store.DB` to fault-inject the 8 sibling error
  sites — they are unreachable without one, and none exists today; this plan does
  not add one.
- Orphaned `runs` rows left `status='running'` by errors *after* `allocateRun`
  (`prepare.go:194`, `:198`, `:203`, `:250`) — a real, pre-existing, unrelated gap
  that this fix does not make worse and does not close. **Action for the
  owner/team: file a separate follow-up issue for this** (no task in this plan
  covers it, and no tool available to this plan run can create it).
- Any change to `lock_owner` handling, `AcquireSessionLock`/`ReleaseSessionLock`,
  or `ReconcileOrphans`'s run-row-and-lock-based detection.
- Making `Reconcile` key off `session.state` directly, or gating dispatch/scheduling
  on `session.state` — confirmed today that nothing does, and this plan does not
  change that.
- Any new `SessionState` value, migration, or session-state history table.
- Removing or cleaning up the worktree on a refused turn — it is idempotent and
  reused by the next turn; deleting it would risk a concurrent/subsequent turn's
  legitimate reuse of it.
- Merging `agentSwitchRefusal` into `failure`/`prepareFailed`, or adding a new
  `Disposition`.
- Changing the refusal reply text, `report.ReplyAgentSwitchRefused`, or the
  issue-body skip behaviour.
- The daemon-level label-ladder refusals (`refuseAgent`, `refuseMode`,
  `refuseTools`) — already correct, not touched.

## Provenance

- Citation base: `ad15980`
- Known-stale areas: none identified — this is a same-day research → plan cycle
  on the current branch tip; the research brief's line citations were verified
  fresh by the architect review during this same run.
- Re-pinning requirements: if implementation is not started promptly from this
  branch tip, re-verify the exact line numbers cited in each sub-issue
  (`prepare.go:111`, `:150-156`, `:159-169`, `:263-271`, `:273`; `execute.go:540-561`,
  `:565-572`, `:492-508`; `report.go:315-319`) before coding, since they are
  load-bearing for the task descriptions.
