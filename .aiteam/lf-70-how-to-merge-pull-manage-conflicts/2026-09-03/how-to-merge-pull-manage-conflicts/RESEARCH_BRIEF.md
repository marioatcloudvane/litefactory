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

## Prior Art & External Findings (web research)

This section is new in this run. The issue was labelled `lf:tools=web` and
`web-researcher` was invoked with the six questions the previous run logged as
blocked. Findings below are tagged **[W]** — reported by a sub-agent citing
external sources, not independently verified by this team — with source and
confidence carried through as given.

**Branching strategy.** [W] GitHub Flow and trunk-based development (short-lived,
single-purpose branches merged frequently via PR) are the simplest models for a
non-technical reviewer — neither exposes them to git internals if they only ever
click "Merge." GitFlow (develop/release/hotfix long-lived branches) is more
complex, requiring a reviewer to understand *where* a change is going before
merging. Trunk-based development's own definition of a "short-lived feature
branch" (single author, ~1–2 days, merged via PR after review/CI, never
diverges enough to make merge-back problematic) already describes litefactory's
one-branch-per-issue model — the two are compatible by design, not by
adaptation. Confidence: high that GitHub Flow/short-lived trunk-based branches
beat GitFlow for this audience; medium on treating GitHub Flow and trunk-based
development as sharply distinct, since sources use them near-interchangeably.
Sources: trunkbaseddevelopment.com, Mergify blog, Assembla blog.

