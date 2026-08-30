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
