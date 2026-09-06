# Amendments

Acceptance criteria, decisions and design rulings that a later piece of work
changed, reversed or settled.

A plan's AC table and its Settled Decisions are historical records and stay as
they were written. When later work contradicts one of them — or has to rule on
whether one of them reaches a thing it was written before anyone had thought of
— the amendment is recorded here, and the original document gets a one-line
pointer to this file under the table or the statement it concerns. The pointer
is the convention, not a rewrite: an old plan that has been edited into
agreement with the present is no longer evidence of anything.

The ledger admits four kinds of entry:

- **an amendment** — a criterion whose wording or meaning a later change
  altered;
- **a ruling** — a decision (`D-*`), a design rule or an invariant that had to
  be interpreted before work could proceed. The original text stays exactly as
  written; this file says what it was held to mean, and why;
- **a review** — a criterion examined and deliberately left alone, with the
  reason it survives written down, so that "nobody looked" and "somebody looked
  and it was fine" stay distinguishable;
- **a deferral** — something considered, not done, and not rejected either,
  recorded with what would have to be true to do it. Deferrals live in their own
  section at the bottom.

Append new entries at the bottom, newest last: the identifier (`AC`, `D`, `P`,
`SD`, …), where it was first stated, the date and the issue or plan that amended
it, what it used to say, what it says now, and why the change is safe. Six
months from now the argument should be readable here without reconstructing it.

**Status** is one short phrase saying what happened to the identifier —
*reversed in part*, *amended*, *ruled*, *reviewed, unchanged*, *deferred*.

An entry that **settles** a question rather than **amending** a criterion has
nothing that "used to say" and "now says": a ruling leaves the original text
standing. Those entries carry an ***Applies to*** line instead, naming what the
ruling reaches and — usually the more useful half — what it does not.

Work that had a GitHub issue names it. Work planned and executed on a branch
with no issue names the plan path instead, which is why some entries below say
`issue #6` and others name a directory under `.aiteam/`.

## One ledger, not two

A sibling `DECISION_AMENDMENTS.md` was proposed when this file was widened to
admit `D-*` rulings, and rejected. Splitting the ledger recreates precisely the
findability problem that killed the convention this file replaced. Two earlier
plans each specified a session-local `$session/AMENDMENTS.md` —
`comment-intent` Story 9 (`T-CI-42`, `AC-CI-9.1`/`9.2`) and
`fix-comments-pickup` Story 5 (`T-FC-27`, `AC-FC-5.1`) — and **neither file was
ever written**. `first-comment-pickup`'s `T-6.16` shipped this repo-root ledger
instead, for exactly that reason: a session-scoped file is unfindable by anyone
who does not already know the session existed, which is everyone who needs it.

A reader asking "what happened to AC-3.1" has one place to look. A reader asking
"what happened to D-X" must have the same one. Do not re-propose the split.

---

## AC-2.16 — an issue body may now start a run

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-2, AC-2 table)
- **Amended:** 2026-08-26, issue #6 (`first-comment-pickup`), plan
  `.aiteam/lf-6-first-comment-pickup/2026-08-26/first-comment-pickup/IMPLEMENTATION_PLAN.md`,
  User Story 3
- **Status:** reversed in part — replaced by AC-2.16a–d

### It used to say

> AC-2.16 — A task opened by someone else never auto-starts anything — only
> comments are ever acted on, never the issue body alone.

### It now says

> AC-2.16a *(replaces AC-2.16)* — An issue that is not assigned to the owner
> auto-starts nothing, however its body is worded — including a body the owner
> wrote himself.
>
> AC-2.16b — An open issue assigned to the owner, with a non-empty body and no
> comments, runs its body as the first instruction on first sight — exactly
> once, verified by ticking again.
>
> AC-2.16c — A body that is empty or whitespace, or that parses to a command
> which spawns nothing, starts nothing and posts nothing.
>
> AC-2.16d — An issue that already has at least one comment does not fire its
> body; the comments drain as they do today.

### Why the reversal is safe

The original ban was not about issue bodies as such. It was about
**authorisation**: anyone can open an issue, so a body carried no signal that
the owner wanted anything done. A comment did, because the daemon matches the
comment author's numeric id against the stored owner id (AC-2.15).

Issue #6 supplies the missing signal from a different field. The body runs only
when the issue is **assigned to the owner**, matched on the numeric GitHub user
id exactly as comment authors are (D-I) — never on a login. Assigning an issue
to yourself is the act of instruction; a stranger cannot do it on your behalf.
The gate is assignment, not authorship, so a run started from a stranger's body
is still owner-authorised, and the prompt keeps that body's `opened by X`
attribution so the model knows who wrote what it was handed.

AC-2.16a preserves the surviving half of the old rule verbatim in effect: an
unassigned issue starts nothing, no matter how its body reads, and that includes
a body the owner wrote himself. That is the half that was load-bearing.

The remaining guards are unchanged: closed issues are skipped (AC-2.22), the
watermark is planted before anything runs (AC-2.10b), the body's trigger id of
`0` under `UNIQUE (session_id, trigger_comment_id)` makes it fire once ever
(AC-2.23), and the classifier still refuses a bare body — the body path does not
go through it, because there is no comment to classify.

---

