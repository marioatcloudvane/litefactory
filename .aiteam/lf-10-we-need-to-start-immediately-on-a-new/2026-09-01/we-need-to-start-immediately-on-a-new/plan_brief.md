# Plan Brief — we-need-to-start-immediately-on-a-new (issue #10)
# Branch: lf/10-we-need-to-start-immediately-on-a-new
# Date: 2026-09-01
# Parent issue: marioatcloudvane/litefactory#10
# Citation base: cd857f0
# Research brief: ./RESEARCH_BRIEF.md
# Feature spec: ./feature-spec.md

Plan mode, headless run. Nobody could be asked a question, so every ambiguity was
resolved on the research brief's own recommendation and the assumption recorded —
either here, or in the *Architecture Notes* of the story it belongs to.

**Mode note.** The owner's comment on issue #10 was *"implement it!"*. Implement mode
requires `plan_brief.md` to exist and it did not; the routing rule for a missing gate
is to fall back to the earlier mode, so this run is Plan. Implement is the natural
next step and needs its own comment.

---

## Shared Decisions

Decisions binding on two or more stories. A decision that belongs to exactly one
story is in that story's sub-issue instead.

- **SD-A — the research brief's recommended mechanism (c) is REJECTED, and a dedicated
  `sessions.body_pending` column replaces it.** The brief recommended reusing the
  existing `sessions.needs_poll` backlog flag and asked Plan to check that against the
  mechanism's real shape. It was checked, and it does not hold. Three independent
  reasons, in descending order of severity:
  1. **A mark would not survive the tick that set it.** `Daemon.work`
     (`internal/daemon/tick.go:933`) calls `clearBacklog` unconditionally whenever
     `!deferred` (`:950-955`), and `deferred` is only ever assigned from `drain`
     (`:944`). `bootstrap` (`:942`) returns an error alone. So a flag set inside
     `runIssueBody` is cleared microseconds later by the same tick. The code names
     `:954` "the one clear site" in its own comment.
  2. **An unrelated run would silently wipe it.** `needs_poll` is also cleared inside
     `run` (`internal/daemon/tick.go:1432`), so a comment-triggered run on the same
     session would destroy the body-pending state. That is a correctness bug, not a
     style objection.
  3. **The contracts differ.** `needs_poll` is documented as self-healing stale-true —
     the next tick re-lists, finds nothing owed, and clears
     (`internal/store/migrations/0002_session_backlog.sql:21-24`). A body-retry flag
     never self-heals; it stays set until the issue is assigned or closed.

  The cost is one migration, `0003_*.sql`. The ledger's precedent that a `runs.owner`
  column was once rejected on migration-budget grounds
  (`.aiteam/AC_AMENDMENTS.md:1286-1312`) was weighed and does not apply: the cheaper
  option here is not merely uglier, it is broken.

- **SD-B — the flag is the discovery trigger *and* the cheap short-circuit.** It is
  what unions a quiet session back into discovery with no API cost, and it is what
  guards the body-gate call on the drain path so that the ~99% of sessions that owe no
  body pay nothing. One flag, two duties, and they do not conflict because both mean
  the same thing: *this session's body has not run and still might*.

- **SD-C — identity is compared on the numeric user id, never on a login (D-I), and
  every comparison carries a zero-id guard.** GitHub reports id `0` for a deleted
  account, which is why `Issue.AssignedTo` already guards it
  (`internal/github/issues.go:94-97`). Binds US-2 and US-3.

- **SD-D — the daemon writes nothing to the forge on the issue-body refusal path, and
  no forge endpoint is added.** SD-3′ is a log-level change. `refuseTools`'s posting
  behaviour is explicitly not revisited. Binds all three stories.

- **SD-E — a doc comment that a change makes untrue is rewritten as part of that
  change**, per this codebase's own stated convention
  (`.aiteam/APP_CONTEXT.md:322-328`). This is not cleanup and is not deferrable. Each
  story carries the doc comments it falsifies.

