# Implementation Plan — fix-comments-pickup
# Branch: main
# Date: 2026-08-25

Scope: close the hole that let two owner comments on `marioatcloudvane/litefactory#3` be swallowed
without a trace — a poll cursor that advances to the tick's start time on the strength of a GitHub
issue listing that may not yet reflect the update.

Inputs: `RESEARCH_BRIEF.md` (spike-promoted, 2026-08-25), a citation re-verification pass against
the tree at `acbe751`, and a survey for the same pattern elsewhere.

> **This plan does not pick the fix.** Tier 1 (a cursor lookback) and tier 2 (drain tracked tasks
> by watermark) are both planned out in full, as Story 2 and Story 3, so the trade-off can be read
> rather than guessed at. **OD-1 is the decision.** Stories 1, 4 and 5 ship whichever way it goes.

---

## The decision, in one table

| | **Tier 1** — cursor lookback | **Tier 2** — drain tracked tasks |
|---|---|---|
| What it does | Re-asks a fixed overlap window every tick | Stops the issue listing being load-bearing for tasks already tracked |
| Failure mode after | Lag **shorter than the margin** is survived; longer lag still swallows, silently | Lag on a **tracked** task cannot swallow anything, at any magnitude |
| Size | ~3 lines + one constant | New store accessor + a liveness definition + per-tick fan-out + dedupe |
| AC-2.8 cost | O(repos + issues updated within the lookback) | O(repos + *live* tracked tasks) — and needs an explicit ceiling |
| Hard limit it introduces | None | At a 30 s tick: ~40 live tasks saturates the 5,000/hr quota. At the 10 s floor (D-C): ~13. See S3 notes. |
| Ships | Today | After OD-2 is answered |
| Residual it does **not** close | first-sight bootstrap swallow (see "Second knife-edge") | same |

The brief's core argument, restated because it is the crux: one of the two misses was 1.2 s of
replica lag, the other **19 s**, and the 19 s could not be attributed from local evidence. The
magnitude of the lag is therefore **unknown and unbounded**. Tier 1 is a bet that it stays under
the margin. Tier 2 does not take the bet.

The counter-argument, stated as fairly: four successful pickups in the same run detected comments
11 s, 15 s, 21 s and 27 s after they were written, so the listing is usually prompt; tier 1 is
three lines; and tier 2 buys its guarantee with a per-tick fan-out whose ceiling is a new product
constraint that does not exist today.

**Sequencing cost, if you want tier 1 now and tier 2 later:** the two changes touch the same ~15
lines of `Tick`/`poll` and both restate AC-2.8, so the restatement is done twice and `tick.go:99`
is revisited once. That is the whole overlap. It is small. Doing tier 1 first is not a trap.

---

## Settled — do not re-open during implementation

| # | Fact |
|---|---|
| F-1 | The defect is `internal/daemon/tick.go:99-102`. **Verified, holds verbatim.** The cursor advances to `startedAt` with no overlap margin; the `pending` hold-back (`tick.go:88-91`, `:167-172`) only covers issues the listing *returned and then deferred*. |
| F-2 | Re-listing an already-drained issue is cheap and produces no work — `AdvanceWatermark` is `MAX`-based (`internal/store/watermarks.go:117-124`) and `ListCommentsAfter` filters `id <= afterID` client-side (`internal/github/comments.go:76`). |
| F-3 | Tier 3 (S4) and the regression test (S1) are independently valuable and ship regardless of OD-1. |
| F-4 | Nothing in this plan touches `internal/turn/classify.go`. The self-filter (S-8, D-I) is unchanged. |
| F-5 | The lookback, if it ships, is applied to the value **persisted** at the end of a tick, never to the value **read** at the top of one. See S2's architecture notes — this is what keeps AC-2.9 and `settle()` intact. |

---

## Corrections to the brief's citations

The plan will be read as ground truth by Implement mode, so every `file:line` carried forward was
re-checked. Three did not hold.

| Brief said | Reality | Claim itself |
|---|---|---|
| `tick.go:99` — cursor advances to `startedAt` | **holds** | true |
| `tick.go:225` — `bootstrap` | **holds** | true |
| `tick.go:274` — `drain` | **holds** | true |
| `tick.go:136` — `poll` | **holds** | true |
| `store/pollcursor.go:53`, `:83` | **holds** | true |
| `github/issues.go:57-64` — "the watermark absorbs the repeat" | brief cited `:60`; the sentence runs `:62-64` | true |
| `github/client.go:19` — "constants rather than configuration" | **holds verbatim** | true |
| `github/comments.go:60` — client-side `id <= afterID` filter | **wrong line**; the filter is `comments.go:76`, inside `ListCommentsAfter` (`comments.go:57`). `:60` is argument validation. | true |
| `turn/classify.go:207` — "the self-filter … records every comment it sees" | **wrong function.** `:207` is `Classify` (pure ordering). The self/authorship test is `selfReason` (`classify.go:225`); the thing that *records every comment* is `Admit` (`classify.go:268`), recording via `RecordComment` at `classify.go:278`. | true, but split across two functions |
| `tick.go:511` — `leave()`, the "stays unconsumed" primitive | **wrong line.** `:511` is `observe` (rate-limit backoff). `leave` is `tick.go:470-485`, doc at `:463-469`. | see below |

