# Manifest
# Branch: lf-10-we-need-to-start-immediately-on-a-new
# Session: 2026-09-01

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — 2026-09-01)*
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

## Notes

Feature slug supplied by the runner: `we-need-to-start-immediately-on-a-new`. Nobody can answer a
question in this run, so open questions are recorded in the brief rather than asked.

Assumptions taken this run, recorded because nobody could be asked:

- The owner's "the owner is the litefactory account (e.g. marioatcloudvane)" was read as meaning the
  *human owner* identity this codebase already stores, not the bot account litefactory posts as. The
  two are required to be different (`internal/turn/classify.go:125-127`).
- "as long as the owner is the litefactory account" was read as *not* a request to add an
  authorship-based trigger, because all four of the owner's own workflows contain an explicit
  assignment step and the assignment gate satisfies all four. This is recorded as open question 5.
- Issue #10's body being empty was taken from the run's task description (`[O]`), not verified
  against the forge from this worktree.