## D-X — a concurrency bound is not the run-rate circuit breaker D-X refused

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md:31`
  (Settled Decisions), restated at `:429` under Out of Scope, LF-11
- **Ruled:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 7 (AC-PAR-8.1). No GitHub issue — the work was planned and executed
  from the branch `lf/parallelization`
- **Status:** ruled — D-X stands, in its original wording, and does not reach
  `max_concurrent_runs`
- **Applies to:** the `max_concurrent_runs` key in `config.yaml`
  (`internal/config/config.go`) and the semaphore it sizes
  (`internal/daemon/pool.go`). It does **not** license a rate limiter, a spend
  ceiling, a `blocked` state or a `/retry` command; every one of those is still
  LF-11 and still out of scope.

D-X reads:

> D-X — **Rejected for M0:** run-rate circuit breaker. No rate limiting until
> LF-11/M1. No task or AC may reintroduce it.

The phrasing is deliberately absolute, so a feature that adds a number bounding
how many runs happen cannot simply be waved through. This is the ruling that
says why this one is not what D-X refused.

### The ruling

D-X rejected a limit on runs **per unit time**. `max_concurrent_runs` is a limit
on runs **at the same instant**. Different mechanism, different purpose, and the
difference is not a technicality.

- A **run-rate limiter** counts runs against a clock and refuses the next one
  inside a window. What it bounds is *spend*: it exists because every run costs
  money and a loop of them costs a lot of it quickly. Under it, work that is
  refused is refused *for a period of time* — the daemon is deliberately doing
  less than it could, and something has to tell the owner so. That is why D-X
  sits inside LF-11, the loud-failure story, next to `blocked`, `/retry` and
  `/reset`: a spending control needs a state machine and a voice.
- **`max_concurrent_runs`** counts runs that are *executing* and refuses the
  next one until one of them finishes. What it bounds is the machine — cores,
  RAM, and how many `claude` processes the operator's plan tolerates at once.
  It refuses nothing over time. A session that finds no free slot keeps its
  `sessions.needs_poll` flag and is reconsidered from scratch, against fresh
  state, by the next tick (`Daemon.offer`, `internal/daemon/tick.go`). There is
  no queue and no dropped work: total work done over an hour is unchanged, and
  only how much of it is in flight at one instant is bounded.

The test that separates them cleanly: **set the cap to 1 and litefactory does
exactly what M0 shipped** — one turn at a time, nothing refused, nothing
deferred that M0 would have run. A run-rate limiter at its lowest setting still
refuses work M0 would have done. A concurrency bound at its lowest setting *is*
M0.

D-X's prohibition is therefore not reached. D-X is not amended by this work, and
no rate limiting arrives with it.

### The record correction: D-X was never superseded

The record said otherwise. The record was wrong, and finding that out was part
of the ruling, so it is written down here rather than left for the next person
to rediscover.

`comment-intent/IMPLEMENTATION_PLAN.superseded.md:19` states:

> D-CI-3 | D-X (no rate limiting until LF-11) is superseded. This feature ships
> a cost bound. *(consequence of D-CI-2)*

and `:29` says the amendment "is recorded in-tree by S9". Three things are true
about that claim:

1. **The document making it is itself superseded.** The 4-task replacement plan
   that actually shipped dropped the cost-bound thread entirely.
2. **That same document reverses itself**, at `:484`:
   *"OD-3 — RESOLVED: no ceilings enforced at ship. Against recommendation. D-X
   stands in effect."* The cost bound D-CI-3 was a consequence of was never
   enforced, so the supersession never had a subject.
3. **The in-tree record S9 promised was never written.**
   `comment-intent/$session/AMENDMENTS.md` does not exist, and `AC-CI-9.2` —
   "the same file records D-X superseded by D-CI-3" — was never satisfied. See
   "One ledger, not two" above; this is one of the two files that convention
   cost us.

So D-X was never superseded in effect and never superseded on the record. It
stands as first written, and this entry is the whole of what it now means.

---

## AC-FC-2.6 — scoped to the cursor lookback; `max_concurrent_runs` is a distinction, not a contradiction

- **First stated in:** `.aiteam/main/2026-08-25/fix-comments-pickup/IMPLEMENTATION_PLAN.md:289`
  (User Story 2, AC-FC-2 table)
- **Reviewed:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 7 (AC-PAR-8.2)
- **Status:** reviewed, unchanged — **AC-FC-2.6 is not amended by this work**
- **Applies to:** the forge-listing lookback margin, and nothing else. It does
  not state a house rule that tuning values are Go constants.

AC-FC-2.6 reads:

> AC-FC-2.6 — The margin is a Go constant with a documented rationale,
> overridable only through `daemon.Options` for tests. It is **not** a
> `config.yaml` key.

Read in place, it is one row of the AC-FC-2 table, every other row of which is
about the poll cursor's lookback window. Its subject is that margin. Its
*reason*, stated in the plan around it and carried into
`internal/config/config.go`'s struct comment, is that the margin is **a value no
operator should have to choose**: it defends against forge index lag, nobody
outside this codebase can observe that lag, and an operator handed the dial
would have no basis on which to turn it.

### The ruling

`max_concurrent_runs` fails that reason on every clause. The right value depends
on the operator's cores, on their RAM, and on how many concurrent `claude`
processes their plan tolerates — three facts litefactory cannot see and the
operator can. It is machine-shaped, so the operator **must** choose it. It is
therefore a `config.yaml` key, following the `poll_interval_seconds` precedent:
a number of exactly the same character, validated to a range (1..16), and
**refused, never clamped**, outside it.

A distinction, then, not a contradiction. AC-FC-2.6 and `max_concurrent_runs`
agree on the principle and differ on which side of it their subject falls. The
config struct comment already carries this argument at the point of use; this
entry is the ledger's copy of it, because the criterion it distinguishes from
lives in a different plan.

### The seam is reused, not departed from

AC-FC-2.6's shape — a documented value plus a `daemon.Options` field a test may
override, where the zero value means "use the configured one" — is the pattern
this feature followed rather than invented around. `daemon.Options.MaxConcurrentRuns`
is exactly that field: zero means `config.DefaultMaxConcurrentRuns`,
`Options.validate` refuses a non-zero value outside 1..16, and `newPool` floors
it again, so the number is refused or floored at three layers
(`internal/config/config.go`, `internal/daemon/daemon.go`,
`internal/daemon/pool.go`).

**One honest note about the criterion as shipped**, recorded rather than fixed
here because it predates this work: the margin ships as the package-level
constant `daemon.ListingOverlap` (`internal/daemon/tick.go`), not under the name
`CursorLookback` that AC-FC-2.6 and the config comment still use, and it carries
**no** `daemon.Options` override — tests read the exported constant instead. The
first clause of AC-FC-2.6 held; the second did not ship as written. The ruling
above rests on the criterion's *reasoning*, which is unaffected either way.

---

## The goroutine inventory — four fixed, plus at most `max_concurrent_runs` turn workers

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md:60`
  (Global Invariants, *"Goroutine inventory is exactly four"*)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 1 (RD-10) and User Story 7 (AC-PAR-8.3)
- **Status:** amended — the count changes and one of the four named goroutines
  never existed
- **Applies to:** the whole tree. **This entry is the canonical text of the
  goroutine inventory.** `lf-setup/IMPLEMENTATION_PLAN.md:60`,
  `.aiteam/APP_CONTEXT.md` and `internal/daemon/pool.go` all point at it or
  restate it; when they disagree, this is what they are supposed to say.

### It used to say

> **Goroutine inventory is exactly four:** tick loop, harness stdout reader,
> harness stderr reader, `logs -f` tailer. A fifth requires justification in
> review.

### It now says

**How many.** Four fixed goroutines:

| Where | How many | What it is |
|---|---|---|
| `internal/harness/runner.go` | exactly two | one per pipe of the `claude` child; neither can outlive `Run`, which drains both to EOF before `Wait` |
| `internal/daemon/daemon.go` | one | the tick loop |
| `cmd/litefactory/main.go` | one | the second-signal watcher, retired by the root context's cancel on the normal path |

— **plus at most `max_concurrent_runs` turn workers**, dispatched from
`Daemon.offer` (`internal/daemon/tick.go`) through `pool.try`
(`internal/daemon/pool.go`). That setting is validated to
`MinConcurrentRuns..MaxConcurrentRuns` (1..16) and **refused, never clamped**,
outside that range, so the hard ceiling on the whole process is **twenty**.

**Termination condition, as one named thing:** `Daemon.Quiesce`. Every worker
ends on one of three bounded conditions — its comment list is exhausted, a
comment was left unconsumed, or the context is done — and all three are joined
there, and at shutdown by the unexported `pool.wait` underneath it.

**What happens when the bound is exceeded:** it cannot be. The semaphore is a
buffered channel made once in `New`, with exactly one send site (the dispatch
loop, on the Tick goroutine) and exactly one receive site (the worker's defer).
The send is a `select` with a `default`, so a full pool refuses a dispatch
rather than parking discovery behind a turn.

**What the goroutines buy:** the wall-clock of a turn minus its git critical
sections, overlapped across sessions. A thirty-minute run stops delaying
discovery by thirty minutes.

A goroutine outside this inventory still requires justification in review, and
the form that justification takes is the four terms above: how many, what stops
them, what happens at the bound, and what they buy.

### The pre-existing record bug, corrected in the same pass

The old inventory named a **`logs -f` tailer**. There is no such goroutine and,
on the evidence of the tree, there never was: `litefactory logs -f`
(`internal/cli/cmd_logs.go`) follows a run by re-reading its JSONL on a 250 ms
interval, synchronously, on the command's own goroutine. The fourth fixed
goroutine is `main`'s second-signal watcher, which the M0 text omitted.

`.aiteam/APP_CONTEXT.md` had this right and `lf-setup:60` had it wrong; the two
documents have disagreed since M0 and nobody noticed, because the count happened
to come out the same. Both now defer to this entry.

### One goroutine deliberately not in the inventory

`pool.quiesce` (`internal/daemon/pool.go`) starts one goroutine to turn the
`WaitGroup` wait into a channel a `select` can read. It is excluded on purpose,
and the exclusion is a judgement rather than an oversight: it is not the
daemon's. It lives for the duration of one `quiesce` call, holds nothing,
decides nothing, and ends when the pool drains — which the shutdown join makes
certain happens. Production shutdown goes through `pool.wait` and never starts
it. `internal/daemon/pool.go` says the same thing at the call site; if these two
ever diverge, the code is right and this is stale.

### Why the change is safe

The invariant's purpose was never the number four. It was that every goroutine
in litefactory has a named owner, a named thing that stops it, and a stated
reason to exist — so that a leak is a review failure rather than a slow
discovery in production. That purpose is strictly better served now than by the
old count, because the old count was decorative (nothing enforced it, and one of
its four entries was fictional) whereas the new bound is a channel capacity that
the runtime enforces on every dispatch, re-validated at three layers, and joined
by a single named function.

---

## AC-2.8 — a tick's API cost is no longer one listing per repository

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-2, AC-2 table)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 7 (RD-12, AC-PAR-8.3)
- **Status:** amended — and this amendment pays off a debt that
  `fix-comments-pickup` Story 5 specified (`T-FC-27`, `AC-FC-5.1`) and never
  wrote