**Merge strategy — squash vs merge-commit vs rebase-merge.** [W] Per GitHub's own
docs: squash-merge collapses a PR into a single commit on the base branch —
GitHub's own words, "a more streamlined git history" — i.e. the option that
minimizes what a non-technical reader has to parse (one entry per
issue/PR, matching litefactory's one-branch-per-issue model). Merge-commit
(GitHub's default) preserves every commit plus an explicit merge point — most
detailed, least skimmable. Rebase-merge replays every original commit with no
merge commit — linear, but a skim still sees every intermediate/messy commit,
and is the more git-literate of the three. GitHub's own caveat: squash is best
for short-lived branches, and reusing a head branch after a squash merge raises
later conflict risk — not a concern for litefactory, since branches map 1:1 to
issues and are not reused. Confidence: high (read directly from GitHub Docs).
Source: GitHub Docs, "About pull request merges."

**Can conflicts always be resolved in GitHub's web UI?** [W] No. GitHub's web
conflict editor only handles text files line-by-line — it cannot merge binary
files (images, PDFs, compiled artifacts, archives) or certain conflict shapes
(e.g. a file deleted on one side, edited on the other); GitHub's own guidance
for these cases is to fall back to local git/CLI. This is a genuine structural
limit on "a business user never needs git," not just a UX inconvenience.
Confidence: high (documented GitHub behavior). Source: GitHub Community
Discussion #46549.

**Can a non-technical approver be structurally shielded from ever seeing a raw
conflict?** [W] Yes, but by pre-emption, not elimination: keep every feature
branch continuously reconciled against its base — via GitHub's own "Update
branch" button/API, a GitHub Action, or a merge-queue/bot — so that by the time
a human looks at the PR it has already been reconciled against the latest base,
and either shows clean or the conflict has already been surfaced to a bot/agent/
engineer rather than the approver. This does not prevent conflicts occurring; it
only prevents a non-technical approver specifically from being the one asked to
resolve one. No single canonical name for this pattern emerged from sources — it
appears as "update branch," "rebase bot," "staging-branch merge," or is folded
into "merge queue" terminology. Confidence: medium (the pattern and its named
implementations are well attested; the "guarantee" is a documented pre-emption/
ejection strategy, not an absolute promise — see merge queue below). Sources:
GitHub Marketplace "Update Branch" Action, GitHub Docs on auto-merge.

**GitHub's merge queue for concurrent AI-generated branches.** [W] Mechanically:
queuing a PR causes GitHub to build a temporary "merge group" combining the
latest base with the queued PR(s) ahead of it, run required checks against that
combined state, and only land it if checks pass — each PR is tested against the
state it will actually land into, not a stale snapshot. This eliminates the
concurrency-driven class of conflict (two independently-green branches breaking
main only once combined) with zero git/CLI skill required to click "Merge when
ready." It does **not** resolve genuine content conflicts (two branches editing
the same lines) — GitHub's own docs state such a PR is ejected from the queue
for someone (an agent or a git-literate human) to fix and resubmit. Directly
relevant to the scale risk already named below (many concurrently open,
non-reconciled task branches): merge queue removes the *race* component of that
risk but not the underlying content-overlap component. Confidence: high
(GitHub's own docs describe both the mechanism and the ejection condition
explicitly). Sources: GitHub Docs "Merging a pull request with a merge queue,"
Mergify "What Is a Merge Queue?"

**How comparable AI coding-agent platforms handle this.** [W]
- *GitHub Copilot coding agent*: creates its own branch/PR; per a 2026-03-26
  changelog, a reviewer can comment `@copilot Merge in main and resolve the
  conflicts` and the agent rebases/merges base into its branch in a cloud
  sandbox, re-runs checks, and re-requests review — but per a community
  discussion this is opt-in per comment, not an automatic default behind every
  PR. Confidence: medium-high.
- *Devin*: "Devin Review" shows per-PR mergeability status (CI, required
  reviews, conflicts) and supports stacked PRs where merging the top auto-merges
  the stack. No public documentation found describing Devin autonomously
  pre-rebasing to avoid conflicts before human review — flagged as **not
  found**, not confirmed absent. Confidence: medium on what is documented.
- *Cursor background agents ("Origin")*: each agent works in an isolated
  branch/worktree, which Cursor frames as reducing conflict likelihood by
  construction (isolation, not automated resolution); "Origin surfaces merge
  conflicts so you can resolve them before merging" — conflicts are shown, not
  silently resolved on the approver's behalf. No auto-rebase-before-review
  documented. Confidence: medium.
- *Sweep AI*: explicitly does **not** auto-resolve conflicts across its own
  concurrently-generated PRs — secondary sources describe a human needing to
  "manually merge them in order and close the conflicting ones" when multiple
  Sweep PRs collide. This is a directly relevant negative example — a platform
  with the same "N issues → N branches" shape as litefactory that pushes
  exactly the problem #70 describes back onto the human. Confidence: medium
  (secondary write-ups, not Sweep's own primary docs).
- *Aider*: a local CLI tool, not a hosted branch/PR platform — does not create
  branches/PRs itself in a comparable way. An open, unresolved 2023 GitHub issue
  requests "automate git merge conflict resolution," suggesting this remains a
  gap for that tool. Confidence: medium that this is a genuine gap rather than a
  search miss.

**Named prior art for "always reconcile branch against base before review."**
[W] **Bors** merges the candidate branch into a copy of the target branch to
test the actual post-merge state before landing, explicitly to prevent "merge
skew." **Mergify** is a GitHub App that queues PRs, auto-rebases/updates them
against base, and re-runs CI before merging — automating the "update branch"
step GitHub itself leaves manual by default. **GitHub's own primitives** — the
"Update branch" button/API and native merge queue — are the built-in version of
the same idea. None of these guarantee a human never sees a raw conflict when
two branches genuinely touch the same lines; they guarantee only that a
non-technical *approver* specifically is not the one asked to resolve it.
Confidence: high that Bors/Mergify are real, established implementations;
medium on there being one canonical name for the underlying pattern itself.

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
- **GitHub's web conflict editor cannot handle every conflict.** [W] Binary
  files and certain conflict shapes (delete-vs-edit) are not resolvable in the
  web UI at all; GitHub's own guidance falls back to local git/CLI in those
  cases. Any strategy that promises "a business user never needs git" has a
  real edge here, even if it is a rare one for a Go backend repo with few
  binary assets.
- **Merge queue and "always reconcile before review" bots reduce but do not
  eliminate the chance a human meets a raw conflict.** [W] Both classes of
  tooling (GitHub's native merge queue, and named bots like Bors/Mergify) are
  pre-emption strategies — they resolve or surface conflicts to CI/an
  engineer/an agent *before* a non-technical approver sees the PR, and they
  eliminate the specific *concurrency-race* conflict class cleanly, but a
  content conflict where two branches edit the same lines still has to be
  fixed by something git-literate. A strategy that promises "an approver never
  sees a conflict" is achievable; a strategy that promises "conflicts never
  happen" is not, and Plan mode should not conflate the two.

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

From web research (this run, `lf:tools=web` present) — the six questions
previously logged as blocked were answered; see *Prior Art & External
Findings* above. A small residue of narrower points remains genuinely
unsettled and is carried forward rather than guessed at:

7. Does Devin autonomously rebase/update a branch against base before
   presenting it for human review, or does it only visualize mergeability
   after the fact? Not found in public docs — treat as unknown, not as "no."
8. Does Cursor's Origin/background agents ever perform an automatic
   base-into-feature update before a human sees the PR, or does it only
   surface conflicts once they already exist? Not found in public docs.
9. Sweep AI's conflict-handling behaviour (used above as a directly relevant
   negative example — same "N issues → N branches" shape as litefactory) is
   sourced from secondary write-ups, not Sweep's own primary docs. Worth a
   direct check before citing it as prior art in the plan.
10. There is no single canonical industry name for "always reconcile a feature
    branch against base before review" (seen variously as "update branch,"
    "rebase bot," "staging-branch merge," or folded into "merge queue").
    Plan mode should pick and define its own term rather than assume the
    industry has settled one.

## Recommended scope for Plan mode

Plan mode now has both the codebase findings and the web research needed to
choose a strategy; the residual points in Open Questions (7–10 above) are
narrow enough not to block scoping. Plan mode should decide:

- Whether litefactory gains a genuinely new capability — opening PRs itself,
  auto-rebasing/merging the base branch into task branches before review,
  and/or auto-merging on a comment-based approval — versus a narrower,
  documentation-only fix (clearer end-of-turn comment guidance, a new
  human-facing doc explaining the current manual PR/merge steps). Web research
  suggests litefactory's existing one-branch-per-issue, short-lived-branch
  model is *already* the branching strategy the industry treats as simplest
  for non-technical reviewers (trunk-based/GitHub-Flow-shaped) — the gap is
  entirely in what happens after the branch is pushed, not in the branching
  model itself.
- **Squash-merge as the default/only merge strategy** is directly supported by
  GitHub's own documentation as the option that keeps base-branch history
  simplest for a non-technical reader (one entry per issue), and fits
  litefactory's model cleanly since branches are never reused after merging.
  Plan mode should treat this as a strong candidate rather than an open
  question.
- Whether to introduce a label-gated merge/base-branch policy surface
  (`lf:merge=`, `lf:base=`, or similar), consistent with the existing
  `lf:tools=` pattern, so a non-engineer project owner can express a
  preference without touching git or a CLI.
- Whether to build custom "reconcile branch against base before review"
  automation, or lean on GitHub's own primitives first (native merge queue,
  "Update branch" button/API, branch-protection's "require branches up to
  date") before reaching for a bespoke solution or a third-party bot
  (Bors/Mergify-style). The web research found GitHub's own tooling already
  covers most of the "approver never resolves a conflict" goal for the
  concurrency-race case; only genuine content conflicts (two branches editing
  the same lines) fall outside what any of these can fully absorb, and that
  residual case is architecture-independent — something git-literate (human or
  agent) will always be needed for it.
- How any new mutating git capability (merge, rebase, auto-merge) is placed
  architecturally relative to the existing, deliberately narrow
  `Clone`/`Worktree` split — this needs `go-architect` involvement, not an
  assumption carried over from this brief.
- How the systemic, scale-driven conflict risk (many concurrently open task
  branches, none reconciled against a moving base) is addressed, not just the
  single-branch case. Merge queue addresses the race component of this risk
  directly; the content-overlap component does not have a purely mechanical
  fix and should be scoped explicitly (e.g. does litefactory route a genuine
  conflict back to a new agent turn, or always to a human?).
- Plan mode should **not** treat "add a Merge or Pull method to `internal/git`"
  as a foregone conclusion — this brief found that boundary to be a deliberate
  design choice, and any widening of it is itself a decision to make
  explicitly, weighing it against a separate-package or GitHub-side (merge
  queue / auto-merge) alternative.
- Plan mode should **not** promise "a business user will never see a
  conflict" as an absolute — web research found this achievable only as "an
  approver is never the one asked to resolve it," which is a materially
  different, and honestly weaker, guarantee that should be stated as such in
  any user-facing documentation this issue produces.
