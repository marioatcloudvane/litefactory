# US-3 — Correct the inaccurate "nothing was touched" comments

> As a maintainer, I want the code comments that claim "nothing was created / no
> session state was touched" on a refused turn to describe what the code actually
> does, so the next reader is not misled into re-introducing this bug.

Rewrites three (four, per an added assumption below) existing comments that
currently overstate how clean a refused turn is, replacing each with the same
precise, narrower claim that is actually true.

## Why This Matters

Part of why this bug existed at all is that the code already had comments
asserting the wrong thing was safe — comments telling a future reader "nothing was
created here" at exactly the point where, in fact, a worktree had already been
created on disk and a database column had already been written. A maintainer
reading those comments in good faith would reasonably conclude a refusal has no
side effects worth worrying about, which is false, and is the kind of false
confidence that produces the next version of this same bug. Correcting the wording
is zero-risk — comments do not execute — but it is not optional: leaving a comment
that documents a bug as if it were intended behaviour is worse than leaving no
comment at all.

## Architecture Notes

- All corrected comments must state the same narrow, true invariant, not three
  different approximations of it: **no session-*state* transition has occurred and
  no `runs` row has been allocated; the worktree exists on disk and
  `sessions.worktree_path` records it, and both are idempotent and reused by the
  next turn.** The naive fix — "prepare returned before creating anything" — is
  still false after this change, because the worktree and `sessions.worktree_path`
  are written before the refusal check runs, unconditionally, whether or not this
  plan's other stories have landed.
- The three sites: `prepare.go:150-156` (at the refusal check itself),
  `execute.go:565-572` (`agentSwitchRefused`'s doc comment), `report.go:315-319`
  (`reportAgentSwitchRefusal`'s comment). The `report.go` comment keeps its true
  half — "no run row was allocated, so this takes the `ReplyRequest`/`ReportReply`
  shape rather than the run-report one" — and only the false blanket claim is
  removed or corrected.
- Do not touch `internal/turn/errors.go:8-21` (the error-taxonomy doc comment) — it
  becomes true again as a side effect of US-1's write move and needs no edit.
- **Assumption recorded here (no owner was available to confirm this call in this
  headless run):** a fourth comment, `prepareFailed`'s doc comment at
  `execute.go:540-561` ("No run row was allocated, so there is no ordinal to
  burn"), is independently false today for any `*failure` raised at or after
  `allocateRun`, and stays false regardless of this plan. It was flagged but not
  mandated by the architecture review, on the reasoning that this plan's changes
  don't make it any less true, so STYLE-1's "your change made this comment untrue"
  trigger doesn't strictly apply. This plan includes fixing it anyway (T-3.4)
  because the cost is one line in a file this story is already opening, and
  leaving a known-false comment sitting next to three just-corrected ones in the
  same package is a poor trade. If this call is wrong, drop T-3.4 and file the
  fourth comment under the same follow-up issue this plan already recommends for
  orphaned `runs` rows (see `plan_brief.md`'s Out of Scope) — the two share a root
  cause.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | Correct the agent-switch refusal comment at `prepare.go:150-156` to the narrow invariant (no state transition, no `runs` row; worktree exists on disk and is reused by the next turn) | logic | US-1 / T-1.1 | XS |
| T-3.2 | Correct `agentSwitchRefused`'s doc comment at `execute.go:565-572` to the same narrow invariant | logic | US-1 / T-1.1 | XS |
| T-3.3 | Correct `reportAgentSwitchRefusal`'s comment at `report.go:315-319` — keep the true "no run row allocated" half, drop or correct the false blanket "before anything was created" half | logic | US-1 / T-1.1 | XS |
| T-3.4 | (Assumption — see Architecture Notes) Correct `prepareFailed`'s doc comment at `execute.go:540-561` ("No run row was allocated, so there is no ordinal to burn") to state that this only holds for failures raised before `allocateRun`, not unconditionally | logic | US-1 / T-1.1 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | `prepare.go:150-156`'s comment no longer claims "nothing above this point wrote anything the switch needs to undo"; it states that the worktree exists on disk and that no session-state write has happened yet. |
| AC-3.2 | `execute.go:565-572`'s comment no longer claims "`prepare` returned before creating anything"; it states that the worktree exists and that the session state was deliberately never written. |
| AC-3.3 | `report.go:315-319`'s comment no longer makes the blanket claim "prepare returned before anything was created"; it keeps the true "no run was allocated" half and states the narrow invariant in place of the false half. |
| AC-3.4 | All three corrected comments state the same invariant (matching wording, not three different phrasings of it). |
| AC-3.5 | (If T-3.4 is kept) `prepareFailed`'s doc comment at `execute.go:540-561` no longer states unconditionally that no run row was allocated; it scopes the claim to failures raised before `allocateRun`. |
