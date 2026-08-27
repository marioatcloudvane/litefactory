# Spike — headless-mode-should-ask-via-a-comment

2026-08-27. The owner asked for a plain-English summary of `RESEARCH_BRIEF.md` (1033 lines,
revision 2): what does it actually come out saying? No new research, no code — just the outcome
in easy terms.

## What was done

Read the revised brief and condensed it. Nothing was investigated afresh; every fact here is
already in the brief, with citations there. Where the brief hedges, this note hedges too.

## What was found

**The one-sentence version:** the original feature got much easier and is ready to plan; the two
new asks the owner added are bigger than they look, and one of them rests on facts nobody has
been able to check.

**The theme joining all three asks:** *the GitHub issue becomes the workspace, not just the
trigger.* Today litefactory reads intent from GitHub and writes work product into the repo. All
three asks move something across that line.

### Ask A — "ask instead of guessing"

The original request. **This is now small and unblocked.**

The whole reason today's runs guess is one paragraph of prompt text that tells the agent nobody
is listening, so don't ask. The feature is: replace that paragraph, give the run a way to say
"I stopped to ask", post the question as a comment, and pick the answer up as the next run.

It used to be blocked. Run 0001 of this research concluded the feature was impossible because
the *second* run of any task crashed — and a human's answer is by definition a second run. The
owner has since fixed that (session resume). **That blocker is gone.**

What's left is genuinely modest: one sidecar file, one new "waiting for an answer" state, one
comment marker, one rule for picking up the reply, and two paragraphs of prompt text.

**The interesting wrinkle:** a resumed run is never re-shown the "don't ask" paragraph — by
design. That's good news (it mostly removes a risk the first research pass flagged as work), but
it means any *limit* on asking — how many questions, how many rounds — has to be enforced in
code. You cannot put it in prompt text the resumed run won't read.

### Ask B — "the research brief should also land as a comment"

**This is a renderer, not an architecture change.** Mostly straightforward, with two catches.

*Catch one — it fits, but barely, and it fails badly when it doesn't.* A comment can hold 63,488
usable bytes. Real briefs come in around 70% of that; real *plans* hit 85%. So they fit today,
with less headroom than is comfortable. And when something is too big, the current behaviour is
to post **nothing at all** — which is precisely the wrong outcome for a feature whose entire
point is visibility.

*Catch two — an ambiguity that changes what gets built.* The owner said "**also** land as a
comment" (sounds like: keep the file, add a comment) but also "rather than being a file somewhere
in the code" (sounds like: replace the file). **Those are different features.** Keeping the file
is easy and breaks nothing. Replacing it means this repo's own rules have to change, because
"you may start planning once `RESEARCH_BRIEF.md` exists" stops being checkable.

### Ask C — "the plan should appear as sub-tasks on the issue"

**This is the big one, and it is not ready to plan.**

- litefactory's GitHub client **cannot create or edit issues at all** today. It can read issues
  and post comments; it has never made a single write of any other kind.
- Adding that capability trips a deliberate tripwire: a test fails the moment an eighth client
  method exists, specifically to force a conversation about whether to adopt a real GitHub SDK.
  This ask needs an eighth *and* a ninth. **So it opens with a design decision, not an edit.**
- The GitHub sub-issue API facts in the brief are **second-hand and unverified** — web access was
  switched off for the whole research session. The source they came from openly records that six
  of *its* predecessor's unverified GitHub claims turned out wrong. Someone with a browser needs
  to check these before anyone builds on them.
- A cost nobody had named: **no Go code has ever read an `IMPLEMENTATION_PLAN.md`.** Something has
  to decide where one task ends and the next begins inside ~54 KB of markdown. The cleanest answer
  is to let the agent do it — which is another reason to build Ask A first.

## Decisions worth remembering

- **Run 0001's headline finding was wrong and is retracted.** It said the feature was hard-blocked
  on session resume. Resume shipped the next day. Worth remembering not for the fact but for the
  pattern: **findings in this repo go stale within days**, so the brief now tags every claim as
  verified / owner-reported / unverified / carried-forward.
- **Recommendation in the brief: plan Ask A on its own first.** A, B and C share a theme and
  almost no code. Bundling turns one small clean change into one large risky one.
- **"Code review and alike are untouched"** — taken as ring-fencing the code reviewer and the
  Implement-mode review gates. Nothing is proposed there.
- **Ask B and Ask C break no running code.** The rule that litefactory infers progress from which
  artifact files exist turns out to be *written down but never implemented*. So these asks
  contradict a specification, not working software — a much cheaper place to have the argument,
  and the last moment that unbuilt layer can be cheaply redirected.

## Not done

No research, no verification, no code, no plan. This is a summary of an existing document.

The brief's open questions are **not** resolved here. Two decide the shape of the work and only
the owner can answer them:

1. **Ask B: is the brief mirrored or moved?** (Keep the file and add a comment, or replace it?)
2. **Ask C: are sub-issues inert records, or live tasks?** If the daemon treats them as tasks, one
   plan would auto-spawn ~20 implement runs — a direct violation of *One Run, One Mode*. And if
   they're inert, what should happen when a human ticks one? The brief calls this the unexamined
   heart of Ask C.

Also unresolved and genuinely blocking for Ask C: the GitHub sub-issue API details need checking
against the live docs by someone whose tools aren't switched off.

## Recommendation

**Plan Ask A next, by itself.** It's the original request, it's now small, it's the only one that
doesn't amend the stated architecture, and its absence is what produced the complaint that started
this issue.

Before that plan is written, answer three things: what happens if nobody ever replies; where the
bar for "worth asking" sits; and how many rounds of asking before giving up.

Ask B can follow as a small separate piece once mirrored-vs-moved is settled. **Ask C wants its own
research pass with web tools enabled** — planning it now would be building on unverified facts.

Per *One Run, One Mode*, this spike ends here. Opening Plan mode needs its own comment.
