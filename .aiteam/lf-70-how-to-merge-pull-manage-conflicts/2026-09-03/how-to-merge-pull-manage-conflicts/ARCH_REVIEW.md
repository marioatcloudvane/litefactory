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

---

# Amendment Review — US-11 / US-12
# Architect: Go Architect
# Date: 2026-09-06
# Scope: the two stories added to `feature-spec.md` on 2026-09-06 only.
#        US-1..US-10 and their sub-issues (`US-1.md`..`US-10.md`) are unchanged
#        by this section and were not re-decomposed.

## Verdict

**Approved with a sequencing deviation.** US-11 is buildable on precedent that
already exists in this codebase and needs no new architecture beyond one narrow
model call, one store table and one reply shape. US-12 is not plumbing: it
introduces the first path in litefactory where model-produced text becomes
committed bytes, and it needs an agentless turn shape that does not exist today.
Neither story fits Phase 6 or Phase 7 of the approved shipping order. See
**D-2**.

## Corrections to the plan record

Two statements in the 2026-09-05 artifacts are wrong or stale. Both are
corrections of record, not design changes.

**C-1 — migration numbers (resolves ODQ-20).** `plan_brief.md` Cross-Story
Constraints says the new migration is `0005_run_sync.sql` and that "0004 is the
current head", and the Tests bullet says "Migration 0005 applies cleanly on top
of 0004". Verified this session by listing
`internal/store/migrations/`: the files present are `0001_init.sql`,
`0002_session_backlog.sql`, `0003_agent.sql`, `0004_mode_selections.sql`,
`0005_schedules.sql`, `0006_schedule_block_replies.sql`. Head is **0006**;
`0005` is taken by another feature that merged since.

- US-9's run-sync columns become **`0007_run_sync.sql`**.
- US-11's proposal table becomes **`0008_conflict_proposals.sql`**.
- Both numbers MUST be re-confirmed by listing the directory at implement time,
  not read out of this document. `main` moves; the head that mattered on
  2026-09-05 was wrong within a day.

**C-2 — `prepareClone` step order.** Verified first-hand against current
`internal/turn/prepare.go:376-410`. The real order inside the single
`CloneGate.Acquire` is:

    CloneGate.Acquire -> fetch -> freezeSlug -> ensureWorktree

`feature-spec.md`'s API/Interface Changes table says `syncBase` is gained
"between `fetch` and `freezeSlug`". That is wrong and is the sentence to fix.
`plan_brief.md` DEV-1 — `syncBase` runs **last, after `ensureWorktree`** — is
correct and stands: `MergeBase` is a `*Worktree` method and no worktree exists
until `ensureWorktree` has run with the branch name `freezeSlug` produced. Also
note the doc comment at that site: the context is passed through untouched so
gate-wait time is not charged against the fetch's 60s or the push's 120s
(AC-PAR-3.6). Nothing added by this amendment may wrap that section in
`context.WithTimeout`.

## US-7 — endorsed, without reservation

The documentation-only note "US-7 reconsidered and kept (2026-09-06)" records
the right outcome. `lf:sync=on|off` stands exactly as specified in
`plan_brief.md` Shared Decisions and `US-7.md`. Three reasons, one of which is
new since 2026-09-05:

1. **It adds a pattern instance, not a pattern.** `lf:sync=` is copied rung for
   rung from `agentLabelPrefix` and `modeLabelPrefix` in `internal/daemon` —
   both live. The marginal architectural cost is close to zero.
2. **Blast-radius mismatch.** Without it, the only escape from a false-positive
   conflict block is flipping `block_run_on_conflict` for the entire repo: a
   per-issue problem answered with a repo-wide switch. That is the wrong shape
   for a lever whose whole purpose is to unstick one issue.
3. **New, and decisive:** US-7 is now the kill switch for US-11 and US-12 as
   well. Per `feature-spec.md`'s own edge case, an issue labelled `lf:sync=off`
   is never `blocked`, so no proposal is ever produced for it and no
   confirmation is ever possible. Removing the label would leave an owner with
   no per-issue way to keep repository file content out of a model prompt. The
   amendment makes US-7 **more** load-bearing, not less.

The standing constraint "US-7 must ship no later than US-2/US-4" holds and is
now stronger: it must also ship no later than US-11.

No dissent.

## System Overview delta

### Where the two stories sit

US-11 attaches to the **end of the blocked path inside `internal/turn`**. The
block is discovered by `syncBase`, which runs under the CloneGate as the fourth
step of `prepareClone`. Collecting and capping the conflicting hunks is a
filesystem read against the prepared worktree and happens there. The model call
does **not**: see the concurrency note below. The proposal row is written
through the store handle `turn` already holds, and the suggestion is rendered
through a new `report.ReplyKind`, which means a new case in
`replyKind(kind CommandKind) (report.ReplyKind, bool)` at
`internal/turn/report.go:722` — `internal/report` cannot import `internal/turn`,
so a new reply shape is always two edits, never one.

US-12 attaches in two places at once, and that split is the whole reason it is
an L. Confirmation **arrives as a comment**, so it is dispatched by
`internal/daemon` on the comment path, exactly where `handleProposalControl`
sits today. But application **needs a prepared worktree under the CloneGate**,
which is `internal/turn`'s job and nothing else's. So the confirmation comment
must start a turn that runs no agent.

### The precedent, and what it does not cover

`internal/daemon/proposals.go` is the pattern to copy and is cited in full
below (ODQ-24). It supplies: TTL sweep before dispatch (`proposalLifetime =
7 * 24 * time.Hour`, which matches `conflict_proposal_ttl_hours: 168` exactly),
`ownedProposal` gating on `SessionID` **and** `c.Author.Login`, status guards on
every transition, `validatedProposalItems` re-validating generator output
locally, and `ReplyScheduleProposalFailed` returned as a **value** when the
advisor is nil or the model fails (CONC-3).

