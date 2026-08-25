# Research Brief — fix-comments-pickup
# Branch: main | Session: 2026-08-25 | Feature: fix-comments-pickup

> **Provenance: promoted from a Spike, not a fresh Research pass.** The findings below come
> from a live diagnosis run on 2026-08-25 against the running daemon (pid 83225), its log,
> its SQLite state and the GitHub REST API. No `codebase-tour` was run. Every claim is
> evidenced; the open questions are flagged as such rather than papered over.

## Request

> "i created a new comment on litefactory github but it was never picked up by the factory.
> check why it got swallowed!" → "ok and how do we fix that?" → "create a plan from that"

Two owner comments on `marioatcloudvane/litefactory#3` were never acted on. They were not
filtered, not classified, not replied to — they were never *seen*.

---

## The incident, established from evidence

### What was lost

Issue `#3` "Logging improvements", created `2026-08-25T11:38:57Z`:

| comment id | created (UTC) | body (truncated) | expected handling |
|---|---|---|---|
| 5409831806 | 11:40:37 | `/research I want the factory to give me better logging…` | `KindUnsupported` → run-less reply |
| 5409849898 | 11:42:25 | `/spike I want the factory to give me better logging…` | `KindSpike` → a real run |

Neither appears in `comments` in `~/.litefactory/litefactory.db` (6 rows, all for sessions 1
and 2). `sessions` row 3 exists with no slug, branch or worktree. `watermarks` row 3 is `0`.

### What it was NOT

Ruled out by direct inspection of the live database and code:

- **Not the self-filter.** `internal/turn/classify.go:207` never ran — nothing was recorded,
  and the filter records *every* comment it sees, including litefactory's own.
- **Not `ParseCommand`.** Both bodies carry a leading slash token in the vocabulary.
- **Not the AC-2.10 bootstrap skip.** `bootstrap` (`internal/daemon/tick.go:225`) ran at
  11:39:26 and logged `existing_comments: 0, watermark: 0, created: true`. The issue genuinely
  had no comments at that moment. Bootstrap skipped nothing.
- **Not a crash, rate limit or transport error.** 211 log lines, zero non-INFO records.
- **Not a second daemon instance.** One process; `daemon.lock` held; no competing writes.
- **Not clock skew.** A local clock offset large enough to explain the miss would also have
  broken the four *successful* pickups on issues #1 and #2 in the same run.

### Where it actually died

`drain` (`internal/daemon/tick.go:274`) never ran for issue #3, because `poll`
(`internal/daemon/tick.go:136`) never listed it again after bootstrap. From `daemon.log`
(times UTC, one tick per 30s):

```
11:39:26.6  LIST since=11:38:56 → issues=1   ← bootstrap, watermark=0
11:40:26.5  LIST since=11:39:56 → issues=0
11:40:56.5  LIST since=11:40:26 → issues=0   ← comment 5409831806 (11:40:37) is 19s old. MISSED
11:41:26.5  LIST since=11:40:56 → issues=0
11:41:56.5  LIST since=11:41:26 → issues=0
11:42:26.5  LIST since=11:41:56 → issues=0   ← comment 5409849898 (11:42:25) is 1s old. MISSED
11:42:56.5  LIST since=11:42:26 → issues=0   ← 11:42:25 < 11:42:26. Correctly excluded, for ever.
```

Every one of those was `requests:1`, HTTP 200, an empty JSON array — no error, nothing to log.

**Verification:** replaying the daemon's exact query today
(`state=all&sort=updated&direction=asc&per_page=100&since=2026-08-25T11:40:26Z`) returns issue
#3 with `updated_at=2026-08-25T11:42:25Z`. So GitHub's `since`-filtered listing did not reflect
the update at the moment the daemon asked, but does now.

### Root cause

`internal/daemon/tick.go:99`, after a successful tick:

```go
next := startedAt
if !pending.IsZero() && pending.Before(next) {
    next = pending
}
```

