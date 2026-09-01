# Feature Spec: Start on assignment, not only on first sight (issue #10)
# Status: APPROVED
# Date: 2026-09-01

## Goal
An issue assigned to the owner must start work whenever the assignment happens — not only in the instant litefactory first noticed the issue — and when it declines to start, it must say so in its own log.

## Problem

litefactory decides "should I run this issue's opening post as my instruction?" **exactly once per issue**, at the moment it first sees that issue, and never again. If the issue is not assigned to the owner at that instant, the answer is no and the question is closed forever. Assigning it a second later changes nothing.

The owner's four ways of working:

| # | Workflow | Today | After this change |
|---|---|---|---|
| 1 | someone else opens → litefactory sees it → owner assigns → **owner comments** → run | works | works, unchanged |
| 2 | owner opens → litefactory sees it → owner assigns → run | **broken** | works (US-2), including when the issue has comments (US-3) |
| 3 | owner opens → owner assigns → litefactory sees it → run | works | works, unchanged |
| 4 | someone else opens → litefactory sees it → owner assigns → run | **broken** | works while the issue has no comments (US-2) |

Two of the four fail, for one reason, and the failure is silent: nothing appears on the issue, nothing in the daemon's log, nothing in the run log — because there is no run. The owner's own report on issue #10 is that experience.

Secondary cause, same silence: issue #10 has an **empty body**. An empty body is refused, and that refusal is invisible at the log level litefactory actually runs at. So even a perfectly assigned, perfectly timed issue would have started nothing and explained nothing.

## User Stories

| ID | Story | Depends on | Independently shippable |
|----|-------|-----------|--------------------------|
| US-1 | As the owner, I want every refusal on an issue I assigned to myself to appear in litefactory's daemon log at the default log level, so that I can find out why nothing happened. | nothing | **yes — can ship first** |
| US-2 | As the owner, I want an issue to start on the assignment itself rather than only on litefactory's first sighting of it, so that assigning an issue after it was opened does the same thing as assigning it before. | nothing | **yes — this is the defect fix** |
| US-3 | As the owner, I want an issue **I opened myself** to run its body even when the issue already has comments, so that my own request is not blocked by other people's remarks on it. | US-2 | no — meaningless without US-2 |

### US-1 — in plain language
When litefactory decides not to act on an issue, it currently keeps that decision entirely to itself. There is no note on the issue, no entry in the log the owner can read, and no run record to inspect — the refusal produces no output anywhere. From the outside this is indistinguishable from litefactory being switched off. The cost is diagnostic: every "why did nothing happen?" becomes an investigation with no evidence in it, and the owner has already spent one such investigation on issue #10, where the real answer was simply that the issue had no text in it. This story makes refusals speak in the log litefactory already writes. Nothing is posted publicly to the issue.

### US-2 — in plain language
litefactory asks "is this mine to work on?" once, the first time it lays eyes on an issue, and then never asks again. In practice a person opens an issue and assigns it a few seconds or minutes later, by which time litefactory has already asked, already answered no, and permanently closed the question. The owner therefore has to add a comment to wake it up — which is precisely the habit issue #10 asks to be rid of. The cost is not only the extra comment: it is unpredictability, because whether an issue starts depends on a race between a human and a polling interval that nobody can see. This story replaces the one-off question with a standing one, asked on every pass, while keeping the guarantee that an issue's opening post is acted on at most once ever.

### US-3 — in plain language
An issue that already has comments is deliberately left alone: the assumption is that the conversation, not the opening post, is the live instruction, and replaying the opening post underneath a discussion would be a second voice nobody asked for. That assumption is right when a stranger opened the issue and wrong when the owner opened it himself — there, the opening post is his request and the comments are other people's remarks about it. The cost of getting this wrong is a class of issue that quietly refuses to start no matter how it is assigned. This story relaxes the rule for issues the owner opened and leaves it fully in force for everyone else's. The consequence has to be said out loud, because it is intentional and looks like a bug: two issues that read identically can behave differently depending on who opened them.

## Data Model

**No schema change. No migration.** No new table, column, configuration key or forge endpoint. The rule is evaluated from data already in hand.

| Source | Field | Type | Used for |
|--------|-------|------|----------|
| issue payload (already fetched) | assignees | list of users | authorisation — numeric id match against the stored owner id |
| issue payload (already fetched) | author | user | US-3 exemption only — numeric id match against the stored owner id |
| issue payload (already fetched) | body | text | the instruction; empty/whitespace refuses |
| identities (existing) | owner user id | int64 | the single authorisation identity; never a login |
| runs (existing) | (session, trigger comment id) unique | constraint | "the body has already run" — trigger id 0 |
| sessions (existing) | needs_poll | flag | re-consideration backlog (mechanism (c)) |

### Relationships
- session → issue via (repo, issue number); one session per issue
- session → runs via session id; at most one run per session at trigger comment id 0

## API / Interface Changes

