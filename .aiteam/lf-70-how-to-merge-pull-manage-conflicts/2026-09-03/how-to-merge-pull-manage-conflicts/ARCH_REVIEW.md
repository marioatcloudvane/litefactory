# Architect: Go Architect
# Feature: How to merge / pull / manage conflicts (litefactory#70)
# Spec reviewed: feature-spec.md (APPROVED, 2026-09-03)
# Created: 2026-09-05

**Verdict: APPROVE WITH CHANGES.** The feature is feasible and lands almost
entirely on existing seams. Three structural corrections are required before an
engineer starts — the sync step cannot sit where the spec places it, two of the
three new git methods should not sit on the type the spec places them on, and
US-6's "per-branch locking" must not be built as written. All three are recorded
as deviations below with the guidance that follows from them, so no task is
blocked waiting on an answer.

---

## System Overview

### How This Feature Fits Into the Existing System

Today a turn is a straight line: `internal/daemon` decides *when* work happens
and hands a `turn.Request` to `internal/turn`, which owns the business logic.
`turn.prepare` runs seven ordered steps; steps 2–4 (`fetch`, `freezeSlug`,
`ensureWorktree`) are one critical section against the registered clone, taken
together by `prepareClone` under a single `CloneGate.Acquire`. Everything that
touches git goes through `internal/git`, a leaf package whose `exec.go` is the
single sanctioned exec point and whose `Clone` method set *is* the S-6
allowlist. When the run ends, `turn` harvests the worktree, commits and pushes
under a second acquisition, and `internal/report` renders a comment that
`internal/github` posts.

This feature inserts a **branch-sync stage into that straight line** and threads
its outcome through four layers that already have a shape for it. The sync stage
measures how far the session branch has drifted from its base, proves whether
the base can be merged into it cleanly, performs that merge when it can, and
otherwise refuses the run. The outcome is a *value* — one of `up_to_date`,
`synced`, `blocked`, `skipped`, `failed` — carried on the existing `preparation`
struct, persisted on the existing `runs` row by a new migration, and rendered
into the existing run comment by a new optional `report.Sync` block. When the
outcome is `blocked` the run does not exec claude at all; it takes the existing
`*failure` path, which `Execute` already turns into a reportable result rather
than an error (CONC-3), and surfaces as a new `report.ReplyRefusesSync` comment
kind.

Nothing about the daemon's scheduling, the gate topology, the harness, or the
commit/push half changes. The feature adds **one new stage inside one existing
critical section**, three new methods on one existing type, three config keys,
four columns, one label lever, and one reply kind. That is the whole footprint,
and any design that grows beyond it has taken a wrong turn.

### New Packages / Components Introduced

**None.** This feature introduces no new package. That is a finding, not an
omission — every piece of it has an existing home:

| Component | Home | Responsibility |
|---|---|---|
| `Divergence`, `MergePreview`, `MergeResult` types | `internal/git` | In-memory value types describing branch drift and merge feasibility. |
| `Worktree.Divergence`, `Worktree.MergePreview`, `Worktree.MergeBase` | `internal/git/merge.go` (new file, existing package) | The three git commands. See DEV-2 for why all three sit on `Worktree`. |
| `ErrMergeConflict` | `internal/git/errors.go` | New sentinel alongside the existing fifteen. |
| `syncBase` | `internal/turn/prepare.go` | The orchestration step, called only from `prepareClone`. |
| `AutoSyncBase`, `AutoSyncMaxBehind`, `BlockRunOnConflict` | `internal/config` `Settings` | Three keys, defaults, template lines, aggregate validation. |
| `0005_run_sync.sql` + four `store.Run` fields | `internal/store` | Persistence of the outcome. |
| `report.Sync`, `Run.Sync`, `ReplyRefusesSync` | `internal/report` | Rendering and refusal wording. |
| `syncLabelPrefix` + `resolveSync` | `internal/daemon` | The `lf:sync=off` lever. |

If an engineer finds themselves creating `internal/sync`, `internal/merge`, or a
`pkg/` anything, stop: the dependency direction in `.aiteam/APP_CONTEXT.md` has
no room for it and the gate topology cannot follow it.

### Key Boundaries

**`internal/git` stays a leaf and stays the only gateway to the git CLI (S-6).**
All three new commands go through `runner.run` in `exec.go`. Nothing outside
`internal/git` ever names a git subcommand, a refspec, or the `base...branch`
three-dot form. `internal/turn` asks for `Divergence` and gets a struct; it
never sees an argv.

**`internal/git` declares no mutex and still will not.** Serialisation remains
the caller's job. The merge writes to the shared object database and moves a
branch ref in the shared `refs/`, so it is a per-*clone* contention, and it must
happen inside `prepareClone`'s single existing acquisition. See DEV-3.

**`internal/report` cannot import `internal/turn`.** `ReplyRefusesSync` is
`report`'s own enum member, and `turn`'s `replyKind()` (`internal/turn/report.go`
:696) gains the case that maps to it. The dependency arrow does not reverse for
this feature any more than it did for the mode and agent refusals.

**`internal/config` stays stdlib + `yaml.v3` only** (`purity_test.go`). Three
`int`/`bool` fields need nothing else.

**The label lever lives in `internal/daemon`, not `internal/config`.** Labels are
a per-tick fact about the world; the daemon reads the tick's fresh snapshot.
See DEV-4.

### Data Flow

