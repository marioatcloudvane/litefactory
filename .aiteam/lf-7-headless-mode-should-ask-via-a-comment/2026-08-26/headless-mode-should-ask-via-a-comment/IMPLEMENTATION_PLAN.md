# Implementation Plan — headless-mode-should-ask-via-a-comment
# Branch: lf-7-headless-mode-should-ask-via-a-comment
# Date: 2026-08-27 (rev 2 — revised in place after the `go-architect` review returned)
# Issue: marioatcloudvane/litefactory#7
# Sources: `RESEARCH_BRIEF.md` rev 2 · `feature-spec.md` · `ARCHITECT_REVIEW.md` · the owner's
#   scoping comment of 2026-08-27
# Citations pin to `origin/main` @ `3427686`.

> **Written headlessly, under the exact behaviour issue #7 exists to remove.** The owner
> instructed: *"if you are unsure, don't assume stuff."*
>
> **Rev 1 claimed "nothing genuinely his to decide has been decided here" and then decided two
> things anyway** — that Go would parse the tag, and that asking permission would carry across a
> resume via a `PreviousAsked` field. Both landed on Story A1, the story meant to ship first. The
> architect caught it. Rev 2 promotes both to the question list (now **Q3** and **Q9**) and drops
> the claim. The honest version is below.
>
> **What is decided here, and by what right:** choices that are engineering rather than product —
> where the conservative option widens no boundary — are recorded as **Decision taken** with their
> reasoning. Everything that changes what the owner gets, or what litefactory is allowed to do, is
> a question. Tasks blocked on a question are marked **BLOCKED-ON-Qn**; everything else is
> executable as written.
>
> **Eleven questions. None has been answered — no human has replied since rev 1 was written.**

## What changed in rev 2

| # | Change | Source |
|---|---|---|
| 1 | **Two silent decisions promoted to questions** (Q3 — who enforces the tag; Q9 — what the resume header may say) | architect, headline process defect |
| 2 | **New Q10 — GitHub quote-reply silently drops the owner's answer**, and a new **Story S0** to fix it | F-21 *(verified)* |
| 3 | **`PreviousAsked` replaced by `PreviousMayAsk`** throughout; one field-name set declared, superseding `feature-spec.md` | architect + own verification |
| 4 | **New task T-A1.2 — split the gag paragraph** so the commit/push prohibition stays unconditional | F-9 *(verified)* |
| 5 | **Deleted B2's "a brief fed through `RenderRun` would be elided" bullet** — it inverted the survival order and was the main cost argument for route (ii) | F-4 *(verified)* |
| 6 | **Re-sequenced:** S0 → B1 → B2 → A1 → A2 | architect |
| 7 | Task-table corrections F-6, F-7, F-10 through F-17, F-22; platform-lens corrections | architect |
| 8 | Unverifiable ACs marked **(manual)** so `implement-orchestrator` does not stall on them | architect |
| 9 | Q7 (was Q6) gains the two costs the review adds, and notes route (i) dissolves OD-10 | F-3, F-3b |

**Where rev 2 disagrees with the review, it says so in place.** Three disagreements, all flagged:
B1/B2 stay two stories (§Shipping order); `PreviousMayAsk` is *not* "the same shape as
`PreviousMode`" and is *not* free (§A1 Architecture Notes); F-19's deliberate-impersonation half is
put to the owner rather than mandated (§Q10).

---

## Questions for the owner

*Recommendations are recommendations. None of them has been applied to the plan below.*

*Renumbered this revision. Old → new: Q1→Q1, Q2→Q2+**Q3**, Q3→Q4, Q4→Q5, Q5→Q6, Q6→Q7, Q7→Q8,
Q8→Q11. New: **Q3**, **Q9**, **Q10**.*

### Q1 — Is the research brief MIRRORED or MOVED?

*"we add them inline already as comments here"* does not say whether `RESEARCH_BRIEF.md` is still
written to the repo.

- **Mirrored** — file stays, comment added. `CLAUDE.md`'s Plan gate (*"Plan requires
  `$session/RESEARCH_BRIEF.md`"*) keeps working untouched.
- **Moved** — comment only. That gate can then never pass, so `CLAUDE.md` must change and
  `initialplan.md:243` (*"State is inferred from artifacts, not self-reported"*) must be amended.

**Why it matters beyond the gate — it decides whether Q5 is cheap or lossy.** Mirrored means the
comment is a *summary* and the full detail survives in the file, so "much shorter" costs nothing.
Moved means everything cut from the comment is gone permanently, and "brief" and "complete" become
a genuine trade.

**Verified, and it cuts against my recommendation:** artifact-based state inference is **entirely
unimplemented in Go** — zero `.go` hits for `RESEARCH_BRIEF`/`IMPLEMENTATION_PLAN`. "Moved" breaks
no running code; it contradicts a written spec and this repo's `CLAUDE.md`. It is also the last
moment that unbuilt layer could be redirected without a rewrite.

**Recommendation: mirrored** — chiefly because it is what makes Q5 free rather than lossy.

### Q2 — The tag: where does it live, and how is it spelled?

*(Rev 1 folded "who enforces it" into this question and then answered it. That half is now **Q3**.)*

**(a) Where.**

| Where | Cost | Failure mode |
|---|---|---|
| **Triggering comment body** | Smallest. `KindFreeText` already carries the whole body, so the tag is available at zero cost. Must be stripped or it becomes part of the instruction. | Per-comment; you must remember it each time. |
| **A GitHub label** | **New client surface.** `internal/github` has no label API — no PATCH/PUT/DELETE anywhere. An eighth method trips `TestClientMethodSetIsTheSevenEndpoints`, reopening the SDK conversation you just deferred. | Sticky (good), invisible in the run record (bad). |
| **Issue body** | Small; the issue is already fetched. | Sticky for the whole issue, never per-run. |
| **`CLAUDE.md`** | Zero Go. | Repo-wide default, not an opt-in. Cannot opt one hard question in. |

**(b) How spelled.** Your words were `research-mode: ask`. Three spellings, and the choice is
coupled to **Q6**:

1. **`research-mode: ask` verbatim** — exactly what you wrote. If Q6 answers "all modes", this
   forces a table (`research-mode:`, `plan-mode:`, `implement-mode:`, …) kept in sync with a mode
   vocabulary that lives in the *target repo's* `CLAUDE.md`, not in litefactory.
2. **`lf: ask`** — mode-neutral, one literal forever, works unchanged under either Q6 answer.
3. **Any `lf:` directive line at all** — see Q3 option 2.

**Recommendation: the triggering comment body, spelled `lf: ask`.** Per-run, no new client
surface, one literal under either Q6 answer. **This renames your tag, so it needs your say-so.** If
you want `research-mode: ask` kept verbatim, say so and the matcher takes that literal instead —
under Q6 = "Research only" the cost is one string, not an architecture.

### Q3 — Who enforces the tag: Go, or the prompt? *(new — promoted from a decision rev 1 took silently)*

**Rev 1 wrote *"So Go must read it"* and moved on. That was an owner decision wearing an
engineering argument.** The argument itself is sound as far as it goes: the gag paragraph is
emitted by Go (`harness/prompt.go:396-401`), so if Go never reads the tag, a tagged run gets a
prompt saying *"do not ask clarifying questions"* and a comment saying *"you may ask"*, with no
precedence rule anywhere. But "resolve the contradiction" does not by itself mean "Go parses the
tag" — there are three ways to resolve it, and they differ in what litefactory becomes.

| Option | What Go does | Cost | What it buys |
|---|---|---|---|
| **1. Go parses the literal** | Matches `lf: ask` (or `research-mode: ask`) line-anchored on the trigger comment; emits gag or permission accordingly | A tag parser, a strip step, `MayAsk`/`PreviousMayAsk` plumbing, the tag-only-comment edge case (F-16), the `KindSpike` strip path (F-15) | Hard enforcement. An untagged run *cannot* ask, whatever the agent decides |
| **2. Go parses only *presence*** | Detects that *some* `lf:` directive line exists; **never parses the word after the colon**; emits permission on presence, gag on absence | Same parser, strictly one bit | Survives any future directive vocabulary with no Go change. Go holds no mode name and no verb |
| **3. Go never parses; the prompt delegates** | Go emits one unconditional paragraph: *"ask only if the triggering comment carries a directive permitting it; otherwise assume and disclose"* | **Near zero.** No parser, no strip, no new `PromptInput` fields, no `PreviousMayAsk`, no tag-only edge case. Most of Story A1 dissolves | Agent-enforced, not machine-enforced. A misreading agent asks when it shouldn't |

