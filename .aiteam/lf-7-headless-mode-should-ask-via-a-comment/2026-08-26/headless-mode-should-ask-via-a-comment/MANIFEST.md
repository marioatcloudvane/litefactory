# Manifest
# Branch: lf-7-headless-mode-should-ask-via-a-comment
# Session: 2026-08-26

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done — 2026-08-26; revised 2026-08-27)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done — 2026-08-27; rev 2 on 2026-08-27; rev 3 and rev 3.1
  on 2026-08-30; rev 4 on 2026-08-30; **amended in place to rev 5** on 2026-08-30 after the owner
  answered the last two questions. **All 18 ANSWERED** — Q1–Q11 and N1–N7. **0 open questions** — the
  open-questions section now says so plainly. **N6: the SDK is `github.com/google/go-github`; its
  version is deliberately NOT named** and is an implementation-time pin (T-D1.1, AC-D1.1, AC-D1.2).
  **N7: (a), full migration** — all seven endpoints move, `net/http` and the Link-header helper come
  out, **D1 is XL**. **7 stories, unchanged**: S0 · B1 · B2 · A1 · E1 · D1 · C1 — **all fully
  unblocked**. **This artifact is scheduled for deletion by its own Story E1** — N3 replaced it with
  `plan_brief.md` plus one sub-issue per story)*
- [ ] **Implement** — code + tests *(**attempted 2026-08-30, delivered nothing — box stays unticked**.
  **Zero of seven stories completed.** Five (S0, B2, A1, D1, C1) blocked on a diverged Go tree that
  the permission system would not let this run sync; two (B1, E1) blocked on `.claude/` not being
  writable; D1 additionally blocked on having no network access to pin the SDK version. The only
  change on disk is a partial, **uncommitted** fragment of E1's docs work — `initialplan.md` and
  `CLAUDE.md`. **That is not a story and must not be read as partial completion.** See
  `DRIFT_REPORT.md` and `notes.md`. **`APP_CONTEXT.md` was deliberately NOT created** — see below)*
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

