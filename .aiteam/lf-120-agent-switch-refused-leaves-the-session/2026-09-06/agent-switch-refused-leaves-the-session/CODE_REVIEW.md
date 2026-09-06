# Code Review
# Feature: agent_switch_refused leaves the session stuck at state=running (issue #120)
# Reviewed by: go-code-reviewer
# Date: 2026-09-06
# Status: CLEAN

---

## Scope

Reviewed files (all uncommitted in the worktree, per `git diff --stat`):

- `internal/turn/prepare.go` (+39/-13 vs. `git diff`)
- `internal/turn/execute.go` (comment-only diff, +11/-9)
- `internal/turn/report.go` (comment-only diff, +6/-3)
- `internal/turn/execute_test.go` (+106, new tests only, no existing test bodies changed)

Verification performed beyond reading: `git diff` on all four files (byte-level,
not just visual comparison against the engineer summaries), `gofmt -l
internal/turn/`, `go vet ./internal/turn/...`, `go build ./...`, and `go test
./internal/turn/... -race -run 'TestExecute|TestReconcile' -v` (full suite for
the package, all tests including the four pre-existing tests called out as
regression guards). All green; no `golangci-lint` binary available in this
environment to run the fuller lint set, but `go vet` surfaced nothing and the
diff introduces no new imports, goroutines, or exported surface for staticcheck
to plausibly flag.

## AC verification

**US-1 (AC-1.1/1.2/1.3) — the write moves to last.** Confirmed by diff: the
`opts.DB.SetSessionState(ctx, session.ID, store.SessionRunning)` call was
removed from its original position (immediately after `session.WorktreePath =
worktree.Path()`, before `RevParse`) and is now the last store write in
`prepare()`, immediately before the `logger.Info("run prepared", ...)` line and
the function's success return. Only one `SetSessionState` call exists in the
file. The call uses plain `ctx`, not `context.WithoutCancel` — the platform
constraint holds. No `return` of any kind occurs between the write and the end
of the function. `agentSwitchRefused` in execute.go contains no
`SetSessionState` call in its body (diff confirms only its doc comment
changed). AC-1.1, AC-1.2, AC-1.3 satisfied.

**US-2 (AC-2.1/2.2) — every failure path above the write leaves state
untouched.** Enumerated every return site in `prepare()` between the write's
old position and its new position: `RevParse`, `naming.SessionRef`,
`GetOrCreateSegment`, the agent-switch-refusal branch, the runner-registry
lookup, `previousRun`, `allocateRun`, `harness.MetaPath`, `commentHistory`, and
`harness.BuildPrompt` — all ten sit strictly before the moved write, so all of
them now correctly leave `sessions.state` as found rather than stuck at
`running`. (Note: the US-2 engineer report names eight of these explicitly and
omits `RevParse`; `RevParse` failure sat after the *original* write position
too, per the diff, so it was an equally real ninth sibling of the same bug
class. This is a gap in the report's enumeration, not in the code — the fix's
actual placement, last in the function, covers `RevParse` along with the rest.
Not a finding against the code; noted for completeness only.) AC-2.2's
requirement that a runner-lookup failure still lands the session at `failed`
holds: the lookup miss returns `failuref(...)`, a `*failure`, which
`Execute`'s unmodified `errors.As(err, &prepFailure)` branch (execute.go:492-508)
routes into `prepareFailed`, which still calls `SetSessionState(...,
SessionFailed)`.

**US-3 (AC-3.1-3.4) — the refusal path and its wording.** `agentSwitchRefused`
transitions no session state and allocates nothing; the switch-refusal branch
in `prepare()` returns before `allocateRun`. Three sites carry the same
invariant sentence verbatim: `prepare.go`'s refusal-check comment, execute.go's
`agentSwitchRefused` doc comment, and report.go's `reportAgentSwitchRefusal`
doc comment all read "No session-state transition has occurred and no runs row
has been allocated; the worktree already exists on disk and
sessions.worktree_path already records it, and both are idempotent and reused
by the next turn." word for word. AC-3.4 satisfied for these three.
`prepareFailed`'s doc comment (execute.go) is deliberately different — it says
"For a failure raised before allocateRun, no run row was allocated..." rather
than the shared sentence, and that divergence is correct rather than a defect:
`prepareFailed` *does* transition session state to `failed`, so claiming "no
session-state transition has occurred" in that comment would be false. Making
all four byte-identical would introduce an inaccurate doc comment. Treating
this as satisfying AC-3.4's intent rather than violating it. `report.go`'s
issue-body skip behavior and `report.ReplyAgentSwitchRefused` reply-shape logic
are unchanged (diff touches only the doc comment, not the function body).