```
tick (daemon)
  ├─ resolveMode / resolveAgent / resolveSync(labels)   ← lf:sync=off read here
  └─ turn.Execute(Request{... SyncOff bool})
       └─ prepare
            ├─ [1] load session/repo
            ├─ [2–5] prepareClone  ── CloneGate.Acquire(repo.Path) ── one release, deferred
            │        ├─ fetch
            │        ├─ freezeSlug            → branch name
            │        ├─ ensureWorktree        → *git.Worktree
            │        └─ syncBase              ← NEW, and it must be here (DEV-1)
            │             ├─ Worktree.Divergence(ctx, base, branch)   30s
            │             ├─ (behind == 0)        → up_to_date, return
            │             ├─ (sync off / cap hit) → skipped,    return
            │             ├─ Worktree.MergePreview(ctx, base)         60s
            │             ├─ (clean)   → Worktree.MergeBase(...)      60s → synced
            │             └─ (unclean) → blocked  (or skipped, if not blocking)
            │
            ├─ [6] shaBefore  ← read AFTER syncBase, so it names the synced tip
            └─ [7] artefacts
       ├─ blocked → *failure → Execute → report.Reply{Kind: ReplyRefusesSync} → PostComment
       └─ otherwise → harness runs claude → harvest → commitAndPush
                       → store.Run{SyncOutcome, SyncBehind, SyncAhead, SyncCommitSHA}
                       → report.Run{Sync: *report.Sync} → PostComment
```

The single most important thing in that diagram is that **`syncBase` is inside
the same acquisition as `fetch` and `ensureWorktree`, and takes no acquisition
of its own.**

### Concurrency & Context Notes

- **No new goroutine.** This feature introduces none, needs none, and any
  proposal to run the divergence probe concurrently with the fetch should be
  refused: they contend on the same clone and the gate exists to stop exactly
  that.
- **One acquisition, not four.** `prepareClone`'s doc names the "One
  acquisition, not three" invariant and `gate_test.go` asserts it against the
  syntax tree. `syncBase` joins the `gated` registry (allowed only inside
  `prepareClone`); it does **not** become a fourth entry in the `sections`
  registry. Adding a fourth section is the failure mode the test's doc comment
  predicts.
- **No deadline is imposed at the section level.** `prepareClone` passes the
  context through untouched because every git command starts its own deadline
  when it execs. `syncBase` must do the same: do not wrap the three merge calls
  in a `context.WithTimeout`, and do not create a `context.Background()` to
  "protect" a rollback from a cancelled parent. The 30s/60s/60s deadlines belong
  in `exec.go`'s single timeout block, next to `readTimeout`/`fetchTimeout`/…,
  so the whole timeout policy stays readable in one place.
- **The rollback path runs under a possibly-cancelled context.** A merge killed
  by its own 60s deadline, or by daemon shutdown, leaves `MERGE_HEAD` and
  possibly `.git/index.lock` behind. This is the one place where "the context is
  already dead but cleanup must still happen" is real. The correct shape is
  *not* a detached context: it is to make the pre-merge cleanliness check and
  the post-merge SHA verification the mechanism (see S-5), so that a rollback
  that cannot run leaves a detectable, reportable `failed` rather than a silent
  half-merge.
- **`ctx` first on every new method** (CONC-5), no exceptions.

### Shared State and Side Effects

Things other packages must know are now true:

1. **The session branch can move without the agent doing anything.** Before this
   feature, a session branch only ever moved because claude changed files and
   `commitAndPush` committed them. After it, `prepareClone` can create a merge
   commit on that branch. Anything reasoning about "the branch changed, therefore
   the run did work" is now wrong. `preparation.shaBefore` must be read **after**
   `syncBase`, not before, or every synced run reports a phantom diff.
2. **`merge-tree --write-tree` writes loose objects into the shared object
   database even when it reports a conflict.** The "dry run" is a dry run of the
   *branch*, not of the repository. Document this in the method's doc comment;
   do not call it a dry run without qualification.
3. **Base-branch content now reaches the coding agent on runs after the first.**
   See S-7. This is a deliberate widening and must be disclosed in the comment
   (US-3), which is what makes it acceptable.
4. **A blocked run posts a comment and consumes a trigger.** It does not exec
   claude, does not create a run artefact directory of the usual shape, and does
   not commit. Anything downstream that assumes "every trigger produces a run
   with a transcript" needs to tolerate the refusal shape — the same way it
   already tolerates `ReplyAgentSwitchRefused`.

---

## Judgement on the Two Flagged Assumptions

The requirements engineer could not validate these alone and this run is
headless, so both are resolved here.

### A-7 — `auto_sync_max_behind: 100`

**Resolution: keep the key, keep commit-count as the unit, keep 100 as the
default — and require the doc comment to say the number is a judgement, not a
measurement.**

Reasoning:

- The unit is right. `behind` is already computed by `Divergence` for the report,
  so capping on it costs nothing. A size-based cap (bytes of diff) would need a
  second git call on every run to answer a question that fires almost never.
- The cap is not a safety mechanism — `merge-tree` is the safety mechanism, and
  it is precise. The cap is a *review trigger*: it exists for the resurrected
  branch, months stale, where a clean merge of several hundred commits would
  silently rewrite the ground the agent reasons about. That case genuinely wants
  a human, and a clean merge-tree result is not evidence that it does not.
- 100 is defensible for that purpose and harmless when wrong, because the
  outcome when it fires is `skipped`: no branch mutation, and the run proceeds
  exactly as it did before this feature existed. A too-low cap costs a stale run;
  it cannot cost a corrupted branch.
- **What is not acceptable is presenting 100 as evidenced.** `internal/git`'s
  `ErrUpstreamNotRecorded` sets this project's convention: measured claims name
  their measurement, and this one has none. The field's doc comment must say so
  plainly — a chosen threshold, revisable, with the reasoning above — rather
  than reading like a number someone benchmarked.
- **`0` means no cap is a magic value** and must be documented on the field and
  in `template.go`'s inline comment. Negative is a config error, never a clamp
  (CFG-6).
- **One behavioural requirement follows:** `skipped` must be disclosed in the run
  comment, not silent. "Your branch is 340 commits behind and litefactory did not
  sync it" is more important for a human to see than "synced", because it is the
  case where the run's output is built on old ground. US-3 says disclose every
  automatic branch change; extend it to disclose the deliberate non-change.

