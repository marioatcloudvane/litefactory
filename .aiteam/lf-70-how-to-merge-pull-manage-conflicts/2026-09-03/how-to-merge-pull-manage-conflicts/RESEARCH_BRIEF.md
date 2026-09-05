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

**Follow-up (this run):** the issue owner added three further questions after
reading the brief below: (1) whether litefactory can detect branch staleness/drift
before starting a run and surface it, (2) whether litefactory can safely
auto-sync the base branch into a task branch when no conflict exists, and (3)
whether git handling should be centralized in litefactory's daemon/router layer
rather than left reachable from individual agents. See *Follow-up Research* below.

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
  comment. **Resolved this run** (see *Follow-up Research* below):
  `internal/daemon/backoff.go` retries nothing about git at all — it only
  gates new dispatch on GitHub rate-limit errors.
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

## Follow-up Research: Staleness Detection, Auto-Sync & Git Centralization (this run)

This section answers three follow-up questions the issue owner raised after
reading the brief above: (1) can litefactory detect branch staleness/drift
before a run starts and surface it, (2) can litefactory safely auto-sync the
base branch into a task branch when no conflict exists, and (3) should git
handling move out of agents entirely and into litefactory's own daemon/router
layer. `codebase-tour` and `web-researcher` ran concurrently against these
questions; `lf:tools=web` was still present on the issue so web access was
available throughout. This run received no clarifying answers from the issue
owner (headless run) — where a codebase or web finding leaves a judgment call,
this section states the assumption made and carries the residual choice
forward into Open Questions rather than guessing silently.

### Run lifecycle and existing signals [V]

- A turn's actual sequence is: `Daemon.Tick` → `discover` → `dispatch`/`offer`
  (backoff-gated) → `work` → `handleComment`/`run` → `turn.Execute` →
  `prepare`/`prepareClone` → agent subprocess → `runAndHarvest`/`commitAndPush`
  → `finish`. Only two points touch git: `prepareClone` (fetch → freezeSlug →
  `EnsureWorktree`, one `CloneGate` acquisition) before the agent runs, and
  `commitAndPush` (commit → push, a second acquisition) after it. [V]
  `internal/turn/prepare.go`, `internal/git/ensure.go:85-143`
- `prepareClone` is the natural checkpoint for a pre-run drift check: it
  already runs a fresh `Fetch` and already holds the `CloneGate`, so the
  remote-tracking ref is guaranteed current at exactly the point a cheap
  ahead/behind comparison would need it — no restructuring of the lifecycle is
  required, only a new call inserted at an existing seam. [V]
  `internal/git/ensure.go:85-87`
- `internal/daemon/backoff.go` retries nothing about git. It exists solely to
  gate new *dispatch* when GitHub's own rate limit is hit (`Daemon.rateLimited`,
  the sole caller of `backoff.fail`, firing only on `*github.RateLimitError`).
  A failed fetch or push today is an ordinary error surfaced by
  `internal/turn`, with no retry/backoff wrapped around it at all. This
  directly resolves Open Question 5 from the prior run: turn-level retry does
  **not** cover git operations specifically. [V] `internal/daemon/tick.go:1704,
  1738-1774`
- No dedicated "how long has this branch been idle" field exists anywhere, but
  the raw ingredients do: `store.Session.UpdatedAt` (bumped on every session
  lifecycle write — state change, branch/slug freeze, worktree-path set, lock
  churn) is a coarse per-issue/branch "last touched" proxy; `store.Run.StartedAt`
  /`FinishedAt` gives the exact last-turn time; `store.Repo.LastFetchAt` gives
  the last fetch time but is per-clone, not per-issue; GitHub's own
  `Issue.UpdatedAt`/`Comment.UpdatedAt` are also directly available. None of
  these is a "commits behind base" or "last commit on this specific branch"
  figure — that would need a new read via `internal/git` (`RevParse`/`ShowRef`
  against the branch tip), which nothing computes today. [V]
  `internal/store/sessions.go:58`, `internal/store/runs.go:101-104`,
  `internal/store/repos.go:37-40`, `internal/github/issues.go:83-84`
