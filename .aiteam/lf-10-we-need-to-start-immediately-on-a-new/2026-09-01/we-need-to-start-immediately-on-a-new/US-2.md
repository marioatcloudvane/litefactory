# US-2 — Re-check the issue body after a late assignment

> As the owner, I want litefactory to start work when I assign an issue to it, even if
> it had already noticed that issue beforehand, so that assigning is enough and I do
> not have to add a comment as well.

Turns the issue-body decision from a single moment — the instant litefactory first sees
an issue — into a durable question it can ask again on a later tick, so that assigning
an issue after litefactory has already seen it starts the work.

## Why This Matters

This is the actual defect behind the owner's complaint, and it is one defect, not two.
Of the four ways he works, two already function and two do not, and the two that fail
fail for exactly the same reason: litefactory asks "is this issue assigned to me?" once,
at the instant it first notices the issue, and never asks again. If he assigns the issue
thirty seconds later, nobody is listening any more. Whether he opened the issue himself
or somebody else did makes no difference — the question was already asked and answered.

The reason this is not a one-line fix is that litefactory has no reliable way to find
out that an assignment happened. It is a polling system with no live connection to
GitHub, and research established that both of the obvious routes are dead ends: GitHub
does not document whether assigning an issue even updates the timestamp litefactory
polls on, and this project has already measured its own issue listing failing to
surface a confirmed update for thirty minutes. The feed that does carry assignment
events cannot be paged through reliably. So the fix cannot be "notice the assignment" —
it has to be "remember to look again", and remembering costs a small, permanent piece of
bookkeeping that has to be kept deliberately narrow or it will pile up.

Planning also overturned the research recommendation here. Research proposed reusing an
existing "look at this again" flag; checking that against the real code showed it would
be erased by the same pass that set it, and could also be wiped by unrelated work on the
same issue. So this story carries a small database change that research had hoped to
avoid. That is the honest cost, and it is why this is the largest of the three pieces.

## Architecture Notes

- **Two separable sub-problems, and the research brief conflated them.** *Discovery*
  (how does the daemon come to look at this session again on a later tick?) and
  *evaluation* (when it does look, does anything re-ask the body question?). Solving
  only the first changes nothing, because `Daemon.work` (`internal/daemon/tick.go:933`)
  routes an existing session to `drain`, and `drain` never touches the issue body.
- **Mechanism decision (OQ-6): a dedicated `sessions.body_pending` column, not
  `needs_poll`.** Full reasoning is SD-A in `plan_brief.md`. The short version, verified
  in this worktree: `clearBacklog` fires unconditionally whenever `!deferred`
  (`tick.go:950-955`), `deferred` comes only from `drain` (`:944`), and `bootstrap`
  (`:942`) returns an error alone — so a `needs_poll` mark set inside `runIssueBody`
  is wiped by the same tick. `needs_poll` is also cleared inside `run` (`:1432`), so an
  unrelated comment-triggered run would destroy the state.
- **The retry signal is a return value, not a store write from inside the gate**
  (SD-H): `runIssueBody` returns `(retry bool, err error)` → `bootstrap` returns
  `(bool, error)` → `work` routes it. A refusal is a value, not an error.
- **Where the gate is re-asked.** Not `drain` unconditionally — a body retry is not a
  comment and drain's contract is comments-above-watermark. Not an unconditional call in
  `work` before the branch, which would re-evaluate for every session on every tick. The
  shape is: `work` keeps its two-way branch, and the drain arm gains a call to the body
  gate **guarded on `session.BodyPending`**. That is why the flag does double duty
  (SD-B) — it is both the discovery trigger and the free short-circuit.
- **Narrowing, and every fact is available at the marking point at no I/O cost.** Mark
  `body_pending` only when *all* hold: the refusal was exactly "not assigned to the
  owner" (`tick.go:1077`); the body is non-empty (`:1079`); there are no comments
  (`:1081`); and the parsed command spawns something. The last one requires **moving
  `ParseCommand` above the gate** — it is a pure function on a string already in memory,
  so the reorder is free, but it is load-bearing: without it the index bound does not
  hold.
