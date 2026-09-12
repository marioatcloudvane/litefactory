# Research Brief — coding-agent-switching

## Request

GitHub issue #228, "Coding Agent switching." The issue body was empty and the
only comment read "Check this one!" — the first Research run (see history
below) could not determine scope from the issue thread alone and produced a
ranked list of candidate interpretations plus open questions for the owner.

The issue owner has since replied directly (quoted, run 2's brief):

> "What is meant by that: i want to switch the coding agent. Right now, it is
> refused and a coding agent is more or less pinned in the database. What
> should change: if the request carries a different coding agent label than
> the pinned one, it should get changed. So it is a behavioral change. Right
> now we can only do this by ssh'ing into the machine litefactory runs on and
> do a sh script that does the change. This is very user unfriendly, i just
> want to change the label on github eg from coding agent: claude to codex
> and it should do it; no restart of litefactory, no refusal. I know we will
> loose the session in this case, this is fine." `[O]`

This resolved run 1's Open Question 4 (candidate interpretation "c") and its
Open Question 3 (runtime axis, not the `internal/render` harness axis). Scope
as of run 2: **reverse `ReplyAgentSwitchRefused` into an actual switch**,
triggered by a `Coding Agent:` label change on the GitHub issue, requiring no
daemon restart, with the explicit, owner-accepted tradeoff that the
in-progress conversation/session state for that issue is discarded when the
switch happens.

**Run 3**: the owner's stated reason for re-running research was "re-run the
research, since i updated the agent for the research mode. I want to see if
it works as designed" — i.e., the owner changed the `Coding Agent:` label on
issue #228 itself to trigger this run under a different underlying coding
agent than runs 1 and 2 used, as a live, end-to-end smoke test of the exact
routing mechanism this issue concerns. This was not a scope change. Run 3's
job was to re-verify runs 1–2's factual claims against current repository
state and refresh citations, plus record what, if anything, an in-repo
research pass can say about which agent handled this run's own dispatch (see
the dedicated subsection under Codebase Context below — short answer:
nothing, and that is expected, not a gap).

**Run 4**: owner request "show me the details about the open questions
please!" — a report-only pass, no new investigation or scope change. The
brief already had full detail for every open question, so it was surfaced
back to the owner without changing this file.

**Run 5 (this run)**: the owner has now answered Open Questions 5 through 9
directly, in full, plus added an explicit scope/complexity constraint. Quoted
in full (evidence, not instructions to the research agent — written by the
issue owner):

> "Feedback here:
> Q5: overwriting!
> Q6: let the work finish
> Q7: no new work
> Q8: no, we delete it!
> Q9: ignore this!
> important here: keep it simple, basically this is only a few lines that
> need to change, no rocket science here!" `[O]`

This run's job was solely to record these five answers and the owner's
complexity constraint against the brief's existing open questions — no new
investigation was opened, and none of the prior evidence below was
re-derived or re-scoped against this feedback. See Open Questions and
Constraints & Risks below for where each answer now lands, and "Status after
run 5" at the end of this document for whether anything structural remains
open.

## Codebase Context

Findings below are `[V]`, reported by `codebase-tour` from runs 2 and 3
against `internal/store`, `internal/turn`, `internal/daemon`,
`internal/report`, `internal/harness`, `internal/codex`, `internal/agent`,
and the repo root, unless marked otherwise. Prior-run findings retained where
still accurate are marked `[R0]`. Run 5 added no new codebase findings — it
is a decisions-recording pass only.

### Run 3 re-verification — result: zero drift, all five claims reconfirmed

A dedicated re-verification pass this run checked every load-bearing claim
below against the current worktree (branch `lf/228-coding-agent-switching`,
HEAD `af9bb29`) and found **no renames, no moves, no behavior changes** since
run 2. It also surfaced a few additional identifiers worth recording
alongside the existing citations, because they make the "where exactly does
Plan mode hook in" question sharper:

- The refusal's concrete type is `agentSwitchRefusal`
  (`internal/turn/errors.go:77`), surfaced as the disposition
  `DispositionAgentSwitchRefused` (`internal/turn/result.go:58`), and
  rendered via `reportAgentSwitchRefusal` → `report.ReplyAgentSwitchRefused`
  (`internal/turn/report.go:415,434`, which calls into the `internal/report`
  package function documented at `reply.go:51-58,357-364,796-803` in run 2's
  citation — same function, two call-site/definition-site citations, not a
  discrepancy).