What it does not supply is the second half of US-12: no schedule proposal ever
touches a worktree, an index or a commit. Accepting a schedule writes rows.
Confirming a conflict resolution writes **files**. The precedent covers the
lifecycle and the ownership check; it covers none of the mutation.

### New components introduced

| Component | Package | Responsibility |
|---|---|---|
| `ConflictResolutionRequest` / `ConflictResolution` / prompt builder / strict parser | `internal/agent` (new file, sibling of `proposal.go`) | The bounded, tool-free model call: prompt in, structured output out. Carries no issue handle, worktree, tool permissions or persistence capability. |
| `turn.ConflictAdvisor` (narrowing interface) | `internal/turn` | One method, declared by the consumer, satisfied by a `Runner` type-asserted out of `turn.RunnerSet` by `agent.Kind` — the `scheduleAdvisor` shape from `internal/daemon/proposal_adapter.go:13-15`. |
| `store.ConflictProposal` + lifecycle calls | `internal/store` | The pending/confirmed/rejected/expired/superseded row and its transitions. Own type and own status vocabulary; not shared with `store.ScheduleProposal`. |
| Confirmation grammar | `internal/turn/command.go` (+ a sibling parser) | A litefactory-owned control command in the `KindSchedule` mould: separate grammar, canonical lowercase token, restricted charset. |
| `confirmedProposal` (unexported value type) | `internal/turn` | Makes the confirmation gate structural — see S-12. |
| Contained-write / stage / complete-merge mutators | `internal/git`, on `*Worktree` | The only place resolved content reaches the filesystem and the index. |
| `ReplySuggestsResolution`, `ReplyAppliesResolution`, refusal kinds | `internal/report` | Rendering, under SEC-5 reject-don't-salvage discipline. |

### Concurrency: the model call must not hold the clone gate

This is the single most important structural constraint in the amendment and it
is not stated in the spec.

`syncBase` runs **inside** `prepareClone`'s CloneGate acquisition. A model call
is seconds to tens of seconds. Making it there would serialise every other issue
on that clone behind a network round-trip to a model, for as long as the model
takes, on every conflicted run. The gate exists to keep concurrent issues from
colliding in one clone, not to queue them behind inference.

Required shape:

1. Inside the gate: `syncBase` detects the block, `MergePreview.Conflicts` is
   known, the hunks are read from the worktree, capped and filtered
   (filesystem-only, fast, bounded).
2. Gate released, `prepareClone` returns as it does today.
3. Outside the gate: the model call, the local re-validation, the store write,
   the comment.

The capped payload is a plain value carried out of the gated section. The call
carries the turn's `context.Context` — a daemon shutdown or a deadline cancels
it. Never `context.Background()`; a detached model call would outlive the turn
that reported the block.

Give the model call its own deadline in the existing style, distinct from the
git deadlines (`Divergence` 30s, `MergePreview` 60s, `MergeBase` 60s). It is a
different kind of wait and must not inherit a git budget.

## Shipping order — D-2

**These two stories do not slot into Phase 6 or Phase 7. Do not force them
there.** Stated plainly, as asked.

Phase 6 is the `internal/turn` convergence for the ten-story feature: the phase
where `syncBase` is wired for the first time and the outcome vocabulary is
proven end to end. Phase 7 is verification. Putting US-11 in Phase 6 would put
the first model call and a new store table into the same change that first wires
sync, and would make the ten-story feature's ship date depend on a security
review that has not happened. Putting US-12 anywhere before US-11 has been
observed against a real conflict would mean building an apply path for
suggestions nobody has read yet.

Recommended extension of the approved order:

| Phase | Content | Gate to enter |
|---|---|---|
| 0-7 | Unchanged. US-1..US-10 ship as planned. | — |
| **7.5** | **ODQ-28 security review pass** (not a story; a blocking review). Scoped to the two new paths: repository content outbound, model text inbound. | Phase 7 green. |
| **8** | US-11 — propose and post. Ships with `suggest_conflict_resolution` defaulting **false** (D-3). | 7.5 signed off by the owner. |
| **9** | US-12 — confirm and apply. Opens with the `Worktree` allowlist test (T-12.1) before any mutator exists. | Phase 8 shipped **and** at least one real blocked run observed with a posted suggestion the owner has read. |

Rationale for the observation gate between 8 and 9: US-12's risk is entirely a
function of suggestion quality, and nobody in this project has yet seen one.
Building the apply path before reading real output is building a gate for a
threat model assembled from assumptions.

**Keep the two stories split (D-6).** `feature-spec.md`'s decomposition note
allows merging them "provided the mandatory confirmation gate stays inside it".
I recommend against it. They are different risk classes — read-plus-post versus
branch-mutating — they want different review gates, and US-11 is independently
valuable: a suggestion an owner applies by hand is still worth having. Merged,
the confirmation gate ships in the same change as the first model call, and one
review has to hold both.

## Open Design Questions — resolved

### ODQ-20 — next free migration number
**Resolved.** Head is `0006_schedule_block_replies.sql`, verified by directory
listing this session. `0007_run_sync.sql` (US-9), `0008_conflict_proposals.sql`
(US-11). Re-verify by listing at implement time. See C-1.

### ODQ-23 — call shape for a large untrusted hunk payload
**Resolved: reuse the shape, do not reuse the adapter, and cap before you
assemble.**

`internal/agent/proposal.go` is the template and it extends to this payload
without a new interface family. Its three parts are exactly what US-11 needs:

