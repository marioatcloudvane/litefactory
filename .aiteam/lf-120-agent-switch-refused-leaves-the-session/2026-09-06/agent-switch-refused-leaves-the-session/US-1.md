# US-1 — Move the running-state write so a refused turn leaves session state untouched

> As an operator, I want a turn refused for agent-switch to leave the task's state
> exactly as it was before the turn, so `litefactory logs` does not show a
> permanently `running` task that no process is working on.

Moves the single `sessions.state = running` write in `internal/turn/prepare.go`
from the middle of turn preparation to the very end of it, so a refused turn never
passes through it at all.

## Why This Matters

When a comment is labelled for the wrong coding agent, litefactory correctly
refuses to run it — but today it marks the task as `running` in its own records
moments before making that decision, and then never corrects the record. The task
sits showing `running` forever in `litefactory logs`, even though nothing is
running and nothing ever will be until someone posts a new comment with the right
label. An operator watching that output has no way to tell a genuinely stuck task
from this cosmetic one, which is exactly the kind of false signal that erodes
trust in the tool's status output. The fix is a single write moved to the end of
the process that produces it, which is cheap, but it is also the fix for a whole
family of the same mistake — see US-2 — so getting its exact placement right here
is what makes the rest of the class close "for free."

## Architecture Notes

- The write (`SetSessionState(ctx, session.ID, store.SessionRunning)`, currently
  `prepare.go:111`) moves to the last statement before `prepare` returns its
  `*preparation` — after the `permissions` construction (`prepare.go:263-271`,
  which cannot fail), immediately before `logger.Info("run prepared", …)` at
  `:273`. The in-memory `session.State = store.SessionRunning` assignment moves
  with it, so `prep.session` matches the row.
- A refusal (`*agentSwitchRefusal`) gets **no** `SetSessionState` call added
  anywhere. The session is left exactly as it was — `idle` stays `idle`, `failed`
  stays `failed`. This is the daemon's own label-refusal precedent
  (`refuseMode`/`refuseTools`/`refuseAgent` in `internal/daemon`, which have zero
  `SetSessionState` call sites), not the `prepareFailed`/`SessionFailed` pattern —
  that pattern answers "a run was supposed to happen and didn't," which is not
  this case.
- Platform: keep `ctx` as the call's first argument (CONC-5). Do **not** wrap it in
  `context.WithoutCancel` — that idiom is reserved for post-cancellation cleanup
  (`execute.go:474-479`, `prepareFailed`), and using it here would let the write
  survive a cancelled turn, which is wrong: this write runs on the live request
  path and a cancelled context should abort it.
- Risk: moving the write later means `allocateRun`'s `runs`-row insert (and the
  attachment-ingestion/manifest writes) now happen before the session is marked
  `running`, opening a narrow window where a `runs` row reads `running` while its
  session still reads `idle`. This is accepted — `ReconcileOrphans` already fails
  every orphaned `running` run and its session together at daemon start, and
  nothing in the codebase gates control flow on `session.state`. No new
  compensating code is added for this window; document why in the invariant
  comment (see Tasks) rather than coding around it.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Move the `SetSessionState(SessionRunning)` write and its paired in-memory `session.State` assignment from `prepare.go:111` to the last statement before `return &preparation{…}` (after `permissions`, before the `run prepared` log line); keep the existing error wrap `fmt.Errorf("mark task %s running: %w", ref, err)` verbatim | logic | — | S |
| T-1.2 | Record the "state write is last" invariant in `prepare`'s doc comment (after its existing seven-step/error-taxonomy paragraph, same register) and in a short comment at the moved statement explaining why it must stay last; do not restructure the doc comment's step enumeration and do not add a session-state section to `internal/turn/doc.go` | logic | T-1.1 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | `internal/turn/prepare.go` contains exactly one `SetSessionState(..., store.SessionRunning)` call, and it is the last store write before `prepare` returns its `*preparation`; no `return nil, err` statement follows it. |
| AC-1.2 | `internal/turn/execute.go`'s `agentSwitchRefused` contains no `SetSessionState` call, and `Execute`'s error routing at `execute.go:492-508` is unchanged in behaviour — no new branch, no new `Disposition`. |
| AC-1.3 | `prepare`'s doc comment states the invariant that the `running` write is the last thing `prepare` does, so every error return above it leaves `sessions.state` untouched, and explains that this cannot be enforced structurally by a test — only read. |
