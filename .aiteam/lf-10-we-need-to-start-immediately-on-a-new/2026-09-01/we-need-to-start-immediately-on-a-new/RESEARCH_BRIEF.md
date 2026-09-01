# Research Brief — we-need-to-start-immediately-on-a-new (issue #10)

2026-09-01. Headless. Branch `lf/10-we-need-to-start-immediately-on-a-new`.

- **Run 0001** — first pass. Seven open questions.
- **Run 0002** — this revision. The owner answered questions 2, 3 and 4 and asked for questions 1
  and 5 to be explained. Revised in place.

**Provenance tags** used throughout:

- `[V]` verified this run by reading the current source at the cited `path:line`.
- `[O]` reported by the owner on issue #10. Owner statements from run 0002's comment are marked
  `[O2]` where it matters which run they came from.
- `[W]` second-hand or external, not verified here.
- `[A]` carried from an archived `.aiteam/` document, re-checked against source where load-bearing.

Every `path:line` in this brief was re-pinned **again** in run 0002 against the current worktree.
Line numbers quoted inside archived `.aiteam/` documents were **not** trusted — this session's own
history records citation rot three times, and run 0002 found two further instances, one of them in
run 0001's own text and one in `.aiteam/APP_CONTEXT.md` (see *Archive corrections*).

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

### What run 0002 added

Three decisions from the owner, folded in as SD-1, SD-2 and SD-3 below, and two findings he did not
ask for but needs:

- **`max_concurrent_runs` bounds nothing about total spend**, and `sessions.needs_poll` makes the
  backlog a durable queue rather than a shed load. Forty assignments become forty runs, all of them.
  Verified against source and against the ledger's own words (SD-1). The owner has accepted this.
- **SD-2 and SD-3 interact badly.** Posting a refusal comment puts a comment on the issue, and
  comments are an input to the body gate. On owner-authored issues — the ones SD-2 exempts — that is
  a comment on every tick, forever. On third-party issues the refusal comment silently closes the
  body path for good. This is R11, it is the most serious finding of the run, and it makes the
  once-per-issue mechanism a precondition for SD-3 rather than a refinement of it.

**Open question 1 is still open.** The live probe was refused by the permission system and web access
was unavailable; run 0002 adds no evidence either way and deliberately records no answer from
memory. What it did establish is that a positive answer would be *necessary but not sufficient*: this
repository has measured its own issue listing failing to surface a confirmed `updated_at` bump for
thirty minutes, and an assignment — unlike a comment — has no second listing to fall back on (R12).

---

## Settled decisions (run 0002)

The owner answered three of run 0001's seven open questions. They are recorded here as decisions,
with his reasoning, and are **not re-argued** below — where a risk section previously left one of
them open, it now records the consequence of the decision instead.

### SD-1 — the stampede (R1) is ACCEPTED, not mitigated

**Decision** `[O2]`: no containment is built. Verbatim: *"this is not something we care, because if
someone does it it is their fault and github permission systems prevent random ppl from doing so; so
only a named group can actually do it."*

**His reasoning, recorded because the decision is only as good as its premise:** the exposure is
bounded socially rather than technically. Assigning an issue in this repository is not something the
public can do — GitHub gates it behind repository permissions — so the set of people who could
trigger a bulk start is a small, named, trusted group. If one of them assigns forty issues at once,
that is a deliberate act by someone entitled to perform it, and the cost lands on the person who
chose it.

**What the premise depends on, stated plainly and once.** Two things, and if either stops holding
the decision should be revisited:

1. **It rests on repository write/triage permissions genuinely being restricted.** GitHub only
   accepts an assignee who has push access to the repository, so the assigning *and* the assigned
   account both sit inside the permitted set. Widening collaborators, adding a team with triage
   rights, or making the repository's permissions more generous widens this exposure directly and
   silently — nothing in litefactory would notice or say so.
2. **The exposure is proportional to how many issues one person can assign in one gesture.** GitHub's
   issue list has a bulk-assign control, so "one gesture" is realistically the whole first page of a
   backlog, not one issue. The decision is therefore an acceptance of a *multiplied* cost, not a
   single one.

**Does anything in litefactory incidentally bound the total?** Checked directly this run. **No — and
`max_concurrent_runs` is not the whole story, it is barely any of it.** `[V]`

- `max_concurrent_runs` (default `3`, `internal/config/config.go:215, :238`) sizes a semaphore that
  bounds how many turns execute *at the same instant*. `Daemon.offer` takes a slot with `d.pool.try()`
  (`internal/daemon/tick.go:766`) and defers the session if there is none.
- **A deferred session is not dropped.** `d.markBacklog(...)` is called at
  `internal/daemon/tick.go:764` — *before* `pool.try`, deliberately, "Every session this tick took
  out of the eligible set is marked, not only the ones that get a slot" (`:759-763`) — and
  `Daemon.deferRest` (`:871-877`) marks every candidate still standing when the pool ran dry. The
  flag is `sessions.needs_poll` (`internal/store/migrations/0002_session_backlog.sql:40-45`), and
  `Daemon.backlog` (`internal/daemon/tick.go:350-375`) unions every flagged session back into the
  next tick's discovery.
- So `needs_poll` is a **durable queue**. Forty assigned issues become forty runs, three at a time,
  spread over subsequent ticks — all forty of them, eventually. Nothing is shed.

The amendment ledger says exactly this, in its own words, and it is the cleanest statement of the
point (`.aiteam/AC_AMENDMENTS.md:157-164`) `[V]`:

> **`max_concurrent_runs`** counts runs that are *executing* … It refuses nothing over time. A
> session that finds no free slot keeps its `sessions.needs_poll` flag and is reconsidered from
> scratch, against fresh state, by the next tick … There is no queue and no dropped work: **total
> work done over an hour is unchanged**, and only how much of it is in flight at one instant is
> bounded.

That sentence was written to argue that `max_concurrent_runs` is *not* the run-rate circuit breaker
D-X forbids. It is equally a statement that **litefactory has no total-spend ceiling of any kind**,
and D-X (`.aiteam/AC_AMENDMENTS.md:119-174`) still forbids adding one. Under SD-1 that is accepted.
`max_concurrent_runs` smooths the *rate* of spend and does not bound its *total*.

### SD-2 — the "zero comments" clause (AC-2.16d) is KEPT, but relaxed for the owner's own issues

**Decision** `[O2]`: *"yes, we keep it, but only if the creator of the issue is someone else!"* The
rule becomes:

| Issue authored by | Existing comments | Body trigger |
|---|---|---|
| **someone else** | any | **blocked** — the conversation is the instruction (AC-2.16d as it stands) |
| **the owner** | any | **not blocked** — the body fires anyway |

This is a real change to the rule, and it **reintroduces issue authorship into the gate**. Run 0001
recommended against scoping an authorship-based trigger. Whether that is a contradiction is worked
through in *SD-2 and AC-2.16a: not actually a conflict* below, and the answer is that it is not —
but the resolution has to be written down rather than assumed.

### SD-3 — refusals become visible on the thread, but not naively

**Decision** `[O2]`: *"yes, of course, i want to see this logged here!"*

**Assumption, recorded explicitly because it changes the size of the work** `[O2]` interpreted: "here"
is taken to mean **on the GitHub issue thread**. A debug log line into `$LF_HOME/logs` already exists
for every one of these refusals (`internal/daemon/tick.go:1085-1087, :1104, :1113-1116`) `[V]`, and
that is not something the owner could "see" in the sense the comment implies. So the request is read
as: post something on the issue.

**A naive reading of this is bad, and the brief says so plainly.** litefactory polls **every open
issue in every registered repository** — `Daemon.sight` creates a session row for every open issue in
the listing window, assigned or not (`internal/daemon/tick.go:394-419`, session created at `:403`)
`[V]`. The very first refusal branch is "it is not assigned to the owner"
(`internal/daemon/tick.go:1077`). If a comment were posted on every refusal, litefactory would
comment on **every issue in the repository**, and under R-body′ it would do so **on every tick**,
forever. That is precisely the outcome AC-2.16c exists to prevent, and it is not what the owner is
asking for. The defensible line, and the mechanism that could make it once-only, are worked out in
*SD-3: which refusals speak, and how they stay quiet afterwards* below.