### It used to say

> AC-2.8 — Each tick issues one bounded, logged query **per registered repo**;
> PR entries are filtered out; tick API cost is O(registered repos).

### It now says

> AC-2.8 *(amended)* — Each tick issues **two** bounded, logged listings per
> registered repository — issues updated since the window, and issues commented
> on since the window — plus one issue read for each number the second listing
> returns that the first did not, plus **at most `max_concurrent_runs`** further
> issue reads for sessions reached only through the local backlog. Pull requests
> are filtered out of the first listing on the payload and out of the second at
> the issue read, which is where a pull-request number is found out. The tick's
> *listing* cost remains O(registered repos); its *request* cost is bounded above
> by `(repos × 2) + (issues with new comments) + max_concurrent_runs`. The local
> backlog union costs no API quota at all — it is one indexed query over
> `sessions.needs_poll`.

### Why the change is safe

**AC-2.8 was already false before this change, and had been for months.**
`Daemon.commentedIssues` (`internal/daemon/tick.go`) added the second listing per
repository plus a `GetIssue` for every issue reachable only through it — its own
doc comment says so, in as many words: *"This is the second listing, not the
safety net."* It was added because the primary listing keys off the issue's
update time and a new comment does not always move it, which is the incident
`fix-comments-pickup` existed to fix. Nobody recorded the amendment, so the
criterion has been quietly wrong in the plan ever since.

This change makes it more wrong in one further way, and the additional cost is
what needed bounding. Re-discovery of outstanding work moved off the poll cursor
and onto `sessions.needs_poll` (AC-PAR-5.1, 5.2), so discovery now unions a third
source into each tick. A backlogged session arrives without its `github.Issue`
and **stays that way**: the fetch happens in `Daemon.materialise`, only after
`Daemon.offer` has already taken a pool slot for it. That placement is not
incidental — it is what makes the bound exact. However deep the backlog grows,
a tick cannot make more than `max_concurrent_runs` of those reads, because it
cannot hold more than `max_concurrent_runs` slots.

What survives untouched is the reason the criterion was written: the tick's cost
must be a function of how many repositories are registered, not of how much work
is outstanding, so that a busy fleet does not spiral. It still is. The two extra
terms are both bounded — one by the number of issues that genuinely have new
comments, one by the pool size — and neither grows with the backlog.

**AC-2.9 is not amended.** See the reviewed-unchanged entry at the bottom of this
file for why, recorded there rather than omitted, because "we checked it and it
holds" is worth exactly as much as an amendment to the next reader.

---

## AC-2.1, AC-2.21 and D-B — "the in-flight run" is now "every in-flight run"

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (D-B in Settled Decisions; AC-2.1 and AC-2.21 in the AC-2 table)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 7 (AC-PAR-8.3)
- **Status:** amended in wording — the guarantee is unchanged, its arity is not

### It used to say

> AC-2.1 — `daemon run` runs in the foreground, logs to stdout and `daemon.log`,
> and exits cleanly on SIGTERM/SIGINT **within 3 seconds**, after abandoning any
> in-flight run per D-B.
>
> AC-2.21 — On daemon start, an orphaned `running` run is reconciled to
> `failed (abandoned)`, a failure comment is posted, and the watermark advances —
> no re-run, and the unique constraint is not violated.
>
> D-B — SIGTERM cancels the run context; daemon exits in under 3s. On next start,
> reconcile any `runs` row still `running` → `failed (abandoned)`, clear stale
> `sessions.lock_owner`, post the normal failure comment, advance the watermark.
> No re-run.

### It now says

The same three sentences, with every singular made plural: **runs**, **rows**,
**comments**, **watermarks**. Concretely:

> AC-2.1 *(amended)* — … exits cleanly on SIGTERM/SIGINT **within 3 seconds**,
> after abandoning **every** in-flight run per D-B. The daemon first joins the
> turns that can finish inside the shutdown grace and abandons only what is left.
>
> AC-2.21 *(amended)* — On daemon start, **every** orphaned `running` run is
> reconciled to `failed (abandoned)`, a failure comment is posted **on each
> affected issue**, and **each** watermark advances — no re-run, and the unique
> constraint is not violated.
>
> D-B *(amended)* — … On next start, reconcile **every** `runs` row still
> `running`, clear **every** stale `sessions.lock_owner`, post the normal failure
> comment on each, advance each watermark.

### Why the change is safe

Nothing in the mechanism moved. `store.DB.ReconcileOrphans` was already written
over sets — it returns a `store.Reconciliation` carrying slices, and
`turn.Reconcile` already loops over them. The store has been plural since
`0001_init.sql`; it was only the prose that was singular, written at a time when
one was the only number the daemon could produce.

The three-second bound survives, and by a mechanism worth stating because it
looks like it should not. `loop` registers `defer d.pool.wait()` first, so the
join runs before `loop` returns and before the wrapper closes `done` — which
means `done` says *"the loop returned **and** every worker finished"*, and
`Daemon.Run`'s pre-existing grace `select` covers N runs with zero lines changed.
The join itself is unbounded on purpose (`pool.wait` takes no context, and
`internal/daemon/pool.go` explains at length why giving it one would turn the
join into a no-op that still reads like a join). The bound lives one level up, in
`Run`'s `ShutdownGrace` timer — default two seconds, inside AC-2.1's three — after
which the process exits and leaves whatever is still running to the abandonment
path.

That is the whole argument for why this is safe rather than merely arithmetic:
**abandonment already had to work for N**, because `kill -9` produces exactly the
same state as an expired grace and always could. Making the daemon able to
abandon several runs at once does not add a failure mode; it adds callers to one
that was load-bearing from the first day.

---

## AC-2.12 — the backoff gates every dispatch, and clears on evidence rather than on completion

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-2, AC-2 table)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 4 (RD-9, AC-PAR-4.2/4.3/4.4) and User Story 7 (AC-PAR-8.3)
- **Status:** amended — three mechanisms under one unchanged promise

### It used to say

> AC-2.12 — 403/429 rate-limit responses trigger logged exponential backoff; the
> daemon does not crash, cursor and watermarks are preserved, and it resumes
> automatically.

### It now says

> AC-2.12 *(amended)* — 403/429 rate-limit responses trigger logged exponential
> backoff, armable from any goroutine. The hold is checked **twice per tick** —
> at the top of `Tick`, and again as the last statement before each dispatch —
> so a refusal met by a worker stops the tick that is still dispatching from
> starting more. Simultaneous refusals inside one standing hold count as **one**
> refusal, not several. The hold clears on **evidence of success** — the first
> forge listing of a tick that returns 200 — not on a tick completing. The daemon
> does not crash, no work is lost, and it resumes automatically. Turns already in
> flight are **not** cancelled: the backoff gates the dispatch of new runs and
> nothing else.

### Why the change is safe

Three things had to change and each of them was broken by concurrency in a
different way.

**Counting.** `failures` means *consecutive refusals*, and with one goroutine it
could only be incremented once per refusal. With N workers, five simultaneous
403s are one refusal that five callers found out about — and the old code would
have doubled the delay four extra times, turning a `Retry-After` of a minute into
sixteen. `backoff.fail` is now idempotent inside an active hold (`internal/daemon/backoff.go`):
inside the same mutex, a refusal arriving before `until` returns the standing
values and does not move the count. A refusal *after* the hold expires still
increments, which is what "consecutive" always meant — the forge is still saying
no after the window it named.

