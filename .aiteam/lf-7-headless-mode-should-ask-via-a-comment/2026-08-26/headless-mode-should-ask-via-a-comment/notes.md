# Implement notes — headless-mode-should-ask-via-a-comment
# Branch: lf-7-headless-mode-should-ask-via-a-comment | Date: 2026-08-30

This run attempted Implement and delivered no story. It is recorded here so the next run does not
repeat the same three dead ends. **Nothing found this run invalidates the plan's design.** All three
blockers are environmental or permissions; two of them need a human to grant something before any
agent can make progress.

---

## Correction to the briefed end state — read this first

I was handed the end state as *"`initialplan.md` is the only modified file, one paragraph."* **That is
not what is on disk.** Verified with `git status --short` and `git diff` at the start of this
bookkeeping run:

```
 M CLAUDE.md      (3 hunks, +14/-2)
 M initialplan.md (4 hunks, +5/-5)
```

Both are **uncommitted, unstaged** working-tree changes. So:

- **`initialplan.md` is four hunks, not one paragraph.** It changes the PLAN box in the pipeline
  diagram, the `/implement` pre-check paragraph, the `forge.blob_url` permalink example, and the
  state-inference paragraph at `:243`.
- **`CLAUDE.md` was also edited and the edits landed.** It gains a new *"Plan mode's output:
  `plan_brief.md` + per-story sub-issues"* section under Artifacts, flips the MANIFEST template's
  Plan line to `plan_brief.md` + per-story sub-issues, and updates the `plan-orchestrator` roster row.

This matters for two reasons. First, **the next run will open a dirty tree with E1 half-applied** and
must not assume a clean slate. Second, it changes the shape of Blocker 2 — see below. The delivered
content itself is correct as far as it goes: it does **not** flip the Implement gate (**AC-E1.9
holds** — the gate section still names `IMPLEMENTATION_PLAN.md`, and the new section says so
explicitly and calls the flip a later shipment), and it preserves the state-inference principle while
naming the new artifact (**AC-E1.6 holds**).

**None of it is committed, and it is a fraction of E1.** It is supporting documentation, not a
delivered story.

---

## Blocker 1 — the Go tree is diverged from `origin/main` and cannot be synced

**Blocks: S0, B2, A1, D1, C1** — every story that touches Go.

Verified this run:

- `origin/main` is `71d3bea`. (The plan pins every `path:line` citation to this SHA.)
- `git diff --shortstat origin/main -- internal cmd` → **52 files changed, 257 insertions,
  7,752 deletions.**
- `git merge-base --is-ancestor HEAD origin/main` **exits 1** — HEAD is genuinely **diverged**, not
  merely behind. A fast-forward will not fix this.

**Attempted and refused by the permission system:**

- `git merge origin/main`
- `git checkout origin/main -- internal cmd`

**Why this stops the work rather than merely inconveniencing it.** Every Go story in the plan edits
files by `path:line` pinned at `71d3bea`. Against the current worktree those line numbers point at
different code or at nothing. An engineer editing the stale tree would either fail to find its
target or, worse, silently edit the wrong line. The plan already anticipates this — it carries a
provenance warning and gives five packages a re-pin task (T-D1.0 and siblings) — but re-pinning
assumes you are on the tree the citations describe. You cannot re-pin against a tree that is missing
7,752 lines of the code being cited.

**What a human must grant or do:**

Either grant permission for, or run by hand, one of:

```
git merge origin/main
# or, to take just the Go tree:
git checkout origin/main -- internal cmd
```

Then confirm `git diff --shortstat origin/main -- internal cmd` is empty before Implement is retried.
If the divergence is deliberate — i.e. this worktree's `internal/` and `cmd/` are wanted as they are
— then the **plan must be re-pinned against this tree instead**, and that is a Plan-mode change, not
something Implement should improvise.

---

## Blocker 2 — `.claude/` is not writable

**Blocks: B1 and E1** — the two stories scoped into this run. Both are almost entirely edits to this
AI team's own configuration.

**Attempted and refused:** two `Edit` calls against `.claude/agents/research-orchestrator.md`.

**The boundary is scoped to `.claude/`, and this run proved where it sits.** Repo-root files are
writable — `initialplan.md` **and `CLAUDE.md`** were both edited successfully. Only paths under
`.claude/` were refused. So the write set E1 and B1 need splits cleanly in two:

| Path | Writable this run? |
|---|---|
| `initialplan.md` (repo root) | yes — edited |
| `CLAUDE.md` (repo root) | yes — edited |
| `.claude/agents/**` (e.g. `research-orchestrator.md`, `plan-orchestrator.md`, `implement-orchestrator.md`) | **no — refused** |
| `.claude/skills/**` | **no — assumed refused; not separately attempted** |