### A-14 — `block_run_on_conflict: true`

**Resolution: confirmed, `true` is correct — conditional on US-7 shipping in the
same release.**

Reasoning, against litefactory's actual lifecycle:

- Disclosure is not what blocking buys. US-4/US-5 name the conflicting files
  either way, so the human is informed either way.
- What blocking buys is that the conflict stays small and stays separable. If
  the run proceeds on an un-synced branch, the human later merges a branch that
  is *both* behind base *and* carries new agent changes in the same files, and
  must untangle two conflicts at once. Blocking keeps them one conflict.
- **The agent cannot resolve it.** This project's permission list admits
  read-only git verbs only; no agent runs `git add`/`commit`/`merge`. A human is
  the only actor who can fix a conflicted branch, so a mechanism that summons a
  human is the correct response, not a fallback.
- A blocked issue goes inert: every subsequent comment gets the same refusal
  until someone intervenes. That is loud and unambiguous, and it is the right
  failure mode for a branch that genuinely needs a person. The alternative —
  runs continuing while the divergence compounds — fails quietly, which is worse
  in every case where nobody is watching, and nobody is watching a headless
  daemon.
- **The false-positive worry is real and US-7 is its answer.** `MergeBase` merges
  the whole base, so a conflict in `README.md` blocks a run whose task lives in
  `internal/store`. Without an override that is an unacceptable default. With
  `lf:sync=off` — one label, no config edit, no redeploy — it is acceptable.
  **Therefore US-7 must ship with or before US-2/US-4, not after.** Shipping the
  block before the escape hatch would put an un-overridable refusal in front of
  real work. This is a hard sequencing constraint, recorded in the task table.
- **`lf:sync=off` must mean "do not sync *and* do not block."** An override that
  disables the merge but leaves the refusal in place is not an escape hatch. Its
  outcome is `skipped`, disclosed like any other.

---

## Deviations

Each of these changes something the approved spec states. They are resolved
here, with the guidance that follows, because there is nobody to ask.

### ⚠️ DEV-1 — The sync step cannot sit between `fetch` and `freezeSlug`

**Spec says** (Business Rules, and again under API/Interface Changes):
`prepareClone` "gains a `syncBase` step between `fetch` and `freezeSlug`".

**Why that is not buildable as written:** `MergeBase` is a `*git.Worktree`
method, and the worktree is produced by `ensureWorktree`, which runs *after*
`freezeSlug` and needs the branch name `freezeSlug` returns. At the point between
`fetch` and `freezeSlug` there is no branch name and no worktree, so there is
nothing to merge into. Splitting measurement (`Divergence`) out to the early
position and leaving the merge late buys nothing: both need the branch name, and
two positions means two chances for the ordering to drift.

**Resolution:** `syncBase` runs **after `ensureWorktree`**, as the fourth and
last step inside `prepareClone`, still inside the one existing acquisition.

**Impact:** `preparation.shaBefore` must be read *after* `syncBase`, not before
— the spec's AC-3.24 rationale ("read here, because that is the only moment it
means anything") now points at a moment one step later. Every acceptance
criterion survives; only the ordering sentence changes. Stories affected: US-2,
US-9, US-10.

### ⚠️ DEV-2 — `Divergence` and `MergePreview` belong on `Worktree`, not `Clone`

**Spec says:** `(*git.Clone) Divergence(...)` and `(*git.Clone) MergePreview(...)`.

**Why that costs more than it should:** `internal/git/surface_test.go`'s
`TestCloneMethodSetIsTheAllowlist` hardcodes `Clone`'s nine-name method set and
fails with *"The method set of Clone is the S-6 allowlist. If a new git command
really belongs against the registered clone, amend S-6 first."* Adding two names
means editing a TEST-10 design-change test **and** writing an S-6/SEC-7
amendment into `.aiteam/AC_AMENDMENTS.md`. Spec AC-4 quietly requires that edit
while AC-5 assumes nothing structural moves.

**Resolution:** put all three methods on `Worktree`. Cost of doing so: nothing.
`Worktree` already carries read methods (`RevParse`, `Status`, exercised by
`TestZeroValuesRefuseToRunGit`), no test pins its method set, and running
`rev-list` or `merge-tree` with a worktree as cwd is ordinary git. Benefit:
`surface_test.go` passes **unchanged**, no AC_AMENDMENTS entry is needed, SEC-7
is not touched, and the feature's entire git surface sits on one type next to
the merge it exists to serve.

**Honest counter, recorded so nobody thinks it was missed:** neither command
technically needs a working tree — both are object-database operations, so
`Clone` is their "natural" home in git's own terms. The boundary argument wins
anyway, because S-6's allowlist is a security boundary and this feature is not a
good enough reason to widen it.

**If the owner prefers `Clone` regardless:** that is a legitimate call, but it
requires (a) an S-6 amendment in `.aiteam/AC_AMENDMENTS.md` naming both methods
as reads, (b) a deliberate edit to `surface_test.go`'s `want` list, and (c) a
note that `merge-tree --write-tree` writes objects, so "read-only against the
registered clone" becomes a claim that needs qualifying. Do not make that edit
without the amendment; TEST-10 forbids editing a design-change test to pass.

**Impact:** `Worktree`'s doc comment sentence — *"The mutating half is Commit
and Push — the only two operations in litefactory that write to a repository or
to a remote"* — must be rewritten for `MergeBase` regardless of this deviation.
Stories affected: US-1, US-2, US-9.

### ⚠️ DEV-3 — US-6's "per-branch (not per-repo) sync locking" must not be built

**Spec/story says:** per-branch rather than per-repo sync locking, so conflict
rate does not scale with concurrent issues.

**Why re-keying the gate would be wrong:**

