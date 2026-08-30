# Implementation Plan — headless-mode-should-ask-via-a-comment
# Branch: lf-7-headless-mode-should-ask-via-a-comment
# Date: 2026-08-30 (rev 3.1 — targeted amendment of rev 3; see *What changed in rev 3.1*)
# Issue: marioatcloudvane/litefactory#7
# Sources: `RESEARCH_BRIEF.md` rev 2 · `feature-spec.md` · `ARCHITECT_REVIEW.md` · the owner's
#   scoping comment of 2026-08-27 · **the owner's answers to Q1–Q11, 2026-08-30** ·
#   **a verification pass that landed after rev 3 was written**
# Citations pin to `origin/main` @ **`71d3bea`** *("aiteam: scaffold the tool-enforcement session")*.

---

## ⚠ PROVENANCE WARNING — read before trusting any line number in this plan

**Rev 3 was written against a stale working tree, and its citation base was wrong.**

1. **`origin/main` is `71d3bea`, not `3427686`.** `3427686` is an ancestor. Every rev-3 line reading
   *"citations pin to `origin/main` @ `3427686`"* was wrong and is corrected here.
2. **Rev 3's stale-file list was badly incomplete.** It named seven files as stale and asserted that
   *"everything else matches main"* — in particular that `internal/report`, `internal/store`,
   `internal/github`, `internal/daemon` and `internal/turn` were safe to read from the working tree.
   **They are not.** `git diff --stat origin/main -- internal cmd` shows **52 differing files**,
   including `internal/report/run.go`, `internal/report/report_test.go`, `internal/turn/classify.go`,
   `internal/turn/report.go`, `internal/turn/execute.go`, `internal/turn/result.go`,
   `internal/store/runs.go`, `internal/github/surface_test.go` and `internal/daemon/tick.go`.

**What this means, precisely.** Citations into `internal/report`, `internal/turn`, `internal/store`,
`internal/github` and `internal/daemon` in this plan — **including ones supplied and vouched for by
the owner** — are **disk line numbers, not `origin/main` line numbers**. The underlying **facts were
verified and they stand**; only the line numbers are suspect. Two known shifts, as examples:

| Citation | On disk | On `71d3bea` |
|---|---|---|
| `report/run.go` — the LF-7 *"Plain text at M0"* comment | 62 | **63** |
| `report/report_test.go` — the `/blob/` guard | 240–241 | **251–254** |

**Do not re-pin them now.** Re-pinning every affected citation against `71d3bea` is **the first task
of Implement mode**, before any of these files is edited. Treat every `path:line` in the five
packages above as approximate until that task is done.

**This is the third measured instance of citation rot in this repo**, and it is recorded as one.
Run 0001's headline finding survived one day. `RESEARCH_BRIEF.md`'s own source records six external
GitHub claims of *its* predecessor as wrong. This is the third. The pattern is not that people are
careless — it is that **this worktree drifts from `origin/main` faster than documents citing it get
rewritten**, and no artifact here has a mechanism that notices.

---

**Worktree caveat, corrected.** `internal/harness/{prompt,runner,meta,errors,doc}.go`,
`internal/turn/{prepare,harvest}.go` and `internal/naming/session.go` are stale on disk — **and so
are 52 files under `internal` and `cmd`.** Re-read from `origin/main` @ `71d3bea` before trusting any
line number.

---

> **Rev 3 is the first revision written against answers rather than guesses.** The owner answered
> **all eleven** questions. Every one is recorded below as **ANSWERED**, with what he decided, so the
> history stays readable and nothing looks outstanding that isn't.
>
> **Where his answer overrode a recommendation, it is applied without re-argument.** Q3 is the clear
> case: rev 2 recommended option 2 (Go parses only *presence*) and he chose **option 1 (Go parses the
> literal, hard enforcement)**. That is applied. The recommendation is left in the record for
> archaeology only.
>
> **Four new questions.** Three of them exist because his Q4 answer changed the shape of the
> deliverable rather than answering the question asked; the fourth is one spelling he settled the
> location of but never the wording. **No others have been manufactured** — he answered a great deal
> and rev 3 does not repay that with a fresh interrogation.
>
> **Rev 3.1 adds a fifth — N5 — and only because a fact changed**, not because a new opinion was
> wanted: `.aiteam/` artifact discovery turns out not to exist, and B2's link needs it. N1–N4 are
> untouched and still unanswered.
>
> **`BLOCKED-ON-Qn` is now rare.** Everything the answers unblocked is unblocked. What remains
> blocked is blocked on the four new questions, and it is confined to one story.

## What changed in rev 3.1

*A targeted amendment, not a rewrite. Rev 3's structure, its eleven ANSWERED entries and its four new
questions are untouched. A verification pass returned **after** rev 3 was written and corrected six
things — one of which makes B2 cheaper and four of which make it more expensive.*

| # | Change | Direction |
|---|---|---|
| 1 | **Citation base corrected**: `origin/main` is `71d3bea`, not `3427686`. Provenance warning added at the top; re-pinning is now Implement's first task | correction |
| 2 | **T-B2.2 (add owner/repo to `report.Run`) DELETED.** `report.Run.Ref` is a `naming.Ref`, which already carries `Forge`, `Owner`, `Repo`, `Issue` (`internal/naming/ref.go:20-31`). No new field, no new dependency, no I/O | **cheaper** |
| 3 | **`SHAAfter`, not `CommitSHA`.** `CommitSHA` is empty on the supported path where claude commits for itself. New task + new AC | more expensive |
| 4 | **The goldens are the real blocker, not the guard test.** `TestSuccessReport` pins the whole body via `checkGolden`; twelve golden files must be regenerated. **A task rev 3 did not have** | more expensive |
| 5 | **Link rendering gated on `run.Pushed`** — promoted from a parenthetical fallback to a requirement, with its own AC | more expensive |
| 6 | **`.aiteam/` artifact discovery does not exist and B2 needs it.** The genuinely expensive item; rev 3 sized B2 without it. **B2 is re-sized. See N5** | more expensive |
| 7 | Run-ordering confirmed a second time (`harvest.go` commits then pushes; the daemon calls `turn.Execute` before `reporter.ReportTurn`). Rev 3's instruction not to re-plan it **stands unchanged** | confirmation |

**Net: B2 grew.** It was *"one field, one builder, one retired test"* — it is not. See B2's re-sizing
note.

## What changed in rev 3

| # | Change | Source |
|---|---|---|
| 1 | **All eleven questions marked ANSWERED**; the tasks they blocked are unblocked and rewritten to the chosen option | owner |
| 2 | **The 8 KiB brief target is deleted** (Q5 — he declined a size limit explicitly). B1 is re-aimed from *shorter* to **business-readable**, with a new audience split | owner, Q5 |
| 3 | **LF-7 (the clickable link) is pulled INTO scope** and out of `Out of Scope`. ~~Verified cheap~~ — **rev 3.1: the "cheap" half of this claim is withdrawn.** `report.Run` does carry `Branch`, `CommitSHA` and `Pushed` at render time (`internal/report/run.go:59-72`, disk line numbers), but `CommitSHA` is the wrong field and artifact discovery does not exist. **See B2's re-sizing note** | owner Q1 + verification |
| 4 | **The run-reordering that LF-7 was thought to need is already done** — `Pushed` plus the doc at `run.go:95-97` confirms push precedes the comment. `initialplan.md:234` anticipated a change that has since landed. **Do not plan it** | verification |
| 5 | **AC-B2.7 inverted.** It asserted the body contains no `/blob/`; the guard test `internal/report/report_test.go:240-241` *(disk; `251-254` on `71d3bea`)* must now be **retired or narrowed**, and `run.go:61-62` *(disk; the comment is at `:63`)*'s *"Plain text at M0 … (LF-7)"* comment removed. **Rev 3.1: this is the small half — the goldens are the real blocker** | verification |
| 6 | **New Story C1 — one GitHub sub-issue per user story.** Q4's answer re-scoped Plan mode's own output. **This reverses his earlier deferral of sub-issues** — flagged as **N1** so he can correct it | owner, Q4 |
| 7 | **Story A2 deleted.** Q11 = A1 only. Moved to `Out of Scope` with the condition that would bring it back — not left vaguely deferred | owner, Q11 |
| 8 | **Q8's cap moves out of Go entirely** — agent-level judgement, no Go cap, no timeout. T-A1.11 changes from a Go task to an agent-definition task | owner, Q8 |
| 9 | **Every asking comment gains a standing "say *move to the next phase*" line** — and that line is exactly the human comment *One Run, One Mode* requires to open the next mode | owner, Q8 |
| 10 | Q6 splits the modes: **Research and Plan may ask; Implement never does.** New architecture note on what that costs given Q3 = option 1 | owner, Q6 |

---

## Answered questions — the record

*All eleven. Nothing here is outstanding. New questions are in the next section.*

### Q1 — Is the research brief MIRRORED or MOVED? — **ANSWERED: MIRRORED**

