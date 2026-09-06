# Research Brief — lf-120-agent-switch-refused-leaves-the-session

## Request

Investigate GitHub issue #120: "agent_switch_refused leaves the session stuck at
state=running". The reporter claims that when `prepare` refuses a turn because the
resolved coding agent does not own the session's segment (`agentSwitchRefusal`,
US-3 / AC-3.1), the session is left at `state='running'` forever, because nothing
ever moves it back to `idle` or `failed`. The reporter cites `internal/turn/prepare.go`
(session marked `running` ~line 111, agent-switch check returning an error ~line 157)
and `internal/turn/execute.go` (`agentSwitchRefused` ~line 573, with a comment
~line 565 claiming no session state was touched), argues both comments are
inaccurate, notes `Reconcile` does not clean this up (no run row, `lock_owner` NULL),
and expects the refusal to end the turn at `idle` with the inaccurate comments fixed
or the state write moved below the agent-switch check. This brief verifies those
claims against the current code, traces the full session-state lifecycle
(prepare → execute → harvest → reconcile), and surfaces the wider class of bug this
belongs to.

## Codebase Context

**Package shape.** `internal/turn/` is the per-comment turn lifecycle: `prepare.go`
(everything up to a runnable `agent.Request`), `execute.go` (the orchestration —
lock, prepare, dispatch, error routing, `Reconcile` entry point), `harvest.go`
(post-run bookkeeping, including the return to a terminal session state),
`report.go` (posting results/replies back to the issue), `errors.go` (the two
distinguished error types). `internal/store/sessions.go` and `internal/store/runs.go`
own the SQLite persistence for sessions, runs, and locks. [V]

**Session state model.** `store.SessionState` is a closed enum of three values —
`idle`, `running`, `failed` — validated by `(s SessionState) valid()`
(`internal/store/sessions.go:13-23`, `:252-259`). `SetSessionState`
(`internal/store/sessions.go:239-250`) is a bare, non-transactional
`UPDATE sessions SET state=…` with no compensating logic and no history table. [V]

**Full session.state lifecycle across a turn** [V]:

1. A new session starts at `idle` (`internal/store/sessions.go:103`).
2. `internal/turn/prepare.go:111` is the **only** place in the turn lifecycle that
   moves a session into `running` (`opts.DB.SetSessionState(ctx, session.ID, store.SessionRunning)`).
3. Normal completion returns the session to a terminal state in
   `internal/turn/harvest.go:417-424`: `state := store.SessionFailed; if result.Succeeded() { state = store.SessionIdle }`, then `SetSessionState`. This is the **only** path that ever sets a session back to `idle`.
4. The `*failure` error path (`internal/turn/errors.go:45`) is handled correctly:
   `prepareFailed` (`internal/turn/execute.go:546-560`) explicitly calls
   `SetSessionState(..., SessionFailed)` at `execute.go:554`.
5. The `*agentSwitchRefusal` error path (`internal/turn/errors.go:74`) is handled by
   `agentSwitchRefused` (`internal/turn/execute.go:573-582`), which sets only
   `result.Disposition`/`AgentOwning`/`AgentRequested` and logs — **it never calls
   `SetSessionState`**, on this path or any of its callers.
6. `lock_owner` is set even earlier than any of the above, in `Execute`
   (`AcquireSessionLock`, `execute.go:450`), before `prepare` is even invoked, and is
   released by an unconditional `defer ReleaseSessionLock` (`execute.go:474-479`) on
   **every** exit path of `Execute`, refusal included — so `lock_owner` is never
   stale in this scenario.
7. `Reconcile` (`internal/turn/execute.go:592`) wraps
   `store.DB.ReconcileOrphans` (`internal/store/runs.go:475-534`), invoked once at
   daemon startup. It has two independent halves in one transaction: (a) it selects
   `runs WHERE status='running'`, fails each such run, and fails **that run's
   session** as a side effect (`internal/store/runs.go:482-483, 506-510`); (b) it
   separately reclaims any `lock_owner` not equal to the current instance id
   (`:513-526`). Neither half inspects `session.state` directly or independently of
   a run row / a foreign lock owner.

**Order of side effects relative to the `running` write** [V]: worktree creation
(`prepareClone` → `ensureWorktree` at `prepare.go:101`, which calls
`git.EnsureWorktree`, `prepare.go:883-895` — a real `git worktree add`, not merely a
path computation) happens **before** `session.state` is set to `running`
(`prepare.go:111`), which happens before the agent-switch check
(`prepare.go:146-157`), which happens before the run row is allocated
(`allocateRun`, `prepare.go:192`).

## Domain / Problem Space