- Base-branch resolution is a single **global** default, not a per-project
  hierarchy, despite the recent config.yaml reshuffle: `resolveBaseBranch`'s
  order is `--base` flag → `settings.default_base_branch` (config) →
  `origin/HEAD` (offline, cached) → `ErrBaseBranchUnknown`, resolved once at
  `repo add` time and cached into `store.Repo.BaseBranch` — it is never
  re-resolved at run time. The lf/50 change moved config keys under a
  `settings:` block (a schema migration) and did not add a
  `projects: <name>: default_base_branch:`-style map. Today, "a different base
  branch per registered repo" is possible only via the one-time `--base`
  override at registration, not via a live per-project config hierarchy. [V]
  `internal/repos/registration.go:219-252`, `internal/config/config.go:629-637`
- No git primitive for "commits behind base" or "would this merge conflict"
  exists in `internal/git` today — only `Fetch`, `RevParse`, `ShowRef`,
  `ConfigGet`, `RemoteURL`. Any drift check or conflict-free-merge check needs
  a new primitive added deliberately to that narrow surface (e.g. wrapping
  `rev-list --left-right --count` or `merge-tree`), not a repurposing of an
  existing method. [V] `internal/git/doc.go:10-15`

### Cheap drift detection — feasibility [V]/[W]

- `git rev-list --left-right --count base...branch` is the standard, cheap
  local primitive for ahead/behind counts: it walks only the diverged portion
  of each side's history from the merge-base, not the whole repository, so
  cost scales with how far apart the branches are, not with total repo size —
  but its answer is only as fresh as the last `fetch`, which `prepareClone`
  already guarantees. [W] Source: git-scm `git-rev-list(1)` docs.
- GitHub's REST "compare two commits" endpoint
  (`GET /repos/{owner}/{repo}/compare/{base}...{head}`) gives the same
  `ahead_by`/`behind_by`/`status` figures without a local clone, at the cost of
  one ordinary REST call per comparison (standard rate limits apply; GitHub
  does not document a special cost weight for this endpoint) — a viable
  alternative or cross-check to the local command, particularly useful for a
  status check that could run *before* a worktree/clone even exists. [W]
  Source: GitHub REST "Commits" docs.
- There is no single canonical "meaningful time has passed" threshold in the
  industry. `probot/stale` (now archived; GitHub itself now points users to
  `actions/stale` as the maintained equivalent) shipped `daysUntilStale: 60` /
  `daysUntilClose: 7` as a default meant to be tuned per repo, not a norm other
  tools converge on. [W] Source: `probot/stale` README. litefactory should
  treat any threshold it picks as a starting default exposed to
  configuration, not a fact discovered by research — and should note that
  "far behind" and "long idle" are two different, independently-true axes: a
  branch can be heavily behind a fast-moving base within hours (no idle time
  at all), or barely behind after weeks of inactivity on a slow-moving base. A
  staleness check that reports only one of the two will miss the other's
  warning case.

### Conditional auto-sync — a decision tree and its prior art [V]/[W]

The issue owner asked for an "if A then B" tree for syncing base into a task
branch. Prior art from Dependabot and Renovate — both of which manage large
numbers of bot-authored, short-lived branches, structurally the closest
published analogue to litefactory's one-branch-per-issue model — converges on
the same two-tier shape, differing only in what happens on conflict:

- **Dependabot** [W]: `rebase-strategy: auto` (default) rebases its own PR
  branches whenever the base branch moves and a conflict would result, for up
  to 30 days after the PR opened; past that window, or with rebasing disabled,
  it stops and leaves the conflict for a human. No auto-abandon, no
  close/reopen — the fallback is simply "stop touching it." Source: GitHub
  Docs, Dependabot options reference.
