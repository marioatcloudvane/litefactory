# Manifest
# Branch: lf-10-we-need-to-start-immediately-on-a-new
# Session: 2026-09-01

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — 2026-09-01, revised in place by run 0002)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
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

## Notes

Feature slug supplied by the runner: `we-need-to-start-immediately-on-a-new`. Nobody can answer a
question in this run, so open questions are recorded in the brief rather than asked.

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