---

## SD-2 and AC-2.16a: not actually a conflict

Run 0001 wrote, under *Recommended scope → Do not scope*: "an authorship-based trigger (contradicts
AC-2.16a, which was kept deliberately)". SD-2 puts authorship back in the gate. These look like they
collide. **They do not**, and the distinction is precise enough to state as a rule.

AC-2.16a, verbatim (`.aiteam/AC_AMENDMENTS.md:78-80`) `[V]`:

> AC-2.16a *(replaces AC-2.16)* — An issue that is not assigned to the owner auto-starts nothing,
> however its body is worded — **including a body the owner wrote himself**.

AC-2.16a is a statement that **authorship is not *sufficient*** to start a run. The ledger's own gloss
is that this is "the half that was load-bearing" (`:107-109`) `[V]`, and the reason is authorisation:
anyone can open an issue, so authorship carries no signal that the owner wants work done.

SD-2 does not touch that. Under SD-2, **assignment to the owner is still required, unchanged, and is
still the sole authorisation.** What authorship does under SD-2 is *relax a secondary block* that
exists for an entirely different reason. AC-2.16d is not an authorisation rule at all — its stated
justification is editorial: "an issue that has already been talked on is one whose conversation is
the instruction, and replaying its opening post underneath that would be a second voice nobody asked
for" (`internal/daemon/tick.go:1038-1041`) `[V]`.

So the two rules answer different questions:

| | Question | Axis | Changed by SD-2? |
|---|---|---|---|
| AC-2.16a | may this run at all? | assignment (authorisation) | **no** |
| AC-2.16d | is the body still the right thing to run? | comments (which voice is live) | **yes, conditionally** |

**The resolution, stated for Plan to carry:** SD-2 is not an authorship-based *trigger*. It is an
authorship-based *exemption from AC-2.16d*. An unassigned issue the owner wrote still starts nothing,
which is AC-2.16a intact and verbatim. Run 0001's "do not scope an authorship-based trigger" stands
as written; SD-2 is not one.

**The justification for the exemption is coherent on AC-2.16d's own terms.** AC-2.16d asks "whose
voice is live here?" On a third-party issue, the answer is genuinely ambiguous — a stranger wrote the
body, other people have commented, and the owner assigning it does not tell you which of those texts
he meant. On an issue the owner wrote *and* then assigned to himself, both signals point at the same
person: he wrote the body and he asked for it to be run. The comments are then other people's
remarks on his own request, not a competing instruction. That is a defensible reading of the same
rule, not a suspension of it.

