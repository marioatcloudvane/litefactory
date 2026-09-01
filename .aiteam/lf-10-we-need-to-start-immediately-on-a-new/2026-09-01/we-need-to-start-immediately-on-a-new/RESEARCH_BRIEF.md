# Research Brief — we-need-to-start-immediately-on-a-new (issue #10)

2026-09-01. Headless run. Branch `lf/10-we-need-to-start-immediately-on-a-new`.

**Provenance tags** used throughout:

- `[V]` verified this run by reading the current source at the cited `path:line`.
- `[O]` reported by the owner on issue #10.
- `[W]` second-hand or external, not verified here.
- `[A]` carried from an archived `.aiteam/` document, re-checked against source where load-bearing.

Every `path:line` in this brief was re-pinned this run. Line numbers quoted inside archived
`.aiteam/` documents were **not** trusted — this session's own history records citation rot three
times, and one instance was found again here (see *Archive corrections*).

---

## Headline finding

**Most of what issue #10 asks for already shipped, in August 2026, under issue #6.** An open issue
that is assigned to the owner, has a non-empty body and has no comments already runs its body as the
first instruction, with no comment required. That is `Daemon.runIssueBody`
(`internal/daemon/tick.go:1074-1125`), gated on `t.issue.AssignedTo(d.filter.Identities().OwnerUserID)`
at `internal/daemon/tick.go:1077`. `[V]`

Of the owner's four workflows, **one already works, one is the status quo, and two are the same
missing rule**:

| # | Owner's workflow | Status today |
|---|---|---|
| 1 | random author → first sight → owner assigns → **owner comments** → run | works — the comment path `[V]` |
| 2 | owner authors → first sight → owner assigns → run | **does not work** `[V]` |
| 3 | owner authors → owner assigns → first sight → run | **already works** (AC-2.16b) `[V]` |
| 4 | random author → first sight → owner assigns → run | **does not work** `[V]` |

Workflows 2 and 4 are the *same* rule, because the existing gate is **assignment, not authorship** —
who opened the issue is irrelevant to it (`internal/github/issues.go:52-59`). `[V]` So there is one
gap, not two.

**The one gap is an ordering condition, not a missing capability.** The body gate is evaluated
exactly once per session, inside first-sight bootstrap, and is structurally unreachable afterwards:
`Daemon.work` chooses `bootstrap` versus `drain` on whether a watermark row exists
(`internal/daemon/tick.go:940-948`), `bootstrap` plants that row *before* it calls `runIssueBody`
(`internal/daemon/tick.go:1003, :1015`), and `drain` never looks at the body. `[V]` An issue first
seen while unassigned therefore has its one chance consumed, and no later assignment can bring it
back.

**And a second, separate finding that probably explains the owner's own experience: issue #10 has no
body.** `[O]` A body that is empty or whitespace is refused at `internal/daemon/tick.go:1079`, and
that refusal is silent — a debug log line and nothing on the thread (AC-2.16c,
`internal/daemon/tick.go:1084-1088`). `[V]` So even a perfectly assigned, perfectly timed issue #10
would have started nothing, and would have said nothing about why. Plain prose *does* spawn a run —
`Command.Spawns()` is true for `KindFreeText` (`internal/turn/command.go:128`) `[V]` — so it is
emptiness, not a missing `/spike`, that stopped it.

**Third finding, a documentation one, and probably why the feature is invisible.**
`internal/daemon/doc.go` describes a worker as "plant the watermark of a task seen for the first time
and process none of its existing comments (AC-2.10), or fetch the comments above the watermark"
(`internal/daemon/doc.go:38-41`). The issue-body path is not mentioned in that file at all — the
strings `assign`, `first sight` and `issue body` appear nowhere in it. `[V]` In a codebase whose
`doc.go` files are the specification (`.aiteam/APP_CONTEXT.md:98-105`), a shipped trigger that the
package spec does not name is a trigger nobody knows about.

---

## Request

Verbatim, from @marioatcloudvane on issue #10 (title: *"We need to start immediately on a new sight
of an issue"*, no body): `[O]`

> Right now, I first need to add a comment before litefactory starts doing stuff. this should be now
> different, the first sight of an issue should already trigger litefactory as long as the owner is
> the litefactory account (e.g. marioatcloudvane). But one thing we need to take care of here is that
> we should also wait for assignee change, because someone else can add an issue that is then left
> unattended!

followed by the four workflows tabulated above.

