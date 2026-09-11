# Research Brief — coding-agent-switching

## Request

GitHub issue #228, "Coding Agent switching." The issue body was empty and the
only comment read "Check this one!" — the first Research run (see history
below) could not determine scope from the issue thread alone and produced a
ranked list of candidate interpretations plus open questions for the owner.

The issue owner has since replied directly (quoted, this run's brief):

> "What is meant by that: i want to switch the coding agent. Right now, it is
> refused and a coding agent is more or less pinned in the database. What
> should change: if the request carries a different coding agent label than
> the pinned one, it should get changed. So it is a behavioral change. Right
> now we can only do this by ssh'ing into the machine litefactory runs on and
> do a sh script that does the change. This is very user unfriendly, i just
> want to change the label on github eg from coding agent: claude to codex
> and it should do it; no restart of litefactory, no refusal. I know we will
> loose the session in this case, this is fine." `[O]`

This resolves the first run's Open Question 4 (candidate interpretation "c")
and its Open Question 3 (runtime axis, not the `internal/render` harness
axis). Scope is now: **reverse `ReplyAgentSwitchRefused` into an actual
switch**, triggered by a `Coding Agent:` label change on the GitHub issue,
requiring no daemon restart, with the explicit, owner-accepted tradeoff that
the in-progress conversation/session state for that issue is discarded when
the switch happens.

## Codebase Context

Findings below are `[V]`, reported by `codebase-tour` this run against
`internal/store`, `internal/turn`, `internal/daemon`, `internal/report`,
`internal/harness`, and the repo root, unless marked otherwise. Prior-run
findings retained where still accurate are marked `[R0]`.

### The "pinned" agent: storage, exactly

Confirmed `[V]`: SQLite (`modernc.org/sqlite`, single connection pool —
`internal/store/store.go:1,20-41,77`), one file at `$LF_HOME/litefactory.db`.
The pin is **segment-level, not session-level**: table `segments`, column
`agent TEXT NOT NULL DEFAULT 'claude'`, added by
`internal/store/migrations/0003_agent.sql:44`. A companion column
`runs.agent` (same migration, line 47) is a per-run historical snapshot,
copied from `segment.Agent` at harvest time (`internal/turn/harvest.go:269`)
— it records what a specific past run actually used, and is not itself the
pin.

- **Write path**: `store.DB.GetOrCreateSegment`
  (`internal/store/segments.go:62-100`) — the `agent` column is set **only
  on first INSERT**. There is no `UpdateSegmentAgent`/`SetSegmentAgent`
  accessor anywhere in the Go code today; the column is otherwise immutable
  once a segment exists. `GetOrCreateSegment` also has a second relevant
  behavior: on the reuse path (row already exists for `(session_id,
  ordinal)`) it returns the **existing row unchanged**, ignoring its `kind`
  argument entirely.
- **Read path**: `internal/turn/delivery.go:295`
  (`prepareDeliverySegment`) reads the last segment's `Agent`;
  `internal/turn/prepare.go:219-227` compares it to the freshly resolved
  request agent and is where the refusal is thrown (below).

This confirms the prior run's "the database" language `[R0]` — it is SQLite,
and the "pin" is one column on one row, not a broader config or cache.

### Where the refusal is thrown, and the exact comparison

`internal/turn/prepare.go:219`: `if segment.Agent != string(resolvedKind) {
return nil, &agentSwitchRefusal{owning: segment.Agent, requested:
string(resolvedKind)} }`. `resolvedKind` is produced by
`daemon.resolveAgent(labels)` (`internal/daemon/agent.go:96`, called once per
dispatched turn — see label-detection section below) from the issue's
current `Coding Agent:` label, already validated (case, uniqueness, known
agent) before it reaches `prepare()`. This runs inside `turn.Execute`
(`execute.go:583`), after the per-session store lock (`AcquireSessionLock`,
line 541) is acquired but before any session-state or `runs` row write —
`agentSwitchRefusal` is converted to `DispositionAgentSwitchRefused`
(`execute.go:583-593`) and rendered by `report.ReplyAgentSwitchRefused`
(`internal/report/reply.go:51-58, 357-364, 796-803`).

Rendered message (`reply.go:796-803`):
> "This task's conversation already belongs to a different coding-agent tool
> than this comment asked for. … A conversation stays with the tool it
> started with — litefactory does not hand one tool's session to another.
> Comment again without asking for a different agent, or start a new task
> for the one you want."

**This was a deliberate design decision, not an oversight** — recovered
in-code (git history for the originating commit/PR could not be retrieved
this run; see Constraints & Risks). The doc comment at `reply.go:51-57`
states the rationale directly: *"A segment's Agent is fixed at creation
(internal/store), and switching it mid-conversation would either hand one
tool's session identity to another tool's process or silently start a
second, unrelated conversation under the first run's number — so the
request is refused rather than guessed at."* Two tests encode this as an
explicit invariant: `internal/turn/execute_test.go:316-378`
(`TestExecuteRefusesAnAgentSwitchWithoutTouchingSessionState` /
`...WithoutClearingAFailure`) assert the refusal allocates nothing and
mutates no session state in either direction. Both the rationale comment and
these tests will need to change if the refusal becomes a switch — they
currently assert the opposite of the requested new behavior. `[V]`

A prior `.aiteam` session directory named
`lf-120-agent-switch-refused-leaves-the-session` exists in this repo's
history (dated 2026-09-06), strongly suggesting this refusal — or a bug
adjacent to it — was already the subject of prior planning work. Per this
session's own scope boundary, its artifacts were not read directly; git
history was the intended fallback, but the sub-agent available this run
(`codebase-tour`) has no shell/`git` access, so the originating commit
message/PR discussion could not be retrieved. The in-code rationale above
substantially recovers *why* the refusal exists even without that history —
see Open Questions for what's still missing.

### Session/task teardown machinery that already exists

- **State model**: `store.SessionState` (`internal/store/sessions.go:12-23`)
  is a three-value enum, `idle` / `running` / `failed`. `SetSessionState` is
  called last in `prepare()` (line 423), deliberately after every
  early-return path (including the switch refusal) so a refusal leaves
  session state untouched.
- **Concurrency guards** (two layers): an in-process `map[int64]struct{}`
  keyed by session id (`internal/daemon/inflight.go`, `enter`/`leave`) is the
  primary intra-process exclusion; `sessions.lock_owner`
  (`AcquireSessionLock`/`ReleaseSessionLock`, held across `turn.Execute`,
  `execute.go:541,566`) is cross-process defence-in-depth.
- **Subprocess teardown**: `internal/harness/kill.go` escalates SIGTERM then
  SIGKILL across the process group; `internal/harness/runner.go` wires
  `exec.CommandContext` with a deferred kill callback on context
  cancellation, invoked today on daemon shutdown (`Daemon.Quiesce`,
  `internal/daemon/pool.go:181`).
- **A precedent for reacting to a live label change already exists, but only
  for a different mechanism**: `internal/daemon/continuation_dispatch.go:
  104-111` (`observeContinuation`) cancels a **pending, not-yet-dispatched**
  auto-resume continuation when `choice.Agent != c.Context.Agent` (comment:
  `"coding agent changed"`). This is the session-limit auto-resume feature,
  separate from the primary switch-refusal path — it shows the codebase
  already has a "notice label drift, tear something down" idiom, just not
  applied to an in-flight run.
- **What's missing**: nothing today kills an **already-dispatched, currently
  running** subprocess on a label change — `resolveAgent` is read once at
  dispatch time (`tick.go:1602`), not polled during a run. There is no
  accessor to mutate `segments.agent`/`resume_token` on an existing row, and
  `prepareDeliverySegment` returns the stale segment unchanged on a vendor
  mismatch (`delivery.go:343-345`) rather than rotating to a new one — the
  same rotation mechanism it already uses for same-vendor instruction-bundle
  changes (`delivery.go:337-358`) is not wired to a vendor/agent change.

### The manual SSH script — found, and it is the exact spec for the mutation

`switch-agent.sh` at the repo root (not under `scripts/`). Usage:
`./switch-agent.sh <session-id> <agent>` or `--list`. Confirmed behavior:

- **Refuses to run while the daemon is alive** — checks
  `pgrep -f 'litefactory daemon (run|start)'` (script lines ~67-69) and
  aborts if found. Its own comment explains why: *"a run in flight holds the
  worktree and will write a runs row for the agent it was prepared with.
  Switching underneath it would leave the two disagreeing."* This precondition
  is exactly the concurrency hazard an automatic, live-daemon version must
  solve some other way (see Constraints & Risks).
- Takes a consistent snapshot first: `sqlite3 "$DB" ".backup '$BACKUP'"`
  (line ~90) before mutating.
- The mutation itself, inside `begin immediate; ... commit;` (lines ~97-102):
  ```sql
  UPDATE segments SET agent = '$AGENT', resume_token = null
  WHERE session_id = $SESSION
  ```
  This clears `resume_token` in the same statement — the script's comment
  explains a stale token would let the new tool try to resume a conversation
  thread it never issued. **This is the exact mutation an automatic switch
  needs to perform**, modulo the design-fork question below (mutate in place
  vs. rotate to a new segment).
- Prints an explicit warning that "the next run on this issue cold-starts" —
  the same discard-in-progress-conversation tradeoff the issue owner has now
  accepted.
- Hardcodes the agent enum as `claude|codex` in a shell case statement — its
  own comment admits why: *"The registry is built at daemon startup from
  config, so this script cannot ask it what is registered."* An in-daemon
  implementation has the real registry available
  (`internal/turn.RunnerSet`/`internal/daemon`'s `agents` map) and must not
  inherit this hardcoding.
- No interaction with the daemon at all beyond the `pgrep` precondition — no
  signal, no restart marker, no IPC.

### Label-change detection — already polling, already fresh per turn; no restart needed for that part

There is no webhook receiver anywhere in the tree — `internal/github`
exposes only `ListIssues`/`GetIssue`, consumed from a `time.NewTicker`
(`internal/daemon/daemon.go:738`) driving `Daemon.Tick`
(`tick.go:128,351`). Labels are re-read fresh from the API on **every**
tick, never cached — `resolveAgent`'s own doc comment: *"read again on every
tick; nothing here is cached."* Agent resolution happens once per dispatched
turn (`tick.go:1602`, inside `Daemon.run`), i.e., once per triggering
comment/event, not continuously against an in-flight run.

Practically: **the existing polling plumbing already surfaces a label change
on the next tick/comment with zero new infrastructure and zero restart** —
this part of the owner's ask ("no restart of litefactory") is already true
of the read side today. What's actually missing is narrower than "detect the
change" — it is (a) performing the DB mutation instead of refusing once the
change is detected at `prepare()`-time, and (b) deciding what happens if the
label changes **while a run is already mid-flight** (nothing observes that
today; see Open Questions).

## Domain / Problem Space

Confirmed, not speculative (owner reply resolves prior Open Questions 3 and
4): this is runtime Claude/Codex agent selection and switching
(`internal/agent`, `internal/daemon`, `internal/store`, `internal/turn`,
`internal/report`), specifically reversing the deliberate
`ReplyAgentSwitchRefused` behavior into an actual mid-conversation handoff,
driven purely by the issue's `Coding Agent:` label, discarding prior
session/conversation state for that issue when it happens.

Out of scope, confirmed this run:
- The `internal/render` harness-rendering axis (candidate 2 in the prior
  brief) — unrelated, not mentioned by the owner.
- A new user-facing selector/CLI flag (candidate 3, prior brief) — the
  owner's stated mechanism is exclusively the existing GitHub label.
- Adding a third coding-agent tool to the registry — not requested; the
  registry's open-by-design shape (prior run's finding, `[R0]`) is relevant
  only insofar as the switch implementation should read from it rather than
  hardcode `claude|codex` the way `switch-agent.sh` does.

## Constraints & Risks

- **Design fork, not yet resolved — Plan mode must decide explicitly**:
  mutate the existing segment row in place (`segments.agent`/`resume_token`,
  same ordinal — what `switch-agent.sh` does, history overwritten) vs. leave
  the old segment alone and create a new one at `ordinal = last.Ordinal + 1`
  under the new agent (the rotation mechanism `prepareDeliverySegment`
  already uses for same-vendor bundle changes, `delivery.go:337-358`, just
  not currently wired to a vendor change). This changes what "discarded"
  concretely means (overwritten row vs. orphaned-but-retained old segment)
  and affects `runs.agent`/history semantics and any future audit trail.
  `[V]`
- **Concurrency/race with an in-flight run**: `switch-agent.sh` requires the
  daemon to be stopped specifically to avoid a run started under the old
  agent finishing and writing a `runs.agent` row that disagrees with a
  `segments.agent` mutated underneath it. An automatic, live-daemon
  implementation must reproduce this guarantee some other way — most likely
  by only applying the mutation while holding the same
  `sessions.lock_owner`/in-process `inflight` guard `turn.Execute` already
  takes for the whole turn, i.e., treating the switch as part of `prepare()`
  under the existing per-session serialization rather than as an
  out-of-band write. `[V]`
- **Undefined behavior for a label change during an already-running
  subprocess**: `resolveAgent` is read once at dispatch time and nothing
  polls it mid-run. The continuation-cancellation precedent
  (`continuation_dispatch.go:104-111`) only cancels a *pending* continuation,
  never a live process. Plan mode must decide whether the accepted
  "discard in-progress state" tradeoff extends to actively killing a running
  subprocess mid-turn, or only applies to conversation/resume-token state
  picked up by the *next* run (simpler, reuses existing kill/cancellation
  machinery only at its current call sites). `[V]`
- **Trigger model ambiguity**: today, agent resolution — and therefore any
  switch — happens once per **dispatched turn**, which in this codebase
  means once per triggering comment/event on the issue, not on every tick
  regardless of activity. The owner's phrasing ("i just want to change the
  label on github... and it should do it") does not specify whether a bare
  label edit with no new comment should itself trigger the switch, or
  whether the switch is expected to happen on the *next comment* after the
  label edit (consistent with how the daemon already dispatches turns).
  These are materially different implementations: the latter needs no new
  trigger plumbing (existing per-comment dispatch already re-reads labels
  fresh); the former would need a new "detect label drift with no comment"
  reconciliation pass. This should be confirmed with the owner or decided
  explicitly by Plan mode. `[V, new this run]`
- **Registry hardcoding must not be inherited**: `switch-agent.sh` hardcodes
  `claude|codex` because it runs outside the daemon process and cannot see
  the registry built at startup from config. The in-daemon implementation
  must read from the actual `RunnerSet`/registry so a third registered agent
  (if one is ever added) is switchable without a script change. `[V]`
- **Tests currently assert the opposite of the new requirement** —
  `internal/turn/execute_test.go:316-378` explicitly assert that an agent
  mismatch touches nothing. These are the correct place to look for what
  currently passes and will need rewriting, not just new tests added
  alongside them. `[V]`
- **Git history for the original refusal decision (and any prior discussion
  of reverting it, e.g. in session `lf-120-agent-switch-refused-leaves-the-
  session`) could not be retrieved this run** — no agent in this run's
  roster had shell/`git` access. The in-code rationale (doc comments, tests,
  `switch-agent.sh`'s own header) substantially recovers the design intent,
  but a full paper trail (was reversing this ever proposed and rejected
  before, and why) is not confirmed. `[V, limitation]`

## Open Questions

1. **Resolved this run** — what issue #228 wants, and which of the prior
   run's candidate interpretations applies. Owner confirmed: reverse
   `ReplyAgentSwitchRefused` into an actual switch, label-driven, no
   restart, session loss accepted. `[O]`
2. **Resolved this run** — why the refusal was built the way it was. The
   in-code rationale (`reply.go:51-57` doc comment, `execute_test.go:316-378`
   invariants, `switch-agent.sh`'s own header) recovers this without needing
   session 120's artifacts. Residual gap: the originating commit/PR
   discussion (was reverting ever proposed/rejected before) is still
   unconfirmed — see Constraints & Risks. `[V, partially resolved]`
3. **Resolved this run** — runtime axis confirmed in scope; `internal/render`
   harness-rendering axis confirmed out of scope. `[O]`
4. **Resolved this run** — yes, the refusal should become a switch, not
   just better labeling UX. `[O]`
5. **New, blocks implementation shape**: mutate-in-place vs. rotate-to-new-
   segment for the switch (see Constraints & Risks, design fork). Settles
   with: Plan mode / architect decision, informed by how `runs.agent`
   history and any future audit needs should behave. `[V, new this run]`
6. **New, blocks implementation shape**: what happens to an already-running
   subprocess when the label changes mid-flight — let it finish under the
   old agent and apply the switch to the next run, or actively cancel it?
   Settles with: owner confirmation or Plan mode decision; the owner's
   "lose the session" acceptance does not by itself specify this. `[V, new
   this run]`
7. **New, blocks trigger design**: does the switch need to fire on a bare
   label edit with no new comment, or only take effect on the next comment
   (matching the existing per-comment dispatch model, which needs no new
   plumbing)? Settles with: owner clarification. `[V, new this run]`
8. **New, low priority**: should `switch-agent.sh` be retired, kept as an
   emergency/offline fallback once the automatic path exists, or left
   as-is? Settles with: Plan mode, not blocking for scoping. `[V, new this
   run]`
9. **Carried forward, low priority, likely unrelated**: `.codex/` parity
   note in `AGENT_ROSTER.md` — flagged only for terminology overlap, not
   believed to be in scope. `[R0]`

## Recommended scope for Plan mode

- **In scope**: reversing `ReplyAgentSwitchRefused` (`internal/report/
  reply.go`) into an actual mid-conversation agent switch, triggered by the
  existing `Coding Agent:` label mechanism (`internal/daemon/agent.go:
  resolveAgent`), implemented inside `internal/turn/prepare.go`'s existing
  comparison point (`prepare.go:219-227`) rather than as new plumbing to
  detect the label change — that detection already exists and already runs
  without a restart.
- **Plan mode must resolve, not inherit silently from `switch-agent.sh`**:
  the mutate-in-place vs. rotate-to-new-segment design fork (Open Question
  5/Constraints), and the in-flight-subprocess behavior (Open Question 6).
  Both have real, different implementation costs and different user-visible
  behavior.
- **Plan mode should get an explicit owner answer on the trigger model**
  (Open Question 7) before committing to "no new plumbing needed" — if a
  bare label edit with no comment must also trigger the switch, that is a
  materially larger scope (a reconciliation pass) than reusing the existing
  per-comment dispatch.
- **Concurrency/locking design must be specified**, not left implicit: the
  plan should state explicitly how the switch avoids the race
  `switch-agent.sh` avoids today by requiring the daemon to be stopped
  (most likely: perform the mutation under the same per-session lock/inflight
  guard `turn.Execute` already acquires).
- **Must not hardcode agent kinds** — implementation should read from the
  existing `RunnerSet`/registry (`internal/turn`, `internal/daemon/
  runners.go`), unlike `switch-agent.sh`.
- **Must account for and rewrite** the existing invariant tests
  (`internal/turn/execute_test.go:316-378`) that currently assert a
  mismatch touches nothing, plus the `reply.go:51-57` rationale comment,
  which both currently describe the behavior being reversed.
- **Decide the fate of `switch-agent.sh`** (retire, keep as fallback, or
  leave) as part of the plan, not as an afterthought — it is the only
  existing, working implementation of the mutation and is a reasonable
  reference/fallback even if superseded.
- **Out of scope**: `internal/render` harness rendering; adding a third
  coding-agent tool; any CLI-flag-based selection surface — none of these
  were requested by the owner.
- **Residual gap for a follow-up pass if needed**: the originating
  commit/PR for `ReplyAgentSwitchRefused` (and any related discussion in
  session `lf-120-agent-switch-refused-leaves-the-session`) was not
  retrievable this run for lack of shell/`git` tool access in the available
  roster. If Plan mode wants that paper trail specifically (rather than the
  in-code rationale already captured above), it needs a pass with `git log`/
  `git blame` access.