**One nuance the brief's `leave()` description misses, and it matters for S1's test design:**
`leave` does **not** stop the queue. For an ordinary failure it logs at Error and returns `nil`
(`tick.go:482-484`); only a `*github.RateLimitError` (`:474-476`) or a context error (`:477-481`)
propagate. The queue stop is detected *separately*, by `drain` re-reading the watermark from the
store (`tick.go:289-300`, helper `consumed` at `tick.go:312-318`). Assertions in S1 must therefore
be made against the **watermark read back from the store**, never against a return value — which
is also the in-tree convention (`drain`'s doc, `tick.go:264-266`).

---

## Second knife-edge, found by the survey (answers OQ-6)

The survey for "does anything else share this pattern" has an answer, and it is not "no".

**`bootstrap` (`internal/daemon/tick.go:225-251`) has the same shape.** It calls
`ListCommentsAfter(…, 0)`, takes the highest id it saw, and plants the watermark there — declaring
every comment at or below it consumed *without processing them* (AC-2.10). If that listing is
incomplete or stale, a `/spike` written moments before first sight is swallowed silently. Unlike
the poll cursor, it has **no deferral escape hatch at all**.

It is narrower: one-time, per task, at first sight only. But **neither tier 1 nor tier 2 closes
it**, because both operate after a task is tracked, and this fires at the moment it becomes
tracked. It is logged under Out of Scope with that reasoning rather than silently inherited.

Everything else that looks similar is clean, and is recorded here so the question is not re-asked:
`InitPollCursor` (`store/pollcursor.go:53`) skips history deliberately and is a guarded
read-then-insert in one transaction; the backoff (`daemon/backoff.go:18-20`) stores a resume
instant, not a data cursor, and is documented as leaving cursor and watermarks untouched;
`TouchRepoFetch` (`store/repos.go:155`) is display-only; `ReconcileOrphans` (`store/runs.go:439`)
trusts process liveness, not listing completeness, and is guarded by a fresh per-process instance
id. And `fetchPages` (`github/pagination.go:24-31`) already encodes the correct instinct — its doc
names this exact hazard ("would advance a watermark over unseen comments") and returns
`ErrUninitialized` rather than an empty slice, and fails loud on truncation (`:38-41`). The
asymmetry it leaves is the one that bit us: truncation is caught, a **complete-looking but stale**
listing is indistinguishable from a quiet repo.

---

## Global Invariants

Inherited from `lf-setup/IMPLEMENTATION_PLAN.md:35-70`. Violations are review blockers. The ones
this feature can actually break:

- **Every invariant in the brief's table survives, and each gets an explicit test or an explicit
  argument.** MAX-based watermark never rewinds; AC-2.10 bootstrap processes nothing on first
  sight; AC-2.11 ascending comment id order; AC-2.12/2.13 a failed tick persists no cursor;
  AC-3.1 a locked task consumes nothing; S-8/D-I self-filter untouched.
- **No new package.** This is a change inside `daemon`, `store` and (for S3) one new store method.
- **`daemon` is about time, `turn` is about one comment.** Nothing here moves that line.
- **No `context.Background()`; `log/slog` only; `go test -race` mandatory.**
- **Hand-written fakes only.** The existing `httptest` forge (`internal/daemon/fixture_test.go`)
  is the test surface; do not introduce a mocking library.
- **A failed tick must still persist no cursor.** Both tiers add work *before* `SetPollCursor`;
  the early return at `tick.go:94-97` must keep covering it.

---

## Build Order

| Wave | Story |
|---|---|
| 0 | **S1** — the failing regression test. First, always: it is the executable definition of the bug. |
| 1 | **S2** (tier 1) **and/or** **S3** (tier 2), per OD-1 |
| 2 | **S4** — tier 3, the near-miss WARN. Depends on nothing, but its threshold is only meaningful once a margin exists. |
| 3 | **S5** — amended criteria recorded in-tree |
| — | **S6** — recovery of the two lost comments. Gated on OD-5; not code; can happen at any point, or never. |

**File ownership.** S1 and S4's test owns `internal/daemon/tick_test.go` and
`internal/daemon/fixture_test.go`; S2 and S3 both own `internal/daemon/tick.go` and are therefore
**strictly serial with each other**. S3 additionally owns a new `internal/store/sessions.go`
accessor. S5 is documentation only.

---

## User Story 1 — The regression test that fails today

> As the owner, I want a test that reproduces a lagging issue listing swallowing a comment, so the
> defect is pinned by something executable before anything is changed to fix it.

Writes the test the brief names as missing and as the one that would have caught this. It must
fail against the tree as it stands, and that failure is the story's deliverable.

### Architecture Notes
- **Write it first and watch it fail.** A regression test authored alongside its fix routinely
  passes for the wrong reason. Land it red, in its own commit, with a skip or a documented
  expected-failure marker only if the suite must stay green in between — otherwise land it
  together with the chosen tier's fix in the same PR, red-then-green in two commits.
- **The mechanism already exists.** `forgeServer.onListIssues` (`fixture_test.go:131`) is called
  with the 1-based call number and reports whether it handled the response itself
  (`fixture_test.go:180-182`). A hook that writes `[]` for calls *N..M* is exactly "the listing
  lagged". Precedent: `tick_test.go:596-602` (rate limit) and `tick_test.go:663-669` (hang-up).
  Set the hook by direct field assignment between ticks, as those two do.
- **`listIssues` is incremented and `sinceValues` recorded *before* the hook runs**
  (`fixture_test.go:172-176`), so a withheld listing still counts as a listing and its `since` is
  still observable. The test can therefore assert on the cursor having sailed past the comment.
- **Shape it on the incident, not on convenience.** The issue must be **discovered and
  bootstrapped first**, then the comment added, then the listing withheld. A test that withholds
  the issue from the start reproduces a different (and by-design) failure — the bootstrap swallow.
  Sequence: `settle()` → `f.issue()` → `tick()` (bootstrap, watermark planted) →
  `addComment(testIssue, id, owner(), "/spike …")` → install the withholding hook → N ticks →
  remove the hook → `tick()` → **assert the run happened and the watermark is at the comment id**.
- **Assert against the store, never a return value** — see the `leave()` nuance above and
  `drain`'s doc at `tick.go:264-266`.
- **Assert the trap, not just the outcome.** Between the withheld ticks, assert that the persisted
  cursor has moved past the comment's `UpdatedAt` (`f.cursor()`). That is the sentence that makes
  the test explain the bug to a future reader instead of merely detecting it.
- **`addComment` touches the issue's `UpdatedAt`** (`fixture_test.go:307-311`), which is precisely
  what makes the withholding a lie rather than a tautology — the fake's `since` filter
  (`fixture_test.go:183-190`) *would* have returned the issue.
- Platform: Testing conventions — table-driven is not the shape here; this is one narrative test.
  Name it for the failure, not the fix, e.g. `TestALaggingIssueListingDoesNotSwallowAComment`.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-FC-1 | Regression test: bootstrap, then a listing that withholds the issue for N ticks, then recovery — asserted against the watermark read back from the store | test | — | M |
| T-FC-2 | Within the same test, assert the persisted cursor has advanced past the comment's `UpdatedAt` during the withheld window | test | T-FC-1 | XS |
| T-FC-3 | Record the failing output (exact assertion and values) in the commit message, so "it failed before" is evidence rather than assertion | test | T-FC-1 | XS |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-FC-1.1 | A test exists in `internal/daemon/tick_test.go` in which the forge's issue listing returns an empty array for one or more ticks *after* the task has been bootstrapped and *after* a triggering comment was written. |
| AC-FC-1.2 | The test asserts the comment is picked up on a later tick: a turn ran for that comment id and the watermark read back from the store is at that id. |
| AC-FC-1.3 | The test asserts, during the withheld window, that the persisted poll cursor has moved past the comment's update time — the trap, made visible. |
| AC-FC-1.4 | The test **fails** against the tree at `acbe751`, and the failure is recorded verbatim. |
| AC-FC-1.5 | The test uses the existing `onListIssues` hook and the existing fixture. No new test harness, no mocking library. |
| AC-FC-1.6 | Every assertion about consumption is made against the store, never against a `Tick` return value. |
| AC-FC-1.7 | No production code changes in this story. |

---

## User Story 2 — Tier 1: a bounded overlap on the poll cursor

> As the owner, I want the daemon to re-ask a short overlap window each tick, so a listing that
> lags by seconds cannot put a comment permanently behind `since`.

Three lines and a constant. Converts an unbounded silent-loss failure into one bounded by the
margin. **Ships subject to OD-1; the value is OD-3.**

### Architecture Notes
- **Apply the lookback to the persisted value, not to the value read.** This is the whole
  correctness of the story:
  ```
  next := startedAt.Add(-CursorLookback)   // at tick.go:99, before the pending comparison
  ```
  `InitPollCursor` (`store/pollcursor.go:53`) still plants the first-ever cursor at `now`, so the
  **first** tick still asks `since = now` and lists nothing — AC-2.9's no-stampede property is
  untouched at the point it actually matters. If instead the lookback were applied on read
  (`poll(…, cursor.TasksUpdatedSince.Add(-lookback), …)`), the first-ever tick would reach behind
  the daemon's birth and replay history, which is the exact thing AC-2.9 exists to prevent. It
  would also break `fixture_test.go:673` `settle()` and `tick_test.go:712`.
- **`settle()` survives — verified, with the reason.** `settle()` (`fixture_test.go:673-680`)
  asserts only that the *first* tick finds nothing, and the lookback does not touch the first
  tick's `since`. Every one of the 17 tests that use it registers its issues afterwards. The
  brief's claim holds, **conditional on the bullet above**; implemented the other way it fails
  immediately. Confirming this by running the suite is a task, not an assumption.
- **`TestCursorIsInitialisedToNowAndSurvivesRestart` (`tick_test.go:699`) passes unchanged —
  static analysis, to be confirmed by running it.** `firstEver.Issues == 0` holds (first tick,
  `since = now`). The restart assertion compares `sinceValues[1]` against `f.cursor()`, i.e.
  against whatever was persisted — so it follows the lookback rather than fighting it.
- **`TestTickCostIsBoundedByTheRepoCount` (`tick_test.go:738`) and
  `TestPullRequestsAreFilteredAndEachRepoCostsOneQuery` (`tick_test.go:428`): the brief expects
  both to need updating. Static analysis says both pass unchanged.** In the cost test, `settle()`
  runs, then exactly one issue is created; the assertions count *listings* (1 issue listing, 1
  comment listing), and a lookback that re-lists nothing else changes neither. In the PR test,
  `result.Issues == 1` holds because the second repo has its own handler returning an empty array
  (`fixture_test.go:154-160`) and the PR is filtered. **Treat this as a prediction to be falsified
  by running the suite, not as a conclusion** — the point of the task is the run.
- **The suite-wide effect is real even though no assertion catches it.** `testClock` advances one
  second per reading (`fixture_test.go:390-397`), so a 5-minute lookback reaches back before every
  fixture's epoch: after the first tick, *every* issue in the fake forge is listed on *every*
  tick. Only `settle()` and `tick_test.go:712` assert `Issues == 0`, and both are first ticks — so
  nothing breaks, but any future test asserting "the second tick lists nothing" will. Say so in a
  comment next to the constant.
- **`pending` is subsumed, not removed.** Keep the comparison at `tick.go:100-102`: a deferred
  task whose `UpdatedAt` is older than `startedAt - CursorLookback` must still win. Deleting the
  hold-back would trade a small correctness property for three fewer lines.
- **Constant, not config — with a test seam.** The brief's reasoning holds and is verified:
  `config.yaml` decodes with `KnownFields(true)` (`internal/config/config.go:231`, doc at `:6-9`),
  so a new key costs a struct field, a template line, a validation range and a drift test, for a
  value no operator should have to choose. `github/client.go:19-20` already states this stance
  verbatim. **But** a package-level `const` cannot be exercised by a test that needs lag exceeding
  the margin (the test clock would have to burn ~300 readings). Recommendation: a
  `daemon.Options.CursorLookback time.Duration` where zero means `DefaultCursorLookback`, mirroring
  `Options.Interval` exactly (`daemon/daemon.go:107-109`) and living beside `Options.Now`, whose
  doc already says the clock seam exists "so that backoff and **cursor** behaviour can be asserted
  without waiting" (`daemon/daemon.go:120-123`). Not exposed in `config.yaml`.
- **Second-tick cost on a real machine, stated so it is not a surprise:** after a fresh install,
  the second tick lists everything updated in the 5 minutes before the daemon's birth and
  *bootstraps* it — one comment listing each, processing nothing (AC-2.10). Bounded and one-off.
- Platform: Logging — the tick's `cursor` field already logs `next` (`tick.go:124`). Add the
  lookback to the startup line so the margin in force is discoverable without reading source.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-FC-4 | `DefaultCursorLookback` constant + `Options.CursorLookback` seam (zero ⇒ default), mirroring `Options.Interval` | logic | — | S |
| T-FC-5 | Apply the lookback at `tick.go:99` — to the persisted value only; keep the `pending` hold-back | logic | T-FC-4 | XS |
| T-FC-6 | Doc comment at the constant: why persist-time not read-time, why constant not config, and the fake-forge consequence for future tests | logic | T-FC-5 | XS |
| T-FC-7 | Run the full `internal/daemon` suite with `-race`; report which of the three named at-risk tests actually moved and why | test | T-FC-5 | S |
| T-FC-8 | Update whichever at-risk tests genuinely broke — changing the assertion's *wording and reason*, not just its number | test | T-FC-7 | S |
| T-FC-9 | Test: AC-2.9 still holds — the first-ever tick lists nothing and persists `startedAt − lookback`; a restart resumes from the persisted value | test | T-FC-5 | S |
| T-FC-10 | Test: a deferred task whose `UpdatedAt` predates the lookback window still holds the cursor back | test | T-FC-5 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-FC-2.1 | After a successful tick the persisted cursor is `startedAt − CursorLookback`, or `pending` when `pending` is earlier. |
| AC-FC-2.2 | The lookback is applied **only** to the value persisted. The `since` passed to `poll` is the cursor as read from the store, unmodified. A test asserts the first-ever tick's `since` equals "now". |
| AC-FC-2.3 | AC-2.9 holds in substance: the first-ever tick lists nothing; tasks surfaced by the overlap on the second tick are first sights and are bootstrapped, processing nothing. |
| AC-FC-2.4 | AC-FC-1.2 now passes: the withheld-listing regression test is green. |
| AC-FC-2.5 | The `pending` hold-back still wins when it is earlier than the lookback window. |
| AC-FC-2.6 | The margin is a Go constant with a documented rationale, overridable only through `daemon.Options` for tests. It is **not** a `config.yaml` key. |
| AC-FC-2.7 | The whole `internal/daemon` suite is green under `-race`. Any test whose expectation changed has its *reason* rewritten, not just its number. |
| AC-FC-2.8 | AC-2.8 is restated in code comments and in-tree docs to the amended wording below. |
| AC-FC-2.9 | The lookback in force is logged once at daemon startup. |

---

## User Story 3 — Tier 2: drain tracked tasks by watermark, not by listing

> As the owner, I want a task litefactory already tracks to be drained every tick regardless of
> what the issue listing says, so listing lag of *any* magnitude cannot swallow a comment.

The structural fix. The issue listing becomes purely a **discovery** mechanism; a task with a
session and a watermark stops depending on it. **Gated on OD-1, and unbuildable until OD-2 is
answered.**

### Architecture Notes
- **There is no liveness signal in the schema today. This is the story's real cost, and it is why
  OD-2 is not a detail.** Verified against `internal/store/migrations/0001_init.sql` (the only
  migration):
  - The column is `sessions.state`, not `status` (`0001_init.sql:57`), legal values `idle` /
    `running` / `failed` (`store/sessions.go:16-24`). It describes **run lifecycle, not
    liveness** — every session is `idle` most of the time, including long-abandoned ones. All
    three of today's sessions are `idle`, which is why the brief said it "carries no signal".
  - **Issue open/closed is not stored.** `github.Issue.State` is parsed (`github/issues.go:24-25`,
    `:167`) and the listing asks `state=all` (`github/issues.go:79`), but nothing in `daemon` or
    `store` ever reads it. The `task` struct carries the whole issue (`daemon/tick.go:179-186`)
    and consumes only `Number`, `Title`, `Body`, `Author`, `UpdatedAt`.
  - **`sessions.updated_at` is not last-activity.** `GetOrCreateSession` returns an existing row
    untouched without bumping it (`store/sessions.go:85-88`). It moves only on slug freeze,
    worktree set, state change, or lock acquire/release.
  - **`watermarks.updated_at` (`0001_init.sql:132`, written at `store/watermarks.go:123`) is the
    only per-session timestamp that tracks comment traffic** — the closest available proxy. Caveat:
    it is bumped even by a no-op MAX advance.
  So "live" must be defined as one of: (a) every session with a watermark row — unbounded and
  monotonically growing; (b) a heuristic window over `watermarks.updated_at`; (c) a new column and
  a migration `0002`. **OD-2.**
- **`sessions` is the one entity in the store with no list accessor.** `ListRepos`
  (`store/repos.go:130`), `ListRuns` (`runs.go:379`), `ListComments` (`comments.go:151`),
  `ListSegments` (`segments.go:92`) all exist; sessions has ten accessors and every one returns a
  single row (`store/sessions.go:75`–`:301`). The multi-row machinery is already there —
  `scanSessionWith` (`sessions.go:336`), `prefixedSessionColumns` (`sessions.go:322`), and
  `FindSessionByIssue` (`sessions.go:141-153`) already runs a joined multi-row query before
  collapsing to one. So this is a new method in an established shape, not new infrastructure.
- **One join, not N+1.** There is no bulk watermark read either (`store/watermarks.go` has only
  `GetWatermark`/`BootstrapWatermark`/`AdvanceWatermark`). The accessor must return sessions,
  watermarks and repos in a single query. `SetMaxOpenConns(1)` (lf-setup invariants) means N+1
  single-row lookups serialise against everything else in the process.
- **The cost arithmetic, which is what OD-2 is really about.** One `ListCommentsAfter` per live
  task per tick. At the default 30 s interval that is 120 requests/hour per task against a
  5,000/hour core budget, so `repos + live_tasks ≤ ~41` saturates it. **At the 10 s floor that D-C
  permits, the ceiling is ~13.** Today's daemon spends ~120/hr total, so there is room now — but
  tier 2 converts "number of issues litefactory has ever seen" from a free variable into a rate
  ceiling.
- **And the per-task cost is proportional to the issue's *total* comment count, not to new
  comments.** `ListCommentsAfter` filters client-side (`github/comments.go:76`) because GitHub's
  endpoint has no after-id cursor (`comments.go:45-50`), and `fetchPages`
  (`github/pagination.go:37-50`) walks every page. A 250-comment issue costs 3 requests **per
  tick, for ever**. The brief's "exactly one extra HTTP request" is true only for issues under 100
  comments. **Mitigation worth building:** pass a `since` derived from `watermarks.updated_at` to
  bound the pages fetched, while still filtering by id client-side — id stays the correctness
  boundary, the timestamp is only a page bound. This is not optional at scale; it is what keeps
  the ceiling arithmetic above honest.
- **Dedupe against the listing, within the tick.** An issue the listing *did* return is already
  drained by `processIssue` (`tick.go:190`). Draining it again in the same tick would double the
  comment listings and make `TestTickCostIsBoundedByTheRepoCount` (`tick_test.go:738`) fail for a
  real reason. Carry a set of session ids handled by the listing pass and skip them.
- **First sight must stay first sight.** The tracked-task pass operates on sessions that already
  have a watermark row; it must never call `bootstrap`. A session without a watermark is not
  tracked and is the listing's business.
- **Bound it explicitly, and log the bound.** An unbounded task set turns every tick into a
  fan-out. Whatever OD-2 decides, the implementation carries a hard ceiling and logs when it is
  hit — the tick logs its cost today (`tick.go:116-125`, `github/issues.go:107-113`) and AC-2.8's
  criterion has always been "bounded and logged", not "one call".
- **A failure in the tracked-task pass must not persist the cursor.** Same discipline as
  `tick.go:94-97`: the pass runs before `SetPollCursor` and an error returns early. AC-2.12/2.13.
- **Then the ordering invariants are unchanged**, because the pass reuses `drain`
  (`tick.go:274`) verbatim: ascending comment id (AC-2.11), stop at the first unconsumed comment,
  a locked task consumes nothing (AC-3.1) and defers, holding the cursor back as it does today.
- **This does not close the bootstrap knife-edge** (see above). Say so where the pass is
  documented, so a future reader does not believe the class is closed.
- Platform: Concurrency — the pass is sequential inside the tick, like `poll`. The goroutine
  inventory stays four.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-FC-11 | **OD-2 gate:** write the "live task" definition down, with its ceiling arithmetic, before any code | infra | OD-2 | S |
| T-FC-12 | Migration `0002` — **only if** OD-2 picks a new column. Otherwise explicitly skipped and recorded as skipped | data | T-FC-11 | M |
| T-FC-13 | `store.ListLiveTasks` (name per OD-2): one joined query over `sessions`/`watermarks`/`repos`, returning what `drain` needs, with a `LIMIT` | data | T-FC-11 | M |
| T-FC-14 | `ListCommentsAfter` gains an optional `since` page bound; id stays the correctness filter | logic | — | M |
| T-FC-15 | Tracked-task pass in `Tick`: after `poll`, before `SetPollCursor`; dedupes against the listing; reuses `drain`; never bootstraps | logic | T-FC-13, T-FC-14 | L |
| T-FC-16 | Cost ceiling + `TickResult` counters + a log line naming live tasks polled, requests spent, and whether the ceiling was hit | logic | T-FC-15 | S |
| T-FC-17 | Test: the withheld-listing regression passes for an arbitrarily long withhold (the tier-2-only case tier 1 cannot satisfy) | test | T-FC-15 | M |
| T-FC-18 | Test: an issue returned by the listing is drained **once**, not twice — comment-listing count asserted | test | T-FC-15 | S |
| T-FC-19 | Test: a session with no watermark row is never touched by the pass; first sight still runs through `bootstrap` | test | T-FC-15 | S |
| T-FC-20 | Test: a failure in the tracked-task pass leaves the cursor unpersisted (AC-2.12/2.13) | test | T-FC-15 | S |
| T-FC-21 | Test: a locked task consumes nothing and defers, unchanged (AC-3.1) | test | T-FC-15 | S |
| T-FC-22 | Rewrite `TestTickCostIsBoundedByTheRepoCount` (`tick_test.go:738`) to the amended AC-2.8 cost model | test | T-FC-16 | M |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-FC-3.1 | Every tick drains every **live** tracked task by its stored watermark, whether or not the issue listing returned it. "Live" is OD-2's answer, written down with its arithmetic. |
| AC-FC-3.2 | A comment on a tracked task is picked up **regardless of how long the issue listing withholds the issue** — asserted by a test that withholds it for longer than any tier-1 margin. |
| AC-FC-3.3 | An issue the listing returned is drained exactly once per tick. Asserted by comment-listing counts, not by inspection. |
| AC-FC-3.4 | A session with no watermark row is never touched by the pass. First sight still runs through `bootstrap` and still processes nothing (AC-2.10). |
| AC-FC-3.5 | The candidate set is bounded by an explicit ceiling; hitting it is logged at WARN naming the ceiling and the count. |
| AC-FC-3.6 | Per-tick cost is logged: live tasks polled and requests spent, alongside the existing per-repo line. |
| AC-FC-3.7 | The candidate query is **one** round trip. No N+1 over sessions or watermarks. |
| AC-FC-3.8 | `ListCommentsAfter` bounds its pagination by a timestamp while continuing to filter by comment id. Correctness still rests on the id; a wrong timestamp can only cost requests, never skip a comment. |
| AC-FC-3.9 | A failure anywhere in the pass returns before `SetPollCursor`. AC-2.12/2.13 hold. |
| AC-FC-3.10 | AC-2.11 ascending order, AC-3.1 locked-task deferral and the MAX-based watermark are unchanged — the pass reuses `drain` rather than reimplementing it. |
| AC-FC-3.11 | AC-2.8 is restated to the amended tier-2 wording below, and `TestTickCostIsBoundedByTheRepoCount` asserts the new model with a rewritten explanation. |
| AC-FC-3.12 | The code documents that this does **not** close the first-sight bootstrap swallow. |

---

## User Story 4 — Tier 3: a near-miss is audible

> As the owner, I want a comment that arrives later than it should to say so in the log, so the
> margin being eaten is visible before it is exceeded.

Independently valuable, ships regardless of OD-1. Cheap.

### Architecture Notes
- **Be precise about what this does and does not detect.** A WARN in `drain` fires only on a
  comment that *was* admitted. A comment lost outright produces nothing, by construction — that is
  the "silent by construction" property from the brief, and tier 3 does not change it. Its value is
  as a **gauge on the margin**: with tier 1 in place, both lost comments would have been picked up
  (19 s and 1.2 s, well inside any sane lookback) and both would have WARNed. Without tier 1 or
  tier 2, tier 3 fires on nothing in this failure mode. State that in the doc comment; a monitoring
  signal misunderstood as a miss detector is worse than none.
- **`github.Comment.CreatedAt` already exists** (`github/comments.go:36`) and is populated. No API
  change.
- **Threshold from the tick interval, not a second constant.** "Older than a couple of tick
  intervals" — derive from the daemon's configured `Interval` (`daemon/daemon.go:107-109`), so a
  60 s poller does not warn constantly and a 10 s one is not deaf. A small multiplier constant is
  fine; a second duration knob is not.
- **Where.** In `drain` (`tick.go:274-303`), on each admitted comment, before `handleComment`.
  `d.now()` is the reference; the tick's `startedAt` is not in scope and threading it in for this
  is not worth the signature change.
- **Log fields, not prose.** Task, session id, comment id, `created_at`, computed age, threshold —
  the existing `slog` convention (`tick.go:294-298` is the template). The comment **body must not
  be logged**: `daemon.log` is unredacted and unrotated (accepted M0 debt).
- **This will be noisy in one legitimate case:** a task that deferred for several ticks (a held
  lock, AC-3.1) will re-present an old comment each tick and WARN each time. Either exclude
  comments already seen in a prior tick, or accept it and say so at the WARN site. Prefer accepting
  it and saying so — suppression state is the more expensive mistake.
- Platform: Logging — `log/slog`, structured fields, no `fmt.Sprintf` into the message.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-FC-23 | Age check in `drain` with a WARN past N tick intervals; structured fields; body never logged | logic | — | S |
| T-FC-24 | Doc comment stating exactly what this does and does not detect, and that it is a gauge on the margin | logic | T-FC-23 | XS |
| T-FC-25 | Test: a comment older than the threshold WARNs; a fresh one does not; the WARN never changes what is processed | test | T-FC-23 | S |
| T-FC-26 | Test: the WARN carries no comment body | test | T-FC-23 | XS |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-FC-4.1 | `drain` compares each admitted comment's `CreatedAt` against the current time and logs at WARN when the age exceeds a small multiple of the configured tick interval. |
| AC-FC-4.2 | The threshold derives from the daemon's `Interval`. No new configuration key. |
| AC-FC-4.3 | The WARN is structured: task, session id, comment id, `created_at`, age, threshold. **Never the body.** |
| AC-FC-4.4 | The WARN changes nothing about processing. It is observability only — asserted by a test showing identical outcomes with and without it triggering. |
| AC-FC-4.5 | The doc comment states that a comment lost outright produces no warning, and that the signal is a gauge on the margin rather than a miss detector. |
| AC-FC-4.6 | The known-noisy case (a repeatedly deferred task re-presenting an old comment) is either handled or documented at the WARN site. |

---

## User Story 5 — The amended criteria, recorded in-tree

> As a future reader of the M0 spec, I want AC-2.8 and AC-2.9 to say what the code actually does,
> so the contradiction is documented rather than discovered.

Follows the S9 precedent from `comment-intent` — an amendments file plus a pointer line, not a
rewrite of the historical spec.

### Architecture Notes
- **Both tiers invalidate AC-2.8 as written.** The criterion was always "bounded and logged", not
  "one call" — `github/issues.go:53-55` says exactly that, and `tick.go:129-132` repeats it. The
  wording needs to follow the code, and the code comments and the spec must not drift apart.
- **A pointer line, not a rewrite.** One "Amended by `<path>`" under AC-2.8 and AC-2.9 in
  `lf-setup/IMPLEMENTATION_PLAN.md`. The historical plan stays historical; that is what makes the
  amendment trail readable.
- **Record the bootstrap knife-edge as a known, open residual**, with its file:line and the reason
  neither tier closes it. A hazard that was found and consciously not fixed is a different artifact
  from one nobody noticed.
- **Record the three corrected citations** so the next reader of the brief is not sent to the wrong
  lines.
- The file reaches the branch through the ordinary `.aiteam/` harvest — same mechanism as every
  other artifact here.

### Amended wording — AC-2.8

*Original (`lf-setup/IMPLEMENTATION_PLAN.md:222`):*
> Each tick issues one bounded, logged query **per registered repo**; PR entries are filtered out;
> tick API cost is O(registered repos).

*If tier 1 ships:*
> **AC-2.8 (amended, tier 1).** Each tick issues one bounded, logged query per registered repo,
> plus one comment listing per issue the forge reports as updated within the cursor window. PR
> entries are filtered out. Tick API cost is O(registered repos + issues updated within
> `CursorLookback`) — bounded by the lookback constant, and every component logged. The criterion
> is "bounded and logged", not "one call".

*If tier 2 ships:*
> **AC-2.8 (amended, tier 2).** Each tick issues one bounded, logged query per registered repo for
> **discovery**, plus at most one comment listing per **live tracked task**, where "live" is
> defined by OD-2 and enforced by an explicit ceiling. Tick API cost is O(registered repos + live
> tracked tasks); the ceiling, not the size of the sessions table, is what bounds the tick.
> Comment listings are page-bounded by a timestamp while remaining id-filtered for correctness.
> Every component is logged, including when the ceiling is hit.

*(If both ship, the tier-2 wording supersedes, with the lookback named as the discovery-path
margin.)*

### Amended wording — AC-2.9

*Original (`lf-setup/IMPLEMENTATION_PLAN.md:223`):*
> The `tasks_updated_since` cursor persists after each successful tick, survives restart, and is
> initialised to "now" at the first-ever tick.

*If tier 1 ships:*
> **AC-2.9 (amended).** The `tasks_updated_since` cursor persists after each successful tick and
> survives restart. It is initialised to "now" at the first-ever tick, so the first tick lists
> nothing and no history is replayed. After a successful tick it persists at
> `startedAt − CursorLookback`, so the second and later ticks re-ask a bounded overlap window.
> Tasks surfaced by that overlap on the second tick are first sights: they are bootstrapped and
> process nothing, so the no-stampede property holds in substance. **The lookback is applied to
> the value persisted, never to the value read at the top of a tick** — applying it on read would
> push the first-ever tick's `since` behind the daemon's birth and replay history, which is what
> this criterion exists to prevent.

*Tier 2 alone does not change AC-2.9.*

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-FC-27 | `$session/AMENDMENTS.md`: AC-2.8 and AC-2.9 restated, with the incident and the reason | infra | S2 and/or S3 landed | S |
| T-FC-28 | "Amended by `<path>`" pointer lines under AC-2.8 and AC-2.9 in `lf-setup/IMPLEMENTATION_PLAN.md` | infra | T-FC-27 | XS |
| T-FC-29 | Record the first-sight bootstrap knife-edge (`daemon/tick.go:225-251`) as a known open residual, with why neither tier closes it | infra | T-FC-27 | XS |
| T-FC-30 | Record the three corrected citations from this plan's verification pass | infra | T-FC-27 | XS |
| T-FC-31 | Update the code comments that state the old cost model: `tick.go:129-132` and `github/issues.go:53-55` | logic | T-FC-27 | XS |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-FC-5.1 | `$session/AMENDMENTS.md` records AC-2.8 and AC-2.9 in their amended wording, with the incident, the date, and the reason. |
| AC-FC-5.2 | A single "Amended by" pointer sits under each of AC-2.8 and AC-2.9 in the M0 plan. The historical plan is not rewritten. |
| AC-FC-5.3 | The doc comments at `tick.go:129-132` and `github/issues.go:53-55` state the same cost model as the amended AC. Code and spec do not drift. |
| AC-FC-5.4 | The first-sight bootstrap knife-edge is recorded as a known, open residual with its file:line. |
| AC-FC-5.5 | The corrected citations (`comments.go:76`, `classify.go:225`/`:268`, `tick.go:470-485`) are recorded. |

---

## User Story 6 — Recovery of the two lost comments *(gated on OD-5)*

> As the owner, I want to decide deliberately whether the two swallowed comments are replayed,
> rather than have that decided by whoever runs the fix.

**Not code. A one-off operational action on live state.** Planned here so it is a choice; **not
performed by this planning pass, and not to be performed by Implement mode without an explicit
instruction.**

### Architecture Notes
- **The mechanism.** `watermarks` row 3 is still `0`, so rewinding
  `poll_cursor.tasks_updated_since` below `2026-08-25T11:38Z` makes the next tick list issue #3,
  find the watermark at 0, and drain both comments in ascending id order. `/research` (5409831806)
  draws a run-less reply; `/spike` (5409849898) starts a **real run**.
- **The daemon is live.** It is polling `marioatcloudvane/litefactory#3` every 30 s right now. A
  rewind takes effect on the next tick, unattended. Stop the daemon, rewind, restart — do not edit
  the database under a running daemon; `SetMaxOpenConns(1)` protects the process from itself, not
  from a second writer.
- **Rewinding the cursor is not surgical.** It replays *every* repo and *every* issue updated since
  the rewind point, not just #3. Every one of those tasks is already tracked with a watermark, so
  the MAX-based watermark absorbs them and they produce no work — but the assertion should be
  checked, not assumed, before the rewind, and the rewind point should be as recent as it can be
  (just before `11:38:57Z`).
- **Cheaper and narrower alternatives exist** and should be weighed before the rewind:
  (a) simply re-post the two comments on the issue by hand — new comment ids, above the watermark,
  picked up on the next tick, zero database access, and the `/spike` run starts from a comment the
  owner deliberately re-issued; (b) do nothing, since `/research` produces only a reply and the
  `/spike` can be re-issued at will.
  **Recommendation: (a) or (b), not the rewind.** Reasoning: the rewind is a write to live state
  whose blast radius is "every task updated since the rewind point", to recover two comments that
  cost one copy-paste to reissue. The evidence value of the current database state — a task with
  `watermark = 0` and no comment rows, which is the incident's fingerprint — is also worth more
  than the two comments, and the rewind destroys it. This is a recommendation, not a decision.
- **If the rewind is chosen anyway:** back up `~/.litefactory/litefactory.db` first, with the
  daemon stopped, and record the pre- and post- cursor values.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-FC-32 | **OD-5 gate.** Nothing below runs until the owner picks a route | infra | OD-5 | XS |
| T-FC-33 | If (a): re-post the two comment bodies on issue #3 by hand; confirm pickup in the log and in `comments` | infra | T-FC-32 | XS |
| T-FC-34 | If rewind: stop the daemon, back up the database, record the cursor, rewind to just before `2026-08-25T11:38:57Z`, restart, observe one tick, record what was replayed | infra | T-FC-32 | S |
| T-FC-35 | Either way: record the outcome in `$session/AMENDMENTS.md` — what was recovered, how, and what it cost | infra | T-FC-33 or T-FC-34 | XS |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-FC-6.1 | No live state is touched until OD-5 is answered by the owner. |
| AC-FC-6.2 | If the rewind route is chosen, the daemon is stopped and the database backed up first, and the pre-rewind cursor value is recorded. |
| AC-FC-6.3 | The tick following any recovery action is inspected and its effect on **other** tasks recorded — the blast radius is observed, not assumed. |
| AC-FC-6.4 | The outcome is recorded in-tree, including if the decision was to do nothing. |

---

## Feature-level Acceptance Criteria

- [ ] A test exists that reproduces the incident and fails against `acbe751`.
- [ ] That test is green after the chosen tier lands.
- [ ] The full `internal/daemon` suite is green under `-race`, and every test whose expectation
      changed has a rewritten *reason*, not just a rewritten number.
- [ ] AC-2.9, AC-2.10, AC-2.11, AC-2.12/2.13 and AC-3.1 each have a passing test after the change.
- [ ] `internal/turn/classify.go` is byte-identical.
- [ ] A comment that arrives later than expected is audible in the log.
- [ ] AC-2.8 says what the code does, in the spec and in the code comments.
- [ ] `make test` and `make lint` are green.

---

## Out of Scope

- **The first-sight bootstrap swallow** (`daemon/tick.go:225-251`) — the same knife-edge applied to
  comment ids at first sight. A comment written between an issue's creation and litefactory's first
  listing of it is skipped by AC-2.10, by design, and neither tier changes that. Recorded by
  T-FC-29 as a known open residual; fixing it means revisiting the anti-stampede contract, which is
  a separate decision.
- Making a total miss detectable. Tier 3 gauges the margin; it cannot see a comment that was never
  listed. A real detector would need an independent reconciliation pass (e.g. a periodic
  `since`-less listing), which is a different design.
- Any change to the self-filter (S-8, D-I), to `UNIQUE (session_id, trigger_comment_id)`, or to
  slug derivation.
- Log rotation for `daemon.log` (`lumberjack` explicitly rejected in M0).
- Attributing the 19-second lag further. GitHub returned HTTP 200 with an empty array; there is no
  server-side detail available locally. The plan treats lag magnitude as unbounded rather than
  designing to 19 s.
- Multi-forge behaviour. The non-GitHub branch (`tick.go:147-153`) stays a loud skip.
- Anything in `comment-intent`. That session's plan lands independently; the two touch `tick.go`
  and will need rebasing against each other, but neither depends on the other.

---

## Open — Owner Decisions Required

Six, carried from the brief. None is resolved here. Where there is a recommendation it is labelled
as one, with its reasoning.

### OD-1 — Tier 2 in or out?

**No recommendation. This is the decision the plan exists to support.**

The facts that bear on it, all verified:

- Tier 1 is three lines and closes the observed incident (19 s and 1.2 s of lag, against a
  5-minute margin). It leaves a hole whose size is exactly "lag longer than the margin", and the
  magnitude of the lag is **unknown and unbounded** — the 19 s case could not be explained.
- Tier 2 makes lag structurally incapable of swallowing a comment on a **tracked** task. It costs a
  new store accessor, a liveness definition that **does not exist in the schema today** (OD-2), a
  per-tick fan-out, and a rate ceiling of roughly 40 live tasks at a 30 s interval — about 13 at
  the 10 s floor D-C permits.
- Neither closes the first-sight bootstrap swallow.
- Doing tier 1 now and tier 2 later costs one revisit of ~15 lines and one AC restated twice.
- Tier 3 (S4) and the regression test (S1) are unaffected either way.

**A variant not in the brief, offered because it changes the shape of the choice** *(assistant's
suggestion, not a research finding)*: a periodic **`since`-less reconciliation listing** — every N
ticks, list open issues with no `since` at all and drain anything whose watermark is behind. Cost
is O(repos × pages of open issues) every N ticks rather than O(live tasks) every tick, so it does
not degrade with the number of tracked tasks, and it also catches the bootstrap swallow. It is
weaker than tier 2 in that recovery takes up to N ticks instead of one. If the tier-2 ceiling
arithmetic is what gives you pause, this is the axis worth exploring before answering OD-1.

### OD-2 — If tier 2 is in, what is a "live" task?

**Only live if OD-1 says yes. Unbuildable until answered — S3 cannot start.**

There is **no liveness signal in the schema**. Verified: `sessions.state` is `idle`/`running`/
`failed` (`0001_init.sql:57`, `store/sessions.go:16-24`) and describes run lifecycle, not liveness;
issue open/closed is fetched but never stored; `sessions.updated_at` is not bumped on comment
traffic (`store/sessions.go:85-88`). The candidates:

| | Definition | Cost | Risk |
|---|---|---|---|
| (a) | Every session with a watermark row | No migration | Unbounded, monotonically growing; the ceiling arithmetic bites |
| (b) | `watermarks.updated_at` within a window (e.g. 7 days) | No migration; the only per-session timestamp tracking comment traffic (`0001_init.sql:132`) | Heuristic; bumped even by a no-op MAX advance; a task quiet for longer than the window silently leaves the safety net |
| (c) | Persist issue open/closed and drain open issues | Migration `0002` + one field already parsed at `github/issues.go:167` | Truest to intent; a closed-then-reopened issue needs care |
| (d) | An explicit `sessions.tracked` column, set and cleared deliberately | Migration `0002` + a lifecycle to maintain | Most control, most to keep correct |

**Recommendation, if tier 2 ships: (c), with (b) as the interim.** Reasoning: "the issue is open"
is the closest thing to what "live" actually means, the field is already parsed and thrown away, and
it bounds the set by something the owner controls directly (closing an issue). (b) ships without a
migration and is a reasonable first cut, but a window is a second guess of the same kind as the
lookback constant, and this story exists to stop guessing. (a) is only viable with a hard ceiling
and an eviction order — at which point you have built (b) implicitly and less legibly.

### OD-3 — Lookback value, and constant or config?

Only live if tier 1 ships.

- **Constant, not `config.yaml` — recommended, and the brief's reasoning is verified.**
  `KnownFields(true)` (`config/config.go:231`, doc at `:6-9`) makes a new key cost a struct field,
  a template line, a validation range and a drift test, for a value no operator should be asked to
  choose. `github/client.go:19-20` states this stance verbatim for exactly this class of tunable.
  **With one addition:** expose it as `daemon.Options.CursorLookback` (zero ⇒ default), mirroring
  `Options.Interval` (`daemon/daemon.go:107-109`) and living beside `Options.Now`, whose doc already
  justifies a seam for asserting cursor behaviour without waiting. Otherwise no test can exercise
  "lag exceeds the margin" without burning ~300 clock readings.
- **The value: no recommendation beyond "5 minutes is a defensible guess".** It is headroom over a
  19-second observation, not a measurement, and the honest position is that the distribution's tail
  is unknown. What is worth knowing before choosing: a longer margin costs only re-listings of
  issues that produce no work (F-2), so the cost curve is shallow — the argument for 5 minutes over
  1 minute is stronger than the arithmetic against it. If tier 2 also ships, the lookback only has
  to cover the **discovery** path and can be shorter.

### OD-4 — How is AC-2.8 restated?

**Recommendation: adopt the wording drafted in Story 5**, which follows whichever tier ships. The
substantive point, and the reason this is not just editing: the criterion was **always** "bounded
and logged", not "one call" — `github/issues.go:53-55` says so verbatim, and `tick.go:129-132`
repeats it. So this is the wording catching up with an intent that was already written down, not a
weakening of the contract. The thing that must not be lost is the word **bounded**: tier 2 in
particular is only acceptable with an explicit ceiling (AC-FC-3.5).

### OD-5 — Recovery of the two lost comments: in scope or separate?

Planned as Story 6, gated, not performed.

**Recommendation: re-post the two comments by hand, or do nothing — not the cursor rewind.**
Reasoning: the rewind writes to live state under a running daemon and replays every task updated
since the rewind point, to recover two comments that cost one copy-paste to reissue. It also
destroys the incident's fingerprint (session 3 with `watermark = 0` and no comment rows), which is
the only remaining physical evidence and is worth more than the two comments. Note the practical
consequence either way: the `/research` comment produces only a reply, but the `/spike` comment
starts a **real run** — so "recovery" is not a free action in either route.

### OD-6 — Does anything else share this knife-edge?

**Answered — this one can be closed.** The survey found exactly one further instance:
**`bootstrap` (`daemon/tick.go:225-251`)**, which plants a watermark at the highest comment id it
saw and declares everything below it consumed, on the strength of a listing that could be stale —
with no deferral escape hatch. Narrower than the poll cursor (one-time, per task) but real, and
closed by neither tier.

Everything else that resembles the pattern is clean, with reasons: `InitPollCursor`
(`store/pollcursor.go:53`) is a guarded read-then-insert in one transaction and skips history
deliberately; the backoff (`daemon/backoff.go:18-20`) stores a resume instant, not a data cursor;
`TouchRepoFetch` (`store/repos.go:155`) is display-only; `ReconcileOrphans` (`store/runs.go:439`)
trusts process liveness rather than listing completeness and is guarded by a fresh instance id; and
`fetchPages` (`github/pagination.go:24-31`) already refuses to return an empty slice from a request
it never made, its doc naming this precise hazard.

**The decision left for you is not "where else", but "do we fix the bootstrap one too?"** It is
logged under Out of Scope with that framing.
