# Manifest
# Branch: lf/6-first-comment-pickup
# Session: 2026-08-26

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-08-26 — session created for issue #6 ("First comment pickup")
- 2026-08-26 — Research complete. `RESEARCH_BRIEF.md` written. Headless run: no
  clarifying questions asked, five assumptions recorded in the brief instead.
  Research is closed; Plan needs its own comment from a human.
- 2026-08-26 — the codebase tour finished after the brief was written; its three
  additive findings (the fixture asymmetry that would let the suite go green on
  a single flipped test, the seven-endpoint surface tripwire, and the absent AC
  amendment mechanism) were verified against the source and folded into
  `RESEARCH_BRIEF.md`. Still Research; nothing else changed.

- 2026-08-26 — Plan complete. `IMPLEMENTATION_PLAN.md` written: three user
  stories (ignore closed issues, parse assignees, issue body as first
  instruction), 16 tasks. The design was settled by the owner in conversation
  before Plan ran, so no requirements conversation was held and no alternatives
  were weighed. Two sections of the brief are recorded in the plan as
  superseded: §4's idempotency options and the `CreatedAt` newness test. Plan is
  closed; Implement needs its own comment from a human.

## Notes

Request, verbatim from the comment that triggered the run:

> When i create a new issue, i want the tick to already pick up the text in the
> issue. I write stuff about the issue and what i want already in the issue text
> and the factory should already pick this up rather than adding another
> comment. this is not how it should be done!

Classified as **Research** by inference: no slash command, no mode named, and
the ask is a change to what starts a run at all — it lands on `bootstrap` in
`internal/daemon/tick.go`, which today deliberately reads no issue body
(AC-2.16). That is a design question, not a small edit.