**What a human must grant:** write permission for `.claude/agents/**` and `.claude/skills/**`.
`CLAUDE.md` needs no new grant — it is already writable and has already been written.

**A precedent worth keeping.** The B1 engineer returned BLOCKED with the tree untouched and
**declined to route around the denial by substituting `Write` for the refused `Edit`.** That was the
right call and should stay the norm: a refused tool call is a decision, not an obstacle to engineer
around. Any future engineer that "works around" a permission denial should be treated as having
failed the task, not completed it.

---

## Blocker 3 — D1 cannot pin the SDK version

**Blocks: D1.** Independent of blockers 1 and 2 — fixing those does not fix this.

The plan deliberately names the SDK (`github.com/google/go-github`) but **not its version**, making
the pin the first action of the story (**T-D1.1**), with **AC-D1.2** requiring confirmation that the
pinned version actually supports sub-issues. That check requires the live module proxy and GitHub's
documentation. **There is no web access in this session**, so the pin cannot be made or verified.

**What a human must grant or do:** either enable network access for the Implement run, or pin the
version by hand and record it in the plan along with the evidence that the version supports
sub-issues. **A version number written into any document without that live check is a guess**, and
this session's record is explicit that it has not made one — do not let a plausible-looking version
number enter the plan unverified.

---

## Plan correction found this run — AC-E1.2's grep will trip

**Verified by me directly against `origin/main`, not against the stale disk tree.**

The plan and several earlier reports state that a grep for the artifact names across `**/*.go`
returns nothing. **That is false at `origin/main`.** Four Go **test** files match:

```
internal/cli/cmd_logs_test.go:534
internal/config/toolprofiles_test.go:13
internal/harness/meta_test.go:222
internal/report/report_test.go:25
```

All four are **historical citations inside comments** — e.g. `// exists to make visible ("Settled
before planning", IMPLEMENTATION_PLAN.md):`. **No Go code reads the artifact.** E1's substantive
conclusion therefore stands: the migration does not require touching Go code.

**But AC-E1.2 is written as *"No live file outside `.aiteam/` refers to `IMPLEMENTATION_PLAN.md` as a
current artifact after E1 + C1. A grep is the check."*** These four files are outside `.aiteam/` and
will appear in that grep. Whoever runs the check must know they are **archive references, not live
ones**, and must not treat them as a failing outcome.

This also collides with **AC-E1.10 — "No Go file is modified by this story."** E1 is forbidden from
editing the four files its own acceptance grep will surface. **AC-E1.2 needs a carve-out** for
comment-only historical citations, or an explicit allow-list of these four paths. That is a small
Plan-mode amendment, not an Implement decision.

**This is the fourth measured instance of citation rot in this session.** The earlier "zero Go
references" claim was made against the stale disk tree and inherited forward unchecked. The pattern
holds: *this worktree drifts from `origin/main` faster than the documents citing it get rewritten,
and no artifact here has a mechanism that notices.*

## Plan confirmation found this run

**T-E1.0's 13-file inventory was re-confirmed and matches the plan exactly at `71d3bea`.** That part
of the plan is sound and does not need redoing. Note this does **not** resolve T-E1.0 as a task —
the story never ran — but the inventory it was meant to re-verify is correct.

---

## Why `.aiteam/APP_CONTEXT.md` was deliberately not created

The Exit phase would normally invoke `app-context-updater` in init mode. **I did not, on purpose.**

The Go tree in this worktree is 52 files and 7,752 deletions away from `origin/main`. Any app context
generated from it would describe utilities, packages and patterns that **do not exist on the real
main branch** — and app context is, by design, the first thing every future implementation engineer
reads and trusts. Writing it now would convert this session's stale-tree problem into a durable one
that silently misleads every later run.

**Do not create `APP_CONTEXT.md` until Blocker 1 is resolved** and the Go tree matches `origin/main`.
At that point init mode is worth running, and running it against a synced tree is cheap.

---

## State of the tree as this run ends

- **Uncommitted:** `CLAUDE.md`, `initialplan.md`. Nothing committed by this run.
- **No branch, stash or commit was created.** The next run inherits a dirty tree.
- **Decide before retrying:** whether to commit the partial E1 documentation changes as a standalone
  docs commit, or revert them so E1 can be run whole. Committing them means E1 later reports "already
  done" for two of its files; reverting means losing correct work that would just be rewritten.
  Committing is probably right, but it is the owner's call and should be made explicitly rather than
  inherited by accident.

---
---

# Implement run 2 — 2026-08-30 — Story A1 only

*A second Implement run, scoped by the owner to **Story A1 alone**. S0 and B2 were already delivered
at `e4a8d29`; B1 and E1 are owner-executed (rev 6); D1 and C1 were explicitly out of scope.*

## The previous run's diagnosis was wrong, and rev 6 already struck it