The full brief **stays a repo file** under `.aiteam/`. The comment carries an easy-terms summary
**plus a clickable link** to the brief.

**Consequences applied:** `CLAUDE.md`'s Plan gate keeps working untouched — T-B2.8 (amend the gate
and `initialplan.md:243`) is **deleted**, it was the `moved` branch. AC-B2.9 deleted; AC-B2.8 becomes
unconditional. And `research-orchestrator.md` must now produce **two** things per run, not one: the
file and the summary.

**This is also what makes the link load-bearing.** A summary that cannot reach its detail is a worse
artifact than the brief it replaced, so B2's link is not a nicety — see Story B2.

### Q2 — Where does the tag live? — **ANSWERED: the triggering comment body**

Settled. Per-run, zero new client surface, `KindFreeText` already carries the whole body. **The
spelling was not settled** — see **N4**.

### Q3 — Who enforces the tag? — **ANSWERED: option 1, Go parses the literal**

Line-anchored, **hard enforcement**: an untagged run *cannot* ask, whatever the agent decides.

**This is not rev 2's recommendation** (which was option 2, presence-only). It is applied as
written and is not re-argued. What it costs, carried into Story A1: a tag parser, a strip step,
`MayAsk`/`PreviousMayAsk` plumbing, the tag-only-comment edge case (F-16), and the `KindSpike` strip
path (F-15). What it buys is the thing he asked for — a machine guarantee rather than a convention.

### Q4 — What did *"-> separate task for that!"* mean? — **ANSWERED, by re-scoping**

He did not pick (a) or (b). He asked for something else: **one GitHub sub-issue per user story,
instead of the stories living inside one flat `IMPLEMENTATION_PLAN.md`.** That is a change to Plan
mode's own output. It is **Story C1**, and it carries three raised questions — **N1**, **N2**, **N3**.

**The question actually asked is now unanswered**, and Q2, Q3 and Q6 all presume the answer. Rev 3
therefore proceeds on the reading that **the tag ships in this plan**, and records that as a
**stated assumption, not a question** — see `Stated assumptions` below.

### Q5 — How brief is "brief"? — **ANSWERED: no imposed size limit**

He declined one explicitly. **The 8,192-byte target and the 20,000-byte ceiling are deleted from
this plan.**

**What replaces them is an audience split, and it is the more useful instruction:**

- **The comment is for business analysts and business users.** Plain language. **Honest — it does not
  swallow technical complexity to sound tidy.** It **names the problems found**. It **always links
  the detailed artifact**.
- **The repo artifact is for technical readers.** It keeps its detail, its citations and its
  provenance tagging. Nothing is cut from it. *(This also retires rev 2's warning that cutting
  provenance tagging was a real loss — under Q1 = mirrored it was never going to be cut.)*

**One truncation warning, stated once and not as a budget.** A physical ceiling remains and it is
not ours to remove: the run's final message is cut at `maxResultBytes = 32 << 10`
(`report/run.go:24`), **from the end** (`run.go:528`). An over-long summary therefore loses its
**tail**, and the tail is where the link and the open questions sit. This is a warning to whoever
writes the summary template, not a number to enforce.

**AC-B1.2 is rewritten as qualitative/observational**, since there is no number left to measure.

### Q6 — Which modes may ask? — **ANSWERED: Research and Plan may ask; Implement stays headless**

Implement never asks.

**The consequence, stated because it is the one place Q3 and Q6 collide.** With **two** modes tagged
and **Go parsing a literal** (Q3 option 1), a *mode-named* tag would force Go to hold a table
(`research-mode:`, `plan-mode:`, …) kept in sync with a mode vocabulary that lives in the **target
repo's `CLAUDE.md`**, not in litefactory. **A single neutral literal does not** — Go matches one
opaque string and the mode gate is a separate, already-existing comparison. This is the whole reason
**N4** is worth asking.

### Q7 — How does the brief physically reach the comment? — **ANSWERED: option 1**

Full brief to the repo, summary as the comment. Consistent with Q1. Delivery is the run's **final
message**, which `secResult` already posts — **zero new Go for delivery**. One comment per run. OD-10
dissolves, as rev 2 predicted. Routes (ii) sidecar and (iii) Go-reads-the-file are both dead; tasks
T-B2.3 and T-B2.4 are **deleted**.

### Q8 — May a resumed run ask a follow-up, and how many times? — **ANSWERED: agent-level, not Go**

**No Go-side cap. No timeout.** The agent judges whether its questions are answered and **honours a
direct instruction to move on**.

**And every asking comment must carry a standing line inviting the owner to say *"move to the next
phase."*** This lands in the **agent definitions and prompt text**, not in `internal/`.

**Worth recording: this is consistent with *One Run, One Mode*, not an exception to it.** "Move to
the next phase" is precisely the human comment that `CLAUDE.md` requires in order to open the next
mode. The standing line is not litefactory granting itself permission to continue — it is
litefactory telling the owner what sentence to write if he wants it to.

T-A1.11 is rewritten from a Go task to a prompt/agent-definition task, and AC-A1.13 with it.

### Q9 — What may the resume header honestly claim? — **ANSWERED: (b), hedged framing**

*"If your previous message ended in a question, treat the new comment as the answer."* Asserts
nothing Go does not know — the condition is evaluated by the agent, from its own transcript.
T-A1.10 is unblocked and its text is settled.

### Q10 — Quote-reply silently drops the owner's answer — **ANSWERED: proceed with rev 2's recommendations**

Both halves of the recommendation: **line-anchored marker matching**, *and* instructing the owner to
reply plainly rather than by quote-reply. **Story S0 stays first.** T-S0.1 is unblocked and its
decision is recorded in S0's notes. The deliberate-impersonation half (F-19, Q10(b)) stays in
`Out of Scope`.

### Q11 — Do you want the parked state at all? — **ANSWERED: A1 only**

**Story A2 and the `needs_input` parked state are deleted from this plan**, not deferred. They are in
`Out of Scope` with one line on what would bring them back.

---

## Stated assumptions — not questions

*Things rev 3 proceeds on without asking, and says so.*

1. **The tag ships in this plan.** Q4's original question — *does the tag mechanism ship here, or in
   its own run?* — went unanswered when the owner re-scoped. Q2 (where the tag lives), Q3 (who
   enforces it) and Q6 (which modes it covers) all presume it does, and he answered all three in
   detail. **Rev 3 reads that as yes.** If it is a no, Story A1 must not ship at all — A1 without the
   tag is *ungated* asking, which is R-3, the largest product risk in the brief.
2. **The blob link is SHA-pinned, not branch-relative.** A branch-relative URL rots the moment the
   branch moves. **Corrected in rev 3.1 on two points:**
   - **The SHA is `SHAAfter`, not `CommitSHA`.** See B2.
   - **Branch-relative is not a fallback and buys nothing.** The `/blob/` guard is a bare unanchored
     `strings.Contains`, so a branch-relative URL trips it exactly as a permalink does. The real
     fallback when the commit is not pushed is **no link at all** — see AC-B2.3.

---

## New questions

*Five. Three come from Q4's re-scope; one is a spelling; **N5 is new in rev 3.1** and is the one that
re-sizes B2. **N1–N4 are unchanged — no human has replied to any of them.***

### N1 — Sub-issues are back in scope. **This reverses your earlier deferral — is that intended?**

On 2026-08-27 you wrote: *"sub issues we defer to a later run; i'll create a own story later on."*
Rev 2 recorded that in `Out of Scope` and built nothing for it. **Your Q4 answer asks for exactly
that thing**, now, as Plan mode's output: one GitHub sub-issue per user story instead of a flat
`IMPLEMENTATION_PLAN.md`.

**Flagging it plainly rather than quietly obeying the newer instruction**, because the reversal may
be accidental — Q4 asked what a phrase *meant*, and the answer changed the deliverable instead.

**If intended, Story C1 is real and the costs in N2 and N3 apply.** If not, say so and C1 is deleted
in one line.

**The costs C1 carries, so the reversal is priced rather than assumed:**

- **No issue-creation or sub-issue call exists in `internal/github`.** Every verb is GET or POST
  across five call sites; there is **no PATCH, PUT or DELETE anywhere**. C1 needs an **eighth and a
  ninth** client method.
- That trips **`internal/github/surface_test.go:56`** — a test that exists specifically to force the
  SDK decision. See **N2**.
- **The sub-issue API facts in `RESEARCH_BRIEF.md` are tagged `[W]` — second-hand and unverified**,
  because web tools were disabled for the entire research session. Every sub-issue task in C1
  therefore carries an acceptance criterion requiring the contract be **confirmed against live GitHub
  docs before any code is written**.
- **No backoff exists** for bulk creation. A plan with eight stories is eight POSTs in a loop against
  an API with rate limits, and nothing in `internal/github` retries.

### N2 — SDK or hand-rolled? The eighth method forces the decision you deferred