1. The contention it protects against is measured and is **per-clone**, not
   per-branch. `clone.go`'s doc records parallelism-4 numbers: fetch failed 75%
   when remote refs moved, `worktree add` failed 0.75% fatally, and
   `push --set-upstream` left 25% of branches with no upstream while exiting 0.
   None of those are branch-scoped. `git fetch --prune`, the object database and
   `.git/config` are shared by every worktree of one clone.
2. Re-keying reopens the TOCTOU window `prepareClone` exists to close. Its doc
   names it: gating each git call separately "would look tidier and would be
   wrong". `gate_test.go` asserts the topology against the syntax tree precisely
   so this cannot happen by accident.
3. The merge itself moves a ref in the shared `refs/` and writes to the shared
   ODB, so it is per-clone contention too.

**Resolution: US-6's goal is already met by the existing model and requires no
new code.** One worker per session, sync inside that run's own `prepareClone`,
one acquisition per repo held for the length of three fast git commands plus a
merge. The claude run — the long part — sits outside the gate entirely, which is
the whole reason serialising git costs almost none of the parallelism it
protects (AC-PAR-3.2). Conflict rate does not scale with concurrency here
because two concurrent issues merge into two different branches; they queue, they
do not conflict.

**What US-6 becomes:** a verification story, not a construction story — a test
proving that N concurrent turns on one clone each sync their own branch without
interference and without a second acquisition. If that test is green, US-6 is
done. Do not add a second gate, a branch-keyed registry, or a mutex in
`internal/git`.

### ⚠️ DEV-4 — `lf:sync=` cannot be modelled on `lf:tools=`

**Research brief says** (and its citation to `internal/config/policy.go:64,351`
is wrong; the code lives in `internal/daemon`): model the new label lever on the
existing `lf:tools=` lever.

**Why that precedent is unusable:** `lf:tools=` is no longer a lever. In
`internal/daemon/tools.go` the constant survives only inside
`legacyToolsLabel(labels []string) bool`, whose doc says *"reports only whether
an obsolete permission label exists. Its value is deliberately neither returned
nor logged."* Tools are resolved from the run mode now. Copying it would produce
a label that is detected and refused, which is the opposite of what US-7 needs.
This is a larger correction than the citation error the RE flagged.

**Resolution:** model `lf:sync=` on the two *live* levers, which share one shape:
`agentLabelPrefix` (`internal/daemon/agent.go:21`) and `modeLabelPrefix`
(`internal/daemon/mode.go:14`). Their detect-loosely / accept-strictly ladder is
what the new lever must copy, rung for rung:

- no label carrying the prefix case-insensitively → nothing resolved, silence;
- more than one such label → ambiguous, refused, never unioned and never
  first-wins (the order GitHub returns labels must not decide behaviour);
- prefix matched case-insensitively but not byte-for-byte → malformed, refused,
  with a `MiscasedPrefix` flag and **no field for the label text to travel in**
  (SEC-5 / S-9);
- value not in the accepted set (`on`, `off`) → malformed, refused;
- otherwise resolved, and the label beats config.

### ⚠️ DEV-5 — `gate_test.go` needs a deliberate registry edit