**AC-2.16d will need an amendment entry**, because its wording ("An issue that already has at least
one comment does not fire its body") becomes conditional. So will AC-2.16b, whose "on first sight"
wording R-body′ falsifies independently.

---

## SD-2's consequences, worked through

### Does `t.issue.Author.ID` against the stored owner id give exactly this rule?

**Yes, exactly, and it needs no new field, no new endpoint and no new store column.** `[V]`

- `github.Issue.Author` is a `User` riding the issue payload already in hand
  (`internal/github/issues.go:60`). Its doc comment already says it is "the attribution … and, on an
  issue whose body is run as its first instruction, the author of that trigger too (AC-2.16b)"
  (`:50-52`), and then says in terms that "It is not the authorisation" (`:54`).
- `d.filter.Identities().OwnerUserID` is already read one line into the gate
  (`internal/daemon/tick.go:1077`), and `t.issue.Author.ID` is already read fifteen lines later, into
  the run's logger (`internal/daemon/tick.go:1093`: `slog.Int64("author_user_id", t.issue.Author.ID)`).
  **Both values are already on this exact code path.** The change is a conjunction, not a fetch.

The refusal branch at `internal/daemon/tick.go:1081-1082` becomes conditional on the author:

```
case len(comments) != 0 && t.issue.Author.ID != d.filter.Identities().OwnerUserID:
```

**Two things Plan must get right, both of which the codebase already legislates:**

1. **Compare the numeric id, never the login** (D-I). `internal/github/doc.go` states this and
   `Issue.AssignedTo` (`internal/github/issues.go:94-104`) is the local precedent — including its
   guard that "a zero id matches nothing", because zero is what GitHub reports for a deleted account
   `[V]`. The author comparison needs the same guard: an issue whose author account was deleted
   reports `Author.ID == 0`, and if the stored owner id were ever also 0 the two would match and the
   block would be lifted for a stranger's issue. `turn.Identities.Validate` refuses a zero owner id
   at startup (`internal/turn/classify.go:119-129`) `[V]`, so the daemon cannot run with one — but
   the guard is cheap and matches `AssignedTo`'s own posture.
2. **This is a second identity comparison against the owner, and it is not the same one.**
   `AssignedTo` asks "is the owner among the assignees"; this asks "is the owner the author". Both
   read the stored owner id, which is fine; what would be a bug is folding them into one helper that
   made the two look interchangeable.

### What R4 (the prompt hazard) does under the relaxed branch — and it is not removed

This is the sharpest consequence of SD-2 and it deserves stating precisely, because the
owner-authored branch is **exactly** where the rule no longer removes the hazard.

R4 as run 0001 stated it: a body-triggered run being handed the body as its instruction while a
conversation it cannot see already exists on the thread. Keeping AC-2.16d closed that off. SD-2
re-opens it for owner-authored issues.

**Is the comment history loaded on the body path at all? Yes — and this is the part run 0001 did not
establish.** `[V]` `turn.prepare` calls `commentHistory(...)` at `internal/turn/prepare.go:147`,
**unconditionally**, ten lines *before* `bodyIsInstruction := req.Trigger.CommentID == 0` is computed
at `:157`. The result lands on `harness.PromptInput.History` (`:183`). There is no branch: the body
path builds its history exactly as the comment path does.

**But what that history contains is the whole question, and the answer splits in two.**
`commentHistory` reads `opts.DB.ListComments(ctx, session.ID)` (`internal/turn/prepare.go:411`) —
litefactory's **local `comments` table**, never the forge. And its own doc comment says what that
costs (`internal/turn/prepare.go:385-389`) `[V]`:

> It is best effort over what the comments table holds. A newly watched issue is bootstrapped by
> recording the highest comment id on it and **discarding the bodies**, so anything said before
> litefactory first saw the issue is not stored and cannot be rendered. The block says so in its own
> Source line rather than implying it is the whole thread.

That is confirmed at the write end: `Daemon.bootstrap` lists every existing comment only to compute
`highest` and plant the watermark (`internal/daemon/tick.go:991-1006`), and records **none** of them
— "its existing comments are skipped" (`:1008`). Comments arriving *after* first sight do get
recorded, because `handleComment` admits and records every comment including third-party and
litefactory's own (`internal/daemon/tick.go:1274-1287`, `d.filter.Admit` at `:1278`; "Everything is
recorded, including litefactory's own comments"). `[V]`

**So, under SD-2's relaxed branch, what the run is actually told:**

| Comments on the thread arrived | In the local table? | In the prompt's history? | Hazard |
|---|---|---|---|
| **after** litefactory first saw the issue | yes | **yes** — rendered as history entries | **none.** The run sees the conversation and the body, correctly framed |
| **before** litefactory first saw the issue | no — bodies discarded at bootstrap | **no** | **live.** The run is handed the body as its instruction and is blind to a conversation that exists on the thread |

The second row is R4, and it is real under SD-2. Mitigating it: the history block "says so in its own
Source line rather than implying it is the whole thread" (`internal/turn/prepare.go:388-389`) `[V]`,
so the model is told the history is partial rather than being misled into thinking it is complete.
That is a genuine mitigation and it is not nothing. It is not a fix.

**The Resume interaction, separately.** `harness.PromptInput.BodyIsInstruction`'s doc comment states
that `Resume` + `BodyIsInstruction` together omit the Context section altogether, and that the
combination "is not reachable today — a body fires at most once per issue, so a body-triggered run is
always the first run of its conversation" (`internal/harness/prompt.go:247-256`) `[V]`. **R-body′
makes it reachable regardless of SD-2**, because it detaches the body from first sight; SD-2 widens
it further by removing the comment block that would otherwise have caught the owner-authored case.
Under `UNIQUE (session_id, trigger_comment_id)` a body still runs at most once per session, so a
body-triggered run is still the first run *at trigger 0* — but a session can now have had a
comment-triggered run first, which is what makes `Resume` true. **That doc comment becomes untrue and
rewriting it is part of the change**, per this codebase's own rule (`.aiteam/APP_CONTEXT.md:322-328`).

### Does SD-2 change R5 — the hole in workflow 4?

**It changes it for two of the four workflows and leaves workflow 4's hole exactly where it was.**
Restated against SD-2 + R-body′:

| # | Workflow | Author | Before run 0002 | After SD-2 + R-body′ |
|---|---|---|---|---|
| 1 | random author → sight → owner assigns → **owner comments** → run | other | works | **works**, unchanged |
| 2 | owner authors → sight → owner assigns → run | **owner** | broken | **works** — and works even if the issue has comments |
| 3 | owner authors → owner assigns → sight → run | **owner** | works | **works**, and now also works when it has comments |
| 4 | random author → sight → owner assigns → run | other | broken | **works — but only while the issue has no comments** |

**The residual hole, stated plainly, is workflow 4 only:** *a stranger opens an issue, somebody
comments on it, the owner then assigns it → nothing runs, and the owner must comment.* SD-2 does not
close that, because SD-2's relaxation is conditioned on the owner being the author and in workflow 4
he is not.

That is the honest position, and it is defensible rather than an oversight: workflow 4 with existing
comments is the one case where AC-2.16d's original argument is at its strongest — a stranger's body,
a live conversation, and no signal about which the owner meant. Adding a comment is a one-line fix
available to the owner in that case, and it is the case the comment path already serves well.

**Note the asymmetry SD-2 creates**, because someone will notice it and think it is a bug: two issues
that look identical on the thread — same body, same comments, same assignee — behave differently
depending on who opened them. That is the intended effect of SD-2, and it should be said out loud in
`internal/daemon/doc.go` and in the amendment entry, or it will read as non-determinism.

### Does SD-2 interact with the once-ever guarantee?

**No. Once-ever is untouched by SD-2, and rests on the same constraint it rests on today.** `[V]`

`runs` carries `UNIQUE (session_id, trigger_comment_id)` (`internal/store/migrations/0001_init.sql:103`,
whose own comment reads "One comment can cause at most one run, ever"). The body's trigger is
`turn.Trigger{CommentID: 0, ...}` (`internal/daemon/tick.go:1123`), so the pair `(session, 0)` is
unique per session however many times the gate is evaluated. `turn.Execute` looks the run up first
with `GetRunByTrigger` (`internal/turn/execute.go:400`) and returns `DispositionAlreadyRan` /
`DispositionAlreadyReported` (`:430-432`) rather than erroring.

SD-2 changes *which issues reach* the gate. It does not change what happens when one reaches it
twice. What does change the register of the guarantee is R-body′ (see R3) — from control flow to a
database constraint — and that is true with or without SD-2.

---

## SD-3: which refusals speak, and how they stay quiet afterwards

### The refusal branches that exist today

Every one re-pinned this run. `[V]` `Daemon.runIssueBody` is `internal/daemon/tick.go:1074-1125`.

| # | Branch | Line | Meaning |
|---|---|---|---|
| 1 | `!t.issue.AssignedTo(owner)` | `:1077` | not assigned to the owner |
| 2 | `strings.TrimSpace(t.issue.Body) == ""` | `:1079` | body empty or whitespace |
| 3 | `len(comments) != 0` | `:1081` | already has comments (AC-2.16d; SD-2 makes this conditional) |
| 4 | `command.Kind == turn.KindNone` | `:1097-1106` | body parses to nothing to act on — in practice, a body whose only content is the `lf: ask` tag (AC-A1.9) |
| 5 | `!command.Spawns()` | `:1107-1118` | body names a command that spawns nothing: `/plan`, `/frobnicate`, a bare `/spike` (AC-2.16c) |
| 6 | `decision.refused()` → `Daemon.refuseTools` with `trigger.CommentID == 0` | `internal/daemon/tools.go:120-188` → `internal/daemon/tick.go:1487-1511` | the `lf:tools=` label does not resolve |

Branches 1–3 log at `:1085-1087` and return. Branch 4 logs at `:1104`. Branch 5 logs at `:1113-1116`.
Branch 6 logs at `:1506-1509` and states in the log line itself that "no comment was posted, because
there is no comment to answer".

### The proposed line: "not for me" versus "meant for me, and I still declined"

**Stay silent — "this issue is not for me":**

- **Branch 1, not assigned to the owner.** This is the one that must never speak, and it is the whole
  reason a naive reading of SD-3 fails. It fires for every open issue in every registered repository
  that is not assigned to the owner — which is most of them, most of the time. Under R-body′ it
  fires on *every tick*. Silence here is not a compromise; it is the feature.
- **Branch 3, existing comments (in its surviving third-party form under SD-2).** The thread already
  has a live conversation. Adding "I noticed this and decided not to act" to someone else's
  discussion is the second-voice problem AC-2.16d exists to avoid, restated. Also self-limiting in a
  bad way: see the feedback loop below.

**Speak up — "this was clearly meant for me and I still declined":** all of these require the issue
to be **assigned to the owner**, which is the signal that someone deliberately pointed litefactory at
it. That conjunction is what makes the volume tractable.

- **Branch 2, empty body on an assigned issue.** The strongest case, and it is the case the owner
  actually hit: issue #10 itself. Someone assigned it, expecting work, and got nothing and no
  explanation. `[O]`
- **Branch 5, a body naming a command that spawns nothing.** Assigned, non-empty, and the first line
  was `/plan` or a typo. The comment path already answers exactly this with
  `report.ReplyUnsupportedCommand` / `ReplyUnknownCommand` / `ReplyEmptySpike`
  (`internal/report/reply.go:27-38`) `[V]`; the body path is the one place that stays quiet.
- **Branch 6, an unresolvable `lf:tools=` label.** Assigned, and someone applied a permission label
  that does not resolve. `refuseTools`'s own doc comment already names itself "the only branch to
  change if that trade is revisited" (`.aiteam/APP_CONTEXT.md:468-472`, matching
  `internal/daemon/tick.go:1505-1510`) `[V]`. Under R-body′ this becomes reachable on every tick
  rather than once — see R2 — which makes it the branch most likely to produce "my label did nothing,
  forever, silently".
- **Branch 4, a body that is only the `lf: ask` tag.** Marginal. Assigned and non-empty, so it clears
  the volume bar, but the refusal is obscure enough that a reply may confuse more than it helps.
  Recommend deferring this one rather than deciding it here.

### At most once per issue — and nothing in the store can carry that today

**This is the hard part of SD-3 and it is not a small change.** Checked concretely.

**What exists:**

- `sessions` (`internal/store/migrations/0001_init.sql:50-64`, plus `needs_poll` from
  `0002_session_backlog.sql:40`) — columns are id, repo_id, issue_number, task_ref, feature_slug,
  branch, worktree_path, state, lock_owner, lock_acquired_at, created_at, updated_at, needs_poll.
  **No column for "the thread has been told about a refusal".** `[V]`
- `runs` — a refusal spawns no run, so there is no row to hang it on. Inserting a sentinel run would
  need a segment_id, an ordinal, a log_path and a status, all of which would be lies. `[V]`
- `watermarks` — `(session_id, last_comment_id)` (`:131-135`). Advancing it for a refusal would move
  the session past unread comments, which is the permanent-silence failure `AdvanceWatermark`'s
  never-rewind posture exists to prevent. Not usable. `[V]`
- `comments` — records forge comments, `UNIQUE (session_id, forge_comment_id)` (`:113-126`). It
  records litefactory's *own* comments once they come back round on a listing, which is the basis of
  the no-migration option below.