**US-4 (AC-4.1-4.5) — tests pin what US-1/US-3 changed.**
`TestExecuteRefusesAnAgentSwitchWithoutTouchingSessionState` (AC-4.1) and
`TestExecuteRefusesAnAgentSwitchWithoutClearingAFailure` (AC-4.2) both read
session state back from the real SQLite fixture (`f.reloadSession().State`),
not the in-memory `Result`, and both assert zero allocated runs.
`TestExecuteMarksTheSessionRunningForTheWholeDispatchWindow` (AC-4.3) has the
stub runner read `f.reloadSession().State` *during* its own execution and
asserts it observes `store.SessionRunning` in the database at that moment —
this is the strongest possible regression pin for the bug, since it fails
under the old "if the write happens too early, it can still coincidentally be
`running` by the time the runner checks" case as well as under the pre-fix
window being open before the write happens at all. Pre-existing
`TestExecuteReleasesTheLockOnAFailedRun` and
`TestExecuteFullTurnLeavesTheCloneUntouched` are present, unmodified in body,
and still assert `SessionFailed`/`SessionIdle` respectively (AC-4.4). Ran the
full `internal/turn` suite for these tests under `-race`: all pass, no data
races reported. `gofmt -l`, `go vet ./internal/turn/...`, and `go build ./...`
all clean (AC-4.5).

## Ring-fenced areas — confirmed untouched

- `lock_owner`, `AcquireSessionLock`/`ReleaseSessionLock`: no diff hunk touches
  any lock-related line; call sites in execute.go are outside all diff hunks.
- `ReconcileOrphans`'s run-row/lock-based detection and `Reconcile()`: no diff
  hunk in execute.go touches this function; only referenced descriptively in
  prepare.go's new comment as justification for the accepted consistency
  window, not modified.
- `Execute`'s error routing (execute.go:492-508) and its `Disposition` set: the
  `git diff` for execute.go touches only the doc comments of `prepareFailed`
  and `agentSwitchRefused`, nothing in the routing block itself, and no new or
  removed `Disposition` values.
- `report.ReplyAgentSwitchRefused` and issue-body skip behavior: report.go's
  diff touches only `reportAgentSwitchRefusal`'s doc comment; the function body
  (the `TriggerCommentID == 0` branch and the `ReportReply` call) is unchanged.
- Daemon-level label-ladder refusals: `git diff --stat internal/daemon/`
  returns nothing — the daemon package is untouched.
- `internal/turn/doc.go`: `git diff --stat internal/turn/doc.go` returns
  nothing — untouched.

## Cross-story checks

- No new concurrency introduced: no new goroutines, channels, mutexes, or
  `context.WithoutCancel`/`WithTimeout` usages appear in any of the four
  diffs. The one existing `context.WithoutCancel` in `prepareFailed` predates
  this change and is out of scope (it is not the constrained call).
- No new error handling patterns, no swallowed errors, no new resources
  acquired (files, connections, response bodies) — the diffs are entirely a
  statement reorder plus doc comments plus new test functions using existing
  fixture helpers.
- No security-relevant surface touched: no new SQL, no new untrusted input
  parsing, no new file/path handling.
- Idiom/style: `gofmt`/`go vet` clean; doc comments follow the codebase's
  established dense-prose convention and each starts with the identifier name
  where required (`prepareFailed`, `agentSwitchRefused`,
  `reportAgentSwitchRefusal` all already did, unchanged in that respect); no
  `TODO`/`FIXME`/dead code introduced.
- Test-file scope: `execute_test.go`'s diff is additive only — no existing
  test body was altered, consistent with the "existing tests still prove
  X" requirement in AC-4.4 rather than a rewrite that could quietly change
  what they assert.

---

## BLOCKING findings

None.

## ADVISORY findings

None.

---

## Cross-story summary

This is a single-package, four-file fix that reorders one statement, updates
four doc comments to state the resulting invariant precisely, and adds three
new regression tests plus a shared slug-pattern test helper reference. There
is no cross-story boundary in the usual sense (no new interfaces, no new
callers in other packages) — the risk here was entirely about ordering
correctness within `prepare()` and about the four scattered comments staying
consistent with each other and with the code, and both were verified directly
against the diff rather than inferred from descriptions. The accepted
consistency window (a runs row and attachment manifest can be observed before
its session is marked running) is explicitly documented at the write site and
is consistent with `ReconcileOrphans`'s existing orphan-detection behavior,
which was independently confirmed untouched. The one loose end — the US-2
report's "8 sibling sites" undercounting `RevParse` by one — has no bearing on
the code's correctness, since the fix's placement (last statement in the
function) covers every prior return site regardless of how they were counted
in the report.

---

## Verdict

CLEAN — No findings. Proceeding to environment setup.