Everything above under *"Blocker 1"* and *"the Go tree is 52 files from `origin/main`"* is **struck**
and was struck by plan rev 6 before this run started. There was never a diverged Go tree: the branch
had never touched `internal/` or `cmd/`, it was 27 commits *behind*, and the "52 files, 7,752
deletions" figure is a one-way diffstat **read backwards** — those were additions on `main` the branch
lacked. The branch has since been merged and is synced.

**This is worth keeping, because the mechanism of the error is more useful than the error.** The
previous run was refused 22 Bash calls (`ls`, `find`, `grep`, `git ls-tree`, `git merge`, `git
checkout`, `mkdir`, `curl`, `go doc`) and then **reasoned about repository state from the fragments it
could still see.** It could not list files at a ref, so it inferred the tree from a diffstat, and it
inferred it backwards. The lesson is not "widen the allowlist" — the allowlist is deliberate and US3
of the tool-enforcement session replaces it with sandbox confinement anyway. The lesson is: **do not
reason about repository state from partial evidence. Use the file tools, which work.**

## `.aiteam/APP_CONTEXT.md` — the block above no longer applies

The section *"Why `.aiteam/APP_CONTEXT.md` was deliberately not created"* was conditioned on Blocker 1.
Blocker 1 did not exist. `APP_CONTEXT.md` **exists and is current** — it was found on disk at the
start of this run, not created by it, and it is accurate against the synced tree. It was read by the
implementation engineer, the code reviewer and the test engineer, and it earned its keep: it is where
the house rules about `doc.go` prose, comment honesty and the `surface_test.go` family come from.

## Code review advisories

`go-code-reviewer` returned **CLEAN** — no BLOCKING findings. One advisory, recorded for completeness:

- **CR-A-001 (advisory).** `internal/daemon/tick.go` was described by the implementer as "two doc
  comments only". It also updates two `log.DebugContext` **message strings** for accuracy. No test
  depends on the exact text and there is no behavioural effect, so this is a reporting imprecision
  rather than a defect. The edits themselves are correct and were necessary: both `KindNone` branches
  carried comments that A1 made false, and the issue-body branch's *"which the gate above has already
  refused"* became wrong once a tag-only body could reach it.

## Decisions worth remembering

1. **The modes that may ask are `research`, `plan` and `infer`** — not the literal `{research, plan}`
   the plan's architecture note writes. `infer` has to be permitted or **AC-A1.7 is unsatisfiable**:
   that criterion exists precisely because Go cannot know the eventual mode on the infer path, and the
   Implement-never-asks rule is carried there as prompt text. Denying `infer` would also make the
   feature unreachable in practice — a comment naming no command is the common path, and every one of
   them is `infer`. **`spike` is excluded**, as a deliberate reading of Q6, which named Research and
   Plan and named nothing else. Both exclusions are recorded in `modeMayAsk`'s doc comment.

2. **`/spike lf: ask do X` — the plan's own F-15 example — is deliberately NOT tagged.** F-15 predates
   N4, which settled the tag as **line-anchored**. A mid-line tag is therefore not a tag, so there is
   nothing to strip and the instruction is left verbatim. Relaxing the anchor for this one shape would
   reopen **F-22**, which is the reason the anchor exists: the plan document itself contains the
   literal `lf: ask` many times, and a `Contains` match would let an issue body quoting the plan turn
   permission on. The *genuine* F-15 hazard — a tag on its own line inside a spike body, landing
   mid-`Text` — is handled and tested. The reviewer was asked for a second opinion on this specific
   reading and judged it sound.

3. **Silently editing an instruction is the worse failure.** Follows from 2: where the tag is not a
   tag, the text stays as the owner wrote it rather than being quietly rewritten.

4. **The tag lives in `ParseCommand`, not at the call sites.** It is the single choke point both the
   comment path (`tick.go:587`) and the issue-body path (`tick.go:463`) go through, so the tag is
   found and stripped once for both. Stripping happens **before** tokenising, which is why F-16
   (tag-only comment → `KindNone`) and the tag-only `/spike` (→ `KindSpikeEmpty`) need no
   special-casing at all — they fall out of the existing paths.

5. **The gag paragraph split is structural, not textual.** `headlessRule` and `worktreeRule` are
   composed with a single space so the untagged path reproduces today's bytes exactly. A paragraph
   break there would have broken AC-A1.1 and `TestSpikePromptIsUnchanged`, whose literal was not
   touched.

6. **`PreviousMayAsk` uses the PREVIOUS run's mode.** Using this run's would make a tagged *spike*
   predecessor read as having had permission it never had, and the next run would announce that
   permission had been withdrawn from a conversation that never held it.

## Not done, and why

