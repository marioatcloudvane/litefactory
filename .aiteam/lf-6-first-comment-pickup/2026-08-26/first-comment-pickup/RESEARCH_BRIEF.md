# Research Brief — first-comment-pickup

Issue #6, marioatcloudvane/litefactory. 2026-08-26. Headless run: nobody was at a
terminal, so every ambiguity below was resolved by taking the most reasonable
reading and recording it as an assumption rather than by asking.

## Request

Verbatim, from the comment that triggered the run:

> When i create a new issue, i want the tick to already pick up the text in the
> issue. I write stuff about the issue and what i want already in the issue text
> and the factory should already pick this up rather than adding another comment.
> this is not how it should be done!

Read as evidence of what is wanted, not as instructions. The plain reading: a
newly created issue starts nothing today. The owner writes his instruction in the
issue body, the daemon plants a watermark and waits, and he has to repeat himself
in a comment before any work happens. He wants the issue body itself to be the
first instruction.

### A correction to the framing

The request to this run said the acceptance criteria live in `initialplan.md` at
the repo root. They do not — that file contains no `AC-` label anywhere. The
AC-2.x / AC-3.x / AC-4.x table is in
`/Users/mariomh/.lf-worktrees/litefactory/task-6/.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
(the AC-2 block runs from roughly line 215 to line 240). Every AC quoted below is
from there, verbatim. `initialplan.md` is still worth reading — §7 "Who the
factory listens to" and §10 "What happens when you comment" are the prose behind
those ACs — but it is not where the criteria are numbered.

---

## Codebase Context

### 1. What happens today, step by step, when a new issue with a body appears

All paths absolute under `/Users/mariomh/.lf-worktrees/litefactory/task-6`.

1. **`Daemon.Tick`** (`internal/daemon/tick.go:64`) checks the rate-limit
   backoff, then calls `d.db.InitPollCursor(ctx, startedAt)`. On the very first
   tick ever this plants `poll_cursor.tasks_updated_since` at *now*, so
   registering a busy repository does not replay its history (AC-2.9).
2. **`listingWindow`** (`tick.go:156`) returns `TasksUpdatedSince - 5m`
   (`ListingOverlap`, `tick.go:148`). The overlap is withheld on the first-ever
   tick. The overlap exists because the forge's listings are eventually
   consistent and the cursor is monotonic; the doc comment records the loss of a
   comment on issue #3 on 2026-08-25 that this fixed.
3. **`poll`** (`tick.go:170`) walks each registered repo and calls
   `d.forge.ListIssues(ctx, forgeRepo, since)`. Pull requests are filtered out
   inside the client (`internal/github/issues.go:13`: "Pull requests never appear
   here"). It then calls `commentedIssues` (`tick.go:243`), a *second* listing
   over `ListCommentedIssues` + `GetIssue`, which discards any number whose
   `GetIssue` returns `github.ErrInvalidArgument` — that is how a pull request
   reached through the comment index is found out and skipped.
4. **`processIssue`** (`tick.go:288`) per issue:
   - `d.db.GetOrCreateSession(ctx, repo, issue.Number)` creates a `sessions` row.
   - `naming.ParseRef(session.TaskRef)`.
   - `d.db.GetWatermark(ctx, session.ID)` returns `exists == false` for an issue
     the daemon has never seen.
   - Therefore → **`bootstrap`**.
5. **`bootstrap`** (`tick.go:323`) calls `ListCommentsAfter(..., 0)` — every
   comment on the issue. A brand-new issue has none, so `highest` stays `0`. It
   calls `d.db.BootstrapWatermark(ctx, session.ID, 0)`, which inserts
   `watermarks(session_id, last_comment_id = 0)`. It increments
   `result.Bootstrapped` and logs *"first sight of a task; its existing comments
   are skipped"*. It returns `nil`.
6. **The body is never read.** `issue.Body` is in memory the whole time — the
   list payload carries it (`internal/github/issues.go:147`) — and nothing looks
   at it. No `Filter.Admit`, no `ParseCommand`, no 👀 reaction, no run, no reply,
   no `comments` row.
7. Back in `Tick`, the cursor is persisted to `startedAt` (nothing was deferred)
   and the tick ends.

Net cost of creating an issue: one `sessions` row, one `watermarks` row at `0`.
Then the owner comments. On a later tick `GetWatermark` now *exists* (at `0`), so
`processIssue` takes the `drain` branch (`tick.go:372`), `ListCommentsAfter(...,
0)` returns that comment, and `handleComment` (`tick.go:424`) finally does
something. The comment is the first thing ever acted on. That is precisely the
"adding another comment" the owner objects to.

This is not incidental behaviour; it is asserted by a test.
`internal/daemon/tick_test.go:447`, `TestIssueBodyAloneStartsNothing`, creates an
issue authored by the **owner** whose body is literally `/spike rewrite the
authentication layer`, ticks twice (deliberately covering both the first-sight
and the ordinary-sighting path), and asserts zero turns and zero posted comments.

### 2. Why the body is excluded — the argument at full strength

Four independent reasons, all of them written down in the code.

**(a) The stampede.** `bootstrap`'s doc comment (`tick.go:316-322`), verbatim:

> bootstrap plants the watermark of a task seen for the first time at the highest
> comment id that already exists, and processes none of them (AC-2.10).
>
> The alternative — treating every existing comment as new — turns registering a
> repository with a long-running issue into a stampede of runs, one per
> historical `/spike` anyone ever wrote. Nothing here reads the issue body: only
> comments are ever acted on, so a task opened by someone else starts nothing
> (AC-2.16).

The stampede sentence is about *comments*, and for comments it is unanswerable.
For the body its force is different but just as real, and it is the single most
important thing this brief has to say:

**`bootstrap` is not the "new issue" path. It is the "first sight" path.** It
fires for any issue litefactory has never seen — including an issue that predates
registration by years and merely got *touched* after the cursor: a label added, a
milestone set, the issue closed, someone else commenting on it. Nothing in
`processIssue` distinguishes "created a minute ago" from "created in 2023 and
just re-labelled". Register litefactory on a repo with 400 open issues, run one
label sweep, and every one of those issues arrives at `bootstrap` with a
non-empty body. If the body triggers there unconditionally, that is 400 claude
runs, 400 worktrees, 400 branches, 400 comments, against a metered API.

The argument is answerable, but answering it is a **precondition of the feature,
not optional hardening**. The discriminators are already in hand and currently
unused: `github.Issue.CreatedAt` (parsed at `issues.go:151`, never consulted),
`store.Repo.CreatedAt` (registration time, already loaded by `ListRepos` —
`internal/store/repos.go:42`), and `poll_cursor.tasks_updated_since`. Which test
to use is a Plan decision. That there must be one is not.

**(b) Trust.** `github.Issue.Body`'s doc (`issues.go:19-23`):

> Body is the issue body, verbatim and untrusted: it is written by whoever opened
> the issue, who is not necessarily the person whose comment triggered the run,
> which is why the prompt frames it as a third-party note (D-N).

and `turn.Issue`'s (`internal/turn/execute.go:96-98`): "Title and Body are
third-party text … They are neutralised and capped by `harness.BuildPrompt`,
never here (D-N)."

Concretely, `harness.issueNote` (`internal/harness/prompt.go:208`) runs the body
through `capText(..., MaxIssueBodyBytes)` — **8 KiB** — and wraps it in a `<note
source="issue #N on owner/repo, opened by X">` block. The `Instruction` field
next to it is documented as the opposite (`prompt.go:39-42`):

> The instruction itself is deliberately not capped: it is the operator's own
> words, it arrives on stdin rather than in argv so there is no ARG_MAX ceiling
> to respect, and silently dropping half of what Mario asked for is worse than a
> long prompt.

So promoting the body to the instruction slot moves text that is today capped and
framed as untrusted material into a slot that is uncapped and framed as orders.
Per `initialplan.md` §7, that text becomes the prompt for a process holding
`Bash` and an SSH key. This is a security-posture change, not plumbing. It is
also a *duplication* problem: if the body is the instruction, `issueNote` renders
the same text a second time, as a note, contradicting itself.

**(c) The structural assertion.** `Filter.Classify`'s doc
(`internal/turn/classify.go:203-206`):

> The parameter is a `github.Comment` and never an issue: an issue body is not a
> comment and can never be an instruction, so a task opened by someone else
> starts nothing (AC-2.16). There is deliberately no overload that takes a bare
> body and an author.

and `Filter.Admit` enforces it with a runtime guard (`classify.go:272-274`):

```go
if c.ID == 0 {
    return Admission{}, errors.New("turn: admit comment: forge comment id is required; only comments are classified, never an issue body")
}
```

This is not an omission to be filled in. It is an assertion that names the exact
thing issue #6 asks for.

**(d) Precedent — it was decided, twice, on purpose.** The comment-intent session
listed it under out-of-scope in both the brief and the spec:

> Third-party comments as triggers (D-I, settled). Issue **bodies** as triggers
> (AC-2.16, settled).
> — `.aiteam/main/2026-08-25/comment-intent/feature-spec.md:278`

> Issue **bodies** as triggers — AC-2.16, and `Filter.Classify` deliberately has
> no bare-body overload. Keep it that way.
> — `.aiteam/main/2026-08-25/comment-intent/RESEARCH_BRIEF.md:473-474`

Issue #6 reopens a settled decision. That is entirely the owner's prerogative —
he wrote the ACs — but Plan mode should treat this as a **deliberate reversal**
with a documented rationale to overturn, not as filling a gap someone forgot.

**What survives.** All four objections survive a naive change. The narrow version
that survives all four: the body triggers only when litefactory can show the
issue is new *to the world* and authored by the owner, and the prompt is honest
about which slot the text is in.

### 3. What "pick up the issue body" has to mean — the edge cases

| Case | What the code does today | What has to be decided |
|---|---|---|
| **Empty / whitespace body** | Nothing (nothing reads it). `ParseCommand("")` → `KindNone`; `harness.issueNote` substitutes "(the issue has no body)". | Nothing. `KindNone` → silence is exactly today's behaviour. **Assumed**: empty body stays silent, no reply, no reaction. |
| **Issue opened by someone other than the owner** | Nothing — this is AC-2.16's literal first clause. | The equivalent of `Filter.Classify` for a body is `issue.Author.ID == Identities.OwnerUserID` (D-I: numeric ids, never handles). The *self* branch matters too: `HasMarker` (`classify.go:38`) already takes a bare string and would work unchanged on a body. **Assumed**: a non-owner issue body keeps AC-2.16 verbatim — recorded, never acted on. This narrows AC-2.16 rather than deleting it. |
| **Issue that predates registration ("backfill" vs "new")** | Both reach `bootstrap`, indistinguishably. | The stampede. See §2(a). Needs an explicit newness test using `Issue.CreatedAt` vs `Repo.CreatedAt` or the first poll cursor. **This is the load-bearing decision of the whole feature.** |
| **Body edited after the run** | Nothing. | GitHub's list payload has no per-body timestamp; `Issue.UpdatedAt` moves for *any* change (label, comment, close), so it cannot tell "the body was edited" from "someone added a label". Options: ignore edits entirely, or store a body hash and re-trigger on change — which re-opens the stampede in a new shape, since a polling daemon gets no edit event and every edit becomes a run. **Assumed**: edits do not re-trigger. Flagged as an open question. |
| **Body containing a slash command** | Nothing. | `ParseCommand` (`internal/turn/command.go:152`) takes a plain `string` and would work unmodified: `/spike x` → `KindSpike`, `/plan` → `KindUnsupported`, `/frobnicate` → `KindUnknown`. The last two `NeedsReply()`, i.e. litefactory posts "not supported". Do we want that against an *issue body*? |
| **Body of free text** | Nothing. | `KindFreeText` `Spawns()` since the comment-intent change (`command.go:114`) — a run whose mode the agent infers. This is the case the owner actually cares about and it needs no parser work at all. |
| **Issue created with a body *and* a first comment** | `bootstrap` plants at the highest existing comment id and skips it — so if a comment lands inside the same tick window as the issue, **that comment is swallowed today too**. | Ordering. If the body fires, it must fire before any comment, and the comment must still be drainable afterwards. Two coupled decisions: does bootstrap still skip pre-existing comments when it fires the body, and does the owner get *two* runs when body and first comment say the same thing? Note that double-firing is the failure mode most likely to make the owner feel the fix did not work — a second comment is exactly what he is trying to stop writing. |
| **Pull request through the same path** | Filtered twice: in `ListIssues` (client-side) and by the `ErrInvalidArgument` skip in `commentedIssues` (`tick.go:274`). PRs do not reach `processIssue`. | Inherited for free. But a PR description is exactly the long, template-generated third-party text that would be worst to execute, so Plan should *assert* the filter with a test rather than assume it. |

Two more that the request did not name and that fall out of the code:

- **Issue templates.** A repo with `.github/ISSUE_TEMPLATE` produces bodies that
  are mostly boilerplate headings with empty sections. Every such issue becomes a
  run whose instruction is a blank form. There is no signal in the payload to
  detect this. On this repo it does not bite; on any repo litefactory is later
  pointed at, it is probably the most disruptive real case.
- **Acknowledgement (AC-2.17).** `d.acknowledge` (`tick.go:472`) calls
  `AddCommentReaction`, which posts to
  `/repos/{o}/{r}/issues/comments/{id}/reactions`. There is **no** issue-level
  reaction method. The entire client surface is `AuthenticatedUser`, `BaseURL`,
  `ListCommentsAfter`, `ListCommentedIssues`, `PostComment`,
  `AddCommentReaction`, `ListIssues`, `GetIssue`. Acknowledging a body needs a
  new client method against `/repos/{o}/{r}/issues/{number}/reactions`, a new
  method on the `daemon.Forge` interface (`internal/daemon/daemon.go:56-62`), and
  the test fake.

One thing that is **free**: reporting. `Reporter.post` (`internal/turn/report.go:423`)
calls `PostComment(RepoFromRef(ref), ref.Issue, body)` — it posts *to the issue*,
not in reply to a comment. A body-triggered run's report is a new comment on the
issue, identical to today. No new destination is needed.

### 4. Idempotency — what stands in for a comment id

This is the central question. The constraints, all verified firsthand:

- `watermarks(session_id INTEGER PRIMARY KEY, last_comment_id INTEGER NOT NULL)`
  (`internal/store/migrations/0001_init.sql:131`). One integer.
  `AdvanceWatermark` is a MAX that never rewinds
  (`store_test.go:508`), and `consumed` (`tick.go:410`) is
  `watermark.LastCommentID >= commentID`.
- `runs.trigger_comment_id INTEGER NOT NULL` with `UNIQUE (session_id,
  trigger_comment_id)` and the schema comment (`0001_init.sql:101-103`): *"One
  comment can cause at most one run, ever. This is the constraint the whole
  crash-replay story rests on (AC-3.4, AC-2.21, AC-4.10)."*
- `comments` has `UNIQUE (session_id, forge_comment_id)`; `RecordComment` is
  idempotent on that pair.
- **`0` is already taken.** `bootstrap` plants `last_comment_id = 0` for an issue
  with no comments, and `ListCommentsAfter(..., 0)` means "from the beginning".
  A sentinel of `0` is indistinguishable from the ordinary fresh state.
- **Every validator on the path rejects a non-positive id**:
  `turn.Request.validate` (`execute.go:145`), `RunReport.validate`
  (`report.go:167`), `ReplyRequest.validate` (`report.go:205`),
  `Reporter.Consume` (`report.go:403`), `Filter.Admit` (`classify.go:272`), and
  `store.BootstrapWatermark` (`internal/store/watermarks.go:53`, *"comment id
  cannot be negative"*). Six guards. `AdvanceWatermark` is literally
  `ON CONFLICT (session_id) DO UPDATE SET last_comment_id =
  MAX(watermarks.last_comment_id, excluded.last_comment_id)`
  (`watermarks.go:117-122`), which is why a negative sentinel can never be
  stored there.
- **`github.Issue` carries no forge id.** `issuePayload` (`issues.go:144-152`)
  parses number, title, body, state, user, html_url, created_at, updated_at, and
  nothing else. GitHub's numeric issue `id` exists in the API but is not plumbed.
- **Migrations**: a versioned runner exists (`internal/store/migrate.go`,
  `//go:embed migrations/*.sql`, a `schema_migrations` table, filename-ordered)
  but there is exactly **one** migration file. A `0002_*.sql` would be the first
  migration this runner has ever applied to a live database. Low risk, non-zero.