`TestClientMethodSetIsTheSevenEndpoints` (`internal/github/surface_test.go:56`) fails the moment an
eighth client method exists. **That is deliberate** — the test is a tripwire whose purpose is to make
someone stop and decide whether to keep hand-rolling HTTP or adopt an SDK, rather than growing a
ninth, tenth and eleventh method by accretion.

C1 needs **two** new methods. **The decision is yours, and rev 3 will not take it silently** — taking
it silently is the exact defect rev 2 was written to correct.

- **(a) Hand-roll two more methods** and widen the test to nine. Cheapest now; the tripwire fires
  again at the tenth.
- **(b) Adopt `go-github`** (or similar) and delete the tripwire. A real dependency decision, a
  migration of five existing call sites, and out of proportion to this feature alone — but it is the
  decision the test was planted to provoke.
- **(c) Neither — C1 does not ship.** See N1.

**No recommendation offered.** This is a dependency choice for the project, not for this issue.

### N3 — If the plan becomes sub-issues, is `IMPLEMENTATION_PLAN.md` MIRRORED or REPLACED?

Exactly the Q1 question, one artifact along. **Your Q1 answer suggests you like mirroring** — file
stays, comment summarises, link connects them — **so this asks rather than assumes.**

- **Mirrored** — `IMPLEMENTATION_PLAN.md` still written; sub-issues carry a business-readable summary
  of each story and link back. **`CLAUDE.md`'s Implement gate (*"Implement requires
  `$session/IMPLEMENTATION_PLAN.md`"*) keeps working untouched.** Symmetric with Q1.
- **Replaced** — sub-issues only, no file. The Implement gate can then never pass, so `CLAUDE.md`
  must change and `initialplan.md:243` (*"State is inferred from artifacts, not self-reported"*) must
  be amended in the same change. Also: a sub-issue is editable by anyone with write access, so the
  plan of record becomes mutable and unversioned.

**Recommendation: mirrored**, for symmetry with Q1 and because it is the only option that leaves the
Implement gate intact.

### N4 — How is the tag spelled?

You settled **where** it lives (Q2: the triggering comment body). You never settled the **wording**.
It now matters more than it did, because Q3 = option 1 means **Go matches the literal**, and Q6 means
**two** modes are tagged.

- **`research-mode: ask` verbatim** — your original words. Under Q6 = Research + Plan this forces a
  second literal (`plan-mode: ask`) and a table in Go kept in sync with a vocabulary that lives in
  the **target repo's `CLAUDE.md`**. Go then holds mode names it cannot validate.
- **`lf: ask`** — one neutral literal, forever, covering both modes with no table and no vocabulary
  coupling. The mode gate stays where Go already knows the mode.

**Recommendation: `lf: ask`.** **This renames your tag, so it needs your say-so.** If you want
`research-mode: ask` kept, say so and A1 takes a two-literal table instead — it is a bigger matcher
and a maintenance coupling, not a blocker.

### N5 — How does the comment find `RESEARCH_BRIEF.md`? *(new in rev 3.1 — this is what re-sized B2)*

**There is no `.aiteam/` artifact discovery anywhere in the code, and B2's link needs one.** The only
path source on `report.Run` is `Files []git.FileChange` — the git-status list. Artifact discovery is
explicitly out of scope at M0. **Rev 3 sized B2 as if the path were already available. It is not.**

This is the genuinely expensive item in B2, and the two ways out differ in cost *and* in whether they
honour a rule you set yourself at Q5.

- **(a) Build real artifact discovery.** Locate `$session/RESEARCH_BRIEF.md` deterministically and
  link it. **Net-new work** — the expensive option, and the reason B2's size moved. **It always
  produces a link.**
- **(b) Match the brief out of `Files`.** The brief will usually appear as a changed path in the same
  run that wrote it, so scanning `Files` for a `RESEARCH_BRIEF.md` suffix is cheap and needs no new
  concept. **But it silently produces no link on a run that posts a summary without rewriting the
  brief** — a resumed research run answering a question, or any Plan-mode summary.

**Recommendation: (a).** Not on cost — (b) is plainly cheaper — but because **(b) breaks a rule you
set.** B1's fourth hard rule is that the summary *"always links the detailed artifact"*, and that
rule is the entire reason the plain-language register is affordable: nothing is lost, it is one click
away. A link that is present most of the time turns *"read the detail here"* into *"read the detail
here, sometimes"*, and the runs where it goes missing are the resumed ones — precisely the runs where
the reader has least context.

**Put to you rather than decided, because it is a cost-versus-your-own-rule trade and the cost is
real.** If you would rather have (b) now and discovery later, say so and B2 shrinks back to roughly
its rev-3 size; AC-B2.2 then relaxes to *"when the brief appears in `Files`"*, and the gap is recorded
in `Out of Scope` rather than left to be discovered in production.

---

## User Story S0 — The owner's reply is never silently dropped

> As the owner, I want my reply to a litefactory comment to actually start a run, so that answering
> a question is not a coin flip on which GitHub button I pressed.

Stops a quote-reply from being misclassified as litefactory's own output and discarded. **Ships
first**, because every other story here depends on the owner being able to reply to a comment that
carries a marker — and today, on the most natural reply path, he cannot.

### Architecture Notes

- **Route settled (Q10): line-anchor the marker match, AND instruct the owner to reply plainly.** Both
  halves, not one. T-S0.1 is unblocked and T-S0.6 is no longer conditional.
- **The defect in one line:** `HasMarker` = `strings.Contains(body, "<!-- lf:")` anywhere
  (`turn/classify.go:38`), and `selfReason` is tested **before** the owner check
  (`classify.go:207-214`). A quoted marker therefore beats owner authorship.
- **The ordering is deliberate and must be preserved.** `classify.go:193-196` documents why: the bot
  account may be the owner's own account by misconfiguration, and author-first classification would
  make litefactory answer itself. **Do not invert precedence in this story.**
- **Why line-anchoring fixes it:** GitHub prefixes quoted lines with `> `, so a quoted marker appears
  as `> <!-- lf:` and a `^<!-- lf:` match no longer fires, while litefactory's own comments — marker
  at the start of a line — still match.
- **Characterize before changing.** The classification ordering is security-relevant and
  under-pinned by tests. Pin it first, then change it.
- **Silence is the real bug.** Some bodies will still legitimately classify `self`. Every drop must
  be logged with its `SelfReasonMarker` so the next occurrence is diagnosable instead of invisible.
- **Now doubly load-bearing.** Under Q1 = mirrored + Q7 = option 1, every research run posts a
  marker-bearing, highly quotable summary. Under Q3 = option 1 the owner must reply to ask-comments
  for the feature to work at all. S0 is the precondition for both.
- Platform: no new package, no dependency, no goroutine, no query, no HTTP surface, no migration. A
  matcher change and logging inside `internal/turn`.
- Out: the deliberate-impersonation half (F-19) — `Out of Scope`.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-S0.1 | Characterization tests pinning today's `Classify` ordering: owner+marker → self; third-party+marker → self; litefactory comment → self. Written **before** any change | Test | — | S |
| T-S0.2 | Change the marker match to line-anchored (`^<!-- lf:`) | Go | T-S0.1 | M |
| T-S0.3 | Log every self-classified drop with its `SelfReasonMarker` at a level the owner can find | Go | T-S0.1 | S |
| T-S0.4 | Tests: an owner quote-reply body (leading `> ` before the marker) classifies as `instruction` and starts a run; litefactory's own comment still classifies `self` | Test | T-S0.2 | M |
| T-S0.5 | Litefactory's question and report comments tell the owner to reply plainly rather than by quote *(per Q10, both halves ship)* | Docs | — | S |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-S0.1 | A comment authored by the owner whose body contains `<!-- lf:` **only inside quoted lines** classifies as an instruction and starts a run. |
| AC-S0.2 | A comment genuinely authored by litefactory still classifies `self` and is still dropped. Regression test. |
| AC-S0.3 | Author precedence in `Classify` is **unchanged** — `selfReason` is still tested before the owner check. |
| AC-S0.4 | Every self-classified drop emits a log line naming `SelfReasonMarker`. No comment is discarded without a trace. |
| AC-S0.5 | The characterization tests from T-S0.1 exist and were committed before the behaviour change. |
| AC-S0.6 | Litefactory's outbound question/report text tells the owner to reply plainly, not by quote-reply. |
| AC-S0.7 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## User Story B1 — The research brief gets a business-readable summary

> As a business reader, I want the issue comment to tell me in plain language what was found and what
> the problems are, so that I do not have to read a technical brief to know where things stand.

**Re-aimed this revision.** Rev 2's B1 was *"briefs get much shorter"*, built around an 8 KiB budget.
Q5 deleted the budget. B1 is now about **audience**, not length: `research-orchestrator.md` learns to
produce **two** outputs — the technical artifact it already writes, and a business-readable summary
for the comment. Zero Go. The fastest thing in this plan to ship.

### Architecture Notes