- **Renovate** [W]: `rebaseWhen: auto` (default) goes a step further on
  conflict — rather than attempting an in-place merge/rebase that might itself
  need resolving, it **regenerates the branch from scratch** off the new
  base-branch tip and reapplies its own change as a fresh commit, explicitly
  documented as "you should not ever need to resolve such conflicts manually."
  This works because Renovate's own change is small and mechanically
  regeneratable; it is a materially different strategy from "merge and hope,"
  and is only realistically available to a tool that can cheaply redo its own
  diff. Source: Renovate docs, "Updating and Rebasing branches."
- **A conflict-free-merge check without touching the branch**: `git merge-tree`
  (modern write-tree mode) performs a full three-way merge computation and
  reports conflicts without writing a commit or touching the index/working
  tree at all — synchronous, local, side-effect-free, and does not require a
  PR to exist. This is a materially better primitive for litefactory than
  GitHub's PR-level `mergeable`/`mergeable_state` field, which is documented
  (via community reports, not a formal GitHub SLA) to be computed
  **asynchronously** and can sit at `unknown` until polled repeatedly — a bad
  fit for a check litefactory wants to run synchronously at the top of a turn,
  and one that would require a PR to already exist, which litefactory does not
  open today. [W] Sources: git-scm `git-merge-tree` docs; GitHub Community
  Discussions on `mergeable_state`.

Combining the codebase findings with this prior art, a decision tree that fits
litefactory's existing `prepareClone` checkpoint:

1. **Fetch** (already happens). Compute ahead/behind counts for
   `base...task-branch` (`rev-list --left-right --count`, a new `internal/git`
   primitive).
2. **Behind count is 0** → branch is already current. Nothing to do, no
   comment needed.
3. **Behind count > 0, and a `merge-tree` dry run reports no conflicts** →
   merge base into the task branch, commit, push, and note it plainly in the
   run's comment ("branch was N commits behind main; synced automatically, no
   conflicts"). This is the "if A then B" case the issue owner asked for:
   proven-safe, so done silently-but-disclosed rather than escalated.
4. **`merge-tree` reports a conflict** → do **not** touch the branch. Stop
   before the agent's own work even starts, and flag it in a comment naming
   the conflicting files, so a human (or a differently-scoped future agent
   turn) makes the call — this is exactly the class of case none of
   Dependabot, Renovate, GitHub's merge queue, or any of the five previously
   researched AI coding platforms fully automates away; every one of them
   stops at "surface it" for a genuine content conflict.
5. A **merge commit**, not a rebase, is the safer default for step 3:
   rebasing would rewrite the task branch's existing commit SHAs and require a
   force-push — a capability `internal/git/push.go` does not have today — and
   could disturb the `Litefactory-Task/Mode/Run` trailer history the existing
   commit path relies on. A merge commit is a normal forward push using the
   existing `Worktree.Push` semantics. Plan mode should treat "merge, not
   rebase, for base-into-branch sync" as the default and require an explicit
   reason to deviate.

**Risk asymmetry, stated explicitly per the issue owner's request**: everything
in this tree operates on the throwaway per-issue task branch, which is low
risk by construction — it is never reused, and litefactory already treats
"worktree in a bad state" as a named, recoverable refusal
(`litefactory task clean`). **Merging into the shared base/working branch
(main) is categorically different** and out of scope for this tree entirely:
main is not something litefactory writes to at all today (see *Prior Art*
above — no PR-creation, no merge capability), and nothing found in this run's
research suggests that should change as a side-effect of solving
base-into-branch sync. Any future step where litefactory itself merges *into*
main would need its own separate research/plan/risk treatment, not an
extension of this decision tree.

**Cross-platform check**: none of the five AI coding platforms researched
previously — Copilot coding agent, Devin, Cursor background agents, Sweep AI,
Aider — is documented to run this kind of check *automatically, on a
schedule, before a human asks*. GitHub Copilot coding agent's own docs are
explicit that it works from a fixed snapshot at session start and only
rebases/merges on an explicit human comment (`@copilot Merge in main and
resolve the conflicts`), never proactively. [W] Devin, Cursor and Aider have
no documented behavior either way (silence, not a documented absence). Sweep
AI's one relevant claim (a "drip queue" staleness check) came from an
unverified secondary source, and this run's web research separately found
Sweep's public product direction may have shifted since — it should not be
relied on. This means a scheduled/pre-run drift check of the kind the issue
owner is describing would be, as far as this research could establish, **ahead
of documented practice** among comparable platforms rather than a known
pattern to copy — worth stating plainly rather than implying it is "catching
up" to an existing norm.