- **T-A1.11, T-A1.21, and the markdown half of T-A1.12** target `.claude/agents/research-orchestrator.md`
  and `.claude/agents/plan-orchestrator.md`. **Writes under `.claude/` are refused** — the same
  boundary that makes B1 and E1 owner-executed. These three are owner-executed. T-A1.12's *prompt-text*
  half is implemented in Go (`askingRule`), which is what actually makes a run emit the standing
  *"move to the next phase"* line, so **AC-A1.15 holds for the prompt**; what is missing is the
  instruction to the two orchestrator definitions.

---

# Implement run 4 — 2026-08-30 — T-B2.1, T-A1.11, T-A1.12 (markdown half), T-A1.21

*The four tasks that close out Stories B2 and A1. Markdown only, two files:
`.claude/agents/research-orchestrator.md` and `.claude/agents/plan-orchestrator.md`. Unit tests,
integration tests and environment setup were skipped by instruction — there is no Go in this run.*

## The `.claude/` permission blocker is confirmed dead

Run 2's *"Not done, and why"* above says these three tasks are owner-executed because **writes under
`.claude/` are refused**. That premise is now falsified for the second time in a day: B1's writes to
both agent definitions landed first try this morning, and this run's writes to the same two files
landed first try again. **It was never a deny rule** — it is a built-in approval prompt, and a
headless run with nobody attached collapses "needs approval" into "denied". With the owner present it
is simply an approval. Any remaining "owner-executed because `.claude/` is unwritable" scoping in the
plan — E1's banner in particular — rests on the same wrong premise.

## Decisions worth remembering

1. **B1 says what the summary *is*; T-B2.1 says *where it goes*.** These were deliberately written as
   one contract split across two adjacent sections rather than one section restating the other.
   `### Where the summary goes` sits between B1's audience split and B1's four hard rules, and it
   carries exactly two facts: litefactory posts the run's **last** message, so the summary must *be*
   that message and **nothing may follow it**; and `RESEARCH_BRIEF.md` is still written to `$session/`
   on every run regardless (AC-B2.9). No part of B1's text was reworded to accommodate it.

2. **The delivery half of B2 really is free.** No Go was needed or written. `secResult` already posts
   the final message; the work was telling the orchestrator to put the summary there. This is Q7 =
   option 1 behaving exactly as the plan predicted.

3. **The two files place `Asking` differently, and that is deliberate.** `research-orchestrator.md`
   gets a top-level `## Asking`; `plan-orchestrator.md` gets `### Asking` nested inside its
   `## Detour rule`. The reason is that each file's Detour default has a different relationship to
   asking. Research mode's default is *log the unknown and continue, never pause* — asking is a tier
   **above** that rule, so it earns its own section and says so. Plan mode's Detour rule **already
   contained** a pausing bullet (*"Requires a user decision → pause and ask"*), so the new text amends
   that bullet in place rather than sitting above it. The code reviewer was asked to judge whether
   this was drift and concluded it was defensible structure.

4. **T-A1.21 was written as a paragraph under the existing rule, not as a new rule.** The
   log-and-continue sentence is untouched; the addition immediately below it says the default stays
   the default and asking does not replace it. The test of this task was whether a reader of
   `## Detour rule` **alone** could conclude asking had replaced logging. They cannot now.

5. **The markdown had to agree with `askingRule`, not restate it.** The Go constant is what a
   permitted run is actually told at runtime; the agent definitions are what the orchestrator reads
   about its own role. Both files were written against `prompt.go`'s constants and the reviewer
   verified the two do not contradict — in particular that neither file claims a Go-side cap exists
   (AC-A1.14) and that both frame *"move to the next phase"* the way `askingRule` frames it.

6. **`modeMayAsk` permits `infer`; `plan-orchestrator.md` says "Plan and Research only".** These do
   not conflict. `infer` is the pre-resolution carrier, and the Implement-never-asks rule rides on it
   as `inferAskingSeam` prompt text (AC-A1.7). The reviewer checked this specific pair.

## Code Review Advisories

`go-code-reviewer` ran as a docs review and returned **ADVISORY_ONLY** — no blocking findings, all
eight gated axes pass. It also recorded that **CR-A-003 from the B1 review is resolved** by this
diff: `## Exit` now mirrors the business-summary requirement instead of listing only the brief.

Two new advisories, both **left unfixed on purpose** — neither is required by an AC, and acting on
either would have widened this run's scope past the four tasks it was given:

- **CR-A-004 (advisory).** `research-orchestrator.md`'s `## Asking` gives the batching *mechanism*
  ("put the questions into your final message and end the run") but not the batching *rationale*
  that `plan-orchestrator.md` and `askingRule` both spell out — that a queue of one-question runs
  costs the owner more attention than a wrong guess would have. Same behaviour, one file short of
  the justification. A one-sentence fix if the owner wants the two files to argue identically.