**There are only two migrations in the tree** (`0001_init.sql`, `0002_session_backlog.sql`) `[V]`, so
new state means `0003_*.sql`. The ledger records that a `runs.owner` column was rejected once on
"the migration budget went to `sessions.needs_poll`" grounds (`.aiteam/AC_AMENDMENTS.md:1286-1312`)
`[A]` — a precedent Plan should weigh, not a prohibition.

**Two concrete mechanisms:**

- **(i) A new `sessions` column**, e.g. `body_refusal_posted INTEGER NOT NULL DEFAULT 0`, or a
  nullable text column naming which refusal was posted. Explicit, queryable, survives anything, and
  is the shape the codebase already reaches for (`needs_poll` is exactly this). Costs a migration.
  **Recommended if SD-3 is scoped at all.** Note it must be set *after* a successful post and in the
  same "post first, then commit" order every reporting path uses (`.aiteam/APP_CONTEXT.md:368-370`),
  or a transient 502 becomes permanent silence.
- **(ii) No migration: read the thread back.** litefactory's own comments carry a marker —
  `turn.HasMarker(c.Body)` is one of the three self-filter conditions
  (`internal/turn/classify.go:263-273`, `SelfReasonMarker`) `[V]` — and the body path already has the
  full comment list in hand on the bootstrap path. So "have I already said this?" could be answered
  by scanning existing comments for litefactory's marker. **Assessed as worse:** it makes the
  daemon's control flow depend on parsing its own rendered output, it cannot distinguish a refusal
  comment from a run report, and on the drain path the comments in hand are only those *above* the
  watermark, so it would need a listing it does not otherwise make. Recorded so Plan does not have to
  rediscover it.

### The feedback loop SD-3 and SD-2 create together — and it is the worst finding of this run

**Posting a refusal comment puts a comment on the issue, and comments are an input to the body
gate.** Tracing it through both branches of SD-2:

| Issue authored by | Refusal comment posted | Next tick's gate | Outcome |
|---|---|---|---|
| **someone else** | yes | branch 3 now fires — the issue "already has comments" | The refusal is **once-ever by accident**, and the issue is now **permanently dead to the body path** even if the body is later filled in |
| **the owner** (SD-2 relaxed) | yes | branch 3 is skipped — comments do not block | The gate refuses again for the same reason, and **posts again. Every tick. Forever.** |

The second row is a comment-spam loop on precisely the issues the owner most cares about, produced by
the interaction of two decisions he made in the same comment, neither of which is wrong on its own.
**It is not hypothetical and it is not avoidable by care — it follows directly from the two rules.**
The once-only mechanism (i) above is therefore **not an optimisation or a nicety; it is what makes
SD-3 safe to build at all**, and Plan must treat it as part of the same story rather than a
follow-up.

The first row is also worth stating: on third-party issues the refusal comment would *silently close
the body path forever*, which is a side effect nobody asked for and which a reader would not predict
from "log it here".

### The cost: a forge write on a path that currently makes none

- **The endpoint exists.** `Client.PostComment` is `internal/github/comments.go:230`, pinned in
  `TestClientMethodSetIsPinned` (`internal/github/surface_test.go:64-109`) `[V]`. **No new forge
  endpoint is needed for SD-3**, and the pinned method set does not have to change.
- **The redaction boundary is already the right shape.** `PostComment` takes a `redact.Body` and
  never a `string`, enforced at compile time and asserted by
  `TestPostCommentTakesARedactedBody` (`internal/github/surface_test.go:20-45`) `[V]`. `redact.Body`
  has no exported constructor — only `redact.Redactor.Apply` builds one
  (`.aiteam/APP_CONTEXT.md:176-181`) — so a refusal body *cannot* reach the forge without going
  through redaction. Nothing new is required here; the type system already forces it.
- **`internal/report` needs a new reply shape, and that is two edits, not one.** The rule is stated in
  `.aiteam/APP_CONTEXT.md:70-77` and holds against source: `report` cannot import `turn` (the arrow
  points the other way), so `report.ReplyKind` is its own enum
  (`internal/report/reply.go:19-68`, six kinds today) and the mapping is a switch at the call site,
  `replyKind()` in `internal/turn/report.go:612-625` `[V]`. **A new reply shape needs a `ReplyKind`
  *and* a case in that switch.**
- **But the bigger obstacle is that the whole reply path is welded to a triggering comment id**, and
  this is the finding that sizes SD-3. `[V]` Three separate places in `internal/turn/report.go`:

  1. `ReplyRequest.CommentID` is documented "Required" (`:198-199`), and `validate` **hard-refuses**
     it: `case req.CommentID <= 0: return invalidRequest("a comment id is required to reply to a
     comment")` (`:254-255`).
  2. `ReportReply`'s idempotency guard is `r.consumed(ctx, req.SessionID, req.CommentID)` (`:413-420`)
     — "comment has already been consumed; posting no reply". With no comment there is nothing to ask
     that question about, so **the existing once-only mechanism does not transfer**.
  3. `ReportReply` finishes by `AdvanceWatermark(..., req.SessionID, req.CommentID)` (`:440`). At
     comment id 0 that is a `MAX(0, 0)` no-op (AC-2.23), so it would carry no idempotency either.

  So SD-3 is not "call `ReportReply` with a zero comment id" — that path refuses by construction, and
  the two things that make it idempotent both degenerate at zero. It needs either a relaxation of
  `ReplyRequest` plus a substitute idempotency source (mechanism (i)), or a parallel posting path in
  `turn` that does not key on a comment. **Plan should size SD-3 as its own story, not as a flag on
  the trigger story.**

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
assignees change?** `[W]` — this is *not* established anywhere in this tree, and **run 0002 failed to
establish it either.** See *Open question 1: what was tried* below. It remains the single
highest-value cheap probe available, because the answer decides the size of the change:

- **If `updated_at` bumps on assignment**: discovery re-lists the issue on a subsequent tick. The
  change is inside the worker, needs no new endpoint and no new listing, and the per-tick API cost is
  unchanged. **But see the lag finding below — "on the next tick" is not what this repository has
  measured, and the caveat is load-bearing.**
- **If it does not**: the issue is invisible to both existing listings after the cursor has passed
  it, and the options get materially worse — either keep every unassigned open issue permanently in
  the `needs_poll` backlog (see *Risks*, R6), or add a forge endpoint (issue events / timeline),
  which means editing `daemon.Forge` (`internal/daemon/daemon.go:78-84`) **and**
  `TestClientMethodSetIsPinned` (`internal/github/surface_test.go:64-109`) — a guard that exists
  precisely so that growing the forge surface is a decision someone has to write down. `[V]`

The comment listing cannot help either way: there is no comment.

### The lag finding — new in run 0002, and it weakens the good branch

Even a confirmed `updated_at` bump does **not** guarantee the issue is re-listed promptly. This
repository has already measured that GitHub's `since`-filtered issue listing lags badly, and wrote it
down (`internal/github/comments.go:116-126`) `[V]`, verbatim:

> [Client.ListIssues] finds the issues whose `updated_at` moved. Writing a comment does move it —
> measured on marioatcloudvane/litefactory#4, whose comment landed at 19:14:47Z and whose issue read
> 19:14:48Z a moment later — so in principle one listing would do.
>
> In practice the two are indexed separately and lag separately. **On issue #3 of the same
> repository, two owner comments were invisible to sixty consecutive issue listings across thirty
> minutes, and only surfaced seven hours later when the issue was closed by hand.**

That is the *same listing* this feature depends on, in the *same repository*, failing for thirty
minutes on an event whose `updated_at` bump was directly measured.

**Why this matters more for assignment than it does for comments.** For comments, the codebase has a
second, independent discovery path — `ListCommentedIssues`, which "is indexed on the comments
themselves rather than on a timestamp copied onto the issue, so it is the shorter path to a comment
and unlikely to lag in step with the other" (`internal/github/comments.go:124-130`) `[V]`. That is
exactly why discovery asks twice.