Two things in that wording need reconciling with the codebase, and are handled under *Identity*
below: "the owner is the litefactory account" conflates two accounts this codebase requires to be
different, and "as long as the owner is the litefactory account" reads as an *authorship* test, which
this codebase has already ruled is not an authorisation.

---

## Codebase context

### What triggers a run today, end to end

Discovery, all on the Tick goroutine (`internal/daemon/tick.go:118`): `[V]`

1. **Backoff check** — `internal/daemon/tick.go:121`.
2. **Poll cursor** — `d.db.InitPollCursor(ctx, startedAt)` at `internal/daemon/tick.go:132`. This is
   the whole of AC-2.9's "bootstrap tick": a conditional insert in
   `internal/store/pollcursor.go` that plants the cursor at *now* only when
   `tasks_updated_since` is still NULL, so registering a busy repository does not replay its history.
   `listingWindow` (`internal/daemon/tick.go:213-218`) withholds the 5-minute `ListingOverlap`
   (`internal/daemon/tick.go:205`) on that first tick only, detected by `cursor.LastTickAt.IsZero()`.
3. **Two forge listings per repository, plus a local one.**
   - `ListIssues` (`internal/daemon/tick.go:281`) → `internal/github/issues.go:141-207`. Query
     parameters, verbatim from source: `State: "open"` (`:154`), `Sort: "updated"` (`:157`),
     `Direction: "asc"` (`:158`), `Since` set only when non-zero (`:161-163`), `PerPage` (`:169`).
     Pull requests are dropped (`:190`). **There is no `assignee` query parameter, deliberately** —
     `internal/github/issues_test.go:386-389` fails the build if one appears, on the grounds that
     "who an issue is assigned to is decided here and not by a query".
   - `ListCommentedIssues` (`internal/daemon/tick.go:526`) → `internal/github/comments.go:138-191`,
     `Sort: "updated"`, `Direction: "asc"`, `Since`. Returns issue *numbers*; each number the first
     listing did not already return costs one `GetIssue` (`internal/daemon/tick.go:545`).
   - `ListBacklogSessions` (`internal/daemon/tick.go:351`) — the `sessions.needs_poll` union, which
     costs no API quota (`internal/store/migrations/0002_session_backlog.sql:40-45`).
4. **`Daemon.sight`** (`internal/daemon/tick.go:394-419`) — skips anything not open *before*
   `GetOrCreateSession` (`:395-403`), so a closed issue leaves no rows behind; otherwise creates or
   finds the `sessions` row and produces a candidate.
5. **Dispatch** (`internal/daemon/tick.go:619-705`), two-level round-robin, then
   `Daemon.offer` (`:748-861`): in-flight enter → mark backlog → pool slot → materialise (lazy
   `GetIssue` at `:896`, re-checks open at `:903`) → second backoff check → `go` worker.

Then, per worker (`Daemon.work`, `internal/daemon/tick.go:933-956`): `[V]`

- `GetWatermark`. **If the row does not exist → `bootstrap`. If it does → `drain`**
  (`:940-948`). This single branch is where the whole of issue #10 lives.
- `bootstrap` (`:990-1016`) lists every existing comment (`ListCommentsAfter(..., 0)`), plants the
  watermark at the highest existing id, processes none of them (AC-2.10), and **then** calls
  `runIssueBody` (`:1015`).
- `drain` (`:1149-1175`) lists comments above the watermark and hands each to `handleComment`
  (`:1268-1314`), which classifies (`Filter.Admit`), parses (`ParseCommand`), reacts with 👀, and
  runs. **`drain` never touches the issue body.**

**Why a comment is required today**, stated precisely: it is not that the body is ignored — the body
has been a legal trigger since issue #6. It is that the body's *only* window is the instant the
session's watermark row is created, and by the time a human has assigned the issue that window is
usually already shut. A comment is the only trigger that has a second chance, because the comment
watermark is a monotonic id and every later comment is above it.

### The issue-body path, and the trigger id of `0`

`Daemon.runIssueBody` (`internal/daemon/tick.go:1074-1125`). `[V]` The gate is three checks in a
switch (`:1075-1083`), each producing a refusal string:

| Check | Line | Refusal |
|---|---|---|
| `!t.issue.AssignedTo(d.filter.Identities().OwnerUserID)` | `:1077` | "it is not assigned to the owner" |
| `strings.TrimSpace(t.issue.Body) == ""` | `:1079` | "its body is empty" |
| `len(comments) != 0` | `:1081` | "it already has comments, which are the conversation to follow instead" |

