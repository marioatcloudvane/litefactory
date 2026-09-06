# Drift Report — agent_switch_refused leaves the session stuck at state=running
# Branch: lf/120-agent-switch-refused-leaves-the-session | Date: 2026-09-06

## Delivered as planned

- **US-1** — The sole `SetSessionState(ctx, session.ID, store.SessionRunning)`
  call in `internal/turn/prepare.go` was moved from immediately after worktree
  materialisation to the last statement before `prepare` returns its
  `*preparation` (immediately before the `"run prepared"` log line), exactly as
  T-1.1 specified. The paired in-memory `session.State` assignment moved with
  it. The existing error wrap (`fmt.Errorf("mark task %s running: %w", ref,
  err)`) was kept verbatim.
- **US-1** — `prepare`'s doc comment and a comment at the moved call site now
  state the "write is last" invariant, including the note that it cannot be
  enforced structurally by a test, only read — matching T-1.2/AC-1.3.
- **US-1** — `agentSwitchRefused` still has no `SetSessionState` call, and
  `Execute`'s error routing (`execute.go:492-508`) is unchanged in behaviour —
  AC-1.2 confirmed by both the implementation engineer and the code reviewer.
- **US-2** — Verified by reading (no code change) that all sibling error-return
  sites in `prepare` sit before the relocated write and either leave state
  untouched or route through the unchanged `prepareFailed` → `SessionFailed`
  path, per T-2.1. The runner-lookup site (`opts.Runners.runner(kind)`) was
  confirmed to still correctly end at `state='failed'`, not "fixed" to leave
  state untouched — AC-2.2 held deliberately.
- **US-3** — All three specified comments (`prepare.go`'s refusal-check
  comment, `agentSwitchRefused`'s doc comment in `execute.go`,
  `reportAgentSwitchRefusal`'s comment in `report.go`) were corrected to state
  the same narrow invariant: no session-state transition occurred, no `runs`
  row was allocated, and the worktree/`sessions.worktree_path` are idempotent
  and reused by the next turn. `report.go`'s comment kept its true "no run
  allocated" half per T-3.3. `internal/turn/errors.go` and `doc.go` were left
  untouched as instructed.
- **US-3 (T-3.4)** — `prepareFailed`'s doc comment was also corrected, scoping
  its "no run row was allocated" claim to failures raised before `allocateRun`
  rather than stating it unconditionally. This was flagged in the plan as an
  unconfirmed assumption pending human approval; per this run's explicit
  dispatch instruction it was treated as already-approved scope and delivered.
- **US-4** — Three new tests were added to `internal/turn/execute_test.go`
  (`TestExecuteRefusesAnAgentSwitchWithoutTouchingSessionState`,
  `TestExecuteRefusesAnAgentSwitchWithoutClearingAFailure`,
  `TestExecuteMarksTheSessionRunningForTheWholeDispatchWindow`), covering
  AC-4.1–AC-4.3. AC-4.4 was satisfied by citing two pre-existing tests
  (`TestExecuteReleasesTheLockOnAFailedRun`, `TestExecuteFullTurnLeavesTheCloneUntouched`)
  rather than adding a duplicate, per the story's explicit preference to extend
  over add. All new/cited tests assert against real SQLite-backed state via
  `f.reloadSession()`/`f.runs()`, never in-memory structs.
- **US-4 (T-4.5)** — `go build ./...`, `go vet ./...`, `gofmt -l .` and the full
  `go test -race ./...` gate all ran clean, independently re-verified in Phase 3
  (108 tests passed in `internal/turn`, 22 packages green repo-wide, 84.0%
  package coverage).
- **Scope confinement** — the change touched exactly the four files the plan
  named (`internal/turn/{prepare.go, execute.go, report.go, execute_test.go}`)
  and nothing else; no new package, `go.mod` change, schema change, migration,
  `SessionState` value, `Disposition`, or store accessor was introduced.
- **Ring-fenced areas** — `lock_owner`, `AcquireSessionLock`/`ReleaseSessionLock`,
  `ReconcileOrphans`'s detection logic, `Execute`'s error routing and
  `Disposition` set, `report.ReplyAgentSwitchRefused`, the daemon-level
  label-ladder refusals, and `internal/turn/doc.go` were all confirmed
  untouched.

## Added scope

- None beyond the plan-recorded T-3.4 assumption above, which was already
  anticipated in the plan brief itself as a likely inclusion.

## Dropped or deferred

- None. All tasks across all four stories (T-1.1–T-1.2, T-2.1, T-3.1–T-3.4,
  T-4.1–T-4.5) were completed.

## Architectural deviations

- **US-1's in-memory `session.State` assignment.** The story's architecture
  notes assumed a paired in-memory assignment already existed at the old call
  site; none did. The implementation engineer added
  `session.State = store.SessionRunning` at the new site, using the existing
  `SetSessionWorktree`/`session.WorktreePath` pairing pattern as precedent.
  This is a minor gap-fill consistent with AC-1.1's intent, not a deviation
  from the plan's design, and was confirmed correct by the code reviewer.
- **Sibling-site count: 9 observed vs. 8 named in the plan.** The code
  reviewer's independent enumeration of `prepare`'s post-write-relevant error
  sites found 9 (including `worktree.RevParse` at `prepare.go:121-124`, a
  `*failure` site not named among the plan's 8), against the plan's corrected
  count of 8. This is a documentation/count discrepancy only — `RevParse`
  already correctly routes through `prepareFailed` under the existing,
  unchanged pattern, exactly like the other `*failure` sites, so no behaviour
  differs based on which number is used. Noted in `notes.md`.

## Verdict

CLEAN — delivered matches plan within acceptable micro-decisions (the
in-memory `session.State` assignment gap-fill and the 8-vs-9 sibling-site
count, both confirmed by the code reviewer as non-defects). No follow-up
planning cycle is needed for this issue. The plan's own Out of Scope section
already recommends a separate follow-up issue for orphaned `runs` rows left
`status='running'` by errors after `allocateRun` — that recommendation stands
unchanged and was not something this run was scoped to act on.