The poll cursor jumps to the tick's start time with **no overlap margin**. The `pending`
hold-back only fires for issues that were *listed and then deferred*; an issue that was never
listed cannot hold anything back. So a single listing that lagged puts the update permanently
behind `since`.

The design intends the per-task watermark to be the real dedupe —
`internal/github/issues.go:60` says so explicitly: *"The per-task comment watermark absorbs the
repeat — a re-seen issue whose comments are all at or below the watermark produces no work."*
It never gets the chance, because the issue is never listed a second time.

**And it is silent by construction.** `bootstrapped_tasks:1` followed by ticks of `issues:0` is
byte-identical to a quiet repository. There is no signal, at any log level, that separates
"nothing happened" from "something happened and we missed it".

### Residual uncertainty (honest)

The 1.2-second miss on comment 5409849898 is unremarkable read-replica lag. The **19-second**
miss on comment 5409831806 is larger than expected and could not be attributed further from
local evidence — GitHub returned a 200 with an empty array and there is no server-side detail
to inspect. Four successful pickups in the same daemon run detected comments 11s, 15s, 21s and
27s after they were written, so the listing is usually prompt.

Plan mode should treat the *magnitude* of the lag as unknown and unbounded rather than
designing to a measured 19s. This matters: it is the argument for tier 2 over tier 1.

---

## Codebase context

### The polling path, hop by hop

| # | File:line | What happens |
|---|---|---|
| 1 | `internal/daemon/tick.go:64` | `Tick` — backoff check, `InitPollCursor`, `ListRepos` |
| 2 | `internal/daemon/tick.go:94` | `poll(repos, cursor.TasksUpdatedSince, …)` |
| 3 | `internal/daemon/tick.go:136` | one `ListIssues(repo, since)` per repo (D-A, AC-2.8) |
| 4 | `internal/github/issues.go:72` | `state=all&sort=updated&direction=asc`, inclusive `since`, RFC3339 truncated to whole seconds |
| 5 | `internal/daemon/tick.go:190` | `processIssue` — `GetOrCreateSession`, `GetWatermark` |
| 6 | `internal/daemon/tick.go:225` | `bootstrap` — first sight, plant watermark, process nothing (AC-2.10) |
| 7 | `internal/daemon/tick.go:274` | `drain` — `ListCommentsAfter(watermark)`, ascending id, stop at first unconsumed |
| 8 | `internal/daemon/tick.go:99` | **cursor advanced to `startedAt`** ← the defect |
| 9 | `internal/store/pollcursor.go:83` | `SetPollCursor` persists it |

### Invariants any fix must preserve

| Invariant | Where | Note |
|---|---|---|
| AC-2.9 — first-ever tick plants the cursor at now, no history replay | `tick.go:74`, `store/pollcursor.go:53` | conditional insert, restart-safe |
| AC-2.10 — first sight of a task processes nothing | `tick.go:225` | makes re-discovery harmless |
| AC-2.11 — comments processed in ascending id order | `tick.go:274` | |
| AC-2.12/2.13 — a failed tick persists no cursor | `tick.go:94-97`, `tick.go:511` | |
| AC-2.8 — tick cost is O(registered repos), bounded and logged | `tick.go:129`, `github/issues.go:100` | **tier 1 and tier 2 both change this claim** |
| Watermark is MAX-based and never rewinds | `store` `AdvanceWatermark`, `tick.go:254` doc | why re-listing is free |
| One run per comment, ever | `UNIQUE (session_id, trigger_comment_id)` | belt-and-braces against replay |

### Why re-listing is cheap

`ListCommentsAfter` (`internal/github/comments.go:60`) filters `id <= afterID` client-side. A
re-seen issue whose comments are all at or below its watermark costs exactly one extra HTTP
request and produces zero work. Budget is 5,000/hr core; the daemon currently spends ~120/hr.

---

## The three candidate fixes

Presented to the owner on 2026-08-25. **Tier 2 is the decision that needs thinking about.**

### Tier 1 — cursor lookback margin (minimal, ~3 lines)