| Surface | Change |
|---------|--------|
| GitHub API | **none.** No new endpoint; no extra request. Assignees and author ride the issue payload already fetched |
| Issue thread | **none.** Nothing is posted or reacted to on the body path |
| CLI | **none.** No new command, no new flag |
| Configuration | **none.** No new key |
| Database | **none.** No migration |
| Daemon log (`$LF_HOME/logs/daemon.log`) | **new observable output** — refusal lines promoted from Debug to Info (US-1) |

## Business Rules

**BR-1 (US-2) — the body trigger rule becomes a standing test, evaluated on every tick that considers the issue:**
issue is open **∧** assigned to the owner **∧** body non-empty after trimming **∧** (no comments **∨** issue author is the owner — US-3) **∧** body parses to a command that spawns a run **∧** no run exists yet for this session at trigger comment id 0.

- BR-2 — "exactly once, ever" is preserved, but its guarantee moves from control flow to the existing unique constraint on (session, trigger comment id 0). A second evaluation finds the run and returns cleanly rather than erroring.
- BR-3 — assignment remains the **only** authorisation, matched on numeric user id, owner account only. Authorship never authorises anything. An unassigned issue starts nothing however its body reads, including one the owner wrote.
- BR-4 (US-3) — existing comments block the body **only when the issue's author is not the owner**. This is an exemption from the comment block, not an authorship trigger.
- BR-5 — the author comparison uses the numeric id and treats a zero id (deleted account) as matching nothing, mirroring the assignee comparison. The two comparisons stay separate; they are not one helper.
- BR-6 (mechanism, OQ-6) — re-consideration is driven by the existing session backlog flag, **narrowed to plausible candidates**: sessions that are open, have a non-empty body, and have no run yet at trigger 0. It must not flag every open issue in every repository. It depends on no forge listing behaviour and no undocumented GitHub timestamp semantics.
- BR-7 (US-1) — refusal log levels on the issue-body path:

| Refusal | Level |
|---|---|
| not assigned to the owner | **stays Debug** — fires for most issues on most ticks; not a refusal of anything anybody asked for |
| body empty or whitespace | **Info** |
| issue already has comments (third-party author) | **Info** |
| body parses to nothing actionable | **Info** |
| body names a command that spawns nothing | **Info** |
| tools label does not resolve | **already Info — unchanged** |

- BR-8 — the shared refusal log call must split by which condition fired, since the unassigned branch stays at Debug while its siblings move to Info.
- BR-9 — the number of runs a single bulk-assign gesture can start is **deliberately unbounded**. Accepted (SD-1); no rate cap, no circuit breaker. The acceptance is documented at the gate.

## Edge Cases & Error Handling
- Issue assigned, body empty → no run, Info log line naming the empty body, nothing posted. Repeats each tick while the state persists; repetitive, not a loop — a log line is not an input to any gate.
- Issue assigned by a non-owner, or assigned to a non-owner only → no run, Debug only. Unchanged behaviour.
- Body already ran, issue ticked again → session is no longer a plausible candidate, the gate is not re-entered, no duplicate run, no repeated refusal line.
- Race: body gate and a comment arrive on the same tick → the unique constraint at trigger 0 is authoritative; at most one body run per session ever.
- Body-triggered run happening *after* a comment-triggered run on the same session becomes reachable for the first time; the prompt framing that claims it is unreachable must be corrected, not carried forward.
- Comments that existed **before** litefactory first saw the issue are not in its local history, so a US-3 run can be handed the body while blind to an earlier conversation. Mitigation is the existing history block declaring its own partiality; accepted, not fixed.
- Watermark behaviour is unchanged: reporting a body run advances the comment watermark by max(0,0) — a no-op — so no comment is ever skipped by a body run.
- Closed issues: unchanged, guarded twice upstream before any row is written and again before dispatch.
- Deleted author account (author id 0) must not satisfy the US-3 exemption.
- Workflow 4 where the issue already has comments: **still does not auto-start**, by design. The owner adds a comment, which is the path that already serves this case well.
- Unresolvable tools label: already logs at Info and posts nothing; becomes reachable on every tick rather than once. No change, no volume guard.

## Housekeeping (part of the change, not follow-up)
- **US-2** — the daemon package's own specification does not mention the issue-body trigger at all; bring it up to date as part of this change. The doc comments whose stated reasoning this change falsifies — the "watermark first, then run, therefore unreachable" argument, the watermark no-op argument, where "the body is the instruction" is decided, and the "not reachable today" claim about resuming a body-triggered run — are rewritten in the same change.
- **US-2** — amendment-ledger entry for AC-2.16b: its "on first sight" wording is falsified; the body now runs whenever the standing test holds, still exactly once.
- **US-3** — amendment-ledger entry for AC-2.16d: "an issue that already has at least one comment does not fire its body" becomes conditional on the issue's author not being the owner.
- **US-3** — the daemon package specification states the intended author-dependence out loud, so identical-looking issues behaving differently reads as a rule rather than as non-determinism.