**Checking.** One check at the top of a tick was sufficient when the tick did the
work itself. It is not sufficient when the tick dispatches: a worker's 429 can arm
the hold while the dispatch loop is still running, and a tick that only checked at
the top would keep starting runs into a forge that is refusing them. The second
check sits in `Daemon.offer`, deliberately as the last statement before the `go`
rather than the first of the function — `materialise` above it may make a network
round trip, which is the widest window in the dispatch path for a hold to arm
inside. Placing the check at the top would leave exactly that window open and make
the invariant "no `go` unless the backoff was clear a round trip ago"; placing it
here makes it the plain one: **no `go` while a hold stands.**

**Clearing.** `backoff.clear()` used to fire when a tick completed. Under dispatch
a tick completing no longer implies no rate limit was hit — the workers are still
running. It now fires from `Daemon.discover`, on the first forge listing of the
tick that returns 200, which is direct evidence the forge is answering again. A
worker's later 429 re-arms it, correctly, as a new refusal. Worst case is one
extra dispatch window, which is cheaper than the alternative of never clearing.

**What did not change is the promise.** "Cursor and watermarks are preserved" held
because a rate-limited tick did no work. It now holds by a different route, and
this is the part worth reading twice: the poll cursor is a **pure monotonic
watermark** and advances on every tick that completes, whatever any worker went on
to do. What preserves the work is `sessions.needs_poll` — every session a tick
takes off the eligible set is flagged before anything can fail, dispatched or
deferred alike, and only a fully drained, error-free turn clears it. Comment
watermarks are untouched by any of this: one still advances only inside
`CompleteRunReport`, after its report has posted.

One accepted gap, recorded because it is real: a standing hold cannot self-clear
with **zero** repositories registered, since `clear` fires from a listing and
there are no listings to make. Assessed as benign — no repositories means no
traffic the backoff protects — and documented rather than fixed (CR-A-002).

---

## AC-2.13 — the unit of failure is a task's turn, not a tick

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-2, AC-2 table)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 7 (AC-PAR-8.3)
- **Status:** amended — the promise splits in two, because the failure did

### It used to say

> AC-2.13 — A transport error on a tick leaves cursor and watermarks unchanged;
> the next tick redoes the work.

### It now says

> AC-2.13 *(amended)* — A transport error during **discovery** ends the tick: the
> poll cursor is not persisted, no watermark has moved, and the next tick asks for
> the same window again. A transport error inside a **dispatched turn** ends that
> turn and no other: its comment is left unconsumed, its session keeps its
> `needs_poll` flag, and the next tick reconsiders it — while every sibling turn
> runs to its end and the tick that dispatched them all completes normally.

### Why the change is safe

The old sentence had one subject because there was one thing that could fail. A
tick and the work it did were the same event, so "a transport error on a tick"
was unambiguous. They are two events now, and the criterion has to say which one
it is talking about or it says nothing.

Discovery is unchanged and still atomic in the way that matters. It runs to
completion on the Tick goroutine before anything is dispatched, and an error
returns before `SetPollCursor` — so there is nothing to undo, exactly as before.

What is new is the second half, and it is a deliberate improvement rather than a
concession. Under the old shape, one repository's failed comment listing ended the
tick for every repository. `Daemon.work` returns nothing to anybody, because the
tick that dispatched it is long gone, so a failure there is a log line and a row
in the database — and, structurally, cannot reach any other task. With two turns
in flight, one repository's bad minute is no longer a reason to stop the other.

"The next tick redoes the work" survives, by the same route as in AC-2.12: not
because the cursor was held back — it no longer is — but because
`sessions.needs_poll` is set before anything can fail and is cleared only by a
turn that fully drained without error, never from a `defer`. A `defer` would clear
it on error paths too, which is the stale-*false* this design exists to refuse.

---

## AC-2.17 — the 👀 no longer lands "within the same tick", and lands sooner

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-2, AC-2 table)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 7 (AC-PAR-8.3)
- **Status:** amended in wording — the guarantee it was written to give is
  strengthened

### It used to say

> AC-2.17 — A 👀 reaction is added to a newly-seen `instruction` comment **within
> the same tick**, before the run starts; reaction failure is logged and does not
> abort the turn.

### It now says

> AC-2.17 *(amended)* — A 👀 reaction is added to a newly-seen `instruction`
> comment **by the worker that will run it, before that run starts**. Reaction
> failure is logged and does not abort the turn.

### Why the change is safe

"Within the same tick" is now literally false and was never the point. Both the
reaction and the run happen inside `Daemon.work`, on a worker goroutine that by
construction outlives the tick that dispatched it — `Daemon.handleComment` calls
`acknowledge` and then `run`, in that order, which is where the ordering
guarantee actually lives and always did.

The phrase was a proxy for a latency promise: *the owner sees an acknowledgement
promptly, and always before anything starts happening in their repository.* Both
halves are better now than when it was written. The ordering half is unchanged and
still adjacent in one function. The latency half improves, because the thing that
used to delay an acknowledgement was another task's thirty-minute run holding up
the tick — which is the whole problem this change exists to remove. A comment on
issue #8 is now acknowledged while issue #7 is still running, rather than half an
hour later.

The failure half is untouched: `acknowledge` logs a warning and returns, and the
turn proceeds. It was never allowed to abort a turn and still is not.

---

## AC-3.1 — the per-task lock survives; its stated role does not

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-3, AC-3 table)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 5 (RD-3) and User Story 7 (AC-PAR-8.3)
- **Status:** amended — this is the most load-bearing entry in this run. The
  mechanism is unchanged and what it is *for* is not

### It used to say

> AC-3.1 — The per-task lock is acquired before preparation; if already held,
> the triggering comment is left unconsumed (no watermark advance) and retried
> on a later tick — no queue, no "busy" reply.

### It now says

> AC-3.1 *(amended)* — The per-task lock (`sessions.lock_owner`) is acquired
> before preparation and is the **cross-process** exclusion: what it prevents is
> a second daemon *process* touching the same worktree and the same `runs` row.
> Within one process, two turns on one session are prevented earlier and more
> widely, by the daemon's in-flight set. If the lock is nonetheless found held,
> the triggering comment is left unconsumed (no watermark advance) and retried on
> a later tick — no queue, no "busy" reply.

### Why the change is safe

Nothing about the lock moved. `AcquireSessionLock` is still
`UPDATE … WHERE id = ? AND lock_owner IS NULL`, still taken before preparation,
still released on every exit path. What changed is that it stopped being the
only thing doing the job, and a criterion that describes a mechanism as *the*
concurrency model becomes actively misleading the moment it is one of two.

The reason a second mechanism was needed is a gap the lock cannot close, and it
is worth stating precisely because it looks like an over-engineering. A comment's
watermark advances inside `CompleteRunReport`, i.e. **after** its report has
posted — "post first, then commit". If exclusion ended when `turn.Execute`
returned, the session would sit, for the width of a forge round trip, both
**unlocked and unconsumed** — which is exactly the state discovery reads as
eligible. The observable is not corruption: comment 2's 👀 and run could appear
before comment 1's report landed, comment 1 would be re-entered, and
`GetRunByTrigger` would catch it as a replay. Safe, and wrong — the thread reads
out of order and the log shows a replay that should never have happened.

So the intra-process exclusion is `d.inflight`, a check-and-insert on the Tick
goroutine at dispatch (`Daemon.offer`), released in the worker's deferred
`inflight.leave` after `d.work` returns — which is wider than `turn.Execute`
alone and covers the report.

Widening the store lock to span the report was considered and rejected: it would
have made `internal/turn` depend on a reporting step it must not know about, and
falsified the very AC-3.1 comment in `execute.go` that this entry amends.

**Two mechanisms, two purposes:**

| Mechanism | Scope | Protects against |
|---|---|---|
| `sessions.lock_owner` — durable, cross-process, unchanged | `turn.Execute` only | a second daemon *process* on the same worktree and `runs` row |
| the daemon's in-flight set — in-memory, new | dispatch → drain → last report → watermark advance | duplicate dispatch and thread ordering within this process |