Openness is a fourth condition but is not re-checked here — it is guaranteed upstream by
`Daemon.sight` (`:395`) and `Daemon.materialise` (`:903`), and the doc comment says so
(`:1025-1031`).

After the gate: `turn.ParseCommand(t.issue.Body)` (`:1096`); `KindNone` returns silently (`:1104`);
a command that spawns nothing returns silently and posts nothing (`:1113`, AC-2.16c); otherwise
`trigger := turn.Trigger{CommentID: 0, Body: "", Author: t.issue.Author.Login}` (`:1123`).

`Trigger.CommentID == 0` is load-bearing in three separate places: `[V]`

- **Idempotency.** `runs` carries `UNIQUE (session_id, trigger_comment_id)`
  (`internal/store/migrations/0001_init.sql:103`), so one body run per session, ever.
  `internal/store/store_test.go:908-946` proves it: a second allocation at trigger 0 returns
  `ErrDuplicateRun`, and a negative trigger is refused outright.
- **The watermark.** Reporting a run advances the comment watermark to the trigger id, so any
  positive value — the issue's own forge id, say — would push the watermark past every comment the
  issue will ever receive. Zero makes that a `MAX(0, 0)` no-op (AC-2.23,
  `internal/turn/execute.go:185-194`).
- **Prompt framing.** `internal/turn/prepare.go:152-157`:
  `bodyIsInstruction := req.Trigger.CommentID == 0`. This is the answer to "what would the prompt look
  like with no triggering comment" — see below.

`turn.Execute` already handles a repeat cleanly rather than erroring: it looks the run up first with
`GetRunByTrigger` (`internal/turn/execute.go:400`) and returns `DispositionAlreadyRan` /
`DispositionAlreadyReported` (`:427-434`). `[V]` So the machinery for "check the body gate again and
find it has already fired" exists and is exercised.

### What the prompt of a comment-less run contains

Fully answered by the existing zero-comment-id path — this is **not** new work. `[V]`

`internal/turn/prepare.go:157` sets `bodyIsInstruction`, which becomes
`harness.PromptInput.BodyIsInstruction` (`internal/harness/prompt.go:226-256`). Three things change
against the comment path, per that doc comment:

- no comment note is rendered, because there is no comment;
- the Context sentence goes singular, because one note is left;
- the issue note keeps its `opened by X` attribution line but carries a *pointer* to the instruction
  rather than a second, capped copy of it.

The instruction itself is `req.Command.Text` — the parsed body — and is uncapped, as every
instruction is. The attribution line is what makes running a stranger's body safe
(`internal/harness/prompt.go:242-245`, `internal/github/issues.go:52-59`).

### Assignment: what the tree reads today

- `github.Issue.Assignees []User` — `internal/github/issues.go:66`. It rides the issue payload that
  is already fetched, so it costs **no extra request** on either the listing path or the single-issue
  path (AC-2.24). `[V]`
- `Issue.AssignedTo(userID int64) bool` — `internal/github/issues.go:94-104`. Matches the numeric id
  only; a zero id matches nothing, because zero is what GitHub reports for a deleted account. `[V]`
- **Exactly one production call site in the whole tree**: `internal/daemon/tick.go:1077`. `[V]`
  Everything else that mentions assignment is `internal/github` itself or a test.
- **Nothing anywhere persists an assignee.** The `sessions` table has no assignee, author or body
  column (`internal/store/migrations/0001_init.sql:50-64`). `[V]`

The consequence matters for how Plan phrases the rule: **"the assignment changed" is not expressible
today, and cannot be made expressible without either storing prior state or calling an endpoint the
daemon does not have.** What *is* expressible, cheaply and with no schema change, is the *state*
"assigned to the owner right now". Under a polling daemon with no webhooks, a state test is the
natural shape and an event test is not.

### Can the daemon even notice a late assignment?

The framing in the task that commissioned this brief — that a re-assignment does not change
`updated_at`'s position in a `created_at`-ordered listing — does not apply here. **Nothing in this
codebase orders by `created_at`.** `ListIssues` sorts by `updated` ascending and filters on `since`
against `updated_at` (`internal/github/issues.go:157-163`), and each tick asks from
`cursor − 5 minutes` (`internal/daemon/tick.go:205, :213-218`). `[V]`

