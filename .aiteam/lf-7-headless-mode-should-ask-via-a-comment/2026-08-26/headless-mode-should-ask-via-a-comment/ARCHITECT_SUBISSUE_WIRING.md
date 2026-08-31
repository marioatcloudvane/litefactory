# Architect: Go Architect — sub-issue wiring (post-Plan-run phase)
# Created: 2026-08-31
# Scope: the missing phase that connects Story C1's orphaned capability
#   (`github.CreateSubIssues`, `report.RenderSubIssue`) to a live Plan run, so
#   AC-C1.3 and AC-C1.3a can hold. **No code in this document.**

## System Overview

### How this fits

A Plan run is one turn like any other: `turn.Execute` → `prepare` → `runAndHarvest`
(`internal/turn/harvest.go`) → `runClaude` → `save` (commit + push) → `finish`, and
then `daemon.run` (`internal/daemon/tick.go:705-736`) calls
`d.reporter.ReportTurn` to post the run's own comment. Nothing in that path knows
"this was a Plan run" — and per `plan-orchestrator.md:182` it must not try to: the
agent writes `$session/plan_brief.md`, an unresolved Story Index, and one file per
story into `$session/`, commits nothing itself, and returns. Everything from there
is litefactory's, unscoped by any prior story.

This design adds one new step — call it **the sub-issue phase** — inside
`runAndHarvest`, after `save()` returns and before `finish()` runs
(`internal/turn/harvest.go:73-74`). It runs on **every** successful turn's harvest,
not only ones that happen to be "Plan," because there is no reliable way to ask
what mode a turn was (see Q3 below) and because the phase's own gate — "is there a
`plan_brief.md` for this task with anything left to link" — is cheap and already
scoped to one task by construction.

It reuses, unmodified: `internal/git.Worktree.Commit`/`.Push` (the same two calls
`save()` already makes), `internal/github.Client.CreateSubIssues`/`.ListSubIssues`
(`internal/github/subissues.go:180,289`), and `internal/report.Reporter.RenderSubIssue`
(`internal/report/subissue.go:153`). It adds one new file to `internal/turn`, two
narrow interfaces in the existing `Poster`/`Renderer` idiom
(`internal/turn/report.go:26-39`), one small leaf-package addition for parsing
`plan_brief.md`'s Story Index and the per-story files, and one small, additive
export in `internal/naming`.

### New components

| Component | Package | Responsibility |
|---|---|---|
| `SubIssueCreator` (interface) | `internal/turn` | The one `CreateSubIssues` method the phase needs. `*github.Client` satisfies it. |
| `SubIssueRenderer` (interface) | `internal/turn` | The one `RenderSubIssue` method the phase needs. `*report.Reporter` satisfies it. |
| the phase itself (new function, e.g. `fileStoryIssues`) | `internal/turn`, new file `subissues.go` | Locate → parse → create → rewrite → commit/push. Called from `runAndHarvest`. |
| Story Index / per-story-file parsing | new leaf package, e.g. `internal/planbrief` | Pure text-in, struct-out: find unresolved rows, read a per-story file, rewrite one cell. No git, no network, no forge. |
| `naming.AITeamBranchDir` (new export) | `internal/naming` | Export what `branchDirName` (`internal/naming/aiteam.go:119`) already computes, so a caller outside `naming` can list `.aiteam/<branchDir>/` without duplicating the `tr '/' '-' | tr [:upper:] [:lower:]` transform a second time. |

### Key boundaries

- **`internal/turn` is the only new consumer.** It already imports `git`, `github`,
  `report`, `naming`, `store` (`APP_CONTEXT.md`'s package map) — everything this
  phase needs is already a permitted import. No new package gets added to the
  dependency graph anywhere else.
- **`internal/planbrief` (or wherever the Markdown parsing lands) is a leaf.** It
  never touches disk, git, or the forge — it is handed bytes and returns structs,
  the same discipline `internal/naming` holds itself to. This is what keeps the
  Story Index's table format (owned by `plan-orchestrator.md`'s contract) from
  leaking into `internal/turn`'s orchestration logic.
- **`internal/github` and `internal/report` are untouched.** Both hard constraints
  (`TestNoForgeAbstraction`, no second `.Reveal()`, no backoff) are satisfied by
  construction: the phase adds no interface to `internal/github`, no new
  credential path, and no retry loop around `CreateSubIssues`.

### Data flow

