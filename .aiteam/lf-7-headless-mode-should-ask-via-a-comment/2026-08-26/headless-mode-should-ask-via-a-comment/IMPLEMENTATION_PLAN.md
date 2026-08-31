# Implementation Plan — headless-mode-should-ask-via-a-comment
# Branch: lf-7-headless-mode-should-ask-via-a-comment
# Date: 2026-08-30 (**rev 6** — **execution scope split: B1 and E1 are owner-executed, not
#   agent-executed**; see *What changed in rev 6*. Rev 5 answered the last of the eighteen owner
#   questions; nothing is outstanding.)
# Issue: marioatcloudvane/litefactory#7
# Sources: `RESEARCH_BRIEF.md` rev 2 · `feature-spec.md` · `ARCHITECT_REVIEW.md` · the owner's
#   scoping comment of 2026-08-27 · **his answers to Q1–Q11 (2026-08-30)** ·
#   **his answers to N1–N5 (2026-08-30)** · **his answers to N6–N7 (2026-08-30)** ·
#   a verification pass that landed after rev 3
# Citations pin to `origin/main` @ **`71d3bea`** *("aiteam: scaffold the tool-enforcement session")*.

> **This document is scheduled for deletion by its own Story E1.** N3 replaced Plan mode's output
> with `plan_brief.md` plus one GitHub sub-issue per story. This file remains the plan of record
> until E1 and C1 ship; it is then superseded, not archived in place. See **Story E1**.

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

**Rev 4 extends the warning to two new areas, without re-pinning either:**

- **`internal/github`'s surface tests.** Every line number in Story D1 (`surface_test.go:56`, `:92`,
  `:137`) and in `doc.go` is **owner-supplied and owner-verified at `71d3bea`** — the best provenance
  in this document. They are still re-pinned before editing, because `surface_test.go` is on the
  52-file stale list and rev 4 read none of it first-hand.
- **`.claude/` and `CLAUDE.md`.** Story E1's file counts below come from a **disk grep**, not from
  `71d3bea`. They are a lower bound on the work, not an exact inventory. **T-E1.0 re-runs the grep
  against `71d3bea` before any file is edited.**

---

**Worktree caveat, corrected.** `internal/harness/{prompt,runner,meta,errors,doc}.go`,
`internal/turn/{prepare,harvest}.go` and `internal/naming/session.go` are stale on disk — **and so
are 52 files under `internal` and `cmd`.** Re-read from `origin/main` @ `71d3bea` before trusting any
line number.

---

