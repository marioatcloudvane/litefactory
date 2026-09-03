# Research Brief — lf-70-how-to-merge-pull-manage-conflicts

## Request

Evaluate strategies for how to work with different branches in litefactory — who
should do the merging, how to pull, and how to manage conflicts. Experienced
developers are fine working with litefactory's current git model, but project
managers and business people who want to use litefactory get confused by the
same branch/merge/conflict mechanics. Check what strategies litefactory can
offer to make this workable for non-technical users, without breaking what
already works for engineers.

(github:marioatcloudvane/litefactory#70)

## Codebase Context

litefactory's git surface is small, deliberate, and — this is the load-bearing
finding for this brief — **has no merge, pull-into-branch, rebase, or
conflict-resolution capability at all today.** That is not an oversight; it is
architecturally enforced.

- `internal/git` is the sole gateway to the git binary, split into two types:
  - `Clone` — a fixed, read-mostly allowlist: `Fetch`, `WorktreeAdd`,
    `WorktreeList`, `WorktreeRemove`, `RevParse`, `RemoteURL`, `ConfigGet`,
    `ShowRef`. No `Merge`, `Pull`, `Rebase`, `Checkout`, `Switch`, `Stash`, or
    `Reset` method exists on it. [V] `internal/git/doc.go:10-15`
  - `Worktree` — the only mutating type, and its method set is just `Commit`
    and `Push`. [V] `internal/git/worktree.go:29-32`
  - This is a compile-time property, not a runtime permission check — there is
    no code path anywhere in the module that could merge or pull into an
    existing branch even if it wanted to. [V] `internal/git/doc.go:10-15`
- The full sentinel-error inventory for `internal/git` has no
  `ErrMergeConflict` or any conflict-specific error at all. [V]
  `internal/git/errors.go:13-83`
- `EnsureWorktree` never fetches or reconciles against the base branch itself;
  the caller (`internal/turn`) fetches once per turn, and branch reuse order is
  local branch → remote-tracking branch → `<remote>/<base>`. Once a task branch
  exists, **nothing in litefactory ever merges or rebases the moving base
  branch into it.** [V] `internal/git/ensure.go:61-87,151-176`
- One branch/worktree per GitHub issue: `naming.Branch(prefix, issue, slug)` →
  `<prefix>/<issue>-<slug>`; `naming.WorktreePath` → `<root>/task-<issue>`. [V]
  `internal/naming/slug.go:87-101`, `internal/naming/paths.go:62-71`
- End of a turn: `commitAndPush` (`Worktree.Commit` then `Worktree.Push`, both
  gated per-clone) — commit trailers `Litefactory-Task/Mode/Run`, untrusted
  Claude-produced summaries are sanitized to strip forged trailer lines, commit
  author is the human who registered the repo (never a bot identity). Push is
  SSH-only; a bot token never reaches the git child process, so an HTTPS-origin
  repo simply cannot be pushed to by the daemon — described in the code as
  intended behaviour, not a gap. [V] `internal/git/push.go:9-80`,
  `internal/git/commit.go` (prior read this session)
- **litefactory never opens a pull request.** `internal/github` has no
  PR-creation method anywhere; the only PR-shaped code is test fixtures that
  simulate GitHub's issues endpoint returning a PR payload, used purely to
  filter PRs *out* of issue polling. [V] `internal/github/issues_test.go:20,23`,
  confirmed by directory listing/grep across the package.
- The only human-facing artifact per turn is a **GitHub issue comment**
  (`Poster.PostComment`) describing branch, outcome, and sub-issue links — not
  a PR review UI. [V] `internal/turn/report.go:17-40`
- Base-branch resolution (`--base` flag → config → error, never a guess) has a
  telling design comment: "a guessed base branch is discovered as a wrong pull
  request rather than as an error" — i.e. the *only* place PRs are mentioned in
  the flow assumes a human opens the PR manually, and a wrong base surfaces
  only once that human-opened PR shows the wrong diff/target. [V]
  `internal/repos/registration.go:219-224`
- Worktree-identity conflicts (occupied path, dirty worktree) are named
  refusals, never auto-resolved — the documented remedy is a human running
  `litefactory task clean [--force]`. This is the closest thing today to
  "conflict handling," and it is about local worktree state, not git merge
  conflicts. [V] `internal/git/ensure.go:255-277`, `internal/git/clone.go`
  (`WorktreeRemove`)
- No retry/backoff logic around git operations specifically was found in
  `internal/turn/execute.go`; a failed fetch/push becomes a reported failure
  comment. Daemon-level `backoff.go` retries *turns* generically — whether that
  covers a failed git op specifically was not directly confirmed this session
  (see Open Questions). [V]/[unconfirmed] `internal/turn/doc.go` reference to
  `internal/daemon/backoff.go`
- `lf:tools=<name>` is the one existing example of label-gated, non-CLI
  configuration exposed to non-engineers (selects a tool profile; values
  validated against a fixed charset). No equivalent label exists today for
  merge, base-branch, or pull behaviour — this is useful prior art for a
  strategy that wants to give a PM a lever without a CLI. [V]
  `internal/config/policy.go:64,351`
- No README/CONTRIBUTING/docs content addresses branching, merging, or
  conflict handling for *any* audience today — `README.md` is a 2-line stub,
  and `Glob docs/**` returns nothing. Whatever explanation surface this issue
  produces will be new authorship, not an edit. [V] `README.md`, confirmed
  absent `docs/`

## Domain / Problem Space

Because litefactory itself never merges, pulls into a branch, or opens a PR,
**all merge/conflict activity a human currently experiences happens entirely
outside litefactory**, in plain GitHub — either the web UI or a local git
client. litefactory's role stops at "branch exists, is pushed, here is a
comment telling you so." Everything downstream (opening the PR, reviewing the
diff, merging it, resolving any conflict) is vanilla GitHub/git work today.