**An assignment creates no comment.** So the assignment path has **no second listing** and no
independent route. If `updated_at` does bump but the issue listing lags — which is the measured
behaviour, not a hypothesis — a late assignment could go unnoticed for an arbitrarily long time, with
no fallback and nothing on the thread to say so. From the owner's seat that is indistinguishable from
the feature not working.

**The consequence for the mechanism choice.** Run 0001 rated option (c) — keeping `needs_poll` set —
"the worst of the three". **Run 0002 revises that assessment upward.** Option (c) is the only one of
the three that does not depend on a forge listing at all: a flagged session is unioned back into
discovery from the local database (`Daemon.backlog`, `internal/daemon/tick.go:350-375`), costing no
API quota (`internal/store/migrations/0002_session_backlog.sql:40-45`) `[V]`. Its cost — inverting
the partial index's stated design point that "the steady state — every session idle — carries no
index entries at all" (`:42-45`) — is real and unchanged. But it is now a trade against a *measured*
failure of the alternative, not against a hypothetical one. Plan should weigh (a) and (c) against
each other properly rather than treating (c) as ruled out.

### Open question 1: what was tried in run 0002, and what stopped it

**Attempted, in the order the task specified.** `[V]` for what was attempted; the question itself
stays `[W]`.

1. **Read-only probe against the live API — blocked.** A sub-agent with shell access attempted
   `gh api repos/marioatcloudvane/litefactory/issues/10`, the issue timeline and events endpoints,
   and a repository-wide issue listing to find an issue whose most recent timeline event was an
   `assigned`/`unassigned` one. **Every `gh` invocation was refused by the permission system**
   ("Permission to use Bash has been denied"). The denial was confirmed to be specific to `gh` rather
   than to shell access generally — a plain `echo` succeeded between two denied `gh` calls. No
   timeline data was retrieved; no issue numbers and no timestamps were obtained. Routing around the
   denial with `curl` against `api.github.com` was deliberately **not** attempted: it would reach the
   same endpoint the denial covers.
2. **GitHub's own documentation — blocked.** `WebSearch` and `WebFetch` were unavailable in this
   session, and shell network egress was denied too. No official documentation, no changelog and no
   community sources were reached. **Nothing is cited because nothing was obtained.**

**No from-memory answer is recorded, deliberately.** A recollection would be indistinguishable in
this document from a measured result, and this codebase's own convention is that external behaviour
is *probed and written up* before it is relied on (`.aiteam/APP_CONTEXT.md:403-424`) `[V]`. Run 0001
noted that a background agent had asserted the bump does happen, with no citation; **that assertion
remains unverified and run 0002 adds no evidence either way.**

**The cheapest unblock, for whoever runs this next:** permission for read-only `gh api` GETs,
specifically `gh api repos/{owner}/{repo}/issues/{n}` and
`gh api "repos/{owner}/{repo}/issues/{n}/timeline?per_page=100"`. Purely observational, no mutation
required. The decisive comparison is an issue whose most recent timeline event is `assigned` or
`unassigned`: if the issue's `updated_at` matches that event's `created_at`, assignment bumps it; if
`updated_at` matches an *earlier* event while a later `assigned` event exists, it does not.

**Whatever that probe returns, the lag finding above stands and must be carried into the design.**
The probe answers "does the timestamp move", not "does discovery notice in time". Those are two
questions and this repository has already measured that the second one can answer no.

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

### Open question 5, explained: "assigned to the owner" versus "assigned to anybody"

The owner asked what this means. It is **left open deliberately** — this section explains it and
recommends, it does not decide.

**What "the owner" actually is in litefactory.** Not a name, not a handle, not a role someone can
claim. It is a **numeric GitHub user id**, recorded once and compared exactly. `[V]`

- It is stored in the `identities` table, primary key `(forge, role)`, with `role = 'owner'` and a
  numeric `user_id` column described in the schema as "the numeric forge id; the identity that
  comparisons use (D-I)" (`internal/store/migrations/0001_init.sql:17-25`).
- `store.RoleOwner` is documented in one line: "RoleOwner is Mario: the author whose comments become
  instructions" (`internal/store/identities.go:24-25`).
- It is derived by the `gh` CLI at `litefactory init`, and `gh` is used for nothing else —
  `SourceGH` is "the identity came from the `gh` CLI. Only `init` uses it, and only for the owner"
  (`internal/store/identities.go:36-38`). `init`'s child environment strips `GITHUB_TOKEN`/`GH_TOKEN`
  precisely so it cannot record the bot as the owner (`.aiteam/APP_CONTEXT.md:191-194`).
- It reaches the daemon as `turn.Identities.OwnerUserID` (`internal/turn/classify.go:101-104`).
- **It is required to differ from the bot account.** `Identities.Validate`
  (`internal/turn/classify.go:119-129`) refuses to construct a filter when either id is zero or when
  `OwnerUserID == BotUserID`, "because an account that is both owner and bot reads its own reports as
  instructions — the loop this whole task exists to prevent (AC-0.6)".
- **The comparison is on the id and never on a login**, everywhere, because "a login is a label
  GitHub lets people change, and the id is the account" (`internal/github/issues.go:89-93`, D-I).

So today's gate — `t.issue.AssignedTo(d.filter.Identities().OwnerUserID)` at
`internal/daemon/tick.go:1077` — means: **"is the one specific account that ran `litefactory init` on
this machine among this issue's assignees?"** One id, one comparison, and nobody else's assignment
counts for anything.

**What changes if the rule became "assigned to anybody".** The gate would drop the id comparison
entirely and ask only whether `Issue.Assignees` is non-empty. The consequences:

- **Every assignment in the repository, by anyone, becomes a start signal.** A maintainer assigning a
  bug to a colleague — an ordinary triage action with no connection to litefactory — would start a
  run on that issue's body. Triage and instruction stop being distinguishable.
- **It is a much wider door, and it widens the *authorisation* rather than the timing.** Everything
  else in this brief widens *when* the door is open; this widens *who may open it*. That is a
  different kind of change and a bigger one.
- **It removes the reason the original ban could be lifted.** The ledger's argument for reversing
  AC-2.16 is precise (`.aiteam/AC_AMENDMENTS.md:99-105`) `[V]`: "The body runs only when the issue is
  **assigned to the owner** … Assigning an issue to yourself is the act of instruction; **a stranger
  cannot do it on your behalf.**" Under "assigned to anybody", a stranger with triage rights *can* do
  it on his behalf, and that sentence stops being true. The whole authorisation story for issue
  bodies would need re-making.
- **It interacts directly with SD-1, and this is the important part.** SD-1's containment argument is
  entirely about *who is allowed to assign* — "github permission systems prevent random ppl from
  doing so; so only a named group can actually do it" `[O2]`. That argument survives "assigned to
  anybody" in the narrow sense that the same permission gate applies. But it changes character: today
  the group who can *trigger* litefactory is one person, and the permission system is a second line
  of defence behind that. Under "assigned to anybody" the permission system becomes the **only**
  line of defence, and every collaborator becomes able to spend money on the owner's account by
  performing routine triage. **The two questions are not independent, and answering 5 with "anybody"
  materially weakens the premise he accepted in 2.**

**The middle option, and it is coherent.** A **named allowlist of user ids** — "assigned to any of
these N accounts" rather than one, or none-and-therefore-all.

- It is a small change to the comparison: `AssignedTo` already loops over assignees
  (`internal/github/issues.go:94-104`), so this becomes a loop over a set on both sides.
- **It costs configuration, and configuration in this codebase is not free.** A new `config.yaml` key
  must be added to `Defaults()`, to the shipped template, and it must survive
  `TestDefaultFileMatchesDefaults` (`.aiteam/APP_CONTEXT.md:236-243`) `[V]`. And it would be a list
  of *numeric ids*, not handles, per D-I — which is genuinely unpleasant to author by hand, since
  nothing in the CLI resolves a handle to an id outside `init`.