So the question reduces to one external fact: **does GitHub bump an issue's `updated_at` when its
assignees change?** `[W]` — this is *not* established anywhere in this tree. The codebase has
measured that a *comment* bumps `updated_at` (`internal/github/comments.go:116-119`, measured on
`marioatcloudvane/litefactory#4`) but has measured nothing about assignment. A background agent
asserted during this run that it does bump; that assertion carried no citation and is recorded here
as unverified. It is the single highest-value cheap probe available to Plan, because the answer
decides the size of the change:

- **If `updated_at` bumps on assignment**: discovery already re-lists the issue on the very next
  tick. The entire change is inside the worker, needs no new endpoint and no new listing, and the
  per-tick API cost is unchanged.
- **If it does not**: the issue is invisible to both existing listings after the cursor has passed
  it, and the options get materially worse — either keep every unassigned open issue permanently in
  the `needs_poll` backlog (see *Risks*, R6), or add a forge endpoint (issue events / timeline),
  which means editing `daemon.Forge` (`internal/daemon/daemon.go:78-84`) **and**
  `TestClientMethodSetIsPinned` (`internal/github/surface_test.go:64-101`) — a guard that exists
  precisely so that growing the forge surface is a decision someone has to write down. `[V]`

The comment listing cannot help either way: there is no comment.

### Identity

What the store and the code already carry: `[V]`

| Concept | Where it lives | Used for |
|---|---|---|
| **Owner** (the human) | `identities` table, PK `(forge, role)`, `role = 'owner'`, numeric `user_id` (`internal/store/migrations/0001_init.sql:17-25`); derived by `gh` at `litefactory init` (`internal/cli/cmd_init.go:105, :131`); surfaced as `turn.Identities.OwnerUserID` (`internal/turn/classify.go:103`) | authorising comments (`classify.go:249`) **and** authorising bodies via assignment (`tick.go:1077`) |
| **Bot** (the account litefactory posts as) | same table, `role = 'bot'`; `turn.Identities.BotUserID` | the self-filter (`classify.go:265`) |
| **Issue author** | `github.Issue.Author` (`internal/github/issues.go:60`), per-issue, not stored | *attribution only* — explicitly not authorisation (`issues.go:52-59`) |
| **Assignees** | `github.Issue.Assignees` (`:66`), per-issue, not stored | the body gate, one call site |

Three reconciliations the owner's wording needs:

1. **"the owner is the litefactory account" conflates owner and bot.** This codebase requires them to
   be *different accounts* and refuses to construct a `Filter` otherwise —
   `Identities.Validate` returns an error when `OwnerUserID == BotUserID`
   (`internal/turn/classify.go:125-127`), because an account that is both reads its own reports as
   instructions (AC-0.6). `[V]` The parenthetical "(e.g. marioatcloudvane)" makes clear the human
   owner is meant, which is the right account; the phrase "the litefactory account" is not.
2. **"as long as the owner is the litefactory account" reads as an authorship test.** This codebase
   has already ruled once that authorship is not authorisation, and kept that ruling deliberately
   when it reversed the surrounding rule: AC-2.16a states that an unassigned issue starts nothing
   "however its body is worded — including a body the owner wrote himself"
   (`.aiteam/AC_AMENDMENTS.md:78-80`), and the ledger calls that "the half that was load-bearing"
   (`:107-109`). `[A]`, re-checked against `internal/github/issues.go:52-59` `[V]`. The owner's own
   four workflows each contain an explicit assignment step, so the assignment gate satisfies all four
   without an authorship axis. Adding one would be a **new authorisation surface**, not a widening of
   an existing one.
3. **"we should wait for assignee change, because someone else can add an issue that is then left
   unattended" is already the shipped behaviour.** That is exactly AC-2.16a, enforced at
   `internal/daemon/tick.go:1077` and tested by `TestAnUnassignedIssueBodyStartsNothing`
   (`internal/daemon/tick_test.go:508-535`) and `TestANonOwnerAssigneeDoesNotFireTheBody`
   (`:774`). `[V]` Plan should not re-invent it.

### Prior art

`[A]`, with the load-bearing claims re-checked against source this run.

- **`.aiteam/lf-6-first-comment-pickup/`** is the direct ancestor and is where this feature came from.
  Its plan established AC-2.16a–d, AC-2.10b, AC-2.23, AC-2.24, AC-2.25, AC-2.26 and AC-2.22. Its
  research brief argued *against* issue bodies on four grounds; the plan then reversed the axis from
  authorship to assignment and shipped. Its brief's §4 (idempotency options A–E) and its `CreatedAt`
  newness test are **superseded by its own plan** and must not be cited as live design. Its
  `MANIFEST.md` still says Implement is "not started"; that is stale — the code is in the tree.