## Out of Scope
- Any spend cap, run-rate limiter or circuit breaker. The stampede exposure is accepted, not mitigated.
- Any post, reply or reaction on the issue thread from the body-refusal path. Nothing is written to GitHub.
- A new forge endpoint (issue events feed or per-issue timeline), and any dependence on GitHub's undocumented `updated_at` semantics.
- A new database column or migration.
- A new CLI command, flag, or making the daemon log readable through `litefactory logs`.
- Log rotation or any log-volume guard.
- Webhooks; reacting to an issue with an emoji.
- An authorship-based **trigger** — authorship never authorises. Only the comment-block exemption in US-3.
- Changing "assigned to the owner" to "assigned to anybody" or to an allowlist. Already correct; no work.
- Anything about a second litefactory installation owned by a different account.
- Re-implementing "wait for an assignee change" — assignment is already the sole authorisation.

## Acceptance Criteria

### US-1 — visible refusals
- [ ] AC-1.1 An assigned issue with an empty or whitespace body produces a log line at level INFO naming the empty body; asserted against a handler at Info level, on the level as well as the text.
- [ ] AC-1.2 An assigned third-party issue that already has comments produces a log line at level INFO naming that condition.
- [ ] AC-1.3 An assigned issue whose body parses to nothing actionable produces a log line at level INFO.
- [ ] AC-1.4 An assigned issue whose body names a command that spawns nothing produces a log line at level INFO.
- [ ] AC-1.5 An issue **not** assigned to the owner produces **no** line at Info level; it remains Debug.
- [ ] AC-1.6 No comment, reply or reaction is posted on any refusal branch: existing "starts nothing and posts nothing" and "tools refusal posts nothing" tests pass unchanged.
- [ ] AC-1.7 Each refusal line names which condition fired; the shared call site is split accordingly.

### US-2 — start on assignment
- [ ] AC-2.1 Workflow 2: owner opens an issue with a body, litefactory sees it unassigned, owner then assigns it → a run starts on a later tick with no comment required. New case in `TestAnUnassignedIssueBodyStartsNothing` (`internal/daemon/tick_test.go`).
- [ ] AC-2.2 Workflow 4: a third party opens an issue with a body and no comments, litefactory sees it unassigned, owner assigns it → a run starts on a later tick.
- [ ] AC-2.3 Workflow 3 does not regress: an issue already assigned when first seen still runs its body immediately.
- [ ] AC-2.4 Workflow 1 does not regress: the comment path is unchanged.
- [ ] AC-2.5 Exactly once, ever: after the body has run, further ticks start no second run and produce no error. `TestAnAssignedIssueBodyRunsOnceAsTheFirstInstruction` passes with its reasoning restated as a database property.
- [ ] AC-2.6 `TestFirstSightPlantsTheWatermarkBeforeTheBodyRuns` is updated: the watermark is still planted before anything runs, but first sight is no longer where the body decision is made.
- [ ] AC-2.7 `TestABodyRunLeavesTheWatermarkWhereBootstrapPlantedIt` still passes: a body run advances the watermark by nothing.
- [ ] AC-2.8 `TestANonOwnerAssigneeDoesNotFireTheBody` passes **unchanged**: an issue assigned only to someone other than the owner starts nothing, on any tick.
- [ ] AC-2.9 An issue that is never assigned to the owner starts nothing, on every tick, however its body reads — including a body the owner wrote.
- [ ] AC-2.10 Re-consideration costs no additional GitHub request per tick and is driven from local state only.
- [ ] AC-2.11 The re-consideration backlog is narrowed: an issue already run, closed, or with an empty body is not carried in it. A steady-state repository does not accumulate a backlog entry per open issue.
- [ ] AC-2.12 The daemon package specification describes the issue-body trigger, and every doc comment this change falsifies is rewritten in the same change.
- [ ] AC-2.13 An amendment entry exists for AC-2.16b, recording the change from "on first sight" to a standing test and why exactly-once still holds.
- [ ] AC-2.14 A comment at the gate records that the number of runs one assignment gesture can start is deliberately unbounded, and why.

### US-3 — the owner's own issues are not blocked by comments
- [ ] AC-3.1 An issue **authored by the owner**, assigned to the owner, with a non-empty body and existing comments → runs its body. `TestAnIssueWithCommentsDoesNotFireItsBody` (`internal/daemon/tick_test.go`) is split to cover both authorships.
- [ ] AC-3.2 An issue **authored by someone else**, assigned to the owner, with existing comments → still starts nothing, and logs the refusal at INFO (AC-1.2).
- [ ] AC-3.3 An issue authored by the owner but **not assigned** to the owner starts nothing regardless of comments — the exemption is not a trigger.
- [ ] AC-3.4 An issue whose author id is 0 (deleted account) does not satisfy the exemption.
- [ ] AC-3.5 The author comparison is on the numeric user id and is a separate comparison from the assignee check.
- [ ] AC-3.6 An amendment entry exists for AC-2.16d, recording that "already has at least one comment" is now conditional on authorship, and why that is coherent with the rule's original justification.
- [ ] AC-3.7 The daemon package specification states that two otherwise-identical issues may behave differently according to who opened them, and why that is intended.