```
claude writes plan_brief.md + $session/<StoryID>.md, exits
  → save(): commit1 (claude's changes, as today) → push1
  → [NEW] fileStoryIssues():
       locate plan_brief.md for this task
       → parse Story Index + read each per-story file
       → CreateSubIssues(repo, parent=ref.Issue, specs)   [one batch call]
       → rewrite the Sub-issue cell of every row the batch answered
       → commit2 (litefactory-authored) → push2   [only if the file actually changed]
  → finish(): unchanged — reads only commit1's fields
  → (back in daemon.run) ReportTurn posts the run's own comment — unchanged,
    describes commit1 only
```

### Concurrency & context notes

None beyond what `runAndHarvest` already carries. The phase is synchronous, runs
under the same per-task lock `turn.Execute` already holds for the whole turn
(`internal/turn/execute.go:285-306`), and starts no goroutine. `CreateSubIssues` is
itself sequential and stops at its first failure by design
(`internal/github/subissues.go:281-288`) — nothing here parallelises it, and nothing
should: parallel `Add` calls are exactly what the endpoint's documented secondary
rate limit punishes.

### Shared state / side effects

- A second commit lands on the task branch, authored as `opts.Author` (the same
  human identity `save()` already commits as), not as the bot.
- Up to N new GitHub issues get created (one per unresolved story), each attached
  as a sub-issue of the parent — a real, visible side effect on the forge that
  persists even if the phase's own local write-back subsequently fails.
- `plan_brief.md`'s Story Index is rewritten in place, cell by cell, in the
  worktree — nothing else in the file is touched.

---

## The three questions

### Q1 — locating `$session/` without a known feature slug

**Answer: derive it, the same way `report.Run.briefLink` already does — do not
scan the whole `.aiteam/<branch>/` tree.**

`internal/report/link.go:135-158` already solves exactly this problem for linking
`RESEARCH_BRIEF.md` into a run's report, and its own doc comment states the house
principle directly: *"The path is derived, not discovered... exactly as CLAUDE.md
computes it."* It uses `naming.SlugFromBranch(branch, issue)`
(`internal/naming/aiteam.go:92`) — *"the inverse of \[naming.Branch\]"* — to recover
the slug frozen into the task branch at the moment the branch was created, then
`naming.AITeamSessionDir(branch, day, slug)` (`internal/naming/aiteam.go:51`) to
build the repository-relative directory. This is prior art the repo already ships
and reviews, and following it here keeps one derivation mechanism for every aiteam
session artefact instead of inventing a second, scanning one.

**The one gap in that mechanism, and how this design closes it.** `briefLink`
uses "the run's own start day" (`Run.startedOn()`, `internal/report/run.go:180-198`)
as `day`, and its own comment admits this is *"right except across a midnight
boundary"* — fine for an informational link that is allowed to be dead, not fine
for a phase that must actually find and rewrite a file, because a miss here means
silently doing nothing (a much worse failure than a dead hyperlink: AC-C1.3a stays
broken forever unless something else notices).

So the phase:
1. Tries the direct guess first — `naming.SlugFromBranch` + `naming.AITeamSessionDir`
   with the **current run's own start day** (`prep.run.StartedAt`, the same field
   `Run.startedOn()` reads). This is exact and free (zero I/O beyond one `os.Stat`)
   for the common case: the Plan run that just wrote `plan_brief.md` is, by
   construction, running on the same calendar day the session directory was named.
2. **Only if that miss** — the file doesn't exist at the guessed path, which
   happens on a retry days later, after an earlier partial failure — falls back to
   one bounded `os.ReadDir` of `.aiteam/<branchDir>/` (one level, no walk, the same
   discipline `repos.DetectAiteam`'s directory half already holds itself to,
   `internal/repos/aiteam.go:162-193`), checking `<date>/<slug>/plan_brief.md` for
   each date directory found, newest first. `<branchDir>` needs
   `naming.AITeamBranchDir` — see below.
3. If `SlugFromBranch` itself fails (a branch this task's own tooling did not
   build), or nothing is found at either step, **the phase does nothing for this
   turn** — logged at debug level, exactly the tolerance `briefLink` already has for
   "no page to link."

**Rejected: scanning `.aiteam/<branch>/*/*/plan_brief.md` unconditionally.** This
was my first instinct and I want to name why it lost. It works, but it duplicates
a derivation the codebase has already built, reviewed, and named a principle for
("derived, not discovered"); it would also need to handle multiple date/feature
matches under one branch with no ordering rule to prefer one, a problem the
derive-then-narrow-fallback approach above sidesteps because the slug is fixed by
`SlugFromBranch` and only the date varies.