This is an internal state-machine correctness bug in the turn lifecycle, not a
domain/user-facing problem — no external research was needed and none was
performed. The design intent, per the doc comment at `internal/turn/errors.go:8-21`,
is a two-lane error taxonomy: a `*failure` is "reportable" and drives a session
state reset to `failed`; an "ordinary error" is meant only for things that happened
**before any mutation**, on the theory that it can simply be dropped and retried on
the next tick without cleanup. `*agentSwitchRefusal` is a third, special-cased type
that is reported (via `ReplyRequest`/`ReportReply`, not a run-report) but was
apparently designed under the same "nothing was mutated yet" assumption — which the
code no longer honors, because a worktree and a `running` state write both happen
before the segment-ownership check runs. [V]

### Verification of the reporter's claims

| Claim | Verdict | Evidence |
|---|---|---|
| Session marked `running` at prepare.go ~111, before the agent-switch check ~157 | **Accurate**, exact lines | `prepare.go:111` (`SetSessionState(..., SessionRunning)`) precedes `prepare.go:157` (`return nil, &agentSwitchRefusal{...}`) in the same function, nothing between them undoes it [V] |
| Worktree creation also happens before the refusal | **Accurate**, and understated — it happens even before the state write, not just before the refusal | `prepareClone`/`ensureWorktree`/`git.EnsureWorktree` complete at `prepare.go:101`, strictly before the state write at `:111` [V] |
| The two comments ("nothing was touched") are inaccurate | **Confirmed**, plus a third undisclosed instance | `prepare.go:150-156` is narrowly true about the segment/runs rows but falsely implies nothing at all was written above that point; `execute.go:565-572`'s claim that "`prepare` returned before creating anything" is factually wrong given the worktree + state write; a third, previously unflagged copy of the same claim exists at `internal/turn/report.go:317` inside `reportAgentSwitchRefusal`'s doc comment [V] |
| `Reconcile` doesn't clean this up because there's no run row and `lock_owner` is NULL | **Confirmed**, mechanism precisely identified | `ReconcileOrphans` only fails a session as a side effect of failing one of its `runs` rows with `status='running'` (`store/runs.go:482-483,506-510`); since `agentSwitchRefusal` returns before `allocateRun` (`prepare.go:192`) is ever reached, no run row exists to be found. `lock_owner` is correctly NULL (released by the unconditional defer), so the separate lock-reclaim half also does not and should not fire [V] |
| Expected fix: end the turn at `idle`; fix the comments or move the state write below the check | Open design question, not a verified fact — see Constraints & Risks and Open Questions | — |

### Confirmed bug mechanism

In order, one refused turn does: fetch → freeze slug → **create the worktree on
disk** (`prepare.go:101`) → write `worktree_path` if changed (`:105-110`) → **write
`session.state='running'`** (`:111`) → read `sha_before` (`:115`) → derive
`sessionRef` (`:127`) → `GetOrCreateSegment` returns the existing segment (`:146`) →
ownership mismatch detected → **return `*agentSwitchRefusal`, no run row allocated,
no state reset** (`:157`). `Execute` catches the typed error (`execute.go:499-502`)
and routes it to `agentSwitchRefused`, which updates only in-memory
`Disposition`/`AgentOwning`/`AgentRequested` fields and logs — it never calls
`SetSessionState`. The session is left with `state='running'`, a real worktree
present on disk, no `runs` row, and `lock_owner` correctly released. The
triggering comment is still consumed (via `reportAgentSwitchRefusal` →
`ReportReply`, `report.go:327-350`), so this is **not** a live/retrying hang — it is
a one-shot, permanent, display-level state inconsistency. [V]

**Practical severity note** [V]: nothing in `internal/daemon` gates dispatch of
future turns on `session.state` — only `lock_owner` plus the daemon's in-process
in-flight set do (comment at `execute.go:429-449`), and the lock is always released
by the unconditional defer. So a session stuck at `state='running'` does **not**
block future turns on the same task; it corrupts the operator-visible status shown
by `litefactory logs` (`internal/cli/cmd_logs.go:403`, which prints
`session.State` directly) without affecting scheduling. This does not reduce the
bug's validity but should inform how urgently/aggressively the fix is scoped.

## Constraints & Risks