Not a spec deviation, but a TEST-10 boundary an engineer will hit and must not
paper over. `internal/turn/gate_test.go`'s `gated` map must gain `"syncBase":
{"prepareClone"}` (plus the git method names `syncBase` calls, if the test's
convention covers them). The `sections` map must **not** gain a fourth entry —
`prepareClone`, `commitAndPush`, `commitStoryIndex` remain the three registered
critical sections. Record the reasoning in the commit; TEST-10 says these tests
are changed deliberately, never edited to pass.

---

## Platform Constraints

- **git ≥ 2.38 is a hard floor.** `merge-tree --write-tree` does not exist
  before it. `internal/git` resolves the binary once at startup (`git.Resolve`);
  the version probe belongs with that resolution or behind a `sync.Once` at first
  sync, never per-run. An unsupported git must produce a **named sentinel** and
  a `skipped` outcome with a clear sentence for the operator — not a raw
  `*CommandError` they have to decode, and not a hard boot failure that breaks
  every existing deployment on an older git.
- **EXT-1/EXT-2 apply to `merge-tree`.** Probe it read-only in a scratch
  directory, and write the findings up naming the exact verified version, in the
  style of `ErrUpstreamNotRecorded`'s doc comment. Three things must be
  established by measurement, not assumption: the exit code for a conflicted
  merge (expected 1) versus a broken invocation (expected 128); the exact output
  shape with `--name-only`; and whether conflicting paths come back C-quoted
  under this project's `core.quotePath` posture.
- **A live daemon runs from `dist/litefactory`.** Do not `make build`, do not
  write into `dist/`, do not kill or restart it, do not edit
  `~/.litefactory/config.yaml`. Safe: `go build ./...`, `go test ./...`,
  `go vet ./...`, `gofmt -l .`.
- **No new dependency.** `internal/config` is stdlib + `yaml.v3` only, enforced
  by `purity_test.go`; three int/bool fields need nothing. No merge library, no
  go-git — `internal/git` shells out and that is the design.
- **SQLite: one connection in the pool**, `modernc.org/sqlite`. Migration 0005 is
  one embedded `.sql` file, version taken from the filename prefix, one
  transaction. `0004_mode_selections.sql` is the current head, so `0005_run_sync.sql`
  is correct. Follow 0004's shape: plain DDL, no library, `NOT NULL DEFAULT`
  on every added column so existing rows migrate without a backfill pass.
- **`BEGIN IMMEDIATE` on the read-then-write path** that stamps the sync outcome
  onto the run row.
- **macOS launchd child environment is deliberately minimal (SD-5).** `childEnv()`
  passes `PATH`, `HOME`, `SSH_AUTH_SOCK`, `GIT_TERMINAL_PROMPT=0` and a BatchMode
  `GIT_SSH_COMMAND`, and nothing else. A merge commit therefore inherits no
  identity and no signing config — see S-4.

---

## Security Findings (forward-looking)

Scoped as instructed to merge execution, branch mutation, untrusted merge
content, command construction, and the gate model. Severity is about this
system, not in the abstract.

| # | Finding | Severity | Where it must be handled |
|---|---|---|---|
| S-1 | Ref names reaching `merge`/`merge-tree`/`rev-list` argv | High | `internal/git` — `checkName` before `run`, always |
| S-2 | Conflicting paths echoed into a comment litefactory signs | High | `internal/report` — re-apply restrictions locally (SEC-5) |
| S-3 | `merge-tree --write-tree` is not a dry run of the repository | Low | doc comment on `MergePreview` |
| S-4 | `merge --no-ff` needs an identity and `commit.gpgsign=false` | Medium | `internal/git` — copy `commit.go`'s `-c` prelude |
| S-5 | "Branch byte-identical after failure" must be proven, not hoped | High | `internal/git` — clean-check, abort, verify SHA |
| S-6 | Merge outside the acquisition reopens the fetch/add race | High | `internal/turn` — `syncBase` inside `prepareClone` |
| S-7 | Auto-sync widens the prompt-injection window into the agent | Medium | disclosure (US-3) + doc |
| S-8 | A timed-out merge leaves `MERGE_HEAD` and `index.lock` | Medium | `internal/git` — `TimedOut` stays distinguishable |
| S-9 | Unbounded conflict list | Low | bound at parse, not at render |

**S-1 — command construction.** `base` and `branch` must pass `checkName` before
they reach `runner.run`, exactly as `Fetch` screens `remote`. This is not
theoretical here: branch names derive from issue slugs, and an issue title is
attacker-controlled on any public repo. `exec.go`'s own comment names the class
— *`"--upload-pack=..."` wearing the costume of a remote name is the classic way
to turn a repository URL into command execution.* Two additional rules for this
feature specifically:

- **Assemble `base...branch` inside `internal/git`, from separately screened
  components.** Never accept a pre-joined range from a caller. A caller that can
  pass a string containing `...` can pass a string containing anything.
- **Use `--end-of-options`** the way `revParse` already does. `checkName` refuses
  a leading `-`; `--end-of-options` is the second lock on the same door, and
  `merge`, `merge-tree` and `rev-list` all accept it.

**S-2 — untrusted merge content, the one that matters most.** Conflicting file
paths come out of the *base branch's* tree. Anyone who can land a commit on base
— which on most repos means anyone whose PR gets merged — controls a string that
litefactory will print into a comment it signs its own name to. Filenames can
carry backticks, markdown, `<!-- ... -->` sequences shaped like litefactory's own
`lf:status` marker, and bidirectional-override runes.

SEC-5 requires `internal/report` to re-apply the restriction locally rather than
trust whatever validated it upstream, and `reply.go` already shows exactly how,
twice: `agentNamePattern`, checked again at the point of echo; and `commandSpan`,
which **stops at the first disallowed rune rather than skipping it**, because
*"skipping characters would let `/ev`il <!-- lf:status -->` be reassembled into
something that looks like a real command."* The conflict list needs the same
treatment, with one strengthening:

- Hold a local path pattern in `internal/report` and **reject** any path that
  fails it, replacing it with the count of rejected paths. Do not sanitise and
  print a salvaged prefix — a truncated path is misinformation, and a filename
  is not a value worth reconstructing.
- Do **not** rely on git's own quoting to make a path safe for markdown. Quoting
  and escaping are different problems.
- Truncation at 20 with a remainder count is a display rule, not a security
  rule. Both are needed.
- The refusal wording belongs in a package-level constant next to
  `onlyCommandSentence` / `toolsRefusalSentence` / `agentRefusalSentence`, which
  exist *"so three refusals must not drift into three different accounts of the
  same rule."* Four now.

**S-4 — merge identity.** `git merge --no-ff` creates a commit, so `MergeBase`
needs the same prelude `Commit` uses (`internal/git/commit.go:160-165`):
`-c user.name= -c user.email= -c commit.gpgsign=false`, with `Author` passed in
rather than inherited, because the daemon's git child gets a deliberately
minimal environment. Omitting `commit.gpgsign=false` means every operator with
global signing configured gets a merge that fails — or worse, hangs for a
passphrase against `GIT_TERMINAL_PROMPT=0` until the 60s deadline kills it,
leaving a half-merged worktree.

**S-5 — the rollback guarantee (US-10, AC-13).** "Byte-identical to before the
run" must be an enforced invariant, not an aspiration. The shape:

1. Refuse to merge unless the worktree is clean — `Status` already exists and
   `WorktreeDirtyError` is already the typed-error precedent.
2. Capture the branch SHA with `RevParse` immediately before the merge.
3. On any failure, `merge --abort`.
4. **Verify**: `RevParse` again and compare to the captured SHA. If it does not
   match, the outcome is `failed` and the comment says so. A rollback that
   silently did not roll back is the worst outcome this feature can produce.

**The obvious wrong turn, named so nobody takes it:** do not add `Reset` to make
step 4 self-healing. `Reset` is on `TestCloneCannotMutateTheRegisteredClone`'s
forbidden list and SEC-7 names it explicitly. Detect and report; do not add a
mutator to recover from a mutator. Note also that `merge --abort` exits non-zero
when no merge was in progress — classify that, do not read it as a failed
rollback.

**S-7 — the widened injection window.** Before this feature, base content reached
the agent once, at worktree creation. After it, *every* run pulls in whatever has
landed on base since. An injection payload committed to base now reaches a coding
agent automatically, with no one commenting on the issue. This is an accepted
consequence of the feature, not a defect — but it is the reason US-3's disclosure
requirement is load-bearing rather than cosmetic, and it belongs in `syncBase`'s
doc comment so the next person to widen it knows what they are widening.

**S-9 — bounding.** A conflicted merge can name tens of thousands of paths. Cap
the slice where it is parsed in `internal/git`, not where it is rendered, so an
unbounded allocation never happens. Confirm the conflict list is **not**
persisted — the spec's four new columns do not include it, which is correct.