- **It also fragments the identity model.** Today litefactory has exactly two identities and a test
  that refuses to let them be the same account. A third concept — "accounts whose assignment counts,
  but whose comments do not" — is a new axis, and it would immediately raise the question of why
  their *comments* still do not trigger runs (`Filter.Classify` admits only the owner id,
  `internal/turn/classify.go:249`) `[V]`. That asymmetry would need an answer.

**Recommendation: keep it as it is — assignment to the owner specifically.** Four reasons:

1. **It is what the issue text asks for.** "as long as the owner is the litefactory account (e.g.
   marioatcloudvane)" names one account. `[O]`
2. **All four of the owner's own workflows are satisfied by it**, unchanged — every one of them has
   him doing the assigning.
3. **It keeps the authorisation story intact**, so no part of the AC-2.16 reversal has to be
   re-argued.
4. **It keeps SD-1's premise as strong as it can be.** He has accepted an unbounded cost on the
   grounds that only a trusted few can trigger it; narrowing "a trusted few" to "one person" is what
   makes that acceptance cheap.

**Take the allowlist only if a concrete second person actually needs to trigger runs.** It is the
right shape if that need exists and is over-engineering if it does not. "Assigned to anybody" is not
recommended in any case — it is the only one of the three that changes what litefactory's
authorisation *means*.

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
  Re-verified in run 0002: a case-insensitive search for `assign`, `first sight` and `issue body`
  returns nothing in that file — the four apparent hits are the substring "body" inside the word
  "nobody". `[V]`

**Found in run 0002:**

- **Run 0001's own citation of `refuseTools` has rotted by fourteen lines.** It cited
  `internal/daemon/tick.go:1473-1511`; the function is now `:1487-1533`, and the body-path branch
  that posts nothing is `:1505-1511`. `[V]` Corrected throughout this revision.
- **"Seven endpoints" is stale across the tree, and this brief inherited it.**
  `.aiteam/APP_CONTEXT.md:212` and `:691` both say `internal/github` has "exactly seven endpoints".
  `TestClientMethodSetIsPinned` (`internal/github/surface_test.go:64-109`) now pins **twelve
  methods** — `AddCommentReaction, AddSubIssue, AuthenticatedUser, CreateIssue, GetIssue,
  ListCommentedIssues, ListCommentsAfter, ListIssues, ListSubIssues, PostComment, CreateSubIssues,
  BaseURL` — of which ten call an endpoint. `[V]` The phrase "an eighth forge endpoint" survives in
  at least four places including `internal/daemon/tick.go:1072` and `internal/github/issues.go:63`.
  **This does not change any conclusion in this brief** — adding an endpoint still means editing the
  pinned set, which is the guard that matters, and the AC-2.24 "no extra request" argument is
  unaffected. But Plan should not repeat "an eighth endpoint" as though it were still accurate, and
  should not be surprised to find the pinned list longer than the archive describes. Per the archive
  rule (`.aiteam/APP_CONTEXT.md:126-134`), the correction belongs in the ledger and not in an edit to
  the archived text.

---

## The four workflows, restated as trigger rules

**As they are today.** `[V]`

| Rule | Condition | Result |
|---|---|---|
| R-comment | open ∧ comment above watermark ∧ author id = owner id ∧ command spawns | run, trigger = comment id |
| R-body | open ∧ **watermark row does not yet exist** ∧ assigned to owner ∧ body non-empty ∧ zero comments ∧ command spawns | run, trigger = 0 |

**As they would need to be, after run 0002's decisions.**

| Rule | Condition | Result |
|---|---|---|
| R-comment | unchanged | unchanged |
| R-body′ | open ∧ assigned to owner ∧ body non-empty ∧ **(zero comments ∨ issue authored by the owner)** ∧ command spawns ∧ **no run yet exists at trigger 0 for this session** | run, trigger = 0 |

Two deltas now, and they are independent of each other:

1. **The timing delta (run 0001).** Replace "first sight" (a moment) with "the body has not run yet"
   (a durable fact). One clause in the *rule*, and a genuinely larger change in the *argument*: it
   moves "exactly once" from being a property of control flow to being a property of a database
   constraint. See R3.