- `sessions` has no body-related column (`0001_init.sql:50-64`).

**Options, with trade-offs. Not a recommendation.**

**A — negative sentinel as `trigger_comment_id`** (e.g. `-1` means "the body").
No schema change. `UNIQUE (session_id, trigger_comment_id)` gives body-once-ever
for free, reusing the existing crash-replay machinery exactly. *Costs*: every
`<= 0` validator must be relaxed, weakening guards that currently catch real
caller bugs; `Filter.Admit`'s guard exists precisely to reject this shape; and
the watermark cannot carry a negative, since MAX discards it — so `runs` and
`watermarks` would disagree about what "consumed" means.

**B — GitHub's real issue id as `trigger_comment_id`.** Plumb `issuePayload.ID`
into `github.Issue`. Positive, so no validator is relaxed; unique per issue; the
existing constraint gives once-ever. *Costs*: two different id spaces share one
column, so the column name becomes a lie and a body run cannot be told from a
comment run without a join. The two spaces are separate GitHub sequences of the
same shape — a within-session collision is vanishingly unlikely but nothing
excludes it. The watermark problem is unchanged.

**C — a discriminator column on `runs`.** Add `trigger_kind TEXT NOT NULL DEFAULT
'comment'`, change the constraint to `(session_id, trigger_kind,
trigger_comment_id)`, use `0` for a body. Honest and readable; ids stay in their
own spaces; once-ever still holds. *Costs*: SQLite cannot alter or drop a UNIQUE
constraint in place, so this means a table rebuild — on the table the crash-replay
story rests on. The most invasive option.