---

## Implementation Order

Seven phases. Phases 2–5 are independent of each other and can run in parallel
once phase 1 lands.

0. **Probe** — verify `merge-tree` behaviour and git version floor (EXT-1). Cheap
   and blocking: the exit-code and output-shape answers determine phase 1.
1. **`internal/git`** — the three methods, `ErrMergeConflict`, timeouts, doc
   updates. Everything else depends on this and nothing depends on anything else.
2. **`internal/config`** — three keys. Independent.
3. **`internal/store`** — migration 0005 and four `Run` fields. Independent.
4. **`internal/report`** — `Sync` block and `ReplyRefusesSync`. Independent.
5. **`internal/daemon`** — the `lf:sync` lever. Independent. **Must land no later
   than phase 6** (see A-14).
6. **`internal/turn`** — `syncBase`, the outcome plumbing, `replyKind()`, the
   `gate_test.go` registry edit. The convergence point.
7. **Verification** — rollback proof and the concurrency test that closes US-6.

---

## Task Hints

Per story. Types: `data | api | logic | infra | test`. No task exceeds L.

### US-1 — Measure behind/ahead before every run

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-1.1 | Probe `merge-tree`/`rev-list` behaviour, record version floor (EXT-1) | infra | — | S |
| T-1.2 | `Divergence` type + `Worktree.Divergence` via `rev-list --left-right --count` | api | T-1.1 | M |
| T-1.3 | Add `divergenceTimeout` (30s) to `exec.go`'s timeout block | infra | T-1.2 | XS |
| T-1.4 | Unit tests: ahead/behind parsing, unknown ref, zero-value refusal | test | T-1.2 | M |

**Architectural hints**

- **Pattern:** `Fetch` in `internal/git/clone.go` is the template — screen the
  arguments, call `r.run` with a named timeout constant, map exit codes with
  `classify`. `revParse`'s shared-helper shape is the model for building the
  `base...branch` range internally.
- **Package placement:** new file `internal/git/merge.go`. Not `clone.go` — this
  is a coherent feature surface and deserves its own file, the way `commit.go`
  and `push.go` do.
- **Key interfaces:** method on `*Worktree` (DEV-2), `ctx` first (CONC-5),
  failures are `*CommandError` classified to a sentinel.
- **Integration points:** `Divergence` is the only one of the three that is
  always called, and its result populates `SyncBehind`/`SyncAhead` even when the
  outcome is `up_to_date`.
- **Watch out for:** `--left-right --count` output order is `<left> <right>` and
  which side is "ahead" depends entirely on which side of `...` you wrote. Pin it
  with a test that would fail if the two were swapped — a silently transposed
  pair produces a plausible-looking wrong number forever. Screen both refs with
  `checkName` (S-1) and pass `--end-of-options`.
- **NOT in scope:** no merging, no preview, no config reading, no persistence.

### US-2 — Auto-merge base into the task branch when the merge is provably clean

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-2.1 | `MergePreview` type + `Worktree.MergePreview` via `merge-tree --write-tree` | api | T-1.1 | M |
| T-2.2 | `MergeResult` type + `Worktree.MergeBase` via `merge --no-ff --no-edit` | api | T-2.1 | L |
| T-2.3 | `ErrMergeConflict` sentinel + exit-code classification | api | T-2.2 | S |
| T-2.4 | Rewrite `Worktree`'s doc comment for the third mutator | api | T-2.2 | XS |
| T-2.5 | `syncBase` in `prepare.go`, called last inside `prepareClone` (DEV-1) | logic | T-2.2, T-8.1 | L |
| T-2.6 | Register `syncBase` in `gate_test.go`'s `gated` map (DEV-5) | test | T-2.5 | S |
| T-2.7 | Unit tests: clean merge, conflicted preview, cap, sync-off, up-to-date | test | T-2.5 | L |

**Architectural hints**

- **Pattern:** `MergeResult.Merged` follows `CommitResult.Committed` exactly, and
  for the same documented reason — *"a run that changed nothing is a normal
  outcome; an error would push every caller into treating a successful no-op run
  as a failure."* There must be no "nothing to merge" sentinel. Likewise
  `syncBase` returns a value, and `*failure` only when the world is broken
  (CONC-3).
- **Package placement:** `MergePreview`/`MergeBase` in `internal/git/merge.go`.
  `syncBase` is an unexported function in `internal/turn/prepare.go`, beside
  `fetch`/`freezeSlug`/`ensureWorktree`, and called from nowhere else.
- **Key interfaces:** `syncBase` returns a sync outcome struct that `preparation`
  carries; it does not write to the store and does not render anything.
- **Concurrency/context:** inside `prepareClone`'s existing acquisition, **after**
  `ensureWorktree` (DEV-1). No new `Acquire`, no new `release`, no
  `context.WithTimeout` wrapper, no detached context for cleanup. Read
  `shaBefore` after this step.
- **Integration points:** consumes `config.Settings` (US-8) and the label decision
  (US-7); produces the outcome US-3/US-4 render and US-9 persists.
- **Watch out for:** `merge --no-ff` creates a commit, so it needs `Author` and
  the `-c commit.gpgsign=false` prelude (S-4). `merge-tree --write-tree` writes
  objects even on the conflict path (S-3). Do not let `syncBase` grow a second
  gate acquisition — `gate_test.go` will catch it, and the reason it will catch
  it is the point.
- **NOT in scope:** rendering, persistence, the label, the config keys, rollback
  verification (US-10).

### US-3 — Disclose every automatic branch change in the run comment

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-3.1 | `report.Sync` struct + `report.Run.Sync *report.Sync` (nil renders nothing) | api | — | S |
| T-3.2 | Renderer for the five outcomes, including `skipped` (A-7) | logic | T-3.1 | M |
| T-3.3 | Golden-file tests for all five outcomes | test | T-3.2 | M |