- **`.aiteam/main/2026-08-25/fix-comments-pickup/`** diagnosed permanent comment loss from a
  zero-overlap monotonic cursor and produced `ListingOverlap`. Its most relevant surviving statement
  for issue #10: **`bootstrap` is the "first sight" path, not the "new issue" path**, and it plants
  the watermark from a listing that may be stale, with no deferral escape hatch. That residual is
  recorded as still open in that plan and is not closed by anything in the ledger.
- **`.aiteam/main/2026-08-25/comment-intent/`** made free text spawn runs (`KindFreeText`). Its large
  `IMPLEMENTATION_PLAN.superseded.md` mostly did not ship; treat it as a design document, not a
  record of behaviour. Its D-CI-3 (claiming D-X superseded) is formally ruled wrong at
  `.aiteam/AC_AMENDMENTS.md:175-202`.
- **`.aiteam/AC_AMENDMENTS.md`** — the entries that bear on this work: AC-2.16 (reversed in part →
  AC-2.16a–d, `:62-116`); AC-2.8 (amended — two listings per repo plus issue reads, `:359-419`);
  AC-2.9 (**reviewed, unchanged**, `:1131-1141` — the cursor still plants at *now* and the first tick
  still withholds the overlap); D-A (amended — three discovery sources, hold-back gone, `:1013-1064`);
  AC-2.12, AC-2.13, AC-2.17, AC-3.1 (all amended by the parallelisation work); D-X (**ruled, stands**
  — no run-rate circuit breaker, `:119-203`); and a rejected `runs.owner` column whose stated reason
  was that "the migration budget went to `sessions.needs_poll`" (`:1286-1312`).

### Archive corrections found this run

- `fix-comments-pickup/RESEARCH_BRIEF.md` records the issue listing query as
  `state=all&sort=updated&direction=asc&…`. The current source sends `state=open`
  (`internal/github/issues.go:154`), with a comment explaining that this is a payload saving and
  explicitly *not* the AC-2.22 guard. `[V]` Do not carry `state=all` forward.
- `internal/daemon/doc.go` does not mention the issue-body trigger at all (see *Headline finding*).
  `[V]`

---

## The four workflows, restated as trigger rules

**As they are today.** `[V]`

| Rule | Condition | Result |
|---|---|---|
| R-comment | open ∧ comment above watermark ∧ author id = owner id ∧ command spawns | run, trigger = comment id |
| R-body | open ∧ **watermark row does not yet exist** ∧ assigned to owner ∧ body non-empty ∧ zero comments ∧ command spawns | run, trigger = 0 |

**As they would need to be.**

| Rule | Condition | Result |
|---|---|---|
| R-comment | unchanged | unchanged |
| R-body′ | open ∧ assigned to owner ∧ body non-empty ∧ zero comments ∧ command spawns ∧ **no run yet exists at trigger 0 for this session** | run, trigger = 0 |

The whole delta is the middle clause: replace "first sight" (a moment) with "the body has not run
yet" (a durable fact). That is a one-clause change in the *rule* and a genuinely larger change in the
*argument*, because it moves "exactly once" from being a property of control flow to being a property
of a database constraint. See R3 below.

**On the conjunction of "first sight" and "assigned to the owner".** The owner's framing treats these
as two conditions that must both hold. In the code they are not two conditions — "first sight" is not
a condition at all, it is the *only moment the assignment condition is ever asked*. That is the whole
of the bug. An issue seen *after* it is assigned satisfies both and runs (workflow 3, works today).
An issue seen *before* it is assigned satisfies neither at the one instant it is asked, and is never
asked again (workflows 2 and 4). Whether the issue was ever "unattended" in the owner's sense is not
recorded anywhere; it is simply never revisited.

**Overlaps and conflicts between the four:**

- Workflows 2 and 4 are one rule. Authorship does not enter the gate, so "I add an issue" and
  "random user adds an issue" produce identical behaviour once the assignment is the same. Plan
  should treat them as a single story, not two.
- Workflow 3 already works and is the *only* one that works without change. Plan must be careful not
  to regress it: the smallest-looking change (moving the gate out of `bootstrap`) touches the path
  that workflow 3 runs on.