- **CR-A-005 (advisory, pre-existing).** Choreography step 4 (*"Present the brief to the user for
  confirmation before exiting"*) is older than this diff and sits in tension with step 5's sharpened
  *"nothing follows it"*. In a headless single-shot run there is no in-run step where the owner can
  confirm the brief before the final message is written. **This is a scope decision, not a wording
  slip** — resolving it means deciding whether "confirmation" means the owner's reply on the next
  run, or whether step 4 folds into step 5 entirely. Deliberately handed back rather than guessed at.

## Not done

- **Nothing from this run's four tasks is outstanding.** T-B2.1, T-A1.11, T-A1.12's markdown half and
  T-A1.21 are all delivered.
- **Both stories are now complete.** B2's Go — the permalink builder, the `Pushed` gate,
  deterministic brief discovery, the truncation marker and every regenerated golden — landed at
  `e4a8d29` (`internal/report/link.go` plus 491 lines of `link_test.go` and twelve goldens); T-B2.1
  was its last open task and this run delivered it. A1's Go landed in run 2 and reviewed CLEAN;
  T-A1.11, T-A1.21 and T-A1.12's markdown half were its last open items.
- **One correction to an earlier draft of this note.** It first claimed B2's Go was untouched. That
  was wrong and was caught by checking the tree rather than the plan: `link.go` exists and is tested.
  Recorded because it is run 1's mistake in miniature — **do not infer repository state from a
  document when the file tools can tell you.**

# Implement run 5 — 2026-08-31 — Story E1 only

## T-E1.0 — re-confirmed inventory, disk grep is a lower bound no longer

The plan's rev-6 table cited `71d3bea`; this worktree was at `5850daa`, two commits ahead (run 0010
partly converted `CLAUDE.md` and `initialplan.md`, committed at `1f0777c`). Re-ran the grep at
`5850daa`. **13 live files outside `.aiteam/`, same set as the plan's table, two counts changed**
because the partial work is now committed rather than uncommitted-on-disk:

| File | Occurrences at `5850daa` | Plan's table (`71d3bea`) |
|---|---|---|
| `.claude/agents/implement-orchestrator.md` | 8 | 8 |
| `.claude/agents/go-architect.md` | 6 | 6 |
| `.claude/agents/go-implementation-engineer.md` | 5 | 5 |
| `.claude/agents/go-code-reviewer.md` | 5 | 5 |
| `.claude/agents/plan-orchestrator.md` | 4 | 4 |
| `.claude/agents/go-unit-test-engineer.md` | 3 | 3 |
| `.claude/agents/proxy-product-owner.md` | 2 | 2 |
| `.claude/agents/go-integration-test-engineer.md` | 2 | 2 |
| `initialplan.md` | **2** | 5 |
| `CLAUDE.md` | **2** | 3 |
| `.claude/skills/shared/task-decomposer.md` | 1 | 1 |
| `.claude/agents/app-context-updater.md` | 1 | 1 |
| `.claude/AGENT_ROSTER.md` | 1 | 1 |

## The rev-6 "SPLIT: MOSTLY OWNER-EXECUTED" banner was stale and was overridden

It claimed `.claude/agents/**` and `.claude/skills/**` are not writable by a run, citing run 0010's
refused `Edit` calls. That measured boundary was specific to *headless* runs with no human present —
the `.claude/**` guard is a built-in approval prompt, and a headless run collapses "needs approval"
into "denied." This was an interactive session with the approval present, and all 11 `.claude/`
files edited cleanly on the first attempt. Same conclusion B1, B2 and A1 already reached in this
session — see run 4's note above, "the `.claude/` permission blocker is confirmed dead."

## What was done

All 13 files. `CLAUDE.md` and `initialplan.md` were already complete from run 0010 (verified, not
redone): the Artifacts section, the `MANIFEST.md` scaffold template, and `initialplan.md`'s five
`plan_brief`-naming edits including the `:243` principle amendment. The remaining 11 `.claude/`
files were rewritten this run:

- **`plan-orchestrator.md`** — the largest rewrite. Replaced the `IMPLEMENTATION_PLAN.md` format
  block with the `plan_brief.md` contract table + format block, plus a new per-story sub-issue
  contract + format block. Rewrote the Synthesise step and Exit steps to produce both artifacts.
  Stated explicitly that `.aiteam/` archives are never rewritten (T-E1.10). Stated the
  anti-duplication rule once, in the Synthesise step.
- **`implement-orchestrator.md`** — all 8 occurrences moved to `plan_brief.md` + per-story
  sub-issue content fetched once and passed inline to every downstream agent (AC-E1.4). Added an
  explicit **transitional fallback**: if `plan_brief.md` is absent but `IMPLEMENTATION_PLAN.md` is
  present (true of every session in this repo until Story C1 ships), treat that file as the whole
  plan. This was a judgment call, reasoned through below.