That reframes the problem #70 is naming. The confusion PM/business users hit is
not something litefactory is doing wrong procedurally — it is the gap between
"a task branch got pushed" and "someone with git literacy has to open a PR,
read a diff, possibly resolve a conflict, and click merge." An engineer already
has the muscle memory to close that gap without thinking about it; a PM does
not, and today there is nothing in litefactory to help them.

Two structurally different kinds of solution follow from that gap, and they are
not mutually exclusive:

1. **Reduce how often a human has to touch git mechanics at all** — e.g. keep
   task branches from ever diverging enough to conflict (rebase/merge base into
   the branch automatically before it's presented for review), or have
   litefactory open the PR itself so "review and click merge" is the only step
   left, or auto-merge once a designated reviewer approves via a comment
   (`lgtm`-style) rather than the GitHub merge button.
2. **Make the git mechanics that remain safe for a non-technical user** — e.g.
   restrict merges to GitHub's web UI only (never require local git), route all
   merges through a serialized merge queue so one branch's conflict never
   blocks or confuses another, and pick a merge strategy (squash vs merge
   commit vs rebase merge) that keeps the base branch's history simple enough
   for a PM to skim without git knowledge.

Both directions are compatible with "One Run, One Mode" — the comment-driven
control surface litefactory already uses is itself evidence that the team's
existing design intent is "the human's whole interaction is comments," which
if anything supports pushing further in this direction (e.g., "approve" or
"merge" as a comment action) rather than asking a PM to learn git.

**A structural risk worth naming explicitly:** with one branch created per
GitHub issue and no reconciliation against the base branch ever happening, the
number of concurrently open, silently-diverging task branches grows with the
number of issues in flight. The likelihood that two branches touch overlapping
code — and therefore that *someone* hits a conflict at merge time — rises with
scale, not usage duration. This is a systemic property of the current design,
not a per-branch edge case, and any strategy proposed here should account for
it rather than only handling the single-branch case.

## Constraints & Risks

- **No existing abstraction to extend.** Merge/pull/PR/conflict handling is
  entirely absent from the codebase, by design. Any solution here is new
  surface area, not a refactor of something broken. This is a genuine
  architectural decision Plan mode (and likely `go-architect`) needs to make
  deliberately, not infer from existing patterns. [V]
- **The `Clone`/`Worktree` split is deliberate discipline**, not an
  accident — read-only vs the two specific mutations (`Commit`, `Push`).
  Introducing a `Merge` or `Pull`-into-branch capability means consciously
  deciding whether it belongs on `Worktree` (widening its narrow, reviewed
  method set) or as a new type/package, and either choice needs to preserve
  the existing "no other package touches git directly" boundary. [V]
  `internal/git/worktree.go:13-32`
- **No PR-creation code exists.** If a strategy calls for litefactory to open
  PRs itself (a plausible way to close the human-facing gap), that is new
  `internal/github` API surface, not a config flag. [V]
- **Push is SSH-only and credential-scoped away from any bot token.** Any
  strategy involving litefactory performing a merge or auto-merge server-side
  needs its own explicit decision about what credential/permission does that
  merge — this cannot silently ride on the existing push path's assumptions.
  [V] `internal/git/push.go:9-80`
- **Base-branch mis-resolution is currently discovered late** — via a human
  opening a PR against the wrong target — rather than being surfaced by
  litefactory directly. Any new PR-related capability should probably close
  this gap rather than preserve it. [V] `internal/repos/registration.go:219-224`
- **Any new label-gated config (e.g. `lf:merge=`, `lf:base=`) needs the same
  validation discipline `lf:tools=` already has** — fixed charset, values kept
  off any code/prompt path unsanitised. [V] `internal/config/policy.go:351`
- **Scale risk**: one branch per issue with no base-branch reconciliation means
  conflict likelihood is a function of how many issues are in flight
  concurrently, not of any single feature's complexity. A strategy that only
  handles "the one branch I'm looking at" will not hold up as issue volume
  grows.
- **Open web-research questions are unresolved** (see below) — this brief's
  answer to "which branching strategy / merge UI pattern to adopt" is
  incomplete without them, and Plan mode should not treat the silence on those
  points as "no constraint exists."

## Open Questions

From the codebase tour (all need an explicit answer, ideally from the issue
owner, before Plan mode commits to a strategy):

1. Branch staleness: is reconciling a task branch against its moving base
   in scope for #70, or is it explicitly deferred? Today nothing does this at
   all.
2. Should litefactory gain the ability to open pull requests itself (new
   `internal/github` capability), or is the fix purely better guidance in the
   end-of-turn issue comment for a human to open one manually?
3. Should base-branch mis-resolution be reported directly by litefactory
   instead of being discovered only via a wrong PR a human already opened?
4. There is no existing branching/merging documentation for *any* audience —
   confirm whether this issue wants developer-facing docs, PM-facing docs, or
   both, since the effort and audience differ.
5. `internal/daemon/backoff.go`'s retry semantics were not directly confirmed
   this session — does turn-level retry cover a failed git fetch/push
   specifically, or only whole-turn dispatch? Relevant if a future strategy
   wants automatic retry around a merge/rebase step.
6. Would a new label lever (e.g. `lf:merge=squash`, `lf:base=main`) be a
   desirable way to let a non-technical project owner express merge policy
   preference, mirroring the existing `lf:tools=` pattern? No such lever
   exists today.

From intended web research — **unresolved because this session had no web
access.** The issue is not labelled `lf:tools=web`, so `web-researcher`'s tools
resolved to none and the agent could not be invoked at all this run. These
need either the label added and Research re-run, or Plan mode invoking
`web-researcher` itself once the label is present:

7. Which git branching strategy (trunk-based development, GitHub Flow,
   GitFlow) minimizes the number of git concepts a non-technical stakeholder
   ever has to touch directly, and is it compatible with litefactory's
   one-branch-per-issue model?
8. What are the tradeoffs of squash-merge vs merge-commit vs rebase-merge
   specifically for a repo where non-engineers are approvers rather than
   committers — which keeps the base branch's history simplest to read
   without git knowledge?
9. Can merge conflicts always be resolved through GitHub's web UI, or is there
   a point (e.g. binary files, large diffs) where a person with CLI/git skill
   is unavoidably required? Is there a way to structurally guarantee "a
   business user never has to resolve a conflict" — e.g. server-side
   auto-rebase, always merging base into feature branches before opening for
   review, or GitHub's merge queue?
10. How do comparable AI coding agent platforms (GitHub Copilot Workspace,
    Devin, Cursor's background agents, Sweep, Aider) handle branch creation,
    keeping branches current with main, and merging — specifically, how do
    they hide git mechanics from a non-technical approver, if at all?
11. Does GitHub's merge queue feature reduce or eliminate conflict-resolution
    burden on approvers when multiple AI-generated branches (as litefactory
    produces, one per issue) are candidates for merge concurrently?
12. Are there established best practices for "one issue = one branch, always
    rebase/merge base in before opening for review" that keep feature branches
    conflict-free by construction, so a human essentially never sees a
    conflict?

## Recommended scope for Plan mode

Plan mode should decide, informed by this brief and by the still-open
web-research questions above (which should be resolved first, either by
re-running Research with the `lf:tools=web` label or by having Plan mode
invoke `web-researcher` directly once that label exists):

- Whether litefactory gains a genuinely new capability — opening PRs itself,
  auto-rebasing/merging the base branch into task branches before review,
  and/or auto-merging on a comment-based approval — versus a narrower,
  documentation-only fix (clearer end-of-turn comment guidance, a new
  human-facing doc explaining the current manual PR/merge steps).
- Whether to introduce a label-gated merge/base-branch policy surface
  (`lf:merge=`, `lf:base=`, or similar), consistent with the existing
  `lf:tools=` pattern, so a non-engineer project owner can express a
  preference without touching git or a CLI.
- How any new mutating git capability (merge, rebase, auto-merge) is placed
  architecturally relative to the existing, deliberately narrow
  `Clone`/`Worktree` split — this needs `go-architect` involvement, not an
  assumption carried over from this brief.
- How the systemic, scale-driven conflict risk (many concurrently open task
  branches, none reconciled against a moving base) is addressed, not just the
  single-branch case.
- Plan mode should **not** treat "add a Merge or Pull method to `internal/git`"
  as a foregone conclusion — this brief found that boundary to be a deliberate
  design choice, and any widening of it is itself a decision to make
  explicitly, weighing it against a separate-package or GitHub-side (merge
  queue / auto-merge) alternative.