### Git centralization — current state and the one named gap [V]

- Structurally, litefactory already does what the issue owner is asking for
  at the package level: `internal/git/exec.go` is, by its own doc comment and
  an unexported `runner` type, the sole place in the entire codebase that
  execs the git binary. A full grep of every `exec.Command`/`exec.CommandContext`
  call site in the module confirms this — every non-git exec found
  (`internal/harness`, `internal/codex` launching the agent binaries;
  `internal/identity/gh.go` execing `gh`; `internal/secrets/keychain.go`
  execing macOS `security`; `internal/launchd/exec.go`) execs something other
  than git. [V] `internal/git/exec.go:1-15,130-150`
- **The one concrete, self-documented exception**: Claude-mode agent profiles
  (research, plan, implement) carry `Bash` itself inside their `--tools`
  surface. The intended restriction — `defaultAllowedTools()` limiting Bash to
  read-only git verbs (`status`, `diff`, `log`, `show`, `rev-parse`, `ls-tree`,
  `merge-base`, `rev-list`) — is described in `internal/harness/settings.go`'s
  own doc comment as "defence in depth," not a hard boundary, because
  `--allowedTools` is *additive* to the operator's own ambient
  `~/.claude/settings.json`. A prior spike (T-LF3.0) found this lets Bash
  execute commands absent from the explicit allowlist, leaking roughly 250
  ambient allow-rules through. In practice, this means an agent's shell access
  to raw, mutating git (`commit`, `push`, `merge`, `pull`) is gated by a leaky
  allowlist today, not by the same hard `--tools`-surface boundary that keeps
  every other package out of `internal/git`. [V] `internal/config/policy.go:63-65,
  423-451`, `internal/harness/settings.go:12-34`
- Codex-mode agents use a filesystem sandbox (`read-only`/`workspace-write`)
  instead of a Bash allowlist; whether that sandbox also blocks outbound git
  network operations (a `push` or `fetch` to a remote, as opposed to a local
  filesystem write) was not confirmed this run and is carried forward as an
  open question. [V]/[unconfirmed] `internal/config/policy.go:495-504`
- This gives the "move git out of agents into litefactory itself" ask a
  concrete justification beyond convention-following: it is not only an
  architectural preference but a way to close a documented, already-flagged
  loophole. Any new mutating capability this issue produces (a drift check
  that writes nothing is unaffected, but a merge/sync step is not) should be
  placed so that **no agent's Bash tool can reach it or trigger it directly**
  — which centralizing in the daemon/turn layer achieves by construction, and
  which today's Claude-mode Bash surface does not fully guarantee for the
  mutating git operations that already exist (`commit`, `push`).

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
- **A pre-run drift check has a natural, cost-free home.** `prepareClone`
  already fetches and already holds `CloneGate` before every turn, so
  inserting an ahead/behind check there adds no new git round-trip beyond one
  new local computation. Any implementation should use this existing seam
  rather than invent a second fetch point. [V]
- **No git primitive for ahead/behind or dry-run-conflict exists yet.**
  `internal/git`'s method set (`Fetch`, `RevParse`, `ShowRef`, `ConfigGet`,
  `RemoteURL`) has no `rev-list --count` or `merge-tree` wrapper today; both
  are new, deliberate additions to a surface this brief already flagged as
  narrow by design, not incidental gaps. [V]
- **Base-branch configuration is global, not per-project, today.** The issue
  owner's phrasing ("whatever base branch is configured per project") assumes
  a live per-project lookup that does not exist — only a one-time `--base`
  override captured at registration. If Plan mode wants true multi-project
  base-branch flexibility to co-exist with a drift/sync feature, that is
  itself new config surface, not something to assume is already wired up. [V]