```go
const CursorLookback = 5 * time.Minute
...
next := startedAt.Add(-CursorLookback)
```

- Makes lag survivable *up to the margin*. Does not eliminate the failure mode.
- Subsumes the `pending` hold-back rather than fighting it.
- Cost: AC-2.8 shifts from O(repos) to O(repos + issues updated within the lookback).
- Bends AC-2.9: the first tick then persists `startedAt - 5m`, so the second tick reaches 5
  minutes behind the daemon's birth. Those tasks hit `bootstrap` and run nothing, so the
  "no stampede" property survives in substance — but the AC-2.9 comment needs amending.
- Constant not config, matching the stance already taken in `internal/github/client.go:19`
  (*"constants rather than configuration because none of them is a decision an operator should
  have to make"*). `config.yaml` also rejects unknown keys, so a key means a validator too.

### Tier 2 — poll known tasks by watermark, not by listing (structural)

The issue listing is a **discovery** mechanism; lag there is harmless, since a new task merely
gets found a tick later. But a task litefactory already tracks has a session and a watermark
and should not depend on the listing at all: drain it directly each tick.

- Makes lag *structurally incapable* of swallowing a comment, rather than survivable.
- Needs a store accessor (`ListActiveSessions` or similar) and a definition of "live" — all
  three sessions are currently `idle`, so session status is not that definition today.
- Needs dedupe against issues already handled by the listing in the same tick.
- Rewrites AC-2.8's cost model to O(repos + tracked tasks). Needs a bound: an unbounded task
  set turns every tick into a fan-out.

### Tier 3 — make a near-miss audible

In `drain`, compare an admitted comment's `CreatedAt` against the tick start and WARN past a
couple of tick intervals. With tier 1 in place this is what reports the margin being eaten
before it is exceeded. It would have fired on both lost comments.

---

## Open questions for Plan mode

1. **Tier 2 in or out?** Tier 1 alone leaves a known, unbounded-lag hole. Tier 2 closes it but
   rewrites the AC-2.8 cost contract. Doing tier 1 now and tier 2 later means touching the same
   code twice — but tier 1 is three lines and ships today.
2. **If tier 2 is in, what is a "live" task?** Session status is `idle` for all three sessions
   today, so it carries no signal. Candidates: issue state open/closed, last-activity window,
   an explicit column.
3. **What is the lookback value, and is it a constant or config?** 5 minutes is a guess with
   headroom over a 19s observation, not a measurement.
4. **How is AC-2.8 restated?** Both tiers invalidate "the whole per-tick API cost". The
   criterion was always "bounded and logged", not "one call" — the wording needs to follow.
5. **Recovery of the two lost comments — in scope or separate?** Watermark 3 is still `0`, so
   rewinding `poll_cursor.tasks_updated_since` below `2026-08-25T11:38Z` makes the next tick
   drain both: `/research` draws a reply, `/spike` starts a real run. This is a one-off
   operational action on live state, not code. It may belong in the plan as an explicit step,
   or outside it entirely.
6. **Does anything else in the tree share this knife-edge pattern?** Not investigated.

## Test surface Plan mode must account for

`internal/daemon/tick_test.go` — 19 tests, fixture at `internal/daemon/fixture_test.go`
(httptest forge, `testClock` advancing 1s per call, `settle()` helper used by 17 tests).

- `settle()` (`fixture_test.go:673`) still holds — it asserts only that the *settling* tick
  finds nothing, and tests register their issues afterwards.
- `TestCursorIsInitialisedToNowAndSurvivesRestart` (`tick_test.go:699`) — expected to pass
  unchanged; verify.
- `TestTickCostIsBoundedByTheRepoCount` (`tick_test.go:738`) — asserts exact call counts and
  **will** need updating under either tier.
- `TestPullRequestsAreFilteredAndEachRepoCostsOneQuery` (`tick_test.go:428`) — same risk.
- **Missing and required:** a regression test with a forge whose listing withholds an issue for
  one or more ticks, asserting the comment is still picked up later. That test fails today and
  is the one that would have caught this.