- **The fix must decide between `idle` and `failed` deliberately, not by
  convenience.** The issue's expected-behavior text says `idle` (nothing was
  actually attempted against the target segment — the agent was simply the wrong
  owner). But every other "turn could not proceed" path in this codebase
  (`prepareFailed`) uses `failed`. Picking one by analogy to the wrong precedent
  would be an easy mistake; this needs an explicit decision recorded in the plan
  brief. [V][O — reporter's expectation, not yet verified as project-desired behavior]
- **This is a class of bug, not a single line fix.** The codebase-tour identified
  seven additional call sites in `prepare.go` that return a **plain**
  `fmt.Errorf`-wrapped error *after* the `running` state write at line 111, none of
  which are recognized as `*failure` by `Execute`'s error routing
  (`execute.go:492-508`), and all of which therefore fall through to
  `return result, err` with **no session-state cleanup of any kind** — not even the
  `failed` reset that `*failure` gets. These are strictly worse than the reported
  bug (they don't even get logged/reported the way the refusal does):
  - `internal/turn/prepare.go:129` — `naming.SessionRef` derivation failure
  - `internal/turn/prepare.go:148` — `GetOrCreateSegment` DB failure
  - `internal/turn/prepare.go:181` (via `previousRun`, error built at `:434`) — `ListRuns` DB failure
  - `internal/turn/prepare.go:194` (via `allocateRun`, errors at `:945-951`) — path-derivation or `AllocateRun` DB failure
  - `internal/turn/prepare.go:198` — `harness.MetaPath` derivation failure
  - `internal/turn/prepare.go:203` (via `commentHistory`, error at `:524`) — `ListComments` DB failure
  - `internal/turn/prepare.go:250` — `harness.BuildPrompt` failure

  [V] Whether these are in scope for this fix or a deliberate follow-up is a
  decision for Plan mode (see Recommended scope below) — but the reported bug
  should not be treated as an isolated one-off when it is one instance of a
  designed-but-unenforced invariant ("ordinary errors only happen pre-mutation")
  that the code has since drifted away from.
- **No `defer`/unwind mechanism exists anywhere in `prepare`/`Execute`** to
  compensate a partial state write on any exit path; the only safety net is
  `Reconcile`, which — as confirmed above — keys exclusively off run rows and
  foreign lock owners, never off `session.state` directly. Any fix that relies on
  `Reconcile` catching this later is a dead end; the fix has to live in
  `prepare`/`Execute` itself. [V]
- **No test currently exercises post-refusal session state.** A grep across
  `internal/turn/*_test.go` for `AgentSwitchRefus`/`switchRefus` found no test
  asserting on `session.state` after a refusal, though `TestExecuteRefusesA...`-style
  tests exist for other `*failure`-shaped refusals (`execute_test.go:516,545`). Any
  fix needs new test coverage; there is no existing test to simply extend. [V]

## Open Questions

1. Should the session-state target for an agent-switch refusal be `idle` (nothing
   was actually attempted against this segment — the reporter's expectation) or
   `failed` (consistent with every other "turn could not proceed" path via
   `prepareFailed`)? This is a product/architecture decision for Plan mode, not
   something the code itself resolves. [O]
2. Is `*agentSwitchRefusal` meant to become a third disposition that reuses the
   existing `prepareFailed`/`SetSessionState` machinery, or does its distinct
   reporting path (`ReplyRequest`/`ReportReply` instead of a run-report) require it
   to remain a separate type with its own explicit `SetSessionState` call? Either
   approach fixes the immediate bug; they differ in how much of `execute.go`'s
   error-routing gets touched. [V — for the mechanics] [O — for which approach is preferred]
3. Is the seven-site class of plain-`fmt.Errorf` post-mutation returns in
   `prepare.go` (listed under Constraints & Risks) in scope for this fix, or is it
   a deliberate, separately-tracked follow-up? Left for Plan mode to decide
   explicitly rather than silently bundling or silently dropping. [V]
4. Should the two (now three, including `report.go:317`) inaccurate comments be
   corrected regardless of which state-value decision is made, since they
   misdescribe current behavior either way? This seems close to uncontroversial
   but is listed as a question so Plan mode makes it an explicit task rather than
   an assumed side effect of the main fix. [V]
5. No web/library research applies to this bug — it is pure internal state-machine
   logic — so there is no `web-researcher` open item to flag here.

## Recommended scope for Plan mode

**In scope:**
- Fix the `*agentSwitchRefusal` path so a refused turn ends with `session.state` in
  a resolved terminal value (`idle` or `failed` — decision needed, see Open
  Question 1) instead of left at `running`.
- Correct the three inaccurate "nothing was touched" comments at
  `internal/turn/prepare.go:150-156`, `internal/turn/execute.go:565-572`, and
  `internal/turn/report.go:317` to reflect that the worktree and the `running`
  state write both happen before the refusal, regardless of which state-value fix
  is chosen.
- Add test coverage asserting on `session.state` after an `agentSwitchRefusal`,
  since none currently exists.

**Recommend Plan mode explicitly decide, rather than infer:**
- Whether the seven other plain-`fmt.Errorf` post-`running`-write paths in
  `prepare.go` are bundled into this fix (e.g., by tightening `Execute`'s error
  routing so any error after `prepare.go:111` gets a state reset by default) or
  tracked as a separate follow-up issue. Given they are the same bug class and
  arguably higher severity (no reset at all, not even `failed`), Plan mode should
  make an explicit call here rather than let scope creep in silently or let a
  known worse instance go unaddressed.

**Out of scope:**
- Any change to `lock_owner` handling or `Reconcile`'s run-row-based detection —
  both are confirmed working as designed for this scenario and are not implicated
  in the bug.
- Any change to dispatch/scheduling behavior — confirmed that `session.state` does
  not gate scheduling today, so this is a correctness/observability fix, not a
  behavior-under-load fix.