- **GitHub's own PR-level "is this mergeable" signal is unreliable for a
  synchronous check.** `mergeable_state` is documented (via consistent
  community reports, not a formal SLA) to be computed asynchronously and can
  return `unknown` on first read — a bad primitive for a check litefactory
  wants to run inline at the start of a turn. Prefer the local, synchronous
  `git merge-tree` computation, which also does not require a PR to exist yet
  (litefactory opens none today). [W]
- **A merge-based sync (not rebase) avoids a new force-push capability.**
  Rebasing the task branch during an automatic sync would rewrite existing
  commit SHAs and require force-push, which `Worktree.Push` does not support
  today; it would also complicate the `Litefactory-Task/Mode/Run` trailer
  history the existing commit/push path relies on. A merge commit uses the
  existing push semantics unchanged. [V]
- **Auto-sync must not blur into "litefactory merges into main."** Everything
  the auto-sync decision tree covers is base-into-task-branch, a throwaway,
  never-reused branch; merging *into* the shared base branch is a different,
  higher-risk class of write this brief has already established litefactory
  does not do at all today, and this run found no reason to change that
  boundary as a side effect of solving staleness/sync.
- **Claude-mode agents' Bash access to git is a documented, leaky boundary,
  not a hard one.** Any decision to centralize git handling in litefactory
  should be paired with awareness that the existing "agents never
  commit/push" convention already has one open exception
  (`internal/harness/settings.go`'s own "defence in depth" framing) that
  centralizing new capabilities does not, by itself, retroactively close for
  the operations that already exist. [V]

## Open Questions

From the codebase tour (original run):

1. Branch staleness: is reconciling a task branch against its moving base in
   scope for #70, or is it explicitly deferred? This run's *Follow-up
   Research* section above answers "what a mechanism could look like," but
   Plan mode still needs to decide whether to build it now or later.
2. Should litefactory gain the ability to open pull requests itself (new
   `internal/github` capability), or is the fix purely better guidance in the
   end-of-turn issue comment for a human to open one manually?
3. Should base-branch mis-resolution be reported directly by litefactory
   instead of being discovered only via a wrong PR a human already opened?
4. There is no existing branching/merging documentation for *any* audience —
   confirm whether this issue wants developer-facing docs, PM-facing docs, or
   both, since the effort and audience differ.
5. ~~`internal/daemon/backoff.go`'s retry semantics~~ — **resolved this run**:
   backoff only gates new dispatch on GitHub rate-limit errors; it does not
   retry git operations at all. A failed fetch/push/merge surfaces as an
   ordinary turn failure today, with no automatic retry. Any new auto-sync
   capability needs its own explicit decision about whether a failed sync
   attempt gets retried or is simply reported and dropped.
6. Would a new label lever (e.g. `lf:merge=squash`, `lf:base=main`) be a
   desirable way to let a non-technical project owner express merge policy
   preference, mirroring the existing `lf:tools=` pattern? No such lever
   exists today.

From web research (prior run):

7. Does Devin autonomously rebase/update a branch against base before
   presenting it for human review, or does it only visualize mergeability
   after the fact? Not found in public docs — treat as unknown, not as "no."
8. Does Cursor's Origin/background agents ever perform an automatic
   base-into-feature update before a human sees the PR, or does it only
   surface conflicts once they already exist? Not found in public docs.
9. Sweep AI's conflict-handling behaviour is sourced from secondary
   write-ups, not Sweep's own primary docs — and this run's web research
   separately found Sweep's public product direction may have shifted since
   those write-ups were made. Worth a direct check before citing it as prior
   art in the plan.
10. There is no single canonical industry name for "always reconcile a feature
    branch against base before review" (seen variously as "update branch,"
    "rebase bot," "staging-branch merge," or folded into "merge queue").
    Plan mode should pick and define its own term rather than assume the
    industry has settled one.

