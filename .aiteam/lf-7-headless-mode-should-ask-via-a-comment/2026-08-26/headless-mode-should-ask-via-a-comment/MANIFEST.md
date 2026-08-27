# Manifest
# Branch: lf-7-headless-mode-should-ask-via-a-comment
# Session: 2026-08-26

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — 2026-08-26; revised 2026-08-27)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done — 2026-08-27; **revised in place to rev 2** after the
  architect review returned; **11 open questions** for the owner at the top, none answered)*
- [ ] **Implement** — code + tests *(not started)*
- [x] **Spike** — `SPIKE.md` *(done — 2026-08-27)*

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

- 2026-08-27 — **Spike (run 0005).** Owner asked for a plain-English summary of the brief and named
  the mode explicitly ("this is a spike"). `SPIKE.md` written. No research, no verification, no
  code — a condensation of the existing brief. Nothing committed by this run.

- 2026-08-27 — **Plan (run 0006).** Owner opened Plan mode explicitly and scoped it: ask (C)
  (sub-issues) deferred to his own story; ask (B) approved; briefs must get much shorter; ask (A)'s
  default **inverted** to opt-in via a `research-mode: ask` tag. `feature-spec.md` and
  `IMPLEMENTATION_PLAN.md` written. Four user stories (A1, A2, B1, B2), each independently
  shippable. **Eight questions for the owner are at the top of the plan and were NOT assumed away**
  — the owner asked for exactly that ("don't assume stuff! we eat our own dogfood"). Three
  sub-agents: `requirements-engineer`, `go-architect`, and two narrow `git show` verifications.
  No code changed, nothing committed by this run. Per *One Run, One Mode*, Implement does not
  follow from this — it needs its own comment.

- 2026-08-27 — **Plan revised in place (run 0007).** Run 0006's `go-architect` review returned after
  that run had already exited; it is on disk as `ARCHITECT_REVIEW.md`. `IMPLEMENTATION_PLAN.md`
  revised to **rev 2** — not a new document. **Still Plan** — no code, nothing committed, and
  Implement still needs its own comment.

  **The headline is a process defect, not a technical one.** Rev 1 opened by claiming *"nothing
  genuinely his to decide has been decided here"* and then silently decided two things: that Go
  would parse the tag, and that asking permission would carry across a resume via a `PreviousAsked`
  field. Both landed on Story A1 — the story meant to ship first. Both are now questions (**Q3**,
  **Q9**). **The question count went 8 → 11; no question was answered or dropped.**

  Four findings were verified against source before this run and applied as fact: the gag paragraph
  bundles the commit/push prohibition (F-9), quote-reply is silently dropped (F-21), the marker
  matcher is a label-upgrade hole (F-19), and rev 1's claim that a brief fed through `RenderRun`
  would be elided was **wrong** and is deleted (F-4). One narrow verification sub-agent was run this
  revision, on `prepare.go`'s `PreviousMode` handling.

  Story count 4 → 5: **Story S0** added, and it now ships first.

## Notes

**Plan rev 2 (run 0007) — the three things most worth knowing.**

1. **A quote-reply to any litefactory comment is silently discarded.** `HasMarker` is
   `strings.Contains(body, "<!-- lf:")` — anywhere in the body (`turn/classify.go:38`) — and
   `Classify` tests `selfReason` *before* the owner check (`classify.go:207-214`), deliberately
   (`classify.go:193-196`). GitHub's quote-reply copies the raw markdown including HTML comments, so
   the owner's own reply classifies `self` and is dropped: no run, no notification. **This bites the
   moment the owner answers any of the eleven questions by quote-reply.** New **Story S0** fixes the
   accidental case by line-anchoring the match; the deliberate-impersonation case (F-19) is put to
   the owner as Q10(b) and otherwise listed in `Out of Scope`, because inverting author-versus-marker
   precedence is a larger security change than issue #7.

2. **`PreviousAsked` was unimplementable and is replaced by `PreviousMayAsk`.** It answered the
   wrong question — *"did the last run ask?"* rather than *"may this run ask?"* — and under A1's
   "no new state" it is not derivable at all: a run that asked and one that did not are
   byte-identical in the store. The architect's replacement holds and was verified, **with one
   correction to its reasoning**: `PreviousMode` is a store-column read (`prepare.go:175`), so
   `PreviousMayAsk` is *not* "the same shape" and is *not* free. It must be re-parsed from the
   previous run's trigger comment — reachable from `store.Run.TriggerCommentID` (`store/runs.go:75`,
   present on every row, currently unused) against the comment list `commentHistory` already loads
   on both paths (`prepare.go:149-152`, `:297`), with **no watermark filtering to worry about**
   (`store/comments.go:151-154`). But `harness.HistoryEntry` carries no comment id, so it needs a
   small local refactor in `prepare.go`, plus a fallback for `TriggerCommentID == 0` (issue-body
   trigger). Both are now their own tasks with their own AC.

3. **Splitting the gag paragraph is the sharpest concrete bug in the plan.** `prompt.go:396-401` is
   one paragraph carrying two unrelated instructions, and the second is *"do the work in this
   worktree; do not commit or push."* Branching the whole paragraph on `MayAsk` — which is what rev 1
   said to do — **makes every tagged run commit and push**, colliding with litefactory's own commit
   step. A new task splits it first so that sentence stays unconditional, and AC-A1.8 makes a tagged
   prompt missing it a failing test.

**Field names are now declared once, in the plan, and supersede `feature-spec.md:47-48`:**
`MayAsk` / `PreviousMayAsk`. Rev 1's `AskingAllowed`/`PreviousAsked` and the spec's
`MayAsk`/`AnswersQuestion` are both retired.

**Two ACs are marked (manual / observational)** — AC-B1.2 and AC-B2.1 — so `implement-orchestrator`
does not stall waiting for a test that cannot exist. Rev 1's AC-A1.6, which the architect correctly
called "a claim, not a criterion", was replaced by an executable `turn`-level assertion instead.

**Plan run 0006 — three facts verified this run that CORRECT the research brief.**
1. `terminalRunStatus` is `succeeded` / `failed` / `reported` (`turn/prepare.go:343-352`), and the
   only other resume gate is segment match. **So a run that asks its question in the final message
   and ends normally is already resumed when the owner replies.** The question round-trip works
   today; only the prompt text forbids it. This splits ask (A) into a near-free story (A1) and an
   optional one (A2), and is the single biggest sizing change in the plan.
2. `internal/report/run.go:24` caps the run's final message at **`maxResultBytes = 32 << 10`**,
   truncated **from the end** (`run.go:528`). The brief sized briefs against 63,488 bytes. If a
   brief is delivered via the final message the real ceiling is **half that**, and the sections
   lost are the last ones — Open Questions and Recommended scope.
3. There are **two** headless paragraphs in `writeHeader`, not one: the known gag at
   `harness/prompt.go:396-401` and the feature-slug paragraph at `:388-390`. Changing only the
   first leaves a contradiction inside the same header.

**A defect found this run that no earlier document contains:** asking permission does not survive
the resume. The tag is on the original comment, the answer comment will not carry it, and
`writeResumeHeader` never re-emits the gag — so the default, if nothing is decided, is a resumed
run that may ask indefinitely. That is Q7 in the plan.

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