- Workflow 1 (the status quo) stays and must keep working. In particular, the "zero comments" clause
  means an issue that has been commented on will never fire its body — so workflow 1 and workflow 4
  are mutually exclusive in practice for the same issue, and that is fine.
- The owner's stated protection ("wait for assignee change so an unattended third-party issue is not
  picked up") is preserved unchanged by R-body′: assignment is still the sole authorisation.

---

## Size and shape of the change, per workflow

| Workflow | Change needed | Rough shape |
|---|---|---|
| 3 (assign then first sight) | **none** | already shipped |
| 1 (comment) | **none** | already shipped |
| 2 and 4 (late assignment) | one rule change in `internal/daemon`, plus its argument, plus a stampede control | see below |

For workflows 2 and 4, three mechanisms are available. **This brief does not choose between them —
that is Plan's job — but the trade-offs are asymmetric enough to be worth stating now.**

- **(a) Re-run the body gate from the drain path as well**, relying on the existing
  `UNIQUE (session_id, trigger_comment_id)` and `GetRunByTrigger` for once-ever. Smallest code
  change; costs no schema change, no new endpoint, no extra API request (the labels and assignees
  already ride the issue payload the tick has in hand). Its cost is entirely in the *argument*: four
  doc comments currently state that exactly-once is a control-flow property and become untrue
  (`internal/daemon/tick.go:1043-1051`; `internal/turn/execute.go:185-194`;
  `internal/turn/prepare.go:152-157`; `internal/harness/prompt.go:251-255`), and the codebase's own
  style rule makes fixing them part of the change (`.aiteam/APP_CONTEXT.md:322-328`). It also adds an
  extra `ListCommentsAfter`-derived comment-count check on the drain path, which drain already has in
  hand.
- **(b) A new `sessions` column** recording that the body has been considered or has fired
  (migration `0003_*.sql`). More explicit, survives a `runs` table cleanup, and gives an obvious
  place to hang a "considered and refused, do not reconsider" state. Costs a migration; note the
  ledger's precedent that a column was rejected once on migration-budget grounds
  (`.aiteam/AC_AMENDMENTS.md:1286-1312`).
- **(c) Keep `needs_poll` set** for sessions whose body has not fired. **Assessed here as the worst of
  the three** and recorded so Plan does not have to rediscover it: `needs_poll` is the dispatch
  backlog, dispatched sessions consume pool slots, and this would put *every open unassigned issue in
  every registered repository* permanently into it. That inverts the purpose of the partial index
  (`internal/store/migrations/0002_session_backlog.sql:42-45`), whose stated design point is that
  "the steady state — every session idle — carries no index entries at all". It becomes worth
  reconsidering only if the `updated_at` probe comes back negative.

---

## Constraints and risks

**R1 — Stampede. This is the largest item and it costs real money.** `[A]` re-checked `[V]`.
The strongest argument in the issue-#6 research brief was that `bootstrap` is the *first sight* path,
not the *new issue* path — so a mechanism that reacts to first sight reacts to every issue in a
repository the first time that repository is polled. Today the one-shot nature of the gate contains
that: once a session is bootstrapped, no amount of later assigning can start anything. **R-body′
removes exactly that containment.** Assigning forty existing open issues to yourself in one
gesture — a plausible piece of backlog grooming — becomes forty run requests, dispatched three at a
time (`config.DefaultMaxConcurrentRuns = 3`, `internal/config/config.go:238`) but with no total
bound at all. D-X still forbids a run-rate circuit breaker and was re-affirmed as recently as the
parallelisation work (`.aiteam/AC_AMENDMENTS.md:119-203`), so there is no existing mechanism to lean
on. Plan must decide what contains this *before* code is written. Options worth weighing include
bounding by issue age or by the assignment being recent, requiring the body to be non-empty (already
required, and it filters a lot of old issues), or an explicit opt-in.

**R2 — the `lf:tools=` silent refusal becomes far more reachable.** `[V]` When the trigger comment id
is 0, `Daemon.refuseTools` refuses the run, logs it and **posts nothing**, because there is no
comment to answer (`internal/daemon/tick.go:1473-1511`). That doc comment names itself as "the only
branch to change if that trade is revisited". Today the branch is reachable only on a first sight of
an assigned issue. Under R-body′ it is reachable on any tick, for any assigned issue carrying a
malformed or unknown `lf:tools=` label — and the owner's experience would be a labelled issue that
does nothing, forever, silently. This is the second-order effect most likely to be missed.