- **SD-F — "exactly once" survives, but changes register**, from a control-flow
  property to a database property resting on `UNIQUE (session_id, trigger_comment_id)`
  (`internal/store/migrations/0001_init.sql:103`). `turn.Execute` already degrades
  cleanly to `DispositionAlreadyRan` rather than erroring. The property holds; its
  *stated reason* does not, in several files — see SD-E.

- **SD-G — migrations are append-only and forward-only.** `0002_session_backlog.sql`
  is never edited; new state means `0003_*.sql`.

- **SD-H — a refusal is a value, not an error.** The retry signal travels as a return
  value through `runIssueBody` → `bootstrap` → `work`, not as a sentinel error and not
  as a store write from inside the gate.

## Workflow

**Shipping order: US-1 → US-2 → US-3.**

- **US-1 first** because it is the only piece useful on its own *today*: it would have
  explained issue #10 without any of the rest existing. It depends on nothing, and
  shipping it before US-2 means US-2's behaviour is observable in the log while it is
  being built rather than after.
- **US-2 second.** It is the actual defect fix and the largest story. It touches the
  same twenty lines as US-1, which is why US-1 goes first rather than alongside — the
  alternative is editing the same function and the same doc comment twice.
- **US-3 last.** It is a hard dependency on US-2, not a preference: US-3 alone changes
  nothing observable, because the gate would still only ever be asked at first sight.

**Dependency graph / hard couplings:**

- `US-1 → US-2 → US-3`, a straight chain. No story is parallelisable with another.
- **US-2's `bootstrap` signature change is a hard coupling**: changing what `bootstrap`
  returns forces `work`'s branch and every bootstrap test in the same commit. It cannot
  be staged.
- **US-3's one-line change lands on the same switch arm US-2 rewrote.** US-3 must
  follow US-2's gate rewrite, not race it.
- US-2 may be split internally along the store-plumbing / control-flow seam if it
  proves too large, but it is **not** split into two stories here: the first half would
  ship a column nothing reads, and dead code is a worse artifact than one large story.

## Cross-Story Constraints

- **Platform** — all work is on the daemon's single Tick goroutine and its workers;
  `context.Context` is threaded, never stored. Schema knowledge stays inside
  `internal/store`; `internal/daemon` consults a `Session` field and never SQL.
  Errors wrap with `%w`. Migrations append-only (SD-G).
- **Security** — the authorisation surface widens in **time**, not in **who**.
  Assignment to the owner's numeric id remains the sole authorisation and is untouched
  (AC-2.16a). What changes is how long the door stays open on a given issue: from one
  tick to as long as the issue is open. The owner has explicitly accepted the resulting
  unbounded-spend exposure (SD-1 in the research brief) on the grounds that GitHub's
  own permissions restrict who may assign. **No rate or spend cap is to be added** —
  D-X still forbids one (`.aiteam/AC_AMENDMENTS.md:119-174`).
- **Tests that must stay green, unchanged** — these encode rules no story is allowed to
  break:
  - `TestANonOwnerAssigneeDoesNotFireTheBody` (`internal/daemon/tick_test.go:774`) —
    this is AC-2.16a, which nothing here touches.
  - `TestABodyThatCommandsNothingStartsNothingAndPostsNothing` (`:696`) and
    `TestIssueBodyToolsRefusalPostsNothing` (`internal/daemon/tools_test.go:339`) —
    "posts nothing" stays true under SD-D.
  - Workflows 1 (comment-triggered) and 3 (assign-then-first-sight) already work and
    must not regress. The smallest-looking change — moving the gate out of `bootstrap`
    — touches the path workflow 3 runs on.
- **Ring-fenced, out of reach for this plan** — `internal/report` (no new reply kind);
  `internal/github` (no new endpoint, and the pinned method set in
  `internal/github/surface_test.go` does not change); `0002_session_backlog.sql`;
  `refuseTools`'s posting behaviour; `internal/config` (no new config key).

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Say out loud why an issue body did not run | S | — | _(link filled in on creation; body: `US-1.md`)_ |
| US-2 | Re-check the issue body after a late assignment | L | US-1 (soft) | _(link filled in on creation; body: `US-2.md`)_ |
| US-3 | Let the owner's own issues run their body despite comments | S | US-2 (hard) | _(link filled in on creation; body: `US-3.md`)_ |