- **The audience split is the whole story (Q5).** The comment is for **business analysts and business
  users**; the repo artifact is for **technical readers**. One run, two registers.
- **The summary's four hard rules**, all from the owner and all testable only by reading it:
  1. **Plain language.** No `path:line` citations, no provenance tags, no Go identifiers.
  2. **Honest — it does not swallow technical complexity** to sound tidy. If something is hard, it
     says it is hard and why, in business terms.
  3. **It names the problems found.** A summary that reports only progress is a failing summary.
  4. **It always links the detailed artifact.** This is what makes the plain-language register
     affordable — nothing is lost, it is one click away. **Delivered by Story B2.**
- **No size limit. Do not reintroduce one.** He declined one explicitly.
- **One truncation warning, not a budget.** The final message is cut at `maxResultBytes = 32 << 10`
  (`report/run.go:24`), **from the end** (`run.go:528`). A runaway summary therefore loses its
  **tail** — and the tail is where the link and the open questions live. **State this in the template
  as a warning about ordering**: put the link and the problems early, not last. It is a physical
  ceiling that is not ours to remove, not a target to hit.
- **The technical artifact keeps everything.** Provenance tagging ([V]/[O]/[W]/[R0]), citations,
  section depth — all stay. Under Q1 = mirrored nothing is being traded away.
- Risk: an instruction with no enforcement is a suggestion. The only available lever is a template
  explicit enough that a deviation is visible when read.
- Platform: **not applicable** — markdown only, no Go, no build, no tests to run.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-B1.1 | Add a **business summary** section to `research-orchestrator.md`'s output contract: audience, register, and the four hard rules above | Docs | — | S |
| T-B1.2 | State the ordering rule and the reason: the message is truncated from the end at 32 KiB, so link and problems go early. **A warning, not a budget** | Docs | T-B1.1 | S |
| T-B1.3 | State that the technical brief keeps its full detail and provenance tagging — the summary is an addition, not a replacement *(Q1 = mirrored)* | Docs | T-B1.1 | S |
| T-B1.4 | Require the summary to name the problems found, explicitly, as its own section | Docs | T-B1.1 | S |
| T-B1.5 | Cap `codebase-tour.md`'s return format for the same unbounded-output problem — this one is machine-to-machine and a cap still applies | Docs | — | S |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-B1.1 | `research-orchestrator.md` specifies a business-readable summary distinct from `RESEARCH_BRIEF.md`, and names its audience. |
| AC-B1.2 | **(manual / observational — not gateable.)** The next summary produced under the changed template is readable by a non-technical reader, names the problems found, and does not hide complexity. There is no numeric check and no automated test. **`implement-orchestrator` must not block on this.** |
| AC-B1.3 | The template requires a link to the detailed artifact, positioned early rather than at the end. |
| AC-B1.4 | The template requires an explicit problems/risks section in the summary. |
| AC-B1.5 | The template imposes **no byte or line limit** on either artifact, and says why the 32 KiB truncation is a warning rather than a budget. |
| AC-B1.6 | `codebase-tour.md` states a return-size cap. |
| AC-B1.7 | No Go file is modified by this story. |

---

## User Story B2 — The summary lands as a comment, with a clickable link to the full brief

> As the owner, I want the summary on the issue with a link straight to the full brief, so that I read
> the short version where I already read everything else and reach the detail in one click.

Delivers B1's summary to the issue thread and makes the link to the repo artifact real. Under
Q7 = option 1 the delivery half is free; the link half is the only Go in this story.

> ### ⚠ Re-sized in rev 3.1 — **this story grew, and rev 3 said the opposite**
>
> Rev 3 called the link *"three small things, not a project"* and sized it **one field, one builder,
> one retired test**. A verification pass that landed after rev 3 was written found that **three of
> those four claims were wrong or incomplete**:
>
> | Rev 3 said | Actually |
> |---|---|
> | Add repo owner/name to `report.Run` — *"the one genuinely missing input"* | **Not needed.** `report.Run.Ref` is a `naming.Ref` and already carries `Forge`, `Owner`, `Repo`, `Issue` (`internal/naming/ref.go:20-31`) — **cheaper** |
> | Use `CommitSHA` | **Wrong field.** `CommitSHA` is empty exactly on the supported path where claude commits for itself. Must be `SHAAfter`, **which is not plumbed** |
> | Retire one guard test | **The goldens are the real blocker.** Twelve golden files pin whole bodies. A task in its own right, which rev 3 did not have |
> | *(not mentioned)* | **`.aiteam/` artifact discovery does not exist.** The link has nothing to point at. **See N5** |
>
> **Net: from S/M to L**, and one of the four is a decision, not a task. **B2 is no longer "fully
> unblocked"** — the shipping order is corrected accordingly.
>
> *One thing rev 3 got right and rev 3.1 confirms: **push already precedes the comment.** `harvest.go`
> commits then pushes, and the daemon calls `turn.Execute` before `reporter.ReportTurn`. **Do not
> re-plan the ordering.** AC-B2.11 stands.*

### Architecture Notes

- **Delivery is free (Q7 = option 1).** `secResult` already posts the run's final message. The work
  is instructing `research-orchestrator.md` to put the summary there. **No new Go for delivery.** One
  comment per run. OD-10 is retired.
- **The link — LF-7 — is IN scope, and it is five things, not three.** Corrected in rev 3.1:
  - **Owner/repo is NOT missing — drop that task.** `report.Run.Ref` is a `naming.Ref`, which already
    carries `Forge`, `Owner`, `Repo` and `Issue` (`internal/naming/ref.go:20-31`). The URL is built
    from a field the package already holds. **No new field, no new dependency, no I/O** — which
    matters, because `internal/report`'s own package doc says it holds **no database handle and does
    no git or forge work**. Importing `internal/github` here would break that; building from `Ref`
    does not. **Keep the package choice: build it in `internal/report`.**
  - **The SHA must be `SHAAfter`, and `SHAAfter` is not plumbed.** `turn.Result.SHAAfter` is not
    passed into `report.Run` by `runInput` (`internal/turn/report.go`, the struct literal).
    **`CommitSHA` alone is wrong**: on the supported path where **claude commits for itself** via its
    own Bash tool, `git.CommitResult.SHA` is empty while `Head` has moved — so `CommitSHA` is empty
    *exactly there* and a SHA-pinned permalink renders blank. **The fix is a `report.Run` field plus
    one line in the `runInput` literal**, and an AC covering that path specifically.
  - **The goldens are the real blocker, not the guard test.** `TestSuccessReport` ends with
    `checkGolden(t, "run_succeeded", body)`, which pins the **entire body** — any added line fails
    it. The same holds for the other goldens under `internal/report/testdata/`: `run_failed`,
    `run_push_failed`, `run_sparse_result`, `run_truncated`, `run_never_started`,
    `run_no_result_recorded`, plus five `outcome_*`. **Regenerating the affected goldens is a task in
    its own right** and rev 3 did not have one.
  - **The `/blob/` guard is a bare unanchored `strings.Contains`**, so a **branch-relative URL trips
    it too**. Branch-relative buys nothing here and is not a fallback. Retire or narrow the guard
    either way.
  - **The blocker comment to remove** is `report/run.go`'s *"Plain text at M0: no links, no
    SHA-pinned permalinks (LF-7)"* — **disk `:62`, `71d3bea` `:63`.**
  - **And there is nothing to point at yet: `.aiteam/` artifact discovery does not exist.** The only
    path source on `report.Run` is `Files []git.FileChange`, the git-status list; artifact discovery
    is explicitly out of scope at M0. **This is the expensive item and it is N5's decision.** Tasks
    below branch on the answer.
- **The link must be gated on `run.Pushed`.** A link to an unpushed commit 404s, and `Pushed` is
  genuinely false on real paths — push is skipped when the branch did not move, and skipped entirely
  for an abandoned run. **Gate the link the same way `metadataList` already gates its "(pushed)"
  suffix.** AC-B2.3.
- **Push already precedes the comment — confirmed twice.** `harvest.go` commits then pushes, and the
  daemon calls `turn.Execute` before `reporter.ReportTurn`. The run-ordering change
  `initialplan.md:234` anticipated **is already done. Do not plan it, do not re-order anything.**
- **Route the body through `internal/report` either way.** `redact.Body` has no exported constructor,
  so "post an unredacted body" is not an expressible program. Credential redaction, the residual
  assertion and the panic guard come free.
- **Redaction is not sanitisation.** `redact` covers credentials only. The summary is agent-authored
  text going straight into a comment and can carry markdown that breaks the report's `<details>`
  fence or callout structure. It **must** route through `report/run.go`'s existing `sanitize` and
  `fenceFor`.
- **Decision taken — never fail silent.** Today an oversize body returns a `LimitError`, no comment is
  posted, the run goes unreported and the watermark does not move. For a visibility feature, "too big
  to show you, so you see nothing" is the wrong failure. Truncate with an explicit marker naming what
  was cut. *(This matters more now than in rev 2: with Q5's size limit gone, an oversize summary is no
  longer prevented upstream.)*