- **Which refusals mark for retry — verdict per branch:**

  | Branch | Retry? | Why |
  |---|---|---|
  | not assigned to the owner | **yes** | the only genuinely time-dependent condition; assignment is exactly the owner action this story exists to catch |
  | body empty | **no** | see the deliberate consequence below |
  | already has comments | **no** | strictly monotonic — comments are only ever added, never removed, so the condition never becomes true again |

- **Deliberate consequence, recorded because the owner will not expect it.** An issue
  whose body was empty when litefactory first saw it will **not** auto-start if the body
  is filled in later; the owner adds a comment, which is the path that already works.
  This is the narrowing that keeps every empty issue in the repository out of the index,
  and it is a headless-run assumption taken on the dispatching instruction rather than
  on the owner's own words. **Issue #10 itself is exactly this case** — it has no body —
  so this story alone would not have started it.
- **What stops it retrying forever.** Three things, and no new policy number or config
  key is needed: the flag clears when the body runs; it clears when the refusal becomes
  permanent; and it clears when the issue closes, which is the owner's natural "stop"
  gesture and is already guarded twice (`tick.go:395`, `:903-907`). An age cap was
  considered and **deferred** — it would need a config key, and config in this codebase
  must be added to `Defaults()`, to the shipped template, and survive
  `TestDefaultFileMatchesDefaults`.
- **Cost bound, verified.** `ListBacklogSessions` (`internal/store/sessions.go:393-395`)
  has no `LIMIT`, so backlog depth is unbounded in SQL — but per-tick cost is bounded,
  because `offer` takes a pool slot (`tick.go:766`) *before* `materialise` (`:891`) does
  the lazy `GetIssue` (`:896`), and `deferRest` (`:871`) re-marks the remainder with no
  fetch. `APP_CONTEXT.md`'s claim that this bounds the extra cost at
  `max_concurrent_runs` issue reads per tick, however deep the backlog, is confirmed.
- **The residual risk this leaves is latency, not cost, and it needs an explicit
  decision.** Body-pending sessions compete for the same bounded pool slots as
  comment-driven work. A large number of them could delay real work even though total
  cost stays bounded. T-2.7 owes an admission policy — at minimum, ordering that
  guarantees no session starves.
- **"Exactly once" changes register** (SD-F) from control flow to the database
  constraint `UNIQUE (session_id, trigger_comment_id)`
  (`internal/store/migrations/0001_init.sql:103`). The constraint is genuine and
  directly tested; `turn.Execute` already returns `DispositionAlreadyRan` rather than
  erroring. The property survives — its stated *reason* does not, in four files.
- **Doc comments this story falsifies** (SD-E — rewriting them is part of the change):
  `tick.go:1046-1055` "watermark first, then run"; `internal/turn/execute.go:190` the
  `MAX(0,0)` watermark argument; `internal/turn/prepare.go:157` where `bodyIsInstruction`
  is decided; and **`internal/harness/prompt.go:247-256`**, whose claim that
  Resume + BodyIsInstruction "is not reachable today — a body fires at most once per
  issue, so a body-triggered run is always the first run of its conversation" becomes
  false, because a session can now have had a comment-triggered run first. *This citation
  was verified directly during planning after a review pass wrongly reported the text as
  absent — it is present and exact. Do not drop this task.*
- Risk: **the authorisation surface widens in time, not in who.** Assignment to the
  owner's numeric id remains the sole authorisation. What changes is that the door stays
  open on an issue for as long as it is open rather than for one tick — so an issue
  opened today can start a run much later, on a body the owner may not have re-read. The
  body is read at retry time, not cached from first sight, which is correct; the prompt's
  "opened by X" attribution is the mitigation.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | `0003_session_body_pending.sql`: add `sessions.body_pending INTEGER NOT NULL DEFAULT 0` plus a partial index on `body_pending = 1`, with a header comment stating the contract and why `needs_poll` was not reused | schema | — | S |