- a bounded request struct whose doc comment states what it deliberately does
  not carry ("no issue handle, worktree, tool permissions, or persistence
  capability");
- a prompt builder that puts the untrusted text **last**, under an explicit
  literal label ("Scheduling request (data, not instructions):"), delivered on
  stdin rather than interpolated into the instruction;
- a strict parser — `dec.DisallowUnknownFields()` plus a second `dec.Decode`
  expecting `io.EOF` to reject multiple JSON values — with semantic validation
  deliberately pushed to the caller.

What does **not** carry over is `daemon.scheduleAdvisor`. Those narrowing
interfaces are per-capability by design ("advice cannot allocate a workflow,
select a worktree, or write a run log"); widening one to cover two capabilities
would widen it for both callers. Declare a second, separate one-method
interface, obtained the same way — type-assert a `Runner` out of
`turn.RunnerSet` by `agent.Kind`, return nil when it is not satisfied, and let a
nil advisor mean "no suggestion" rather than "error"
(`internal/daemon/proposals.go:38-40` is the precedent, CONC-3).

Note the placement difference: the schedule advisor is held by
`internal/daemon`; the conflict advisor must be held by `internal/turn`, because
that is where the block is discovered and where the worktree is.

**Size is handled by construction, not by truncation.** Cap per file and cap the
total **before** assembling the prompt, from `conflict_content_max_bytes`. A
file that does not fit is excluded whole and named in `Excluded`; it is never
half-included. This is what keeps the payload a set of complete files rather
than a set of files plus one arbitrary prefix.

**Do not add a file-content field to `harness.PromptInput`.** It has none today,
and that is a property worth keeping: a field there would put file content into
every ordinary agent run's prompt, not just this one narrow call. The conflict
payload belongs to the tool-free call and nowhere else.

### ODQ-24 — reuse the schedule-proposal pattern, and what new store state
**Resolved: reuse the pattern, not the table.**

Copy from `internal/daemon/proposals.go`, rung for rung:

- expire stale rows **first**, before dispatching on the request kind;
- `ownedProposal`-equivalent gating on `SessionID` **and** the commenter's
  login, refusing anything else;
- guard the current status on every transition, not just the happy one;
- a `…Failed` reply returned as a value on every unhappy path (CONC-3);
- re-validate model output locally before persisting
  (`validatedProposalItems` is the precedent, and its discipline — never trust
  the generator's output shape — is the one that matters most here).

Do **not** reuse `store.ScheduleProposal`, its table, or its
`ProposalPending`/`ProposalAccepted` constants. A conflict proposal has a
different lifecycle (it gains `superseded` and `AppliedCommitSHA`, and it has no
items), and coupling the two would make every future schedule change a conflict
change. New table, new type, own status vocabulary.

New store state required: create-or-get, get, expire-by-cutoff, supersede-pending-
for-session, confirm, and mark-applied. **Every one of these is a read-then-write
and therefore runs under `BEGIN IMMEDIATE` on the single-connection pool** — in
particular supersede-on-new-proposal, which must be one transaction so two ticks
cannot leave a session with two pending proposals. Follow the NULL-mapping
convention already in the store: zero `time.Time` is NULL, plain `string` when
empty carries no meaning, and use pointers only for numbers whose zero is real.

**The pause does not exist and must not be invented.** There is no mid-run wait
anywhere in this codebase, and US-12 must not add one. Propose and apply are two
finished runs linked by a row. AC-28 asserts this and should be written early,
not last.

### ODQ-25 — net-new `internal/git` surface, and its own allowlist test
**Resolved: yes to both. On `Worktree`, never on `Clone`, and the test comes
first.**

The surface is genuinely required — verified: there is no scoped `git add
<path>`, no index-inspection call and no merge-completion primitive anywhere in
`internal/git` today. The classification for unmerged paths, however, already
exists and is dormant: `ChangeUnmerged` at `internal/git/status.go:41-44`,
assigned by `classifyStatus` at `status.go:131-139`. Use it rather than adding a
parallel notion.

Placement is already settled by evidence.
`TestCloneCannotMutateTheRegisteredClone` in `internal/git/surface_test.go`
lists `Add`, `Merge`, `Reset`, `Apply` and `Restore` among the methods `Clone`
must never have. Anything US-12 needs therefore sits on `*Worktree`, which is
also what DEV-2 already decided for `Divergence`/`MergePreview`/`MergeBase`, and
what SEC-7 requires (SEC-7 constrains the registered clone; it does not
constrain the worktree, which already carries `Commit` and `Push`). **No
`AC_AMENDMENTS.md` entry is needed for the git surface** — nothing here bends
SEC-7 or edits an existing design-change test.

Keep the surface as narrow as three calls: write the listed resolved paths,
stage exactly those paths, complete the in-progress merge. No general `Add`, no
`Checkout`, no `Reset`, no `Apply`, no `Restore` — and specifically never
`Reset` as a self-heal after a failed rollback (S-5 already says this; it
matters more now).

**Yes, it needs its own allowlist test, and it is a new test rather than an edit
to an existing one.** `Worktree` has no method-set allowlist today; it is
guarded only by `TestWorktreeHasNoExportedConstructionPath` and
`TestZeroValuesRefuseToRunGit`. That was defensible while only `Commit` and
`Push` mutated. The moment `Worktree` can write file content supplied — however
indirectly — by a model, its method set becomes a security boundary and must be
pinned the way `Clone`'s is:

- `TestWorktreeMethodSetIsTheAllowlist` — the exact method set, with the same
  failure message discipline as the `Clone` test ("if a new git command really
  belongs here, amend the allowlist first");
- a forbidden-mutator counterpart covering `Reset`, `Rebase`, `CherryPick`,
  `Clean`, `Stash`, `Checkout`, `Switch`.

**Write it in Phase 9 before the mutators exist** (T-12.1), pinning today's set.
Then adding each mutator is a deliberate, reviewed edit with a recorded reason
(TEST-10) rather than a test that quietly grew.

### ODQ-26 — trailer convention for a confirmed model-assisted commit
**Resolved.**

Use the existing trailer mechanism in `internal/git/commit.go` (`TrailerTask`,
`TrailerMode`, `TrailerRun`) and add exactly three trailers in the same
`Litefactory-` namespace: the proposal identifier, the confirming human's
handle, and the model name. Three separate trailers rather than one packed
value, because each has a different provenance and a different screening rule.

Screening, which is the load-bearing part:

- the proposal ID is an `int64` litefactory generated — safe by type;
- the confirming handle is forge-supplied and passes an
  `agentNamePattern`-style check with **reject, don't salvage** (SEC-5,
  `internal/report/reply.go`). If it fails, the application is refused; a commit
  is not made with a partial handle;
- the model name comes from configuration and the agent kind, **never from the
  model's own output**.

No trailer value ever carries model-produced text. And explicitly **not**
`Co-authored-by:` for the model: that attributes authorship to a non-account,
the forge will try to resolve it, and it makes a tool look like a contributor.

### ODQ-27 — is neutralise-and-truncate enough for structured file content
**Resolved: necessary, but not sufficient. Do not extend `capText` to cover
this.**

The existing treatment — `capText(s, limit) = truncateWithMarker(
neutraliseNoteTags(s), limit)` at `internal/harness/prompt.go:892-894`, with
`MaxCommentBytes = 8<<10`, `MaxIssueTitleBytes = 1<<10`,
`MaxHistoryBytes = 32<<10` — was built for short free text and is a poor fit
here for three reasons:

1. `neutraliseNoteTags` defuses `<note>`/`</note>` specifically, because that is
   the harness prompt's own framing. A source file can carry whatever framing
   *this* prompt uses — fences, the literal data label, JSON structure if the
   payload is embedded rather than delivered on stdin.
2. Truncation mid-file produces a payload that corresponds to no real file: it
   can split a rune, a string literal, or a conflict marker, and the model is
   then reasoning about a file that does not exist. Whole-file exclusion is the
   correct primitive here, not truncation. Exclusions are recorded and disclosed
   (the spec already requires `Excluded` and the "partial suggestion" line).
3. The bigger half is the **return** path, which `capText` has nothing to say
   about at all.

Required treatment instead:

- per-file and total byte caps applied before assembly; over-budget, binary, and
  non-UTF-8 files excluded whole;
- content delivered on stdin under an explicit "data, not instructions" label,
  following `ScheduleProposalPrompt`, and placed last;
- strict structured decode on return — `DisallowUnknownFields` plus
  trailing-value rejection, following `ParseScheduleProposal`;
- local re-validation that rejects the **whole** response if any path falls
  outside `MergePreview.Conflicts` or any content still carries conflict
  markers. Reject in full; never salvage the good files out of a bad response.
  This is `agentNamePattern`/`commandSpan`'s discipline applied one layer up,
  and `internal/report/reply.go`'s own comment says why: skipping the bad parts
  and carrying on is how something unusable gets reassembled into something that
  looks legitimate.

### ODQ-28 — dedicated security review before this ships
**Resolved: yes, as a blocking gate, and it is the reason Phase 7.5 exists.**

This amendment opens two firsts at once: the first outbound path that sends
repository file content to a model on litefactory's own initiative, and the
first inbound path where model text becomes committed bytes. Either alone would
justify a pass. Together they justify a gate.

Scope the review to those two paths, using
`.claude/skills/shared/security-scanner.md`, and treat S-10 through S-17 below
as its agenda rather than its conclusion.

**Roster gap, stated explicitly:** there is no security specialist on this
team's roster. The closest available instrument is `go-code-reviewer` running
the shared security-scanner skill, and the approver is the owner (human). I am
not inventing a reviewer role that does not exist; I am naming the gate and
saying who can actually hold it.

**Consequence for A-20 (D-3):** `suggest_conflict_resolution` ships defaulting
**`false`**, flipped to `true` only after the Phase 7.5 review signs off.
`feature-spec.md`'s A-20 explicitly delegates this call — "The architect or a
security review (ODQ-28) may flip this default" — so this is a decision the spec
authorises, not a deviation from it. The propose half writes nothing, which is
the argument for `true`; the counter-argument is that shipping a new outbound
content path on by default gives the review nothing to gate.

## Platform lens

Read against `.claude/skills/go/platform-constraints.md` and the constraints
already recorded in `plan_brief.md`.

- **No new dependency, anywhere.** Everything above is stdlib plus existing
  internal packages. In particular the JSON contract uses `encoding/json` the
  way `internal/agent` already does; no schema library, no validation library.
- **SQLite has one connection.** Every proposal status transition is a
  read-then-write under `BEGIN IMMEDIATE`. Supersede-and-create is one
  transaction, not two calls.
- **Migrations** `0007`/`0008` are embedded plain-DDL files, one transaction
  each, recorded in `schema_migrations`, `NOT NULL DEFAULT` on added columns, no
  backfill. Migration libraries remain a rejected dependency.
- **The live daemon runs from `dist/litefactory`.** Implementation never runs
  `make build`, writes into `dist/`, restarts the daemon, or edits
  `~/.litefactory/config.yaml`. Safe: `go build ./...`, `go test ./...`,
  `go vet ./...`, `gofmt -l .`.
- **launchd's child environment is minimal** (`childEnv()` passes only `PATH`,
  `HOME`, `SSH_AUTH_SOCK`, `GIT_TERMINAL_PROMPT=0`, and a BatchMode
  `GIT_SSH_COMMAND`). The US-12 application commit inherits no identity and no
  signing configuration, so it carries the same mandatory prelude as every other
  commit: `-c user.name= -c user.email= -c commit.gpgsign=false`. Omitting
  `commit.gpgsign=false` risks a hang against `GIT_TERMINAL_PROMPT=0` until the
  deadline kills it (S-4).
- **Deadlines.** The model call gets its own, distinct from the git budgets, and
  is made outside the CloneGate (see the concurrency note). No new
  `context.Background()` on any request path.
- **git ≥2.38** floor is unchanged; nothing in US-11/US-12 raises it. The
  merge-completion primitive is ordinary `git merge` machinery, not
  `merge-tree`.
- **No new goroutine is warranted by either story.** Both are sequential inside
  an existing turn. If one appears in review, ask what owns its lifetime.

## Security Findings

**Scope:** the two new paths only — repository file content leaving litefactory
for a model (US-11), and model text entering a commit (US-12). S-1..S-9 from the
2026-09-05 review are unchanged and still apply.

**Rulebook:** `.claude/skills/shared/security-scanner.md`, plus SEC-1..SEC-11 in
`.aiteam/APP_CONSTRAINTS.md`. Findings are forward-looking: none of this code
exists yet.

| # | Area | Status | Finding |
|---|---|---|---|
| S-10 | Outbound content | **High — superset of S-7, not a duplicate** | S-7 recorded (accepted, disclosed) that auto-sync widens the prompt-injection window into the *coding agent*, because base-branch content now reaches it every run. S-10 is a strictly larger surface: litefactory itself now reads conflicting file **content**, assembles it, and sends it to a model. Narrower in one dimension (the tool-free call has no tools, no worktree, no session to resume, so the model cannot act on an injected instruction) and wider in another (litefactory is the courier, and in US-12 the response is trusted enough to become file bytes). S-7's mitigation was disclosure; S-10's cannot be, because the return path writes. Mitigation is the ODQ-27 treatment in full. |
| S-11 | Inbound content | **High** | First path where model-produced text becomes committed bytes. Mitigations, all required together: reject the whole response on any out-of-list path or any remaining conflict marker; write only the paths in `ConflictProposal.Files`; re-verify `BaseSHA`/`BranchSHA` inside the gate immediately before writing; exactly one merge commit; US-10's rollback guarantee covers the failure path. |
| S-12 | Confirmation gate | **High** | The gate is the entire security model of US-12, and "mandatory and non-configurable" must be structural, not merely unconfigured. Make the applying code path reachable only from a function taking an unexported `confirmedProposal` value that only the confirmation validator can construct, so a missing check is a compile error rather than a missing `if`. AC-20 asserts the property; the type is what keeps the assertion cheaply true as the code grows. |
| S-13 | Path containment | **High — sharpest new edge** | Model-supplied paths reach the filesystem. Containment MUST be enforced by `*git.Worktree`, the type that owns the root, rejecting absolute paths, `..` traversal, symlink components, and any path not present in `ConflictProposal.Files`. Not by the caller, and not by string prefix comparison. Related to S-2 but distinct: S-2's sink is markdown, S-13's sink is the filesystem. |
| S-14 | Command grammar | Medium | The confirmation phrase is a new grammar any commenter can reach. Parse it like `KindSchedule` — separate parser, canonical lowercase token, restricted charset so a reply may quote it without smuggling markup (`internal/turn/command.go:85-90`) — and apply the ownership check *after* parsing. A non-owner confirmation must write nothing and must not move the status off `pending` (AC-21). Refusals never echo raw comment text. |
| S-15 | TOCTOU / replay | Medium | Base or branch can move between propose and confirm. Stored SHAs are the guard and MUST be re-checked inside the CloneGate at apply time, not before acquiring it. Double confirmation of the same proposal is a no-op reported as already applied — never a second commit (AC-24 + the spec's edge case). Supersede-on-new-proposal is one `BEGIN IMMEDIATE` transaction. |
| S-16 | Provenance rendering | Medium | The model name and confirming handle land in a commit trailer *and* in a comment. Both come from litefactory-controlled or pattern-screened values, never from model output; the handle is reject-don't-salvage (SEC-5). |
| S-17 | Persistence | Low — **amends S-9** | S-9 said the conflict-path list is never persisted to the store. US-11 changes that: `ConflictProposal.Files` and `Resolution` are persisted by necessity, since application happens in a later run. Record the reversal rather than leaving two contradictory statements on file. The *input* hunks are still never persisted — only paths, the resolution to be applied, and the SHAs. Cap `Resolution` at the store boundary as well as at the parse boundary. |

**Summary.** US-11 is a medium-risk story with a well-precedented mitigation set;
its risk is almost entirely "did the caps and the rejection actually get
written". US-12 is the high-risk story and carries four of the five High
findings. S-12 and S-13 are the two that should be designed first and reviewed
hardest: they are the difference between a feature that can only write what a
human approved, and one that can be talked into writing something else.

**Known and unchanged:** the Claude-mode `--allowedTools` Bash loophole is not
closed by this amendment either, and nothing in US-11/US-12 depends on it being
closed. The tool-free call has no tools at all, which is why it is the right
shape for this.

---

## US-11 — Suggest a resolution when a run is refused for a conflict

> As the issue owner, when litefactory refuses my run because the base branch
> conflicts with my task branch, I want it to suggest how the conflict could be
> resolved, so that I can judge the fix without reconstructing the conflict
> myself.

Produced only for a run whose sync outcome is `blocked`. Reads the conflicting
hunks, asks a narrow tool-free model call for a resolution, validates the answer
locally, stores it as a pending proposal, and posts it. **Writes nothing to the
branch, the index, or a commit.**

### Why This Matters

A block today ends with a list of file names and no way forward except opening
an editor. The suggestion turns a dead end into a decision. It also produces the
artefact US-12 needs: without a stored, validated proposal there is nothing for
a human to confirm.

### Architecture Notes

- **Pattern:** `internal/agent/proposal.go` end to end — bounded request struct,
  prompt builder placing untrusted text last under an explicit "data, not
  instructions" label and delivering it on stdin, strict parser with
  `DisallowUnknownFields` and trailing-value rejection, semantic validation left
  to the caller. Then `internal/daemon/proposals.go` for the lifecycle:
  expire-first, validate-locally, failure-as-a-value.
- **Package placement:** the model-call types in `internal/agent`; the
  narrowing interface, hunk collection, capping and orchestration in
  `internal/turn`; the row in `internal/store`; the rendering in
  `internal/report`. Nothing new in `internal/daemon` for this story.
- **Key interfaces:** a one-method `ConflictAdvisor` declared by `internal/turn`
  and satisfied by type-asserting a `Runner` out of `turn.RunnerSet` by
  `agent.Kind` — `internal/daemon/proposal_adapter.go:19-29` is the shape. A nil
  advisor means "no suggestion", not "error".
- **Concurrency/context:** collect and cap hunks **inside** `prepareClone`'s
  gate; make the model call **outside** it. Carry the turn's `context.Context`
  into the call; never `context.Background()`. Own deadline, not a git budget.
- **Integration point:** `syncBase` (DEV-1: the last step of `prepareClone`,
  after `ensureWorktree` — see C-2) produces the block and
  `MergePreview.Conflicts`. A new `report.ReplyKind` needs a matching case in
  `replyKind` at `internal/turn/report.go:722`; `internal/report` cannot import
  `internal/turn`, so it is always both edits.
- **Watch out:** (1) do not extend `harness.PromptInput` with a content field —
  that would put file content into every ordinary run's prompt; (2) exclude
  over-budget files whole, never truncate them; (3) reject a bad response in
  full — no salvaging the good files out of it; (4) a failed, disabled, timed-out
  or unusable-response model call must leave the US-4 refusal byte-identical
  plus one line, never degrade it; (5) migration number is `0008`, re-verified at
  implement time.
- **Depends on:** US-4 (the `blocked` outcome), US-5 (path rendering
  discipline), US-8 (config), US-9 (`0007` landing first). Phase 8; gated on the
  Phase 7.5 security review.
- **NOT in scope:** any write to the branch, index or a commit; the confirmation
  grammar; editing a suggestion; any second model call; a "reasoning" tier —
  this resolves to the default tier and adds no `config.Tier` value.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-11.1 | Migration `0008_conflict_proposals.sql` and the `store.ConflictProposal` type with its own status vocabulary | Code | US-9 (`0007`) | M |
| T-11.2 | Store lifecycle calls: create-or-get, get, expire-by-cutoff, supersede-pending-for-session — each one read-then-write under `BEGIN IMMEDIATE` | Code | T-11.1 | M |
| T-11.3 | Store tests: status transitions, TTL expiry, supersede leaves exactly one pending row, migration applies cleanly on the current head | Test | T-11.2 | S |
| T-11.4 | `internal/agent` conflict-resolution request/response types, prompt builder, strict parser | Code | — | M |
| T-11.5 | Parser tests: unknown fields, multiple JSON values, empty response, response carrying conflict markers | Test | T-11.4 | S |
| T-11.6 | Hunk collection and capping in `internal/turn`: per-file and total caps, whole-file exclusion for over-budget/binary/non-UTF-8, `Excluded` list | Code | US-4 | M |
| T-11.7 | Cap and exclusion tests, including AC-27's assertion on the cap and the treatment | Test | T-11.6 | S |
| T-11.8 | Local re-validation of model output: paths subset of `MergePreview.Conflicts`, no conflict markers, reject in full | Code | T-11.4 | S |
| T-11.9 | `ConflictAdvisor` narrowing interface and wiring from `turn.RunnerSet`; call sited outside the CloneGate | Code | T-11.4 | M |
| T-11.10 | `internal/report`: `report.ConflictProposal`, `ReplySuggestsResolution`, the `replyKind` case, rendering under SEC-5 discipline | Code | US-5 | M |
| T-11.11 | Report tests: rendering, refusal lines, `ReplyKind` enum exhaustiveness | Test | T-11.10 | S |
| T-11.12 | Config: `suggest_conflict_resolution` (default `false`, D-3), `conflict_proposal_ttl_hours`, `conflict_content_max_bytes` | Code | US-8 | S |
| T-11.13 | End-to-end blocked-run tests with a stubbed advisor: happy path, failure, disabled, unusable response, terminal state | Test | T-11.9, T-11.10 | M |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-11.1 | A blocked run with suggestions enabled posts one comment carrying the conflicting files, a per-file suggested resolution, the model name, a proposal ID and the confirmation phrase; the branch SHA is unchanged after the run (spec AC-17). |
| AC-11.2 | With the advisor stubbed to fail or time out, the run still produces the full US-4 refusal, writes no proposal row, and adds exactly one line saying no suggestion could be produced (spec AC-18). |
| AC-11.3 | With `suggest_conflict_resolution: false`, the blocked run is byte-identical in behaviour and comment content to the US-4-only path and makes no model call (spec AC-19). |
| AC-11.4 | A response naming a path outside `MergePreview.Conflicts`, and separately one containing conflict markers, each produce no proposal; both cases are covered (spec AC-26). |
| AC-11.5 | Conflicting content in the prompt is capped per file and in total per `conflict_content_max_bytes`; over-budget, binary and non-UTF-8 files are excluded whole and named in `Excluded`; a test asserts cap and treatment (spec AC-27). |
| AC-11.6 | The blocked run reaches a terminal state after posting; no code path sleeps, polls or waits for a reply (spec AC-28). |
| AC-11.7 | A test asserts the model call is not made while the CloneGate is held. |
| AC-11.8 | A new proposal for a session supersedes any earlier pending one in a single transaction; a session never has two pending proposals. |
| AC-11.9 | No test or production path writes conflicting file *content* to the store. |

---

## US-12 — Apply a confirmed resolution and complete the merge

> As the issue owner, after reading a suggested resolution, I want to confirm it
> in a comment and have litefactory apply it and finish the merge, so that I do
> not have to reproduce the fix by hand — and I want nothing applied unless I
> said so.

Two separate turns linked by a stored row. The blocked run ends after posting.
A later, explicit confirmation comment from the owner — naming the proposal ID
and carrying the confirmation phrase — starts a turn that runs no agent, applies
only the listed files, completes the merge, and discloses everything.

### Why This Matters

This is the story that turns a suggestion into a resolved branch, and it is the
only story in the whole feature that lets model output reach a commit. Its value
and its risk are the same fact.

### Architecture Notes

- **Pattern:** `internal/daemon/proposals.go` for the lifecycle half —
  expire-first, ownership check on session **and** commenter login, status
  guards on every transition, refusals as values. `internal/turn/command.go`'s
  `KindSchedule` for the grammar half: a litefactory-owned control command,
  parsed by its own grammar, that never starts an agent run.
- **Package placement:** grammar in `internal/turn/command.go` plus a sibling
  parser; dispatch and validation in `internal/daemon`; the application turn in
  `internal/turn`; the mutators in `internal/git` on `*Worktree`; replies in
  `internal/report`.
- **Key interfaces:** an unexported `confirmedProposal` value that only the
  confirmation validator constructs, and that the application function requires
  as a parameter. That is what makes the gate structural (S-12).
- **The genuinely net-new part — say it out loud:** confirmation arrives as a
  comment (daemon), but application needs a worktree under the CloneGate (turn).
  So US-12 introduces a **third turn shape: an agentless apply turn**. It reuses
  `prepareClone` unchanged (gate, fetch, `freezeSlug`, `ensureWorktree`) and the
  existing commit-and-push path, and skips the harness entirely. What is new is
  the run kind, not new git plumbing — plus the three mutators. **Rejected
  alternative:** letting the *next ordinary run* apply a `confirmed` proposal
  inside `syncBase`. Cheaper, no new turn shape, but the owner confirms and
  nothing visible happens until they happen to trigger a run — which
  contradicts spec AC-24's expectation that the confirmation produces the commit
  and a comment naming it. Recorded and rejected, not overlooked.
- **Concurrency/context:** the application runs inside the existing
  session/clone serialisation and never concurrently with another turn on the
  same worktree. `BaseSHA`/`BranchSHA` are re-verified **inside** the gate,
  immediately before writing (S-15). No new locking primitive, no second gate
  acquisition, no per-branch keyed gate — DEV-3's finding still holds.
- **Integration point:** `internal/git` gains exactly three `*Worktree`
  mutators (contained write of listed paths, stage those paths, complete the
  merge) plus reuse of the dormant `ChangeUnmerged` classification at
  `internal/git/status.go:41-44`. Trailers extend the existing
  `TrailerTask`/`TrailerMode`/`TrailerRun` set (ODQ-26).
- **Watch out:** (1) the allowlist test lands **before** the first mutator, not
  after; (2) never add `Reset` to self-heal a failed rollback (S-5); (3) a
  non-owner confirmation must not move the status off `pending`; (4) "yes" or a
  reaction is not consent — the phrase and the ID are both required; (5) the
  identity prelude is mandatory, not defensive — launchd's child environment has
  no identity and no signing config; (6) if some files were excluded from the
  proposal, applying it resolves only the listed ones and the merge is **not**
  completed while conflicts remain.
- **Depends on:** US-11 (there is nothing to confirm without a proposal), US-10
  (the rollback guarantee covers the failed-application path), and the Phase 7.5
  security review. Phase 9, after a real suggestion has been observed.
- **NOT in scope:** any bypass of the gate, in any form — no config value,
  label, flag, environment variable or trusted-repo exemption; applying an
  owner's edited version of the suggestion; retrying an application inside the
  run; touching the base branch; force-push; history rewrite.

### Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-12.1 | `internal/git/surface_test.go`: `TestWorktreeMethodSetIsTheAllowlist` plus a forbidden-mutator counterpart, pinning today's method set **before** any mutator exists | Test | — | S |
| T-12.2 | `internal/git` `*Worktree` mutators: contained write of listed paths, stage those paths, complete the merge; unmerged-path read via `ChangeUnmerged` | Code | T-12.1 | L |
| T-12.3 | `internal/git` tests: path containment refusals (absolute, `..`, symlink component, unlisted path), deadline behaviour, error classification | Test | T-12.2 | M |
| T-12.4 | Confirmation grammar: new `CommandKind`, separate parser, canonical lowercase token, restricted charset; never spawns an agent run | Code | — | M |
| T-12.5 | Daemon dispatch: TTL sweep first, ownership check on session and login, status and SHA guards, every refusal a value with a reason | Code | T-12.4, US-11 | M |
| T-12.6 | The agentless apply turn: reuse `prepareClone`, apply, commit, push; `confirmedProposal` type making the gate structural | Code | T-12.2, T-12.5 | L |
| T-12.7 | Trailers and the identity prelude for the application commit; handle screened reject-don't-salvage | Code | T-12.6 | S |
| T-12.8 | `internal/report`: `ReplyAppliesResolution` and the refusal kinds, their `replyKind` cases, disclosure content (model, confirmer, files, commit link) | Code | US-11 | M |
| T-12.9 | Gate tests: no-bypass, non-owner refusal, unknown/expired/superseded/already-decided proposal | Test | T-12.6 | M |
| T-12.10 | Rollback test: forced mid-application failure leaves the branch at its pre-application SHA with no `MERGE_HEAD` and no modified tracked files | Test | T-12.6 | M |
| T-12.11 | Happy-path test: exactly one merge commit, correct trailers, only the listed files written, full disclosure rendered | Test | T-12.7, T-12.8 | M |
| T-12.12 | ODQ-28 security review sign-off recorded before the story closes | Doc | all | S |

### Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-12.1 | A test asserts that no code path writes to the worktree, the index, or a commit for a proposal whose status is not `confirmed`; the gate has no bypass (spec AC-20). |
| AC-12.2 | A confirmation from a non-owning author is refused, writes nothing, leaves the status at `pending`, and is reported in the thread (spec AC-21). |
| AC-12.3 | A confirmation naming an unknown, expired, superseded or already-decided proposal writes nothing and is reported with the reason (spec AC-22). |
| AC-12.4 | A valid confirmation whose stored `BaseSHA`/`BranchSHA` no longer match the current tips marks the proposal `superseded`, writes nothing, and asks for a fresh suggestion (spec AC-23). |
| AC-12.5 | A valid confirmation on an unchanged branch writes only the files named in the proposal, produces exactly one merge commit carrying the run trailers plus the proposal and confirmer trailers, and the comment names the model, the confirmer, the files and the commit (spec AC-24). |
| AC-12.6 | A forced failure part-way through application leaves the branch at its pre-application SHA with no `MERGE_HEAD` and no modified tracked files (spec AC-25). |
| AC-12.7 | An attempt to write a path outside `ConflictProposal.Files` — absolute, traversing, symlinked, or simply unlisted — is refused by `internal/git`, aborts the application, and leaves the branch unchanged. |
| AC-12.8 | `TestWorktreeMethodSetIsTheAllowlist` exists and passes; each mutator added to `Worktree` appears in it with a recorded reason (TEST-10). |
| AC-12.9 | Confirming the same proposal twice produces no second commit and is reported as already applied. |
| AC-12.10 | No run is held open waiting for a confirmation; propose and apply are two independently terminal runs. |

---

## Deviations and decisions raised by this section

| ID | Kind | Statement | Decision needed from |
|----|------|-----------|---------------------|
| D-1 | Correction | `feature-spec.md`'s API table places `syncBase` "between `fetch` and `freezeSlug`". Verified wrong against `internal/turn/prepare.go:376-410`. DEV-1's "last, after `ensureWorktree`" stands; the spec sentence is what changes. Wording only. | proxy-product-owner (spec edit) |
| D-2 | **DEVIATION — sequencing** | US-11 and US-12 do not fit Phase 6/7. Proposed: Phase 7.5 (security review gate), Phase 8 (US-11), Phase 9 (US-12), with an observation gate between 8 and 9. This changes the approved plan's shipping order. **Impact if granted:** the ten-story feature ships on its current schedule, unblocked; the two new stories ship behind a review. **Impact if refused:** the ten-story feature's ship date becomes dependent on a security review that has not happened, and US-12's apply path gets built before anyone has read a real suggestion. | the owner (human) |
| D-3 | Decision (spec-authorised) | `suggest_conflict_resolution` defaults **`false`** until the Phase 7.5 review signs off. Authorised by A-20's own escape clause, so not a deviation. | recorded; owner may override |
| D-4 | Correction | US-9's migration is `0007_run_sync.sql`, not `0005`; head is `0006`. US-11 adds `0008`. `plan_brief.md`'s Cross-Story Constraints and Tests bullets both need the edit. | plan-orchestrator (plan_brief edit) |
| D-5 | Amendment to a prior finding | S-9's "the conflict-path list is never persisted to the store" is reversed by `ConflictProposal.Files` and `Resolution`, which must persist for a later run to apply them. Input hunks are still never persisted. | recorded |
| D-6 | Recommendation | Keep US-11 and US-12 as two stories despite the spec's decomposition note permitting a merge. Different risk classes, different gates, and US-11 is independently valuable. | proxy-product-owner |

**No `.aiteam/AC_AMENDMENTS.md` entry is required by this section.** Nothing
here bends a rule in `.aiteam/APP_CONSTRAINTS.md`: SEC-7 constrains the
registered clone and the new mutators sit on `Worktree`, which already carries
`Commit` and `Push`; the new `Worktree` allowlist test is an addition, not an
edit to an existing design-change test. D-2 is a deviation from the approved
*plan*, not from a standard.

## Out of scope for this architect (amendment)

- Whether the suggestions are actually *good*. That is a product question
  answerable only by reading real output, which is why Phase 9 has an
  observation gate rather than a date.
- Model or prompt tuning beyond the structural rules above.
- Anything about the forge UI, notification behaviour, or how the confirmation
  phrase reads to a human. No frontend or product-writing specialist is rostered
  on this team; `proxy-product-owner` owns the phrasing and I have deliberately
  not chosen it.