- **Verify before building T-B2.4.** The silent-failure path may already be unreachable for
  `RenderRun`: `runBody` trims to budget, `run.go:528` truncates with an explicit marker, and
  `redact.Redactor.Apply`'s hard-truncate is the backstop. **If so, T-B2.4 collapses to a regression
  test.**
- **Decision taken — no multi-comment splitting.** Nothing in the tree splits one document across
  comments; `post` is the single forge call in `internal/turn` and is not in a loop.
- **Security: a summary posted as a comment re-enters the prompt as history wearing litefactory's own
  label.** Quoted third-party text laundered from `[old_third_party]` into `[litefactory]` is trusted
  material it should not be. Keep quoted third-party text clearly attributed and fenced, prefer
  summarising over quoting. **This risk drops under B1** — a plain-language summary quotes far less
  than a technical brief did.
- **Depends on S0.** A posted summary is a marker-bearing, highly quotable document. Until S0 ships, a
  quote-reply to it is silently discarded.
- **`RESEARCH_BRIEF.md` still gets written (Q1 = mirrored).** `CLAUDE.md`'s Plan gate is untouched.
  Rev 2's T-B2.8 — amend the gate and `initialplan.md:243` — is **deleted**; it was the `moved`
  branch.
- Platform: **no new `internal/github` method.** The seven-endpoint surface test stays closed by this
  story. *(Only Story C1 breaks it — see N2.)*
- Out: nothing here touches code review.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-B2.0 | **Re-pin every citation in this story against `origin/main` @ `71d3bea`** before editing any file in `internal/report` or `internal/turn`. See the provenance warning | Chore | — | S |
| T-B2.1 | Instruct `research-orchestrator.md` to emit the business summary as its final message, and to keep writing `RESEARCH_BRIEF.md` to `$session/` | Docs | — | S |
| ~~T-B2.2~~ | ~~Add repo owner/name to `report.Run`~~ — **DELETED in rev 3.1.** `report.Run.Ref` is a `naming.Ref` already carrying `Forge`/`Owner`/`Repo`/`Issue` (`internal/naming/ref.go:20-31`) | — | — | **0** |
| T-B2.2a | **Plumb `SHAAfter`**: add the field to `report.Run` and pass `turn.Result.SHAAfter` in `runInput`'s struct literal (`internal/turn/report.go`) | Go | T-B2.0 | S |
| T-B2.3 | Blob-URL builder **inside `internal/report`**: permalink from `Ref` (owner/repo) + **`SHAAfter`** + path. No `internal/github` import, no I/O | Go | T-B2.2a | M |
| T-B2.3a | **Gate link rendering on `run.Pushed`** — emit no link when false, mirroring how `metadataList` gates its "(pushed)" suffix | Go | T-B2.3 | S |
| T-B2.3b | **Artifact discovery for `$session/RESEARCH_BRIEF.md`** — locate the brief's path so the builder has something to point at. **(a)** real discovery, net-new; **(b)** match a `RESEARCH_BRIEF.md` suffix out of `Files` | Go | T-B2.3 · **BLOCKED-ON-N5** | **L if (a) · S if (b)** |
| T-B2.4 | **First verify** whether `RenderRun`'s existing truncation already makes silent oversize failure unreachable. If it does, this collapses to a regression test; if not, add an explicit truncation marker | Go | — | S–M |
| T-B2.5 | Route the summary body through `report`'s existing `sanitize` and `fenceFor` | Go | — | S |
| T-B2.6 | **Retire or narrow the `/blob/`-and-commit-URL guard** (`internal/report/report_test.go` — disk `240-241`, `71d3bea` `251-254`), whose stated premise (*"LF-7 is out of scope"*) no longer holds. Note it is an unanchored `strings.Contains`, so it trips on **any** URL form | Test | T-B2.3 | S |
| T-B2.6a | **Regenerate the affected goldens.** `TestSuccessReport` pins the whole body via `checkGolden(t, "run_succeeded", body)`; the same applies to `run_failed`, `run_push_failed`, `run_sparse_result`, `run_truncated`, `run_never_started`, `run_no_result_recorded` and the five `outcome_*` files under `internal/report/testdata/`. **Regenerate deliberately and eyeball every diff** — a blind regeneration hides exactly the regressions goldens exist to catch | Test | T-B2.3, T-B2.3a | **M–L** |
| T-B2.7 | Remove the blocker comment in `report/run.go` (*"Plain text at M0: no links, no SHA-pinned permalinks (LF-7)"* — disk `:62`, `71d3bea` `:63`) and replace it with what is now true | Docs | T-B2.3 | S |
| T-B2.8 | Tests: a rendered comment contains a `SHAAfter`-pinned `/blob/` URL for the brief; a run with `Pushed == false` contains **no** link and no dead URL | Test | T-B2.3a | M |
| T-B2.8a | Test: **the claude-committed-for-itself path** — `git.CommitResult.SHA` empty, `Head` moved — still renders a working permalink. This is the case `CommitSHA` gets wrong | Test | T-B2.2a, T-B2.3 | M |
| T-B2.9 | Tests: an oversize summary still produces a posted comment, explicitly marked as truncated | Test | T-B2.4 | M |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-B2.1 | **(manual / observational — not gateable.)** After a Research run, the summary is readable on the issue and its link opens the full brief. **`implement-orchestrator` must not block on this.** |
| AC-B2.2 | The comment contains a **`SHAAfter`-pinned** `/blob/<sha>/…` URL to `$session/RESEARCH_BRIEF.md`. *(If N5 = (b), this relaxes to: **when the brief appears in `Files`**.)* |
| AC-B2.2a | **On the claude-committed-for-itself path** — `git.CommitResult.SHA` empty, `Head` moved — the permalink still renders a real SHA. **A blank or missing SHA there is a failing test.** This is the case `CommitSHA` gets wrong. |
| AC-B2.2b | The URL is built inside `internal/report` from `report.Run.Ref`. **No `internal/github` import is added to `internal/report`, and no owner/repo field is added to `report.Run`** — the package still holds no DB handle and does no git or forge work. |
| AC-B2.3 | When `run.Pushed == false`, the comment contains **no** link at all — never a URL to an unpushed commit. Gated the same way `metadataList` gates its "(pushed)" suffix. Covers both real paths: branch did not move, and abandoned run. |
| AC-B2.4 | The `/blob/`-and-commit-URL guard (`internal/report/report_test.go` — disk `240-241`, `71d3bea` `251-254`) is retired or narrowed, and the `report/run.go` LF-7 blocker comment (disk `:62`, `71d3bea` `:63`) is gone. Neither may be left asserting a premise this story removes. |
| AC-B2.4a | **Every affected golden under `internal/report/testdata/` is regenerated and its diff reviewed** — `run_succeeded`, `run_failed`, `run_push_failed`, `run_sparse_result`, `run_truncated`, `run_never_started`, `run_no_result_recorded`, and the five `outcome_*`. A golden regenerated without its diff being read is a failing outcome, not a passing one. |
| AC-B2.4b | **Every `path:line` citation this story relied on was re-pinned against `71d3bea` before the file was edited** (T-B2.0). |
| AC-B2.5 | An oversize summary still produces a comment, explicitly marked as truncated. **Silence is a failing test.** |
| AC-B2.6 | The comment carries an `<!-- lf:` marker and is filtered out of the bot's own input. |
| AC-B2.7 | The comment body passes the redaction residual assertion; no credential reaches the forge. |
| AC-B2.8 | Agent-authored markdown cannot break the report's fence or callout structure — the body goes through `sanitize`/`fenceFor`. |
| AC-B2.9 | `$session/RESEARCH_BRIEF.md` still exists after a Research run, and `CLAUDE.md`'s Plan gate is unmodified. |
| AC-B2.10 | `internal/github` still exposes exactly seven network methods; `TestClientMethodSetIsTheSevenEndpoints` is unchanged and green **by this story**. |
| AC-B2.11 | **No run-ordering change is made.** Push already precedes the comment; a diff that re-orders them is a failing outcome. |
| AC-B2.12 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## User Story A1 — Opt-in asking, Go-enforced, Research and Plan only

> As the owner, I want a run I have tagged to stop and ask me in a comment instead of guessing, so
> that a wrong assumption does not propagate into a brief, then a plan, then code.

Replaces the unconditional headless gag with a two-way branch on an opt-in tag that **Go parses and
enforces** (Q3 = option 1), available to **Research and Plan only** (Q6). No new state — the existing
resume path already carries the round trip.

**Sized honestly: the *diff* is small, the *risk* is not.** This is the only story that can change
the behaviour of untagged runs, and it carries three sharp defects (F-9, F-10, F-16).

### Field names — one set, declared here

**`MayAsk` and `PreviousMayAsk`.** This **supersedes** `feature-spec.md:47-48`
(`MayAsk`/`AnswersQuestion`) and rev 1's `AskingAllowed`/`PreviousAsked`.