- **`go-architect.md`** (6), **`go-implementation-engineer.md`** (5), **`go-code-reviewer.md`** (5),
  **`go-unit-test-engineer.md`** (3), **`go-integration-test-engineer.md`** (2),
  **`proxy-product-owner.md`** (2), **`app-context-updater.md`** (1), **`.claude/AGENT_ROSTER.md`**
  (1), **`.claude/skills/shared/task-decomposer.md`** (1) — same pattern: primary artifact renamed
  to `plan_brief.md` + sub-issue content passed in the invocation, transitional fallback to
  `IMPLEMENTATION_PLAN.md` noted where the file still reads it directly.

## A judgment call worth flagging: why the transitional fallback exists

T-E1.4 and AC-E1.4 are explicit that `implement-orchestrator.md` (and the downstream agents it
invokes) should read `plan_brief.md` and fetch sub-issue content, full stop. Taken completely
literally, that would mean `implement-orchestrator.md`'s own `## Inputs` gate requires
`plan_brief.md` — but `CLAUDE.md`'s top-level gate (unchanged by AC-E1.9) still requires
`IMPLEMENTATION_PLAN.md`, and **every session in this repo, including this one's own `plan_brief.md`-
less `IMPLEMENTATION_PLAN.md`, would still need to run Implement mode for D1 and C1** before C1's
`T-C1.13` ever flips the gate. A literal switch would make `implement-orchestrator.md` unusable for
the rest of this very plan.

Resolution taken: every rewritten section names `plan_brief.md` as primary and adds one sentence
naming `IMPLEMENTATION_PLAN.md` as the fallback for a session that doesn't have `plan_brief.md` yet
— mirroring the same transitional language `CLAUDE.md` itself already uses ("Expect both names to
appear in the docs until then"). This satisfies T-E1.4's instruction to rewrite the 8 (well, 9 by
final count) occurrences around `plan_brief.md`, without breaking Implement mode for this plan's own
remaining stories in the gap between E1 and C1. Flagging this as a judgment call, not a silent
reinterpretation of the task — if the plan's authors intended `implement-orchestrator.md` to hard-
require `plan_brief.md` with no fallback, that is a one-sentence removal per file, not a redesign.

## Verified before commit

- `git diff --stat` — no `.go` file appears; no path under `.aiteam/` appears (the two file changes
  in this session's own `notes.md` and `MANIFEST.md` are bookkeeping, not the diff being measured
  for AC-E1.7/AC-E1.10 — they're this run's own artifacts, not a rewrite of an archived session).
- AC-E1.2 grep across the repo, excluding `.aiteam/` and `.git/`: only `CLAUDE.md` (the two settled,
  unchanged mentions — the gate line and its own transition note), `initialplan.md` (three settled
  transitional/historical mentions), the four Go test-file comments (settled exemption — historical
  references, not current-artifact claims), and the `.claude/agents/*.md` transitional-fallback
  sentences added this run. No file claims `IMPLEMENTATION_PLAN.md` as *the* current artifact except
  `CLAUDE.md`'s own gate line, which AC-E1.9 requires to stay exactly as it is.

---

## D1 pre-flight — T-D1.0, T-D1.1, T-D1.2, T-D1.3

2026-08-31, interactive session, before any D1 code was written. These four tasks
are the ones the plan says must be done with live access and never guessed, so
they were done here and the findings recorded before an implementation agent was
dispatched.

### T-D1.0 — citations re-pinned

Re-checked against `71cd53b` (the plan cites `71d3bea`, five commits stale). All
five citations are valid:

| Citation | State at `71cd53b` |
|---|---|
| `internal/github/doc.go:11` `# No SDK` | present; says *"Six endpoints do not justify a dependency"* |
| `internal/github/surface_test.go:56` `TestClientMethodSetIsTheSevenEndpoints` | present |
| `internal/github/surface_test.go:92` `TestNoForgeAbstraction` | present |
| `internal/github/surface_test.go:137` `TestRevealIsCalledExactlyOnce` | present; asserts `.Reveal()` appears exactly once and only on a line containing `Authorization` (`:156-167`) |
| `internal/github/pagination.go` | present; Link-header helpers `nextPage:62`, `nextLink:80`, `cutLinkValue:100`, `hasRel:123`, plus generic `fetchPages:24` |

**The "six versus seven" discrepancy the plan flags is real and confirmed**:
`doc.go:13` says six, `surface_test.go:56` says seven. The eight `func (c *Client)`
methods on disk are `BaseURL` (an accessor, not an endpoint) plus seven real
endpoints: `ListCommentsAfter`, `ListCommentedIssues`, `PostComment`,
`AddCommentReaction`, `ListIssues`, `GetIssue`, `AuthenticatedUser`. **Seven is
correct; `doc.go`'s "six" is the stale number.** T-D1.8 reconciles it.

### T-D1.1 — the version pin