One consequence worth knowing, because it inverts what a reader expects:
`DispositionLocked` is now reachable **only from a second process**. The
in-flight set is checked on the Tick goroutine, `Tick` is never concurrent with
itself, so two sibling workers can never both reach `AcquireSessionLock`. Inside
one daemon the store lock has become defence in depth against a dispatcher bug.
Meeting it in a log is a signal, not routine.

The "no queue" half is untouched and is now doing more work than before: a
session that finds no free slot is not enqueued anywhere. It keeps its
`needs_poll` flag and is reconsidered from scratch, against fresh fairness and
backoff state, by the next tick.

---

## AC-3.3 — no second `recover()`; the exposure is accepted and written down

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-3, AC-3 table)
- **Ruled:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 4 (RD-6) and User Story 7 (AC-PAR-8.3)
- **Status:** ruled — AC-3.3 reviewed, unchanged
- **Applies to:** the collision between AC-3.3's *"released on every exit path,
  including panic"* and the tree-wide invariant that the only `recover()` in
  litefactory is in `redact.Redactor.Apply`. It rules on whether a turn worker
  gets a second one. It does **not** license a `recover()` anywhere else, and it
  does not weaken AC-3.3.

### The question

A panic inside a goroutine cannot be recovered by anyone else's frame. Before
this change a panic in a turn unwound through `loop` — which does not recover —
and killed the daemon. After it, the same panic on a worker goroutine kills the
daemon just as dead, taking every sibling run with it. That looked like a new
reason to add a `recover()` in the worker, and APP_CONTEXT forbids a second one
**by name**. One of the two had to give, in writing.

### The ruling

**No second `recover()`.** The exposure is accepted and documented, which is what
this entry is.

Four things carried it:

1. **The exposure is not new — only its blast radius.** `loop` does not recover
   today and never did, so a panic in a turn has always killed the daemon. What
   changes is how many runs die with it, and every one of them dies into a path
   that already exists.
2. **The abandonment path already handles N, and must.** `ReconcileOrphans`
   fails every abandoned run, posts a failure comment on every affected issue and
   advances every watermark. `kill -9` produces the identical outcome and always
   could, so that path has to work regardless of anything decided here. A panic
   is a `kill -9` that announces itself.
3. **A worker `recover()` would have to decide what to do with the wreckage** —
   a half-written worktree, an open `runs` row, an unposted report — and the
   honest answer is "abandon it". Which is what the crash already does, more
   loudly, through code that is exercised on every unclean shutdown rather than
   only in the branch nobody tests.
4. **`defer` unwinding runs during a panic.** `ReleaseSessionLock` still fires, so
   AC-3.3's actual promise — the lock is released on every exit path including
   panic — holds without a `recover()`, exactly as it did before. AC-3.3's own
   comment in `internal/turn/execute.go` already contemplates a panic without
   recovery.

This also leaves intact the invariant that made the question worth asking: the
only `recover()` in litefactory is in `redact.Redactor.Apply`, where a panic
mid-redaction becomes an error so the post aborts and the watermark does not
advance (AC-4.9). That invariant is grep-able and is worth more than the crash
isolation a second one would buy.

**What an operator sees, stated plainly so nobody is surprised by it:** a panic
in one turn ends the daemon and abandons every run in flight. Each abandoned run
gets its failure comment on the next start. That is the accepted cost.

---

## AC-3.5, AC-3.8 and AC-3.9 — the git operations they name now contend, and are serialised per clone

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-3, AC-3 table)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 3 (AC-PAR-3.1/3.2) and User Story 7 (AC-PAR-8.3)
- **Status:** amended — each criterion's outcome is unchanged; each acquires a
  precondition it did not have

### They used to say

> AC-3.5 — `git fetch --prune` runs against the registered clone first and
> updates `last_fetch_at`; fetch failure aborts with a failure comment and no
> worktree side effects.
>
> AC-3.8 — Branch `lf/<issue>-<slug>` is created from `origin/<base_branch>`, and
> reused rather than recreated if it already exists locally or remotely.
>
> AC-3.9 — Worktree created at `<worktree_root>/task-<issue>` with parent dirs as
> needed; reused if valid; the turn fails loudly with no delete or overwrite if
> the path exists but is not a matching worktree.

### They now say

The same three, each qualified: **the fetch, the branch resolution and the
worktree add are performed while this turn holds the clone gate for the
registered clone it is working against.** They are one critical section
(`prepareClone`, `internal/turn/prepare.go`), taken as a single acquisition, and
a turn against a *different* registered clone is not delayed by it.

### Why the change is safe

These three criteria were written for a daemon that could only do one of them at
a time, so they never had to say anything about who else might be in the
repository. That silence became wrong: they all mutate state inside one `.git`
directory, and git does not serialise them for you.

The contention is measured rather than feared. At parallelism 4, with
litefactory's own argv: **`fetch` failed 75% of the time** whenever the remote's
refs had actually moved (`cannot lock ref`); **`worktree add` failed 0.75%**
fatally, on a sibling's half-written registry entry under `.git/worktrees`. Both
are loud, so a caller could in principle retry — but 75% is not a flake, it is
the normal case, and AC-3.5's own promise ("fetch failure aborts with a failure
comment") would have turned it into a stream of fatal-looking comments on
healthy repositories.

The fix is `internal/daemon`'s `cloneGate`, keyed on `store.Repo.Path`, consumed
by `internal/turn` only through the one-method `turn.CloneGate` interface it
declares. Two properties are worth knowing because they were both chosen against
the obvious alternative:

- It is a **1-buffered channel acquired with a `select` on `ctx.Done()`**, never
  a `sync.Mutex`. `Lock` is uninterruptible, and a waiter parked in one would
  blow the shutdown grace budget.
- A **nil gate is a startup error**, not a degraded no-op (`turn.Options.validate`).
  A silent no-op would look healthy for a long time and then start posting fatal
  git errors that read like flakes — which is precisely the failure mode the
  measurements above describe.

What is deliberately **not** inside the section is the `claude` run. That gap is
the entire source of surviving parallelism (AC-PAR-3.2), and
`internal/turn/gate_test.go` asserts it directly. Do not widen any critical
section "to be safe": widening one closes exactly the gap this feature exists to
open.

---

## AC-3.10 — the clone-untouched guarantee survives; its assertion form needed a scope

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-3, AC-3 table)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 7 (AC-PAR-8.3)
- **Status:** amended in wording — this is the product's headline test and the
  guarantee is unweakened

### It used to say

> AC-3.10 — After a full turn, the registered clone's HEAD, current branch and
> `git status --porcelain` are unchanged versus pre-turn, verified against a
> dirtied fixture using the LF-1 helper.

### It now says

> AC-3.10 *(amended)* — After a full turn, the registered clone's HEAD, current
> branch and `git status --porcelain` are unchanged versus pre-turn. "Versus
> pre-turn" is well defined only when the turn under observation is the only one
> touching that clone, so the assertion is made **against a clone no sibling turn
> is running on** — which is what the fixture provides. The guarantee itself is
> unconditional and holds under any amount of concurrency, because it is enforced
> by a type rather than by an observation: `git.Clone`'s method set has no
> `Commit`, `Checkout`, `Reset`, `Merge` or `Pull`, so mutating a registered clone
> is a compile error.

### Why the change is safe

The distinction is between a promise and the way it is checked, and only the
second one needed touching.

`gittest.AssertUnchanged` captures a snapshot before, runs the turn, captures
after, and diffs. That comparison means "this turn changed nothing" only if
nothing else was changing the clone in between — with a sibling turn fetching
into the same `.git` directory, a difference in `refs/remotes/*` would be a true
observation of a false conclusion. So the *test* is scoped to a clone with one
turn on it, which is also the honest thing for a test named "a full turn leaves
the clone untouched" to be doing.

The *guarantee* needs no such scope, and it is important that the two do not get
confused. It never rested on the test. It rests on `internal/git`'s surface
(`internal/git/surface_test.go` pins the method set both positively and
negatively) and on `Worktree` having no exported construction path — every
mutating operation lives on `Worktree`, and the only way to get one is
`Clone.WorktreeAdd`. Concurrency does not weaken a compile error.