| Field | Meaning | Source | Derivable? |
|---|---|---|---|
| `MayAsk bool` | may **this** run ask? | tag on **this** run's trigger comment **AND** mode ∈ {research, plan} | **yes** |
| `PreviousMayAsk bool` | did the **previous** run have permission? | re-parse the tag from the previous run's trigger comment | **yes** — see below |
| ~~`PreviousAsked` / `AnswersQuestion`~~ | did the previous run actually ask? | requires `needs_input` | **no — and A2 is now deleted**, so this is gone for good |

**Why the wrong bit matters:** because `writeResumeHeader` emits no permission statement, silence on
the resume path means *inherit from transcript* — so the dangerous cell is **previous run tagged →
this resume untagged**, where the transcript still says "you may ask" and nothing contradicts it.
`PreviousAsked` never touches that cell.

**`PreviousMayAsk` is derivable with zero schema cost, but it is not free:**

- `PreviousMode` is a **store column read** (`prepare.go:175`). There is no such column for permission.
- `PreviousMayAsk` must be **re-parsed from the previous run's trigger comment**. Everything needed is
  in memory: `store.Run.TriggerCommentID` (`store/runs.go:75`, in `runColumns`, populated by every
  `ListRuns`, currently unused by `prepare`) matched against the comment list `commentHistory`
  already loads on both paths (`prepare.go:149-152`, `:297`).
- **No watermark problem** — `store.ListComments` (`store/comments.go:151-154`) is
  `WHERE session_id = ? ORDER BY forge_comment_id`, no watermark predicate, and nothing in `internal`
  deletes comment rows.
- **But `harness.HistoryEntry` (`harness/prompt.go:45-60`) carries no forge comment id**, so either
  `commentHistory` also returns the matched body or the match happens inside it. A small local
  refactor in `prepare.go` — real work. **T-A1.6 is sized accordingly.**
- **Fallback:** if the previous run was triggered by the issue body, `previous.TriggerCommentID == 0`
  and no comment row exists (`store/runs.go:122-132`). Parse from `req.Issue.Body`, also in memory.
  **T-A1.7, with its own AC.**

### Architecture Notes

- **The feature is prompt text plus one bit.** Everything else is plumbing.
- **Q3 = option 1 is applied: Go parses the literal, line-anchored, hard enforcement.** An untagged
  run **cannot** ask, whatever the agent decides. Rev 2 recommended presence-only parsing; the owner
  chose literal parsing. Not re-argued.
- **Q6 = Research and Plan may ask; Implement never.** So permission is a **conjunction**:
  `MayAsk = tagPresent && mode ∈ {research, plan}`. The mode is already on `PromptInput` and Go
  already branches on it (`const InferMode = "infer"`, `prompt.go:422`) — so the mode half costs
  nothing new.
- **Decision taken, and flagged because it is the seam between Q3 and Q6.** When the mode is
  **`infer`**, Go does **not** know whether the run will turn out to be Research, Plan or Implement —
  the agent classifies it from `CLAUDE.md` after the prompt is built. Go therefore cannot machine-
  enforce "Implement never asks" on an infer run. **The rule is carried in prompt text for that path**
  (*"if this run resolves to Implement mode, do not ask; proceed headless"*) while the tag gate stays
  machine-enforced. **This is the one place Q3's hard enforcement is soft, and it is soft for a
  structural reason, not an oversight.** *(Flag if you want infer runs denied permission outright
  instead — that is stricter, cheaper, and costs you the ability to tag an unclassified comment.)*
- **There are TWO headless paragraphs, not one, and they must change together.** The gag at
  `harness/prompt.go:396-401` and the feature-slug paragraph at `:388-390` — *"It is stated here
  because this run is headless and no one can answer a question."* Changing only the first leaves a
  contradiction inside the same header.
- **CRITICAL (F-9, verified) — the gag paragraph bundles two unrelated instructions and must be split
  before it is branched.** It ends: *"**Do the work in this worktree; do not commit or push —
  litefactory does that once you are finished.**"* Branching the whole paragraph on `MayAsk` **drops
  the commit/push prohibition on every tagged run**, colliding with litefactory's own commit step.
  Split first: the worktree/commit sentence stays **unconditional**. T-A1.2 gates T-A1.5.
- **F-10 — the slug paragraph's substance must survive.** Stating the feature slug **is** the D-3
  mechanism; without it a run stops on a question nobody can route to a session. **Only the
  justification clause (*"because no one can answer"*) may change.** AC-A1.3 pins this.
- **`writeResumeHeader` (`prompt.go:404-425`) gains a conditional permission paragraph**, following
  the precedent inside that same function — `if mode == InferMode && mode != TrimSpace(in.PreviousMode)`
  (`prompt.go:422`).
- **Emit whenever `MayAsk != PreviousMayAsk`, in BOTH polarities, and write the reason into the code
  comment**, or someone will "simplify" it to one direction: for infer-guidance, not emitting is
  merely *redundant*; here, **not emitting fails open**.
- **Q9 = (b): the resume header's answer framing is hedged** — *"If your previous message ended in a
  question, treat the new comment as the answer."* It asserts nothing Go does not know: the condition
  is evaluated by the agent, from its own transcript. T-A1.10 is unblocked.
- **Q8 = agent-level. There is no Go cap and no timeout.** The agent judges whether its questions were
  answered and honours a direct instruction to move on. **T-A1.11 is a prompt/agent-definition task,
  not a Go task**, and nothing in `internal/` counts rounds.
- **Every asking comment must carry a standing line inviting *"move to the next phase."*** This is
  both the owner's stop-word and, per *One Run, One Mode*, exactly the human comment that opens the
  next mode. It belongs in the ask-comment template, in `research-orchestrator.md` and
  `plan-orchestrator.md`.
- **`KindFreeText` carries the whole comment body as the instruction.** The tag line must be stripped
  or it becomes part of what the agent is asked to do.
- **F-15 — the free-text path is not the only path.** `ParseCommand` (`command.go:152`) also yields
  `KindSpike` with `Text = TrimSpace(rest)`; a tag inside `/spike lf: ask do X` lands mid-`Text` and
  must be stripped there too.
- **F-16 — the tag-only comment is an unhandled failure.** A body containing nothing but the tag goes
  `firstToken` → `KindFreeText` → strip → empty `Instruction` → `BuildPrompt` returns
  `ErrInvalidPrompt`, and the run dies with a validation error the owner will not understand. Treat
  it as `KindNone` — no run. AC-A1.7.
- **F-22 — match line-anchored, never with `Contains`.** An issue body or comment *quoting this plan*
  contains the literal tag, and a `Contains` match would turn permission on. Line-anchor, and ensure
  the tag never reaches `Instruction` un-stripped — `Instruction` is documented as trusted and is the
  one field with **no size cap**.
- **Security: the tag is honoured only from the triggering comment, read by Go.** Third-party comments
  trigger nothing (`classify.go:150,214`) but are replayed into history as `old_third_party`. A tag in
  replayed history, in a third-party comment, or in an unrelated issue body grants **no** permission.
  AC-A1.5.
- **Error handling (platform).** `ParseCommand`'s contract is *"never fails. Every body classifies as
  something."* **The tag parser must not return an `error`** — a malformed tag is simply *absent*.
- Constraint worth knowing: **the owner's answer comment is itself capped at 8,192 bytes**
  (`MaxCommentBytes`). A very long answer is truncated on the way in.
- Edge case: a resumed run triggered by the **issue body** returns before `## Context`
  (`prompt.go:306`) and never sees the permission paragraph. Rare — document, do not fix.
- **Depends on S0.** Until S0 ships, a quote-reply to litefactory's question is silently dropped and
  the run never resumes.
- **Existing test coverage, named precisely (F-6).** There is no `testdata/` or `.golden` file in
  `internal/harness`. There are:
  - **`TestSpikePromptIsUnchanged` (`harness/prompt_test.go:359`)** — one full-head literal
    comparison. **Must pass byte-for-byte unmodified.**
  - **`TestBuildPromptStatesTheFeatureSlugAndForbidsQuestions` (`prompt_test.go:76`)** — asserts
    **both** facts in one test. A1 splits those facts apart, so **this test must itself be split**
    (F-11). It is not possible to claim "both existing tests untouched" — one changes by design.
- **F-7 — `writeResumeHeader` is entirely uncovered.** `internal/harness` has **zero** tests setting
  `Resume: true` or `PreviousMode`. **A characterization test must exist before the function is
  touched** — T-A1.8.
- **The tag parser is the load-bearing new function in this plan.** Same code reads `MayAsk`, derives
  `PreviousMayAsk`, and gates two modes. It gets its own AC (AC-A1.4).
- Risk: **ungated asking is R-3.** The tag prevents it. **A1 must not ship without T-A1.1.**
- Platform: no new package, no HTTP layer, no DB access, no migration, no new dependency, no
  concurrency. Synchronous string assembly.