**`github.com/google/go-github/v90 v90.0.0`.**

Chosen against `proxy.golang.org` on 2026-08-31, not from memory:

- `v90.0.0`, published 2026-08-04, is the newest major. `v91`, `v92`, `v93` return
  *"no matching versions"*. `v90/@v/list` returns `v90.0.0` alone — no patch
  releases, so `v90.0.0` is both newest and only.
- go-github ships one major per release, so "newest major" is "newest version".
- Its own `go.mod` requires `go 1.25.0`; litefactory is on `go 1.26`. Compatible.
- Transitive deps: `github.com/google/go-cmp v0.7.0` (**already a direct dep of
  litefactory at the same version — no new version in the graph**) and
  `github.com/google/go-querystring v1.2.0` (new, indirect).
- Licence: **BSD-3-Clause**, `Copyright (c) 2013 The go-github AUTHORS`. Permissive,
  no notice obligation beyond retaining the licence. The repo does **not** vendor
  (no `vendor/`), so the module-cache checkout is the whole of the vendoring step.

`v85.0.0` is worth noting as an oddity: its proxy origin `Ref` is
`refs/heads/master` rather than `refs/tags/v85.0.0`, unlike every neighbouring
major. Not our pin, but a reason not to reach for v85 if a downgrade is ever
considered.

### T-D1.2 — sub-issue support, confirmed against the pinned module

Confirmed by reading `v90.0.0`'s own source in the module cache — **not** from
documentation, and not from the `[W]`-sourced block in `RESEARCH_BRIEF.md`.

`github/sub_issue.go` exists and `SubIssueService` is wired onto the client
(`github/github.go:248` declares the field, `:701` constructs it), so it reaches
callers as `client.SubIssue`. Five methods, covering everything C1 needs:

| Method | Signature shape |
|---|---|
| `Add` | `(ctx, owner, repo string, issueNumber int64, body SubIssueRequest)` |
| `Remove` | `(ctx, owner, repo string, issueNumber int64, subIssue SubIssueRequest)` |
| `ListByIssue` | `(ctx, owner, repo string, issueNumber int64, opts *ListOptions)` |
| `Reprioritize` | `(ctx, owner, repo string, issueNumber int64, body SubIssueRequest)` |
| `GetParentIssue` | `(ctx, owner, repo string, subIssueNumber int64)` |

**Parent linkage: `Add`, with `SubIssueRequest.ReplaceParent` to move a sub-issue
that already has a parent.**

**The `sub_issue_id`-versus-issue-number trap the plan names is real, and the SDK
makes it explicit rather than removing it:**

```go
type SubIssueRequest struct {
    SubIssueID    int64  `json:"sub_issue_id"`             // Required: The ID of the sub-issue
    AfterID       *int64 `json:"after_id,omitempty"`
    BeforeID      *int64 `json:"before_id,omitempty"`
    ReplaceParent *bool  `json:"replace_parent,omitempty"`
}
```

The **parent** is addressed by its `issueNumber`; the **child** is addressed by
`SubIssueID`, which is the issue's database `id`, not its number. `GetParentIssue`
then takes a `subIssueNumber` — a *number* again. So the two identifier kinds sit
side by side in one API and the compiler cannot tell them apart: both are `int64`.
**This is C1's single sharpest edge.** The SDK's field name and comment are the
warning; nothing enforces it.

**No fallback needed.** Sub-issues are supported natively, so the generic-request
escape hatch named in AC-D1.2 is not required and was not exercised.

### T-D1.3 — the migration decision, recorded

Already settled at N7 = (a), full migration. Recorded here with the reasoning the
plan asks for: **one credential path is one thing to guard.** A hybrid would leave
two HTTP stacks, two error taxonomies and two credential paths, and the third of
those is what makes AC-D1.6 hard to hold. The rejection of the hybrid is a
security argument, not a tidiness one.

### The credential path — the design fact T-D1.6 turns on

**v90's constructor is not the classic `NewClient(*http.Client) *Client`.** It is:

```go
func NewClient(opts ...ClientOptionsFunc) (*Client, error)
```

— variadic options, **and it returns an error**. Relevant options:
`WithAuthToken(token string)`, `WithHTTPClient`, `WithTransport`, `WithTimeout`,
`WithUserAgent`, `WithURLs(baseURL, uploadURL *string)`, `WithEnterpriseURLs`.

**`WithAuthToken(token string)` is the single credential site**, and it takes a
plain `string`. So the reworked `TestRevealIsCalledExactlyOnce` should re-aim its
line assertion from `Authorization` to that call — one `.Reveal()` in the package,
on the line handing the token to `github.WithAuthToken`. The guard's shape
survives; only the anchor moves.

`WithTransport` is the hook for T-D1.11's faked-transport tests, and `WithURLs`
replaces whatever `BaseURL()` overriding the tests do today.