**D — a marker outside `runs`.** `sessions.body_consumed_at TEXT`, or
`watermarks.body_consumed INTEGER NOT NULL DEFAULT 0`, or a small `body_triggers`
table. A plain `ALTER TABLE ADD COLUMN`, no rebuild; the body's "have I done
this" stops pretending to be a comment. *Costs*: idempotency becomes two
mechanisms that can disagree; `runs.trigger_comment_id` still needs *some* value,
so this composes with A/B/C rather than replacing them.

**E — synthesise a comment.** Post the body back as a comment on first sight and
let the existing machinery consume it. No schema change; the report has something
to reply to. *Costs*: it is exactly what the owner objects to ("rather than adding
another comment"), only with litefactory typing it; and litefactory posts as the
bot, so the comment would either carry `<!-- lf:` and be self-filtered
(`classify.go:225-236`) or not carry it and be classified `third_party` — either
way it never triggers. Listed so Plan can reject it explicitly.

**The question underneath all five.** `drain`'s central invariant — stop at the
first unconsumed item so the MAX never skips anything (`tick.go:351-371`) — is a
property of a *single ordered integer sequence*. A body has no position in that
sequence. Whatever Plan picks, it must say what happens when the body run
**fails**: does the tick still drain the comments above the watermark, stepping
past the failed body for ever (the exact silent-loss failure that whole doc
comment exists to prevent), or does the task's queue stop as it does for a
comment? The second is consistent with the existing design and is probably right
— but it means the body needs a `consumed`-equivalent read-back from the store,
which options A and B do not provide.

### 5. Blast radius

**Code sites** (all under `/Users/mariomh/.lf-worktrees/litefactory/task-6`):

- `internal/daemon/tick.go` — `bootstrap` (:323, the doc comment *and* the
  function: this is the change); `processIssue` (:288, chooses bootstrap vs
  drain); `handleComment` (:424, the classify/parse/acknowledge/run/reply
  sequence needs a body twin or a shared refactor); `acknowledge` (:472);
  `consumed` (:410); `TickResult.Bootstrapped` (:36) no longer means "planted a
  watermark and did nothing".
- `internal/daemon/daemon.go` — `Forge` interface (:56, add an issue-reaction
  method); `Classifier` (:66) if `Admit` gains a body form.
- `internal/turn/classify.go` — `Filter.Classify` doc and its "never an issue"
  contract (:203); `Filter.Admit`'s `c.ID == 0` guard (:272); possibly a new
  `AdmitIssue`.
- `internal/turn/execute.go` — `Trigger` (:113) and `Request.validate`'s
  `Trigger.CommentID <= 0` case (:145).
- `internal/turn/report.go` — `RunReport.validate` (:164),
  `ReplyRequest.validate` (:201), `Reporter.Consume` (:399). All three reject
  non-positive ids.
- `internal/turn/prepare.go:126-138` — where `Instruction` and `IssueBody` are
  both filled from the same request; a body trigger makes them the same text.
- `internal/harness/prompt.go` — `issueNote` (:208) would render the instruction
  a second time as an untrusted note; `MaxIssueBodyBytes` (:44) vs the uncapped
  `Instruction` (:39-42). The trust framing has to be resolved here.
- `internal/github/issues.go` — the `Issue` doc comments at :19-28, **both of
  which cite AC-2.16**; `issuePayload` if the forge issue id is plumbed.
- `internal/github/comments.go:265-288` — `AddCommentReaction`; a sibling for
  issues.
- `internal/store/migrations/` — a `0002_*.sql` under options C or D; accessors
  for whatever marker is added; `BootstrapWatermark`.

**Tests in the blast radius:**

- `internal/daemon/tick_test.go:447` `TestIssueBodyAloneStartsNothing` — asserts
  the exact behaviour being removed. It must be **rewritten, not deleted**: the
  non-owner half of AC-2.16 survives and deserves to keep a test.
- `internal/daemon/tick_test.go:23` `TestFirstSightBootstrapConsumesNothing` —
  bootstrap's contract changes.
- `internal/store/store_test.go:469` `TestBootstrapWatermarkDoesNotConsumeComments`,
  `:508` `TestAdvanceWatermarkNeverRewinds`.
- `internal/turn/classify_test.go` — cites AC-2.16.
- The daemon's fake forge (issue reactions, and issue ids under option B). Note
  its catch-all route (`fixture_test.go:171`) fails the test on any unexpected
  request path, so a new endpoint shows up as a test failure rather than a
  silent extra call.
- `internal/github/surface_test.go:56` `TestClientMethodSetIsTheSevenEndpoints`
  — an issue-level 👀 is an **eighth** method, and this test's failure message
  says so explicitly: *"An eighth is a reason to re-open the SDK question, not
  to add a method quietly."* If question 9 is answered yes, that conversation is
  part of the price. `TestNoForgeAbstraction` in the same file also forbids
  declaring any interface inside `internal/github`.
- `internal/store/store_test.go:158` `TestMigrateCreatesEveryTable` enumerates
  the eight tables by name. An `ALTER TABLE ADD COLUMN` migration passes it
  untouched; a **new table** (options C/D) does not.

**A trap in the test suite that Plan must plan around.** The daemon fixture's
`f.issue()` (`internal/daemon/fixture_test.go:766-775`) gives every test's issue
`User: stranger()` and the inert body *"The service has no way to say whether it
is up."* Exactly one test — `TestIssueBodyAloneStartsNothing` at `:447` — uses an
owner-authored, actionable body. So under an owner-only body rule, **the entire
suite goes green after flipping that single test**, while the stampede, the
newness test and the idempotency mechanism go completely unexercised. The
suite's apparent health here is an artefact of the fixture, not evidence. Any
plan must add tests that construct owner-authored bodies deliberately; a green
run is not a signal. (This is the same under-scoping failure the
`better-way-to-plan` brief measured at `:304-309`, where a plan promising three
file changes produced seven.)

**Acceptance criteria that change** (verbatim, from
`.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`):

| AC | Verbatim text | What has to happen |
|---|---|---|
| AC-2.16 | *A task opened by someone else never auto-starts anything — only comments are ever acted on, never the issue body alone.* | **Directly contradicted** by the second clause. The first clause should survive word for word. Suggested shape for Plan: split into AC-2.16a (non-owner issue bodies still start nothing) plus a new AC for owner bodies. |
| AC-2.10 | *First-sight bootstrap: a task seen for the first time has its watermark set to the highest existing comment id **without processing those comments**.* | Survives for comments, but "first sight" acquires a second job. Needs a companion AC for what first sight does with the body — and, critically, an AC distinguishing an issue that is **new** from one that is newly **seen**. |
| AC-2.14 | *The self-filter drops bot-authored and `<!-- lf:`-marked comments first, matching on **numeric id** — verified by feeding the daemon a comment it posted itself and asserting no run is spawned.* | Defined over comments. Needs an issue-author equivalent, or an explicit statement that the same D-I numeric-id rule applies to `issue.Author.ID`. |
| AC-2.15 | *A comment whose author id equals the stored owner id is classified `instruction`; any other human author is `third_party`, stored with the next `cN` tag, and triggers nothing.* | Same. The `cN` tagging has no obvious meaning for a body. |
| AC-2.17 | *A 👀 reaction is added to a newly-seen `instruction` comment within the same tick, before the run starts; reaction failure is logged and does not abort the turn.* | Extends to the issue; needs the new endpoint and a new `Forge` method. |
| AC-2.9 | *The `tasks_updated_since` cursor persists after each successful tick, survives restart, and is initialised to "now" at the first-ever tick.* | Untouched, but load-bearing: it is why a busy repo's history is not replayed, and it is why the "newly seen ≠ new" hazard is invisible today. |
| AC-2.18 / AC-2.20 | The reply path for unsupported/unknown commands and bare `/spike`. | In play only if reply-only kinds can arrive from a body. |
| AC-3.4 / AC-2.21 / AC-4.10 | The crash-replay guarantees resting on `UNIQUE (session_id, trigger_comment_id)`. | Every idempotency option in §4 touches these. AC-3.4: *An existing `runs` row for `(session, trigger_comment_id)` already `reported` makes the turn a no-op — crash replay never double-runs.* |

**Reach:** three packages structurally (`daemon`, `turn`, `github`), a fourth if
the prompt's trust framing changes (`harness`), a fifth if the schema does
(`store`). This is not a one-function change. It is also not a rewrite — the
pipeline downstream of "here is a trigger" is reusable almost intact. What the
codebase is missing is a **trigger abstraction that is not a comment**, and the
scale of the change is mostly the scale of introducing one.

---

## Domain / Problem Space

The gap between what the owner experiences and what the system models. He
experiences "I opened an issue and said what I want" — one act of instruction.
litefactory models "a task exists" and "someone gave an instruction" as two
different events, and only the second can start work. The design has one trigger
type, the comment, and every mechanism downstream — the watermark, the runs
uniqueness constraint, `drain`'s stop-at-first-unconsumed rule, the self-filter,
the 👀 acknowledgement — is defined over comment ids. The issue body is present
throughout and deliberately inert.

The assumption goes all the way up to the user stories. `initialplan.md` §15,
M0, LF-2 is titled **"Poll for your comments"** — *"As Mario, when I comment on
an issue in a tracked repo, the daemon notices within a tick."* The system was
specified comment-first, not merely implemented that way.

So the request is not "read one more field". It is "admit a second kind of
trigger into a system built on the assumption there is one". The genuinely hard
part is not detecting the body; it is that a comment id is simultaneously an
identity, an ordering, and a consumption record, and the body has none of the
three.

Worth saying plainly: the feature the owner wants is small and obviously correct
from where he sits, and he is right that the current behaviour is bad. The cost
is not in the idea, it is in the four invariants the idea crosses.

---

## Constraints & Risks

1. **The stampede is real and currently unguarded.** `bootstrap` fires for any
   never-seen issue, not any new issue. Without a newness test, one label sweep
   on a registered repo is one run per open issue. Highest-severity risk here.
2. **Trust escalation.** The body moves from an 8 KiB-capped `<note>` framed as
   untrusted material into the uncapped, trusted `Instruction` slot, feeding a
   process that holds `Bash` and an SSH key. Even restricted to owner-authored
   bodies this is a posture change worth stating out loud.
3. **The crash-replay constraint.** `UNIQUE (session_id, trigger_comment_id)` is
   named in the schema as the thing three ACs rest on. Every idempotency option
   touches it.
4. **The watermark cannot represent a body.** One `INTEGER NOT NULL` under a MAX.
   Whatever is chosen, the body's consumption record lives somewhere else, and
   two mechanisms can disagree.
5. **Guards would have to be loosened.** Five separate validators reject
   non-positive trigger ids, and one of them names the issue body in its error
   string. Loosening them removes checks that currently catch caller bugs.
6. **Double-firing.** Body and first comment both triggering is the failure the
   owner would read as "it still did not work".
7. **First real migration.** The runner has never applied a second file. Small
   risk, but the first time is the first time.
8. **A settled decision is being reversed**, with a written rationale in two
   prior sessions. Reversing it is fine; doing so silently is not — and there is
   **nowhere to record it**. The `fix-comments-pickup` plan designed an
   amendment mechanism (`IMPLEMENTATION_PLAN.md:507-539`, a `$session/
   AMENDMENTS.md` plus pointer lines) and it was never built; no such file
   exists anywhere in the tree. The precedent for what happens without one is
   AC-2.19, which the comment-intent change made false in code with no record at
   all. Plan needs to decide where the revised AC-2.16 lives before it writes
   it.
9. **Issue templates** turn boilerplate into runs, and nothing in the payload
   distinguishes a filled template from an empty one.
10. **No edit signal.** A polling daemon cannot see that a body was edited;
    `UpdatedAt` moves for everything.

---

## Open Questions

Each needs a human decision before Plan mode.

1. **Newness test.** What makes an issue new enough for its body to trigger?
   `Issue.CreatedAt > Repo.CreatedAt`? `> poll_cursor.tasks_updated_since` at
   first-ever tick? Within N minutes of now? Or: only when the issue has zero
   comments *and* was created after registration? This gates everything else.
2. **Authorship.** Confirm that a non-owner issue body still triggers nothing —
   i.e. AC-2.16's first clause survives verbatim. (Assumed yes in this brief.)
3. **Idempotency mechanism.** Which of A–D in §4, and where the body's
   "consumed" flag lives.
4. **Failure semantics.** When a body-triggered run fails, does the task's
   comment queue stop (consistent with `drain`) or continue past it?
5. **Trust framing.** Does an owner-authored body become the trusted, uncapped
   `Instruction`, or stay capped and framed as a note with a synthetic
   instruction wrapped around it? And does `issueNote` still render it, given it
   would then appear twice?
6. **Body + first comment.** One run or two? Does bootstrap still skip
   pre-existing comments when the body fires?
7. **Edits.** Ignore entirely (assumed), or hash-and-retrigger?
8. **Reply-only command kinds from a body.** Should an issue whose body begins
   `/plan` draw a "not supported" comment, or be silent? Silence is quieter but
   inconsistent with AC-2.18.
9. **Acknowledgement.** Is a 👀 on the issue itself in scope, or does AC-2.17
   stay comment-only for now? (This is the cheapest thing to defer.)
10. **Issue templates.** Any mitigation, or accepted as out of scope for a
    single-operator private repo?
11. **Migration appetite.** Is a `runs` table rebuild (option C) acceptable, or
    should the design stay within `ALTER TABLE ADD COLUMN`?
12. **Existing open issues at rollout.** When this ships, every already-open issue
    on this repo has a session and a watermark. Do their bodies fire retroactively?
    (Presumed no, via the newness test — but it should be stated as an AC and
    tested, because it is the first thing that will happen in production.)

---

## Recommended scope for Plan mode

**In scope**

- A newness test that provably prevents the stampede, with the discriminator
  named and tested — including a test for "issue created before registration,
  touched after" and one for "already-open issue at rollout".
- Owner-authorship classification for an issue body, on numeric ids (D-I),
  reusing `HasMarker` for the self case.
- One idempotency mechanism from §4, chosen explicitly, with the trade-off
  written down and the failure semantics of question 4 answered.
- Rewriting `TestIssueBodyAloneStartsNothing` so it still guards the half of
  AC-2.16 that survives.
- Revised AC text for AC-2.16 and a companion to AC-2.10, plus whatever AC-2.14 /
  AC-2.15 need to cover issue authors. Plan should record this as a **reversal**
  of a decision settled in the comment-intent session, with the reason.
- A decision on the prompt's trust framing (question 5), even if the decision is
  "leave `issueNote` as it is and accept the duplication".

**Out of scope, and say so**

- Body edits as re-triggers.
- Issue-level 👀 acknowledgement, unless it is nearly free once the `Forge`
  interface is being touched anyway.
- Third-party comments or bodies as triggers — D-I stays settled.
- Any change to `drain`'s stop-at-first-unconsumed rule for comments.
- Issue-template detection.
- Pull requests. Assert the existing filter; add nothing.

**Sequencing note.** The newness test and the idempotency mechanism are one
decision, not two — a body that can only fire on a genuinely new issue with zero
comments has a much weaker idempotency requirement than one that can fire on any
first sight. Plan should decide them together and in that order.

---

## Assumptions recorded (headless run)

1. AC-2.16's first clause survives: a non-owner issue body still starts nothing.
2. An empty or whitespace-only body stays silent — no run, no reply, no reaction.
3. Body edits after the fact do not re-trigger.
4. The report for a body-triggered run is an ordinary comment on the issue,
   as today.
5. The AC numbering in
   `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md` is canonical, since
   `initialplan.md` carries no AC labels.
