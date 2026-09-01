# Manifest
# Branch: lf-10-we-need-to-start-immediately-on-a-new
# Session: 2026-09-01

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — 2026-09-01, revised in place by runs 0002 and 0003)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(done — 2026-09-01, three stories: `US-1.md`, `US-2.md`, `US-3.md`)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-01 — session created. Headless run 0001 from a comment on issue #10 with no slash
  command. Classified as **Research**: the comment asks for new trigger conditions
  ("first sight of an issue", "assignee change") to start a run, which is a feature request
  against `internal/daemon`'s discovery/eligibility path, not an approved plan. No
  `RESEARCH_BRIEF.md` existed in this session, so the Plan gate was closed anyway.
- 2026-09-01 — Research complete. `RESEARCH_BRIEF.md` written. Headline: three of the owner's four
  workflows already ship under AC-2.16a–d (issue #6); the single gap is that the issue-body gate is
  evaluated once, inside first-sight bootstrap (`internal/daemon/tick.go:940-948, :1015, :1074-1125`),
  and is unreachable afterwards. Workflows 2 and 4 are the same rule. Seven open questions recorded,
  the first of which (does GitHub bump `updated_at` on an assignment change?) decides the size of the
  change. Research is closed; Plan needs its own human comment.

- 2026-09-01 — Research **run 0002**. Headless. `RESEARCH_BRIEF.md` revised **in place** (not
  rewritten) after the owner answered open questions 2, 3 and 4 and asked for 1 and 5 to be
  explained. Three questions closed as settled decisions: **SD-1** the stampede is accepted, not
  mitigated (with his reasoning and its two premises recorded); **SD-2** the zero-comments clause is
  kept but relaxed when the owner authored the issue; **SD-3** refusals become visible on the issue
  thread, but only for refusals on issues assigned to the owner, and only once per issue.
  Open question 1 (does GitHub bump `updated_at` on assignment?) **could not be settled** — the
  read-only `gh` probe was refused by the permission system and `WebSearch`/`WebFetch` were
  unavailable; no from-memory answer was recorded. Open question 5 explained with a recommendation
  and left as the owner's decision. Two new risks: **R11**, the SD-2 × SD-3 comment loop, which makes
  a once-per-issue mechanism a precondition for SD-3 rather than a refinement; and **R12**, that
  assignment has no second discovery listing the way comments do, against a measured thirty-minute
  lag in this repository's own issue listing. Run 0001's ranking of mechanism (c) revised upward as a
  result. Research remains closed; Plan needs its own human comment.

- 2026-09-01 — Research **run 0003**. Headless. `RESEARCH_BRIEF.md` revised **in place** again after
  the owner answered open question 1's blocker (he granted the web tools), answered open question 5,
  **corrected run 0002's reading of open question 4**, and answered run 0002's "problem 1". The
  substantive work of this run is a **correction, not an addition**. Three outcomes. **(1) SD-3 is
  superseded by SD-3′.** "I want to see this logged here" meant the **daemon log**, not a comment on
  the issue thread — *"i ment the log actually, not a comment on git!"*. Run 0002 had assumed the
  thread and built the reply-kind plumbing, a comment-less posting path, a `0003_*.sql` migration,
  the once-per-issue durable state and the whole of R11 on top of it. All of that is struck and
  marked superseded rather than deleted, since the History above narrates run 0002 sizing Story C as
  "the largest of the three" on its strength. The corrected story is a log-level change: the three
  gate refusals log at `DebugContext` (`internal/daemon/tick.go:1085-1087, :1104, :1113-1116`), the
  default level is `info` (`internal/config/config.go:256`, `internal/logging/logging.go:24-25, :38`),
  and no run or run JSONL exists on a refused body — so `litefactory logs` prints "no runs yet"
  (`internal/cli/cmd_logs.go:203-209`) and the refusal is **observable nowhere at all**. Promote to
  `InfoContext` on branches 2–5, keep branch 1 (not assigned) at Debug on volume grounds, and branch
  6 is **already** Info (`:1505-1511`). The house precedent is AC-S0.4 (`internal/turn/classify.go:343-352`,
  `classify_test.go:521-527` — "a line only visible under debug is not a line the owner can find").
  Story C therefore **dissolves into Story A**; no migration, no forge write, no new reply shape.
  **(2) R11 is retired**, with the reason written out rather than the paragraph removed: a loop needs
  the output to feed back into the input, a posted comment did that via `len(comments)` at
  `tick.go:1081`, and a log line has no path back into anything the daemon reads. **(3) Open question
  1 is closed by research, but not with a yes or a no.** With web access granted, GitHub's Issues
  docs, Assignees docs, timeline docs, events docs and the Changelog were checked: **GitHub documents
  nothing about what mutates an issue's `updated_at`**, and there is no authoritative list. The more
  decisive finding is the fallback: the repository-wide issue-events endpoint
  (`GET /repos/{owner}/{repo}/issues/events`) does carry `assigned`/`unassigned` events but its
  **entire parameter list is `owner`, `repo`, `per_page`, `page`** — no `since`, no `sort`, no
  documented ordering, no documented retention — so it cannot be cursored the way every other
  listing in this daemon is; and the per-issue timeline is one request per issue per tick. Both
  escape hatches from R12 are therefore closed, which **reverses run 0001's ranking**: mechanism
  **(c)** (keep `needs_poll` set, narrowed to plausible candidates) is now the recommendation,
  because it is the only option resting on no external contract. Two new settled decisions: **SD-4**,
  "assigned to me and nobody else", which is **already exactly what
  `t.issue.AssignedTo(d.filter.Identities().OwnerUserID)` does** (`internal/daemon/tick.go:1077`,
  `internal/github/issues.go:94-104`) and so needs **no code change**; and **SD-5**, the owner's
  bot-account reasoning, which is correct and enforced (`internal/turn/classify.go:245-253, :263-274,
  :119-129`) and is now **moot**, because SD-3′ withdrew the premise that litefactory would post at
  all. Full open-question audit written into the brief in answer to his direct "is everything
  answered now?": **eight questions ever raised, seven closed, one (OQ-6, which discovery mechanism)
  open and belonging to Plan rather than to him.** Research remains closed; Plan needs its own human
  comment.

- 2026-09-01 — **Plan** complete. Headless. Reached from the owner's comment *"implement it!"*, which
  points at Implement; the Implement gate requires `plan_brief.md` and only `RESEARCH_BRIEF.md`
  existed, so per `CLAUDE.md`'s missing-gate rule this run fell back to Plan rather than asking
  (nobody can answer in a headless run). Wrote `plan_brief.md` plus three sub-issues — `US-1.md`
  (the brief's Story A1), `US-2.md` (Story A2/R-body′), `US-3.md` (Story B/SD-2). The brief's Story C
  stays dissolved.
  **The substantive work of this run is that OQ-6 was decided *against* the research brief's own
  recommendation.** Run 0003 recommended mechanism (c) — keep `sessions.needs_poll` set, narrowed to
  plausible candidates — and explicitly left it to Plan to check that against the mechanism's real
  shape. It was checked and it does not hold, for three reasons verified in this worktree: (1) a mark
  set inside `runIssueBody` is **wiped by the same tick**, because `Daemon.work` calls `clearBacklog`
  unconditionally whenever `!deferred` (`internal/daemon/tick.go:950-955`), `deferred` is only ever
  assigned from `drain` (`:944`), and `bootstrap` (`:942`) returns an error alone — the code names
  `:954` "the one clear site" in its own comment; (2) `needs_poll` is *also* cleared inside `run`
  (`:1432`), so an unrelated comment-triggered run on the same session would silently destroy the
  state — a correctness bug, not a style objection; (3) `needs_poll`'s documented contract is
  self-healing stale-true (`0002_session_backlog.sql:21-24`) and a body-retry flag never self-heals.
  **Adopted instead: a dedicated `sessions.body_pending` column via `0003_*.sql`**, doing double duty
  as the discovery trigger (unioned into `discover` at no API cost) and as the free short-circuit
  guarding a body-gate call added to the **drain** arm of `work` — because bringing the session back
  is useless on its own, `drain` never touching the body being the second half of the defect the
  brief had folded into the first. The ledger's migration-budget precedent
  (`.aiteam/AC_AMENDMENTS.md:1286-1312`) was weighed and judged not to apply: the cheaper option is
  broken, not merely uglier. Retry marking narrowed to the **not-assigned** branch only, plus
  non-empty body, zero comments and a spawning command — which needs `ParseCommand` moved above the
  gate (pure function, free, but load-bearing for the index bound). Empty-body and has-comments
  refusals are **not** retried; the second is monotonic, the first is a deliberate narrowing whose
  consequence is recorded in US-2 (**issue #10 itself is the empty-body case, so US-2 alone would not
  have started it**). Bound on runaway retries: no age cap and no config key — the flag clears on run,
  on permanent refusal, and on close, and per-tick cost stays at `max_concurrent_runs` issue reads
  because `materialise`'s `GetIssue` (`:896`) happens *after* `offer` takes a pool slot (`:766`),
  confirming `APP_CONTEXT.md`'s claim against source. Residual recorded as an explicit task
  (T-2.7): body-pending sessions compete for the same pool slots as comment-driven work, so cost is
  bounded but *latency* is not, and an admission order is owed.
  **Citation rot: six new instances found, all in the brief's own text**, breaking run 0003's clean
  streak — `tick.go:940-948`→`:940-944` (bootstrap-vs-drain), `:1043-1051`→`:1046-1055` ("watermark
  first"), `:1505-1511`→`:1510-1515` (`refuseTools` Info branch), `:1107-1118`→`:1105`/log `:1113`
  (`!Spawns()`), `classify.go:343-352`→`:352-356` (the Info precedent),
  `classify_test.go:521-552`→`:528` (`TestSelfDropIsLogged`). None changes a conclusion; all are
  tabulated in `plan_brief.md`'s *Provenance*. **One claimed drift was itself wrong and is recorded
  because it nearly deleted a task**: the architecture review reported that
  `internal/harness/prompt.go`'s "not reachable today" claim no longer exists and advised dropping
  the rewrite. It was read directly — the text **is present and the brief's `:247-256` is exact** —
  and the task is retained in US-2 (T-2.9). Plan is closed; Implement needs its own human comment,
  and the owner's earlier "implement it!" does not carry over, because the gate was shut when it was
  written.

## Notes

**Plan-run assumptions**, recorded because this was headless and nobody could be asked. The
load-bearing one: **an issue whose body was empty at first sight is deliberately not retried**, so
filling in a body later does not auto-start it — the owner comments, which already works. This was
taken on the dispatching instruction rather than from the owner's own words, and it is the narrowing
that keeps every empty issue in the repository out of the new partial index. It is stated in US-2's
*Architecture Notes* together with the fact that issue #10 is itself that case. Second: an **age cap**
on retries was considered and deferred rather than dropped, on the grounds that it would need a
config key and config in this codebase is not free. Third: the three-story split, and the decision to
keep US-2 whole rather than splitting its store plumbing from its control flow, is Plan's judgement —
splitting would ship a column nothing reads, and dead code is a worse artifact than one large story.

Feature slug supplied by the runner: `we-need-to-start-immediately-on-a-new`. Nobody can answer a
question in this run, so open questions are recorded in the brief rather than asked.

**Run 0003: no new citation rot in the brief's own text — the first clean run of this session.** Runs
0001 and 0002 recorded five instances between them. Run 0003 re-pinned every citation it relied on
(`runIssueBody` and all its refusal branches, `refuseTools` and its doc comment, `Filter.Classify`,
`selfReason`, `Identities.Validate`, `Admit`'s AC-S0.4 comment) and found all of them exact; run
0002's corrections held. One further stale statement was found **outside** the brief:
`.aiteam/APP_CONTEXT.md:127` is a *third* instance of the "seven endpoints" claim run 0002 found at
`:212` and `:691`, against twelve pinned methods in `internal/github/surface_test.go:64-109`. It
changes no conclusion and, per the archive rule, belongs in the ledger rather than in an edit to the
archived text.

**Run 0003's own assumptions**, recorded in the brief's *Assumptions taken in run 0003*. The two that
matter: SD-3′ is read as promoting existing log lines to `Info`, **not** as adding a CLI command to
read `daemon.log` — `litefactory logs` reads run JSONL only (`internal/cli/cmd_logs.go:184-198`) and
there is no `daemon logs` command, so surfacing the daemon log through the CLI would be a separate
feature and is not scoped here. And branch 1 (not assigned to the owner) is excluded from SD-3′ on
volume grounds by run 0003's judgement rather than by the owner's instruction, since it fires for
most issues in the repository on most ticks.

**Run 0002's load-bearing assumption was wrong and the correction is the main event of run 0003.**
The note below stands as written and is now answered: the owner did mean the log, and Story C did
disappear.

**Run 0002 citation rot found (this session's fourth and fifth instances).** Run 0001's own citation
of `Daemon.refuseTools` had rotted by fourteen lines (`tick.go:1473-1511` → `:1487-1533`), and
`.aiteam/APP_CONTEXT.md:212, :691` still describe `internal/github` as having "exactly seven
endpoints" when `TestClientMethodSetIsPinned` pins twelve methods, ten of them endpoint-calling. The
"an eighth forge endpoint" phrasing survives in at least four in-tree comments. Neither changes a
conclusion; both are recorded in the brief's *Archive corrections*. Per the archive rule, the
correction belongs in the ledger rather than in an edit to the archived text.

Assumptions taken in run 0002 are listed in the brief's own *Assumptions taken in run 0002* section.
The load-bearing one: SD-3's "log it here" is read as **the GitHub issue thread**, not the daemon
log — if the owner meant the log, that story disappears entirely.

Assumptions taken in run 0001, recorded because nobody could be asked:

- The owner's "the owner is the litefactory account (e.g. marioatcloudvane)" was read as meaning the
  *human owner* identity this codebase already stores, not the bot account litefactory posts as. The
  two are required to be different (`internal/turn/classify.go:125-127`).
- "as long as the owner is the litefactory account" was read as *not* a request to add an
  authorship-based trigger, because all four of the owner's own workflows contain an explicit
  assignment step and the assignment gate satisfies all four. This is recorded as open question 5.
- Issue #10's body being empty was taken from the run's task description (`[O]`), not verified
  against the forge from this worktree.
