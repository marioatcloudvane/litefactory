# Drift Report — headless mode should ask via a comment
# Branch: lf-7-headless-mode-should-ask-via-a-comment | Date: 2026-08-30

> ## ⚠ CORRECTED 2026-08-30, AFTER THIS REPORT WAS WRITTEN — READ THIS FIRST
>
> **Blocker 1 below ("the Go tree is diverged") was wrong, and it is the reason five stories were
> deferred.** Re-measured against the branch itself: `git diff --name-only main...HEAD` returns
> **zero** `.go`, `go.mod` or `go.sum` files. This branch has never touched `internal/` or `cmd/`.
> It was **27 commits behind**, not diverged, and `git merge-tree` reported a clean merge.
>
> The *"52 files, 257 insertions, 7,752 deletions"* figure is a one-way diff read backwards: those
> 7,752 lines are **additions on `main` that this branch did not have yet**, not code missing from a
> diverged tree. **`main` has since been merged in cleanly** (`da5ef58`); `go build ./...` and
> `go vet ./...` pass, and this report's own check — `git diff --shortstat origin/main -- internal
> cmd` — is now **empty**.
>
> **Why the misreading happened, and it matters more than the misreading.** The run could not
> execute `git ls-tree`, `git merge` or `git checkout`: `claude.allowed_tools` grants only
> `git status|diff|log|show`. Unable to list files at a ref, it reasoned from a diffstat alone.
> **Bash is 22 of this run's 29 denials** — `git ls-tree`, `git merge`, `git checkout`, `go doc`,
> `grep`, `find`, `ls`, `mkdir`, `curl`. The Bash allowlist, not `.claude/`, was the dominant
> blocker, and this report does not say so.
>
> **Blocker 2 stands as an observation but not as a request.** The `.claude/` refusal is *not*
> litefactory's deny list and cannot be granted in `config.yaml` — see `IMPLEMENTATION_PLAN.md`
> rev 6. B1 and E1 are now **owner-executed**.
>
> **Blocker 3 is cleared:** the issue is labelled `lf:tools=web`, so `WebFetch`/`WebSearch` are on
> the surface and T-D1.1 can pin the SDK itself.
>
> Everything below is left exactly as written, as the record of what the run believed at the time.

**Verdict up front: zero of seven stories delivered.** Five blocked on a diverged Go tree that could
not be synced, two blocked on a write permission that was refused. One supporting documentation
change from Story E1 landed and is sitting uncommitted in the working tree.

**This is an environmental and permissions outcome, not the plan being wrong.** Nothing found this
run invalidates the plan's design. The one correction it did surface (AC-E1.2, below) is a wording
fix to a single acceptance criterion, not a structural problem.

## Delivered as planned

Nothing. **No user story reached DONE.**

The only change on disk is a partial, uncommitted fragment of Story E1's documentation work:

- `initialplan.md` — four hunks: the PLAN box in the pipeline diagram, the `/implement` pre-check
  paragraph, the `forge.blob_url` permalink example, and the state-inference paragraph at `:243`.
  Records that Plan mode's output moves from `IMPLEMENTATION_PLAN.md` to `plan_brief.md` plus one
  sub-issue per story, and that state inference is untouched by the move because `plan_brief.md` is
  still a fixed-name repo file. Satisfies the intent of **AC-E1.6**.
- `CLAUDE.md` — three hunks: a new *"Plan mode's output"* section under Artifacts, the MANIFEST
  template's Plan line, and the `plan-orchestrator` roster row. Correctly does **not** flip the
  Implement gate, and says in as many words that the flip is a later shipment — **AC-E1.9 holds.**

Both are correct as far as they go. **Neither is a story.** E1 also requires edits across
`.claude/agents/**` and `.claude/skills/**`, which were refused; the story is a small minority
complete and is recorded as **not delivered**.

## Added scope

None. No work was done outside the plan. No new files, endpoints or logic.

## Dropped or deferred

All seven stories. Grouped by cause:

**Blocked on the diverged Go tree (Blocker 1) — five stories: S0, B2, A1, D1, C1.**
`origin/main` is `71d3bea`; `git diff --shortstat origin/main -- internal cmd` reports 52 files,
257 insertions, 7,752 deletions, and `git merge-base --is-ancestor` confirms HEAD is genuinely
diverged rather than behind. Both `git merge origin/main` and
`git checkout origin/main -- internal cmd` were attempted and refused by the permission system.
Every one of these stories edits Go by `path:line` pinned at `71d3bea`; those citations do not
resolve against the current tree.

**Blocked on `.claude/` not being writable (Blocker 2) — two stories: B1, E1.**
Both are almost entirely edits to this AI team's own configuration. Two `Edit` calls against
`.claude/agents/research-orchestrator.md` were refused. The B1 engineer returned BLOCKED with the
tree untouched. E1 partially landed only because the boundary is scoped to `.claude/` — its
repo-root files were writable, its `.claude/**` files were not.

**Additionally blocked (Blocker 3) — D1.** Its first task (T-D1.1) must pin the `go-github` version
against the live module proxy and confirm sub-issue support (AC-D1.2). No web access in this
session. This is independent of the other two blockers.

## Architectural deviations

None. No architectural hint in the plan was departed from, because no story ran far enough to reach
one. The sequencing (S0 · B1 · B2 · A1 · E1 · D1 · C1), the E1+C1 coupling and the D1 → C1
dependency were all untouched.

## Correction the plan needs

One, found and verified this run against `origin/main`:

**AC-E1.2 will trip on four Go test files.** The criterion reads *"No live file outside `.aiteam/`
refers to `IMPLEMENTATION_PLAN.md` as a current artifact after E1 + C1. A grep is the check."* The
plan and several earlier reports claim that grep returns nothing across `**/*.go`. It does not —
`internal/cli/cmd_logs_test.go:534`, `internal/config/toolprofiles_test.go:13`,
`internal/harness/meta_test.go:222` and `internal/report/report_test.go:25` all match. All four are
historical citations inside comments; no Go code reads the artifact, so **E1's substantive conclusion
stands**. But the grep-as-check will surface them, and **AC-E1.10 forbids E1 from modifying any Go
file** — so E1 cannot clear its own check. AC-E1.2 needs a carve-out for comment-only historical
citations, or an explicit allow-list of those four paths.

The earlier "zero Go references" claim was made against the stale disk tree. This is the fourth
measured instance of citation rot in this session.

**Confirmed sound:** T-E1.0's 13-file inventory was re-checked and matches the plan exactly at
`71d3bea`.

## Verdict

**DRIFTED — zero of seven stories delivered; the entire plan is deferred behind two permission grants
and one network grant, none of which an agent can give itself.**

The drift is total in delivery and near-zero in design. The plan does not need re-planning. It needs
its three blockers cleared by a human — a sync of `internal/` and `cmd/` to `origin/main`, write
access to `.claude/agents/**` and `.claude/skills/**`, and either network access or a hand-pinned
`go-github` version — plus the one-line AC-E1.2 amendment above. `$session/notes.md` states exactly
what must be granted for each.

**No follow-up planning cycle is required** beyond the AC-E1.2 wording fix. Retrying Implement before
the blockers are cleared will reproduce this result.

---

# Drift Report — run 4: T-B2.1, T-A1.11, T-A1.12 (markdown half), T-A1.21
# Branch: lf-7-headless-mode-should-ask-via-a-comment | Date: 2026-08-30

*Appended, not overwriting. The report above covers the abandoned first Implement attempt and its
correction; this section covers the run that closed out Stories B2 and A1.*

## Delivered as planned

- **T-B2.1** — `research-orchestrator.md` now states that the business summary is emitted as the
  run's **final message**, with the mechanical reason (litefactory posts the last message as the
  issue comment) and the rule that **nothing follows it**; and that `RESEARCH_BRIEF.md` is still
  written to `$session/` on every run. New `### Where the summary goes`, plus Choreography step 5 and
  Exit steps 3–4 tightened to agree with it. **This was B2's last open task.**
- **T-A1.11** — both `research-orchestrator.md` and `plan-orchestrator.md` state that the agent
  itself judges when its questions have been answered and **must honour a direct instruction to move
  on**, with re-asking a declined question named as a failure. Both say explicitly that there is no
  round cap, no counter and no timeout, in Go or anywhere, and that none is to be added.
- **T-A1.12 (markdown half)** — both files carry `### The standing "move to the next phase" line`,
  framed as the *One Run, One Mode* handoff: an invitation for the **owner** to open the next mode,
  never a licence for the agent to self-continue. The prompt-text half was already live in
  `askingRule`; nothing in Go was changed or proposed.
- **T-A1.21** — `research-orchestrator.md`'s log-and-continue Detour rule is untouched and gains a
  paragraph directly beneath it saying the default stays the default and asking is a **tier above**
  it, not a replacement.

## Added scope

- **Nothing functional.** No file outside the two agent definitions was edited except the session
  artifacts this mode is required to write (`CODE_REVIEW.md` appended, `notes.md`, `MANIFEST.md`,
  this report).
- **Two manifest corrections that were not tasks**, made because they were false on disk: the
  Implement mode box still read *"delivered nothing — zero of seven stories"*, which four runs have
  since falsified, and it still cited the two struck blockers as fact.

## Dropped or deferred

- **The two review advisories, deliberately.** **CR-A-004** (research-orchestrator's `## Asking`
  gives the batching mechanism but not the batching rationale the other file gives) and **CR-A-005**
  (Choreography step 4's *"present the brief to the user for confirmation"* sits in tension with
  step 5's sharpened *"nothing follows it"*, in a system where runs are single-shot). Neither is
  required by an AC. **CR-A-005 in particular is a scope decision, not a wording slip** — it asks
  what "confirmation" means in a headless run — so it was handed back rather than guessed at.
- **Unit tests, integration tests, environment setup and the bug loop** — skipped by instruction.
  There is no Go in this run and nothing executable to assert against; the ACs here are properties of
  prose, checked by review.

## Architectural deviations

**None.** The plan's own architecture note for B2 says *"Delivery is free (Q7 = option 1) … **No new
Go for delivery**"*, and that is exactly what happened. A1's Q8 note says asking limits are
agent-level with no Go cap and no timeout; the text says so in both files and adds no mechanism.

One structural choice worth recording, since the plan did not dictate it: **the two files place
`Asking` differently** — top-level `## Asking` in `research-orchestrator.md`, nested `### Asking`
under `## Detour rule` in `plan-orchestrator.md`. This follows each file's own default (Research logs
and continues, so asking sits *above* that rule; Plan already had a pausing bullet, so asking amends
it in place). The reviewer was asked to judge this specifically and found it defensible.

## Verdict

**CLEAN** — the four tasks were delivered as specified, in the two files named, with no Go touched
and no scope added. The only judgement calls were where to place new sections and whether to act on
two advisories; both are recorded above and the advisories were deliberately left to the owner.