What concurrency *did* introduce against the clone was contention, not mutation:
`fetch`, `worktree add` and `push` all take locks inside `.git` even though none
of them change the checkout. That is AC-3.5/3.8/3.9's entry above, and it is a
separate problem with a separate fix.

---

## AC-3.20 — abandonment became plural, and a run's kill sweep stays its own

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-3, AC-3 table)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 7 (AC-PAR-8.3)
- **Status:** amended in wording — the three reasons stay distinct and stay
  per-run

### It used to say

> AC-3.20 — A non-zero exit is captured and surfaced per S-9, never swallowed;
> timeout, abandonment and plain non-zero exit are recorded with distinct
> reasons.

### It now says

> AC-3.20 *(amended)* — A non-zero exit is captured and surfaced per S-9, never
> swallowed; timeout, abandonment and plain non-zero exit are recorded with
> distinct reasons, **per run**. Timeout remains a property of one run and its own
> `claude.timeout_seconds`. Abandonment is no longer a whole-daemon event: a
> shutdown that outruns the grace abandons every run still in flight, and each is
> recorded, reported and watermark-advanced separately on the next start.

### Why the change is safe

The three reasons were always attributes of a run rather than of the daemon; it
is only that with one run at a time the difference could not be observed. Making
it observable required no change to how any of them is attributed — `RunResult`
carries the reason, `internal/turn` writes it to that run's row, and no two
workers share either.

The reason worth writing down is the one that could have gone wrong and did not.
Timeout enforcement kills a process subtree, and a sweep that got its target
wrong would kill a sibling's `claude`. It cannot: `killSubtree`
(`internal/harness/kill.go`) walks `pgrep -P` **from this run's own pid** and
signals the pgids it finds. It does not match on process name, and each child is
`Setpgid` into its own group. That was designed for SD-1's grandchild-escape
problem, years before anything here was concurrent, and it happens to be exactly
the property concurrency needs. See the SD-1 line in the reviewed-unchanged entry
below.

---

## AC-3.22 — the credential is unchanged; the push is no longer trusted to its exit code

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-3, AC-3 table)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 3 (AC-PAR-3.4) and User Story 7 (AC-PAR-8.3)
- **Status:** amended — one clause added, none withdrawn

### It used to say

> AC-3.22 — The branch is pushed to `origin` over the ambient SSH credential —
> never the bot token.

### It now says

> AC-3.22 *(amended)* — The branch is pushed to `origin` over the ambient SSH
> credential — never the bot token. The push happens while this turn holds the
> clone gate, and **the recorded upstream is read back afterwards rather than
> inferred from the exit code**: a push that exits 0 without leaving
> `branch.<name>.remote` behind is reported as a failure
> (`git.ErrUpstreamNotRecorded`), not as a success.

### Why the change is safe

The security half — the ambient SSH credential, never the bot token — is exactly
as it was, and nothing in this change goes near it.

The added clause is a correctness fix that concurrency made mandatory and that is
independently right. Measured at parallelism 4 on git 2.50.1: 240 concurrent
`push --set-upstream` runs from one clone produced **0 non-zero exits** and put
all 240 branches on the remote, while 89 printed
`error: could not lock config file .git/config: File exists` and **60 finished
with no `branch.<name>.remote` entry at all**. git loses the race for the config
lock, skips the write, and exits 0 anyway.

That is the dangerous shape of failure: the objects are on the remote and the
bookkeeping is missing, so a caller that trusts the exit code reports a clean
push for a branch that has no upstream — and litefactory signs its name to what
it reports. `internal/git/push.go` now reads the upstream back and checks both
halves of it. The clone gate makes the race rare; the read-back is what makes the
report true when it happens anyway, and it costs one `git config` read.

---

## D-A — the per-repo listing is still primary, and no longer the only source

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (Settled Decisions)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 5 (RD-2) and User Story 7 (AC-PAR-8.3)
- **Status:** amended — the rejection stands, the exclusivity does not

### It used to say

> D-A — Issue **search** dropped. Primary is per-repo
> `GET /repos/{o}/{r}/issues?since=&state=all`. Filter entries where
> `pull_request != nil`. `since` is inclusive, so the boundary issue is re-seen
> each tick — the watermark absorbs it.

### It now says

> D-A *(amended)* — Issue **search** stays dropped, for the reasons it was
> dropped: its query string caps at 256 characters, it is eventually consistent,
> and it runs on a thirtieth of the quota. The per-repo listing remains
> **primary** and is one of **three** discovery sources unioned per tick: the
> issues listing, the comments listing (`Daemon.commentedIssues`), and the local
> `sessions.needs_poll` backlog, which costs no API quota at all. `since` is
> inclusive and each tick asks from `cursor − ListingOverlap`, so boundary issues
> are re-seen deliberately and repeatedly — the per-task watermark, a monotonic
> MAX over comment ids, absorbs them exactly as D-A said it would.

### Why the change is safe

Every clause of D-A that was a *decision* survives untouched. Search is still
rejected and the reasons still hold; the per-repo listing is still what the
daemon is built around; and the "re-listing is absorbed by the watermark"
property is not merely preserved but is now leant on much harder — it is what
makes a five-minute overlap window and a three-way union safe to run every thirty
seconds.

What changed is that D-A read, in 2026-08, as though the listing were the whole
of discovery. Two things have been added since. The comments listing arrived with
`fix-comments-pickup`, because a new comment does not reliably move an issue's
update time and a comment was lost for seven hours as a result. The backlog union
arrives with this change, because the poll cursor is now a pure monotonic
watermark: it no longer holds back to the oldest unfinished issue, so neither
timestamp-indexed listing can re-find an issue that has been busy for longer than
`ListingOverlap`. `sessions.needs_poll` is the local record that finds it
(AC-PAR-5.1, 5.2).

Why the hold-back had to go, since removing a safety net is the sort of thing a
future reader will want justified: it re-listed the oldest unfinished issue every
tick, and because the cursor is one global row it rewound discovery **for every
other repository at the same time**. One busy issue pinned the whole fleet. The
flag is per-session and pins nothing.

---

## P-3 — copying slices at struct boundaries is now an isolation requirement, not a hygiene habit

- **First stated in:** `.aiteam/main/2026-08-26/tool-permissions/IMPLEMENTATION_PLAN.md:107`
  (User Story 1, Architecture Notes)
- **Amended:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 7 (AC-PAR-8.3)
- **Status:** amended — the rule is identical, its justification is strictly
  stronger, and the consequence of breaking it changed category

### It used to say

> P-3 — **Copy the profile's slices at the resolution boundary**:
> `append([]string(nil), p.Tools...)`. The config value lives for the daemon's
> whole lifetime, and `prep.request` is mutated in place on the cold-rerun path,
> so one `append` with spare capacity downstream would corrupt the operator's
> profile for every later run in the process.

### It now says

> P-3 *(amended)* — Copy slices at struct boundaries, with
> `append([]string(nil), …)` or `slices.Clone` / `maps.Clone`. The original
> reason stands and is sequential: an `append` into spare capacity on the
> cold-rerun path would rewrite the operator's configuration for every later run
> in the process. **Under concurrent turns it is also the isolation between live
> sibling turns**: two workers legitimately hold requests built from the same
> configuration at the same instant, so a missed copy at one of these sites is a
> **data race**, not merely a stale value. Do not remove one of these copies on
> the grounds that nothing looked like it was reading the slice again.

### Why the change is safe

Nothing about the practice changes — the same eleven-odd call sites, the same
idiom. What changes is what happens when someone deletes one, and that is worth
recording because it changes how the rule should be reviewed.

Sequentially, a missing copy produced a deterministic wrong value: the operator's
configured tool list would grow, reproducibly, and a test could pin it. Under
concurrency the same omission is two goroutines writing one backing array, which
is a race — nondeterministic, invisible in a passing run, and caught only if
`-race` happens to observe the interleaving. `make test` runs with `-race` and the
suite exercises concurrent dispatch, so it *would* catch it; that is a reason to
keep the tests, not a reason to relax the rule.

