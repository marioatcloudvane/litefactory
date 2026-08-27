# Manifest
# Branch: lf-7-headless-mode-should-ask-via-a-comment
# Session: 2026-08-26

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — 2026-08-26; revised 2026-08-27)*
- [ ] **Plan** — `IMPLEMENTATION_PLAN.md` *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-08-26 — session created, classified as Research (issue #7: headless mode should ask via a comment rather than infer)
- 2026-08-26 — Research done. `RESEARCH_BRIEF.md` written. One `codebase-tour` sub-agent run;
  findings folded in and cross-checked against source. No code changed, nothing committed by
  this run.
- 2026-08-27 — **Research revised in place (run 0002).** Triggered by an owner comment that both
  corrected run 0001's headline finding and enlarged the scope from one ask to three. Two
  verification sub-agents run; citations re-checked against `origin/main` @ `3427686`. Still
  Research — no plan, no code, no comments posted, nothing committed by this run.

## Notes

Headless run — started from a comment on issue #7 with no slash command. Feature
slug supplied by the runner: `headless-mode-should-ask-via-a-comment`.

No `.aiteam/APP_CONTEXT.md` exists in this repo (checked). Research did not create one; per
`CLAUDE.md` that is the Implement orchestrator's job and it was not blocked on.

No `.aiteam/rulebook.md` exists either, so the shared scanner skills would have fallen back to
generic best practice. No scanner was run — the question is about behaviour and state, not
coverage or layering.

**CORRECTION — run 0001's headline finding was WRONG and has been retracted.** It read: *"the
requested feature is hard-blocked on `session-resume` … today the second run of any task exits 1
with 'Session ID … is already in use'."* **That is no longer true.** Session resume was implemented
after run 0001 and is live on `origin/main` (`previousRun()` in `internal/turn/prepare.go:237-258`,
the single `sessionFlag` at `runner.go:393-401`, and the same-turn cold rerun at
`harvest.go:93-142`). It needed **no migration**. The blocker is gone; it is now a **prerequisite
that is already satisfied**. Re-verified first-hand this run at commit `3427686`.

**Headline finding, revision 2 — the scope, not the blocker, is the story.** The issue now carries
**three asks**: (A) ask via a comment — the original, now small and unblocked; (B) the research
brief also lands as a comment; (C) the plan appears as **sub-issues** instead of a file. They share
a theme and almost no code. **The brief recommends planning (A) alone first.** Two facts dominate
the other two asks: `internal/github/surface_test.go:56` **fails the moment an eighth client method
exists, by design**, and (C) needs an eighth *and* ninth — so (C) opens with a deliberate SDK
decision, not an edit. And **artifact-based state inference turns out to be entirely unimplemented
in Go** (zero `.go` hits for `RESEARCH_BRIEF`/`IMPLEMENTATION_PLAN`), so (B) and (C) break no
running code — they contradict `initialplan.md:243` and this repo's own `CLAUDE.md` mode gates,
which is a cheaper and more honest place to have the argument.

**Two warnings for whoever reads this next.**
1. **Citation rot is real here** — run 0001's central finding survived one day. Every fact in the
   brief is tagged [V] verified-this-run, [O] owner-reported, [W] second-hand-and-unverified, or
   [R0] carried forward. **The [W] block on GitHub's sub-issue API is explicitly untrusted**: its
   own source records six of *its* predecessor's external GitHub claims as wrong, two load-bearing.
2. **Web tools were disabled for this entire session**, sub-agents included, so nothing external
   could be checked. Open Questions 22-24 need a human with a browser.

`.aiteam/APP_CONTEXT.md` still does not exist (re-checked); per `CLAUDE.md` that is the Implement
orchestrator's job and this run did not block on it. No `.aiteam/rulebook.md` either.

Research is closed. Per *One Run, One Mode*, Plan does not follow from this automatically — it
needs its own comment from a human.