2. **The comment-block delta (SD-2, run 0002).** The zero-comments clause gains a disjunct. One
   comparison, both operands already on the code path (see *SD-2's consequences*). Cheap in code,
   and it is what re-opens R4 for owner-authored issues.

Plan can ship (1) without (2) — they are separable stories and (1) alone closes workflows 2 and 4 for
issues with no comments. It should not ship (2) without (1), because (2) alone changes nothing: the
gate would still only be asked at first sight.

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
| 2 and 4 (late assignment) | one rule change in `internal/daemon`, plus its argument | see below. **No stampede control — SD-1** |

Run 0002 adds two further pieces of work, and they are three separate stories rather than one:

| Story | Depends on | Size |
|---|---|---|
| **A — the timing change (R-body′)** | open question 1 for the mechanism choice | the original story; contained in `internal/daemon` if (a), plus a migration if (b) |
| **B — the SD-2 author exemption** | A (pointless without it) | **small.** One conjunct in the gate; both operands already on the code path (`internal/daemon/tick.go:1077, :1093`). Its cost is in the argument and the doc comments, not the code |
| **C — SD-3, visible refusals** | **R11: needs its own once-only state before it is safe** | **the largest of the three, and larger than it looks.** New `ReplyKind` + a case in `replyKind()`, a reply path that does not key on a comment id, and new durable state (migration `0003_*.sql`). See *SD-3 → the cost* |

Story C should not be shipped in the same change as B without its once-only mechanism. That is R11
and it is a spam loop, not a rough edge.

For story A, three mechanisms are available. **This brief does not choose between them — that is
Plan's job — but the trade-offs are asymmetric enough to be worth stating now, and run 0002 revised
the ranking.**

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
- **(c) Keep `needs_poll` set** for sessions whose body has not fired. Run 0001 assessed this as "the
  worst of the three"; **run 0002 revises that to "the most robust and the most expensive", and Plan
  should weigh it against (a) properly rather than treating it as ruled out.** The cost is unchanged
  and real: `needs_poll` is the dispatch backlog, and this would put *every open unassigned issue in
  every registered repository* permanently into it, inverting the purpose of the partial index
  (`internal/store/migrations/0002_session_backlog.sql:42-45`), whose stated design point is that
  "the steady state — every session idle — carries no index entries at all". What changed is the
  comparison: (a) and (b) both depend on the forge issue listing re-surfacing the issue after a late
  assignment, and R12 records that this repository has *measured* that listing failing to do so for
  thirty minutes on an event whose timestamp bump was itself measured — with no second listing to
  cover assignment the way one covers comments. (c) is the only option that does not depend on the
  listing at all. A narrowed variant is worth Plan's attention: flag only sessions that are *plausible
  candidates* — open, non-empty body, not yet run at trigger 0 — rather than every open issue, which
  keeps the index small in the common case where most issues are already dealt with.

---

## Constraints and risks

**R1 — Stampede. ACCEPTED by the owner (SD-1). Recorded, not mitigated.** `[A]` re-checked `[V]`.
The strongest argument in the issue-#6 research brief was that `bootstrap` is the *first sight* path,
not the *new issue* path — so a mechanism that reacts to first sight reacts to every issue in a
repository the first time that repository is polled. Today the one-shot nature of the gate contains
that: once a session is bootstrapped, no amount of later assigning can start anything. **R-body′
removes exactly that containment.** Assigning forty existing open issues to yourself in one
gesture — a plausible piece of backlog grooming — becomes forty runs. Not forty *attempts*: forty
runs, dispatched three at a time (`config.DefaultMaxConcurrentRuns = 3`,
`internal/config/config.go:215, :238`) and queued durably in `sessions.needs_poll` for the ticks
after that, all of them eventually executed. D-X still forbids a run-rate circuit breaker
(`.aiteam/AC_AMENDMENTS.md:119-174`), so there is no existing mechanism to lean on and none may be
added.

**The owner has accepted this, on the grounds that only a small named group can assign issues in this
repository.** See SD-1 for his reasoning, for the two premises it rests on, and for the verification
that nothing in litefactory incidentally bounds the total. **Plan does not need to decide what
contains this and should not spend effort on it** — the options run 0001 listed (bounding by issue
age, by assignment recency, or an explicit opt-in) are recorded as *not wanted*. What Plan **should**
do is make sure the accepted exposure is visible in the code: a doc comment on the gate saying that
the number of runs a single assignment gesture can start is deliberately unbounded, and why.

**R2 — the `lf:tools=` silent refusal becomes far more reachable. SD-3 is the answer to it.** `[V]`
When the trigger comment id is 0, `Daemon.refuseTools` refuses the run, logs it and **posts
nothing**, because there is no comment to answer (`internal/daemon/tick.go:1487-1533`, the body
branch at `:1505-1511`). Today the branch is reachable only on a first sight of an assigned issue.
Under R-body′ it is reachable on any tick, for any assigned issue carrying a malformed or unknown
`lf:tools=` label — and the owner's experience would be a labelled issue that does nothing, forever,
silently.

SD-3 makes this branch speak, which is the right answer. But note the ordering trap: **it is also the
branch where the SD-2/SD-3 feedback loop is most likely to fire**, because unlike an empty body a
mis-typed label is a state the owner may leave in place for a while. Without the once-only mechanism
it would post on every tick until the label is fixed. See *the feedback loop* under SD-3.

**R3 — "exactly once" changes register.** `[V]` Today it is a control-flow property, argued at
`internal/daemon/tick.go:1043-1051`: the watermark is planted before the body runs, so the next tick
takes `drain` and the function is unreachable. Under R-body′ it becomes a database property resting on
`UNIQUE (session_id, trigger_comment_id)`. The constraint is genuine and directly tested
(`internal/store/store_test.go:908-946`), and `turn.Execute` already degrades to a clean
`DispositionAlreadyRan` rather than an error (`internal/turn/execute.go:400-434`). So the property
survives — but the *stated reason* does not, in four files, and this codebase treats a comment its
change falsified as part of the change.

**R4 — `Resume` + `BodyIsInstruction` becomes reachable. SD-2 removes the thing that was going to
stop it.** `[V]` `internal/harness/prompt.go:247-256` states that the combination "is not reachable
today — a body fires at most once per issue, so a body-triggered run is always the first run of its
conversation", and that the resume fallback rebuilds the same input with `Resume` flipped. When both
are set the Context section is omitted altogether and the prompt is "the header and the instruction".

Run 0001's assessment was that **retaining AC-2.16d closes this**, because a session with a prior run
necessarily has a report comment on the thread, so the body gate refuses it — and that this was a
concrete reason to keep the clause. **SD-2 removes that protection for owner-authored issues**, which
is precisely the class of issue the owner works on most. The hazard is therefore live under SD-2 and
is not closed by anything else.

**What the run would actually be told** is worked out in full under *SD-2's consequences → What R4
does under the relaxed branch*, and the short version is that it splits: comments recorded *after*
first sight do reach the prompt as history (`internal/turn/prepare.go:147, :183, :411`), and comments
that existed *before* first sight do not, because bootstrap discards their bodies
(`internal/turn/prepare.go:385-389`; `internal/daemon/tick.go:991-1008`). The second case is the real
hazard. The mitigation that already exists is that the history block declares its own partiality in a
Source line rather than implying it is the whole thread (`internal/turn/prepare.go:388-389`).

**Plan must not carry `internal/harness/prompt.go:251-255` forward unchanged** — its central claim
becomes false, and under this codebase's rules updating it is part of the change
(`.aiteam/APP_CONTEXT.md:322-328`).

**R5 — AC-2.16d becomes time-dependent *and* author-dependent.** `[V]` The doc comment justifies it
as "an issue that has already been talked on is one whose conversation is the instruction"
(`internal/daemon/tick.go:1038-1041`). Under R-body′ the same issue answers differently at different
*times*; under SD-2 two otherwise-identical issues answer differently according to *who opened them*.
Both are intended, and both need saying out loud in the package spec or they read as non-determinism.

**The surviving hole, restated after SD-2:** *a stranger opens an issue, somebody comments on it, the
owner then assigns it → nothing runs, and the owner must comment.* Workflow 4 only. SD-2 closes the
equivalent hole in workflows 2 and 3 but cannot close this one, because its relaxation keys on the
owner being the author. See the workflow table under *Does SD-2 change R5*.

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

**R11 — the SD-2 × SD-3 comment loop. New in run 0002, and the most serious finding of this run.**
`[V]` A refusal comment is a comment, and comments are an input to the body gate. On an
owner-authored issue SD-2 says comments do not block, so the gate refuses again on the next tick and
posts again — **every tick, forever**. On a third-party issue the refusal comment silently and
permanently closes the body path for that issue. Neither decision is wrong alone; the loop is
produced by their conjunction. **The once-per-issue mechanism is not a refinement of SD-3, it is a
precondition for it**, and the existing reply machinery cannot supply it (`ReportReply` keys its
idempotency on a comment id that is 0 here — `internal/turn/report.go:254-255, :413-420, :440`). Full
working under *SD-3 → the feedback loop*.

**R12 — no discovery path is guaranteed to notice a late assignment. New in run 0002.** `[V]` Open
question 1 asks whether `updated_at` moves on assignment; even a yes does not settle whether
discovery *notices*. This repository has measured its own issue listing failing to surface a
confirmed `updated_at` bump for thirty minutes across sixty consecutive listings
(`internal/github/comments.go:121-126`). For comments there is a second, independently indexed
listing that covers that gap by design (`:124-130`). **An assignment produces no comment, so it has
no second path.** The consequence for Plan is that mechanism (c) — keeping `needs_poll` set — is no
longer clearly the worst of the three, because it is the only one that does not depend on a forge
listing at all. Run 0001's ranking is revised; see *The lag finding*.

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
- A run-rate circuit breaker. D-X stands (`.aiteam/AC_AMENDMENTS.md:119-174`). **Run 0002: R1 no
  longer needs *a* bound either — SD-1 accepts the exposure.**
- Adding an "issue authored by the owner" **trigger**. See *Identity*, point 2. **Run 0002
  qualification:** SD-2 adds an authorship *exemption from AC-2.16d*, which is a different thing and
  is in scope. AC-2.16a — authorship is not sufficient to start anything — is untouched. See *SD-2
  and AC-2.16a: not actually a conflict*.
- Routing a body through `Filter.Admit`. It refuses a zero comment id by construction
  (`internal/turn/classify.go:313-315`) and `Filter.Classify`'s doc states there is deliberately no
  bare-body overload (`:236-244`). `[V]`
- A new forge endpoint — **unless** the `updated_at` probe forces it, in which case it stops being
  out of scope and becomes the main cost of the feature.

---

## Open questions

**Closed by run 0002:** OQ2 → SD-1 (accepted). OQ3 → SD-2 (kept, relaxed for owner-authored issues).
OQ4 → SD-3 (yes, on the thread, with a defensible line and a mandatory once-only mechanism).

**Still open:**

1. **Does GitHub bump an issue's `updated_at` when its assignees change?** `[W]` **Still open. Run
   0002 tried to settle it and could not** — the live `gh` probe was refused by the permission
   system and web access was unavailable in the session. See *Open question 1: what was tried* for
   exactly what was attempted, what blocked it, and the two read-only commands that would settle it.
   Not re-asked as a decision for the owner: it is a fact about GitHub, not a preference, and it
   needs a probe rather than an answer.

   **Run 0002 narrowed its consequence, though.** R12 records that even a positive answer does not
   guarantee timely discovery, because this repository has measured its own issue listing lagging by
   thirty minutes on a confirmed bump, and assignment has no second listing to fall back on. So the
   question now sizes the *mechanism choice* rather than deciding *whether the feature is possible*:
   (a)/(b) if the answer is yes and the lag is tolerable, (c) if it is no — or if the lag is not.

2. **Assignment to the owner specifically, or to anybody?** `[O]` asked for an explanation, which is
   given under *Open question 5, explained*. **This is the one genuinely open decision left for the
   owner.** Recommendation there is to keep it as it is — assigned to the stored owner id — with a
   named allowlist of user ids as the middle option if a second person actually needs to trigger
   runs, and "assigned to anybody" not recommended in any case, because it is the only one of the
   three that changes what litefactory's authorisation *means* and it directly weakens the premise
   SD-1 rests on.

3. **Which mechanism — (a), (b) or (c)?** Depends on (1) and on Plan's appetite for a migration.
   Note that SD-3 needs a migration anyway (R11), so if stories A and C ship together the migration
   cost of (b) is partly already paid.

4. **Should `internal/daemon/doc.go` be brought up to date as part of this work?** It does not
   describe the issue-body trigger at all, re-verified in run 0002. Recommend yes, and that it be a
   task rather than a side-effect. SD-2 raises the stakes: the author-dependent behaviour it
   introduces will read as non-determinism to anyone who cannot find it written down.

5. **Should branch 4 — a body whose only content is the `lf: ask` tag — speak under SD-3?**
   Marginal, and deliberately not decided here. It clears the volume bar (the issue is assigned and
   the body is non-empty) but the refusal is obscure enough that a reply may confuse more than it
   helps. Recommend deferring rather than resolving it in this feature.

---

## Recommended scope for Plan mode

**Three stories, in this order.**

**Story A — the timing change (R-body′).** Turn the issue-body gate from a first-sight moment into a
durable state test — "open ∧ assigned to the owner ∧ non-empty body ∧ no comments ∧ no run yet at
trigger 0" — evaluated on every tick that sees the issue, with once-ever enforced by
`UNIQUE (session_id, trigger_comment_id)`, which already enforces it. Closes workflows 2 and 4 for
issues with no comments, leaves workflows 1 and 3 untouched.

**Story B — the SD-2 author exemption.** Add the disjunct: existing comments stop blocking when the
issue's author id equals the stored owner id. Small in code, and its whole cost is in the argument:
the AC-2.16d amendment entry, the R4 doc comment at `internal/harness/prompt.go:247-256`, and saying
out loud in `internal/daemon/doc.go` that two issues that look identical can behave differently
according to who opened them.

**Story C — SD-3, visible refusals.** The largest of the three. **Do not ship it without its
once-only state (R11)** — the SD-2 × SD-3 loop posts a comment on every tick, forever, on exactly the
issues the owner cares about most. Scope it as: a new `report.ReplyKind` plus its case in
`replyKind()` (`internal/turn/report.go:612-625`); a posting path that does not key on a triggering
comment id, because `ReplyRequest.validate` refuses one at zero and both of `ReportReply`'s
idempotency sources degenerate there (`internal/turn/report.go:254-255, :413-420, :440`); a
`0003_*.sql` migration carrying "this thread has been told"; and the "post first, then commit"
ordering every other reporting path uses. Speak only on branches 2, 5 and 6 — never on branch 1
(not assigned), which fires for most issues in the repository on every tick.

**Sequence A behind the probe.** Open question 1 is unresolved and run 0002 could not resolve it. It
is two read-only `gh api` GETs and it is the difference between a contained change inside
`internal/daemon` and one that grows the forge surface and its pinned method set. **Read R12 before
choosing** — a positive answer is not sufficient on its own, because the listing this feature depends
on has been measured lagging thirty minutes in this very repository, with no second listing to cover
assignment.

**Do not scope a stampede control.** SD-1: the owner has accepted the exposure and given his
reasoning. R1's containment options are recorded as *not wanted*. Do document the accepted exposure
in a comment on the gate.

**Do not scope:**

- an authorship-based **trigger** — AC-2.16a stands, and SD-2 is an exemption from AC-2.16d rather
  than a trigger, which is a distinction worth carrying into the plan verbatim (see *SD-2 and
  AC-2.16a: not actually a conflict*);
- anything that re-implements "wait for assignee change" — it already exists;
- webhooks, issue reactions, or a run-rate limiter (D-X stands);
- a change to "assigned to anybody" — that is open question 2 and is the owner's to decide.

**Expect these tests to need changing**, because they encode the current behaviour rather than merely
covering it. All line numbers re-pinned in run 0002. `[V]`

| Test | Location | Why |
|---|---|---|
| `TestAnUnassignedIssueBodyStartsNothing` | `internal/daemon/tick_test.go:508` | its second half asserts an *ordinary sighting* of an unassigned issue starts nothing, which stays true — but the fixture needs a late-assignment case |
| `TestAnAssignedIssueBodyRunsOnceAsTheFirstInstruction` | `:550` | its "nothing looks at the body a second time" assertion is exactly the property whose *reason* changes (R3) |
| `TestFirstSightPlantsTheWatermarkBeforeTheBodyRuns` | `:622` | first sight stops being where the body decision happens |
| `TestABodyRunLeavesTheWatermarkWhereBootstrapPlantedIt` | `:653` | the AC-2.23 `MAX(0,0)` property; should survive, worth re-asserting under R-body′ |
| **`TestAnIssueWithCommentsDoesNotFireItsBody`** | **`:743`** | **the test SD-2 directly falsifies for owner-authored issues. Story B's primary test change, and run 0001 did not name it** |
| `TestANonOwnerAssigneeDoesNotFireTheBody` | `:774` | must keep passing unchanged — it is AC-2.16a, which SD-2 does not touch |
| `TestABodyThatCommandsNothingStartsNothingAndPostsNothing` | `:696` | "posts nothing" is exactly what SD-3 changes for branch 5 |
| `TestIssueBodyToolsRefusalPostsNothing` | `internal/daemon/tools_test.go:339` | same, for branch 6 |

**Expect these doc comments to need rewriting** — under this codebase's rules that is part of the
change, not cleanup (`.aiteam/APP_CONTEXT.md:322-328`). Re-pinned in run 0002:

- `internal/daemon/tick.go:1018-1073` — the gate's four conditions, of which condition 4
  (`:1038-1041`) becomes conditional under SD-2;
- `internal/daemon/tick.go:1043-1051` — "Watermark first, then run", the exactly-once argument that
  R-body′ falsifies;
- `internal/daemon/tick.go:1487-1504` and `:1505-1511` — the body-path refusal trade, if SD-3 ships;
- `internal/turn/execute.go:185-194` — the `MAX(0, 0)` watermark argument;
- `internal/turn/prepare.go:152-157` — where `bodyIsInstruction` is decided;
- `internal/harness/prompt.go:226-256`, **especially `:247-256`**, whose "not reachable today" claim
  becomes false under R-body′ and more so under SD-2 (R4);
- `internal/daemon/doc.go:38-41` — which does not mention the issue-body trigger at all, and under
  SD-2 also owes a reader the author-dependence;
- `.aiteam/AC_AMENDMENTS.md` — **two** entries now: AC-2.16b's "on first sight" wording, and
  AC-2.16d's unconditional "an issue that already has at least one comment does not fire its body".

---

## Assumptions taken in run 0002

Recorded because this is a headless run and nobody could be asked.

- **SD-3's "here" means the GitHub issue thread**, not the daemon log. A debug log line already
  exists on every refusal branch, and it is not something the owner could "see" in the sense his
  comment implies. If he meant the log, story C disappears entirely — so this assumption is worth
  confirming before Plan sizes it.
- **The owner's answers are taken as decisions, not as opinions to be re-argued.** SD-1 in particular
  was raised in run 0001, put to him, and reaffirmed; this brief records its premises and does not
  relitigate it.
- **SD-2's rule is read as "author is the owner", using the numeric user id already stored**, not
  "author is not a bot" or "author has write access". His wording — "only if the creator of the issue
  is someone else" — names the creator, and the owner id is the only identity litefactory stores that
  can answer it.
- **Run 0001's assumptions carry forward unchanged**: that "the owner is the litefactory account
  (e.g. marioatcloudvane)" means the human owner identity and not the bot; and that issue #10's body
  is empty, taken from the run's task description `[O]` and still not verified against the forge —
  the `gh` probe that would have confirmed it was refused (see *Open question 1: what was tried*).