- 2026-08-30 — **Plan revised in place to rev 3 (run 0008).** The owner answered **all eleven**
  questions. `IMPLEMENTATION_PLAN.md` revised — not a new document. **Still Plan** — no code, nothing
  committed, and Implement still needs its own comment.

  **Every one of the eleven is now marked ANSWERED with what he decided.** Where his answer overrode
  a recommendation it was applied without re-argument — Q3 is the clear case: rev 2 recommended
  presence-only parsing and he chose **Go parses the literal, hard enforcement**.

  **Story count 5 → 5, but the set changed.** A2 **deleted** (Q11 = A1 only) and moved to
  `Out of Scope` with the one condition that would bring it back. **C1 added** — one GitHub sub-issue
  per user story, from Q4. Final set: **S0 · B1 · B2 · A1 · C1**.

  **Two earlier runs died before writing.** An earlier attempt hit a session limit; another stopped
  waiting on a verification sub-agent. Rev 3 was written first and verified nothing further.

  **Four new questions, deliberately few.** N1 (the sub-issue scope **reversal** — flagged plainly
  because Q4 asked what a phrase meant and the answer changed the deliverable), N2 (SDK versus
  hand-rolled, forced by the eighth client method), N3 (is `IMPLEMENTATION_PLAN.md` itself mirrored or
  replaced — asked rather than assumed, because Q1's answer suggests he likes mirroring), N4 (the
  tag's spelling — he settled its location at Q2 but never its wording).

- 2026-08-30 — **Plan amended in place to rev 3.1 (run 0009).** A verification pass returned **after**
  rev 3 had been written and reported. `IMPLEMENTATION_PLAN.md` amended, **not rewritten** — rev 3's
  structure, its eleven ANSWERED entries and N1–N4 are intact. **Still Plan** — no code, nothing
  committed, and Implement still needs its own comment. No sub-agents were run.

  **The headline is citation rot, for the third measured time in this session.** Rev 3's citation base
  was wrong: **`origin/main` is `71d3bea`**, not `3427686` (an ancestor). And rev 3's stale-file list
  was badly incomplete — it named seven files and said *"everything else matches main"*, when
  `git diff --stat origin/main -- internal cmd` shows **52 differing files**, including
  `internal/report/run.go`, `internal/report/report_test.go`, `internal/turn/{classify,report,execute,result}.go`,
  `internal/store/runs.go`, `internal/github/surface_test.go` and `internal/daemon/tick.go`.
  **So citations into `internal/report`, `internal/turn`, `internal/store`, `internal/github` and
  `internal/daemon` are disk line numbers, not `origin/main` line numbers.** The **facts stand**; the
  line numbers do not. A prominent provenance warning now sits at the top of the plan, and
  **re-pinning is Implement's first task** (T-B2.0), not something rev 3.1 attempted.

  **Story B2 grew, and rev 3 said the opposite.** Rev 3 sized the LF-7 link as *"one field, one
  builder, one retired test"*. Four corrections, one cheapening and three expensive:
  - **Cheaper:** the owner/repo field is **not needed** — `report.Run.Ref` is a `naming.Ref` already
    carrying `Forge`/`Owner`/`Repo`/`Issue`. T-B2.2 deleted.
  - **`CommitSHA` is the wrong field.** It is empty exactly on the supported path where claude commits
    for itself, so the permalink would render blank there. Must be **`SHAAfter`**, which is not
    plumbed into `report.Run`.
  - **The goldens are the real blocker, not the guard test.** `TestSuccessReport` pins the whole body
    via `checkGolden`; twelve golden files need deliberate regeneration. Rev 3 had no such task.
  - **`.aiteam/` artifact discovery does not exist**, and the link needs it. The only path source is
    `Files []git.FileChange`.

  **B2: S/M → L, and no longer fully unblocked.** Rev 3's offer to merge B1 and B2 is **withdrawn** —
  B2 being L-sized is the exact objection rev 2 raised and rev 3 believed had gone away.

  **N5 added — and only because a fact changed, not to reopen a settled thing.** Does the comment get
  real artifact discovery (net-new, always links), or match `RESEARCH_BRIEF.md` out of `Files` (cheap,
  but silently no link on a run that summarises without rewriting the brief)? **Recommended (a)** —
  not on cost, but because (b) breaks B1's fourth hard rule, which the owner set himself: the summary
  *always* links the detail.

  **One rev-3 claim survived intact:** push already precedes the comment (`harvest.go` commits then
  pushes; the daemon calls `turn.Execute` before `reporter.ReportTurn`). AC-B2.11 stands.

- 2026-08-30 — **Plan revised in place to rev 4 (run 0010).** The owner answered **all five**
  N-questions. `IMPLEMENTATION_PLAN.md` revised — not a new document. **Still Plan** — no code,
  nothing committed, and Implement still needs its own comment. One grep run this revision; no
  sub-agents.

  **Sixteen questions are now answered and nothing is outstanding from the owner.** Q1–Q11 at rev 3,
  N1–N5 at rev 4. **Three of the five N-answers overrode a recommendation** and all three are applied
  without re-argument: N2 reverses a documented project policy, N3 rejects rev 3.1's *mirrored*
  recommendation outright, N5 took the recommendation at its expensive end.

  **Story count 5 → 7.** N2 and N3 were each far too large to bury inside C1, so each became its own
  story. Final set: **S0 · B1 · B2 · A1 · E1 · D1 · C1**.

  - **N1 — CONFIRMED, deliberate.** *"yes, i changed my mind and we add it here, for completness!"*
    All hedging about an accidental reversal is removed; C1 is firmly in scope.
  - **N2 — USE AN SDK → new Story D1 (XL, or L if hybrid).** This overrides `internal/github/doc.go`'s
    explicit *"# No SDK"* section, whose stated trigger — a third pagination special case — **did not
    fire**; the trigger was sub-issues plus an owner decision, and the rewrite must record that
    distinction. **The sharpest item is security:** `TestRevealIsCalledExactlyOnce`
    (`surface_test.go:137`) fails the build if `.Reveal()` appears anywhere but the `Authorization`
    line, and it is **what keeps the token out of logs**. An SDK takes credentials through a different
    path, so **the guard will break** — the plan requires an equivalent guarantee be re-established,
    never deleted (T-D1.6 / AC-D1.6). Also flagged: **SDK adoption must not smuggle in the forge
    abstraction** — `TestNoForgeAbstraction` (`surface_test.go:92`) stays green; a concrete wrapper is
    fine, an interface is not. **A real upside is recorded, not just costs:** the sub-issue facts in
    `RESEARCH_BRIEF.md` are `[W]` and untrusted, and an SDK encapsulates exactly what that block is
    untrusted about — endpoint paths, payload names, the `sub_issue_id`-vs-number trap — so **N2
    materially reduces C1's risk.**
  - **N3 — REPLACED, not mirrored → new Story E1 (L).** Plan mode's output becomes **`plan_brief.md`
    plus one sub-issue per story**; the flat plan file goes. **It is 13 live files, not the 4 the
    owner named** — a grep finds 123 occurrences across 34 files, of which 21 are `.aiteam/` archives
    that must **not** be rewritten. **And zero are Go**: artifact-based state inference is entirely
    unimplemented in code, so this breaks nothing running and can be tested only by reading.
    **The gate risk is named and answered:** `plan_brief.md` becomes the Implement gate — a local file
    check — and sub-issues are discovered from its **Story Index**, rather than putting a network call
    into mode entry. `plan_brief.md`'s contents are specified as a section table, with an
    **anti-duplication rule**: it carries only what two or more stories need.
  - **N4 — `lf: ask`**, line-anchored. **A1 is now fully unblocked.** One collision note added: it is a
    different literal from the `<!-- lf:` self-marker and neither matches the other.
  - **N5 — (a), real artifact discovery.** B2 keeps its **L** size and the link half is unblocked;
    option (b) is removed with one line recording why it was rejected.

  **The one hard coupling, and it drove the re-sequencing: E1 and C1 must complete together.** E1
  alone leaves Plan mode producing shared context with nowhere for stories to live. **The gate flip is
  deliberately the last task in the plan** (T-C1.13) and must not merge before creation works.

  **Two new questions, and only two** — both fall out of N2 and neither is answerable from this repo:
  **N6** (which SDK, which **exact pinned version**; recommendation `github.com/google/go-github`, with
  **no version named**, because web tools are disabled and guessing one would be the `[W]`-grade error
  this plan spends a page warning about) and **N7** (migrate all seven endpoints or hybrid;
  **recommendation: full migration**, decided on AC-D1.6 rather than tidiness — a hybrid leaves two
  credential paths). **One genuine unknown is recorded as an acceptance criterion rather than an
  assumption:** whether the chosen SDK version supports sub-issues at all (AC-D1.2).

- 2026-08-30 — **Plan amended in place to rev 5 (run 0011).** The owner answered the last two
  questions. `IMPLEMENTATION_PLAN.md` amended — **small and surgical, not a rewrite.** Nothing was
  re-verified, no sub-agents were run, and **citations still pin to `origin/main` @ `71d3bea`** with
  the provenance warning and **T-B2.0** untouched. **Still Plan** — no code, nothing committed, and
  Implement still needs its own comment from the owner.

  **All eighteen questions are now ANSWERED and the plan has no open questions.** The
  open-questions section says so plainly rather than sitting empty. **No new question was raised.**

  - **N6 — the SDK is `github.com/google/go-github`.** The recommendation was taken.
    **The version is deliberately NOT settled and was NOT invented.** This session has no web access,
    so it could not check which major is current or which majors ship sub-issue support, and writing a
    number would have been exactly the `[W]`-grade guess the plan spends a page warning about.
    **The library is chosen; the version is an implementation-time decision.** Pinning it is
    **T-D1.1, the first action of D1**, taken by someone with live access to the module proxy and
    GitHub's docs. **AC-D1.2 stands:** confirm the pinned version actually supports sub-issues, with
    the generic-request escape hatch as the fallback if it does not.
  - **N7 — (a), full migration.** All seven existing endpoints move to the SDK; `net/http` and
    `pagination.go`'s Link-header helper come out. **D1's size is flatly XL** — the *"XL if migration /
    L if hybrid"* conditional is deleted. **T-D1.3 is unblocked** and now only *records* the decision;
    **T-D1.7 is unconditional** and its *"(if hybrid: …)"* branch is gone. **The deciding argument
    stays on the record: one credential path is one thing to guard (AC-D1.6), not tidiness.**
    **The hybrid moved to `Out of Scope` as considered and rejected**, with its two-credential-path
    risk and its one real advantage both stated, so a future reader sees it was weighed rather than
    missed.

  **`BLOCKED-ON-Nn` no longer appears anywhere in the plan.** All seven stories are fully unblocked.

  **Sequencing re-checked and unchanged:** S0 · B1 · B2 · A1 · E1 · D1 · C1. The **E1 + C1 hard
  coupling** (T-C1.13 is the join and the last task) and the **D1 → C1 dependency** are both still
  stated correctly; nothing needed adjusting. **The rev-4 story set of seven stands — no story was
  added or removed.** Code review remains ring-fenced.

- 2026-08-30 — **Implement attempted and abandoned. Nothing delivered; the Implement box stays
  unticked.** Two stories were scoped in (B1, E1); the run exited while waiting on the E1 engineer
  and a follow-up bookkeeping run wrote the exit artifacts. **No tests were written or run — the
  run never reached Phase 3, so there is no pass/fail count to report.** No code review, no
  environment setup, no bug loop. **Three blockers, none of which an agent can clear for itself:**

  1. **The Go tree is diverged from `origin/main` and could not be synced.** `origin/main` is
     `71d3bea`; `git diff --shortstat origin/main -- internal cmd` gives **52 files, 257
     insertions, 7,752 deletions**, and `git merge-base --is-ancestor` exits 1 — **genuinely
     diverged, not merely behind**. Both `git merge origin/main` and
     `git checkout origin/main -- internal cmd` were attempted and **refused by the permission
     system**. Blocks **S0, B2, A1, D1, C1** — every story touching Go, because every one of them
     edits by `path:line` pinned at `71d3bea`.
  2. **`.claude/` is not writable.** Two `Edit` calls against `.claude/agents/research-orchestrator.md`
     were refused. Blocks **B1 and E1**, which are almost entirely edits to this team's own config.
     The B1 engineer returned BLOCKED with the tree untouched and **correctly declined to route
     around the denial by substituting `Write` for the refused `Edit`** — recorded as a precedent
     worth keeping. **The boundary is scoped to `.claude/`**: repo-root files were writable, which
     is how E1's fragment landed.
  3. **D1 cannot pin the SDK version.** T-D1.1 needs the live module proxy and GitHub's docs to pin
     `go-github` and confirm sub-issue support (AC-D1.2). **No web access in this session.**
     Independent of blockers 1 and 2.

  **On disk, uncommitted:** `initialplan.md` (4 hunks) and `CLAUDE.md` (3 hunks) — E1's
  repo-root documentation edits. They are correct and they correctly **do not flip the Implement
  gate (AC-E1.9 holds)**, but they are a small minority of E1. **The next run inherits a dirty tree
  with E1 half-applied** and must decide whether to commit or revert them before retrying.

  **Note the briefed end state was itself slightly wrong** and is corrected in `notes.md`: this run
  was told `initialplan.md` was the only modified file and a single paragraph. `git status` shows
  **two** modified files and nine hunks.

  **One plan correction, verified against `origin/main`:** the claim that grepping `**/*.go` for the
  artifact names returns nothing is **false** — four Go **test** files match
  (`internal/cli/cmd_logs_test.go:534`, `internal/config/toolprofiles_test.go:13`,
  `internal/harness/meta_test.go:222`, `internal/report/report_test.go:25`), all **historical
  citations in comments**. No Go code reads the artifact, so **E1's substantive conclusion stands**,
  but **AC-E1.2's "a grep is the check" will trip on these** and **AC-E1.10 forbids E1 from touching
  Go files** — so E1 cannot clear its own check. AC-E1.2 needs a carve-out. **Fourth measured
  instance of citation rot this session.** Separately, **T-E1.0's 13-file inventory was
  re-confirmed and matches the plan exactly at `71d3bea`** — that part of the plan is sound.

  **Artifacts written:** `notes.md`, `DRIFT_REPORT.md` (verdict: **DRIFTED — zero of seven
  delivered**, environmental and permissions, **not the plan being wrong**).

- 2026-08-30 — **Blockers cleared and execution scope corrected (owner-driven, no mode entered).**
  `main` merged into this branch cleanly (`da5ef58`); `go build ./...` and `go vet ./...` pass. The
  Implement run's Blocker 1 — *"the Go tree is diverged"* — **was measured and does not exist**: this
  branch has never touched `internal/` or `cmd/`, and the *"7,752 deletions"* figure was a one-way
  diff read backwards. `DRIFT_REPORT.md` carries a correction banner.

  Re-measured from the run logs, the run's **29 denials are Bash 22 · Edit 4 · Read 2 · Write 1** —
  **the Bash allowlist, not `.claude/`, was the dominant blocker**, and it is what caused the
  diffstat misreading (`git ls-tree` was denied 10 times alongside `git merge` and `git checkout`).
  The allowlist is **deliberately not being widened**: the `tool-enforcement` session's US3 replaces
  it with OS-sandbox confinement, and patching it now is throwaway work.

  `IMPLEMENTATION_PLAN.md` is **rev 6**: **B1 and E1 are re-scoped as owner-executed**, because every
  file B1 writes and most of E1's thirteen sit under `.claude/`, which no run can write — and which
  litefactory's config cannot grant, since the guard is not litefactory's. Story set, sequencing and
  every AC are untouched. Issue labelled `lf:tools=web`, so **Blocker 3 is cleared** and T-D1.1 can
  pin the SDK itself.

  **Still open:** AC-E1.2 needs a carve-out for four comment-only Go test citations that its own grep
  will surface while AC-E1.10 forbids E1 from touching Go. **T-B2.0's re-pin is now mandatory** — the
  merge moved the tree past the `71d3bea` citation base.

  No story was implemented. Implement still needs its own human comment.

- 2026-08-30 — **Implement, Story B1 only — delivered.** `research-orchestrator.md` gains a business
  summary contract (audience split, the four hard rules, required sections, the ordering warning, and
  what the brief keeps); `codebase-tour.md` gains a return-size cap. **Markdown only — zero Go, no
  build, no tests; the unit- and integration-test engineers and environment setup were skipped as the
  plan says they do not apply.** `go-code-reviewer` ran as a docs review: **ADVISORY_ONLY**, no
  blocking findings, both advisories (CR-A-002, CR-A-003) fixed in place before the commit. Six gated
  ACs pass — AC-B1.1, AC-B1.3, AC-B1.4, AC-B1.5, AC-B1.6, AC-B1.7. **AC-B1.2 is manual/observational
  and was deliberately not gated.** AC-B1.7 verified: no `.go` file modified.

  **The rev-6 "OWNER-EXECUTED, NOT AGENT-EXECUTED" banner on B1 is superseded and should be struck
  from the plan.** Its premise was that `.claude/` is unwritable. It is not a deny rule — it is a
  built-in approval prompt, and a headless run (`permissionMode: default`, nobody attached) collapses
  "needs approval" into "denied". With the owner present, **both `.claude/agents/` writes landed on
  the first attempt.** The same correction applies to E1 and to T-A1.11 / T-A1.21 / T-A1.12, which
  were re-scoped to the owner for the same wrong reason. Not pushed — the owner decides that.

## Notes

**Implement attempt (2026-08-30) — three things to carry forward.**

1. **`.aiteam/APP_CONTEXT.md` was deliberately NOT created, and must not be created until the Go tree
   is synced.** The tree is 52 files and 7,752 deletions away from `origin/main`. App context
   generated from it would describe packages, utilities and patterns **that do not exist on main** —
   and app context is the first thing every future implementation engineer reads and trusts. Writing
   it now would turn this session's stale-tree problem into a durable one that silently misleads
   every later run. Run `app-context-updater` in init mode **after** Blocker 1 is cleared, not before.
2. **Do not retry Implement until a human has granted three things**: a sync of `internal/` and
   `cmd/` to `origin/main` (`git merge origin/main`, or `git checkout origin/main -- internal cmd`);
   write access to `.claude/agents/**` and `.claude/skills/**`; and either network access or a
   hand-pinned, evidence-backed `go-github` version. Retrying without them reproduces this run
   exactly. `notes.md` states what each blocker needs.
3. **A refused tool call is a decision, not an obstacle to engineer around.** The B1 engineer hit a
   denied `Edit` and returned BLOCKED rather than retrying as `Write`. Keep that norm — an engineer
   that "works around" a permission denial should be treated as having failed the task, not
   completed it.

**Plan rev 5 (run 0011) — one thing to carry forward.** The plan names the SDK but **not its
version, on purpose**. Anyone starting D1 must pin the version themselves as its first action
(T-D1.1) and confirm sub-issue support against a live source (AC-D1.2). **A version number that
appears in any downstream document without that check is a guess, and this session's record is
explicit that it did not make one.**

**Plan rev 4 (run 0010) — three things worth knowing before Implement.**

1. **`TestRevealIsCalledExactlyOnce` is the most dangerous thing in this plan.** It will break under
   D1, it is the only mechanism keeping the GitHub token out of logs, and the plan makes deleting it
   without an equivalent replacement a **failing outcome** (AC-D1.6). Anyone implementing D1 who
   treats it as a stale test in the way is removing a security control.
2. **`IMPLEMENTATION_PLAN.md` is now scheduled for deletion by its own Story E1.** The file carries a
   banner saying so. It remains the plan of record until E1 and C1 ship.
3. **The provenance warning below still governs, and rev 4 extended it to two new areas without
   re-pinning either:** `internal/github`'s surface tests (owner-verified at `71d3bea`, but the file is
   on the 52-file stale list — T-D1.0 re-pins) and `.claude/`+`CLAUDE.md` (E1's 13-file inventory is a
   **disk** grep, therefore a lower bound — T-E1.0 re-runs it).