> **Rev 5 closes the plan's question list entirely. There are no open questions and no blocked
> tasks.** He has now answered **eighteen**: Q1–Q11 at rev 3, **N1–N5 at rev 4**, and **N6–N7 at
> rev 5**. Every one is recorded below as **ANSWERED**, with what he decided.
>
> **The one thing rev 5 deliberately does not settle is the SDK's version literal**, and that is
> recorded as an acceptance criterion rather than a question — see **AC-D1.1**, **AC-D1.2** and
> **T-D1.1**. Pinning it needs live access to the module proxy and GitHub's docs, which this session
> does not have; **inventing a version number here would be exactly the `[W]`-grade guess this plan
> spends a page warning about.**
>
> **Three of the five N-answers overrode a recommendation, and all three are applied without
> re-argument.** N2 (SDK) reverses a documented project policy. N3 (replace the plan file) rejects
> rev 3.1's *mirrored* recommendation outright. N5 took the recommendation but at the expensive end.
> Where an answer reversed something, rev 4 records it as **a deliberate decision by the owner**, not
> as drift — that is the whole point of having asked.
>
> **Story count 5 → 7.** N2 and N3 are each far too large to bury inside C1, so each is its own
> story: **D1** (adopt an SDK) and **E1** (Plan mode's output becomes `plan_brief.md`). C1 shrinks to
> what it always should have been — creating and linking the sub-issues.
>
> **Rev 4's two new questions — N6 (which SDK) and N7 (migrate or hybrid) — are answered at rev 5
> and no new ones are raised.** Rev 5 asks nothing.
>
> **`BLOCKED-ON-Nn` no longer appears anywhere in this plan.** S0, B1, B2, A1, E1, D1 and C1 are
> **all fully unblocked**. Every task is ready to be picked up.

## What changed in rev 6

*Scope only. **No story was added or removed, no acceptance criterion was weakened, and the
sequencing S0 · B1 · B2 · A1 · E1 · D1 · C1 is untouched.** What changes is **who executes two of the
seven stories** — a fact established by measurement, not by preference.*

**Provenance: the 2026-08-30 Implement run (`~/.litefactory/logs/runs/.../7/0001-0011.jsonl`),
which delivered zero of seven stories.** Its `DRIFT_REPORT.md` attributed the failure to a diverged
Go tree and a missing `.claude/` write grant. Re-measured against the logs, **29 tool denials**
break down as **Bash 22 · Edit 4 · Read 2 · Write 1**, and the diagnosis was wrong in one place and
incomplete in another:

| # | Change | Evidence | Direction |
|---|---|---|---|
| 1 | **Blocker 1 ("diverged Go tree") did not exist and is struck.** `git diff --name-only main...HEAD` returns **zero** `.go`/`go.mod`/`go.sum` files: this branch has never touched `internal/` or `cmd/`. It was 27 commits *behind*, not diverged. The report's *"52 files, 257 insertions, 7,752 deletions"* is a one-way diff read backwards — those are additions on `main` the branch lacked | measured 2026-08-30 | **struck; now resolved** |
| 2 | **The branch is synced.** `main` merged in cleanly, no conflicts; `go build ./...` and `go vet ./...` pass. The report's own check, `git diff --shortstat origin/main -- internal cmd`, is now **empty** | merge `da5ef58` | **cleared** |
| 3 | **The dominant blocker was never `.claude/` — it was the Bash allowlist**, 22 of 29 denials: `git ls-tree`, `git merge`, `git checkout`, `go doc`, `grep`, `find`, `ls`, `mkdir`, `curl`. `claude.allowed_tools` grants only `git status\|diff\|log\|show`, `go build\|test\|vet`, `gofmt`, `make`. The run could not list files at a ref, which is **how it came to misread the diffstat in #1** | run logs | **root cause, restated** |
| 4 | **Do not widen `allowed_tools` to fix #3.** The `tool-enforcement` session's **US3 — "Bash is confined by the OS sandbox"** replaces the command allowlist with sandbox confinement. Patching the allowlist now is work that is thrown away when US3 ships | owner, 2026-08-30 | **deferred, deliberately** |
| 5 | **B1 and E1 become owner-executed** — see the banners on both stories. The `.claude/**` write refusal is **not litefactory's**: the live `claude-settings.json` deny list contains no rule matching the worktree's `.claude/`; its only `.claude` entry is `Read(~/.claude/**)`, pointing at the operator's home. The refusals carry the generic *"Permission to use Edit has been denied"*, not the deny-rule wording, and repo-root `CLAUDE.md` and `initialplan.md` were edited successfully in the same run | run 0010 | **re-assigned** |
| 6 | **AC-E1.2 still needs its carve-out.** Its grep trips on four Go **test** files carrying comment-only historical citations — `internal/cli/cmd_logs_test.go:534`, `internal/config/toolprofiles_test.go:13`, `internal/harness/meta_test.go:222`, `internal/report/report_test.go:25` — while **AC-E1.10 forbids E1 from modifying any Go file**. E1 cannot clear its own check. **Left open; rev 6 does not amend it** | run 0010, verified | **open** |
| 7 | **Blocker 3 (no network for T-D1.1) is cleared by configuration, not by code.** The `web` tool profile already carries `WebFetch`/`WebSearch`; the issue is labelled `lf:tools=web`. **D1 can now pin `go-github` itself**, and AC-D1.2's live check is executable | owner, 2026-08-30 | **cleared** |

**Precedent worth keeping, from the same run.** The B1 engineer returned BLOCKED with the tree
untouched and **declined to substitute `Write` for the refused `Edit`**. That is the norm: a refused
tool call is a decision, not an obstacle to engineer around.

**What rev 6 did not touch, deliberately:** the provenance warning, the `71d3bea` citation base (the
merge in #2 moves the tree past it — **T-B2.0's re-pin is now mandatory, not optional**), the story
set, the sequencing, and every AC.

## What changed in rev 5

*A small, surgical amendment. The owner answered the last two questions. **No story was added, no
story was removed, and the seven-story set from rev 4 stands unchanged.** Nothing was re-verified —
citations still pin to `71d3bea` and the provenance warning above still governs, unamended.*

| # | Change | Source | Direction |
|---|---|---|---|
| 1 | **N6 answered: the SDK is `github.com/google/go-github`.** The recommendation was taken | owner, N6 | settled |
| 2 | **The version is still NOT settled, and is not invented here.** The *library* is chosen; the *version* is an **implementation-time decision**, taken by someone with live access to the module proxy and GitHub's docs. **T-D1.1 is the first action of D1**, and **AC-D1.2** still requires sub-issue support be confirmed against that pinned version, with the generic-request fallback if it is absent | owner, N6 | recorded, not guessed |
| 3 | **N7 answered: (a), full migration.** All seven existing endpoints move to the SDK; `net/http` and `pagination.go`'s Link-header helper come out | owner, N7 | **settled at the expensive end** |
| 4 | **D1's size is flatly XL.** The *"XL if migration / L if hybrid"* conditional is deleted | rev 5, N7 | — |
| 5 | **T-D1.3 unblocked.** Its remaining job is to **record** the migration decision and its reasoning, not to make it | rev 5, N7 | unblocked |
| 6 | **T-D1.7 loses its conditional.** Unconditionally: migrate the seven endpoints call site by call site and remove the Link-header helper. The *"(if hybrid: …)"* branch is deleted | rev 5, N7 | unblocked |
| 7 | **The deciding argument stays on the record: one credential path is one thing to guard (AC-D1.6)** — not tidiness | owner/rev 4 | preserved |
| 8 | **The hybrid option moves to `Out of Scope` as *considered and rejected*, with its two-credential-path risk**, so a future reader sees it was weighed rather than missed | rev 5 | recorded |
| 9 | **All eighteen questions are ANSWERED and the open-questions section is gone**, replaced by a line saying plainly that nothing is outstanding | rev 5 | **closed** |
| 10 | **Sequencing re-checked, and it holds unchanged:** S0 · B1 · B2 · A1 · E1 · D1 · C1. The E1+C1 hard coupling and the D1→C1 dependency are stated correctly and are untouched | rev 5 | confirmed |

**What rev 5 did not touch, deliberately:** the provenance warning, the `71d3bea` citation base,
**T-B2.0** (re-pinning is still Implement's first task), the story set, and every AC not named above.

## What changed in rev 4

*The owner answered all five outstanding questions. Three answers changed the shape of the
deliverable rather than filling a hole in it, so this is the largest structural revision since rev 1.*

| # | Change | Source | Direction |
|---|---|---|---|
| 1 | **All five N-questions marked ANSWERED.** N1–N5 join Q1–Q11 in the answered record; nothing is left outstanding from the sixteen | owner | — |
| 2 | **N1 confirmed: sub-issues are deliberately in scope.** The hedging about an accidental reversal is **removed** — he confirmed it as a change of mind, made *"for completness"*. C1 is real | owner, N1 | settled |
| 3 | **N2 answered: adopt an SDK.** This **overrides a documented project policy** (`internal/github/doc.go`'s *"# No SDK"* section) and retires the tripwire test that forced the question. **Split out as its own Story D1** | owner, N2 | **much more expensive** |
| 4 | **The `.Reveal()` guard is the sharpest item in D1.** `TestRevealIsCalledExactlyOnce` (`surface_test.go:137`) is what keeps the token out of logs, and an SDK takes credentials through a different path. **It will break, and it must be re-established, not deleted.** Own task, own AC | owner, N2 | **security-critical** |
| 5 | **N3 answered: `IMPLEMENTATION_PLAN.md` is REPLACED, not mirrored.** Plan mode's output becomes **`plan_brief.md` + one sub-issue per story**. Rev 3.1 recommended *mirrored*; the recommendation is **overridden and not re-argued**. **Split out as its own Story E1** | owner, N3 | **much more expensive** |
| 6 | **E1 is 13 files, not 4.** A disk grep for `IMPLEMENTATION_PLAN` finds **123 occurrences across 34 files**; excluding `.aiteam/` archives, **13 live files** name it. The owner's list named four. **And zero of them are Go** — see E1 | verification | more expensive |
| 7 | **The Implement gate stays a local file check.** `plan_brief.md` becomes the gate; sub-issues are **discovered from its Story Index**. Recommended over a forge-side gate, with reasons | rev 4, N3 | risk avoided |
| 8 | **N4 answered: the tag is `lf: ask`**, line-anchored, in the triggering comment body. T-A1.1 unblocked; **A1 is now fully unblocked** | owner, N4 | settled |
| 9 | **N5 answered: (a), build real artifact discovery.** T-B2.3b keeps its **L** size and is **unblocked**. The `Files`-matching fallback is **removed from the plan**, with one line recording why it was rejected | owner, N5 | settled at the expensive end |
| 10 | **C1 shrinks.** With D1 owning the client change and E1 owning the artifact change, C1 is creation, linkage, idempotency and partial-failure recovery. It now **depends on both** | rev 4 | re-scoped |
| 11 | **A real upside recorded, not just costs.** The sub-issue API facts in `RESEARCH_BRIEF.md` are `[W]` — second-hand and unverified. **An SDK encapsulates exactly the facts that block is untrusted about**, so N2 materially *reduces* the risk C1 carries. This is an argument in the owner's favour and it belongs in the record | rev 4 | **risk reduced** |
| 12 | **Two new questions: N6** (which SDK, which pinned version) and **N7** (migrate or hybrid). Both fall out of N2; neither is answerable from this repo. **Both answered at rev 5** | rev 4 | ~~open~~ → answered |

---

## Answered questions — the record

*Eighteen — **the complete list.** Q1–Q11 answered at rev 3; **N1–N5 at rev 4**; **N6–N7 at rev 5**.
**Nothing is outstanding, and there is no open-questions section after this one.***

### Q1 — Is the research brief MIRRORED or MOVED? — **ANSWERED: MIRRORED**

The full brief **stays a repo file** under `.aiteam/`. The comment carries an easy-terms summary
**plus a clickable link** to the brief.

**Consequences applied:** `CLAUDE.md`'s Plan gate keeps working untouched — T-B2.8 (amend the gate
and `initialplan.md:243`) is **deleted**, it was the `moved` branch. AC-B2.9 deleted; AC-B2.8 becomes
unconditional. And `research-orchestrator.md` must now produce **two** things per run, not one: the
file and the summary.

**This is also what makes the link load-bearing.** A summary that cannot reach its detail is a worse
artifact than the brief it replaced, so B2's link is not a nicety — see Story B2.

> **Note the asymmetry rev 4 introduces, deliberately.** The *research brief* is mirrored (Q1); the
> *plan* is replaced (N3). Two artifacts, two opposite answers, both from the owner. That is
> consistent, not contradictory: a brief has no per-story structure to move into sub-issues, and a
> plan does. It is recorded here so a future reader does not "fix" one to match the other.

### Q2 — Where does the tag live? — **ANSWERED: the triggering comment body**

Settled. Per-run, zero new client surface, `KindFreeText` already carries the whole body. **The
spelling is now settled too — see N4: `lf: ask`.**

### Q3 — Who enforces the tag? — **ANSWERED: option 1, Go parses the literal**

Line-anchored, **hard enforcement**: an untagged run *cannot* ask, whatever the agent decides.

**This is not rev 2's recommendation** (which was option 2, presence-only). It is applied as
written and is not re-argued. What it costs, carried into Story A1: a tag parser, a strip step,
`MayAsk`/`PreviousMayAsk` plumbing, the tag-only-comment edge case (F-16), and the `KindSpike` strip
path (F-15). What it buys is the thing he asked for — a machine guarantee rather than a convention.

### Q4 — What did *"-> separate task for that!"* mean? — **ANSWERED, by re-scoping**

He did not pick (a) or (b). He asked for something else: **one GitHub sub-issue per user story,
instead of the stories living inside one flat `IMPLEMENTATION_PLAN.md`.** That is a change to Plan
mode's own output. **N1 confirmed it as deliberate; N2 and N3 priced it.** It is now **three**
stories: **C1** (the sub-issues), **D1** (the client change it forces) and **E1** (the artifact
change it forces).

**The question actually asked is still unanswered**, and rev 4 still proceeds on the stated
assumption that the tag ships in this plan — see `Stated assumptions`.

### Q5 — How brief is "brief"? — **ANSWERED: no imposed size limit**

He declined one explicitly. **The 8,192-byte target and the 20,000-byte ceiling are deleted from
this plan.**

**What replaces them is an audience split, and it is the more useful instruction:**

- **The comment is for business analysts and business users.** Plain language. **Honest — it does not
  swallow technical complexity to sound tidy.** It **names the problems found**. It **always links
  the detailed artifact**.
- **The repo artifact is for technical readers.** It keeps its detail, its citations and its
  provenance tagging. Nothing is cut from it.

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
repo's `CLAUDE.md`**, not in litefactory. **A single neutral literal does not.** This was the whole
reason N4 was worth asking, and **N4 chose the neutral literal.**

### Q7 — How does the brief physically reach the comment? — **ANSWERED: option 1**

Full brief to the repo, summary as the comment. Consistent with Q1. Delivery is the run's **final
message**, which `secResult` already posts — **zero new Go for delivery**. One comment per run. OD-10
dissolves. Routes (ii) sidecar and (iii) Go-reads-the-file are both dead; tasks T-B2.3 and T-B2.4 of
rev 1 are **deleted**.

### Q8 — May a resumed run ask a follow-up, and how many times? — **ANSWERED: agent-level, not Go**

**No Go-side cap. No timeout.** The agent judges whether its questions are answered and **honours a
direct instruction to move on**.

**And every asking comment must carry a standing line inviting the owner to say *"move to the next
phase."*** This lands in the **agent definitions and prompt text**, not in `internal/`.

**Worth recording: this is consistent with *One Run, One Mode*, not an exception to it.** "Move to
the next phase" is precisely the human comment that `CLAUDE.md` requires in order to open the next
mode. The standing line is not litefactory granting itself permission to continue — it is
litefactory telling the owner what sentence to write if he wants it to.

T-A1.11 is a prompt/agent-definition task, not a Go task, and AC-A1.14 makes a Go implementation of
the cap a **failing** outcome.

### Q9 — What may the resume header honestly claim? — **ANSWERED: (b), hedged framing**

*"If your previous message ended in a question, treat the new comment as the answer."* Asserts
nothing Go does not know — the condition is evaluated by the agent, from its own transcript.
T-A1.10 is unblocked and its text is settled.

### Q10 — Quote-reply silently drops the owner's answer — **ANSWERED: proceed with rev 2's recommendations**

Both halves of the recommendation: **line-anchored marker matching**, *and* instructing the owner to
reply plainly rather than by quote-reply. **Story S0 stays first.** T-S0.1 is unblocked. The
deliberate-impersonation half (F-19, Q10(b)) stays in `Out of Scope`.

### Q11 — Do you want the parked state at all? — **ANSWERED: A1 only**

**Story A2 and the `needs_input` parked state are deleted from this plan**, not deferred. They are in
`Out of Scope` with one line on what would bring them back.

---

### N1 — Sub-issues are back in scope — **ANSWERED: CONFIRMED, DELIBERATE**

> *"yes, i changed my mind and we add it here, for completness!"*

**Recorded as an owner-confirmed change of mind, not a slip.** On 2026-08-27 he wrote *"sub issues we
defer to a later run; i'll create a own story later on"*; rev 3 flagged the Q4 answer as a possible
accidental reversal and asked. **It was intentional.** All hedging about accidental reversal is
**removed from rev 4** — C1 is firmly in scope and is planned as a first-class story.

**What the confirmation costs, now priced across three stories rather than assumed inside one:**

- **`internal/github` cannot create anything today.** Every verb across the call sites is GET or POST
  for reads and comments; there is **no PATCH, PUT or DELETE anywhere**. Sub-issues need an **eighth**
  and a **ninth** capability. → **N2, and Story D1.**
- **The plan's own shape changes**, because stories move out of a file and onto the forge. → **N3,
  and Story E1.**
- **The sub-issue API facts in `RESEARCH_BRIEF.md` are `[W]`** — second-hand and unverified. Every
  sub-issue task carries an AC requiring the contract be confirmed against live GitHub documentation
  **before any code is written**. *(N2 materially reduces this risk — see below.)*
- **No backoff exists** for bulk creation. Eight stories is eight creations plus eight attachments
  against a rate-limited API, in a loop, with no retry anywhere. Idempotency and partial-failure
  recovery are **requirements**, not niceties.

### N2 — SDK or hand-rolled? — **ANSWERED: USE AN SDK**

> *"we do it via the SDK!"*

**This overrides a documented project policy, and rev 4 plans it as the deliberate reversal it is —
not as a quiet swap.** Three artifacts in `internal/github` exist specifically to prevent this
happening by accident, and each must be dealt with **on purpose, with the reasoning recorded in the
commit**. That is Story **D1**.

**What the policy says, verbatim, and what must happen to it:**

1. **`internal/github/doc.go` has an explicit `# No SDK` section.** It reads: *"Six endpoints do not
   justify a dependency. Everything here is net/http plus the Link-header helper in pagination.go.
   The rule for revisiting that: if a third pagination special case appears, reach for a library."*
   **That section must be rewritten**, and the rewrite must record **who decided and why** — and in
   particular that **the trigger was sub-issues, not the third pagination case the rule anticipated**.
   A future reader must not conclude the documented trigger fired. *(Minor: the prose says "six
   endpoints" and the test says seven. Reconcile both while rewriting.)*
2. **`TestClientMethodSetIsTheSevenEndpoints` (`surface_test.go:56`) is the tripwire that forced this
   decision, and it worked.** Its message says: *"An eighth is a reason to re-open the SDK question,
   not to add a method quietly."* **The question was re-opened and it has now been answered.** The
   test is retired or reworked **deliberately**, and the change references that message so the record
   shows the tripwire did its job rather than being stepped over.
3. **`TestNoForgeAbstraction` (`surface_test.go:92`) forbids declaring an interface in any non-test
   file in `internal/github`**, and `doc.go` calls a `forge.go` at M0 *"a review blocker"*.
   **An SDK does not require an interface — but a wrapper is exactly where one gets smuggled in.**
   **Explicit constraint for D1: adopting an SDK must not introduce the forge abstraction.** A
   concrete wrapper struct is fine; an interface in a non-test file is not. LF-18 says that
   abstraction is to be *extracted later from two real implementations*, not guessed now. **This test
   stays green through D1**, and a diff that turns it red is a failing outcome, not a consequence.

**The security item, and it is the sharpest thing in this revision.**

`TestRevealIsCalledExactlyOnce` (`surface_test.go:137`) fails the build if `.Reveal()` appears on any
line not containing `Authorization`, or more than once in the package. **That test is what keeps the
token out of logs** — it makes "the credential is revealed at exactly one place, and that place is
the `Authorization` header" a compile-time-ish property rather than a habit.

**An SDK takes credentials through a different path** — typically an `oauth2` token source or a
custom `http.Client` installed at construction — **so this guard will break.** It is not incidental
breakage; it is the removal of the single mechanism preventing credential leakage into logs.

**Requirement, not a suggestion: re-establish an equivalent guarantee. Do not delete the test.**
It gets **its own task (T-D1.6) and its own AC (AC-D1.6)**. Whatever form the new guard takes, it
must still fail the build when the token is revealed anywhere other than the one place that hands it
to the transport.

**Three things D1 must decide and state — two of them were N6 and N7, and both are now answered:**

- **Which SDK** → **N6: `github.com/google/go-github`.** Settled.
  **Which pinned version → deliberately still open, as an implementation-time decision, not a
  question.** See AC-D1.1 and T-D1.1.
- **Do the existing seven endpoints migrate, or do only the new sub-issue calls use the SDK?** →
  **N7: (a), full migration.** All seven move; `net/http` and `pagination.go`'s Link-header helper
  come out. The hybrid was **considered and rejected** — it leaves two HTTP stacks and two error
  taxonomies in one package, and, decisively for the item above, **two credential paths**, which makes
  the `.Reveal()` guarantee materially harder to re-establish than either pure option. **One
  credential path is one thing to guard.** The rejected option is recorded in `Out of Scope`.
- **The one genuine unknown: whether the chosen SDK version supports sub-issues at all.** GitHub's
  sub-issue API is recent, and SDK coverage of recent endpoints lags. **Web tools are disabled
  session-wide, so this cannot be checked here.** It is therefore **an acceptance criterion — that it
  is confirmed before implementation — and explicitly not an assumption.** If the SDK does not cover
  sub-issues, the fallback is the SDK's generic request escape hatch (most Go GitHub SDKs expose one),
  and **that fallback must be confirmed too, not assumed.** AC-D1.2.

**The upside, recorded because the record has so far been all cost.** The sub-issue facts in
`RESEARCH_BRIEF.md` are tagged **`[W]` — second-hand and unverified**, and that block is the least
trustworthy input to this entire plan: its own source records six of its predecessor's external
GitHub claims as wrong, two load-bearing. **An SDK encapsulates precisely what that block is untrusted
about** — endpoint paths, payload field names, pagination shape, and the `sub_issue_id`-versus-issue-
number trap that is the easiest way to get sub-issues silently wrong. **Adopting one materially
reduces the risk C1 carries**, converting a class of unverifiable-here facts into a dependency's
tested surface. **That is a real argument in the owner's favour and it is why N2 makes C1 cheaper
even as it makes the package more expensive.**

### N3 — Is the plan mirrored or replaced? — **ANSWERED: REPLACED**

> *"the plan is not necessary at this state, since we move user stories into sub-tasks! we would only
> keep a 'plan_brief.md' that keeps all necessary information shared across sub issues that the agents
> then need to read, e.g. common decisions, workflow and alike!"*

**Rev 3.1 recommended *mirrored*. That recommendation is overridden and is not re-argued.** It is left
in the record for archaeology only.

**Plan mode's output becomes two things:**

1. **`plan_brief.md`** — the shared context every sub-issue's implementer needs: common decisions,
   workflow, cross-cutting constraints, and the index of where the stories live.
2. **One GitHub sub-issue per user story** — carrying that story's own architecture notes, tasks and
   acceptance criteria.

**The flat `IMPLEMENTATION_PLAN.md` goes.** This is **Story E1**, and it is bigger than the owner's
question implied — see the file count below.

**The obvious risk, named rather than discovered later.** `CLAUDE.md`'s Implement gate is
*"Implement requires `$session/IMPLEMENTATION_PLAN.md`"* — **a local file-existence check**. If the
stories live in sub-issues, a naive reading says the gate must check the forge. **That would put a
network call in a place that has never made one**, and it makes mode entry fail on a rate limit, an
outage, an expired token or an offline worktree — failure modes a gate should not have.

**Recommendation, and it is what rev 4 plans: `plan_brief.md` is the gate.** Sub-issues are
**discovered from its Story Index**, not from the forge. Reasons:

- The gate stays a **local file-existence check** — the same shape it has today, no new failure modes.
- `plan_brief.md` is **in git**: versioned, diffable, reviewable. A sub-issue is editable by anyone
  with write access, so the forge copy is mutable and unversioned. **The gate should key on the
  versioned artifact**, and the owner's own phrasing supports this — it is the thing *"the agents then
  need to read"*.
- Discovery via the index is **testable offline**; discovery via the forge is not.

**And the anti-duplication rule, which is the failure mode this design must avoid.** If `plan_brief.md`
repeats what the sub-issues say, the two drift, and a reader cannot tell which is authoritative.
**The rule: `plan_brief.md` carries only what two or more stories need.** Anything true of exactly one
story lives in that story's sub-issue and nowhere else. **No per-story task tables. No per-story
acceptance criteria. No per-story architecture notes.** The Story Index carries identity and location
only — ID, title, size, dependencies, sub-issue link — never content. **AC-E1.5.**

### N4 — How is the tag spelled? — **ANSWERED: `lf: ask`**

The recommendation was taken. The tag is the **neutral literal `lf: ask`**, **line-anchored**, in the
**triggering comment body**.

One neutral literal covers both Research and Plan with no table in Go and no coupling to a mode
vocabulary that lives in the target repo's `CLAUDE.md`. **T-A1.1 is unblocked and A1 is now fully
unblocked.**

**One collision check, worth a line because two line-anchored literals now exist in this codebase and
they are not the same thing:** litefactory's self-marker is `<!-- lf:` (an HTML comment,
`turn/classify.go:38`, line-anchored by Story S0); the ask tag is the bare literal `lf: ask`. They do
not match each other — `<!-- lf:` will not match a line beginning `lf: ask`, and vice versa. **State
this in the tag parser's doc comment**, because the resemblance is close enough to invite a future
"simplification" that merges them. **T-A1.1.**

### N5 — How does the comment find `RESEARCH_BRIEF.md`? — **ANSWERED: (a), build real artifact discovery**

The recommendation was taken, **at the expensive end**. Locate `$session/RESEARCH_BRIEF.md`
deterministically so the link **always** renders. **B2 keeps its L size and the link half is
unblocked.** T-B2.3b is no longer BLOCKED and no longer branches.

**Option (b) — matching a `RESEARCH_BRIEF.md` suffix out of `Files` — is removed from the plan.** One
line records why, so it is not rediscovered as a clever shortcut: **it drops the link on any run that
summarises without rewriting the brief** (a resumed research run answering a question; any Plan-mode
summary), which breaks **B1's fourth hard rule that the summary always links the detail** — and it
fails precisely on the runs where the reader has least context.

### N6 — Which SDK, and which version is pinned? — **ANSWERED: `github.com/google/go-github`**

**The recommendation was taken. The library is `github.com/google/go-github`** — the de-facto standard
Go client for the GitHub REST API, permissively licensed, and the library `doc.go`'s *"reach for a
library"* rule was plainly written about. **The choice is settled and is not re-argued.**

**The version is NOT settled, and this plan does not invent one.** This is the one thing rev 5
deliberately leaves for implementation time, and it is recorded as a fact about the plan rather than
as a question:

- **Pinning the version is the first action of D1** — **T-D1.1** — and it must be taken by **someone or
  something with live access to the module proxy and to GitHub's own documentation.** This session has
  no web access, so it cannot check which major is current or which majors ship sub-issue support.
- **`go.mod` pins an exact version, and the literal goes into `plan_brief.md` and `doc.go`.**
  **"Latest" is a failing outcome** (AC-D1.1).
- **`AC-D1.2` stands unchanged:** the pinned version's sub-issue support is **confirmed against a live
  source before any code is written**, with the **generic-request escape hatch as the fallback if it is
  not supported** — and that fallback is confirmed too, not assumed.
- **Writing a version number into this document would be exactly the `[W]`-grade guess the provenance
  warning above spends a page on.** It is left blank on purpose.
- **Nothing else in D1 depends on it.** Every task is written against "the SDK" and is ready now.

### N7 — Do the existing seven endpoints migrate, or only the new sub-issue calls? — **ANSWERED: (a), full migration**

**All seven existing endpoints move to the SDK.** `net/http` and `pagination.go`'s Link-header helper
**come out.** This is also what rev 4 recommended.

**The deciding argument is not tidiness — it is AC-D1.6.** A hybrid would leave **two HTTP stacks, two
error taxonomies and two credential paths** permanently in one package. The third is what matters: the
`.Reveal()`-exactly-once guarantee would have to hold across both paths, which is materially harder to
re-establish and easier to erode later. **One credential path is one thing to guard.**

**The cost is real and is accepted, not overlooked.** (a) rewrites seven working, tested methods that
nobody asked to have changed, in a package whose surface is deliberately pinned by tests, in service of
a feature about sub-issues. **That is why D1 is XL and why it is reviewed as a client-and-security
change rather than as plumbing.**

**Consequences applied:**

- **D1 is XL, flatly.** The *"XL if migration / L if hybrid"* conditional is deleted.
- **T-D1.3 is unblocked.** Its job is now to **record** this decision and its reasoning — not to make
  it.
- **T-D1.7 is unconditional**: migrate the seven endpoints call site by call site and remove the
  Link-header helper. The *"(if hybrid: …)"* branch is deleted.
- **The hybrid is in `Out of Scope` as considered and rejected**, with its two-credential-path risk
  recorded, so a future reader can see it was weighed rather than missed.

---

## Open questions — **none**

**There are no open questions. All eighteen — Q1–Q11 and N1–N7 — are answered and recorded above, and
rev 5 raises none.** This section exists to say that plainly rather than leave an empty heading behind.

**The single thing still genuinely unsettled is the SDK's version literal, and it is not a question —
it is an acceptance criterion.** It cannot be answered by asking the owner or by reading this repo; it
needs live access to the module proxy and GitHub's docs at implementation time. It is carried as
**T-D1.1** (pin it, first action of D1) and **AC-D1.1 / AC-D1.2** (pin an exact version; confirm that
version supports sub-issues, with the generic-request fallback if it does not).

**No task in this plan carries a `BLOCKED-ON-Nn` marker any more.**

---

## Stated assumptions — not questions

*Things rev 4 proceeds on without asking, and says so.*

1. **The tag ships in this plan.** Q4's original question — *does the tag mechanism ship here, or in
   its own run?* — went unanswered when the owner re-scoped. Q2 (where the tag lives), Q3 (who
   enforces it), Q6 (which modes) and N4 (its spelling) all presume it does, and he answered all four
   in detail. **Rev 4 reads that as yes.** If it is a no, Story A1 must not ship at all — A1 without
   the tag is *ungated* asking, which is R-3, the largest product risk in the brief.
2. **The blob link is SHA-pinned, not branch-relative.** A branch-relative URL rots the moment the
   branch moves, **and** buys nothing here: the `/blob/` guard is a bare unanchored
   `strings.Contains`, so a branch-relative URL trips it exactly as a permalink does. **The SHA is
   `SHAAfter`, not `CommitSHA`** — see B2. The real fallback when the commit is not pushed is **no
   link at all** (AC-B2.3).
3. **E1's file inventory is a lower bound.** The 13-file count is from a disk grep, and disk is stale
   in 52 files. T-E1.0 re-runs it against `71d3bea`. **The count may go up; it will not go down.**

---

## User Story S0 — The owner's reply is never silently dropped

> As the owner, I want my reply to a litefactory comment to actually start a run, so that answering
> a question is not a coin flip on which GitHub button I pressed.

Stops a quote-reply from being misclassified as litefactory's own output and discarded. **Ships
first**, because every other story here depends on the owner being able to reply to a comment that
carries a marker — and today, on the most natural reply path, he cannot.

**Size: M. Fully unblocked.**

### Architecture Notes

- **Route settled (Q10): line-anchor the marker match, AND instruct the owner to reply plainly.** Both
  halves, not one.
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
- **Now triply load-bearing.** Under Q1 = mirrored + Q7 = option 1, every research run posts a
  marker-bearing, highly quotable summary. Under Q3 = option 1 the owner must reply to ask-comments
  for the feature to work at all. **And under N3, sub-issues become a second stream of
  marker-bearing comments to reply to.** S0 is the precondition for all three.
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

`research-orchestrator.md` learns to produce **two** outputs — the technical artifact it already
writes, and a business-readable summary for the comment. Zero Go. The fastest thing in this plan to
ship.

**Size: S. Unblocked as work — but see the execution scope below: an agent cannot perform it.**

> ### ⚠ EXECUTION SCOPE — OWNER-EXECUTED, NOT AGENT-EXECUTED *(rev 6)*
>
> **Every file this story writes lives under `.claude/`, and a litefactory run cannot write there.**
> Measured 2026-08-30 (run 0010): two `Edit` calls against
> `.claude/agents/research-orchestrator.md` were refused, while `CLAUDE.md` and `initialplan.md` at
> the repo root were edited successfully in the same run. The boundary is `.claude/`, exactly.
>
> **This is not litefactory's deny list and cannot be granted in `config.yaml`.** The live
> `claude-settings.json` has no rule matching the worktree's `.claude/`; its only `.claude` entry is
> `Read(~/.claude/**)`, which points at the operator's home directory. Nothing an operator writes in
> `claude.allowed_tools` or a `tool_profiles` entry lifts it.
>
> **Treat that as correct, not as an obstacle.** This story rewrites the definition of the agent that
> would be executing it. An agent editing its own orchestrator is self-modification, and a run
> triggered by a third-party issue comment is exactly the case the guard exists for.
>
> **So: the owner writes this story's diff by hand.** The plan keeps it because the *content* — the
> four hard rules, the audience split, the ordering warning — is still the specification. What
> changes is who types it. **Do not dispatch B1 to an implementation agent; it will return BLOCKED,
> as it did on 2026-08-30.** An agent that appears to complete it has routed around a refused tool
> call and has failed the task, not finished it.

### Architecture Notes

- **The audience split is the whole story (Q5).** The comment is for **business analysts and business
  users**; the repo artifact is for **technical readers**. One run, two registers.
- **The summary's four hard rules**, all from the owner and all testable only by reading it:
  1. **Plain language.** No `path:line` citations, no provenance tags, no Go identifiers.
  2. **Honest — it does not swallow technical complexity** to sound tidy. If something is hard, it
     says it is hard and why, in business terms.
  3. **It names the problems found.** A summary that reports only progress is a failing summary.
  4. **It always links the detailed artifact.** This is what makes the plain-language register
     affordable — nothing is lost, it is one click away. **Delivered by Story B2, and N5 = (a) is
     what makes "always" literally true.**
- **No size limit. Do not reintroduce one.** He declined one explicitly.
- **One truncation warning, not a budget.** The final message is cut at `maxResultBytes = 32 << 10`
  (`report/run.go:24`), **from the end** (`run.go:528`). A runaway summary therefore loses its
  **tail** — and the tail is where the link and the open questions live. **State this in the template
  as a warning about ordering**: put the link and the problems early, not last.
- **The technical artifact keeps everything.** Provenance tagging ([V]/[O]/[W]/[R0]), citations,
  section depth — all stay.
- **These rules are reused verbatim by C1.** Sub-issue bodies are read by the same audience under the
  same register. **Written once here, referenced there** — and under N3 they end up stated once in
  `plan_brief.md`, which is exactly the kind of cross-story content it exists to hold.
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

**Size: L. Fully unblocked as of rev 4 (N5 = (a)).**

> ### Sizing note — B2 grew at rev 3.1 and rev 4 does not shrink it back
>
> Rev 3 called the link *"three small things, not a project"*. A verification pass found three of its
> four claims wrong or incomplete:
>
> | Rev 3 said | Actually |
> |---|---|
> | Add repo owner/name to `report.Run` — *"the one genuinely missing input"* | **Not needed.** `report.Run.Ref` is a `naming.Ref` already carrying `Forge`, `Owner`, `Repo`, `Issue` (`internal/naming/ref.go:20-31`) — **cheaper** |
> | Use `CommitSHA` | **Wrong field.** `CommitSHA` is empty exactly on the supported path where claude commits for itself. Must be `SHAAfter`, **which is not plumbed** |
> | Retire one guard test | **The goldens are the real blocker.** Twelve golden files pin whole bodies |
> | *(not mentioned)* | **`.aiteam/` artifact discovery does not exist.** **N5 = (a): build it.** |
>
> **Net: L.** **Rev 4 changes only the last row** — it is answered, not cheaper. *Push already
> precedes the comment: `harvest.go` commits then pushes, and the daemon calls `turn.Execute` before
> `reporter.ReportTurn`. **Do not re-plan the ordering.** AC-B2.11 stands.*

### Architecture Notes

- **Delivery is free (Q7 = option 1).** `secResult` already posts the run's final message. The work
  is instructing `research-orchestrator.md` to put the summary there. **No new Go for delivery.**
- **The link — LF-7 — is IN scope, and it is five things:**
  - **Owner/repo is NOT missing.** `report.Run.Ref` is a `naming.Ref` carrying `Forge`, `Owner`,
    `Repo` and `Issue` (`internal/naming/ref.go:20-31`). **No new field, no new dependency, no I/O** —
    which matters, because `internal/report`'s package doc says it holds **no database handle and does
    no git or forge work**. Importing `internal/github` here would break that; building from `Ref`
    does not. **Build it in `internal/report`.**
  - **The SHA must be `SHAAfter`, and `SHAAfter` is not plumbed.** `turn.Result.SHAAfter` is not
    passed into `report.Run` by `runInput` (`internal/turn/report.go`, the struct literal).
    **`CommitSHA` alone is wrong**: on the supported path where **claude commits for itself** via its
    own Bash tool, `git.CommitResult.SHA` is empty while `Head` has moved — so a SHA-pinned permalink
    renders blank *exactly there*. **A `report.Run` field plus one line in the `runInput` literal**,
    with an AC covering that path specifically.
  - **The goldens are the real blocker, not the guard test.** `TestSuccessReport` ends with
    `checkGolden(t, "run_succeeded", body)`, pinning the **entire body** — any added line fails it.
    Same for `run_failed`, `run_push_failed`, `run_sparse_result`, `run_truncated`, `run_never_started`,
    `run_no_result_recorded`, plus five `outcome_*` under `internal/report/testdata/`.
    **Regenerating them is a task in its own right.**
  - **The `/blob/` guard is a bare unanchored `strings.Contains`**, so a branch-relative URL trips it
    too. Retire or narrow it.
  - **Artifact discovery is real work and is now decided (N5 = (a)).** Locate
    `$session/RESEARCH_BRIEF.md` deterministically. **It must be deterministic, not a scan of
    `Files`** — the whole point of (a) is that the link renders on runs that did not rewrite the brief.
    **The session path is derivable** the same way `CLAUDE.md` derives it (branch, date, feature slug),
    and `internal/naming` already owns session naming — **look there before inventing a mechanism**.
- **The link must be gated on `run.Pushed`.** A link to an unpushed commit 404s, and `Pushed` is
  genuinely false on real paths — push is skipped when the branch did not move, and skipped entirely
  for an abandoned run. **Gate it the same way `metadataList` already gates its "(pushed)" suffix.**
- **Push already precedes the comment — confirmed twice. Do not re-order anything.**
- **Route the body through `internal/report` either way.** `redact.Body` has no exported constructor,
  so "post an unredacted body" is not an expressible program. Credential redaction, the residual
  assertion and the panic guard come free.
- **Redaction is not sanitisation.** `redact` covers credentials only. The summary is agent-authored
  text going straight into a comment and can carry markdown that breaks the report's `<details>`
  fence or callout structure. It **must** route through `report/run.go`'s existing `sanitize` and
  `fenceFor`.
- **Decision taken — never fail silent.** Today an oversize body returns a `LimitError`, no comment is
  posted, the run goes unreported and the watermark does not move. For a visibility feature, "too big
  to show you, so you see nothing" is the wrong failure. Truncate with an explicit marker.
- **Decision taken — no multi-comment splitting.** `post` is the single forge call in `internal/turn`
  and is not in a loop.
- **Security: a summary posted as a comment re-enters the prompt as history wearing litefactory's own
  label.** Quoted third-party text laundered from `[old_third_party]` into `[litefactory]` is trusted
  material it should not be. Keep quoted third-party text attributed and fenced; prefer summarising
  over quoting. **This risk drops under B1.**
- **Depends on S0.** A posted summary is a marker-bearing, highly quotable document.
- Platform: **no new `internal/github` method.** The endpoint surface test is untouched by this story
  — **only D1 changes it.**

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-B2.0 | **Re-pin every citation in this story against `origin/main` @ `71d3bea`** before editing any file in `internal/report` or `internal/turn`. See the provenance warning | Chore | — | S |
| T-B2.1 | Instruct `research-orchestrator.md` to emit the business summary as its final message, and to keep writing `RESEARCH_BRIEF.md` to `$session/` | Docs | — | S |
| T-B2.2a | **Plumb `SHAAfter`**: add the field to `report.Run` and pass `turn.Result.SHAAfter` in `runInput`'s struct literal (`internal/turn/report.go`) | Go | T-B2.0 | S |
| T-B2.3 | Blob-URL builder **inside `internal/report`**: permalink from `Ref` (owner/repo) + **`SHAAfter`** + path. No `internal/github` import, no I/O | Go | T-B2.2a | M |
| T-B2.3a | **Gate link rendering on `run.Pushed`** — emit no link when false, mirroring how `metadataList` gates its "(pushed)" suffix | Go | T-B2.3 | S |
| T-B2.3b | **Real artifact discovery for `$session/RESEARCH_BRIEF.md` (N5 = (a))** — derive the session path deterministically so the link renders on every run, including runs that did not rewrite the brief. Check `internal/naming` first | Go | T-B2.3 | **L** |
| T-B2.4 | **First verify** whether `RenderRun`'s existing truncation already makes silent oversize failure unreachable. If it does, this collapses to a regression test; if not, add an explicit truncation marker | Go | — | S–M |
| T-B2.5 | Route the summary body through `report`'s existing `sanitize` and `fenceFor` | Go | — | S |
| T-B2.6 | **Retire or narrow the `/blob/`-and-commit-URL guard** (`internal/report/report_test.go` — disk `240-241`, `71d3bea` `251-254`), whose stated premise (*"LF-7 is out of scope"*) no longer holds. Note it is an unanchored `strings.Contains` | Test | T-B2.3 | S |
| T-B2.6a | **Regenerate the affected goldens** — `run_succeeded`, `run_failed`, `run_push_failed`, `run_sparse_result`, `run_truncated`, `run_never_started`, `run_no_result_recorded` and the five `outcome_*` under `internal/report/testdata/`. **Regenerate deliberately and eyeball every diff** — a blind regeneration hides exactly the regressions goldens exist to catch | Test | T-B2.3, T-B2.3a | **M–L** |
| T-B2.7 | Remove the blocker comment in `report/run.go` (*"Plain text at M0: no links, no SHA-pinned permalinks (LF-7)"* — disk `:62`, `71d3bea` `:63`) and replace it with what is now true | Docs | T-B2.3 | S |
| T-B2.8 | Tests: a rendered comment contains a `SHAAfter`-pinned `/blob/` URL for the brief; a run with `Pushed == false` contains **no** link and no dead URL | Test | T-B2.3a | M |
| T-B2.8a | Test: **the claude-committed-for-itself path** — `git.CommitResult.SHA` empty, `Head` moved — still renders a working permalink | Test | T-B2.2a, T-B2.3 | M |
| T-B2.8b | Test: **a run that posts a summary without rewriting the brief still renders the link.** This is the case option (b) got wrong and the reason (a) was chosen | Test | T-B2.3b | M |
| T-B2.9 | Tests: an oversize summary still produces a posted comment, explicitly marked as truncated | Test | T-B2.4 | M |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-B2.1 | **(manual / observational — not gateable.)** After a Research run, the summary is readable on the issue and its link opens the full brief. **`implement-orchestrator` must not block on this.** |
| AC-B2.2 | The comment contains a **`SHAAfter`-pinned** `/blob/<sha>/…` URL to `$session/RESEARCH_BRIEF.md`. **Unconditional** — the N5 = (b) relaxation is withdrawn. |
| AC-B2.2a | **On the claude-committed-for-itself path** — `git.CommitResult.SHA` empty, `Head` moved — the permalink still renders a real SHA. **A blank or missing SHA there is a failing test.** |
| AC-B2.2b | The URL is built inside `internal/report` from `report.Run.Ref`. **No `internal/github` import is added to `internal/report`, and no owner/repo field is added to `report.Run`.** |
| AC-B2.2c | **A run that posts a summary without rewriting `RESEARCH_BRIEF.md` still renders the link.** Discovery is deterministic, not a scan of `Files`. **A missing link there is a failing test** — it is the exact defect N5 = (a) was chosen to avoid. |
| AC-B2.3 | When `run.Pushed == false`, the comment contains **no** link at all — never a URL to an unpushed commit. Covers both real paths: branch did not move, and abandoned run. |
| AC-B2.4 | The `/blob/`-and-commit-URL guard is retired or narrowed, and the `report/run.go` LF-7 blocker comment is gone. Neither may be left asserting a premise this story removes. |
| AC-B2.4a | **Every affected golden under `internal/report/testdata/` is regenerated and its diff reviewed.** A golden regenerated without its diff being read is a failing outcome. |
| AC-B2.4b | **Every `path:line` citation this story relied on was re-pinned against `71d3bea` before the file was edited** (T-B2.0). |
| AC-B2.5 | An oversize summary still produces a comment, explicitly marked as truncated. **Silence is a failing test.** |
| AC-B2.6 | The comment carries an `<!-- lf:` marker and is filtered out of the bot's own input. |
| AC-B2.7 | The comment body passes the redaction residual assertion; no credential reaches the forge. |
| AC-B2.8 | Agent-authored markdown cannot break the report's fence or callout structure — the body goes through `sanitize`/`fenceFor`. |
| AC-B2.9 | `$session/RESEARCH_BRIEF.md` still exists after a Research run, and `CLAUDE.md`'s **Plan** gate is unmodified by this story. *(The **Implement** gate changes — that is E1.)* |
| AC-B2.10 | `internal/github` is unmodified by this story. |
| AC-B2.11 | **No run-ordering change is made.** Push already precedes the comment; a diff that re-orders them is a failing outcome. |
| AC-B2.12 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## User Story A1 — Opt-in asking, Go-enforced, Research and Plan only

> As the owner, I want a run I have tagged to stop and ask me in a comment instead of guessing, so
> that a wrong assumption does not propagate into a brief, then a plan, then code.

Replaces the unconditional headless gag with a two-way branch on an opt-in tag that **Go parses and
enforces** (Q3 = option 1), available to **Research and Plan only** (Q6). No new state.

**Size: L. Fully unblocked as of rev 4 (N4 = `lf: ask`).**

**Sized honestly: the *diff* is small, the *risk* is not.** This is the only story that can change
the behaviour of untagged runs, and it carries three sharp defects (F-9, F-10, F-16).

### The tag — settled at N4

**The literal is `lf: ask`.** Line-anchored, in the triggering comment body.

**Do not confuse it with the self-marker.** `<!-- lf:` (`turn/classify.go:38`) identifies
litefactory's *own* comments and is line-anchored by Story S0. `lf: ask` grants *permission* and is
line-anchored by this story. **They are different literals for different purposes and neither matches
the other.** State this in the tag parser's doc comment — the resemblance invites a future
"simplification" that merges them, and merging them would let any litefactory comment grant
permission.

### Field names — one set, declared here

**`MayAsk` and `PreviousMayAsk`.** This **supersedes** `feature-spec.md:47-48`
(`MayAsk`/`AnswersQuestion`) and rev 1's `AskingAllowed`/`PreviousAsked`.

| Field | Meaning | Source | Derivable? |
|---|---|---|---|
| `MayAsk bool` | may **this** run ask? | tag on **this** run's trigger comment **AND** mode ∈ {research, plan} | **yes** |
| `PreviousMayAsk bool` | did the **previous** run have permission? | re-parse the tag from the previous run's trigger comment | **yes** — see below |
| ~~`PreviousAsked` / `AnswersQuestion`~~ | did the previous run actually ask? | requires `needs_input` | **no — and A2 is deleted**, so this is gone for good |

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
  run **cannot** ask. Rev 2 recommended presence-only parsing; the owner chose literal parsing. Not
  re-argued.
- **Q6 = Research and Plan may ask; Implement never.** Permission is a **conjunction**:
  `MayAsk = tagPresent && mode ∈ {research, plan}`. The mode is already on `PromptInput` and Go
  already branches on it (`const InferMode = "infer"`, `prompt.go:422`) — the mode half costs nothing.
- **The one place hard enforcement is soft, and it is structural.** When the mode is **`infer`**, Go
  does not know whether the run will be Research, Plan or Implement — the agent classifies it from
  `CLAUDE.md` after the prompt is built. **The rule is carried in prompt text for that path**
  (*"if this run resolves to Implement mode, do not ask; proceed headless"*) while the tag gate stays
  machine-enforced. *(Flag if you want infer runs denied permission outright — stricter, cheaper, and
  costs you the ability to tag an unclassified comment.)*
- **There are TWO headless paragraphs, not one, and they must change together.** The gag at
  `harness/prompt.go:396-401` and the feature-slug paragraph at `:388-390` — *"It is stated here
  because this run is headless and no one can answer a question."*
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
- **Q9 = (b): the resume header's answer framing is hedged.** It asserts nothing Go does not know.
- **Q8 = agent-level. There is no Go cap and no timeout.**
- **Every asking comment must carry a standing line inviting *"move to the next phase."*** Per *One
  Run, One Mode*, that is exactly the human comment that opens the next mode. It belongs in the
  ask-comment template, in `research-orchestrator.md` and `plan-orchestrator.md`.
- **`KindFreeText` carries the whole comment body as the instruction.** The tag line must be stripped
  or it becomes part of what the agent is asked to do.
- **F-15 — the free-text path is not the only path.** `ParseCommand` (`command.go:152`) also yields
  `KindSpike` with `Text = TrimSpace(rest)`; a tag inside `/spike lf: ask do X` lands mid-`Text` and
  must be stripped there too.
- **F-16 — the tag-only comment is an unhandled failure.** A body containing nothing but `lf: ask`
  goes `firstToken` → `KindFreeText` → strip → empty `Instruction` → `BuildPrompt` returns
  `ErrInvalidPrompt`, and the run dies with a validation error the owner will not understand. Treat
  it as `KindNone` — no run. AC-A1.9.
- **F-22 — match line-anchored, never with `Contains`.** **This plan itself now contains the literal
  `lf: ask` many times.** An issue body or comment quoting it under a `Contains` match would turn
  permission on. Line-anchor, and ensure the tag never reaches `Instruction` un-stripped —
  `Instruction` is documented as trusted and is the one field with **no size cap**.
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
- **Depends on S0.**
- **Existing test coverage, named precisely (F-6).** No `testdata/` or `.golden` in `internal/harness`.
  There are:
  - **`TestSpikePromptIsUnchanged` (`harness/prompt_test.go:359`)** — one full-head literal
    comparison. **Must pass byte-for-byte unmodified.**
  - **`TestBuildPromptStatesTheFeatureSlugAndForbidsQuestions` (`prompt_test.go:76`)** — asserts
    **both** facts in one test. A1 splits those facts apart, so **this test must itself be split**
    (F-11). It is not possible to claim "both existing tests untouched" — one changes by design.
- **F-7 — `writeResumeHeader` is entirely uncovered.** Zero tests set `Resume: true` or
  `PreviousMode`. **A characterization test must exist before the function is touched** — T-A1.8.
- **The tag parser is the load-bearing new function in this plan.** Same code reads `MayAsk`, derives
  `PreviousMayAsk`, and gates two modes. Own AC (AC-A1.4).
- Risk: **ungated asking is R-3.** **A1 must not ship without T-A1.1.**
- Platform: no new package, no HTTP layer, no DB access, no migration, no new dependency, no
  concurrency. Synchronous string assembly.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-A1.1 | Tag parser for the literal **`lf: ask`**, matched **line-anchored**, stripped from the instruction text, **never returning an error**. Doc comment records that it is distinct from the `<!-- lf:` self-marker | Go | — | M |
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
| T-A1.18 | Table-driven tag-parsing tests: present, absent, malformed, mid-body, tag-only, quoted/indented, **third-party comment**, **replayed history**, **an issue body quoting this plan**, **tagged Implement run** | Test | T-A1.1, T-A1.3 | M |
| T-A1.19 | `turn`-level test: a comment arriving after a succeeded run in the same segment produces `Resume: true` / `RunPath = resume` | Test | T-A1.9 | S |
| T-A1.20 | Update `prompt.go`'s package doc — it states the design rationale this story falsifies | Docs | T-A1.4 | S |
| T-A1.21 | Note in `research-orchestrator.md` that its log-and-continue rule survives; asking is a higher tier above it, not a replacement | Docs | T-A1.4 | S |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-A1.1 | An **untagged** run's cold prompt is byte-for-byte identical to today's. `TestSpikePromptIsUnchanged` (`prompt_test.go:359`) passes **unmodified**. |
| AC-A1.2 | A **tagged** Research or Plan run's cold prompt contains neither *"do not ask clarifying questions"* nor *"no one can answer a question"*. |
| AC-A1.3 | **A tagged run's cold prompt still states the feature slug.** Deleting `prompt.go:388-390` outright is a failing outcome. |
| AC-A1.4 | The parser matches the literal **`lf: ask`**, **line-anchored**, returns **no error** for any input, and treats a malformed tag as absent. A body merely *quoting* the tag inside a quoted or indented line grants no permission. |
| AC-A1.4a | **The tag and the `<!-- lf:` self-marker do not match each other**, in either direction. A test asserts both. |
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
| AC-A1.16 | The owner's reply resumes the same Claude conversation via the existing resume path — asserted at `turn` level. |
| AC-A1.17 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## User Story E1 — Plan mode's output becomes `plan_brief.md` *(new in rev 4 — see N3)*

> As an implementing agent, I want one short shared-context document plus per-story sub-issues, so
> that I read the decisions that apply to every story once and the story I am building in its own
> place.

Retires `IMPLEMENTATION_PLAN.md` and replaces it with `plan_brief.md`. **Split out of C1 because it is
an artifact-and-process change affecting thirteen files, and burying it inside a story about creating
sub-issues would hide it from review.**

**Size: L. Unblocked as work — but see the execution scope below, and the hard coupling to C1.**

> ### ⚠ EXECUTION SCOPE — SPLIT: MOSTLY OWNER-EXECUTED *(rev 6)*
>
> **Of E1's 13 live files, the majority sit under `.claude/agents/**` and `.claude/skills/**` and are
> not writable by a run** — same measured boundary as B1 above, same reason, same non-remedy.
> Run 0010 refused `Edit` on `.claude/agents/research-orchestrator.md`,
> `.claude/agents/plan-orchestrator.md` and `.claude/AGENT_ROSTER.md`.
>
> **The split is clean and was proven by the same run:**
>
> | Path | Writable by a run? | Executed by |
> |---|---|---|
> | `CLAUDE.md` (repo root) | yes — **already edited, uncommitted on this branch** | agent |
> | `initialplan.md` (repo root) | yes — **already edited, uncommitted on this branch** | agent |
> | `.claude/agents/**` | **no — refused** | **owner, by hand** |
> | `.claude/skills/**` | **no — refused** | **owner, by hand** |
>
> **The partial work already on disk is not the story.** Both repo-root files were edited in run
> 0010 and remain uncommitted. They are correct as far as they go — the new *"Plan mode's output"*
> section does **not** flip the Implement gate, so **AC-E1.9 holds** — but they are a small minority
> of E1 and do **not** make it deliverable.
>
> **Do not dispatch E1 whole to an implementation agent.** Either the owner executes it, or it is
> split so that only the two repo-root files are dispatched and the `.claude/**` edits are handed
> back. **AC-E1.2's carve-out (rev 6, #6) is still open and must be settled before E1 can pass its
> own acceptance check**, whoever executes it.

> **Hard coupling, and it is the sequencing risk in this plan.** E1 alone leaves Plan mode producing
> shared context with **nowhere for the stories to live**, because only C1 creates sub-issues.
> **The gate flip and the deletion of `IMPLEMENTATION_PLAN.md` must not merge before C1 ships.**
> Rev 4 handles this as a two-shipment transition: **E1 introduces `plan_brief.md` alongside the
> existing file and leaves the gate alone; C1's final task flips the gate and retires the file**
> (T-C1.13). **This is a transition mechanism, not a return to *mirrored* — N3's end state is
> replacement, and T-C1.13 is what reaches it.**

### Architecture Notes

- **Zero Go. Verified, and it is the most useful fact in this story.** A grep across the repo for
  `RESEARCH_BRIEF|IMPLEMENTATION_PLAN|plan_brief` in `**/*.go` returns **no files**. Artifact-based
  state inference is **entirely unimplemented in Go** — the mode gates are enforced by agents reading
  `CLAUDE.md`, not by code. **So this story breaks no running code**; it changes documents that change
  agent behaviour. That makes it cheap to execute and impossible to test automatically.
- **It is 13 files, not 4.** The owner's answer named `CLAUDE.md`, `plan-orchestrator.md`,
  `implement-orchestrator.md` and `go-architect.md`. A disk grep for `IMPLEMENTATION_PLAN` finds
  **123 occurrences across 34 files**; excluding `.aiteam/` session archives, **13 live files** name
  it:

  | File | Occurrences (disk) |
  |---|---|
  | `.claude/agents/implement-orchestrator.md` | 8 |
  | `.claude/agents/go-architect.md` | 6 |
  | `.claude/agents/go-code-reviewer.md` | 5 |
  | `.claude/agents/go-implementation-engineer.md` | 5 |
  | `initialplan.md` | 5 |
  | `.claude/agents/plan-orchestrator.md` | 4 |
  | `CLAUDE.md` | 3 |
  | `.claude/agents/go-unit-test-engineer.md` | 3 |
  | `.claude/agents/proxy-product-owner.md` | 2 |
  | `.claude/agents/go-integration-test-engineer.md` | 2 |
  | `.claude/AGENT_ROSTER.md` | 1 |
  | `.claude/skills/shared/task-decomposer.md` | 1 |
  | `.claude/agents/app-context-updater.md` | 1 |

- **`.aiteam/` archives are NOT edited.** Twenty-one of the 34 files are past sessions' artifacts.
  They are the historical record and rewriting them would falsify it. **AC-E1.7.**
- **Counts are from disk and disk is stale.** **T-E1.0 re-runs the grep against `71d3bea` first.** The
  list is a lower bound.
- **Three files matter most, and they matter for different reasons:**
  - **`CLAUDE.md`** — three places: the **Implement Mode Gate**, the **Artifacts** section, and the
    **`MANIFEST.md` scaffold template**. The gate is the load-bearing one.
  - **`plan-orchestrator.md`** — its Output section, its `IMPLEMENTATION_PLAN.md` format block, and
    its Exit steps are all rewritten. **This is the largest single rewrite**, because the format block
    is the template being replaced.
  - **`implement-orchestrator.md`** (8 occurrences) — it must learn to read `plan_brief.md` **and then
    the sub-issues**, which is a genuinely new step, not a rename.
- **Everything downstream of the orchestrator reads per-story content, so it reads the sub-issue.**
  `go-unit-test-engineer` derives tests from a story's acceptance criteria; `go-integration-test-engineer`
  from the same; `go-implementation-engineer` from a story's tasks; `go-code-reviewer` from a story's
  architectural hints. **Under N3, all of that content moved.** These are not filename swaps — each
  agent needs a source. **The simplest correct answer, and the one rev 4 plans: the orchestrator
  fetches the sub-issue and passes its content in the invocation**, so no downstream agent needs forge
  access. **AC-E1.4.**
- **The gate stays a local file check (N3's recommendation, taken).** `plan_brief.md` is the gate;
  sub-issues are discovered from its **Story Index**. No network call in mode entry, no failure on
  rate limits or offline worktrees, and the gate keys on the versioned artifact rather than a mutable
  forge object.
- **The anti-duplication rule is the design's main failure mode.** `plan_brief.md` carries only what
  **two or more stories** need. Anything true of exactly one story lives in its sub-issue and nowhere
  else. The Story Index carries **identity and location only** — never content. **AC-E1.5.**
- **`initialplan.md:243`** — *"State is inferred from artifacts, not self-reported"* — is **not
  falsified** by this change and must not be deleted. `plan_brief.md` is still an artifact and the
  gate is still artifact-inferred. **Amend it to name the new artifact; do not remove the principle.**
- **What is lost, stated plainly.** A sub-issue is editable by anyone with write access, so **the
  per-story plan of record becomes mutable and unversioned** — no diff, no blame, no review. That is a
  real cost of N3 and the owner accepted it. **`plan_brief.md` being in git is what limits the
  blast radius**, which is a second reason it is the gate.
- Platform: **no Go, no build, no tests, no dependency, no migration.** Markdown only.

### `plan_brief.md` — the contract

*Concrete enough to implement. Everything here is cross-story by construction; nothing here is
per-story.*

| Section | Contains | Never contains |
|---|---|---|
| **Header** | Feature, branch, date, parent issue, **citation base commit**, link to `RESEARCH_BRIEF.md` | — |
| **Shared decisions** | Decisions binding on two or more stories: field-name choices, literals (e.g. `lf: ask`), the SDK and its pinned version, package-placement rules, register/audience rules | A decision used by one story |
| **Workflow** | Shipping order, the story dependency graph, hard couplings, what ships together and why | Task-level ordering inside a story |
| **Cross-story constraints** | Platform constraints, security invariants that must survive every story, test invariants that must stay green, ring-fenced areas (e.g. *code review is untouched*) | Story-specific architecture notes |
| **Story Index** | One row per story: **ID · title · size · depends-on · sub-issue link**. **This is the discovery mechanism.** | Tasks, acceptance criteria, architecture notes — **any content at all** |
| **Out of scope** | Global exclusions | Exclusions belonging to one story |
| **Provenance** | Citation base, known-stale areas, re-pinning requirements | — |

**The rule, stated once so it can be enforced: if it is true of exactly one story, it does not belong
in `plan_brief.md`.** Duplication is how the two artifacts drift, and drift is how a reader stops
knowing which one is authoritative.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-E1.0 | **Re-run the `IMPLEMENTATION_PLAN` grep against `71d3bea`** and confirm the file inventory before editing anything. Disk counts are a lower bound | Chore | — | S |
| T-E1.1 | Write the `plan_brief.md` contract — the section table above — into `plan-orchestrator.md`, including the **anti-duplication rule** | Docs | T-E1.0 | M |
| T-E1.2 | Rewrite `plan-orchestrator.md`'s Output section and **replace its `IMPLEMENTATION_PLAN.md` format block** with the `plan_brief.md` format | Docs | T-E1.1 | M |
| T-E1.3 | Rewrite `plan-orchestrator.md`'s Exit steps: write `plan_brief.md`, create the sub-issues *(C1)*, populate the Story Index with their links, stamp `MANIFEST.md` | Docs | T-E1.2 | M |
| T-E1.4 | `implement-orchestrator.md`: read `plan_brief.md`, then **fetch each story's sub-issue from the Story Index** and pass its content to the downstream agent. 8 occurrences | Docs | T-E1.1 | M |
| T-E1.5 | `go-architect.md`: read `plan_brief.md` plus the sub-issues. 6 occurrences | Docs | T-E1.1 | S |
| T-E1.6 | Update the four downstream agents that read per-story content — `go-implementation-engineer` (5), `go-code-reviewer` (5), `go-unit-test-engineer` (3), `go-integration-test-engineer` (2) — to take story content **from the invocation**, not from a file they open | Docs | T-E1.4 | M |
| T-E1.7 | Update `proxy-product-owner.md` (2), `app-context-updater.md` (1), `.claude/AGENT_ROSTER.md` (1), `.claude/skills/shared/task-decomposer.md` (1) | Docs | T-E1.1 | S |
| T-E1.8 | `CLAUDE.md`: **Artifacts** section and the **`MANIFEST.md` scaffold template**. **Not the gate yet** — that is T-C1.13 | Docs | T-E1.1 | S |
| T-E1.9 | `initialplan.md` (5): amend to name `plan_brief.md`. **Keep the principle at `:243`** — state is still inferred from an artifact | Docs | T-E1.1 | S |
| T-E1.10 | State explicitly, in `plan-orchestrator.md`, that **`.aiteam/` session archives are never rewritten** when an artifact format changes | Docs | T-E1.1 | S |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-E1.1 | `plan-orchestrator.md` specifies `plan_brief.md` — every section in the contract table — and **no longer contains an `IMPLEMENTATION_PLAN.md` format block**. |
| AC-E1.2 | **No live file outside `.aiteam/` refers to `IMPLEMENTATION_PLAN.md` as a current artifact** after E1 + C1. A grep is the check. |
| AC-E1.3 | `plan_brief.md` **is the Implement gate**, and the gate is a **local file-existence check**. **No mode gate makes a network call** — a forge-side gate is a failing outcome. |
| AC-E1.4 | Every downstream agent that needs per-story content **receives it in its invocation**. No downstream agent requires forge access to do its job. |
| AC-E1.5 | **`plan_brief.md` contains no per-story tasks, acceptance criteria or architecture notes**, and its Story Index carries identity and location only. Duplicated per-story content is a failing outcome. |
| AC-E1.6 | `initialplan.md:243`'s principle — state inferred from artifacts, not self-reported — **survives**, naming the new artifact. Deleting it is a failing outcome. |
| AC-E1.7 | **No file under `.aiteam/` is rewritten by this story.** Session archives are the historical record. |
| AC-E1.8 | The file inventory was re-confirmed against `71d3bea` before editing (T-E1.0). |
| AC-E1.9 | **`CLAUDE.md`'s Implement gate is unchanged by E1 alone.** Flipping it before C1 can create sub-issues is a failing outcome — see T-C1.13. |
| AC-E1.10 | No Go file is modified by this story. |

---

## User Story D1 — Adopt a GitHub SDK *(new in rev 4 — see N2)*

> As a maintainer, I want litefactory's GitHub client built on a maintained SDK, so that new endpoints
> — starting with sub-issues — do not each require hand-rolling request shapes we cannot verify.

Implements the owner's N2 decision. **Split out of C1 because it reverses a documented policy, retires
three deliberate guard tests and touches the one file in the repo whose job is keeping a credential
out of logs. It must be reviewed as a client-and-security change, not as plumbing for sub-issues.**

**Size: XL. Fully unblocked as of rev 5 (N6 = `github.com/google/go-github`, N7 = (a) full
migration).** The rev-4 conditional *"XL if migration / L if hybrid"* is gone — **N7 chose full
migration, so D1 is XL.**

**The SDK is `github.com/google/go-github`. Its version is not named in this plan and must not be
guessed:** pinning it is **T-D1.1, the first action of this story**, taken by someone with live access
to the module proxy and GitHub's documentation. **AC-D1.2 then confirms that pinned version actually
supports sub-issues**, with the generic-request escape hatch as the fallback if it does not.

> **This story exists because a tripwire fired and the owner answered it.**
> `TestClientMethodSetIsTheSevenEndpoints` says an eighth method is *"a reason to re-open the SDK
> question, not to add a method quietly."* **The question was re-opened and answered: use an SDK.**
> Every guard retired here is retired **on the record, with the reasoning in the commit** — which is
> the outcome the tripwire was planted to produce.

### Architecture Notes

- **This overrides `internal/github/doc.go`'s `# No SDK` section**, which reads: *"Six endpoints do not
  justify a dependency. Everything here is net/http plus the Link-header helper in pagination.go. The
  rule for revisiting that: if a third pagination special case appears, reach for a library."*
  **Rewrite it, and record who decided and why.** Critically: **the documented trigger did not fire.**
  There was no third pagination special case. **The trigger was sub-issues** — a new capability the
  package cannot express — **and an owner decision.** A future reader must be able to tell those
  apart. *(While rewriting: the prose says "six endpoints", the test says seven. Reconcile.)*
- **`TestClientMethodSetIsTheSevenEndpoints` (`surface_test.go:56`) is retired or reworked
  deliberately**, and the change **references the test's own message** so the record shows the
  tripwire worked rather than being stepped over. **If any pinned-surface guard survives in a new
  form, it must still fail when the surface grows silently** — the value was never the number seven,
  it was that growth is noticed.
- **DO NOT let SDK adoption smuggle in the forge abstraction.** `TestNoForgeAbstraction`
  (`surface_test.go:92`) forbids declaring an interface in any non-test file in `internal/github`, and
  `doc.go` calls a `forge.go` at M0 *"a review blocker"*. **An SDK does not require an interface — a
  wrapper is where one gets added by reflex.** A **concrete wrapper struct is fine**; an interface in a
  non-test file is not. **`TestNoForgeAbstraction` stays green through this story**, and a diff that
  turns it red is a failing outcome. LF-18's position — extract the abstraction later from two real
  implementations, do not guess it now — is unchanged by adopting a dependency.
- **THE SECURITY ITEM. `TestRevealIsCalledExactlyOnce` (`surface_test.go:137`) will break, and it is
  what keeps the token out of logs.** It fails the build if `.Reveal()` appears on any line not
  containing `Authorization`, or more than once in the package. An SDK takes credentials through a
  different path — typically an `oauth2` token source or a custom `http.Client` installed at
  construction — so the single `Authorization`-header call site disappears and the guard's premise
  with it.
  **Requirement: re-establish an equivalent guarantee. Do not delete the test.** The new guard must
  still fail the build when the credential is revealed anywhere other than the one place that hands it
  to the transport. **Own task (T-D1.6), own AC (AC-D1.6).** *(Design hint, not a decision: the natural
  shape is a single construction site that reveals once into the SDK's credential type, with the
  line-based assertion re-aimed at that call rather than at `Authorization`.)*
- **The one genuine unknown: does the pinned `go-github` version support sub-issues?** GitHub's
  sub-issue API is recent and SDK coverage of recent endpoints lags. **Web tools are disabled
  session-wide, so neither the current major nor its sub-issue coverage could be checked while
  planning — which is precisely why no version literal appears in this plan.** **Pinning the version is
  T-D1.1, the first action of this story**, and it requires live access to the module proxy and
  GitHub's documentation. It is **AC-D1.2 — confirmed before implementation, never assumed.** If it is
  unsupported, the fallback is the SDK's generic-request escape hatch, **and that must be confirmed
  too**.
- **The upside, and it is why this makes C1 safer.** The sub-issue facts in `RESEARCH_BRIEF.md` are
  `[W]` — second-hand and unverified, from a source that recorded six of its predecessor's external
  GitHub claims as wrong. **An SDK encapsulates exactly what that block is untrusted about**: endpoint
  paths, payload field names, pagination shape, and the **`sub_issue_id`-versus-issue-number trap**,
  which is the easiest way to get sub-issues silently wrong. **N2 converts a class of
  unverifiable-here facts into a dependency's tested surface**, materially reducing C1's risk.
- **The blast radius is settled: N7 = (a), full migration.** All seven existing endpoints move to the
  SDK. **The deciding argument is the credential guard, not tidiness:** a hybrid would leave **two HTTP
  stacks, two error taxonomies and two credential paths**, and the third of those makes **AC-D1.6**
  harder to satisfy and easier to erode. **One credential path is one thing to guard.** The hybrid is
  recorded in `Out of Scope` as considered and rejected.
- **Error taxonomy is the main migration hazard.** Callers depend on today's error shapes; the SDK
  returns its own. **Map at the package boundary so `internal/github`'s error contract to its callers
  is unchanged**, or the migration leaks into every call site. **T-D1.5.**
- **`pagination.go`'s Link-header helper comes out.** It is named in the policy being rewritten, and
  under full migration the SDK owns pagination. **Remove it and say so in `doc.go`** — a dead helper
  left behind is how the next reader concludes the package still hand-rolls.
- Platform: **this is the only story that adds a dependency.** New `go.mod` entry, pinned version,
  vendoring/licence check per project convention. **Review it as a dependency decision.**

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-D1.0 | **Re-pin every `internal/github` citation against `71d3bea`** — `doc.go`, `surface_test.go:56`, `:92`, `:137`, `pagination.go` — before editing. They are owner-verified but the file is on the 52-file stale list | Chore | — | S |
| T-D1.1 | **Pin the SDK version. The first action of D1.** The library is settled — **`github.com/google/go-github`** (N6). The **version is not**, and must not be invented: pin an exact version using **live access to the module proxy and GitHub's docs**, then record the literal and the reasoning in `plan_brief.md` and `doc.go`. **No other D1 task starts before this one** | Design | T-D1.0 | S |
| T-D1.2 | **Confirm the pinned version supports sub-issues** — parent linkage, `sub_issue_id` semantics, and the generic-request fallback if it does not. Record what was checked, against what source, and when. **No code before this** | Design | T-D1.1 | M |
| T-D1.3 | **Record the migration decision.** It is **already made** — N7 = (a), **full migration**. This task writes it down with its reasoning: **one credential path is one thing to guard (AC-D1.6)**, not tidiness. **Nothing here is left to decide** | Design | T-D1.1 | S |
| T-D1.4 | Add the dependency: `go.mod`, pinned version, licence/vendoring check per project convention | Go | T-D1.3 | S |
| T-D1.5 | **Error-taxonomy mapping at the package boundary**, so `internal/github`'s error contract to its callers is unchanged by the SDK | Go | T-D1.4 | M |
| T-D1.6 | **Re-establish the credential guarantee.** Route the token through the SDK's credential path at a **single** construction site, and **rework `TestRevealIsCalledExactlyOnce` to guard that site.** The test is reworked, **never deleted** | Go + Test | T-D1.4 | **M–L** |
| T-D1.7 | **Migrate the seven existing endpoints to the SDK, call site by call site, and remove `pagination.go`'s Link-header helper.** Unconditional — N7 = (a) | Go | T-D1.5, T-D1.6 | **L** |
| T-D1.8 | **Rewrite `doc.go`'s `# No SDK` section**: who decided, why, and that **the documented trigger (a third pagination special case) did not fire** — the trigger was sub-issues plus an owner decision. Reconcile "six" versus "seven" endpoints | Docs | T-D1.3 | S |
| T-D1.9 | **Retire or rework `TestClientMethodSetIsTheSevenEndpoints` deliberately**, referencing its own message in the commit. If a surface guard survives, it must still fail on silent growth | Test | T-D1.7 | M |
| T-D1.10 | **Assert `TestNoForgeAbstraction` still passes.** No interface enters a non-test file in `internal/github` | Test | T-D1.7 | S |
| T-D1.11 | Tests: existing endpoint behaviour is unchanged after migration — same errors, same pagination results, same redaction — against a faked transport | Test | T-D1.7 | **L** |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-D1.1 | The SDK is **`github.com/google/go-github`** (settled at N6), and its **exact pinned version** — **chosen at implementation time, never guessed from this plan** — is recorded in `plan_brief.md` and in `doc.go` with its reasoning. `go.mod` pins that exact version. **"Latest" is a failing outcome, and so is a version copied out of a document written without live access.** |
| AC-D1.2 | **The pinned version's sub-issue support was confirmed against a live source before implementation** — parent linkage and `sub_issue_id` semantics — and the check is recorded with its date and source. **If unsupported, the SDK's generic-request escape hatch was confirmed as the fallback, also against a live source.** **A `[W]`-sourced claim is not sufficient evidence.** |
| AC-D1.3 | `doc.go`'s `# No SDK` section is rewritten and **records that the documented trigger did not fire** — the reversal was sub-issues plus an owner decision, not a third pagination special case. |
| AC-D1.4 | `TestClientMethodSetIsTheSevenEndpoints` was changed **deliberately**, the commit references the test's own message, and any surviving surface guard **still fails on silent growth**. |
| AC-D1.5 | **`TestNoForgeAbstraction` is green.** No interface is declared in any non-test file in `internal/github`. **SDK adoption did not introduce the forge abstraction.** |
| AC-D1.6 | **The credential guarantee is re-established, not removed.** The token is revealed at **exactly one** place, that place is the SDK's credential path, and **a build-failing test enforces it**. **Deleting `TestRevealIsCalledExactlyOnce` without an equivalent replacement is a failing outcome — this is the guard keeping the token out of logs.** |
| AC-D1.7 | `internal/github`'s **error contract to its callers is unchanged**. No caller outside the package changes to accommodate SDK error types. |
| AC-D1.8 | **All seven existing endpoints use the SDK, and `pagination.go`'s Link-header helper is gone.** One HTTP stack, one error taxonomy, **one credential path**. A surviving hand-rolled `net/http` call site in `internal/github` is a failing outcome, and so is a dead Link-header helper left behind. The decision and its reasoning are recorded (T-D1.3). |
| AC-D1.9 | Existing endpoint behaviour is unchanged: same results, same errors, same pagination, same redaction. |
| AC-D1.10 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## User Story C1 — The plan ships as one GitHub sub-issue per user story

> As the owner, I want each user story to arrive as its own GitHub sub-issue, so that I can track,
> comment on and close them independently instead of scrolling one flat plan document.

**Confirmed deliberately in scope at N1** — *"yes, i changed my mind and we add it here, for
completness!"*

**Size: L. Depends on D1 (the client can create nothing today) and E1 (the artifact shape).**
**Re-scoped in rev 4:** with D1 owning the SDK adoption and E1 owning the artifact change, C1 is
creation, linkage, idempotency and partial-failure recovery — plus the final gate flip.

### Architecture Notes

- **`internal/github` cannot create anything today.** Every verb across the call sites is GET or POST
  for reads and comments; there is **no PATCH, PUT or DELETE anywhere.** C1 needs **create issue** and
  **attach as sub-issue**. **Both are built on the SDK adopted in D1** — this story adds no
  hand-rolled HTTP.
- **The sub-issue API facts in `RESEARCH_BRIEF.md` are `[W]` — second-hand and unverified.** Web tools
  were disabled for the entire research session, and the brief's own source records six of *its*
  predecessor's external GitHub claims as wrong, two load-bearing. **The contract is confirmed against
  live documentation before any code.** *(D1 reduces this exposure but does not eliminate it: the SDK
  encapsulates the shapes, not the semantics — **`sub_issue_id` is an issue **ID**, not an issue
  **number**, and getting that wrong links the wrong issue silently.** Confirm it.)*
- **No backoff exists for bulk creation.** A plan with eight stories is eight creations plus eight
  attachments against a rate-limited API, in a loop. A partial failure halfway leaves a half-built plan
  and no record of where it stopped. **Idempotency and resumability are requirements** — the natural
  mechanism is the existing `<!-- lf:` marker convention, checking for an already-created sub-issue
  before creating another. *(If D1's SDK provides retry/backoff, use it rather than hand-rolling —
  that is part of what the dependency is being bought for.)*
- **The Story Index in `plan_brief.md` is written from this story's output.** Creation produces the
  sub-issue links; the links populate the index; **the index is how everything downstream finds the
  stories.** An index row without a link is a broken plan, not a cosmetic gap.
- **Sub-issue bodies are agent-authored text going to the forge** — same rules as B2: route through
  `internal/report`, `sanitize`/`fenceFor`, redaction residual assertion.
- **Audience.** Per Q5's split and B1's four rules, a sub-issue body is read by business users: business
  register, names its problems, links the technical detail. **Stated once in `plan_brief.md`, per E1's
  anti-duplication rule, not restated per story.**
- **Sub-issues are a second stream of marker-bearing comments the owner will reply to. C1 therefore
  depends on S0** as much as B2 does.
- **The gate flip lands here, last.** T-C1.13 changes `CLAUDE.md`'s Implement gate to `plan_brief.md`
  and retires `IMPLEMENTATION_PLAN.md`. **It must not merge until creation actually works** — before
  that, the gate would point at an artifact whose stories have nowhere to live.
- Platform: **the only story that performs a network write beyond commenting.** Review as a client
  change.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-C1.1 | **Confirm the GitHub sub-issue API contract against live documentation** — endpoint, payload, parent linkage, **`sub_issue_id` vs issue number**, limits, GA-or-preview. Record what was checked and when. **No code before this** | Design | D1 (T-D1.2) | M |
| T-C1.3 | Create-issue capability, via the SDK | Go | D1, T-C1.1 | M |
| T-C1.4 | Attach-as-sub-issue capability, via the SDK | Go | T-C1.3 | M |
| T-C1.6 | Idempotent creation: a marker-based check so a re-run does not duplicate sub-issues after a partial failure | Go | T-C1.4 | M |
| T-C1.7 | Backoff and partial-failure handling for bulk creation; a run that fails halfway reports what it created. **Use the SDK's retry support if it has one** | Go | T-C1.6 | M |
| T-C1.8 | `plan-orchestrator.md`: emit one sub-issue per user story in the business register, each linking the technical detail, **and write its link into `plan_brief.md`'s Story Index** | Docs | T-C1.1, E1 (T-E1.3) | M |
| T-C1.11 | Route sub-issue bodies through `internal/report`'s `sanitize`/`fenceFor` and redaction | Go | T-C1.3 | S |
| T-C1.12 | Tests: creation, attachment, idempotent re-run, partial failure, rate-limit backoff — all against a faked client | Test | T-C1.7 | L |
| T-C1.13 | **The gate flip, last.** Change `CLAUDE.md`'s Implement gate to require `$session/plan_brief.md`, and retire `IMPLEMENTATION_PLAN.md`. **Must not merge before creation works** | Docs | T-C1.12, E1 | S |

*(Rev-3 task IDs T-C1.2, T-C1.5, T-C1.9 and T-C1.10 moved to Stories D1 and E1. IDs are not reused.)*

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-C1.1 | **The sub-issue API contract was verified against live GitHub documentation before implementation**, and the verification is recorded with its date. **A `[W]`-sourced claim is not sufficient evidence for any assertion in this story.** |
| AC-C1.1a | **`sub_issue_id` semantics were confirmed specifically** — issue ID versus issue number. Linking the wrong issue is a silent failure, so this is called out separately. |
| AC-C1.3 | Each user story in a plan produces exactly one sub-issue linked to the parent issue. |
| AC-C1.3a | **Every Story Index row in `plan_brief.md` carries a working sub-issue link.** A row without one is a failing outcome. |
| AC-C1.4 | Re-running Plan mode on the same session creates **no duplicate** sub-issues. |
| AC-C1.5 | A failure partway through bulk creation reports which sub-issues were created; a subsequent run completes the rest rather than starting over. |
| AC-C1.6 | Sub-issue bodies are in the business register, name the problems, and link the technical detail. |
| AC-C1.7 | **`CLAUDE.md`'s Implement gate requires `plan_brief.md`**, and `IMPLEMENTATION_PLAN.md` is retired. **The gate is a local file check — it makes no network call.** |
| AC-C1.8 | **The gate flip did not merge before sub-issue creation worked.** A window in which the gate names an artifact whose stories have nowhere to live is a failing outcome. |
| AC-C1.9 | Sub-issue bodies pass the redaction residual assertion and cannot break report structure. |
| AC-C1.10 | `go test -race -cover ./...` green; `gofmt` clean; `go vet` clean. |

---

## Shipping order

**Re-sequenced in rev 4.** N3 changed what must land together, and N2 inserted a dependency in front
of C1.

1. **S0** — the quote-reply fix. **First**, because A1, B2 and C1 all depend on the owner being able to
   reply to a marker-bearing comment. **Fully unblocked.** *(M)*
2. **B1** — no Go, no dependencies. **Fully unblocked.** *(S)*
3. **B2** — delivery is free; the link is not. **Fully unblocked as of N5 = (a).** The delivery half
   (T-B2.1) can still ship on its own if you want the summary landing before the link works. *(L)*
4. **A1** — after the above, because it is the only story that can change the behaviour of untagged
   runs, and it carries F-9, F-10 and F-16. **Fully unblocked as of N4.** *(L)*
5. **E1** — `plan_brief.md`. **Fully unblocked**, zero Go, and it can be written in parallel with the
   Go stories. **But it must not flip the gate** — that is C1's last task. *(L)*
6. **D1** — the SDK. **Before C1**, because C1 has nothing to build on until the client can create.
   **Fully unblocked as of rev 5** (N6 = `github.com/google/go-github`, N7 = (a) full migration).
   **Its first action is T-D1.1, pinning the version** — which needs live access to the module proxy
   and GitHub's docs, and is the one thing this plan deliberately does not supply. *(XL)*
7. **C1** — sub-issue creation, then **the gate flip, last**. *(L)*

**Re-checked at rev 5 and unchanged.** D1 becoming XL and unblocked does not move it: it still sits
**after E1** (which is zero-Go and can run in parallel with everything) and **before C1** (which cannot
start until the client can create). **The D1 → C1 dependency stands** — C1's `T-C1.1` depends on
`T-D1.2`, and `T-C1.3`/`T-C1.4` build on the SDK D1 adopts. **The E1 + C1 hard coupling stands too**,
with **T-C1.13 still the join and still the last task in the plan.** Nothing in the sequence needed
adjusting.

**The one hard coupling: E1 and C1 must complete together.** E1 alone leaves Plan mode producing
shared context with nowhere for stories to live; C1 without E1 creates sub-issues nothing reads.
**T-C1.13 is the join** — it is deliberately the last task in the plan.

**Two independent tracks, if you want parallelism.** S0 → B2 → A1 is Go work in `internal/turn`,
`internal/report` and `internal/harness`. E1 → (D1) → C1 is the plan-shape-and-client track. **B1 and
E1 touch no Go at all** and can proceed alongside either.

**Hidden coupling worth stating once:** the tag parser (T-A1.1) is the same code that derives
`PreviousMayAsk` (T-A1.6) and gates both Research and Plan. It is the one genuinely load-bearing new
function in this plan, which is why AC-A1.4 exists as a criterion of its own.

---

## Out of Scope

- **Story A2 — parking as a first-class state (`needs_input`).** **Deleted, not deferred** (Q11 = A1
  only). Everything it contained is gone: the `needs_input` run status and session state,
  `terminalRunStatus` changes, the `lf:question` marker and renderer, the sidecar signal channel,
  watermark/consumption handling, `litefactory status` visibility, and the no-answer behaviour.
  **What would bring it back, in one line:** a run that asks reporting as **✅ succeeded** turns out to
  be genuinely confusing in practice — at which point parking becomes worth its cost, and LF-12's
  permission gate becomes its second caller so it is built once rather than twice.
- **Code review.** Ring-fenced by the owner: *"code review and alike are untouched by this!"* Nothing
  is proposed for `go-code-reviewer`, `CODE_REVIEW.md`, or the Implement-mode review gates. **E1 edits
  `go-code-reviewer.md` for one reason only — where it reads story content from.** Its review
  behaviour, its BLOCKING/ADVISORY classification and its gates are untouched.
- **Matching the brief out of `Files` for the B2 link (rev 3.1's option N5(b)).** **Considered and
  rejected**: it drops the link on runs that summarise without rewriting the brief, breaking B1's rule
  that the summary always links the detail.
- **The hybrid SDK adoption — the seven existing endpoints staying hand-rolled while only sub-issue
  calls use the SDK (rev 4's option N7(b)).** **Considered and rejected at N7**, in favour of (a) full
  migration. **The reason is the credential guard, not tidiness:** a hybrid leaves **two HTTP stacks,
  two error taxonomies and two credential paths** permanently in one package, and the third means the
  `.Reveal()`-exactly-once guarantee (**AC-D1.6**) would have to hold across **both** paths — materially
  harder to re-establish and easier to erode later. **One credential path is one thing to guard.** It
  would also leave `doc.go` describing a "No SDK" package that half-uses one. **Recorded here so a
  future reader sees it was weighed, not missed.** Its one real advantage — a much smaller diff, since
  (a) rewrites seven working, tested methods nobody asked to have changed — was **known and accepted**
  as the price of the single credential path.
- **The forge abstraction (`forge.go`, LF-18).** **Explicitly not introduced by D1**, and
  `TestNoForgeAbstraction` stays green. Extract it later from two real implementations; do not guess it
  from one plus an SDK.
- **F-19 — deliberate marker impersonation by a third party.** Any third party can promote their
  comment from `old_third_party` to `litefactory` by writing `<!-- lf:` in it. **Real, pre-existing,
  and explicitly NOT fixed by Story S0** — line-anchoring stops the accident, not the attack. The real
  fix is inverting author-versus-marker precedence in `Classify`, which `classify.go:193-196`
  deliberately argues against for a stated reason. **A security change larger than issue #7.**
- **A Go-side ask-round cap or timeout.** Explicitly excluded by Q8.
- **Any imposed length limit on the research brief or the summary.** Explicitly declined at Q5. The
  32 KiB truncation is a physical property of `report/run.go`, not a policy this plan sets.
- **Run re-ordering for the blob link.** **Already done — confirmed twice.** `harvest.go` commits then
  pushes, and the daemon calls `turn.Execute` before `reporter.ReportTurn`. **A diff that re-orders
  them is a failing outcome** (AC-B2.11).
- **Rewriting `.aiteam/` session archives** when the plan artifact changes shape. They are the
  historical record (AC-E1.7).
- **Versioning or review workflow for sub-issue bodies.** N3 accepts that the per-story plan of record
  becomes mutable and unversioned. Named here so it does not look handled.
- **Reworking the eleven interactive approval gates** across the agent definitions.
- **LF-12 — the permission gate.** Named as the second caller of A2's machinery so that if A2 ever
  returns, it is not built twice.
- **LF-6 labels, LF-13 draft PRs, PR review comments as input, `/include`, prompt-injection
  hardening, rate limiting (LF-11).**
- **Unblocking `/research` `/plan` `/implement` as slash commands** — a separate Plan-sized question
  per `mode-closure/SPIKE.md`.
- **Multi-comment document splitting.** Explicitly rejected under B2.
- **Any schema migration.** Nothing in this plan needs one.

---

*Note on what LF-7 no longer is: it was in `Out of Scope` at rev 2 as *"unstarted and actively
test-blocked"*. It is now Story B2's link work — in scope, and **L-sized**: a `SHAAfter` plumb, a
builder, a `Pushed` gate, twelve golden regenerations, and real artifact discovery. Rev 2's
"test-blocked" was closer to right than rev 3 allowed.*
