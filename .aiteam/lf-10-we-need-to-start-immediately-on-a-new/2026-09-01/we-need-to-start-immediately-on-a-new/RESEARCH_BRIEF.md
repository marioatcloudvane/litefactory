# Research Brief — we-need-to-start-immediately-on-a-new (issue #10)

2026-09-01. Headless. Branch `lf/10-we-need-to-start-immediately-on-a-new`.

- **Run 0001** — first pass. Seven open questions.
- **Run 0002** — the owner answered questions 2, 3 and 4 and asked for questions 1 and 5 to be
  explained. Revised in place.
- **Run 0003** — this revision. Web tools were granted, so open question 1 was researched properly;
  the owner answered open question 5, **corrected run 0002's reading of open question 4**, and
  answered the R11 hazard. Revised in place. Run 0003's substantive work is: **SD-3 is superseded
  and rewritten** (it is a log line, not a posted comment), **R11 is retired** as an artefact of the
  superseded reading, **SD-4 and SD-5 are added**, and open question 1 is closed as
  *undocumented by GitHub* with a different, better-evidenced consequence than either earlier run
  expected.

**Provenance tags** used throughout:

- `[V]` verified this run by reading the current source at the cited `path:line`.
- `[O]` reported by the owner on issue #10. Owner statements are marked `[O2]` / `[O3]` where it
  matters which run's comment they came from.
- `[W]` second-hand or external, not verified here.
- `[A]` carried from an archived `.aiteam/` document, re-checked against source where load-bearing.
- `[X]` external fact established this run from a cited primary source outside this repository
  (GitHub's own documentation or changelog). New in run 0003 — it is deliberately *not* `[V]`,
  because nothing in this worktree proves it, and deliberately not `[W]`, because it is cited.

Every `path:line` in this brief was re-pinned **again** in run 0003 against the current worktree.
Line numbers quoted inside archived `.aiteam/` documents were **not** trusted — this session's own
history records citation rot five times across runs 0001 and 0002 (see *Archive corrections*). Run
0003 re-pinned every citation it relies on and found **no new rot in the brief's own text**: the
run-0002 corrections held. One further stale statement was found in `.aiteam/APP_CONTEXT.md` and is
recorded below.

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
that refusal is silent (AC-2.16c, `internal/daemon/tick.go:1084-1088`). `[V]` **Run 0003 establishes
that it is silent in the strongest possible sense: it produces no observable output anywhere at
all.** The refusal logs at `DebugContext`, litefactory runs at `info` by default
(`internal/config/config.go:256`, `internal/logging/logging.go:24-25, :38`), so the line is dropped
before it reaches `daemon.log`; and because the branch returns before `d.run(...)` (`:1124`) no
`runs` row and no run JSONL is created, so `litefactory logs <issue>` prints `"<ref> has no runs
yet"` (`internal/cli/cmd_logs.go:203-209`). `[V]` Not on the thread, not in the log, not in the CLI.
**That is what SD-3′ fixes.** So even a perfectly assigned, perfectly timed issue #10
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
  body path for good. This was R11, and it was the most serious finding of that run. **Run 0003
  retires it** — it was produced entirely by a misreading of SD-3 that the owner has now corrected.
  See *R11, retired*.

### What run 0003 added

**Three things changed, and one of them changes the shape of the recommended plan.**

1. **SD-3 was read wrongly by run 0002, and is superseded.** The owner has said plainly `[O3]`:
   *"Question 4: i ment the log actually, not a comment on git!"* So "i want to see this logged here"
   meant **litefactory's own daemon log**, not a comment on the issue thread. Run 0002 built the
   reply-kind plumbing, the once-per-issue durable state, the migration and the whole of R11 on top
   of the wrong reading. The corrected SD-3 is **a log-level change and a log line** — see
   *SD-3 (superseded)* and *SD-3′ (corrected)*. Story C shrinks from "the largest of the three" to
   roughly a dozen lines with no migration, no forge write and no new reply shape.
2. **R11 evaporates**, and so does the migration it forced. Recorded rather than deleted, because it
   was a real hazard under the reading that produced it.
3. **Open question 1 is closed — but not with a yes or a no.** GitHub does not document whether an
   assignment change bumps `updated_at`, anywhere, and there is no authoritative statement either
   way `[X]`. What run 0003 *did* establish, and what nobody had checked before, is that the
   fallback everyone assumes exists — a repository-wide issue-events feed litefactory could poll —
   **has no `since` parameter and no documented ordering** `[X]`. That kills mechanism (b′) "add a
   forge endpoint" as a cheap escape hatch and, together with R12, makes mechanism **(c)** the
   recommended one. See *Open question 1, closed*.

**Open question 5 is answered and costs nothing.** *"It should only count when it is assigned to me.
Not to anyone else."* `[O3]` That is **exactly what the code does today** — SD-4. No change.

**The owner's answer to "problem 1" was correct and is now moot.** SD-5.

---

## Settled decisions

Owner decisions, recorded with his reasoning, and **not re-argued** below — where a risk section
previously left one of them open, it now records the consequence of the decision instead.

| # | Decision | Run | Status |
|---|---|---|---|
| SD-1 | the stampede (R1) is accepted, not mitigated | 0002 | stands |
| SD-2 | AC-2.16d kept, relaxed for owner-authored issues | 0002 | stands |
| SD-3 | refusals become visible **on the issue thread** | 0002 | **SUPERSEDED by SD-3′** — the premise was a misreading |
| SD-3′ | refusals become visible **in the daemon log** | 0003 | stands |
| SD-4 | "assigned to the owner" means the owner's account only | 0003 | stands — **already the code** |
| SD-5 | litefactory posts as the bot account, and never triggers itself | 0003 | correct, and **moot** under SD-3′ |

**On superseding rather than editing.** SD-3 is struck through and kept, not deleted. This
document's own *History* narrates run 0002 building real analysis on the wrong reading — the
once-per-issue mechanism, the reply-kind plumbing, R11, and a `0003_*.sql` migration — and a reader
who finds only the corrected version will not understand why the recommended plan shrank by a story.
That follows the spirit of this repository's amendment ledger, whose whole design is that a
superseded rule is marked superseded and the reason recorded rather than rewritten away
(`.aiteam/AC_AMENDMENTS.md`, and the archive rule at `.aiteam/APP_CONTEXT.md:126-134`). `[V]` The
ledger convention is for `AC_AMENDMENTS.md` specifically; applying it here is run 0003's own choice
and is recorded as such.

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

### ~~SD-3 — refusals become visible on the thread, but not naively~~ — **SUPERSEDED**

> **Superseded by SD-3′ on 2026-09-01 (run 0003). Do not build from this entry.**
>
> The decision text was right; run 0002's *interpretation* of it was wrong, and the wrong
> interpretation is what everything downstream was sized against.
>
> **Decision, verbatim** `[O2]`: *"yes, of course, i want to see this logged here!"*
>
> **Run 0002's assumption, now falsified:** that "here" meant **the GitHub issue thread**. Run 0002
> reasoned that a debug log line already existed on every refusal branch and so could not be what he
> meant. That reasoning was not unreasonable — see SD-3′, where it turns out the debug line really
> *is* invisible at the default log level — but the conclusion it drew was wrong.
>
> **The owner's correction, verbatim** `[O3]`: *"Question 4: i ment the log actually, not a comment
> on git!"*
>
> **What this entry caused, so the reader can discount it wherever it still appears:** a proposed
> `report.ReplyKind` and its `replyKind()` case; a posting path not keyed on a comment id; a
> `0003_*.sql` migration carrying "this thread has been told"; the "post first, then commit"
> ordering requirement; the entire *SD-3: which refusals speak, and how they stay quiet afterwards*
> section below; and **R11**, run 0002's headline risk. All of it is downstream of a forge write
> that is no longer proposed. The analysis is kept for the record and is marked where it appears.

### SD-3′ — refusals become visible **in the daemon log** (run 0003)

**Decision** `[O3]`, reading the two comments together: a refusal on an issue the owner assigned to
himself must leave a trace **the owner can actually find in litefactory's own log**. Nothing is
posted to the forge.

**This is a much smaller change than SD-3 was, but it is not a no-op, and the reason is the log
level.** `[V]`

- **litefactory runs at `info` by default.** `logging.New` builds one `slog.HandlerOptions{Level:
  opts.Level}` and shares it across both sinks (`internal/logging/logging.go:38, :42, :45`); the
  zero value is documented as `slog.LevelInfo` (`:24-25`); `ParseLevel` returns `LevelInfo` for the
  empty string and for a parse error (`:99-108`); `cmd/litefactory/main.go:103-117` falls back to
  `LevelInfo` on every failure path; and the shipped default is `DefaultLogLevel = "info"`
  (`internal/config/config.go:256`, applied at `:234`). The only way to change it is the `log_level`
  key in `$LF_HOME/config.yaml` (`internal/config/config.go:79-81`, template
  `internal/config/template.go:65-66`, validated `internal/config/validate.go:87, :98-106`). **There
  is no CLI flag and no environment variable.**
- **So `DebugContext` never reaches the log the owner reads.** Three of the four silent refusal
  branches are `DebugContext` calls: `internal/daemon/tick.go:1085-1087` (the assignment /
  empty-body / has-comments gate), `:1104` (`KindNone`) and `:1113-1116` (`!command.Spawns()`).
  `[V]` At the default level they are dropped before the JSON handler sees them.
- **And there is no run log to fall back on either.** All three branches `return nil` *before*
  `d.run(...)` at `internal/daemon/tick.go:1124`, which is the only path into `turn.Execute`. No
  `runs` row is allocated and no `NNNN.jsonl` is created. `litefactory logs <issue>` reads the
  per-run JSONL and **never** `daemon.log` (`internal/cli/cmd_logs.go:184-198, :319-324`) `[V]`, so
  for a refused issue it prints `"<ref> has no runs yet"` (`:203-209`) and nothing else.

**The honest statement of today's behaviour, which is worse than run 0002 realised:** on the three
`DebugContext` branches, a refusal produces **no observable output anywhere at all** — nothing on
the thread, nothing in `daemon.log`, nothing in `litefactory logs`. That is exactly the owner's
experience on issue #10 and it is the whole of what SD-3′ fixes.

**Where the log actually is, and how the owner reads it.** `[V]`

| | |
|---|---|
| Path | `$LF_HOME/logs/daemon.log` — `internal/config/paths.go:20, :22, :73, :75-77` |
| Format | **JSON lines.** `slog.NewJSONHandler` (`internal/logging/logging.go:45`); a text copy goes to the console only when stdout is a tty (`:42`, `cmd/litefactory/main.go:56-59`) |
| Redaction | **None, deliberately** — "line-oriented and deliberately unredacted: the local copy is always the full truth (S-7)" (`internal/config/paths.go:75-77`) |
| Rotation | **None.** Accepted M0 debt, named as such at `internal/launchd/plist.go:58-62` |
| How to read it | `tail`/`jq` on the file, or run the daemon in the foreground (`litefactory daemon run`) and watch the tty copy. **`litefactory logs` does not show it** and there is no `daemon logs` command — the CLI surface is fixed at `internal/cli/commands.go:9-20` |

**The change SD-3′ asks for is: promote those refusals from `DebugContext` to `InfoContext`, and
make the lines say enough to be the answer.** No forge write, no reply shape, no migration, no new
state.

**This is squarely the house rule, not a deviation from it.** The codebase already legislates exactly
this trade, in almost these words, for the one other place where litefactory silently throws
something away — `Filter.selfReason` dropping a comment (`internal/turn/classify.go:343-352`) `[V]`:

> Info rather than debug, at the default level, because this is the one verdict that throws a comment
> away. Silence is the real cost of a misclassification: a comment dropped as self produces no run,
> no reply and no reaction, so from the thread it is indistinguishable from a daemon that is not
> running at all. The reason is what makes the next occurrence diagnosable rather than a mystery …
> (AC-S0.4).

and its test states the level rule outright (`internal/turn/classify_test.go:521-527`) `[V]`:

> the log line is the only trace a human can follow, and it has to name which condition fired. **The
> level matters as much as the line.** It is asserted against a handler at `slog.LevelInfo`, which is
> what `litefactory` runs at without `log_level` being set: **a line only visible under debug is not
> a line the owner can find.**

`Admit`'s own doc comment says the same thing from the other end: "the database says what was
decided, **the log is what somebody reads when they are asking why nothing happened**" (AC-S0.4,
`internal/turn/classify.go:291-297`) `[V]`. SD-3′ is that principle applied to the one path that was
missed.