From this run's follow-up research (staleness detection, auto-sync,
centralization):

11. What specific ahead/behind and idle-time thresholds should trigger a
    staleness flag? This run found no industry-standard number (only
    `probot/stale`'s tunable 60-day default as one example) — Plan mode/the
    issue owner needs to pick concrete defaults, ideally exposed as config
    rather than hardcoded.
12. Should the drift/staleness signal be posted as its own comment at the
    start of a run, or folded into the existing end-of-turn report comment?
    The former surfaces it before the human reads anything else; the latter
    is one fewer comment but delays the signal until after the run's own work
    is already visible.
13. Does the Codex-mode filesystem sandbox (`read-only`/`workspace-write`)
    block outbound git network operations (push/fetch), or only local
    filesystem writes? Not confirmed this run — relevant to how complete
    "agents cannot touch git" actually is outside the Claude-mode path.
14. Should Plan mode treat hardening the Claude-mode Bash/`--allowedTools`
    git-access loophole (leaking ~250 ambient allow-rules per the T-LF3.0
    spike) as in-scope alongside a new daemon-side sync capability, or as a
    separate, pre-existing security item this issue should merely flag rather
    than fix? Building new centralized git capability while leaving the
    existing loophole open is an inconsistent posture worth naming
    explicitly, but closing it may be a larger, separately-scoped change.
15. Should per-project base-branch resolution move from a one-time `--base`
    registration-time override to a live, re-resolvable per-project config
    value (a genuine hierarchy), given the issue owner's phrasing assumes the
    latter already exists and it does not?
16. On a genuine merge conflict during auto-sync, should litefactory's
    fallback be "flag a human" only (this run's default recommendation,
    matching Dependabot/GitHub merge-queue precedent), or should Plan mode
    also consider a Renovate-style "regenerate the branch from the new base
    and re-run the agent" fallback? The latter is more automation but is a
    materially bigger scope change (implies triggering a new agent turn
    automatically) than a simple sync-or-flag tree.

## Recommended scope for Plan mode

Plan mode now has both the codebase findings and the web research needed to
choose a strategy; the residual points in Open Questions are narrow enough not
to block scoping. Plan mode should decide:

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
- **Staleness/drift detection** is cheap to add at the existing `prepareClone`
  checkpoint (fetch already happens there) using a new `internal/git`
  primitive wrapping `rev-list --left-right --count`. Plan mode should scope
  this as a small, mostly-additive change: one new git primitive, one new
  comment path, and one or two new config thresholds — not a lifecycle
  restructuring.
- **Conditional auto-sync of base into a task branch is safe to automate up to
  the point of a genuine content conflict**, using the decision tree above:
  check ahead/behind → if behind, dry-run via `git merge-tree` → if clean,
  merge (not rebase) and push with a disclosing comment → if conflicting, stop
  before the agent's own work and flag a human. This mirrors Dependabot's own
  published pattern for its own bot-branches and is the recommended default
  shape; Plan mode should decide only the specific thresholds/config surface,
  not re-derive the tree.
- **Do not extend this into litefactory merging into the shared base/main
  branch.** That remains explicitly out of scope — a categorically
  higher-risk operation this brief has already established litefactory does
  not perform today, and nothing in this run's research changes that
  recommendation.
- **Centralizing git handling in litefactory (rather than in agents) is
  already substantially true structurally** (`internal/git` is the sole exec
  point) but has one documented, real gap: Claude-mode agents' Bash tool has
  leaky, additive-only restrictions on git verbs. Plan mode should decide
  whether closing that gap is part of this issue's scope or a named,
  separately tracked follow-up — but should not build new centralized git
  capability while implying the existing boundary is airtight when this run
  found it is not.
- Any new mutating git primitive (the `merge-tree` dry-run wrapper, the actual
  merge-and-push step) belongs in `internal/git`, behind the same deliberate,
  narrow-surface discipline as `Clone`/`Worktree` — this reinforces, rather
  than revises, the architectural note already given to `go-architect` in the
  original brief.