**R3 — "exactly once" changes register.** `[V]` Today it is a control-flow property, argued at
`internal/daemon/tick.go:1043-1051`: the watermark is planted before the body runs, so the next tick
takes `drain` and the function is unreachable. Under R-body′ it becomes a database property resting on
`UNIQUE (session_id, trigger_comment_id)`. The constraint is genuine and directly tested
(`internal/store/store_test.go:908-946`), and `turn.Execute` already degrades to a clean
`DispositionAlreadyRan` rather than an error (`internal/turn/execute.go:400-434`). So the property
survives — but the *stated reason* does not, in four files, and this codebase treats a comment its
change falsified as part of the change.

**R4 — `Resume` + `BodyIsInstruction` would become reachable, and keeping AC-2.16d is what stops it.**
`[V]` `internal/harness/prompt.go:251-255` states that the combination "is not reachable today — a
body fires at most once per issue, so a body-triggered run is always the first run of its
conversation", and that the resume fallback rebuilds the same input with `Resume` flipped. If the
body could fire after earlier runs, that combination becomes live and the prompt would render with no
Context section at all. **Retaining the "zero comments" clause closes this**: a session with a prior
run necessarily has a report comment on the thread, so the body gate refuses it. This is a concrete
reason to keep AC-2.16d rather than a stylistic one.

**R5 — AC-2.16d stops being a stable premise and becomes time-dependent.** `[V]` The doc comment
justifies it as "an issue that has already been talked on is one whose conversation is the
instruction" (`internal/daemon/tick.go:1038-1041`). Under R-body′ the same issue answers differently
at different times. The user-visible consequence, worth stating plainly for workflow 4: *random user
opens an issue, somebody comments on it, the owner then assigns it → nothing runs, and the owner must
comment.* That is a real hole in workflow 4 as the owner described it. It is defensible (and R4 says
it is also protective), but it is a limitation Plan should state rather than discover.

**R6 — backlog and `needs_poll` interaction.** `[V]` See option (c) above. Separately, note that
`sight` already creates a `sessions` row for **every open issue** in the listing window, assigned or
not (`internal/daemon/tick.go:403`) — so R-body′ adds no new rows, only new reasons to look at
existing ones.

**R7 — closed and draft issues.** `[V]` No new exposure. Openness is guarded twice, at
`internal/daemon/tick.go:395` (before any row is written) and `:903` (for backlog-reached sessions).
GitHub Projects draft items are not issues and never appear in these listings.

**R8 — the bootstrap watermark residual interacts, mildly favourably.** `[A]` `fix-comments-pickup`
recorded that `bootstrap` plants the watermark from a listing that may be stale, with no deferral
escape hatch, so a comment written moments before first sight can be swallowed silently. That
residual is still open. R-body′ *reduces* the coupling slightly, by no longer making the body gate
share that one fragile instant — but it does not close the residual, and Plan should not claim it
does.

**R9 — multi-assignee.** `[A]` The issue-#6 plan accepted as an unhandled risk that two litefactory
installs assigned to the same issue would both fire, since `naming.Branch` has no user component.
R-body′ widens the window in which that can happen from one instant to indefinitely. Still an
accepted risk, now slightly larger.

**R10 — permission surface, stated plainly.** Auto-starting on sight is a wider surface than
comment-triggered work, but the authorisation itself does not change: it is still one numeric-id
match against the owner (D-I), and still an act only the owner's account can perform. What widens is
*how long* the door stays open on any given issue — from one tick to forever. That is the honest
characterisation, and it is why R1 matters more than the authorisation question does.

---

## Out of scope

Recorded so Plan does not have to relitigate them:

- Webhooks. This is a polling daemon by design; nothing in this brief needs an inbound listener.
- Reacting to an *issue* (👀). AC-2.17 is comment-only, deliberately, because an issue reaction would
  be a new forge endpoint for a cosmetic gain (`internal/daemon/tick.go:1070-1073`).
- A run-rate circuit breaker. D-X stands (`.aiteam/AC_AMENDMENTS.md:119-203`). R1 needs *a* bound,
  but D-X is what it may not be.
- Adding an "issue authored by the owner" trigger. See *Identity*, point 2.
- Routing a body through `Filter.Admit`. It refuses a zero comment id by construction
  (`internal/turn/classify.go:313-315`) and `Filter.Classify`'s doc states there is deliberately no
  bare-body overload (`:236-244`). `[V]`