**Rejected: the run's own changed-file list as the sole signal.** `result.Changes`
tells you *this run* touched `plan_brief.md`, which is exactly right for the first
successful Plan run and exactly wrong for a retry after a partial sub-issue-creation
failure, where the very next turn on the task may touch nothing in `$session/` at
all. Used alone it would silently strand an interrupted plan forever. (It is still
worth using as a cheap short-circuit — if `result.Changes` already names
`plan_brief.md`, skip straight to step 1 above with high confidence — but it must
not be the only path in.)

**Rejected: an agent-written pointer file.** `plan-orchestrator.md:182` is explicit
that the agent's part ends at writing the story files and `plan_brief.md` — adding
"and also write a pointer to yourself" is a second, parallel place the session path
can be recorded and drift from the truth, which is precisely what
`initialplan.md:243`'s *"state is inferred from artifacts, not self-reported"*
rules out. The derivation above needs no such pointer.

**Required addition to `internal/naming`:** export what `branchDirName`
(`internal/naming/aiteam.go:117-121`) already computes — e.g.
`naming.AITeamBranchDir(branch string) (string, error)`, returning
`path.Join(AITeamDirName, branchDirName(branch))` with the same
`validatePathSegment` check `AITeamSessionDir` already applies. This is additive,
pure, zero behaviour change to any existing caller, and keeps the `tr '/' '-' |
tr [:upper:] [:lower:]` transform defined in exactly one place rather than
re-derived in `internal/turn`.

### Q2 — ordering relative to commit and push: **two commits, two pushes**

**Decision: the write-back is a second, litefactory-authored commit and push,
strictly after `save()`'s commit1/push1 — never folded into the same commit, and
never attempted before push1 succeeds.**

Three independent reasons all point the same way:

1. **The permalink needs a confirmed-remote SHA, and there isn't one before push1.**
   `report.Story.DetailSHA` — the sha a sub-issue's "technical detail" link is
   pinned to — is documented to be *"Set it only for a commit that is on the
   remote. A permalink into a commit that exists only in a local worktree is a
   guaranteed 404"* (`internal/report/subissue.go:97-105`), and `detailLine()`
   already degrades to an unlinked plain path with no sha rather than guess
   (`internal/report/subissue.go:314-326`). The per-story file each sub-issue links
   back to was written by claude in *this* run, so it only becomes a real,
   pushable commit at commit1/push1. Rendering sub-issues before that point means
   every `Detail` link is either absent or dead; rendering them after means
   `Story.DetailSHA = result.SHAAfter` (already confirmed on the remote, per
   `result.Pushed`) and every link is good from the moment the sub-issue exists.