| T-2.2 | `internal/store/sessions.go`: `SetSessionBodyPending`, `ClearSessionBodyPending`, `ListBodyPendingSessions`; add `BodyPending` to `Session` and to every scan site | code | T-2.1 | M |
| T-2.3 | Move `turn.ParseCommand` above the gate switch in `runIssueBody` so "does the command spawn?" is available at the marking point | code | — | S |
| T-2.4 | Change `runIssueBody` to return `(retry bool, err error)`; mark retry only on the not-assigned branch and only when all four narrowing facts hold | code | T-2.2, T-2.3 | M |
| T-2.5 | Change `bootstrap` to return `(bool, error)`; route it through `Daemon.work`'s branch (`tick.go:940-944`) without disturbing the existing `deferred` semantics | code | T-2.4 | M |
| T-2.6 | Add a guarded body-gate call to the drain arm of `work`, conditional on `session.BodyPending` | code | T-2.5 | M |
| T-2.7 | Union body-pending sessions into `Daemon.discover` (`tick.go:241`) alongside the existing backlog union, and decide the admission policy so body-pending sessions cannot starve comment-driven work | code | T-2.2 | M |
| T-2.8 | Clear the flag on a successful body run, on any refusal that has become permanent, and when the issue is found closed | code | T-2.6 | S |
| T-2.9 | Rewrite the falsified doc comments: `tick.go:1046-1055`, `internal/turn/execute.go:190`, `internal/turn/prepare.go:157`, `internal/harness/prompt.go:247-256` | docs | T-2.6 | M |
| T-2.10 | Update `internal/daemon/doc.go` so the package spec says the body decision is durable rather than a first-sight moment | docs | T-2.6 | S |
| T-2.11 | Add an `.aiteam/AC_AMENDMENTS.md` entry amending **AC-2.16b**'s "on first sight" wording, in the ledger's existing format | docs | T-2.6 | S |
| T-2.12 | Tests: late assignment fires; no-retry branches never re-queue; the partial index is empty when every session is idle; workflows 1 and 3 do not regress | test | T-2.6, T-2.8 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | **Workflow 4.** Somebody else opens an issue with a non-empty body and no comments; litefactory sees it while unassigned; the owner then assigns it to himself. On a later tick, the body runs as the first instruction, with no comment required. |
| AC-2.2 | **Workflow 2.** The owner opens an issue with a non-empty body; litefactory sees it while unassigned; the owner then assigns it to himself. On a later tick, the body runs. |
| AC-2.3 | **Workflow 3 does not regress.** An issue already assigned to the owner when litefactory first sees it still runs its body on that first sighting. |
| AC-2.4 | **Workflow 1 does not regress.** The comment-triggered path is unchanged in behaviour and in timing. |
| AC-2.5 | **Once ever.** However many times the gate is re-asked, at most one run is ever created at trigger 0 for a session; a repeat is a clean no-op, not an error. |
| AC-2.6 | An issue never assigned to the owner starts nothing, however long it remains open. `TestANonOwnerAssigneeDoesNotFireTheBody` (`internal/daemon/tick_test.go:774`) passes unchanged. |
| AC-2.7 | An issue refused for an empty body is **not** marked for retry, and an issue refused because it already has comments is **not** marked for retry. |
| AC-2.8 | When every session is idle, the `body_pending` partial index carries no entries — the steady-state design point of the existing backlog index is preserved, not inverted. |
| AC-2.9 | The flag is cleared when the body runs, when the refusal becomes permanent, and when the issue is closed. A closed issue leaves no pending state behind. |
| AC-2.10 | Per-tick cost stays bounded at `max_concurrent_runs` issue reads however deep the pending set, because the issue is fetched lazily after a pool slot is taken. |
| AC-2.11 | Body-pending sessions cannot indefinitely starve comment-driven work; the admission order guarantees every session is eventually considered. |
| AC-2.12 | No forge write is added on any refusal path, no new forge endpoint is added, and `internal/github`'s pinned method set is unchanged. |
| AC-2.13 | No spend cap, run-rate cap or circuit breaker is added (D-X). |
| AC-2.14 | `0002_session_backlog.sql` is not edited; the new state lives in `0003_*.sql`. |
| AC-2.15 | Every doc comment this change makes untrue is rewritten in the same change — specifically the four named in Architecture Notes. |
| AC-2.16 | `.aiteam/AC_AMENDMENTS.md` carries a new entry amending AC-2.16b, in the ledger's existing format, recording that the body is no longer tied to first sight. |
| AC-2.17 | `internal/daemon/doc.go` states that the issue-body decision is durable rather than a single moment. |