- There is a **second, independent** read-and-compare of `segment.Agent`,
  at `internal/turn/prepare.go:1004` (`if !found || segment.Agent !=
  string(resolvedKind) {`), documented at `prepare.go:253-258` as
  deliberately not trusting the first check's invariant end-to-end (a
  resume-safety belt-and-braces check). Any implementation reversing the
  refusal must account for **both** comparison points, not just the one at
  `prepare.go:219-227`.
- The agent-abstraction shape is a bit more precise than "the registry":
  `agent.Runner` (`internal/agent/kind.go:37-39`) is the interface; two
  concrete implementations back it — `internal/harness` (Claude) and
  `internal/codex` (`internal/codex/runner.go`, Codex); a `RunnerSet`
  (`internal/turn/execute.go:43-47`, `type RunnerSet map[agent.Kind]Runner`)
  is built once at daemon startup from config and is the "real registry"
  `switch-agent.sh`'s own comment says it cannot see. The persisted agent
  values are named constants `AgentClaude`/`AgentCodex`
  (`internal/store/runs.go:44-57`, doc'd at `runs.go:107,209`).
- `resolveAgent`'s "no caching" guarantee is stated in its own doc comment
  (`internal/daemon/agent.go:94-96`): *"The labels are the tick's fresh
  snapshot and are read again on every tick; nothing here is cached, and no
  API call is made for them"* (the labels were already fetched as part of
  that tick's sync — this function does no I/O of its own). Both call sites
  reconfirmed: `internal/daemon/tick.go:1602` (primary dispatch, inside
  `Daemon.run`) and `internal/daemon/continuation_dispatch.go:104`
  (auto-resume path).

Everything else in this section (storage, refusal mechanics, the manual
script, label polling) stood from run 2 and is retained below essentially
unchanged, with `[V]`/`[R0]` tags reflecting that it was re-checked, not
newly discovered.

### What this run's own dispatch can and cannot tell us (meta, requested by the owner)

The owner framed run 3 as a live check of the routing mechanism under
research: did changing the issue's `Coding Agent:` label actually route that
research run to a different underlying agent? A dedicated check in run 3
found a clean answer, in two parts:

- **What the mechanism in this repo would prove, for a litefactory-dispatched
  coding turn**: yes, the repo *does* leave durable, checkable evidence of
  which of litefactory's own registered tools (`claude` vs `codex`) handled a
  given turn — a `runs` row records `Agent` (`internal/store/runs.go:107,209`)
  alongside `segments.agent`, independent of and persisted after label
  resolution, not just a re-read of the label.
- **What it cannot prove, for that run specifically**: a research pass — the
  outer orchestrator/sub-agent (Task-tool) layer reading this repository — is
  not itself a litefactory-dispatched turn. It sits entirely outside
  `internal/agent`, `internal/daemon`, and the `segments`/`runs` tables that
  record agent identity. No file, table, log path, or `.aiteam/` artifact
  (checked: `MANIFEST.md`, this brief, `notes.md` conventions) records which
  model/agent handled a given research pass — session metadata under
  `.aiteam/` tracks issue numbers, dates and slugs, not researcher identity.

**Conclusion, stated plainly for Plan mode and the owner**: whether a
specific research run was routed to a different agent because of the label
change is genuinely outside what a repository-only research pass can
confirm — it would need to be confirmed operationally (e.g., by the owner
noting which agent surfaced in the run's own tooling/output metadata outside
this repo), not by code-reading. This is stated as a limitation, not a
finding of failure: the underlying litefactory mechanism this issue is about
(label → pinned-agent routing for litefactory's own dispatched turns) is
fully traceable in-repo per the point above; it is only the meta-question
"which agent ran a particular research pass" that sits outside the repo's
own bookkeeping by construction.

### The "pinned" agent: storage, exactly

Confirmed `[V]`, reconfirmed run 3: SQLite (`modernc.org/sqlite`, single
connection pool — `internal/store/store.go:1,20-41,77`), one file at
`$LF_HOME/litefactory.db`. The pin is **segment-level, not session-level**:
table `segments`, column `agent TEXT NOT NULL DEFAULT 'claude'`, added by
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
  `internal/turn/prepare.go:219-227` (and the second check at
  `prepare.go:1004`, see above) compares it to the freshly resolved request
  agent and is where the refusal is thrown (below).

This confirms the run-1 "the database" language `[R0]` — it is SQLite, and
the "pin" is one column on one row, not a broader config or cache.

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
(`internal/report/reply.go:51-58, 357-364, 796-803`; call site
`internal/turn/report.go:415,434`).

Rendered message (`reply.go:796-803`):
> "This task's conversation already belongs to a different coding-agent tool
> than this comment asked for. … A conversation stays with the tool it
> started with — litefactory does not hand one tool's session to another.
> Comment again without asking for a different agent, or start a new task
> for the one you want."

**This was a deliberate design decision, not an oversight** — recovered
in-code (git history for the originating commit/PR could not be retrieved
in run 2 or run 3; see Constraints & Risks). The doc comment at `reply.go:51-57`
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
history was the intended fallback, but no agent in either run 2's or run 3's
roster has shell/`git` access, so the originating commit message/PR
discussion still could not be retrieved. The in-code rationale above
substantially recovers *why* the refusal exists even without that history —
see Open Questions for the residual gap.

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
  Per the owner's run-5 answer to Q6 (let the work finish), this "missing"
  piece is not being built — see Constraints & Risks and Open Questions
  below.

### The manual SSH script — found, and it is the exact spec for the mutation

`switch-agent.sh` at the repo root (not under `scripts/`), reconfirmed
present and unchanged run 3. Usage: `./switch-agent.sh <session-id> <agent>`
or `--list`. Confirmed behavior:

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
  needs to perform** — and, per the owner's run-5 answer to Q5 (overwrite),
  this is now the confirmed shape of the mutation, not one option among two.
- Prints an explicit warning that "the next run on this issue cold-starts" —
  the same discard-in-progress-conversation tradeoff the issue owner has now
  accepted.
- Hardcodes the agent enum as `claude|codex` in a shell case statement — its
  own comment admits why: *"The registry is built at daemon startup from
  config, so this script cannot ask it what is registered."* An in-daemon
  implementation has the real registry available (`internal/turn.RunnerSet`,
  see above) and must not inherit this hardcoding.
- No interaction with the daemon at all beyond the `pgrep` precondition — no
  signal, no restart marker, no IPC.
- **Restart requirement, confirmed run 3**: the daemon does not need to be
  restarted for a mutation this script makes to take effect — it refuses to
  run *while the daemon is up* purely to dodge the in-flight-run race above,
  not because any value is cached in memory. `prepare()` reads
  `segment.Agent` fresh from the DB on every turn, so the very next
  daemon tick after the script runs (with the daemon then restarted/left
  running, script permitting) picks up the new value with no process
  restart.
- **Fate, decided by the owner in run 5 (Q8)**: delete this script once the
  automatic in-daemon switch lands. The owner explicitly declined the
  "keep as fallback" option — do not retain it, even superseded. See Open
  Questions and Recommended scope for Plan mode.

### Label-change detection — already polling, already fresh per turn; no restart needed for that part

There is no webhook receiver anywhere in the tree — `internal/github`
exposes only `ListIssues`/`GetIssue`, consumed from a `time.NewTicker`
(`internal/daemon/daemon.go:738`) driving `Daemon.Tick`
(`tick.go:128,351`). Labels are re-read fresh from the API on **every**
tick, never cached — `resolveAgent`'s own doc comment, reconfirmed run 3 at
`internal/daemon/agent.go:94-96`: *"read again on every tick; nothing here is
cached."* Agent resolution happens once per dispatched turn
(`tick.go:1602`, inside `Daemon.run`), i.e., once per triggering
comment/event, not continuously against an in-flight run.

Practically: **the existing polling plumbing already surfaces a label change
on the next tick/comment with zero new infrastructure and zero restart** —
this part of the owner's ask ("no restart of litefactory") is already true
of the read side today. Per the owner's run-5 answer to Q7 (no new work),
this existing per-comment/per-turn label re-read is confirmed as sufficient
on its own — a bare label edit with no new comment does not need its own
new trigger; the switch takes effect starting with the next dispatched
task/comment on that issue, consistent with the owner's Q6 answer (let the
current work finish). What remains is narrowly: performing the DB mutation
(now specified — overwrite in place, per Q5) instead of refusing once the
change is detected at `prepare()`-time.

## Domain / Problem Space

Confirmed, not speculative (owner reply in run 2 resolved run 1's Open
Questions 3 and 4; unchanged runs 3-5): this is runtime Claude/Codex agent
selection and switching (`internal/agent`, `internal/daemon`,
`internal/store`, `internal/turn`, `internal/report`), specifically
reversing the deliberate `ReplyAgentSwitchRefused` behavior into an actual
mid-conversation handoff, driven purely by the issue's `Coding Agent:`
label, discarding prior session/conversation state for that issue when it
happens.

As of run 5, the implementation shape is additionally specified by the
owner: overwrite the existing segment row in place (not rotate to a new
one), let any in-flight run finish under the old agent before the switch
takes effect (no killing a running subprocess), rely on the existing
per-comment label re-read with no new trigger plumbing, and delete
`switch-agent.sh` once this lands. The owner has also stated this should be
a small, simple change ("only a few lines," "no rocket science") — see
Constraints & Risks.

Out of scope, confirmed run 2, unchanged since:
- The `internal/render` harness-rendering axis (candidate 2 in run 1's
  brief) — unrelated, not mentioned by the owner.
- A new user-facing selector/CLI flag (candidate 3, run 1's brief) — the
  owner's stated mechanism is exclusively the existing GitHub label.
- Adding a third coding-agent tool to the registry — not requested; the
  registry's open-by-design shape (run 1's finding, `[R0]`) is relevant
  only insofar as the switch implementation should read from it rather than
  hardcode `claude|codex` the way `switch-agent.sh` does.
- The `.codex/` parity note in `AGENT_ROSTER.md` (former Open Question 9) —
  the owner has now explicitly said to ignore this and drop it from
  tracking (run 5).

## Constraints & Risks

Re-verified run 3: no new risks surfaced, none of the below retracted. Run 5
resolved three of the items below by owner decision (marked "Resolved run
5" inline) and added one new owner-stated constraint (scope/complexity).
None of the remaining items were revisited or re-derived this run.

- **Design fork — resolved by owner, run 5 (was Open Question 5)**: mutate
  the existing segment row in place (`segments.agent`/`resume_token`, same
  ordinal — what `switch-agent.sh` already does) is the chosen approach; the
  rotate-to-new-segment alternative is not being pursued. The owner
  explicitly chose the simpler, non-recommended-by-research option
  ("overwriting!"). Implementation must still account for **both** refusal
  comparison points (`prepare.go:219-227` and the resume-safety check at
  `prepare.go:1004`), not just the first, since both currently compare
  against the (now overwritten) `segment.Agent` value. `[V]`
- **Concurrency/race with an in-flight run — implementation detail, not a
  design question, still to be specified in Plan/Implement**:
  `switch-agent.sh` requires the daemon to be stopped specifically to avoid
  a run started under the old agent finishing and writing a `runs.agent` row
  that disagrees with a `segments.agent` mutated underneath it. An
  automatic, live-daemon implementation must reproduce this guarantee some
  other way — most likely by only applying the mutation while holding the
  same `sessions.lock_owner`/in-process `inflight` guard `turn.Execute`
  already takes for the whole turn, i.e., treating the switch as part of
  `prepare()` under the existing per-session serialization rather than as an
  out-of-band write. This is unchanged by the owner's answers and is a
  mechanical detail for Plan/Implement, not an open design fork. `[V]`
- **In-flight subprocess behavior — resolved by owner, run 5 (was Open
  Question 6)**: let currently-running work finish under the old agent; do
  not actively kill a running subprocess on a mid-flight label change. The
  switch takes effect starting with the next dispatched task on that issue.
  This is the owner's explicitly recommended option. No new
  cancellation/kill wiring beyond what exists today
  (`continuation_dispatch.go:104-111`'s pending-continuation cancellation,
  unaffected) is required for this behavior. `[V]`
- **Trigger model ambiguity — resolved by owner, run 5 (was Open Question
  7)**: a bare label edit with no new comment does **not** need its own new
  trigger/reconciliation pass; the existing per-comment dispatch, which
  already re-reads the label fresh (`resolveAgent`, `internal/daemon/
  agent.go:94-96`), is sufficient. This is the owner's explicitly
  recommended option and confirms no new plumbing is needed for detection —
  only the mutation itself (now specified) is new. `[V]`
- **Registry hardcoding must not be inherited**: `switch-agent.sh` hardcodes
  `claude|codex` because it runs outside the daemon process and cannot see
  the registry built at startup from config. The in-daemon implementation
  must read from the actual `RunnerSet`/registry so a third registered agent
  (if one is ever added) is switchable without a script change. Unaffected
  by the owner's run-5 answers. `[V]`
- **Tests currently assert the opposite of the new requirement** —
  `internal/turn/execute_test.go:316-378` explicitly assert that an agent
  mismatch touches nothing. These are the correct place to look for what
  currently passes and will need rewriting, not just new tests added
  alongside them. Unaffected by the owner's run-5 answers. `[V]`
- **`switch-agent.sh` fate — resolved by owner, run 5 (was Open Question
  8)**: delete it once the automatic switch is implemented. The owner
  explicitly declined the "keep as fallback" option ("no, we delete it!").
  Plan/Implement should treat removing this file as part of the change, not
  a follow-up. `[V]`
- **`.codex/` parity note in `AGENT_ROSTER.md` — resolved by owner, run 5
  (was Open Question 9)**: ignore; drop from tracking. Not part of this
  change. `[V]`
- **Owner-stated scope/complexity constraint (new, run 5)**: the owner
  expects this to be a small, simple change — quoted: *"keep it simple,
  basically this is only a few lines that need to change, no rocket science
  here!"* Read together with the owner's other run-5 answers (overwrite in
  place, let in-flight work finish naturally, no new label-watching
  infrastructure, delete the manual script rather than keep it as a parallel
  path), this is a directive for Plan and Implement: do not introduce new
  segment-rotation machinery, new polling/reconciliation passes, new kill/
  cancellation wiring, or a fallback/dual-path design for `switch-agent.sh`.
  This is recorded here as owner guidance to be honored, not something for
  a research pass to independently verify against the code or use to
  re-scope the investigation — this run intentionally did not re-derive or
  re-litigate scope against this constraint. `[O, new run 5]`
- **Git history for the original refusal decision (and any prior discussion
  of reverting it, e.g. in session `lf-120-agent-switch-refused-leaves-the-
  session`) could not be retrieved in run 2 or run 3** — no agent in either
  run's roster had shell/`git` access. The in-code rationale (doc comments,
  tests, `switch-agent.sh`'s own header) substantially recovers the design
  intent, but a full paper trail (was reversing this ever proposed and
  rejected before, and why) is not confirmed. This is unrelated to the
  owner's run-5 answers and remains a non-blocking residual gap. `[V,
  limitation, unchanged since run 3]`
- **A prior run's own dispatch could not be verified from the repository**
  — see the dedicated subsection under Codebase Context. Noted as a
  limitation of what a repo-only research pass can confirm, not a defect in
  the mechanism itself. Unrelated to the owner's run-5 answers. `[V, run 3]`

## Open Questions

1. **Resolved run 2** — what issue #228 wants, and which of run 1's
   candidate interpretations applies. Owner confirmed: reverse
   `ReplyAgentSwitchRefused` into an actual switch, label-driven, no
   restart, session loss accepted. `[O]`
2. **Resolved run 2, unchanged since** — why the refusal was built the way
   it was. The in-code rationale (`reply.go:51-57` doc comment,
   `execute_test.go:316-378` invariants, `switch-agent.sh`'s own header)
   recovers this without needing session 120's artifacts. Residual,
   non-blocking gap: the originating commit/PR discussion (was reverting
   ever proposed/rejected before) is still unconfirmed — see Constraints &
   Risks. This residual gap is unrelated to the owner's run-5 answers and
   was not addressed by them. `[V, partially resolved]`
3. **Resolved run 2** — runtime axis confirmed in scope; `internal/render`
   harness-rendering axis confirmed out of scope. `[O]`
4. **Resolved run 2** — yes, the refusal should become a switch, not
   just better labeling UX. `[O]`
5. **Resolved run 5** — mutate-in-place vs. rotate-to-new-segment for the
   switch. Owner decision: **overwrite the existing segment record in
   place** (matches `switch-agent.sh`'s existing mutation; the
   non-recommended, simpler option). `[O, resolved run 5]`
6. **Resolved run 5** — what happens to an already-running subprocess when
   the label changes mid-flight. Owner decision: **let it finish**; the
   switch takes effect starting with the next dispatched task on the issue.
   No active cancellation of a running subprocess. `[O, resolved run 5]`
7. **Resolved run 5** — does a bare label edit with no new comment need its
   own trigger. Owner decision: **no new work needed** — rely on the
   existing behavior of re-reading the label fresh on the next comment/turn.
   `[O, resolved run 5]`
8. **Resolved run 5** — fate of the manual `switch-agent.sh` script. Owner
   decision: **delete it** once the automatic path is implemented (not kept
   as a fallback). `[O, resolved run 5]`
9. **Resolved run 5** — the unrelated `.codex/` parity note in
   `AGENT_ROSTER.md`. Owner decision: **ignore it, drop from tracking**.
   `[O, resolved run 5]`

All nine of the brief's open questions are now resolved. The only remaining
item touching this area is the non-blocking residual gap noted under
Question 2 and in Constraints & Risks (the originating commit/PR history for
`ReplyAgentSwitchRefused` could not be retrieved for lack of `git` access in
the research roster) — this was never one of Questions 5-9, was not raised
by the owner in run 5, and does not block Plan or Implement; it would only
matter if Plan mode specifically wants that paper trail.

## Recommended scope for Plan mode

Updated run 5 to reflect the owner's decisions; items that previously said
"Plan mode must decide" are now settled and restated as directives.

- **In scope**: reversing `ReplyAgentSwitchRefused` (`internal/report/
  reply.go`) into an actual mid-conversation agent switch, triggered by the
  existing `Coding Agent:` label mechanism (`internal/daemon/agent.go:
  resolveAgent`), implemented inside `internal/turn/prepare.go`'s existing
  comparison points (`prepare.go:219-227` and the resume-safety check at
  `prepare.go:1004`) rather than as new plumbing to detect the label
  change — that detection already exists and already runs without a
  restart.
- **Settled by the owner, not for Plan mode to re-decide**: overwrite the
  existing `segments.agent`/`resume_token` row in place (Question 5); let
  an already-running subprocess finish under the old agent, with the switch
  applying starting with the next dispatched task (Question 6); no new
  trigger/reconciliation plumbing for a bare label edit — the existing
  per-comment label re-read is sufficient (Question 7); delete
  `switch-agent.sh` as part of this change rather than keeping it as a
  fallback (Question 8); ignore the `.codex/` `AGENT_ROSTER.md` note
  (Question 9).
- **Concurrency/locking still needs to be specified mechanically** (not a
  design fork, just an implementation detail): the plan should state
  explicitly how the switch avoids the race `switch-agent.sh` avoids today
  by requiring the daemon to be stopped — most likely by performing the
  mutation under the same per-session lock/inflight guard `turn.Execute`
  already acquires.
- **Must not hardcode agent kinds** — implementation should read from the
  existing `RunnerSet`/registry (`internal/turn`, `internal/daemon/
  runners.go`), unlike `switch-agent.sh`.
- **Must account for and rewrite** the existing invariant tests
  (`internal/turn/execute_test.go:316-378`) that currently assert a
  mismatch touches nothing, plus the `reply.go:51-57` rationale comment,
  which both currently describe the behavior being reversed.
- **Remove `switch-agent.sh`** as part of this change (owner decision,
  Question 8) — it is no longer being kept as a fallback or reference once
  the automatic path exists.
- **Honor the owner's simplicity constraint**: the owner expects this to be
  a small, few-lines change, not a larger redesign. Plan mode should size
  the implementation accordingly — overwrite in place, no new segment
  rotation, no new polling/kill infrastructure, no dual-path
  script-plus-automation design — rather than building the
  more-defensive alternatives this brief's earlier runs surfaced as
  options (e.g., segment rotation, subprocess cancellation, keeping
  `switch-agent.sh` as a fallback). Those alternatives are recorded above
  for context but are explicitly not what the owner asked for.
- **Out of scope**: `internal/render` harness rendering; adding a third
  coding-agent tool; any CLI-flag-based selection surface; the `.codex/`
  `AGENT_ROSTER.md` note — none of these were requested by the owner.
- **Residual gap for a follow-up pass if needed, non-blocking**: the
  originating commit/PR for `ReplyAgentSwitchRefused` (and any related
  discussion in session `lf-120-agent-switch-refused-leaves-the-session`)
  was not retrievable in run 2 or run 3 for lack of shell/`git` tool access
  in the available roster. If Plan mode wants that paper trail specifically
  (rather than the in-code rationale already captured above), it needs a
  pass with `git log`/`git blame` access. This does not block starting
  Plan.

## Status after run 5

With Open Questions 5 through 9 now answered by the owner, every structural
decision this brief previously flagged as blocking Plan mode is resolved:
the mutation is specified (overwrite in place), the in-flight-run behavior
is specified (let it finish, switch applies to the next task), the trigger
model is specified (no new plumbing, reuse existing per-comment label
re-read), and the fate of `switch-agent.sh` is specified (delete it). The
owner has also constrained the expected implementation size (a few lines,
not a redesign), which this brief records as a directive for Plan/Implement
rather than something this run re-derived from the code.

**Nothing structurally open remains in this brief.** The only unresolved
item is the pre-existing, explicitly non-blocking residual gap on the
original design-rationale paper trail (Question 2's residual, git-history
access), which does not require a decision before Plan proceeds. **This
brief is ready to hand to Plan mode.**