The research brief calls these Story A, Story B and Story C respectively. Its Story C
("visible refusals posted to the thread") was dissolved by the owner's correction and
is **not** in this plan; the story numbered US-1 here is what the brief calls A1.

## Out of Scope

Global exclusions. Recorded so Implement does not relitigate them.

- **Any spend, rate or run-count cap.** SD-1: the owner accepted the exposure with his
  reasoning recorded. D-X forbids a run-rate circuit breaker.
- **Any forge write on the body-refusal path**, and any new forge endpoint. The
  repo-wide issue-events endpoint has no `since`, no `sort` and no documented ordering,
  so it cannot be cursored; the per-issue timeline is one request per issue per tick.
- **Depending on GitHub bumping `updated_at` on assignment.** Undocumented by GitHub,
  and this repository has measured its own issue listing lagging thirty minutes on a
  bump it had itself confirmed (`internal/github/comments.go:116-130`). No spike is
  needed, because the adopted mechanism does not depend on the answer.
- **Any change for SD-4** ("assigned to me and nobody else"). That is already exactly
  what `internal/daemon/tick.go:1077` does. No config key, no allowlist, no
  "assigned to anybody" mode.
- **An authorship-based *trigger*.** AC-2.16a stands verbatim. US-3 is an authorship
  *exemption from AC-2.16d*, which is a different thing — an unassigned issue the owner
  wrote still starts nothing.
- **Webhooks; issue reactions; log rotation; a log-volume guard.** `daemon.log` is
  unrotated by accepted M0 debt and this plan does not materially change that.
- **Surfacing `daemon.log` through the CLI.** `litefactory logs` reads run JSONL only;
  a `daemon logs` command would be a separate feature and the owner did not ask for one.
- **Retrying an issue whose body was empty at first sight.** See US-2's Architecture
  Notes — this is a deliberate narrowing with a consequence the owner should know about.

## Provenance

- **Citation base: `cd857f0`.** Every `path:line` in this plan and in the three
  sub-issues was re-pinned against this worktree during this run.
- **Citation rot found and corrected this run.** The research brief's run 0003 was the
  first clean run of the session; this run found six further drifts, all in the brief's
  own text, none of which changes a conclusion:

  | Cited in brief | Actual | Item |
  |---|---|---|
  | `tick.go:940-948` | `:940-944` | the bootstrap-vs-drain branch |
  | `tick.go:1043-1051` | `:1046-1055` | the "watermark first, then run" argument |
  | `tick.go:1505-1511` | `:1510-1515` | `refuseTools` body-path Info branch |
  | `tick.go:1107-1118` | `:1105`, log `:1113` | the `!command.Spawns()` branch |
  | `classify.go:343-352` | `:352-356` | the house Info-level precedent |
  | `classify_test.go:521-552` | `:528` | `TestSelfDropIsLogged` |

- **One claimed drift was itself wrong, and is recorded because it nearly removed a
  task.** The architecture review reported that
  `internal/harness/prompt.go`'s "not reachable today" claim no longer exists and
  advised dropping the task that rewrites it. It was checked directly: the text **is
  present and the brief's citation `:247-256` is exact**. The task is retained in US-2.
- **Known-stale, unchanged from the brief:** `.aiteam/APP_CONTEXT.md` describes
  `internal/github` as having "exactly seven endpoints" at three separate places
  (`:127`, `:212`, `:691`) against twelve pinned methods. Per the archive rule the
  correction belongs in the ledger, not in an edit to the archived text. Do not repeat
  "an eighth forge endpoint" as though it were accurate.
- **Re-pinning requirement before merge:** `internal/daemon/tick.go` is edited by all
  three stories, so every line number quoted against it in US-2 and US-3 must be
  re-pinned after US-1 lands, and again after US-2 lands. Line numbers in the sub-issues
  are pinned to `cd857f0` and will drift as the stories ship.