**One branch is already done.** `Daemon.refuseTools`'s body path — refusal branch 6, the unresolvable
`lf:tools=` label — **already logs at `InfoContext`** (`internal/daemon/tick.go:1505-1511`) `[V]`,
and its line already explains itself in full ("Nothing was run and no comment was posted, because
there is no comment to answer; the next comment on this issue is refused on the thread"). It is the
in-tree precedent for exactly what SD-3′ wants, on exactly this code path, and it means branch 6
needs **no change at all** unless the wording is revisited.

### SD-3′ — which branches it covers, and it is all of them

**Run 0002 spent a long section deciding which refusals may *speak on the thread*, because the
volume of forge comments was the constraint. Under SD-3′ that constraint is gone**, and the answer
changes accordingly: a log line costs nothing socially, is never seen by a third party, and cannot
change litefactory's own behaviour. So the question is no longer "which refusals are worth a
comment" but "which log lines would drown the log".

**The owner's "yes, of course, i want to see this logged here!" was a general answer** `[O2]` — it
was given to the general question of whether a refusal on the body path should ever be visible, not
scoped to one branch. Run 0003 therefore reads SD-3′ as covering **all of `runIssueBody`'s refusal
branches**, and checks each against the volume test rather than against a "does it deserve a
comment" test.

| # | Branch | Site | Today | Under SD-3′ | Why |
|---|---|---|---|---|---|
| 1 | not assigned to the owner | `tick.go:1077`, logged `:1085-1087` | Debug | **stays Debug** | This is the only branch with a volume problem, and it is a real one. `Daemon.sight` creates a session for **every open issue in every registered repository** (`tick.go:394-419`, session at `:403`) `[V]`, so this fires for most issues most of the time — and under R-body′ it fires *on every tick*. Promoting it would flood `daemon.log`, which is unrotated (`internal/launchd/plist.go:58-62`). It is also not a refusal in the owner's sense: nobody asked for anything. |
| 2 | body empty or whitespace | `:1079`, logged `:1085-1087` | Debug | **Info** | The case the owner actually hit. Assigned by him, expecting work, got nothing. `[O]` |
| 3 | already has comments (AC-2.16d) | `:1081`, logged `:1085-1087` | Debug | **Info** | Assigned by him; the body was deliberately not run. Under SD-2 this fires only on third-party issues, so its volume is bounded and small. |
| 4 | body is only the `lf: ask` tag (`KindNone`) | `:1097-1106`, logged `:1104` | Debug | **Info** | Run 0002 deferred this because a *comment* here might confuse more than it helps. That objection was entirely about posting; it does not survive the correction. A log line for an obscure refusal is exactly what a log is for. **OQ5 of run 0002's list is closed by this** — see *Open questions*. |
| 5 | command spawns nothing (`/plan`, bare `/spike`) | `:1107-1118`, logged `:1113-1116` | Debug | **Info** | Assigned, non-empty, and the first line was a typo or a non-spawning command. |
| 6 | `lf:tools=` label does not resolve | `internal/daemon/tools.go` → `tick.go:1487-1533`, body branch `:1505-1511` | **already Info** | **no change** | Already done, and already self-explaining. See SD-3′ above. |

**Branch 1 is the one asymmetry and it should be written down rather than left to be rediscovered.**
The distinction that makes it defensible is the same one run 0002 drew, and it survives the change
of medium intact: branches 2–6 all require the issue to be **assigned to the owner**, which is the
signal that somebody deliberately pointed litefactory at it. Branch 1 is the absence of that signal.
"Not for me" stays quiet; "meant for me, and I still declined" speaks.

**On `internal/daemon/tools.go`'s body-path behaviour, called out separately as asked.** The
tool-refusal path posts nothing on the body path, by design, and its doc comment names itself "the
only branch to change if that trade is revisited" (`internal/daemon/tick.go:1480-1486`) `[V]`.
**Under SD-3′ that trade is not being revisited** — nothing new is posted anywhere, so the branch's
posting behaviour is untouched, and its logging behaviour is already correct. Plan should leave it
alone. The one thing worth noting is that under R-body′ this branch becomes reachable on *every*
tick rather than once (R2), so if Plan later wants a per-tick rate guard on any log line, this is
the branch that needs it — not because it is loud today but because R-body′ makes it capable of
being loud. Recorded, not recommended.

### SD-3′ — is it its own story, or a line in Story A?

**It folds into Story A. It should not be its own story.** Reasoning, stated so Plan does not have
to re-derive it:

- **The code change is roughly a dozen lines in one function.** Four `DebugContext` call sites in
  `internal/daemon/tick.go` become `InfoContext` (`:1085-1087`, `:1104`, `:1113-1116`), one of them
  conditionally — branch 1 must stay Debug, which means the single shared log line at `:1084-1088`
  has to split by which refusal fired, since today all three gate branches share one call site. That
  split is the only real design decision in the whole of SD-3′, and it is small.
- **It shares Story A's blast radius exactly.** Story A rewrites the gate at
  `internal/daemon/tick.go:1074-1125` and the doc comments around it (`:1018-1073`, `:1043-1051`).
  SD-3′ edits the same twenty lines. Shipping them separately means touching the same function twice
  and writing the same doc comment twice.
- **It needs no migration, no new endpoint, no new reply shape and no durable state.** Every reason
  run 0002 gave for making Story C its own story was a consequence of the forge write.
- **Its test cost is small and already located.** `TestABodyThatCommandsNothingStartsNothingAndPostsNothing`
  (`internal/daemon/tick_test.go:696`) and `TestIssueBodyToolsRefusalPostsNothing`
  (`internal/daemon/tools_test.go:339`) both keep passing unchanged — **"posts nothing" is still
  true**, which was not the case under SD-3. What is needed is a new assertion in the style of
  `TestSelfDropIsLogged` (`internal/turn/classify_test.go:521-552`), which asserts a log line's text
  *and* its `"level":"INFO"` against a handler at `slog.LevelInfo` `[V]`. That test is the template
  and Plan should say so.

**One caveat against folding.** Story A is behind a mechanism decision (see *Open question 1,
closed*); SD-3′ is not, and is independently useful *today* — it would have explained issue #10 on
its own. If Plan wants to ship anything before the mechanism question is resolved, SD-3′ is the
piece that can go first. That is an argument for sequencing it first *within* Story A, not for
making it a separate story.

### SD-4 — "assigned to the owner" means the owner's account only (run 0003)

**Decision** `[O3]`, verbatim: *"It should only count when it is assigned to me. Not to anyone else.
So if an issue is assigend to "maxleo", this is not taken into my litefactory processing."*

**This confirms run 0002's recommendation, and the good news is plain: nothing needs to change.**
`[V]` The gate today is

```go
case !t.issue.AssignedTo(d.filter.Identities().OwnerUserID):
```

at `internal/daemon/tick.go:1077` — re-pinned this run and exact. `Issue.AssignedTo`
(`internal/github/issues.go:94-104`) matches the **numeric user id** only and treats a zero id as
matching nothing; `Identities.OwnerUserID` is the one account recorded by `litefactory init`
(`internal/turn/classify.go:103`, `internal/store/migrations/0001_init.sql:17-25`). So the rule
already reads: *is the one specific account that ran `litefactory init` on this machine among this
issue's assignees?* An issue assigned to `maxleo` and nobody else fails that test and starts
nothing. There is exactly one production call site (`internal/daemon/tick.go:1077`) and
`TestANonOwnerAssigneeDoesNotFireTheBody` (`internal/daemon/tick_test.go:774`) already pins the
behaviour. `[V]`

**So SD-4 is a confirmation, not a change.** Plan should scope **no work** for it, and should not
add a config key, an allowlist, or an "assigned to anybody" mode. Run 0002's *Open question 5,
explained* section is retained below as the reasoning behind why this is the right rule; it is now
settled rather than open.

**The `maxleo` aside, addressed because it was raised** `[O3]`: *"However, maxleo might run his own
instance of a litefactory and thus trigger something on another account!"* **True, and it changes
nothing here.** Each litefactory installation stores its own owner identity in its own
`$LF_HOME/litefactory.db` and compares against that one id and no other. A second installation,
owned by a different account, would independently decide that an issue assigned to *its* owner is
work for *it*. Nothing in this instance can see, read or be triggered by another instance's
identity, and nothing in this instance's configuration would need to change to accommodate one. The
only place two installations meet at all is R9 — two installs assigned to the *same* issue would
both fire, because `naming.Branch` has no user component — and that was already recorded as an
accepted risk from issue #6. **This aside needs no design response.**

### SD-5 — litefactory posts as the bot account, and never triggers itself (run 0003)

**The owner's answer to run 0002's "problem 1"** `[O3]`, verbatim: *"no, it should not because the
bot account (not my personal one!) should comment, never my account; so i think it would not trigger,
because a post from the bot account so far has never triggered anything!"*

**His reasoning is exactly right, and it is enforced in code rather than by habit.** `[V]`
Re-verified this run, not carried from run 0002:

- `Filter.Classify` (`internal/turn/classify.go:245-253`) asks `selfReason` **first**, before it
  asks whether the author is the owner.
- `Filter.selfReason` (`internal/turn/classify.go:263-274`) is three conditions, any of which is
  sufficient: the author id equals the stored bot id (`:265`, guarded on the bot id being non-zero,
  `:260-262`), the forge flagged the author as a bot (`:267`), or a line of the body begins with
  `<!-- lf:` (`:269`, `HasMarker`, `SelfReasonMarker`).
- A comment classified `self` is recorded and produces no run, no reply and no reaction
  (`internal/turn/classify.go:291-297`).
- And the separation he relies on is not merely conventional: `Identities.Validate`
  (`internal/turn/classify.go:119-129`) **refuses to construct a `Filter` at all** when
  `OwnerUserID == BotUserID`, "because an account that is both owner and bot reads its own reports
  as instructions — the loop this whole task exists to prevent (AC-0.6)".

So: three independent guards, checked before the owner test, backed by a startup refusal. He was
right on the facts.

**And it no longer applies to anything.** The question he was answering was whether a refusal
*comment* posted by litefactory would re-trigger the body gate on the next tick — R11. Under SD-3′
**litefactory posts no comment**, so there is no comment for the self-filter to run against and no
loop to be closed. The premise was withdrawn by his own correction on question 4, in the same
message.

**Both things are true and neither cancels the other.** He answered precisely the question that was
in front of him, correctly, and the machinery he pointed at is real and would have done exactly what
he said. It simply has nothing to do now. SD-5 is recorded so that if a future change ever *does*
put litefactory back on the thread on the body path, the answer is already here and does not have to
be worked out again.

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

## ~~SD-3: which refusals speak, and how they stay quiet afterwards~~ — **SUPERSEDED (run 0003)**

> **This entire section is downstream of the superseded reading of SD-3 — that litefactory would
> post a refusal *comment on the issue thread*. It does not. See SD-3′.**
>
> **What is still live in it:** the refusal-branch table immediately below (re-pinned in run 0003
> and correct), and the observation that branch 1 fires for most issues in the repository on most
> ticks, which is why SD-3′ leaves branch 1 at Debug.
>
> **What is dead in it:** everything about posting — the "which refusals speak" line as a *forge*
> decision, the whole of *At most once per issue*, both storage mechanisms (i) and (ii), the
> `0003_*.sql` migration, the `report.ReplyKind` and `replyKind()` work, the `ReplyRequest`
> comment-id obstacle, and *The feedback loop SD-3 and SD-2 create together*. **None of it is
> proposed any more.**
>
> Kept, not deleted, because run 0002's *Recommended scope* sized Story C as "the largest of the
> three" on the strength of it, and a reader needs to see why that judgement was made and why it no
> longer holds. The findings themselves were sound; the premise was not.

### The refusal branches that exist today

**Re-pinned in run 0003 against the current worktree. Still accurate.** `[V]`

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

### ~~The feedback loop SD-3 and SD-2 create together — and it is the worst finding of this run~~ — **RETIRED (run 0003)**

> **This hazard does not exist.** It required litefactory to post a comment; SD-3′ posts nothing.
> Retained verbatim below, and analysed under *R11, retired*, because it was real under the reading
> that produced it and because retiring a headline risk silently is worse than showing the working.

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
assignees change?** **Run 0003 researched this properly, with web access, and the answer is that
GitHub does not say.** `[X]` See *Open question 1, closed* below for the sources and for what was
found instead. The consequence for the design is set out there and it is not the branch either
earlier run expected.

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

### Open question 1, closed (run 0003): what GitHub documents, and what it does not

The owner granted web access for this run — *"Question 1: I added the web tools, so you can now
answer this question!"* `[O3]` — and the question was researched against GitHub's own documentation
and changelog. **It produced a real answer, but not the yes-or-no answer that was expected.** All of
the following is `[X]`: cited to a primary source outside this repository, fetched 2026-09-01,
against REST API version `2022-11-28`.

#### The direct question: not documented, either way

**GitHub documents nothing about what mutates an issue's `updated_at`.** Checked directly:

- [REST API endpoints for issues](https://docs.github.com/en/rest/issues/issues?apiVersion=2022-11-28)
  — `updated_at` appears in the response schema as a field with **no semantics attached**.
- [REST API endpoints for issue assignees](https://docs.github.com/en/rest/issues/assignees) — the
  endpoints that add and remove assignees **do not mention `updated_at` at all**.
- The GitHub Changelog carries no entry on the subject. The only `updated_at`-clarifying changelog
  entry found is [for the code scanning API,
  2023-08-07](https://github.blog/changelog/2023-08-07-improving-the-code-scanning-rest-api-updated_at-field/)
  — a different API, no bearing on Issues, and cited only to show that GitHub *does* publish such
  clarifications when it makes them, and has not made this one.
- No GitHub Community discussion or support answer on the specific question was found.

**There is no authoritative list of what does and does not move `updated_at` on an issue.** That is
itself a finding, and it is stated with high confidence: the absence was checked, not assumed.

**The nearest adjacent evidence, and it is explicitly not an answer.** GitHub's own
[`actions/stale`](https://raw.githubusercontent.com/actions/stale/main/README.md) README — a
GitHub-org repository whose entire `days-before-close` arithmetic depends on the behaviour — states
that *"since adding the stale label will alter the last update date, we can calculate the number of
days from this date"*. **That establishes that a label change bumps `updated_at`.** It says nothing
about assignment. Reasoning "labels bump it, therefore assignees do" is an inference across two
different mutation paths and is recorded here as **low confidence and not a finding**. It is
suggestive; it is not evidence.

**A second, quieter uncertainty worth carrying:** the `since` parameter is documented as *"Only show
results that were last updated after the given time"* — a paraphrase. GitHub never writes the token
`updated_at` in that sentence, and never says what `sort=updated` orders by. Every consumer treats
`since`, `sort=updated` and the `updated_at` response field as one clock and they almost certainly
are, but if Plan needs that identity to be load-bearing it rests on a paraphrase.

#### What was found instead, and it is more decisive than the original question

**Run 0003 checked whether the obvious fallback exists — a forge endpoint litefactory could poll for
assignment events instead of relying on `updated_at`. It does exist, and it cannot be polled.**

| Endpoint | Carries `assigned`/`unassigned`? | Usable as a discovery source? |
|---|---|---|
| [`GET /repos/{owner}/{repo}/issues/{n}/timeline`](https://docs.github.com/en/rest/issues/timeline?apiVersion=2022-11-28) | **Yes.** "Timeline Assigned Issue Event" and "Timeline Unassigned Issue Event" are documented return types, each carrying `actor`, `assignee` and `created_at` as **required** fields | **No — it is per-issue.** One API call per issue per tick. litefactory polls every open issue in every registered repository; this is a request-count multiplier, not a listing |
| [`GET /repos/{owner}/{repo}/issues/events`](https://docs.github.com/en/rest/issues/events?apiVersion=2022-11-28) | **Yes.** "Assigned Issue Event" and "Unassigned Issue Event" are documented return types, carrying `assignee` and `assigner`. Repository-wide | **Barely.** Its **entire parameter list is `owner`, `repo`, `per_page` (max 100), `page`.** There is **no `since` filter and no `sort` parameter**, and **the ordering is not documented in either direction** |

Two further facts about the repo-wide events endpoint, both relevant to Plan:

- **Retention is not documented.** The [2024-11-08 retention
  changelog](https://github.blog/changelog/2024-11-08-upcoming-changes-to-data-retention-for-events-api-atom-feed-timeline-and-dashboard-feed-features/)
  cut retention from 90 to 30 days effective 2025-01-30 and enumerates the affected endpoints
  exhaustively — `/repos/{owner}/{repo}/events` is on that list; **`/repos/{owner}/{repo}/issues/events`
  is not.** Those are two different APIs one path segment apart, and the 30-day figure must not be
  allowed to migrate onto the wrong one. But absence from the list is **silence, not a guarantee**:
  GitHub documents no retention for the Issues events endpoint anywhere.
- **The timeline endpoint is GA, not a preview.** The current docs page carries no preview media
  type, and the [2021-10-14 preview promotions
  changelog](https://github.blog/changelog/2021-10-14-rest-api-preview-promotions/) graduated
  twenty-three of twenty-five previews, naming the only two survivors as `corsair` and
  `scarlet-witch`. `mockingbird` is not among them. So `application/vnd.github.mockingbird-preview+json`
  is no longer required. (This is an inference off a two-item exclusion list, corroborated by the
  docs page carrying no preview header — high confidence, but it is an inference.)

**No indexing-lag documentation was found**, in either direction. The one community report retrieved
— [community discussion
#174956](https://github.com/orgs/community/discussions/174956), opened 2025-09-27, **never answered
by GitHub staff**, auto-closed as inactive — describes a *different* failure: the REST issue listing
returning only pull requests and omitting issues entirely, while GraphQL returned correct results.
That is a correctness report, not a timestamp-lag report, and it is unresolved secondhand evidence.
It is noted because R12's measured thirty-minute lag in this repository is now the *only* evidence
about this listing's reliability that anybody has, and it is this project's own.

**What the research could not reach:** stackoverflow.com is not fetchable by the researching agent's
user agent, so any observed-behaviour reports living there are absent. That is a real gap and is
stated rather than glossed.

#### What this settles, and what it changes

**OQ1 is closed as a research question and moved out of Open Questions.** Not because it has a yes
or a no, but because **more searching will not produce one** — GitHub does not document it, and the
only thing that would settle it is an empirical test. That is a different kind of task from research
and is recorded as a Plan-time spike below rather than as an unanswered question.

**The consequence for the design is the important part, and it inverts run 0001's ranking
completely.** Both of the "if it does not bump" escape hatches that runs 0001 and 0002 assumed
existed have now been examined and neither is cheap:

1. **"Add a forge endpoint (issue events / timeline)"** was recorded as the fallback. It is now
   materially worse than it looked. The per-issue timeline is one request per issue per tick, which
   AC-2.24's whole posture argues against. The repo-wide events feed **has no `since` parameter** —
   so polling it means page-walking from page 1 on every tick and client-side cursoring on
   `created_at`, **against an ordering GitHub has not committed to in writing**, with undocumented
   retention. That is not a listing; it is a scrape with a guess about sort order at its centre. And
   it would still mean growing `daemon.Forge` (`internal/daemon/daemon.go:78-84`) and
   `TestClientMethodSetIsPinned` (`internal/github/surface_test.go:64-109`) `[V]` — the guard that
   exists so that growing the forge surface is a decision somebody writes down.
2. **"Rely on the `updated_at` bump"** is now an *undocumented* dependency rather than an unverified
   one. Even if a spike confirms the bump today, GitHub has published no contract, so the behaviour
   could change without a changelog entry — and R12 shows this repository has already measured the
   listing failing to surface a bump it had itself measured.

**So mechanism (c) — keep `needs_poll` set — is the one run 0003 recommends.** It is the only
option that depends on **no external contract at all**: a flagged session is unioned back into
discovery from the local database (`Daemon.backlog`, `internal/daemon/tick.go:350-375`) at no API
cost (`internal/store/migrations/0002_session_backlog.sql:40-45`) `[V]`. Run 0001 called it "the
worst of the three"; run 0002 revised that to "the most robust and the most expensive"; **run 0003
ranks it first**, because the two alternatives both turned out to rest on things GitHub declines to
promise. Its cost is unchanged and real — see the narrowed variant under *Size and shape*.

### ~~Open question 1: what was tried in run 0002, and what stopped it~~ — superseded by the above, retained for the record

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

### Open question 5, **now settled as SD-4**: "assigned to the owner" versus "assigned to anybody"

**Decided by the owner in run 0003** `[O3]`: assigned to him specifically. See **SD-4** above for the
decision and for the finding that it needs no code change. This section is the reasoning that
produced the recommendation he accepted, and is retained because Plan should carry the argument, not
just the verdict. It is **no longer an open question**.

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

**Found in run 0003:**

- **A third instance of the stale "seven endpoints" claim.** `.aiteam/APP_CONTEXT.md:127` — the
  *Do not reinvent these* table — describes `internal/github` as "Concrete `*Client`, **seven
  endpoints**, `net/http` only". `[V]` Run 0002 found the same claim at `:212` and `:691`;
  `TestClientMethodSetIsPinned` (`internal/github/surface_test.go:64-109`) pins twelve methods, ten
  of them endpoint-calling. Changes no conclusion. Per the archive rule
  (`.aiteam/APP_CONTEXT.md:126-134`) the correction belongs in the ledger, not in an edit to the
  archived text.
- **No new rot found in this brief's own citations.** Run 0003 re-pinned `runIssueBody`
  (`internal/daemon/tick.go:1074-1125`, gate at `:1077/:1079/:1081`, refusal logs at `:1085-1087`,
  `:1104`, `:1113-1116`, run at `:1120-1124`), `refuseTools` (`:1487-1533`, body branch
  `:1505-1511`, doc `:1480-1486`), `Filter.Classify` (`internal/turn/classify.go:245-253`),
  `selfReason` (`:263-274`), `Identities.Validate` (`:119-129`) and `Admit`'s AC-S0.4 comment
  (`:291-297`). **All exact.** Run 0002's corrections held.

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

**Run 0003's sizing. Two stories, not three, and no migration.** Run 0002 recorded three; SD-3′
collapses the third into the first.

| Story | Depends on | Size |
|---|---|---|
| **A — the timing change (R-body′), including SD-3′'s visible refusals** | the mechanism choice (below); the log-level part depends on nothing and could ship first | contained in `internal/daemon`. **No migration under mechanism (a) or (c).** The code is small; the cost is in the doc comments and the amendment entries |
| **B — the SD-2 author exemption** | A (pointless without it) | **small.** One conjunct in the gate; both operands already on the code path (`internal/daemon/tick.go:1077, :1093`) `[V]`. Its cost is in the argument and the doc comments, not the code |
| ~~C — SD-3, visible refusals~~ | ~~R11~~ | **DISSOLVED (run 0003).** Folded into A. See *SD-3′ — is it its own story*. No `ReplyKind`, no `replyKind()` case, no comment-less reply path, no `0003_*.sql`, no once-only state. Roughly a dozen lines and a log-assertion test |
| **S — a spike, if Plan wants mechanism (a)** | nothing | ten minutes. Empirically test whether assignment bumps `updated_at`. Only needed if Plan prefers (a) over (c). See below |

**SD-4 requires no story.** The rule the owner asked for is already the rule the code enforces.

For story A, three mechanisms remain available. **Run 0003 recommends (c) and states why; Plan still
chooses.**

- **(a) Re-run the body gate from the drain path as well**, relying on the existing
  `UNIQUE (session_id, trigger_comment_id)` and `GetRunByTrigger` for once-ever. Smallest code
  change; costs no schema change, no new endpoint, no extra API request (the labels and assignees
  already ride the issue payload the tick has in hand). Its cost is entirely in the *argument*: four
  doc comments currently state that exactly-once is a control-flow property and become untrue
  (`internal/daemon/tick.go:1043-1051`; `internal/turn/execute.go:185-194`;
  `internal/turn/prepare.go:152-157`; `internal/harness/prompt.go:251-255`), and the codebase's own
  style rule makes fixing them part of the change (`.aiteam/APP_CONTEXT.md:322-328`). It also adds an
  extra `ListCommentsAfter`-derived comment-count check on the drain path, which drain already has in
  hand. **Its real weakness is external and now documented: it depends entirely on the issue listing
  re-surfacing the issue after a late assignment — which requires an `updated_at` bump GitHub does
  not document (OQ1), delivered promptly by a listing this repository has measured lagging thirty
  minutes (R12), with no second listing to cover it (R12).** Three unguaranteed things in series.
- **(b) A new `sessions` column** recording that the body has been considered or has fired
  (migration `0003_*.sql`). More explicit, survives a `runs` table cleanup, and gives an obvious
  place to hang a "considered and refused, do not reconsider" state. Costs a migration; note the
  ledger's precedent that a column was rejected once on migration-budget grounds
  (`.aiteam/AC_AMENDMENTS.md:1286-1312`). **Run 0003 note: (b) does not solve the discovery problem.**
  It changes how "has it already run" is recorded; it does not change how the daemon comes to look at
  the issue again. It is a variant of (a) on that axis and inherits (a)'s external dependency. The
  migration it was going to share with Story C no longer exists, so its cost is now unshared.
- **(c) Keep `needs_poll` set** for sessions whose body has not fired. Run 0001 assessed this as "the
  worst of the three"; run 0002 revised that to "the most robust and the most expensive"; **run 0003
  recommends it.** The cost is unchanged and real: `needs_poll` is the dispatch backlog, and the
  naive form would put *every open unassigned issue in every registered repository* permanently into
  it, inverting the purpose of the partial index
  (`internal/store/migrations/0002_session_backlog.sql:42-45`), whose stated design point is that
  "the steady state — every session idle — carries no index entries at all". `[V]`

  **What changed to make it the recommendation** is not its cost but its competitors'. (a) and (b)
  both depend on a chain of three things GitHub either does not document or this repository has
  measured failing. (c) depends on **none of them**: a flagged session is unioned back into
  discovery from the local database (`Daemon.backlog`, `internal/daemon/tick.go:350-375`) at no API
  quota cost. It is the only option whose correctness argument is entirely inside this codebase.

  **The narrowed variant is what Plan should actually cost.** Flag only sessions that are *plausible
  candidates* — open, non-empty body, no run yet at trigger 0 — rather than every open issue. In the
  steady state on a real repository most issues are either already run, closed, or have an empty
  body, so the index stays small and the design point of the partial index is bent rather than
  inverted. This is the piece of design work Plan owes the story, and it is the only genuinely
  open engineering question left in this brief.

**If Plan prefers (a), it needs a ten-minute spike first, and the brief will not answer it from
memory.** GitHub does not document the behaviour (OQ1), so the only way to know is to measure it:
against a scratch repository, `GET /issues/{n}` → record `updated_at` → `POST /issues/{n}/assignees`
→ `GET /issues/{n}` again. **Test the mutation paths separately** — `POST /assignees`,
`DELETE /assignees`, `PATCH /issues/{n}` with an `assignees` field, and the GraphQL
`addAssigneesToAssignable` mutation — because there is no guarantee they share a code path and the
REST assignees endpoints are documented as a distinct sub-resource. **A positive result is still not
sufficient**: R12 stands regardless, because it is about the listing and not the timestamp.

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

**R2 — the `lf:tools=` silent refusal becomes far more reachable. Largely already handled; downgraded
in run 0003.** `[V]` When the trigger comment id is 0, `Daemon.refuseTools` refuses the run, logs it
and **posts nothing**, because there is no comment to answer (`internal/daemon/tick.go:1487-1533`,
the body branch at `:1505-1511`). Today the branch is reachable only on a first sight of an assigned
issue. Under R-body′ it is reachable on any tick, for any assigned issue carrying a malformed or
unknown `lf:tools=` label.

**Run 0003 correction: this branch is not silent.** It logs at **`InfoContext`**
(`internal/daemon/tick.go:1506-1509`) `[V]`, which is the default level, and its line already says
what happened and why nothing was posted. So the owner *can* find it in `daemon.log` today. The
residual risk is only that nothing appears on the *thread* — and under SD-3′ nothing is supposed to.
**Nothing needs to change here.**

What *does* remain is a volume observation, recorded for Plan rather than as a risk to mitigate:
under R-body′ this Info line becomes reachable on every tick for as long as a mis-typed label sits in
place, where today it fires once. Unlike an empty body, a mis-typed label is a state the owner may
leave alone for a while. If any per-tick log-volume guard is ever wanted, this is the branch that
needs one. Run 0003 does not recommend building one now — `daemon.log` is unrotated
(`internal/launchd/plist.go:58-62`) but one line per tick per mislabelled issue is not what fills a
disk. **The run-0002 version of this paragraph made SD-3's once-only mechanism the answer here; that
mechanism no longer exists and is no longer needed.**

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

**~~R11 — the SD-2 × SD-3 comment loop.~~ RETIRED in run 0003. It does not exist.**

Run 0002 raised R11 as its most serious finding, and it was a correct and careful piece of work
**under the premise it was given**. The premise was that SD-3 meant litefactory posting a refusal
*comment on the issue thread*. The owner has corrected that: he meant the log. `[O3]`

**Why the hazard evaporates, stated properly rather than deleted.** R11 was a *loop*, and a loop
needs the output to feed back into the input. That is exactly what a posted comment did:

- litefactory posts a refusal comment →
- the comment lands on the issue →
- the next tick's body gate reads `len(comments)` (`internal/daemon/tick.go:1081`) `[V]` →
- the gate's decision changes because of litefactory's own output.

**A log line has no such path back.** `daemon.log` is a file under `$LF_HOME/logs`
(`internal/config/paths.go:75-77`) `[V]`; nothing in `internal/daemon` reads it, nothing in
`internal/store` records it, and it is not an input to any gate, any classification or any listing.
The daemon's notion of "existing comments" comes from `ListCommentsAfter` against the forge and from
the local `comments` table — neither of which a log line touches. Writing to the log therefore
**cannot** change what the next tick decides. Both rows of run 0002's loop table are void:

| Issue authored by | Under SD-3 (comment) | Under SD-3′ (log line) |
|---|---|---|
| someone else | refusal comment silently and permanently closes the body path | **nothing changes.** The gate sees the same comments it saw before |
| the owner (SD-2 relaxed) | refuses and posts again, every tick, forever | **an Info line repeats while the condition persists.** Repetitive, not a loop, and bounded by how long the owner leaves the issue in that state |

**Note the residual, honestly:** SD-3′ does still produce a *repeating* log line on an owner-authored
issue whose body refuses on every tick under R-body′. That is R2's volume observation, it is a
tidiness matter in an unrotated log, and it is categorically not R11 — nothing feeds back, no
behaviour changes, and no third party sees it.

**What R11's retirement takes with it:** the once-per-issue durable state, the `0003_*.sql`
migration, the `report.ReplyKind` and `replyKind()` work, the comment-less reply path, and the
"do not ship C without its once-only mechanism" sequencing constraint. All of it existed to contain
R11. **See SD-5 for the owner's own answer to this question, which was correct on its own terms and
is now moot for the same reason.**

**R12 — no discovery path is guaranteed to notice a late assignment. New in run 0002; strengthened
in run 0003 and now the load-bearing risk of the whole brief.** `[V]` `[X]` Open question 1 asked
whether `updated_at` moves on assignment; even a yes would not settle whether discovery *notices*.
This repository has measured its own issue listing failing to surface a confirmed `updated_at` bump
for thirty minutes across sixty consecutive listings (`internal/github/comments.go:121-126`) `[V]`.
For comments there is a second, independently indexed listing that covers that gap by design
(`:124-130`) `[V]`. **An assignment produces no comment, so it has no second path.**

**Run 0003 adds three things and every one of them makes R12 worse, not better:**

1. **GitHub does not document the bump at all** `[X]`, so the first link in the chain is not merely
   unverified — it is uncontracted, and could change without a changelog entry.
2. **The obvious second path does not exist in usable form.** The repository-wide issue-events
   endpoint carries `assigned`/`unassigned` events but **has no `since` parameter and no documented
   ordering** `[X]`, so it cannot be cursored the way every other listing in this daemon is.
3. **No external evidence about the listing's reliability was found in either direction** `[X]`, so
   this repository's own measurement in `internal/github/comments.go:121-126` remains the only
   evidence anybody has — and it is negative.

**The consequence for Plan is now a recommendation rather than a weighing:** mechanism (c) —
keeping `needs_poll` set — is the only option that does not depend on a forge listing at all, and
run 0003 ranks it first. Run 0001's ranking is reversed; see *Open question 1, closed* and *Size and
shape*.

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
- A new forge endpoint. **Run 0003 hardens this from a conditional into a recommendation.** Run 0002
  left it conditional on the `updated_at` probe. Research has now established that the only
  repository-wide alternative — `GET /repos/{owner}/{repo}/issues/events` — **has no `since`
  parameter, no `sort` parameter and no documented ordering** `[X]`, so polling it means page-walking
  from page 1 every tick against a sort order GitHub has not committed to, with undocumented
  retention; and the per-issue timeline endpoint is one request per issue per tick, which is what
  AC-2.24 exists to avoid. Mechanism (c) reaches the same outcome with no forge surface at all.
  **Recommended out of scope.**
- **Posting anything on the issue thread from the body-refusal path.** Formerly the whole of Story C.
  Removed by the owner's correction of OQ-4 — see *SD-3 (superseded)* and *SD-3′*.
- **A new `sessions` column / migration `0003_*.sql`.** It existed only to contain R11, which is
  retired. Mechanism (b) could still want one, but (b) is not recommended.
- **Anything about other litefactory installations.** Raised by the owner as an aside `[O3]` and
  answered under SD-4: a second instance has its own owner identity and cannot see or be triggered by
  this one's. No design response.

---

## Open questions

### "Is everything answered now?" — the audit

The owner asked directly `[O3]`. **Answer: yes, with one qualification and no blockers.** Every
question this brief has ever raised is now either settled, dissolved, or is an ordinary engineering
choice that belongs to Plan rather than to him. **Nothing is waiting on him.**

Below is every open question as it stood after run 0002 — run 0002's list, not the manifest's
summary — plus run 0001's, with a verdict on each.

| # | Question | Raised | Verdict |
|---|---|---|---|
| OQ-1 | Does GitHub bump `updated_at` on an assignment change? | 0001 | **Closed by research, not by a yes/no.** GitHub documents nothing, either way `[X]`. More searching will not help. Recorded as an optional ten-minute empirical spike, needed only if Plan prefers mechanism (a). **Not blocking**, because run 0003 recommends (c), which does not depend on the answer |
| OQ-2 | Should the stampede be contained? | 0001 | **Settled — SD-1.** Accepted, not mitigated. `[O2]` |
| OQ-3 | Keep the "zero comments" clause? | 0001 | **Settled — SD-2.** Kept, relaxed when the owner authored the issue. `[O2]` |
| OQ-4 | Should refusals be made visible? | 0001 | **Settled — SD-3′.** Yes, **in the daemon log**. `[O2]` + `[O3]` correction. Run 0002's SD-3 (post on the thread) is superseded |
| OQ-5 | Assigned to the owner, or to anybody? | 0001 | **Settled — SD-4.** The owner specifically. `[O3]` **And it needs no code change** — the gate already does exactly this |
| OQ-6 | Which mechanism — (a), (b) or (c)? | 0001/0002 | **Open, and it is Plan's to decide, not the owner's.** Run 0003 recommends **(c)**, narrowed to plausible candidates, because it is the only option resting on no external contract. This is an engineering trade-off inside `internal/daemon`, and it is the one genuinely open design question left |
| OQ-7 | Bring `internal/daemon/doc.go` up to date? | 0001 | **Settled by recommendation: yes, as a task.** Nobody has objected and there is nothing to object to; the package spec does not mention the issue-body trigger at all (re-verified run 0002). Not a question for the owner |
| OQ-8 | Should the `lf: ask`-only body (branch 4) speak? | 0002 | **Closed by SD-3′.** Run 0002 deferred it because a *comment* there might confuse. That objection was entirely about posting and does not survive the correction — a log line for an obscure refusal is what a log is for. **Yes, it logs at Info** |
| — | R11, the SD-2 × SD-3 comment loop | 0002 | **Retired.** Not a question and not a risk. It required a posted comment; nothing is posted |
| — | The "problem 1" answer (bot account) | 0002 → 0003 | **Confirmed correct and now moot — SD-5.** `[O3]` The self-filter is real and enforced; there is simply no longer a comment for it to act on |
| — | The `maxleo` other-instance aside | 0003 | **Out of scope, and always was.** A different installation with a different owner identity. No effect on this instance and no design response needed. See SD-4 |

**Count: eight numbered open questions ever raised. Seven are closed. One (OQ-6) is open and belongs
to Plan.** Two run-0002 findings (R11, "problem 1") are retired rather than answered, because the
premise underneath both was withdrawn.

**The one qualification, stated plainly so it is not mistaken for a blocker.** OQ-1 has no yes-or-no
answer and never will have one from documentation — GitHub does not publish the behaviour. If Plan
chooses mechanism (a) it should spend ten minutes measuring it against a scratch repository first. If
Plan takes run 0003's recommendation and chooses (c), the question does not arise at all. Either way
**nothing here waits on the owner, and Plan is not blocked.**

### The one question that is still genuinely open

**OQ-6 — which discovery mechanism.** Run 0003 recommends **(c) `needs_poll`, narrowed to plausible
candidates**, on the grounds that (a) and (b) both depend on a chain of three things GitHub either
declines to document or this repository has measured failing (see R12). The narrowing — flag only
sessions that are open, have a non-empty body, and have no run at trigger 0 — is the piece of design
work Plan owes this story, because the naive form inverts the stated design point of the partial
index (`internal/store/migrations/0002_session_backlog.sql:42-45`). `[V]`

**This is an engineering decision, not an owner decision.** It does not change what litefactory does
from the owner's seat; it changes how reliably and how soon it does it.

---

## Recommended scope for Plan mode

**Rewritten in run 0003. Two stories, not three. No migration. No forge write.**

**Story A — the timing change (R-body′), with SD-3′ folded in.** Two parts, and the second can ship
first if Plan wants something in the owner's hands before OQ-6 is decided.

- **A1 — visible refusals (SD-3′).** Promote the issue-body refusals from `DebugContext` to
  `InfoContext`, so that a refusal on an issue the owner assigned to himself leaves a line he can
  find in `$LF_HOME/logs/daemon.log`. Today it leaves nothing anywhere: the three gate branches log
  at Debug (`internal/daemon/tick.go:1085-1087, :1104, :1113-1116`), the default level is `info`
  (`internal/config/config.go:256`, `internal/logging/logging.go:24-25, :38`), and no run and no run
  JSONL exists to fall back on, so `litefactory logs <issue>` prints `"<ref> has no runs yet"`
  (`internal/cli/cmd_logs.go:203-209`). `[V]` **Branch 1 (not assigned to the owner) stays at
  Debug** — it fires for most issues in the repository on most ticks — which means the single shared
  log call at `:1084-1088` has to split by which refusal fired. That split is the only design
  decision in A1. Branch 6 is **already** at Info (`:1505-1511`) and needs nothing. **Nothing is
  posted to the forge, so `TestABodyThatCommandsNothingStartsNothingAndPostsNothing`
  (`internal/daemon/tick_test.go:696`) and `TestIssueBodyToolsRefusalPostsNothing`
  (`internal/daemon/tools_test.go:339`) keep passing unchanged.** The new test should follow
  `TestSelfDropIsLogged` (`internal/turn/classify_test.go:521-552`), which asserts a line's text
  *and* its `"level":"INFO"` against a handler at `slog.LevelInfo` — the level is the point, not an
  incidental. Roughly a dozen lines.
- **A2 — the durable state test.** Turn the issue-body gate from a first-sight moment into a durable
  test — "open ∧ assigned to the owner ∧ non-empty body ∧ no comments ∧ no run yet at trigger 0" —
  evaluated on every tick that sees the issue, with once-ever enforced by
  `UNIQUE (session_id, trigger_comment_id)`, which already enforces it. Closes workflows 2 and 4 for
  issues with no comments, leaves workflows 1 and 3 untouched. **Decide OQ-6 first** — see below.

**Story B — the SD-2 author exemption.** Add the disjunct: existing comments stop blocking when the
issue's author id equals the stored owner id. Small in code, and its whole cost is in the argument:
the AC-2.16d amendment entry, the R4 doc comment at `internal/harness/prompt.go:247-256`, and saying
out loud in `internal/daemon/doc.go` that two issues that look identical can behave differently
according to who opened them.

**~~Story C — SD-3, visible refusals.~~ Dissolved.** It was sized as "the largest of the three" on
the premise that litefactory would post a comment. It does not. It is now A1, and everything that
made it large — the `report.ReplyKind` and its `replyKind()` case, a posting path not keyed on a
comment id, the `0003_*.sql` migration, the once-only state, the "post first, then commit"
ordering — is **not in scope**. Plan should not carry any of it forward. See *SD-3 (superseded)* for
what was proposed and why it was dropped.

**Decide OQ-6 before A2, and run 0003 recommends mechanism (c).** The recommendation rests on R12:
mechanisms (a) and (b) both depend on the forge issue listing re-surfacing an issue after a late
assignment, which needs an `updated_at` bump **GitHub does not document** `[X]`, delivered by a
listing **this repository has measured lagging thirty minutes** (`internal/github/comments.go:121-126`)
`[V]`, with **no second listing** to cover it — and the obvious alternative, the repository-wide
issue-events feed, **has no `since` parameter and no documented ordering** `[X]` and so cannot be
cursored. Mechanism (c) depends on none of that. **Plan owes the story the narrowing** — flag only
sessions that are open, have a non-empty body and have no run at trigger 0 — because the naive form
inverts the partial index's stated design point
(`internal/store/migrations/0002_session_backlog.sql:42-45`) `[V]`.

**If Plan prefers (a) anyway, spike it first.** Ten minutes against a scratch repository: read
`updated_at`, change the assignees, read it again — testing `POST /assignees`, `DELETE /assignees`,
`PATCH /issues/{n}` and the GraphQL mutation separately, since nothing guarantees they share a code
path. Do not answer it from memory and do not cite this brief as having answered it. **A positive
result is still not sufficient**, because R12 is about the listing and not the timestamp.

**Do not scope a stampede control.** SD-1: the owner has accepted the exposure and given his
reasoning. R1's containment options are recorded as *not wanted*. Do document the accepted exposure
in a comment on the gate.

**Do not scope any change for SD-4.** "Assigned to the owner, and to nobody else" is what
`internal/daemon/tick.go:1077` already does, via `Issue.AssignedTo`'s numeric-id comparison
(`internal/github/issues.go:94-104`) `[V]`. No config key, no allowlist, no "assigned to anybody"
mode. `TestANonOwnerAssigneeDoesNotFireTheBody` (`internal/daemon/tick_test.go:774`) already pins it
and must keep passing.

**Do not scope:**

- an authorship-based **trigger** — AC-2.16a stands, and SD-2 is an exemption from AC-2.16d rather
  than a trigger, which is a distinction worth carrying into the plan verbatim (see *SD-2 and
  AC-2.16a: not actually a conflict*);
- anything that re-implements "wait for assignee change" — it already exists;
- webhooks, issue reactions, or a run-rate limiter (D-X stands);
- **any forge write on the issue-body refusal path** — SD-3′ posts nothing, and
  `internal/daemon/tools.go`'s "the only branch to change if that trade is revisited"
  (`internal/daemon/tick.go:1480-1486`) `[V]` is **not** being revisited;
- a change to "assigned to anybody" — settled as SD-4, and settled the other way;
- a log-rotation or log-volume mechanism. `daemon.log` is unrotated by accepted M0 debt
  (`internal/launchd/plist.go:58-62`) `[V]` and A1 does not materially change that. Recorded under
  R2 as an observation, not a task.

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
| `TestABodyThatCommandsNothingStartsNothingAndPostsNothing` | `:696` | **run 0003: keeps passing unchanged.** Under SD-3 this was going to be falsified; under SD-3′ "posts nothing" is still true, because nothing is posted. Only a log-level assertion is added beside it |
| `TestIssueBodyToolsRefusalPostsNothing` | `internal/daemon/tools_test.go:339` | **run 0003: keeps passing unchanged**, same reason. Branch 6 already logs at Info and posts nothing; neither fact changes |
| **new** — a log-level assertion for the refusal lines | new, modelled on `internal/turn/classify_test.go:521-552` | **added in run 0003.** SD-3′'s whole point is the *level*, so the test must assert `"level":"INFO"` against a handler at `slog.LevelInfo`, not merely that a line was emitted. `TestSelfDropIsLogged` is the in-tree template and says so in its own comment: "a line only visible under debug is not a line the owner can find" |

**Expect these doc comments to need rewriting** — under this codebase's rules that is part of the
change, not cleanup (`.aiteam/APP_CONTEXT.md:322-328`). Re-pinned in run 0002:

- `internal/daemon/tick.go:1018-1073` — the gate's four conditions, of which condition 4
  (`:1038-1041`) becomes conditional under SD-2;
- `internal/daemon/tick.go:1043-1051` — "Watermark first, then run", the exactly-once argument that
  R-body′ falsifies;
- `internal/daemon/tick.go:1480-1486` and `:1505-1511` — the body-path refusal trade. **Run 0003:
  under SD-3′ this needs *no* rewrite.** The trade it describes ("this is the only branch to change
  if that trade is revisited") is about *posting*, and posting is not being revisited. The one thing
  worth adding is that under R-body′ the branch becomes reachable on every tick rather than once;
- `internal/turn/execute.go:185-194` — the `MAX(0, 0)` watermark argument;
- `internal/turn/prepare.go:152-157` — where `bodyIsInstruction` is decided;
- `internal/harness/prompt.go:226-256`, **especially `:247-256`**, whose "not reachable today" claim
  becomes false under R-body′ and more so under SD-2 (R4);
- `internal/daemon/doc.go:38-41` — which does not mention the issue-body trigger at all, and under
  SD-2 also owes a reader the author-dependence;
- `.aiteam/AC_AMENDMENTS.md` — **two** entries now: AC-2.16b's "on first sight" wording, and
  AC-2.16d's unconditional "an issue that already has at least one comment does not fire its body".

---

## Assumptions taken in run 0003

Recorded because this is a headless run and nobody could be asked.

- **SD-3′ is read as "promote the existing refusal lines to `Info`", not "add a new log sink, a new
  file, or a CLI command to read `daemon.log`."** The owner said "the log", and litefactory has
  exactly one daemon log. Making it *readable through the CLI* would be a new command
  (`internal/cli/commands.go:9-20` is the whole surface, and `litefactory logs` reads run JSONL only
  — `internal/cli/cmd_logs.go:184-198`) `[V]`, which is a bigger and different request than the one
  he made. **If he actually wants `litefactory logs` to show daemon lines, that is a separate
  feature and this brief does not scope it.**
- **Branch 1 (not assigned to the owner) is assumed to be excluded from SD-3′.** He asked to see
  refusals; branch 1 is not a refusal of anything he asked for, and promoting it would put a line in
  the log for most issues in the repository on most ticks. This is run 0003's judgement, not his
  instruction, and it is the one place where "all of the refusal branches" was narrowed.
- **`InfoContext` is assumed to be the right level, not `WarnContext`.** The house precedent is
  Info — `internal/turn/classify.go:343-352` and `internal/daemon/tick.go:1506` `[V]` — and these
  are refusals by design, not malfunctions.
- **Issue #10's body is still assumed empty** `[O]`, taken from the run task description across all
  three runs and still never verified against the forge from this worktree.
- **The owner's "is everything answered now?" is read as a request for a verdict, not for more
  research.** Run 0003 answers it with a per-question audit and a count rather than a narrative.

## Assumptions taken in run 0002

Recorded because this is a headless run and nobody could be asked.

- ~~**SD-3's "here" means the GitHub issue thread**, not the daemon log.~~ **FALSIFIED by the owner
  in run 0003** `[O3]`: *"i ment the log actually, not a comment on git!"* Run 0002's own note said
  "if he meant the log, story C disappears entirely" — and it does. The reasoning that produced the
  wrong assumption was that a debug line already existed and so could not be what he meant; run 0003
  found that the debug line is invisible at the default log level, which is why the existing line
  did not satisfy him and why the corrected reading still requires a change. See *SD-3′*.
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