- Out: code review is ring-fenced.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-A1.1 | Tag parser: recognise the tag **line-anchored** on the triggering comment, strip it from the instruction text, **never return an error** | Go | — | **BLOCKED-ON-N4** *(spelling only — the shape is settled)* · M |
| T-A1.2 | **Split the gag paragraph (`prompt.go:396-401`)** so the worktree/commit-and-push sentence is unconditional and only the ambiguity sentences are branchable. No behaviour change yet | Go | — | S |
| T-A1.3 | Plumb `MayAsk` from the parsed comment through `turn.Request` to `harness.PromptInput`, as `tagPresent && mode ∈ {research, plan}` | Go | T-A1.1 | M |
| T-A1.4 | Write the permission text: ask only when the answer changes what gets built and cannot be settled by reading the repo; ask by stopping; batch into one message | Prompt text | T-A1.2 | M |
| T-A1.5 | Branch **both** headless paragraphs in `writeHeader` on `MayAsk`; keep the slug statement on **both** paths; untagged output byte-for-byte unchanged | Go | T-A1.3, T-A1.4 | M |
| T-A1.6 | Derive `PreviousMayAsk`: surface the previous trigger comment's body out of `commentHistory` via `previous.TriggerCommentID`, and re-parse the tag from it | Go | T-A1.1 | M |
| T-A1.7 | Body-trigger fallback: when `previous.TriggerCommentID == 0`, parse from `req.Issue.Body` | Go | T-A1.6 | S |
| T-A1.8 | **Characterization test for `writeResumeHeader`** — pin today's output for `Resume: true` with and without `PreviousMode`. Written **before** T-A1.9 | Test | — | M |
| T-A1.9 | Add the conditional permission paragraph to `writeResumeHeader`, emitted when `MayAsk != PreviousMayAsk` in **both** polarities, with a code comment recording that non-emission fails open | Go | T-A1.6, T-A1.8 | M |
| T-A1.10 | Resume-header answer framing, **hedged per Q9(b)**: *"If your previous message ended in a question, treat the new comment as the answer."* | Prompt text | T-A1.9 | S |
| T-A1.11 | **Agent-level, not Go (Q8).** State in `research-orchestrator.md` and `plan-orchestrator.md` that the agent judges when its questions are answered and **must honour a direct instruction to move on**. No Go cap, no timeout | Docs | T-A1.4 | M |
| T-A1.12 | **Standing line in every asking comment** inviting the owner to say *"move to the next phase"* — and note in the agent definitions that this is the *One Run, One Mode* handoff sentence, not a licence to self-continue | Docs | T-A1.11 | S |
| T-A1.13 | Handle the tag-only comment: treat as `KindNone`, no run, rather than `ErrInvalidPrompt` | Go | T-A1.1 | S |
| T-A1.14 | Strip the tag on the `KindSpike` path too (`command.go:152`) | Go | T-A1.1 | S |
| T-A1.15 | Prompt text for the **infer-mode seam**: if the run resolves to Implement, do not ask, regardless of the tag | Prompt text | T-A1.4 | S |
| T-A1.16 | **Split `TestBuildPromptStatesTheFeatureSlugAndForbidsQuestions` (`prompt_test.go:76`)** into a slug test asserting both paths and a gag test asserting the untagged path only | Test | T-A1.5 | S |
| T-A1.17 | New head tests: tagged cold header; the resume header under all four `MayAsk`×`PreviousMayAsk` combinations. Assert `TestSpikePromptIsUnchanged` still passes unmodified | Test | T-A1.5, T-A1.9 | M |
| T-A1.18 | Table-driven tag-parsing tests: present, absent, malformed, mid-body, tag-only, quoted/indented, **third-party comment**, **replayed history**, **issue body quoting this plan**, **tagged Implement run** | Test | T-A1.1, T-A1.3 | M |
| T-A1.19 | `turn`-level test: a comment arriving after a succeeded run in the same segment produces `Resume: true` / `RunPath = resume` | Test | T-A1.9 | S |
| T-A1.20 | Update `prompt.go`'s package doc — it states the design rationale this story falsifies | Docs | T-A1.4 | S |
| T-A1.21 | Note in `research-orchestrator.md` that its log-and-continue rule survives; asking is a higher tier above it, not a replacement | Docs | T-A1.4 | S |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-A1.1 | An **untagged** run's cold prompt is byte-for-byte identical to today's. `TestSpikePromptIsUnchanged` (`prompt_test.go:359`) passes **unmodified**. |
| AC-A1.2 | A **tagged** Research or Plan run's cold prompt contains neither *"do not ask clarifying questions"* nor *"no one can answer a question"*. |
| AC-A1.3 | **A tagged run's cold prompt still states the feature slug.** Deleting `prompt.go:388-390` outright is a failing outcome. |
| AC-A1.4 | The tag parser matches **line-anchored**, returns **no error** for any input, and treats a malformed tag as absent. A body merely *quoting* the tag inside a quoted or indented line grants no permission. |
| AC-A1.5 | A tag in a third-party comment, in replayed comment history, or in an unrelated issue body grants **no** permission. |
| AC-A1.6 | **A tagged run whose mode is `implement` gets the gag, not permission.** Q6 is machine-enforced wherever the mode is known. |
| AC-A1.7 | A tagged run whose mode is `infer` carries the prompt-text rule that Implement does not ask. *(The one place enforcement is textual — documented, not accidental.)* |
| AC-A1.8 | The tag does not appear in the instruction text handed to the agent, on **either** the `KindFreeText` or the `KindSpike` path. |
| AC-A1.9 | A comment containing only the tag starts no run and produces no `ErrInvalidPrompt`. |
| AC-A1.10 | **The worktree/commit-and-push prohibition is present on every cold prompt, tagged and untagged alike.** A tagged prompt missing it is a failing test. |
| AC-A1.11 | When `MayAsk == PreviousMayAsk`, the resume header equals the output pinned by T-A1.8's characterization test, byte for byte. |
| AC-A1.12 | When `MayAsk != PreviousMayAsk`, the resume header contains an explicit permission statement — in **both** directions. A test covers all four combinations. |
| AC-A1.13 | The previous run's permission is recovered from its trigger comment; when `TriggerCommentID == 0`, from the issue body. **No new store query, no new column, no migration.** |
| AC-A1.14 | **No ask-round counter, cap or timeout exists anywhere in `internal/`.** Per Q8 this is agent-level; a Go implementation of it is a failing outcome. |
| AC-A1.15 | Every asking comment carries the standing *"move to the next phase"* line. |
| AC-A1.16 | The owner's reply resumes the same Claude conversation via the existing resume path — asserted at `turn` level: a comment after a succeeded run in the same segment yields `Resume: true` / `RunPath = resume`. |
| AC-A1.17 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## User Story C1 — The plan ships as one GitHub sub-issue per user story *(new this revision — see N1)*

> As the owner, I want each user story to arrive as its own GitHub sub-issue, so that I can track,
> comment on and close them independently instead of scrolling one flat plan document.

Changes Plan mode's own output shape. **This story is the largest thing in the plan, and it is the
only one gated on a decision the owner has not yet made.**

> **Read N1 first.** This story exists because the Q4 answer re-scoped the deliverable, and **that
> reverses the owner's own earlier deferral** of sub-issues (*"sub issues we defer to a later run"*).
> If the reversal was not intended, this story is deleted in one line and nothing else in the plan
> moves.

### Architecture Notes

- **`internal/github` cannot create anything today.** Every verb across the five call sites is GET or
  POST for reads and comments; **there is no PATCH, PUT or DELETE anywhere.** C1 needs an **eighth**
  method (create issue) and a **ninth** (attach as sub-issue).
- **That trips `internal/github/surface_test.go:56` — deliberately.** The test exists to force the
  SDK-versus-hand-rolled decision rather than let the client grow by accretion. **The decision is the
  owner's: N2.** Rev 3 does not take it.
- **The sub-issue API facts in `RESEARCH_BRIEF.md` are `[W]` — second-hand and unverified.** Web tools
  were disabled for the entire research session, and the brief's own source records six of *its*
  predecessor's external GitHub claims as wrong, two of them load-bearing. **Every sub-issue task here
  carries an AC requiring the contract be confirmed against live GitHub documentation before any code
  is written.** This is not diligence theatre; the last time this repo trusted a `[W]` GitHub fact it
  was wrong.
- **No backoff exists for bulk creation.** A plan with eight stories is eight creations plus eight
  attachments against a rate-limited API, in a loop, with no retry anywhere in `internal/github`. A
  partial failure halfway through leaves a half-built plan on the issue and no record of where it
  stopped. **Idempotency and resumability are requirements, not niceties** — the natural mechanism is
  the existing `<!-- lf:` marker convention, checking for an already-created sub-issue before
  creating another.