2. **The run's own report must describe only what claude did.** `finish()`'s
   `result.CommitSHA`/`result.Changes` feed `runInput` → `report.Run`
   (`internal/turn/report.go:552-577`), which is what `ReportTurn` posts as the
   comment on *this run*. A litefactory-authored edit to `plan_brief.md`'s Story
   Index folded into commit1 would show up in that comment's file list as
   something "the run" did, when in fact claude's subprocess never touched it —
   exactly the kind of comment-that-is-not-true this codebase's style rules
   forbid (`APP_CONTEXT.md`, *Style*: "Comments explain *why*, and are expected to
   be true"; the same discipline extends to what a run report claims). Keeping the
   two commits separate keeps that claim honest without adding a single new field
   or branch to `runInput`.
3. **Failure containment.** A GitHub outage or a partial batch failure during
   sub-issue creation must never retroactively touch a run that already succeeded
   and was already reported. Two independent commits, with the phase's own errors
   confined to its own return value (see *Failure modes* below), is what makes that
   true by construction rather than by care at every call site.

**Concretely, in `internal/turn/harvest.go`:**

```
result = save(ctx, opts, prep, result, runResult, logger)      // :73, unchanged
result = fileStoryIssues(ctx, opts, prep, result, logger)      // NEW, inserted here
return finish(bookkeeping, opts, prep, result, runResult, &meta, logger)  // :74, unchanged
```

`fileStoryIssues` must not write to `result.Changes`, `result.SHAAfter`,
`result.CommitSHA`, `result.Committed`, `result.Pushed`, `result.Err`,
`result.Outcome`, or `result.Reason` — those fields stay commit1's, because
`finish()` reads `result.CommitSHA` to call `opts.DB.SetRunCommit`
(`internal/turn/harvest.go:308-313`) and that call must record commit1's SHA, not
commit2's. If a second commit's outcome needs surfacing at all, it belongs on new,
separate `Result` fields that `runInput`/`report.Run` never read — see *Deferred*.

**Gating fileStoryIssues on whether it is safe to trust the worktree's HEAD as
"on the remote":** run it whenever `!runResult.Outcome == harness.OutcomeAbandoned`
(mirrors `save()`'s own guard, `internal/turn/harvest.go:216-220`) **and** it is not
the case that this run changed something and failed to push it
(`result.Changed && !result.Pushed`, the push-failure path). In every other
case — including a run that changed nothing at all — the worktree's current HEAD
is provably already on the remote, either because this run's push just succeeded
or because an earlier run's did and nothing has moved since. This is the same
reasoning `save()` itself uses for `Head` vs `SHA` (`internal/git/commit.go:95-109`),
carried one step further.

**`Worktree.Commit` staging everything (`git add --all`,
`internal/git/commit.go:119-121,148`) is what makes the second commit mechanically
trivial and automatically a no-op when there is nothing to say.** The phase writes
the rewritten `plan_brief.md` bytes to disk (nothing else), then calls
`prep.worktree.Commit` again with a litefactory-authored `Summary` (not
model-authored — no sanitisation concern) and the same `Task`/`Author` trailers as
commit1; if every Story Index row already carries its correct link (a retry that
found nothing left to do), the diff is empty and `Committed` comes back false —
exactly `save()`'s own AC-3.23 shape, reused for free. Only push when
`Committed` is true.

### Q3 — knowing the run was a Plan run: **don't try**

**Answer: gate on artifact presence (Q1's derivation succeeding, and the Story
Index having at least one unresolved row after the phase compares it against
GitHub's own state), not on any declared or inferred mode.**

Three things independently rule out mode-detection:

- **There is nothing to detect it from.** `store.ModeSpike` (`internal/store/runs.go:42`)
  is still the only mode constant the store carries; a free-text run is
  `harness.InferMode` (`internal/turn/execute.go:210-211`), and *which* mode that
  inferred work turns out to be is decided by the agent reading the target
  repository's own `CLAUDE.md` (`internal/turn/doc.go`'s own words: *"litefactory
  does not classify the request itself... Classifying here instead would mean a
  second model call to derive a label the run derives anyway"*). Building a
  mode-detector in Go would directly contradict that stated design choice.
- **Even a perfect mode tag wouldn't solve retries.** A retry that completes an
  interrupted sub-issue batch is very often triggered by an *unrelated* later
  comment — a `/spike`, a plain follow-up question — not by another `/plan`. Gating
  on "this turn's mode is plan" would miss every one of those, which is the same
  failure mode Q1 already rejected for the changed-files-only approach.
- **`initialplan.md:243`'s principle already answers it.** *"State is inferred
  from artifacts, not self-reported."* A `plan_brief.md` with an unresolved Story
  Index row is the artifact; its presence is the only fact this phase needs, and
  it is self-verifying in a way a mode label never is.

Practically: the phase's "is there anything to do" check *is* its mode check.
Every ordinary Spike/Implement/free-text turn on a task with no `plan_brief.md`
(or one that is already fully linked) pays for exactly one failed `os.Stat` and
returns — no different in cost from `save()` already reading worktree status on
every turn regardless of what kind of turn it was.

---

## Locating and resolving stories, precisely

1. Locate `plan_brief.md` per Q1.
2. Parse its Story Index table (`| ID | Title | Size | Depends On | Sub-issue |`,
   the exact shape `plan-orchestrator.md:96-101` specifies) into rows. This parsing
   is the new leaf package's job — pure, testable against literal fixture strings,
   no filesystem or network.
3. **Trust GitHub, not the file, for "does this story already have a sub-issue."**
   Build one `github.SubIssueSpec` per row **regardless of what the row's own
   Sub-issue cell currently says** — including rows that already look linked — and
   hand the whole batch to `CreateSubIssues` in one call. Its own idempotency check
   (`ListSubIssues` + marker match, `internal/github/subissues.go:300-321`) is the
   authoritative answer; a Story Index cell that was hand-edited, stale, or simply
   correct already produces `SubIssueExisted` and a same-value rewrite, which
   `Worktree.Commit`'s "no diff, no commit" behaviour makes free. This also means
   the phase never needs its own separate "is this row resolved" heuristic beyond
   deciding whether to even attempt the file at all.
4. **A missing or unparseable per-story file must not block its siblings.**
   `checkSubIssueSpecs` (`internal/github/subissues.go:428-467`) refuses the
   *entire batch* before sending anything if even one spec is malformed — so the
   phase must validate each story's spec locally (file exists, title/body build
   cleanly, marker is present in the body per
   `internal/report/subissue.go:153-181`) **before** adding it to the batch, and
   log-and-skip any story that fails that local check rather than let one broken
   story starve seven working ones. A skipped story's row is left exactly as it
   was, retried on a later turn once its file exists.
5. Call `opts.SubIssueCreator.CreateSubIssues(ctx, github.RepoFromRef(prep.ref),
   prep.ref.Issue, specs)` — `prep.ref.Issue` is the parent, by construction: one
   litefactory task branch belongs to exactly one GitHub issue, so there is no
   "parent issue" to parse out of `plan_brief.md`'s header at all.
6. Rewrite each row whose spec was in the batch, from the matching
   `SubIssueResult`:
   - `SubIssueCreated`, `SubIssueExisted` → write `HTMLURL` into the Sub-issue
     cell. Both carry a real, attached sub-issue's URL.
   - `SubIssueUnattached` → **write `HTMLURL` anyway.** The issue genuinely exists
     and is reachable at that URL; it is simply not yet nested under the parent as
     a formal sub-issue. See *Failure modes* for why this is the deliberate,
     documented choice rather than an oversight.
   - `SubIssueFailed`, `SubIssueNotAttempted` → leave the row exactly as it was.
     Nothing was created; a later turn's batch will include it again.

---

## Failure modes and their effect on the reported outcome

| Failure | What happens | Effect on `result`/the run's own report |
|---|---|---|
| No `plan_brief.md` locatable | Phase no-ops, debug log | None. Indistinguishable from an ordinary non-Plan turn. |
| A story's per-story file missing/malformed | That story excluded from the batch, `slog.Warn` naming the story id and reason | None. Other stories still attempted. |
| `ListSubIssues` fails (network, auth) — `CreateSubIssues`'s error return | Phase logs `slog.Error`, no commit2 attempted this turn | None. Retried next turn per Q1/Q3. |
| A story hits `SubIssueFailed` mid-batch | Batch stops (per `CreateSubIssues`'s own "stop at first failure"); earlier successes in this batch are still real issues and still get written back | None on `result`. The stories after the failure keep their unresolved rows for the next attempt — this is `CreateSubIssues`'s existing resumability, unchanged. |
| A story hits `SubIssueUnattached` | Logged loudly by story id and orphan URL; its `HTMLURL` is still written back (see above) | None on `result`. **Deferred**: no `AttachID`-based resume is wired, so this orphan is never formally re-attached by litefactory — a human or a future story does it by hand. Writing the URL back is what stops a *duplicate* orphan being created on the next retry (the row now reads as resolved, so this story is not attempted again). |
| Writing the rewritten `plan_brief.md` to disk fails (I/O) | `slog.Error`, no commit2 attempted | None. |
| commit2 fails | `slog.Error`, no push2 attempted. The already-created GitHub issues are unaffected — they exist regardless of whether the local write-back landed. | None on `result`. Retried next turn: the batch call is safe to repeat (`SubIssueExisted` for what already landed), only the local write is redone. |
| push2 fails | `slog.Error`, quoting `prep.worktree.Path()` the same way `save()`'s own push failure does (`internal/turn/harvest.go:256-265`) | None on `result`. The commit is intact locally and reaches the remote on the next successful push of this branch (including the next ordinary run's own push, since `Worktree.Push` publishes whatever HEAD currently is). |

**The one invariant across every row above: nothing this phase does can turn a
succeeded run into a failed one, or a failed run into a succeeded one.** `result.Outcome`,
`result.Reason`, `result.Err`, `result.Disposition` are set before `save()` runs and
are not touched by `fileStoryIssues`. The daemon's `run()` log line
(`internal/daemon/tick.go:736-745`) and `ReportTurn`'s posted comment continue to
describe exactly what they describe today. If the phase's own failures deserve a
visible signal, that is a `slog.Error` line an operator watching the daemon log
sees — not a change to the forge-facing report. Whether that is enough, or whether
a later story should add a distinct log-only counter/metric, is explicitly a
judgement call for whoever picks this up, not one this design makes for them.

---

## Acceptance criteria for this phase

Since the plan supplies none for this specific work, here is what I would hold it
to:

- **AC-W1** — Given a pushed commit whose worktree contains a `plan_brief.md` with
  at least one unresolved Story Index row and a matching well-formed per-story
  file, a subsequent turn's harvest creates the missing sub-issue(s) and rewrites
  the row(s) with a working link, without any comment, flag, or pointer file
  telling it to.
- **AC-W2** — Given the same starting state but no *new* turn ever touches
  `plan_brief.md` again, the next turn on the task — of any kind, any mode,
  regardless of what it changes — still completes the outstanding sub-issue
  creation. (Proves Q1/Q3's artifact-driven gating, not mode- or diff-driven.)
- **AC-W3** — A re-run after every row is already resolved makes no GitHub write
  call beyond the one `ListSubIssues` and produces no second commit
  (`Worktree.Commit`'s `Committed == false` path).
- **AC-W4** — One story's per-story file being missing does not prevent its
  siblings, in the same `plan_brief.md`, from being filed in the same turn.
- **AC-W5** — Every sub-issue's `Detail` link, when rendered, is pinned to a sha
  that is confirmed pushed (`result.Pushed == true` at the time it was used) —
  never an unpushed or guessed sha.
- **AC-W6** — The run's own posted report (`ReportTurn`'s comment) is
  byte-for-byte unaffected by this phase: same `Files`, same `CommitSHA`, same
  `SHAAfter` it would have had if this phase did not exist.
- **AC-W7** — Simulating a `ListSubIssues`/`CreateIssue`/`AddSubIssue` failure at
  each point in the batch leaves `result.Outcome`/`result.Disposition` unchanged
  from what they were after `save()`, and the runs row in `store` is unaffected.
- **AC-W8** — `TestNoForgeAbstraction`, `TestRevealIsCalledExactlyOnce`,
  `TestTheCredentialPathIsTheSDKs` (all in `internal/github`) stay green with no
  changes to their assertions.
- **AC-W9** — `internal/github` gains no new exported method, no retry loop, no
  sleep; `CreateSubIssues` is called exactly once per turn that attempts the
  phase, never in a loop-and-wait.
- **AC-W10** — Two Plan sessions on the same task branch and the same calendar
  date, with a colliding story id, is explicitly out of scope and not required to
  behave correctly (see *Deferred*).

---

## Deferred — flagged, not built here

- **`SubIssueUnattached` resumability.** No `AttachID` is threaded back into a
  future `SubIssueSpec`, so an orphaned, unattached issue is never formally
  reprioritised onto its parent by litefactory itself. The interim behaviour
  (write the orphan's own URL into the Story Index row so it reads as resolved and
  is not retried) avoids a worse outcome — a duplicate orphan on every retry — but
  it is a real, named gap against full AC-C1.3 ("each user story produces exactly
  one sub-issue" in the formally-nested sense). A future story can wire
  `AttachID` through a small persisted breadcrumb if this is judged worth fixing.
- **No schema change for the second commit's SHA.** `store.runs` already has one
  `commit_sha` column, used by commit1 (`internal/turn/harvest.go:308-313`).
  Commit2 is not recorded anywhere but the git log itself at v1. Adding a column
  is a genuine schema decision belonging to its own task, not a side effect of
  this wiring.
- **No daemon-level sweep for a task that receives no further comments at all.**
  This design's resumability relies on *some* future turn reaching `runAndHarvest`
  for the task. A plan that finished with unresolved rows and then never sees
  another comment stays unresolved indefinitely. Whether `Reconcile`
  (`internal/turn/execute.go:389-414`, run once at daemon start) or a periodic tick
  should also sweep for this is worth a follow-up, not decided here.
- **Extending the Implement gate.** `CLAUDE.md`'s gate is deliberately a pure
  local file-existence check on `plan_brief.md` (AC-E1.3, *"no mode gate makes a
  network call"*). Checking that every Story Index row also carries a link-shaped
  cell would still be network-free and would make AC-C1.3a enforced at the gate
  rather than merely produced by this phase — I think that is worth doing, but it
  touches a gate E1/T-C1.13 already shipped and is a decision for whoever owns
  that file next, not something I am changing here.
- **Two plan sessions on one branch with colliding story ids.** `StoryMarker` only
  carries `task=<ref>` and `story=<ID>` (`internal/report/marker.go:94-96`); a
  second plan session that reuses a story id from an earlier, still-open one on
  the same branch would have its story misread as already filed. Documented as
  unsupported rather than silently mishandled — this is an edge case worth a
  one-line callout in `internal/turn/doc.go` if this phase is built, not a
  behaviour to design around now.