**Architectural hints**

- **Pattern:** an optional pointer field that renders nothing when nil is the
  existing shape for "this run had nothing to say about X". Keep it; do not
  introduce an `Outcome == ""` sentinel check at the render site.
- **Package placement:** `internal/report/run.go` and its `testdata/`.
- **Key interfaces:** `internal/report` renders only — no network, no DB, and it
  cannot import `internal/turn`. The struct is populated by `turn`.
- **Watch out for:** `skipped` must render, not fall through to silence — that is
  the case where the run's output stands on old ground and the human most needs
  telling (A-7). Name the base branch and the commit SHA for `synced`, so the
  human can find what changed under them.
- **NOT in scope:** the conflict list rendering (US-5) and the refusal comment
  (US-4).

### US-4 — Refuse the run and name the conflicting files

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-4.1 | `ReplyRefusesSync` const, `String()`, `valid()`, `RefusesSync()`, `body()` case | api | — | M |
| T-4.2 | Refusal-sentence constant beside the existing three | api | T-4.1 | XS |
| T-4.3 | `replyKind()` case in `internal/turn/report.go` | logic | T-4.1 | S |
| T-4.4 | `blocked` takes the `*failure` path; claude is not exec'd | logic | T-2.5, T-4.3 | M |
| T-4.5 | Tests: refusal posts, run does not exec, branch untouched | test | T-4.4 | M |

**Architectural hints**

- **Pattern:** follow `ReplyAgentSwitchRefused` end to end — it is the closest
  existing analogue (a refusal that names a resolved value). Adding a kind means
  touching the const block, `String()`, `valid()`, a `Refuses…()` predicate and
  `body()`; missing one leaves a kind that renders as nothing.
- **Package placement:** `internal/report/reply.go` for the kind,
  `internal/turn/report.go:696` for the mapping.
- **Key interfaces:** the enum is `report`'s own, deliberately, because
  `internal/turn` depends on `report` and the arrow cannot reverse.
- **Watch out for:** this refusal *does* need a field for conflicting paths, which
  makes it the first refusal that carries attacker-influenced text. That is
  allowed only with S-2's local re-validation in place — it is not the "no field
  for the text to travel in" pattern, so the protection has to be explicit
  instead of structural. Blocking must not be reachable when the `lf:sync=off`
  label is present (A-14).
- **NOT in scope:** the wording and path handling of the list itself (US-5).

### US-5 — Plain-language conflict report, no git jargon

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-5.1 | Local path pattern + reject-don't-salvage span helper (SEC-5 / S-2) | logic | T-4.1 | M |
| T-5.2 | Truncate at 20 with remainder count; count rejected paths separately | logic | T-5.1 | S |
| T-5.3 | Plain-language wording: what happened, what to do, how to override | api | T-4.2 | S |
| T-5.4 | Tests: markdown/HTML-comment/bidi payloads in paths, oversized lists | test | T-5.1 | M |

**Architectural hints**

- **Pattern:** `commandSpan` and `agentNamePattern` in `internal/report/reply.go`,
  copied in spirit — validate again *here*, at the point of echo, and stop at the
  first disallowed rune rather than skipping it.
- **Package placement:** `internal/report`. The path hygiene lives at the render
  boundary, not in `internal/git`. `internal/git` may report what git said;
  `internal/report` decides what is safe to sign litefactory's name to.
- **Watch out for:** the wording must tell the human the three things they
  actually need — the branch is behind, these files clash, and `lf:sync=off` will
  proceed anyway. Without the third, the refusal is a dead end (A-14). Avoid
  "merge-tree", "three-dot", "fast-forward" — the story is explicit about jargon.
- **NOT in scope:** the enum plumbing (US-4).

### US-6 — Sync locking that does not scale conflicts with concurrency

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-6.1 | Concurrency test: N turns, one clone, N branches, no interference | test | T-2.5 | L |
| T-6.2 | Document in `syncBase`'s doc why the gate stays keyed on `repo.Path` | api | T-2.5 | XS |

**Architectural hints**

- **Read DEV-3 before starting.** This story requires no new locking. Re-keying
  `CloneGate` to a branch would reopen the fetch/resolve/add window that
  `prepareClone` exists to close, and would be wrong on the facts: the measured
  contention is per-clone.
- **Pattern:** `internal/turn/gate_test.go` and `internal/daemon/clock_test.go`
  are the style. This test proves behaviour under parallelism; `gate_test.go`
  already proves the structure.
- **Watch out for:** a test that passes because it never actually overlapped. Make
  the turns genuinely concurrent and assert each branch ends at its own expected
  SHA, not merely that nothing errored.
- **NOT in scope:** any change to `CloneGate`, `internal/daemon`'s registry, or
  the `sections` map.

### US-7 — `lf:sync=off` to opt one issue out

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-7.1 | `syncLabelPrefix` + `resolveSync` on the agent/mode ladder (DEV-4) | logic | — | M |
| T-7.2 | Thread the decision into `turn.Request` and on to `syncBase` | logic | T-7.1 | S |
| T-7.3 | Tests: absent, `off`, `on`, miscased prefix, duplicated, bad value | test | T-7.1 | M |

**Architectural hints**

- **Pattern:** `resolveAgent` in `internal/daemon/agent.go`, rung for rung. Not
  `legacyToolsLabel` — see DEV-4; that label is dead and refused, and copying it
  produces the opposite of a lever.
- **Package placement:** `internal/daemon`. Labels are a per-tick fact and the
  daemon reads the tick's fresh snapshot; nothing is cached and no API call is
  made for them.
- **Key interfaces:** the decision travels on `turn.Request` alongside the mode
  and agent decisions. `internal/turn` never reads a label.
- **Watch out for:** `off` must disable **both** the merge and the block, or it is
  not an escape hatch (A-14). Ambiguity (two labels) is refused, never
  first-wins. A malformed refusal carries no field for the label text (SEC-5).
  **Sequencing: this must land no later than US-2/US-4.**