- **N3 decides whether `IMPLEMENTATION_PLAN.md` survives.** If mirrored, `CLAUDE.md`'s Implement gate
  is untouched and sub-issues are an additional rendering. If replaced, the gate can never pass,
  `CLAUDE.md` and `initialplan.md:243` must be amended in the same change, and the plan of record
  becomes a mutable, unversioned GitHub object.
- **Sub-issue bodies are agent-authored text going to the forge**, so the same rules as B2 apply:
  route through `internal/report`, `sanitize`/`fenceFor`, redaction residual assertion.
- **Audience.** Per Q5's split, a sub-issue body is read by business users — so it takes the
  **business register**, names its problems, and links the technical detail. Same rules as B1.
- Platform: **this is the only story that adds `internal/github` surface**, the only one that adds a
  network write verb, and the only one that may add a dependency. It should be reviewed as a client
  change, not as a plan-formatting change.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-C1.1 | **Confirm the GitHub sub-issue API contract against live documentation** — endpoint, payload, parent linkage, limits, and whether it is GA or preview. Record what was checked and when. **No code before this** | Design | — | **BLOCKED-ON-N1** · M |
| T-C1.2 | Settle SDK versus hand-rolled and record the decision with its reasoning | Design | T-C1.1 | **BLOCKED-ON-N2** · M |
| T-C1.3 | Eighth client method: create issue | Go | T-C1.2 | M |
| T-C1.4 | Ninth client method: attach an issue as a sub-issue of a parent | Go | T-C1.2 | M |
| T-C1.5 | Update or replace `TestClientMethodSetIsTheSevenEndpoints` per T-C1.2's decision — deliberately, with the reasoning in the commit | Test | T-C1.3, T-C1.4 | S |
| T-C1.6 | Idempotent creation: a marker-based check so a re-run does not duplicate sub-issues after a partial failure | Go | T-C1.4 | M |
| T-C1.7 | Backoff and partial-failure handling for bulk creation; a run that fails halfway reports what it created | Go | T-C1.6 | M |
| T-C1.8 | `plan-orchestrator.md`: emit one sub-issue per user story, in the business register, each linking the technical detail | Docs | T-C1.1 | M |
| T-C1.9 | Decide and document whether `IMPLEMENTATION_PLAN.md` is still written | Docs | — | **BLOCKED-ON-N3** · S |
| T-C1.10 | *(only if N3 = replaced)* Amend `CLAUDE.md`'s Implement gate and `initialplan.md:243`, in the same change | Docs | T-C1.9 | M |
| T-C1.11 | Route sub-issue bodies through `internal/report`'s `sanitize`/`fenceFor` and redaction | Go | T-C1.3 | S |
| T-C1.12 | Tests: creation, attachment, idempotent re-run, partial failure, and rate-limit backoff — all against a faked client | Test | T-C1.7 | L |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-C1.1 | **The sub-issue API contract was verified against live GitHub documentation before implementation**, and the verification is recorded with its date. A `[W]`-sourced claim is not sufficient evidence for any assertion in this story. |
| AC-C1.2 | The SDK-versus-hand-rolled decision is recorded with its reasoning; `TestClientMethodSetIsTheSevenEndpoints` was changed **deliberately**, not silently widened. |
| AC-C1.3 | Each user story in a plan produces exactly one sub-issue linked to the parent issue. |
| AC-C1.4 | Re-running Plan mode on the same session creates **no duplicate** sub-issues. |
| AC-C1.5 | A failure partway through bulk creation reports which sub-issues were created; a subsequent run completes the rest rather than starting over. |
| AC-C1.6 | Sub-issue bodies are in the business register, name the problems, and link the technical detail. |
| AC-C1.7 | *(if N3 = mirrored)* `$session/IMPLEMENTATION_PLAN.md` still exists and `CLAUDE.md`'s Implement gate is unmodified. |
| AC-C1.8 | *(if N3 = replaced)* `CLAUDE.md` and `initialplan.md` are amended in the same change, not left contradicted. |
| AC-C1.9 | Sub-issue bodies pass the redaction residual assertion and cannot break report structure. |
| AC-C1.10 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## Shipping order

1. **S0** — the quote-reply fix. **First**, because A1, B2 and C1 all depend on the owner being able
   to reply to a marker-bearing comment, and today the most natural reply path is silently discarded.
   **Fully unblocked** (Q10).
2. **B1** — no Go, no dependencies. **Fully unblocked** (Q1, Q5, Q7).
3. **B2** — delivery is free. **The link is not.** Rev 3 called it *"one field, one builder, one
   retired test"*; rev 3.1 corrects that to a `SHAAfter` plumb, a builder, a `Pushed` gate, a golden
   regeneration, and artifact discovery. **No longer fully unblocked** — the link half is
   **BLOCKED-ON-N5**. **The delivery half (T-B2.1) is still free and can ship first**, so B2 splits
   cleanly if you want the summary landing before the link works.
4. **A1** — after the above, because it is the only story that can change the behaviour of untagged
   runs, and it carries F-9, F-10 and F-16. **Unblocked except for the tag's spelling (N4)**, which
   blocks only T-A1.1's literal, not its shape — everything else in A1 can be written against a
   constant.
5. **C1** — **only if N1 confirms the reversal**, and not before N2 and N3 are answered.

**~~B1 and B2 may now be merged~~ — withdrawn in rev 3.1.** Rev 3 offered the merge on the grounds
that B2's link work was small. It is not: **B2 is L-sized after all**, which is the exact objection
rev 2 raised and rev 3 believed had gone away. **Keep them separate.**

**Hidden coupling worth stating once:** the tag parser (T-A1.1) is the same code that derives
`PreviousMayAsk` (T-A1.6) and gates both Research and Plan. It is the one genuinely load-bearing new
function in this plan, which is why AC-A1.4 exists as a criterion of its own.

---

## Out of Scope

- **Story A2 — parking as a first-class state (`needs_input`).** **Deleted, not deferred** (Q11 = A1
  only). Everything it contained is gone from this plan: the `needs_input` run status and session
  state, `terminalRunStatus` changes, the `lf:question` marker and renderer, the sidecar signal
  channel, watermark/consumption handling, `litefactory status` visibility, and the no-answer
  behaviour.
  **What would bring it back, in one line:** a run that asks reporting as **✅ succeeded** turns out to
  be genuinely confusing in practice — at which point parking becomes worth its cost, and LF-12's
  permission gate becomes its second caller so it is built once rather than twice.
- **Code review.** Ring-fenced by the owner: *"code review and alike are untouched by this!"* Nothing
  is proposed for `go-code-reviewer`, `CODE_REVIEW.md`, or the Implement-mode review gates.
- **F-19 — deliberate marker impersonation by a third party.** Any third party can promote their
  comment from `old_third_party` to `litefactory` by writing `<!-- lf:` in it. **Real, pre-existing,
  and explicitly NOT fixed by Story S0** — line-anchoring stops the accident, not the attack. The real
  fix is inverting author-versus-marker precedence in `Classify`, which `classify.go:193-196`
  deliberately argues against for a stated reason. **A security change larger than issue #7; plan it
  as its own story.** *(Named here so it does not look handled.)*
- **A Go-side ask-round cap or timeout.** Explicitly excluded by Q8 — this is the agent's judgement,
  and building it in `internal/` would contradict the answer.
- **Any imposed length limit on the research brief or the summary.** Explicitly declined at Q5. The
  32 KiB truncation is a physical property of `report/run.go`, not a policy this plan sets.
- **Run re-ordering for the blob link.** **Already done — confirmed twice.** `harvest.go` commits
  then pushes, and the daemon calls `turn.Execute` before `reporter.ReportTurn`. `initialplan.md:234`
  anticipates a change that has landed. **A diff that re-orders them is a failing outcome**
  (AC-B2.11), not an improvement. This is the one B2 claim rev 3.1's verification pass left standing
  unchanged.
- **Reworking the eleven interactive approval gates** across the agent definitions. Opt-in asking
  defuses the risk that motivated it; a twelve-file markdown rewrite is its own change.
- **LF-12 — the permission gate.** Named as the second caller of A2's machinery so that if A2 ever
  returns, it is not built twice.
- **LF-6 labels, LF-13 draft PRs, PR review comments as input, `/include`, prompt-injection
  hardening, rate limiting (LF-11).**
- **Unblocking `/research` `/plan` `/implement` as slash commands** — a separate Plan-sized question
  per `mode-closure/SPIKE.md`.
- **Multi-comment document splitting.** Explicitly rejected under B2.
- **Any schema migration.** Nothing in this plan needs one.

---

*Note on what LF-7 no longer is: it was in `Out of Scope` in rev 2 as *"unstarted and actively
test-blocked"*. It is now Story B2's link work. **Rev 3.1 correction: rev 2's "test-blocked" was more
right than rev 3 gave it credit for.** Rev 3 identified the block as the single `/blob/` guard and
sized retiring it at S. The guard is the small half — **twelve whole-body goldens are the real
block**, and `.aiteam/` artifact discovery does not exist at all. LF-7 is in scope, but it was never
as unblocked as rev 3 said.*