The practical consequence for a reviewer: "this copy looks redundant, nothing
reads the slice again" was a weak argument before and is not an argument at all
now. The sites are listed in `.aiteam/APP_CONTEXT.md` under "Copy slices at
struct boundaries" — `harness/runner.go`, `harness/meta.go`, `turn/execute.go`,
`turn/report.go`, `daemon/daemon.go`, `daemon/tools.go`, `daemon/tick.go`.

---

## Reviewed, unchanged — AC-2.9, AC-3.2, D-7 and SD-1

- **Reviewed:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`,
  User Story 7 (AC-PAR-8.3, which explicitly permits recording why a criterion
  was left alone)
- **Status:** reviewed, unchanged
- **Applies to:** four identifiers that this change was expected to alter and
  did not. Each survives for a different reason, and each reason is given, so
  that "nobody looked" and "somebody looked and it holds" stay distinguishable.

**AC-2.9** — *"The `tasks_updated_since` cursor persists after each successful
tick, survives restart, and is initialised to 'now' at the first-ever tick."*
Untouched in mechanism and in wording. `InitPollCursor` still plants the cursor
conditionally in the store, and `listingWindow`'s zero-`LastTickAt` branch still
withholds the overlap from the first tick ever, so registering a busy repository
still does not replay its history. The cursor's *meaning* narrowed elsewhere — it
is a pure monotonic watermark now, with the hold-back gone (see D-A above) — but
nothing AC-2.9 asserts depends on the hold-back. "After each successful tick"
also stayed well defined: a tick succeeds when discovery and dispatch complete,
which is entirely the Tick goroutine's own business and observable the instant
`Tick` returns.

**AC-3.2** — *"A lock held by a non-current daemon instance id is reclaimed at
daemon start and the reclaim is logged."* Unchanged, and correct for a reason
that has to be checked rather than assumed, because the instance id is
per-*process* and all N workers share one. Reclaim excludes own-instance locks,
which is exactly right: a live sibling's lock must never be reclaimed mid-run,
and under one shared instance id it cannot be. The obvious-looking alternative —
giving the instance id a worker component so a wedged worker's lock could be
reclaimed — was considered and rejected (RD-7). It buys mid-life reclaim, which
is the one thing that would let two `claude` processes into one worktree, and it
costs the identity contract `ReconcileOrphans` and every log line depend on.
"Wedged forever" is also not reachable: every blocking call in a worker is
deadline-bounded from exec time — git commands (pinned by
`internal/git/exec_internal_test.go`), `claude` (`claude.timeout_seconds`,
validated 60..86400) and the forge client's own — and the only un-deadlined wait
is the clone gate, whose holder is bounded by all of the above.

**D-7** — *"SQLite via `modernc.org/sqlite` (pure Go, cgo-free). Driver name is
`sqlite`, not `sqlite3`."* Unchanged, and the single-connection pool
(`SetMaxOpenConns(1)`) stays. What changed is that it went from a simplification
to a load-bearing one: N turn workers now rely on `internal/store`'s promise that
concurrent callers **queue** rather than contend. It holds because the pool is a
genuine convoy point only if something long runs inside a transaction, and
nothing does — no transaction spans `claude`, git or the forge, and the longest
hot-path transaction is `CompleteRunReport` at three statements. Raising
`SetMaxOpenConns` is not the fix for a contention problem here; if one ever
appears, the transaction that grew is.

**SD-1** — *"Enumerate all descendant pgids in the process subtree (recursive
`pgrep -P`), then `kill(-g)` each pgid, re-scanning once."* Unchanged, and
already concurrency-correct — one of the few places where a decision taken for an
unrelated reason turned out to be exactly what was needed. `killSubtree` walks
from **this run's own pid** and does not match on process name, and each child is
`Setpgid` into its own group, so a sibling turn's `claude` can never be swept.
The naive alternative SD-1 measured and rejected — a single `kill(-pgid)` —
would have been unsafe here too, for a second reason on top of the grandchild
escape it was rejected for. What needed restating rather than changing is SD-1's
*scope*: the sweep is per-run, so with N runs in flight there are N independent
sweeps, and "the process subtree" in SD-1's text means one run's subtree, never
the daemon's.

---

# Deferrals

Things considered during a piece of work, deliberately not done, and **not
rejected** either — recorded with the reason, and with what would have to be
true to do them. A deferral that is really a rejection says so; the two are not
the same and conflating them costs the next person a day.

## Deferred: a per-repo concurrency bound (`repo add --max-parallel N`)

- **Deferred:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md` (D2,
  AC-PAR-8.6)
- **Status:** deferred, not rejected

`max_concurrent_runs` is **one daemon-wide number shared by every registered
repository**. A per-repo bound — a repository that should never have more than
one turn against it, next to one that can take four — was asked about, judged
not needed yet, and left out. Nothing about the shipped design forecloses it,
and that is deliberate rather than lucky, so here is the seam.

**Where it goes.** The global bound is consulted **at dispatch**, not at
discovery and not when a session is created: `Daemon.offer`
(`internal/daemon/tick.go`) takes a slot from `d.pool.try()` as the third of
three guards, immediately before the `go`. `Daemon.dispatch` above it already
walks repositories in a round-robin, one candidate per repository per pass, and
already has `q.repo.ID` in hand at the moment it decides. A per-repo bound is
therefore **one extra counter checked beside the global slot take**: a
`map[int64]int` of in-flight turns per repository, incremented where `dispatch`
records `offerDispatched` and decremented in the worker's defer beside
`pool.done()`, with a repository at its bound `continue`-ing to the next
repository in the rotation rather than ending the pass the way a full pool does.

**Why that composes rather than fights.** Two properties have to hold for it,
and both do today:

1. **Every bound in this design is consulted at dispatch, and none of them
   mutates the configured value.** The plan's secondary-rate-limit response
   (RD-9 — after a secondary limit clears, cap the first dispatch window at one
   slot, then return to `max_concurrent_runs`) is specified in exactly that
   shape for exactly this reason. *It has not shipped* — `T-PAR-36` was not
   done, and `github.RateLimitError.Secondary` is currently logged and nothing
   more — but when it does, it is a ceiling read in the dispatch loop, not a
   write to the pool's size. Bounds that are all read in one place compose by
   `min`; bounds that mutate shared state do not compose at all.
2. **There is no queue.** A dispatch either starts now or the session keeps its
   `needs_poll` flag and is reconsidered from scratch next tick. Nothing has been
   committed to a repository in advance, so a new bound can change its mind every
   tick without anything having to be un-decided.

**What it would cost that this entry cannot pay for you:** a `repos` column and
a migration, a `repo add --max-parallel` flag, validation (does a per-repo bound
above `max_concurrent_runs` mean anything?), and a fairness question the current
round-robin does not have to answer — whether a repository at its own bound
should yield its rotation turn or hold it.

## Deferred: a repository component in `naming.WorktreePath`

- **Deferred:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md` (AC-PAR-4.8,
  AC-PAR-8.6)
- **Status:** deferred; the hazard it would fix is closed by refusal instead

`naming.WorktreePath(worktreeRoot, issue)` returns
`<worktree_root>/task-<issue>`. It carries the issue number and **not** the
repository, so two registered repositories sharing a `worktree_root` map their
respective issue #4 to one directory on disk. Sequentially that was a mess
waiting to happen; concurrently it is two `claude` processes in one working
tree.

The obvious fix — put owner/name into the path — was not taken. It is a
**migration** question, not a naming one: every worktree that already exists on
disk is at the old path, and changing the derivation relocates all of them.
`WorktreePath` is also pure, deterministic and recomputable from its inputs
after a database loss, which is a property worth more than the convenience of
fixing this in one line, and a repository component makes the input set larger.

What shipped instead is **refusal**: `internal/repos/collision.go`
(`ValidateWorktreeRoots`, `ValidateNewWorktreeRoot`) refuses two registered
repositories that would resolve one issue number to one worktree directory —
checked at daemon startup **and** at `repo add`, both before any row is written
(AC-PAR-4.8). That closes the hazard completely for any configuration
litefactory will accept, at the price of refusing a configuration that a
repo-qualified path would have allowed. `naming.WorktreePath` is unchanged.

If the deferral is ever taken up, the work is the migration, not the derivation.

## Deferred: the LF-11 in-flight view

- **Deferred:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md` (AC-PAR-8.6)
- **Status:** deferred — out of scope here, and the likely next request