---

## D1 — implemented (2026-08-31)

T-D1.4 through T-D1.11, on top of the pre-flight commit `5b40c87`. Three commits:
`f9e5d7d` (the migration itself), `09f5088` (T-D1.9, the surface-guard rework),
`296dfc1` (a follow-up fix pinning the Accept header per endpoint after the
first pass left it at the SDK's default). Not pushed.

**Files changed:** `go.mod`/`go.sum` (the pin); `internal/github/{client,errors,
pagination,issues,comments,user,repo,doc}.go`; `internal/github/{surface_test,
client_test,pagination_test,github_test}.go` reworked; `internal/github/sdk_test.go`
new; `internal/github/pagination_internal_test.go` deleted (tested only the
removed Link-header helpers). **Nothing outside `internal/github` changed** —
verified by the implementation engineer and independently re-verified by the
code reviewer via `git diff --name-only`.

**AC-D1.6, the credential guarantee, holds.** Exactly one `.Reveal()` call site
in the package (`client.go:155`), feeding `gogithub.WithAuthToken(...)`.
`TestRevealIsCalledExactlyOnce`'s anchor moved from `Authorization` to that call
and the test was not weakened. A companion test,
`TestTheCredentialPathIsTheSDKs`, compiles against `WithAuthToken` directly so a
future SDK rename can't leave the grep-based guard passing against a dead
anchor.

**AC-D1.5 holds** — no interface entered any non-test file; `TestNoForgeAbstraction`
is unweakened.

**AC-D1.7 holds** — the new `errors.go` preserves every exported type, field and
sentinel of the old error taxonomy (`APIError`, `UnauthorizedError`,
`ForbiddenError`, `NotFoundError`, `ValidationError`+`ValidationDetail`,
`RateLimitError` with all six fields, and all nine sentinels). No caller outside
the package changed.

**Four deliberate, tested behaviour deltas**, all judged benign or a net
improvement by the reviewer:
1. Accept header: `application/vnd.github+json` → `application/vnd.github.v3+json`,
   plus a `squirrel-girl-preview` override the SDK sends on five of seven
   endpoints (its issue/reaction services). Pinned per endpoint by
   `TestTheAcceptHeaderIsTheSDKs`.
2. HTTP 202 was previously a success path; the SDK's `AcceptedError` is now
   mapped to `*APIError`/`ErrUnexpectedStatus` rather than leaked. No endpoint
   this package calls actually returns 202 in practice.
3. An off-host/scheme-changed `Link` header used to fail with `ErrInvalidArgument`
   explicitly; now the SDK simply can't reach it — only a page number survives
   in `Response.NextPage`. Verified **stronger**: a real second `httptest.Server`
   in the test receives zero requests.
4. String-shaped entries in a 422 `errors` array are still dropped, deliberately
   — `ValidationDetail.Code` is what the reporter branches on and the message
   text is already on `APIError.Message`.

**One judgment call kept:** `WithDisableRateLimitCheck()` stays enabled. Without
it, go-github remembers a 403/429 locally and refuses the *next* call without
asking GitHub — a form of backoff that belongs to the daemon, one layer up, not
to this package (which retries and sleeps nowhere, by design). Pinned by
`TestARateLimitDoesNotStopTheNextCall`.

**T-D1.9** retired `TestClientMethodSetIsTheSevenEndpoints`; the replacement,
`TestClientMethodSetIsPinned`, still fails on any silent surface change and adds
a count check that would also catch method promotion via embedding (relevant
because `Client` now wraps `*gogithub.Client` as an unexported field, never
embedded — a `go vet`/reviewer-checked choice that keeps the SDK's own method
set from leaking onto litefactory's `*Client`).

**Verification, independently run twice** (once by the implementing engineer,
once by the code reviewer): `gofmt -l .` clean, `go vet ./...` clean,
`go test -race -cover ./...` green across every package
(`internal/github` at 93.1% coverage), `make lint` (golangci-lint) 0 issues.

**Code review: CLEAN.** Fifth block appended to `CODE_REVIEW.md` (now 859
lines); the first four blocks are untouched — verified their header lines still
start at 1, 164, 283, 423. No BLOCKING, no ADVISORY findings, so nothing to
carry forward under "Code Review Advisories" here.

One `internal/harness` test (`TestRunTimeoutKillsTheWholeSubtree`) flaked once
mid-session on an unrelated fixture (`sleeper pgid ""`); `internal/harness` was
not touched by this story and the test passed on every other run, including
three targeted re-runs. Recorded so a future run does not chase it as a D1
regression.

**Remaining in the plan: C1 only.** D1 was the last blocker in C1's dependency
chain (`T-D1.5`, `T-D1.6` gate `C1`'s error handling and credential path); C1
can now proceed once a human opens Implement for it.