- **NOT in scope:** config defaults (US-8).

### US-8 — Config-governed defaults with a size cap

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-8.1 | Three fields on `Settings` + `Defaults()` + named default constants | api | — | S |
| T-8.2 | Three lines in `template.go`, comments included (CFG-3) | api | T-8.1 | S |
| T-8.3 | Validation joins the aggregate; negative is an error, never clamped | logic | T-8.1 | S |
| T-8.4 | Doc comments: 100 is a judgement not a measurement; `0` means no cap | api | T-8.1 | XS |
| T-8.5 | Tests incl. `template_test.go` drift check | test | T-8.3 | M |

**Architectural hints**

- **Pattern:** the attachment ceilings in `config.go` are the closest existing
  shape — named `Default*` constant, field in `Defaults()`, line in `template.go`
  with a `# valid range:` comment, entry in the aggregate validation.
- **Package placement:** `internal/config/config.go` and `template.go` only.
  stdlib + `yaml.v3`; `purity_test.go` enforces it.
- **Watch out for:** CFG-3 — `template_test.go` fails if the generated YAML and
  `Defaults()` drift, and it fails *loudly by design*. CFG-6 — a negative
  `auto_sync_max_behind` is a config error, not something to clamp to zero. CFG-5
  — validation joins the aggregate so an operator sees every problem at once, not
  the first one. `bool` defaults of `true` need care under defaults-by-decode
  (CFG-2): a decoded `false` must survive, so verify the decode path cannot
  overwrite an explicit `false` with the default.
- **NOT in scope:** consuming the values (US-2).

### US-9 — Every new git operation stays behind the type boundary

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-9.1 | Migration `0005_run_sync.sql` (four columns, NOT NULL DEFAULT) | data | — | S |
| T-9.2 | Four fields on `store.Run` + accessors, `ctx` first (CONC-5) | data | T-9.1 | M |
| T-9.3 | Persist the outcome on the run row under `BEGIN IMMEDIATE` | data | T-9.2, T-2.5 | M |
| T-9.4 | Confirm `surface_test.go` and `exec_internal_test.go` pass unchanged | test | T-2.2 | XS |
| T-9.5 | Store round-trip tests incl. migration from a 0004 database | test | T-9.3 | M |

**Architectural hints**

- **Pattern:** `0004_mode_selections.sql` is the template — plain DDL, one
  transaction, version from the filename prefix. No migration library; that
  dependency is explicitly rejected.
- **Package placement:** `internal/store/migrations/0005_run_sync.sql` and
  `internal/store/runs.go`.
- **Key interfaces:** T-9.4 is the story's real acceptance test. With DEV-2's
  placement, `surface_test.go` passes **unchanged** — `Clone`'s method set does
  not move, S-6 is not amended, and AC-4 and AC-5 are both satisfied without
  editing a TEST-10 test. If a change to `surface_test.go` becomes necessary,
  that is the signal that the placement decision was reversed, and an
  `.aiteam/AC_AMENDMENTS.md` entry becomes mandatory before the edit.
- **Watch out for:** `NOT NULL DEFAULT ''` / `DEFAULT 0` on every added column so
  existing rows need no backfill. Follow the package's established NULL-mapping
  conventions rather than inventing one for these four. Do not persist the
  conflict list (S-9).
- **NOT in scope:** rendering.

### US-10 — A failed or refused sync leaves the branch byte-identical

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-10.1 | Pre-merge clean check + SHA capture (S-5 steps 1–2) | logic | T-2.2 | S |
| T-10.2 | `merge --abort` on failure; classify "no merge in progress" | logic | T-10.1 | M |
| T-10.3 | Post-rollback SHA verification; mismatch ⇒ `failed`, reported | logic | T-10.2 | M |
| T-10.4 | Timeout path: `MERGE_HEAD`/`index.lock` handling, `TimedOut` preserved | logic | T-10.2 | M |
| T-10.5 | Tests: no `MERGE_HEAD` left, branch at original SHA, push refspec | test | T-10.3 | L |

**Architectural hints**

- **Pattern:** `WorktreeDirtyError` is the typed-error-unwrapping-to-a-sentinel
  precedent (STYLE-5) for the pre-merge clean check.
- **Package placement:** `internal/git/merge.go`. The rollback is `internal/git`'s
  responsibility — `internal/turn` must not be able to observe a half-merged
  worktree at all.
- **Concurrency/context:** the rollback may run under an already-cancelled
  context. Do **not** create a detached context to work around that. The
  verification step (T-10.3) is what makes a rollback that could not run
  *detectable*, which is the guarantee US-10 actually needs — an undetected
  failed rollback is far worse than a reported one.
- **Watch out for:** **do not add `Reset`.** It is on
  `TestCloneCannotMutateTheRegisteredClone`'s forbidden list and named in SEC-7.
  Detect and report; do not add a mutator to recover from a mutator. `merge
  --abort` exits non-zero when no merge was in progress — classify it rather
  than reading it as a failed rollback. `classify` must never reclassify a
  timeout: *"git could not read the remote" and "git never answered" are
  different problems and must stay distinguishable.*
- **NOT in scope:** the rendering of `failed` (US-3).

---

## Out of Scope for This Architect

Nothing in this feature falls outside Go backend architecture. There is no
frontend contract and no infrastructure topology here; the git-version floor is
the closest thing to an operational concern and it is handled inside the Go
binary, so it stays mine.

## Notes for the Product Owner

- No task was added or removed. Where a story's shape changed (US-6 from
  construction to verification), the change is recorded as a deviation rather
  than absorbed silently.
- The A-14 sequencing constraint — US-7 landing no later than US-2/US-4 — is a
  release-content decision, not just an ordering preference. Shipping the block
  without the escape hatch would put an un-overridable refusal in front of real
  work.