**Rev 1's argument against Go parsing was that it teaches Go a mode name, citing
`mode-closure/SPIKE.md:70-73`. The architect is right that this overstates the cost, and I accept
the correction:** `harness/prompt.go` already defines `const InferMode = "infer"` and already
branches prompt text on it (`prompt.go:422`). Go holds a mode name today. Matching one more opaque
literal concedes nothing new. The purity argument is weaker than rev 1 made it sound.

**What remains genuinely yours** is option 3 versus 1/2 — machine enforcement versus trust. Option
3 is dramatically cheaper (it removes roughly two thirds of Story A1's Go work) at the price that
"never ask unless tagged" becomes something the agent honours rather than something it cannot
violate. Given R-3 — eleven latent approval gates across the agent definitions all firing at once
if the gag comes off — I do not think trust is the right call here, but it is not my call.

**Recommendation: option 2.** Machine-enforced, one bit, no vocabulary to keep in sync, immune to
Q6's answer. **If you disagree and want option 3, say so — it is the cheapest thing in this plan
by a wide margin and I would want to hear the reason before dismissing it.**

### Q4 — What did *"-> separate task for that!"* mean?

- **(a) A separate user story inside this plan** — ships with everything else, tracked apart.
- **(b) A separate issue/run entirely** — this plan then must not build the tag at all, and
  everything marked (A) below is deleted rather than deferred.

If (b), **Story A1 must not ship**, because A1 without the tag is *ungated* asking — the single
largest product risk in the brief (R-3: eleven latent approval gates across the agent definitions
all firing at once).

**Recommendation: (a).** The sentence sits immediately after *"research never asks unless there is
a tag"*, which reads as "the tag mechanism is its own piece of work", not "drop (A)".

### Q5 — How brief is "brief"? A concrete target to correct.

*"Shorter"* is not testable. Two verified numbers give it a natural shape:

- **`MaxCommentBytes = 8 << 10` (8,192 bytes)** — the per-comment cap on the way into a prompt. A
  brief at or under this is the only one replayed into a later run **whole** rather than truncated.
- **`maxResultBytes = 32 << 10` (32,768 bytes)** — see Q7; a hard ceiling on one delivery route.

**Proposal:**
- **Target: ≤ 8,192 bytes** (~1,200 words, ~150 lines of markdown).
- **Hard ceiling: 20,000 bytes.** Past that the brief is wrong, not just long.
- **Survives:** Request · Findings (max 8, one paragraph each, citations reduced to `path:line`) ·
  Risks (max 6, one line each) · Open questions · Recommended scope.
- **Cut:** provenance/tagging tables, revision notices, verbatim block quotes, "Domain / Problem
  Space" prose, meta-commentary about the run itself, and any fact restated in a second section.

For calibration: this issue's `RESEARCH_BRIEF.md` is **1,034 lines**. The target is about **one
seventh of it**.

**The honest cost:** rev 2 of the brief's provenance tagging ([V]/[O]/[W]/[R0]) exists because
citations in this repo go stale within days — its own predecessor's headline finding survived one
day. Cutting it is a real loss under Q1=moved and a non-loss under Q1=mirrored. **Answer Q1 and Q5
together.**

**A new argument for the 8 KiB target, from the architect (F-3):** under Q7 route (i) the brief is
truncated **twice**, not once — `report/run.go:528` cuts at 32 KiB, then `runBody` cuts again to
`roomFor(...)` if the assembled body is still over budget, and below
`len(resultHeading)+minTailBytes` the whole result section is replaced by `droppedResultNotice`. So
a brief competes for space with the files list and the log tail. At 8 KiB that competition never
happens. **This makes the target load-bearing rather than aesthetic.**

**Confirm these numbers or give me different ones.** Implement mode needs something it can check.

### Q6 — Does "never ask unless tagged" apply to Plan and Implement too, or only Research?

You wrote *"**research** never asks"*. That reads two ways.

- **Research only** → the tag is a Research-mode concept; Plan and Implement keep today's
  unconditional gag and can never ask.
- **All modes** → one global opt-in switch.

**Recommendation: all modes, one tag.** Research is where you noticed it; it is not where it is
worst. **This document is the argument** — Plan mode hit eleven decisions that were yours and had
to write them down instead of asking. Under "Research only", the four approval gates in
`plan-orchestrator.md` and `requirements-engineer.md` stay permanently fictional.

**Coupled to Q2(b) and Q3.** "All modes" plus a mode-named tag means Go holds a mode table synced
to a vocabulary that lives in the target repo. "All modes" plus `lf: ask` or plus Q3 option 2 costs
nothing extra.

### Q7 — How does the brief physically reach the comment, and is it one comment or two?

Nothing in Go has ever read a `RESEARCH_BRIEF.md`, and the agent has no GitHub tool. Three routes:

| Route | Go cost | Ceiling | Comments per research run |
|---|---|---|---|
| **(i) The run's final message** | **Zero.** `secResult` already posts it. | **32,768 bytes** (`report/run.go:528`), then a second cut by `runBody` if the whole body is still over budget | **One.** The brief arrives inside the status report — heading, callout, metadata, files, log tail. |
| **(ii) A sidecar signal channel** — agent writes a fixed-path JSON, Go reads and posts it | Moderate; shared with Story A2 | 63,488 bytes | **Two** — an `lf:brief` comment *and* the `lf:status` report. |
| **(iii) Go reads `$session/RESEARCH_BRIEF.md` off disk** | Moderate | 63,488 bytes | Two. Also teaches Go where sessions live and what a brief is — a larger concession than Q3's. Incompatible with Q1=moved. |

**Verified, and it corrects the research brief.** The brief sized briefs against 63,488 bytes and
concluded they fit at ~70%. Via route (i) the real cap is **32,768** — and truncation is from the
**end**, so a long brief loses *Open questions* and *Recommended scope*, the two sections you would
actually act on. Of the eight real briefs the research measured, **only two are under 32 KiB.**

**Two costs the architect adds:**

1. **Route (i) truncates twice, not once (F-3).** See Q5. The brief competes with the files list
   and the log tail for the remaining budget. **Under Q5's 8 KiB target this never bites** — which
   is a stronger argument for coupling B1 to B2 than rev 1 gave.
2. **Route (ii) costs more than thread noise (F-3b).** Both comments carry `<!-- lf:`, so both
   classify `self` and both re-enter `History` as `RoleLitefactory`. At `MaxCommentBytes` 8 KiB
   against `MaxHistoryBytes` 32 KiB, **two litefactory comments per research run consume half the
   history block**, evicting your own comments oldest-first on the next cold rerun. Route (i)
   consumes one slot.

**Corrected, and it removes rev 1's main argument against route (ii) (F-4 — verified).** Rev 1
claimed a brief fed through `RenderRun` "would be elided as though it were a log tail". **That is
wrong and the bullet is deleted.** `report/run.go:165-175` states the section order **is** the
survival order, and `runBody` cuts log tail (step 1) → result (step 2) → files (step 3). `secResult`
is the *second-most-protected* section, behind only the never-cut `secFixed`. Route (ii) is
therefore a fairer option than rev 1 presented it as; the recommendation below now rests on Go cost
and history-slot cost, not on a survival-order claim that did not hold.

**Route (i) silently answers OD-10 — flagging it rather than letting you find it missing.** OD-10
asked whether the brief *replaces* or *accompanies* the status comment. Under route (i) the
question dissolves: the brief **is** the status comment's result section, so it neither replaces
nor accompanies. Under route (ii) OD-10 comes back and needs an answer.

**Recommendation: (i), made safe by Q5.** At ≤ 8 KiB a brief fits with room to spare, it ships with
**no new Go at all**, it stays one comment per run, and it retires OD-10. Route (ii) becomes worth
building only if Story A2 is also wanted. **Route (iii) I would not build.**

### Q8 — May a resumed run ask a follow-up, and how many times?

The tag will be on your *original* comment. Your *answer* comment almost certainly will not carry
it. And `writeResumeHeader` **never re-emits the gag** — by design, because the cold prompt already
said it once and it lives in the transcript.

**So the default, if nothing is decided, is a resumed run that is un-gagged and may ask
indefinitely.** That is the "a bot that asks too often is worse than one that guesses" failure,
arriving by omission rather than by choice.

- **(a) Permission does not carry over.** The resume header explicitly re-gags. One question per
  tag. Safest, but makes a genuine two-turn clarification impossible — which is most of the value.
- **(b) Permission carries the whole parked chain, capped in Go.** Precedent for the number:
  `implement-orchestrator.md`'s "maximum 2 cycles".
- **(c) Permission carries over only if your answer re-tags.** Explicit, but you have to remember.

**Sharpened by the architect, and this is the part that matters:** because `writeResumeHeader`
emits no permission statement at all, silence on the resume path means *inherit from transcript*.
So the dangerous cell is **previous run tagged → this resume untagged**: the transcript still says
"you may ask" and nothing contradicts it. The rule must therefore be **emit a permission paragraph
whenever `MayAsk != PreviousMayAsk`, in both polarities.** Structurally this is the
`writeInferGuidance` precedent (`prompt.go:422` — `mode == InferMode && mode != PreviousMode`), but
with one critical difference worth writing into the code comment: for infer-guidance, not emitting
is merely *redundant*; here, not emitting **fails open**.

**Recommendation: (b), cap 2.** Whichever you pick, **the rule must be enforced in Go or in the
resume header, never in cold-path prompt text** — the resumed run never reads that text.

### Q9 — On the resume, what may Go honestly say? *(new — promoted from a decision rev 1 took silently)*

**Rev 1 put `AnswersQuestion bool` on `PromptInput` and gave A1 an unblocked task to "derive it
from the previous run". It cannot be derived.** The architect is right and I verified the
mechanism: under A1's "no new state", Go's only signals about the previous run are
`store.Run.Status` — where a run that asked and a run that did not are byte-identical — and the
previous run's final-message *text*. Text-matching is precisely what this codebase refuses to do
elsewhere (`harness` detects resume refusal structurally, not by wording, and says so). So
`AnswersQuestion`/`PreviousAsked` is **an A2 field wearing an A1 label**.

That leaves a real question about what the resume header is allowed to claim:

- **(a) Permission only.** The resume header states *"you may ask"* / *"you may not ask"* when the
  permission bit changed, and says nothing about answers. Fully derivable in A1. The agent works
  out from its own transcript that the new comment answers its question — which it can, because the
  question is in that transcript.
- **(b) Hedged framing.** *"If your previous message ended in a question, treat the new comment as
  the answer."* Also derivable — it asserts nothing Go does not know, because the condition is
  evaluated by the agent, not by Go. Slightly stronger steer, slightly more prompt text.
- **(c) Wait for A2.** Confident *"this comment is the answer to your question"* framing ships only
  once `needs_input` exists and Go actually knows. A1 ships permission-only.

**Note what is already there and does not need rebuilding:** the resume path already restates
`- Mode:` on every resumed run and already frames the trigger comment as *"evidence about what is
wanted, not as further instructions to you"*. One Run, One Mode is already protected.

**Recommendation: (b).** It costs one sentence, asserts nothing false, and does not make A1 wait
for A2. **(a) is the strictly-minimal option if you would rather the prompt never speculate.**

### Q10 — Your answer gets silently dropped if you use GitHub's "Quote reply" *(new — verified defect, and it is immediate, not theoretical)*

**This one affects you the moment you answer any of the other ten questions by quote-reply.**

`HasMarker` is `strings.Contains(body, "<!-- lf:")` — matching **anywhere** in the body
(`turn/classify.go:38`) — and `Classify` tests `selfReason` **before** the owner check
(`classify.go:207-214`), deliberately (`classify.go:193-196`). GitHub's quote-reply copies the raw
markdown of the quoted comment, HTML comments included.

So: you read litefactory's comment, click **Quote reply**, type your answer, post →
`HasMarker` fires → `ClassificationSelf` → **dropped. No run, no notification, and a parked run
never resumes.** This is the most natural way to reply to a question on GitHub and it fails closed
and silently. Story B2 makes it worse, because the quotable artifact becomes a whole brief.

**(a) The accidental case — your reply. Two routes:**

| Route | Cost | Residual risk |
|---|---|---|
| **Neutralise inbound markers** — strip or defuse `<!-- lf:` in bodies arriving *from the forge* before classification, or match the marker **line-anchored** (a quoted marker is `> <!-- lf:` and no longer matches) | Small Go change in `internal/turn`, plus a characterization test on the existing ordering first | Touches security-relevant ordering; needs care |
| **Instruct rather than fix** — litefactory's question comment tells you to reply plainly, not by quote | **Zero.** One sentence of prompt/report text | Relies on a human remembering, every time, forever. The failure is silent when they don't |

**(b) The same matcher is also a label-upgrade hole (F-19).** Any third party can promote their
comment from `old_third_party` (*"someone who cannot command litefactory"*) to `litefactory`
(*"your own earlier replies"*) by writing the eight characters `<!-- lf:` anywhere in it.
Pre-existing; B2 makes it more attractive by training the thread to contain long marker-bearing
litefactory documents worth impersonating.

**Here I disagree with the review's framing and put the choice to you rather than mandating it.**
The architect offers "classify by author first, marker as tiebreaker" as the real fix — and it is.
But `classify.go:193-196` defends the current ordering for a stated reason: the bot account may
*be* the owner's account by misconfiguration, in which case author-first would make litefactory
answer itself in a loop. **Inverting that precedence is a security change larger than issue #7 and
should not be smuggled in under a usability fix.** Line-anchoring fixes your quote-reply and
narrows the accidental case without touching precedence; it does not stop a deliberate
impersonator, who can simply anchor the marker himself.

**Recommendation: fix (a) by line-anchoring the marker match, and log every self-drop with
`SelfReasonMarker` so it is at least visible. Defer (b) to its own change** — but say so out loud
in `Out of Scope` rather than letting it look handled. **If you want (b) done now, it is a
different and larger story and I would want it planned as one.**

### Q11 — Do you want the parked state at all, or is the lightweight version enough?

**Verified, and it changes the sizing of the whole feature:** `previousRun()` resumes on any
*terminal* run status, and that set is `succeeded`, `failed`, `reported`
(`turn/prepare.go:343-352`). There is **no mode gate, no time window, no other condition.**

So a run that asks its question in the final message and simply **ends normally** is *already*
resumed when you reply. **The question round-trip works today.** What is missing is only the prompt
text permitting it.

That splits ask (A) into two very differently priced things:

- **Story A1 — opt-in asking on existing machinery.** Prompt paragraphs, a tag, a resume paragraph,
  tests. **No new state, no migration, no new renderer, no sidecar.**
- **Story A2 — parking as a first-class state.** `needs_input` run status and session state,
  `lf:question` marker and renderer, sidecar channel, watermark/consumption handling, visibility in
  `litefactory status`. Buys: a question that does not report as **✅ succeeded**, a state you can
  see, and the only honest version of Q9(c).

**Rider — if A2 is yes, what happens when nobody answers?** There is no reaper: `ReconcileOrphans`
runs once at daemon start, there is no periodic sweep, and `claude.timeout_seconds` does not apply
to a run that ended cleanly. A parked session would sit indefinitely, indistinguishable from `idle`.
Options: park forever · park forever but visible in `litefactory status` · time out after N hours
and fall back to assume-and-disclose.

**Recommendation: ship A1, then judge A2 from experience.** A1 is close to free. A2 is real work
whose value I cannot judge for you — and the research brief's urgency for it (caps, timeouts, "the
bar for asking") **drops sharply now that asking is opt-in and rare**, which was your inversion.
If A2 is built, park forever *and* make it visible; do not build a reaper for a rare opt-in path.

---

## User Story S0 — The owner's reply is never silently dropped *(new this revision)*

> As the owner, I want my reply to a litefactory comment to actually start a run, so that answering
> a question is not a coin flip on which GitHub button I pressed.

Stops a quote-reply from being misclassified as litefactory's own output and discarded. **Ships
first**, because every other story in this plan depends on the owner being able to reply to a
comment that carries a marker — and today, on the most natural reply path, he cannot.

**This is not scope creep for its own sake.** It is a precondition. Ship A1 or B2 before it and the
first real use of either fails silently, with no log line the owner will see.

### Architecture Notes

- **The defect in one line:** `HasMarker` = `strings.Contains(body, "<!-- lf:")` anywhere
  (`turn/classify.go:38`), and `selfReason` is tested **before** the owner check
  (`classify.go:207-214`). A quoted marker therefore beats owner authorship.
- **The ordering is deliberate and must be preserved.** `classify.go:193-196` documents why: the
  bot account may be the owner's own account by misconfiguration, and author-first classification
  would make litefactory answer itself. **Do not invert precedence in this story.** Line-anchoring
  narrows the match without touching the order.
- **Line-anchoring is what fixes quote-reply specifically:** GitHub prefixes quoted lines with
  `> `, so a quoted marker appears as `> <!-- lf:` and a `^<!-- lf:` match no longer fires, while
  litefactory's own comments — where the marker is emitted at the start of a line — still match.
- **Characterize before changing.** The current classification ordering is security-relevant and
  under-pinned by tests. Pin it first, then change it. This is the same discipline F-7 asks for in
  A1.
- **Silence is the real bug.** Even after the fix, some bodies will legitimately classify `self`.
  Every drop must be logged with its `SelfReasonMarker` so the next occurrence is diagnosable
  instead of invisible.
- Platform: no new package, no new dependency, no goroutine, no query, no HTTP surface, no
  migration. A matcher change and logging inside `internal/turn`.
- Out: the deliberate-impersonation half (F-19) — see Q10(b) and `Out of Scope`.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-S0.1 | Settle and record the route — line-anchor, strip inbound, or instruct-only | Design | — | **BLOCKED-ON-Q10** · S |
| T-S0.2 | Characterization tests pinning today's `Classify` ordering: owner+marker → self; third-party+marker → self; litefactory comment → self. Written **before** any change | Test | — | S |
| T-S0.3 | Change the marker match to line-anchored (`^<!-- lf:`), or neutralise inbound markers, per T-S0.1 | Go | T-S0.1, T-S0.2 | M |
| T-S0.4 | Log every self-classified drop with its `SelfReasonMarker` at a level the owner can find | Go | T-S0.2 | S |
| T-S0.5 | Tests: an owner quote-reply body (leading `> ` before the marker) classifies as `instruction` and starts a run; litefactory's own comment still classifies `self` | Test | T-S0.3 | M |
| T-S0.6 | *(instruct-only route, or belt-and-braces alongside T-S0.3)* Litefactory's question/report comment tells the owner to reply plainly rather than by quote | Docs | T-S0.1 | S |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-S0.1 | A comment authored by the owner whose body contains `<!-- lf:` **only inside quoted lines** classifies as an instruction and starts a run. |
| AC-S0.2 | A comment genuinely authored by litefactory still classifies `self` and is still dropped. Regression test. |
| AC-S0.3 | Author precedence in `Classify` is **unchanged** — `selfReason` is still tested before the owner check, and `classify.go:193-196`'s reasoning still holds. |
| AC-S0.4 | Every self-classified drop emits a log line naming `SelfReasonMarker`. No comment is discarded without a trace. |
| AC-S0.5 | The characterization tests from T-S0.2 exist and were committed before the behaviour change. |
| AC-S0.6 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## User Story B1 — Research briefs get much shorter

> As the owner, I want a research brief I can actually read, so that I do not have to skim a
> thousand lines to find the four decisions that are mine.

Adds a hard length budget and a fixed section list to the brief template in
`research-orchestrator.md`. Zero Go. The fastest thing in this plan to ship.

### Architecture Notes

- The root cause is plain: `research-orchestrator.md`'s brief template (lines 51-73) specifies
  **six section headings and no budget whatsoever.** Length was never constrained, so it grew.
- Two verified numbers make the budget principled rather than arbitrary — `MaxCommentBytes = 8,192`
  and `maxResultBytes = 32,768`. See Q5.
- **This story is a hard prerequisite for B2 under Q7 route (i)**, and the architect strengthens
  why: an oversize brief is truncated **twice** (`run.go:528`, then `runBody`'s step-2 cut), and
  below `len(resultHeading)+minTailBytes` the result section is replaced wholesale by
  `droppedResultNotice`. At 8 KiB none of that machinery ever engages.
- Risk: a budget with no enforcement is a suggestion. Prefer a stated byte count the orchestrator
  is told to self-check and report over an adjective.
- The trade to name honestly: cutting provenance tagging is a real loss under Q1=moved and a
  non-loss under Q1=mirrored. The two questions are coupled.
- Platform: **not applicable** — markdown only, no Go, no build, no tests to run. Nothing in
  `platform-constraints.md` engages.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-B1.1 | Add an explicit byte/line budget and hard ceiling to `research-orchestrator.md`'s brief template | Docs | — | **BLOCKED-ON-Q5** · S |
| T-B1.2 | Add the surviving-section list and an explicit "cut these" list | Docs | T-B1.1 | S |
| T-B1.3 | Require the orchestrator to state the brief's final byte count in its closing message, so the budget is observable | Docs | T-B1.1 | S |
| T-B1.4 | Cap `codebase-tour.md`'s return format for the same unbounded-output problem | Docs | T-B1.1 | S |
| T-B1.5 | Record in the template where the cut detail goes instead — the file, or nowhere | Docs | T-B1.1 | **BLOCKED-ON-Q1** · S |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-B1.1 | `research-orchestrator.md` states a numeric budget and a hard ceiling for `RESEARCH_BRIEF.md`. |
| AC-B1.2 | **(manual / observational — not gateable.)** The next brief produced under the changed template is within the Q5 target, measured in bytes. No automated check exists; verified by observing the next research run. **`implement-orchestrator` must not block on this.** |
| AC-B1.3 | The template names both the sections that must be present and the material that must not appear. |
| AC-B1.4 | Open questions and Recommended scope both fall within the first 8,192 bytes of the template's prescribed order. |
| AC-B1.5 | `codebase-tour.md` states a return-size cap. *(Added this revision — T-B1.4 previously had no AC.)* |
| AC-B1.6 | No Go file is modified by this story. |

---

## User Story B2 — The research brief lands as a comment on the issue

> As the owner, I want the research brief to appear on the issue, so that I read it where I already
> read everything else instead of going looking in the repo.

Delivers the brief's content to the issue thread. Its shape depends entirely on Q7; under the
recommended route it is a documentation change with no Go at all.

### Architecture Notes

- **Under Q7 route (i) this story is nearly empty.** `secResult` already posts the run's final
  message. The work is instructing the orchestrator to put the brief there, plus the oversize
  guard. Answer Q7 before estimating this.
- **Corrected this revision (F-4 — verified). Rev 1's central argument against route (ii) was
  wrong and has been deleted.** It claimed that feeding a brief through `RenderRun` "would elide it
  as though it were a log tail". The opposite is true: `report/run.go:165-175` states the section
  order **is** the survival order, and `runBody` cuts log tail → result → files. `secResult` is the
  *second-most-protected* section. Route (ii)'s remaining costs are Go work and history slots (Q7),
  not elision.
- **Under route (ii) it still needs a brief-shaped renderer**, because `internal/report`'s sections
  are fixed in code as `secFixed / secResult / secFiles / secOutput` and a brief is not one of them.
  Reuse the pattern, not the code. A `<!-- lf:brief` marker self-filters free from the existing
  `MarkerPrefix = "<!-- lf:"` match.
- **Route it through `internal/report` either way.** `redact.Body` has no exported constructor, so
  "post an unredacted body" is not an expressible program. Credential redaction, the residual
  assertion and the panic guard come free.
- **Redaction is not sanitisation, and a brief needs both.** `redact` covers credentials only. A
  brief is agent-authored text going straight into a comment, so it can carry markdown that breaks
  the report's `<details>` fence or its callout structure. `report/run.go` already has `sanitize`
  and `fenceFor`; **the brief must route through the same two.** *(Architect, sidecar hygiene.)*
- **Decision taken — never fail silent.** Today an oversize body returns a `LimitError`, no comment
  is posted, the run goes unreported and the watermark does not move. For a feature whose purpose is
  visibility, "too big to show you, so you see nothing" is the wrong failure. Truncate with an
  explicit marker naming what was cut. *(Flag if you disagree; a loud hard-fail telling the
  orchestrator to shorten the brief is the defensible alternative and would make Q5 self-enforcing.)*
- **Verify before building T-B2.5.** The architect's reading is that under route (i) the silent-
  failure path may already be unreachable for `RenderRun`: `runBody` trims to budget, `run.go:528`
  truncates with an explicit marker, and `redact.Redactor.Apply`'s hard-truncate is the backstop.
  **If so, T-B2.5 collapses to S and AC-B2.2 becomes a regression test rather than new work.**
- **Decision taken — do not build multi-comment splitting.** Nothing in the tree splits one document
  across comments; `post` is the single forge call in `internal/turn` and is not in a loop. It
  interacts badly with watermark ordering and with the duplicate-post trade-off.
- **Do not absorb LF-7** (blob-SHA permalinks). Unstarted and actively test-blocked —
  `report/report_test.go:240-243` fails any body containing `/blob/`. A "brief attached, link below"
  design cannot link to anything today.
- **Security: a brief posted as a comment re-enters the prompt as history, wearing litefactory's own
  label.** Briefs quote third-party and owner text verbatim. Quoted text that re-enters a later
  prompt as `[litefactory]` output has been laundered from `[old_third_party]` into trusted
  material. Keep quoted third-party text clearly attributed and fenced, and prefer summarising over
  quoting.
- **Depends on S0.** A posted brief is a long, marker-bearing, highly quotable document. Until S0
  ships, replying to one by quote-reply is silently discarded.
- Known, accepted degradation: a posted brief is replayed **truncated to its first 8,192 bytes**
  against a 32 KiB total history block. Story B1 makes this a non-issue rather than a compromise.
- Platform: **no new `internal/github` method under any of Q7's three routes**, so the
  seven-endpoint surface test stays closed. Only a Q2 answer of "a GitHub label" would break that —
  which is why Q2 and Q7 should be answered together.
- Out: nothing here touches code review, and nothing here creates issues or sub-issues.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-B2.1 | Settle the delivery route and record it before writing code | Design | — | **BLOCKED-ON-Q7** · S |
| T-B2.2 | *(route i)* Instruct `research-orchestrator.md` to emit the brief as its final message | Docs | T-B2.1 | S |
| T-B2.3 | *(route ii only)* Brief-shaped renderer in `internal/report` with an `lf:brief` marker and its own section priority | Go | T-B2.1 | L |
| T-B2.4 | *(route ii only)* Sidecar signal channel at a **fixed path named in the prompt** — not an env var; `harness/env.go` is a closed eight-name allowlist (SD-4) and widening it is a security change | Go | T-B2.1 | L |
| T-B2.5 | **First verify** whether `RenderRun`'s existing truncation already makes silent oversize failure unreachable. If it does, this collapses to a regression test; if not, add an explicit truncation marker | Go | T-B2.1 | S–M |
| T-B2.6 | Route the brief body through `report`'s existing `sanitize` and `fenceFor` so agent-authored markdown cannot break the report's fence or callout structure | Go | T-B2.1 | S |
| T-B2.7 | Decide and document whether `RESEARCH_BRIEF.md` is still written | Docs | — | **BLOCKED-ON-Q1** · S |
| T-B2.8 | *(only if Q1 = moved)* Amend `CLAUDE.md`'s Plan gate and `initialplan.md:243`'s artifact-inference principle, in writing, in the same change | Docs | T-B2.7 | M |
| T-B2.9 | Tests: a brief at the Q5 target, at 32,768 bytes and at 70,000 bytes all produce a posted comment | Test | T-B2.5 | M |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-B2.1 | **(manual / observational — not gateable.)** After a Research run, the brief is readable on the issue without opening the repo. There is no failing condition a test can express; confirmed by looking at the issue. **`implement-orchestrator` must not block on this.** |
| AC-B2.2 | An oversize brief still produces a comment, explicitly marked as truncated. **Silence is a failing test.** |
| AC-B2.3 | The comment carries an `<!-- lf:` marker and is filtered out of the bot's own input. |
| AC-B2.4 | The comment body passes the redaction residual assertion; no credential reaches the forge. |
| AC-B2.5 | Agent-authored markdown in the brief cannot break the report's fence or callout structure — the body goes through `sanitize`/`fenceFor`. |
| AC-B2.6 | `internal/github` still exposes exactly seven network methods; `TestClientMethodSetIsTheSevenEndpoints` is unchanged and green. |
| AC-B2.7 | The comment body contains no `/blob/`; the existing LF-7 guard stays green. |
| AC-B2.8 | *(if Q1 = mirrored)* `$session/RESEARCH_BRIEF.md` still exists and `CLAUDE.md`'s Plan gate is unmodified. |
| AC-B2.9 | *(if Q1 = moved)* `CLAUDE.md` and `initialplan.md` are amended in the same change, not left contradicted. |

---

## User Story A1 — Opt-in asking, on the machinery that already exists

> As the owner, I want a run I have tagged to stop and ask me in a comment instead of guessing, so
> that a wrong assumption does not propagate into a brief, then a plan, then code.

Replaces the unconditional headless gag with a two-way branch on an opt-in tag, and makes the
resumed run's permission explicit. No new state — the existing resume path already carries the
round trip.

**Sized honestly: the *diff* is small, the *risk* is not.** This is the only story that can change
the behaviour of untagged runs, and it carries three unanswered owner decisions (Q3, Q8, Q9) plus
three sharp defects (F-9, F-10, F-16).

### Field names — one set, declared here

**`MayAsk` and `PreviousMayAsk`.** This **supersedes** `feature-spec.md:47-48`
(`MayAsk`/`AnswersQuestion`) and rev 1's `AskingAllowed`/`PreviousAsked`. Wherever those names
appear in earlier artifacts, this table wins.

| Field | Meaning | Source | Derivable in A1? |
|---|---|---|---|
| `MayAsk bool` | may **this** run ask? | the tag on **this** run's trigger comment | **yes** |
| `PreviousMayAsk bool` | did the **previous** run have permission? | re-parse the tag from the previous run's trigger comment | **yes** — see below |
| ~~`PreviousAsked` / `AnswersQuestion`~~ | did the previous run actually ask? | requires `needs_input` | **no — deferred to A2** (see Q9) |

**Why the wrong bit matters:** `PreviousAsked` answers *"did the last run ask?"*; only *"may this
run ask?"* governs permission. Because `writeResumeHeader` emits no permission statement, silence
on the resume path means *inherit from transcript* — so the dangerous cell is **previous run tagged
→ this resume untagged**, where the transcript still says "you may ask" and nothing contradicts it.
`PreviousAsked` never touches that cell.

**`PreviousMayAsk` is derivable with zero schema cost — verified this revision — but the review's
"same shape as `PreviousMode`, free" is not quite right, and the difference is a task.**

- `PreviousMode` is a **store column read**: `previousRun` returns the full row and `prepare.go:175`
  does `PreviousMode: previous.Mode`. There is no such column for permission.
- `PreviousMayAsk` must instead be **re-parsed from the previous run's trigger comment**. Everything
  needed is already in memory: `store.Run.TriggerCommentID` (`store/runs.go:75`, in `runColumns`
  and populated by every `ListRuns`, currently unused by `prepare`) matched against the comment list
  that `commentHistory` already loads unconditionally on both paths (`prepare.go:149-152`, `:297`).
- **The watermark is not a problem** — `store.ListComments` (`store/comments.go:151-154`) is
  `WHERE session_id = ? ORDER BY forge_comment_id` with no watermark predicate, and nothing in
  `internal` ever deletes comment rows.
- **But it is not free.** `commentHistory` returns `[]harness.HistoryEntry`, and `HistoryEntry`
  (`harness/prompt.go:45-60`) carries **no forge comment id**. So either `commentHistory` also
  returns the matched body, or the match happens inside it. Either way it is a small local refactor
  in `prepare.go` — real work, not a field assignment. **T-A1.6 is sized accordingly.**
- **One genuine gap that needs a fallback:** if the previous run was triggered by the issue body
  rather than a comment, `previous.TriggerCommentID == 0` and no comment row exists
  (`store/runs.go:122-132`). The parse must fall back to `req.Issue.Body`, which is also already in
  memory. **This is T-A1.7 and it has its own AC.** *(Pre-bootstrap comments are not a gap: a
  comment below the bootstrap watermark was never processed and so never triggered a run.)*

### Architecture Notes

- **The feature is prompt text plus one bit.** Everything else is plumbing.
- **There are TWO headless paragraphs, not one, and they must change together.** The known gag at
  `harness/prompt.go:396-401` and the feature-slug paragraph at `:388-390` — *"It is stated here
  because this run is headless and no one can answer a question."* Changing only the first leaves a
  contradiction inside the same header.
- **CRITICAL (F-9, verified) — the gag paragraph bundles two unrelated instructions and must be
  split before it is branched.** It reads: *"…do not stop for approval. Where something is
  ambiguous, take the most reasonable reading, act on it, and say what you assumed in your final
  message. **Do the work in this worktree; do not commit or push — litefactory does that once you
  are finished.**"* Branching the whole paragraph on `MayAsk` **drops the commit/push prohibition on
  every tagged run**, colliding with litefactory's own commit step. Split it first: the
  worktree/commit sentence stays **unconditional**, only the ambiguity sentences branch. This is
  T-A1.2 and it gates T-A1.4.
- **F-10 — the slug paragraph's substance must survive.** Deleting `:388-390` outright satisfies a
  naive reading of AC-A1.2, but stating the feature slug **is** the D-3 mechanism; without it a run
  stops on a question that will never be read, because nobody knows which session it belongs to.
  **Only the justification clause (*"because no one can answer"*) may change.** AC-A1.3 pins this.
- Both paragraphs sit in `writeHeader`, the last unconditional statements in it, emitted on **every
  cold prompt regardless of mode**. `writeHeader` is called only when `!in.Resume`
  (`prompt.go:290-295`).
- **`writeResumeHeader` (`prompt.go:404-425`) gains a conditional permission paragraph.** Follow the
  precedent already inside that same function — `if mode == InferMode && mode !=
  strings.TrimSpace(in.PreviousMode)` (`prompt.go:422`) — rather than inventing a mechanism.
- **Emit whenever `MayAsk != PreviousMayAsk`, in BOTH polarities, and write the reason into the code
  comment.** Structurally identical to `writeInferGuidance`, but with a difference that must be
  recorded or someone will "simplify" it back to one-directional: for infer-guidance, not emitting
  is merely *redundant*; here, **not emitting fails open** — the transcript's stale "you may ask"
  stands unchallenged.
- **`KindFreeText` carries the whole comment body as the instruction.** A tag line must be stripped
  or it becomes part of what the agent is asked to do.
- **F-15 — the free-text path is not the only path.** `ParseCommand` (`command.go:152`) also yields
  `KindSpike` with `Text = TrimSpace(rest)`, so a tag inside `/spike research-mode: ask do X` lands
  mid-`Text` and must be stripped there too.
- **F-16 — the tag-only comment is an unhandled failure.** A comment containing nothing but the tag
  goes `firstToken` → `KindFreeText` → strip → empty `Instruction` → `BuildPrompt` returns
  `ErrInvalidPrompt`, and the run dies with a validation error the owner will not understand.
  Recommended: treat it as `KindNone` — no run at all. AC-A1.7 pins it.
- **F-22 — match the tag line-anchored, never with `Contains`.** Tag text is owner-only in practice
  (it is read only from a comment that already classified `instruction`), so injection risk is low —
  but an issue body or comment *quoting this plan* contains the literal `lf: ask`, and a `Contains`
  match would turn permission on. Two guardrails: line-anchor the match, and ensure the tag never
  reaches `Instruction` un-stripped, since `Instruction` is documented as trusted and is the one
  field with **no size cap**.
- **Security: the tag must be honoured only from the triggering comment, read by Go.** Third-party
  comments trigger nothing (`classify.go:150,214`) but they *are* recorded and replayed into history
  as `old_third_party`. A tag appearing in replayed history, in a third-party comment, or in the
  issue body of an unrelated run must grant no permission. AC-A1.5.
- **Error handling (platform).** `ParseCommand`'s contract is *"never fails. Every body classifies
  as something."* **The tag parser must not return an `error`** — a malformed tag is simply
  *absent*. Returning an error breaks the invariant the whole command layer rests on.
- Constraint worth knowing: **the owner's answer comment is itself capped at 8,192 bytes**
  (`MaxCommentBytes`, applied to the triggering comment on both paths). A very long answer is
  truncated on the way in.
- Edge case: a resumed run triggered by the **issue body** returns before `## Context` is written
  (`prompt.go:306`) and never sees the permission paragraph. Rare — document, do not fix.
- **Depends on S0.** The whole point of A1 is that the owner replies to litefactory's question
  comment. Until S0 ships, a quote-reply to that comment is silently dropped and the parked run
  never resumes.
- Platform: **most of `platform-constraints.md` does not apply** — no new package, no HTTP layer, no
  DB access, no migration, no new dependency, no concurrency (this is synchronous string assembly).
  What applies is the error-handling rule above and the testing rule below.
- **Existing test coverage, named precisely (F-6). Rev 1's "two golden tests" was wrong** — there is
  no `testdata/` or `.golden` file in `internal/harness`. There are:
  - **`TestSpikePromptIsUnchanged` (`harness/prompt_test.go:359`)** — one full-head literal
    comparison. **This is the one that must pass byte-for-byte unmodified.**
  - **`TestBuildPromptStatesTheFeatureSlugAndForbidsQuestions` (`prompt_test.go:76`)** — a substring
    test asserting **both** facts in one test. **A1 splits those two facts apart, so this test must
    itself be split** (F-11) into a slug assertion (both paths) and a gag assertion (untagged only).
    It is therefore *not* possible to "assert both existing tests are untouched" — one of them
    changes by design, and that change must be reviewed deliberately, not waved through.
- **F-7 — `writeResumeHeader` is entirely uncovered.** `internal/harness` has **zero** tests setting
  `Resume: true` or `PreviousMode`. AC-A1.9 ("today's resume header, unchanged") currently has
  nothing protecting it. **A characterization test must be written before the function is touched**
  — T-A1.8.
- **The tag parser is the load-bearing new function in this plan.** It is the same code that reads
  `MayAsk`, derives `PreviousMayAsk`, and — if Q6 answers "all modes" — gates Plan and Implement.
  It gets its own acceptance criterion (AC-A1.4).
- Risk: **ungated asking is R-3.** The tag is what prevents it. **A1 must not ship without T-A1.1.**
- Out: code review is ring-fenced — nothing in `go-code-reviewer`, `CODE_REVIEW.md`, or the
  Implement-mode review gates.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-A1.1 | Tag parser: recognise the tag **line-anchored** on the triggering comment, strip it from the instruction text, **never return an error** | Go | — | **BLOCKED-ON-Q2**, **BLOCKED-ON-Q3**, **BLOCKED-ON-Q4** · M |
| T-A1.2 | **Split the gag paragraph (`prompt.go:396-401`)** so the worktree/commit-and-push sentence is unconditional and only the ambiguity sentences are branchable. No behaviour change yet | Go | — | S |
| T-A1.3 | Plumb `MayAsk` from the parsed comment through `turn.Request` to `harness.PromptInput` | Go | T-A1.1 | S |
| T-A1.4 | Write the permission text: ask only when the answer changes what gets built and cannot be settled by reading the repo; ask by stopping; batch into one message | Prompt text | T-A1.2 | M |
| T-A1.5 | Branch **both** headless paragraphs in `writeHeader` on `MayAsk`; keep the slug statement on **both** paths; untagged output byte-for-byte unchanged | Go | T-A1.3, T-A1.4 | M |
| T-A1.6 | Derive `PreviousMayAsk`: surface the previous trigger comment's body out of `commentHistory` via `previous.TriggerCommentID`, and re-parse the tag from it | Go | T-A1.1 | M |
| T-A1.7 | Body-trigger fallback: when `previous.TriggerCommentID == 0`, parse the tag from `req.Issue.Body` instead | Go | T-A1.6 | S |
| T-A1.8 | **Characterization test for `writeResumeHeader`** — pin today's output for `Resume: true` with and without `PreviousMode`. Written **before** T-A1.9 | Test | — | M |
| T-A1.9 | Add the conditional permission paragraph to `writeResumeHeader`, emitted when `MayAsk != PreviousMayAsk` in **both** polarities, with a code comment recording that non-emission fails open | Go | T-A1.6, T-A1.8 | M |
| T-A1.10 | Decide what the resume header may claim about answers, and write it | Prompt text | T-A1.9 | **BLOCKED-ON-Q9** · S |
| T-A1.11 | Enforce the ask-round cap in Go or the resume header — never in cold-path prompt text | Go | T-A1.9 | **BLOCKED-ON-Q8** · M |
| T-A1.12 | Handle the tag-only comment: treat as `KindNone`, no run, rather than `ErrInvalidPrompt` | Go | T-A1.1 | S |
| T-A1.13 | Strip the tag on the `KindSpike` path too (`command.go:152`, `Text = TrimSpace(rest)`) | Go | T-A1.1 | S |
| T-A1.14 | **Split `TestBuildPromptStatesTheFeatureSlugAndForbidsQuestions` (`prompt_test.go:76`)** into a slug test asserting both paths and a gag test asserting the untagged path only | Test | T-A1.5 | S |
| T-A1.15 | New head tests: tagged cold header, and the resume header under all four `MayAsk`×`PreviousMayAsk` combinations. Assert `TestSpikePromptIsUnchanged` still passes unmodified | Test | T-A1.5, T-A1.9 | M |
| T-A1.16 | Table-driven tag-parsing tests: present, absent, malformed, mid-body, tag-only, quoted/indented, **tag in a third-party comment**, **tag in replayed history**, **tag in an issue body quoting this plan** | Test | T-A1.1 | M |
| T-A1.17 | `turn`-level test: a comment arriving after a succeeded run in the same segment produces `Resume: true` / `RunPath = resume` | Test | T-A1.9 | S |
| T-A1.18 | Update `prompt.go`'s package doc — it currently states the design rationale this story falsifies | Docs | T-A1.4 | S |
| T-A1.19 | Note in `research-orchestrator.md` that its log-and-continue rule survives; asking is a higher tier above it, not a replacement | Docs | T-A1.4 | S |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-A1.1 | An **untagged** run's cold prompt is byte-for-byte identical to today's. `TestSpikePromptIsUnchanged` (`prompt_test.go:359`) passes **unmodified**. |
| AC-A1.2 | A **tagged** run's cold prompt contains neither *"do not ask clarifying questions"* nor *"no one can answer a question"*. |
| AC-A1.3 | **A tagged run's cold prompt still states the feature slug.** Deleting `prompt.go:388-390` outright is a failing outcome, not a passing one. |
| AC-A1.4 | The tag parser matches **line-anchored**, returns **no error** for any input, and treats a malformed tag as absent. A body merely *quoting* the tag inside a quoted or indented line grants no permission. |
| AC-A1.5 | A tag in a third-party comment, in replayed comment history, or in an unrelated issue body grants **no** permission. |
| AC-A1.6 | The tag does not appear in the instruction text handed to the agent, on **either** the `KindFreeText` or the `KindSpike` path. |
| AC-A1.7 | A comment containing only the tag starts no run and produces no `ErrInvalidPrompt`. |
| AC-A1.8 | **The worktree/commit-and-push prohibition is present on every cold prompt, tagged and untagged alike.** A tagged prompt missing it is a failing test. |
| AC-A1.9 | When `MayAsk == PreviousMayAsk`, the resume header equals the output pinned by T-A1.8's characterization test, byte for byte. |
| AC-A1.10 | When `MayAsk != PreviousMayAsk`, the resume header contains an explicit permission statement — in **both** directions. A test covers all four combinations. |
| AC-A1.11 | The previous run's permission is recovered from its trigger comment; when `TriggerCommentID == 0`, from the issue body. **No new store query, no new column, no migration.** |
| AC-A1.12 | The owner's reply resumes the same Claude conversation via the existing resume path — asserted at `turn` level: a comment after a succeeded run in the same segment yields `Resume: true` / `RunPath = resume`. |
| AC-A1.13 | The ask-round limit is enforced in Go or the resume header, provably not in cold-path prompt text. |
| AC-A1.14 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## User Story A2 — Parking as a first-class state *(proposed; hold pending Q11)*

> As the owner, I want a run that is waiting on my answer to say so, so that it does not report as
> a success and does not vanish from view.

Makes "stopped, correctly, pending a human" a real state rather than a `succeeded` run with a
question in its text. **Do not start this story until Q11 is answered** — Story A1 may make it
unnecessary.

### Architecture Notes

- **Whatever state is added must be TERMINAL.** `previousRun()` resumes only on
  `succeeded`/`failed`/`reported` (`turn/prepare.go:343-352`). A non-terminal parked status means
  the answering run does **not** resume the conversation that asked — it starts cold and the
  question's context is gone.
- **The real trap, corrected this revision (architect, platform lens).** Rev 1 said A2's platform
  concern was "new store queries". That was wrong — A2 adds no query, it adds values to existing
  typed string constants. The actual trap is that **`terminalRunStatus` is an explicit switch with
  `default: return false`**, so a status added to the store but not to that switch **silently
  becomes non-terminal**. No compile error, no test failure. The symptom is a cold start with the
  question's context gone — exactly the failure the story exists to prevent. T-A2.10 exists solely
  to catch this.
- **No migration.** Enumerated columns carry no CHECK constraint on purpose
  (`store/migrations/0001_init.sql:10-14`), explicitly so a later state costs nothing. Go-side
  gates: `SessionState.valid()` (`store/sessions.go:248`), `FinishRun`'s status check,
  `CompleteRunReport`'s switch, and `turn/result.go`'s `Reportable()`/`ConsumesComment()`.
  Note `store/store_test.go:382` asserts that a state outside the M0 set is **rejected** — that
  test is a deliberate gate and updating it is part of the work, not an accident.
- **The sharpest trap: a parked run must still consume its own trigger comment and advance the
  watermark.** `drain` stops at the first unconsumed comment and `AdvanceWatermark` is a monotonic
  `MAX` that never rewinds. A non-consuming parked disposition deadlocks the task permanently and
  the answer is never read. Test this case explicitly.
- Use `needs_input` — the name `initialplan.md` already established, and the state
  `store/sessions.go:15` and `0001_init.sql:13` both anticipate as *"LF-11's `blocked`"*.
  **LF-12, the permission gate, is the second caller of this same machinery.** Building it twice is
  the failure mode worth naming now.
- Copy the harness's **structural** refusal detection (`resuming && Outcome == OutcomeFailed &&
  Model == "" && ExitCode > 0`) rather than matching on text — the existing code is explicit that
  the wording belongs to another project's binary and a reword would wedge the task.
- Signal channel: **fixed path named in the prompt, not an env var.** `harness/env.go` is a closed
  eight-name allowlist and widening it is a documented security boundary (SD-4) — for zero benefit
  here. The rule from `initialplan.md` stands: **missing or malformed → fall back to today's
  behaviour, never guess.** A `needs_input` signal with an empty `questions[]` is malformed — a park
  with no question is a deadlock.
- **Sidecar hygiene.** A question body is agent-authored text going straight into a comment. It can
  legitimately contain `<!-- lf:` (self-filtered, harmless — litefactory posted it) and markdown
  that breaks the report's `<details>` fence or callout structure. **Route the question renderer
  through `report/run.go`'s existing `sanitize` and `fenceFor`**, exactly as B2 does.
- **A2 is what makes Q9(c) honest.** Only with `needs_input` does Go actually know a question was
  asked, and only then can the resume header say so without speculating.
- Risk: a duplicate question comment is possible by design — post succeeds, DB write fails, a later
  tick posts a second. Tolerable for a report, more confusing for a question. Marker-based recovery
  is already contemplated in `report/marker.go`.
- Risk: **cost.** Every non-empty owner comment spawns a run. A question/answer cycle at least
  doubles runs per exchange, and there is no rate limiting until LF-11.
- Open: what a `cold_after_resume_refused` rerun does with a pending question. The transcript is
  gone and the answer is just another `old_owner` entry in replayed history. A fourth history label
  (`answer`) alongside `old_owner`/`old_third_party`/`litefactory` is likely the whole fix.
- Platform: new branch conditions use typed constants, never string compares — already the
  convention in `internal/store`. No new query, no migration, no new dependency.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-A2.1 | Add a terminal `needs_input` run status and session state; update `SessionState.valid()` and the M0-set test; no migration | Go | — | **BLOCKED-ON-Q11** · M |
| T-A2.2 | **Add `needs_input` to `terminalRunStatus`'s explicit switch** — the `default: return false` makes omission silent | Go | T-A2.1 | S |
| T-A2.3 | Update `Reportable()`/`ConsumesComment()` — a parked run **must** consume its trigger and advance the watermark | Go | T-A2.1 | M |
| T-A2.4 | Sidecar signal channel at a fixed path; missing, malformed, or question-less falls back and never parks | Go | T-A2.1 | L |
| T-A2.5 | `lf:question` marker and renderer alongside `RenderRun`/`RenderReply`, routed through `sanitize`/`fenceFor` | Go | T-A2.1 | M |
| T-A2.6 | Pickup rule: while a session is parked, the next owner instruction comment is the answer | Go | T-A2.3 | M |
| T-A2.7 | Fourth history label for an answer comment, for the `cold_after_resume_refused` path | Go | T-A2.6 | M |
| T-A2.8 | Surface the parked state in `litefactory status` so it is distinguishable from `idle` | Go | T-A2.1 | S |
| T-A2.9 | No-answer behaviour | Go | T-A2.1 | **BLOCKED-ON-Q11** · M |
| T-A2.10 | Test that `needs_input` is terminal, by asserting `previousRun()` returns a parked run as resumable. **This is the only thing standing between T-A2.2 being forgotten and a silent cold start** | Test | T-A2.2 | S |
| T-A2.11 | Regression test: a parked run must not wedge the comment queue | Test | T-A2.3 | M |
| T-A2.12 | *(if Q9 = (c))* Promote the resume header's answer framing from hedged to definite, now that Go knows | Prompt text | T-A2.1 | S |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-A2.1 | A run that asks reports as awaiting input, not as ✅ succeeded. |
| AC-A2.2 | `needs_input` is present in `terminalRunStatus`'s switch, asserted by a test that fails if it is removed. |
| AC-A2.3 | A parked run consumes its trigger comment and advances the watermark; a test asserts the queue is not wedged. |
| AC-A2.4 | The owner's next instruction comment resumes the parked run in its original mode. |
| AC-A2.5 | The parked state is terminal — the answering run uses `--resume`, not a cold start. |
| AC-A2.6 | A missing, malformed, or question-less signal document produces today's disposition. No park is ever inferred. |
| AC-A2.7 | Exactly one question comment is posted per parked run, carrying an `lf:question` marker, and it is self-filtered. |
| AC-A2.8 | The question body passes through `sanitize`/`fenceFor`; agent-authored markdown cannot break the report structure. |
| AC-A2.9 | A parked session is distinguishable from `idle` in `litefactory status`. |
| AC-A2.10 | No schema migration is added. |
| AC-A2.11 | `harness/env.go`'s allowlist is unchanged — the signal channel adds no environment variable. |
| AC-A2.12 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## Shipping order

**Revised this revision, following the architect.**

1. **S0** — the self-filter / quote-reply fix. **First**, because both A1 and B2 depend on the owner
   being able to reply to a marker-bearing comment, and today the most natural reply path is
   silently discarded. Ship either before it and the first real use fails with no visible symptom.
   Needs Q10.
2. **B1** — no Go, no dependencies, and it is what makes B2 route (i) safe. Needs Q5.
3. **B2** — nearly empty under route (i), and its only correctness requirement is B1's byte budget.
   Needs Q1 and Q7.
4. **A1** — after the above, because it carries three unanswered owner decisions (Q3, Q8, Q9) plus
   F-9, F-10 and F-16, and it is **the only story that can change the behaviour of untagged runs**.
   Needs Q2, Q3, Q4, Q6, Q8, Q9.
5. **A2** — only if Q11 says so.

**Where I differ from the review: B1 and B2 stay two stories.** The architect recommends merging
them, on the grounds that B2 route (i) is a single instruction in `research-orchestrator.md` whose
only correctness requirement is B1's budget. That is true *under route (i)* — but **route (i) is not
yet chosen; it is Q7, and it is unanswered.** Under route (ii) B2 becomes an L-sized Go story with a
renderer and a sidecar. Merging an unanswered story into an answered one would either block B1 on
Q7 or force a de-merge later. They ship as one unit under route (i) — the shipping order says so —
while staying separable if Q7 goes the other way. **If you answer Q7 = (i), merging them is then
free and I would have no objection.**

**Hidden coupling worth stating once:** the tag parser (T-A1.1) is the same code that derives
`PreviousMayAsk` (T-A1.6) and, if Q6 answers "all modes", the same code that gates Plan and
Implement. It is the one genuinely load-bearing new function in this plan, and it is the reason
AC-A1.4 exists as a criterion of its own.

**The other coupling: Q2 → B2.** A tag that lives in a GitHub label would need an eighth client
method and would reopen the SDK question this plan otherwise avoids entirely.

---

## Out of Scope

- **Ask (C) — the implementation plan as GitHub sub-issues.** Deferred by the owner: *"sub issues
  we defer to a later run; i'll create a own story later on."* No client methods are added for it,
  no `IMPLEMENTATION_PLAN.md` parsing, no `sub_issue_id` schema work. The SDK-versus-hand-rolled
  question that `TestClientMethodSetIsTheSevenEndpoints` forces is therefore **not settled here and
  does not need to be** — no story in this plan adds an eighth method. *(Sole exception: a Q2
  answer of "a GitHub label" would reopen it.)*
- **Code review.** Ring-fenced by the owner: *"code review and alike are untouched by this!"*
  Nothing is proposed for `go-code-reviewer`, `CODE_REVIEW.md`, or the Implement-mode review gates.
- **F-19 — deliberate marker impersonation by a third party.** Any third party can promote their
  comment from `old_third_party` to `litefactory` by writing `<!-- lf:` in it. **Real, pre-existing,
  and explicitly NOT fixed by Story S0** — S0's line-anchoring stops the accident, not the attack.
  The real fix is inverting author-versus-marker precedence in `Classify`, which
  `classify.go:193-196` deliberately argues against for a stated reason. **That is a security change
  larger than issue #7 and should be planned as its own story.** Raised as Q10(b) so the owner can
  overrule this. *(Named here so it does not look handled.)*
- **Reworking the eleven interactive approval gates** across the agent definitions. Opt-in asking
  defuses the risk that motivated it; a twelve-file markdown rewrite is its own change.
- **LF-7 — blob-SHA permalinks.** Unstarted and actively test-blocked. Do not absorb it into B2.
- **LF-12 — the permission gate.** Named as the second caller of A2's parking machinery so it is
  not built twice. Not built here.
- **LF-6 labels, LF-13 draft PRs, PR review comments as input, `/include`, prompt-injection
  hardening, rate limiting (LF-11).**
- **Unblocking `/research` `/plan` `/implement` as slash commands** — a separate Plan-sized
  question per `mode-closure/SPIKE.md`.
- **Multi-comment document splitting.** Explicitly rejected under B2.
- **Any schema migration.** Nothing in this plan needs one.
- **Teaching Go what a mode is**, beyond matching one opaque literal tag (Q2/Q3) — and note that
  `harness/prompt.go`'s existing `const InferMode = "infer"` means Go already holds one.