- A new forge endpoint — **unless** the `updated_at` probe forces it, in which case it stops being
  out of scope and becomes the main cost of the feature.

---

## Open questions

1. **Does GitHub bump an issue's `updated_at` when its assignees change?** The single question that
   decides whether this feature is worker-only or needs a new forge endpoint. Cheap read-only probe:
   assign an existing issue on `marioatcloudvane/litefactory` and read `updated_at` before and after.
   Nothing in this tree answers it. `[W]`
2. **What contains the stampede (R1)?** Assigning a backlog of old issues in one gesture becomes a
   request for that many runs. D-X rules out the obvious answer. Owner decision.
3. **Is the "zero comments" clause (AC-2.16d) kept?** Keeping it leaves a real hole in workflow 4
   (R5) and closes a real prompt hazard (R4). Recommend keeping; owner should confirm, since the hole
   is in a workflow he asked for by name.
4. **Should a refusal on the body path ever be visible?** Today an empty body, an unassigned issue and
   an unresolvable `lf:tools=` label are all refused silently, with a debug log line and nothing on
   the thread. Issue #10 itself is an instance (empty body). The silence is deliberate — litefactory
   posting on an issue nobody asked it about is the thing AC-2.16c avoids — but under R-body′ the
   silence becomes much more reachable (R2). Owner decision.
5. **Does the owner want the trigger to be assignment to *him specifically*, or assignment to
   *anybody*?** The gate today is "assigned to the stored owner id". The issue text implies the
   former. Confirm, since "assigned to anyone" would be a materially different and much wider rule.
6. **Which mechanism — (a), (b) or (c)?** Depends on the answer to (1) and on Plan's appetite for a
   migration.
7. **Should `internal/daemon/doc.go` be brought up to date as part of this work?** It does not
   describe the issue-body trigger at all. Recommend yes, and that it be a task rather than a
   side-effect.

---

## Recommended scope for Plan mode

**Scope first, and possibly only:** turn the issue-body gate from a first-sight moment into a durable
state test — "open ∧ assigned to the owner ∧ non-empty body ∧ no comments ∧ no run yet at trigger 0"
— evaluated on every tick that sees the issue, with once-ever enforced by the mechanism that already
enforces it. That single story closes workflows 2 and 4 together, leaves workflows 1 and 3 untouched,
and needs no new forge endpoint if open question 1 comes back positive.

**Sequence it behind the probe.** Answer open question 1 before choosing a mechanism. It is a
five-minute read-only check and it is the difference between a contained change inside
`internal/daemon` and a change that grows the forge surface and its pinned method set.

**Treat the stampede control as part of the same story, not as a follow-up.** R1 is the only item in
this brief that can cost money, and shipping the trigger without it is shipping the exposure.

**Do not scope:**

- an authorship-based trigger (contradicts AC-2.16a, which was kept deliberately);
- anything that re-implements "wait for assignee change" — it already exists;
- webhooks, issue reactions, or a run-rate limiter.

**Expect these tests to need changing**, because they encode the current behaviour rather than merely
covering it: `TestAnUnassignedIssueBodyStartsNothing` (`internal/daemon/tick_test.go:508-535`) —
its second half deliberately asserts that an *ordinary sighting* of an unassigned issue starts
nothing, which stays true, but the fixture around it will need a late-assignment case;
`TestAnAssignedIssueBodyRunsOnceAsTheFirstInstruction` (`:550-610`) — its "and again, nothing looks at
the body a second time" assertion is exactly the property whose *reason* changes;
`TestFirstSightPlantsTheWatermarkBeforeTheBodyRuns` (`:622`); and
`TestIssueBodyToolsRefusalPostsNothing` (`internal/daemon/tools_test.go:339`) if R2 is acted on.

**Expect these doc comments to need rewriting** — under this codebase's rules that is part of the
change, not cleanup: `internal/daemon/tick.go:1018-1073` (the gate), `:1043-1051` (the exactly-once
argument), `:1473-1486` (the body-path refusal trade); `internal/turn/execute.go:185-194`;
`internal/turn/prepare.go:152-157`; `internal/harness/prompt.go:226-256` (especially `:251-255`);
`internal/daemon/doc.go:38-41`; and `.aiteam/AC_AMENDMENTS.md`, where AC-2.16b's "on first sight"
wording will need an amendment entry of its own.