**Plan rev 3.1 (run 0009) — read the provenance warning at the top of the plan before using any
`path:line` in it.** Citations pin to `origin/main` @ **`71d3bea`**. Five packages' line numbers are
stale and must be re-pinned before Implement edits them. **This is the third measured instance of
citation rot in this session** — run 0001's headline finding survived one day; `RESEARCH_BRIEF.md`'s
own source recorded six of its predecessor's external GitHub claims as wrong; this is the third. The
pattern is not carelessness: **this worktree drifts from `origin/main` faster than documents citing it
get rewritten, and no artifact here has a mechanism that notices.**

**Superseded below:** Plan-rev-3 note 1 (the LF-7 sizing) is **wrong on three of its four points** and
is corrected in the plan. It is left in place for archaeology, not as guidance.

**Plan rev 3 (run 0008) — the four things most worth knowing.**

1. **The clickable link (LF-7) is cheaper than every earlier document claimed, and it is now IN
   scope.** `report.Run` already carries `Branch`, `CommitSHA` and `Pushed` at render time
   (`internal/report/run.go:59-72`). **`Pushed`, plus the doc at `run.go:95-97`, confirms push already
   precedes the comment — the run-ordering change `initialplan.md:234` anticipated is already done and
   must not be planned again.** The only missing input is **repo owner/name**, which `report.Run` does
   not carry. So the link is: add owner/repo to `report.Run` + a blob-URL builder + **retire the guard
   test at `internal/report/report_test.go:240-241`** (which asserts no `/blob/`, commented *"LF-7 is
   out of scope"*) + remove the blocker comment at `run.go:61-62`. **SHA-pinned**, since `CommitSHA`
   and `Pushed` are both present; a branch-relative URL rots.

2. **The 8 KiB brief target is gone, replaced by an audience split.** Q5: no imposed size limit,
   declined explicitly. **The comment is for business analysts and business users; the repo artifact
   is for technical readers.** The summary must be plain-language, **honest — not swallowing technical
   complexity**, must **name the problems found**, and must always link the detail. One truncation
   *warning* survives, not a budget: the final message is cut at `maxResultBytes = 32 << 10`
   (`run.go:24,528`) **from the end**, so a runaway summary loses its tail — which is why the link and
   the problems go early. AC-B1.2 is now qualitative.

3. **Q3 and Q6 collide in exactly one place, and rev 3 says so rather than papering over it.** Go
   parses the literal (hard enforcement) and only Research and Plan may ask — so permission is
   `tagPresent && mode ∈ {research, plan}`, machine-enforced wherever the mode is known. **But when
   the mode is `infer`, Go does not yet know what the run will be**, so "Implement never asks" is
   carried in prompt text on that path only. Documented as a structural seam, not an oversight, with
   an offered alternative (deny infer runs outright).

4. **The Q8 cap left Go entirely.** No ask-round counter, no timeout, nothing in `internal/` — the
   agent judges whether its questions are answered and honours a direct instruction to move on.
   **AC-A1.14 makes a Go implementation of it a *failing* outcome.** Every asking comment carries a
   standing line inviting *"move to the next phase"* — which is consistent with *One Run, One Mode*,
   because that is precisely the human comment required to open the next mode, not litefactory
   licensing itself to continue.

**Costs C1 carries, recorded so the reversal in N1 is priced rather than assumed:** no issue-creation
or sub-issue call exists in `internal/github` (every verb GET or POST, five call sites, **no PATCH,
PUT or DELETE anywhere**); it needs an **eighth and ninth** method, tripping
`internal/github/surface_test.go:56`, a test that exists to force the SDK decision — **that decision
is the owner's, so it is N2, not a choice rev 3 takes**; the sub-issue API facts in
`RESEARCH_BRIEF.md` are **`[W]`, second-hand and unverified** (web tools disabled all session), so
every sub-issue task carries an AC requiring the contract be confirmed against live docs **before any
code**; and **no backoff exists** for bulk creation, so idempotency and partial-failure recovery are
requirements.

**One stated assumption, recorded as an assumption rather than a question:** the tag **ships in this
plan**. Q4's original question went unanswered when the owner re-scoped, but Q2, Q3 and Q6 all presume
it does and he answered all three in detail. If that reading is wrong, **Story A1 must not ship at
all** — A1 without the tag is ungated asking, which is R-3.

~~**Worktree caveat carried into the plan header:** `internal/harness/{prompt,runner,meta,errors,doc}.go`,
`internal/turn/{prepare,harvest}.go` and `internal/naming/session.go` are **STALE on disk** in this
worktree; everything else matches main. Citations pin to `origin/main` @ `3427686`.~~
**WRONG — corrected in rev 3.1.** `origin/main` is **`71d3bea`**, and **52 files** under `internal`
and `cmd` differ from it, not seven. See the rev-3.1 entry above.

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