There is no way to ask a running daemon what it is doing right now.
`litefactory status`, a `blocked` state, `/retry` and `/reset` are all LF-11 and
all still out of scope; the daemon logs a per-tick `in_flight` count and that is
the whole of the observability this change adds.

This is the request that will arrive next, because concurrency is what makes it
interesting: with one turn at a time, "what is running" was answerable by
reading the last log line.

## Rejected, not deferred: an owner column on `runs`

- **Rejected:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md` (RD-8,
  AC-PAR-8.6)
- **Status:** rejected — recorded here so it is not re-proposed as an oversight

Adding `runs.owner` (which daemon instance, or which worker, owns a `running`
row) was considered and refused.

Ownership is already **derivable**: `runs.session_id` → `sessions.lock_owner`,
for exactly as long as the lock is held — which is exactly as long as ownership
means anything. A `runs` row whose session is unlocked is not owned by anybody;
that is what `ReconcileOrphans` is for.

The only thing a real owner column would buy is **mid-life reclaim or a TTL** —
taking a run away from a worker that is presumed stuck — and RD-7 rules that
out. It is the one operation that could put two `claude` processes into one
worktree, "wedged forever" is not reachable in the first place (every blocking
call in a worker is deadline-bounded from exec time), and it would cost the
identity contract that `ReconcileOrphans` and every log line depend on.

The migration budget went to `sessions.needs_poll`, which is load-bearing: it is
the only thing that re-finds an issue once the poll cursor stopped holding back
for it. `UNIQUE (session_id, trigger_comment_id)` and `UNIQUE (session_id,
ordinal)` continue to do the real work of refusing a replay, which is the job an
owner column is usually reached for.

## Recorded: `Daemon.Quiesce` is the seam, and it is not test-only

- **Recorded:** 2026-08-31, plan
  `.aiteam/main/2026-08-30/parallelization-lf/IMPLEMENTATION_PLAN.md`
  (AC-PAR-8.6)
- **Status:** recorded — deliberately exported, deliberately singular

`Daemon.Quiesce(ctx)` is the **only exported API this feature adds**. Three
things about it belong in the deferral register rather than only at its
definition, because they are about what should happen *next* rather than about
what it does:

1. **It is not a test-only export.** It is used in production, in the shutdown
   join — which reaches it through the unexported `pool.wait` underneath it, not
   through `Quiesce` itself, and `internal/daemon/pool.go` explains at length why
   that path must not take a context. Removing `Quiesce` as "only tests call it"
   would be wrong on the facts and would take the join's caller-facing half with
   it.
2. **It is the natural seam for the deferred LF-11 view.** "Wait until this
   daemon is idle" and "tell me what this daemon is doing" are the same question
   asked at two levels of detail, and they should not have two exported ways to
   be asked. **Extend `Quiesce` rather than adding a second entry point.**
3. **Call it between ticks, not during one.** It is safe from any goroutine, but
   a dispatch starting at the same instant is a `WaitGroup` `Add` racing a
   `Wait`, which is a misuse the runtime is entitled to panic on.

The doc comments on `pool.wait`, `pool.quiesce` and `Daemon.Quiesce`
(`internal/daemon/pool.go`) make the mechanical case in full and are the
authority on it. This entry records only what they cannot: that the export was a
decision, and which future request it is waiting for.

## Amendment: SEC-7 override for Configurable Agent Teams — Owned Paths (interim)

- **Amendment:** 2026-09-06, plan
  `.aiteam/main/go-team/plan_brief.md` (US-1, T-34; sponsor-approved override of
  SEC-7)
- **Status:** amendment — sponsor-approved, scope expected to be refined by US-16

SEC-7 says litefactory MUST NOT write inside a registered clone. The
"Configurable Agent Teams" feature is a deliberate, sponsor-approved exception:
it composes and writes each harness's agent/skill definitions into the
registered clone itself, because that is where the harness (Claude Code, Codex,
opencode) reads them from at run time. There is no way to honor the letter of
SEC-7 and deliver this feature — the override is the point, not a gap.

**Owned Paths, as understood at this stage of the plan:**

- `.claude/agents/`
- `.codex/skills/`
- `.opencode/agents/`

These are the three harness-specific directories this feature writes to, inside
a registered clone, once teams are composed and materialized. This list is
deliberately narrow and deliberately provisional: it names only the
harness-facing output directories as they are understood at US-1 time, before
any bookkeeping/composition-tracking directory exists.

It is expected to be corrected — not merely extended — once US-16 lands its own
bookkeeping directory design (a `.litefactory/`-rooted location, and any
root-file composition targets that story introduces). Until then, treat this
entry as the floor of the override's scope, not its ceiling: any story that
needs to write somewhere else inside a registered clone must widen this list
explicitly, by its own amendment, rather than assume the override already
covers it.
## Recorded: DEV-1 resolution — `--level` is read twice on `daemon run`

- **Recorded:** 2026-09-05, plan
  `.aiteam/lf-89-logging-is-only-on-issues-but-not-on-the/2026-09-05/logging-is-only-on-issues-but-not-on-the/ARCH_REVIEW.md`
  (DEV-1; US-6, T-6.2–T-6.4, AC-6.4)
- **Status:** recorded — Option A implemented as approved

US-6 needs `litefactory daemon run --level debug` to affect which level the
process logs at. The catch is ordering: `cmd/litefactory/main.go` builds
`env.Logger` — and logs the first "dispatch" line — before `internal/cli`'s
`Router` ever parses a command's flags. By the time the router's normal flag
parsing would see `--level` on `daemon run`, the logger already exists at
whatever level `configuredLevel()` picked from `config.yaml`. There is no
single parse of argv that can both build the logger early and let the router's
usual `Command.New` / `flag.FlagSet` machinery own the flag.

**Resolution (Option A):** read `--level` in two places, not one.

1. `internal/cli.LogLevelOverride(args []string) (slog.Level, bool, error)`
   (`internal/cli/cmd_daemon.go`) scans raw argv by hand for `--level`/
   `-level` (bare-with-value or `=value` forms) — not with a
   `flag.FlagSet`, since `fs.Parse` stops at the first non-flag token, which
   for these commands is the command name itself, before it ever reaches
   `--level` — then parses the keyword with `logging.ParseLevel`, the
   project's one level parser (D-4), before the router exists.
   `cmd/litefactory/main.go`'s `run` calls
   it before opening the log sink or constructing the logger, and uses its
   result in place of `configuredLevel()` when present. An error here is a
   usage error: `main` prints it and returns `cli.ExitUsage` immediately,
   before the poll loop — or anything else — starts (AC-6.2). It never falls
   back to `configuredLevel()`'s silent default the way every other failure
   in `configuredLevel()` does.
2. `newDaemonRunCommand`'s `Command.New` also binds `--level` on its
   `flag.FlagSet`, via `daemonRunOptions.bind`, purely so `--help` and an
   unknown flag are reported correctly by the router. The handler closure
   ignores the bound value — main already resolved it and already built the
   logger by the time this handler runs.

Both sites carry a comment cross-referencing this entry and each other
(STYLE-1). `daemonStartCommand` binds `--level` a third time, for a different
reason and at a different layer: not because the logger needs it early, but
because `daemonProgram` needs a validated keyword to bake into the plist's
`ProgramArguments`, so `daemon start --level <garbage>` fails before an agent
is installed rather than at that agent's first tick. All three parses call
`logging.ParseLevel` and only that parser — D-3 still holds: `--level` here is
a write-time threshold, unrelated to `logs --level`'s display-time filter,
and no option type spans the two commands.
