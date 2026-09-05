# Feature Spec: Pre-run branch drift detection and safe auto-sync
# Status: APPROVED
# Date: 2026-09-05

## Goal
Stop a litefactory task branch silently diverging from its base branch by detecting drift before every run and, when a dry run proves it is safe, merging the base into the task branch automatically — so the non-technical owner is never asked to resolve a conflict in the GitHub web editor, and is told plainly when one exists.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As a repo owner, I want litefactory to check before each run how far the task branch is behind the base branch, so that work never starts on a stale tree without anyone noticing. |
| US-2 | As a repo owner, I want litefactory to merge the base branch into the task branch by itself when that merge is provably conflict-free, so that my branches stay current without me running any git commands. |
| US-3 | As a repo owner, I want the run's issue comment to say in plain words that the branch was N commits behind and was synced, so that an automatic change to my branch is never invisible. |
| US-4 | As a repo owner, I want litefactory to refuse to start the run when the base cannot be merged cleanly, and to name the conflicting files in the issue comment, so that an agent never builds on a tree that is about to be thrown away. |
| US-5 | As a repo owner, I want a conflict report that tells me exactly what to do next in one comment, so that I do not have to know what "rebase", "merge base" or "diverged" mean to unblock the task. |
| US-6 | As a repo owner running several issues at once, I want each task branch synced independently at its own run, so that the number of issues in flight does not decide how often I hit conflicts. |
| US-7 | As a repo owner, I want to switch auto-sync off for one issue with a label, so that a task that deliberately needs an old base is not disturbed. |
| US-8 | As an operator, I want auto-sync governed by config with safe defaults, so that I can disable it repo-wide or cap how large an automatic merge may be. |
| US-9 | As an operator, I want every new git operation to live behind the existing `internal/git` boundary, so that the "litefactory never writes to the registered clone" guarantee stays a compile-time property. |
| US-10 | As an operator, I want a failed or refused sync to leave the branch byte-identical to what it was, so that a partial merge state can never be handed to an agent or pushed. |

## Data Model

### New value types (in-memory, `internal/git`)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `Divergence` | `Ahead` | `int` | yes | Commits on branch not on base; `>= 0` |
| `Divergence` | `Behind` | `int` | yes | Commits on base not on branch; `>= 0` |
| `Divergence` | `Base` | `string` | yes | Base ref compared against, e.g. `origin/main` |
| `Divergence` | `Branch` | `string` | yes | Task branch name |
| `MergePreview` | `Clean` | `bool` | yes | True when the merge produced a tree with no conflicts |
| `MergePreview` | `Conflicts` | `[]string` | no | Repo-relative paths; empty iff `Clean` |
| `MergePreview` | `TreeSHA` | `string` | no | Written tree from the dry run; empty when not `Clean`; recorded only, never checked out |
| `MergeResult` | `Head` | `string` | yes | Worktree HEAD after the merge commit |
| `MergeResult` | `Merged` | `bool` | yes | False when nothing to merge (already up to date) |

### New persisted columns (`runs` table, migration `0005_run_sync.sql`)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `Run` | `SyncOutcome` | `TEXT NOT NULL DEFAULT ''` | yes | One of `""`, `up_to_date`, `synced`, `blocked`, `skipped`, `failed` |
| `Run` | `SyncBehind` | `INTEGER NOT NULL DEFAULT 0` | yes | `Divergence.Behind` observed at prepare time |
| `Run` | `SyncAhead` | `INTEGER NOT NULL DEFAULT 0` | yes | `Divergence.Ahead` observed at prepare time |
| `Run` | `SyncCommitSHA` | `TEXT NOT NULL DEFAULT ''` | yes | Merge commit created; empty unless `SyncOutcome = synced` |

### New report fields (`internal/report`)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `report.Run` | `Sync` | `*report.Sync` | no | Nil renders no sync section at all |
| `report.Sync` | `Outcome` | `SyncOutcome` | yes | Same enum as the persisted column |
| `report.Sync` | `Behind` / `Ahead` | `int` | yes | Rendered as counts, never as raw git output |
| `report.Sync` | `Base` | `string` | yes | Named in the comment so the owner sees which branch was merged in |
| `report.Sync` | `CommitSHA` | `string` | no | Permalinked only when the branch was pushed |
| `report.Sync` | `Conflicts` | `[]string` | no | Rendered as a file list; truncated at 20 with a count of the remainder |
| `report.ReplyKind` | `ReplyRefusesSync` | const | yes | New enum member plus its case in `replyKind()` in `internal/turn/report.go` |

### Config additions (`config.Settings`)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `Settings` | `AutoSyncBase` | `bool` (`auto_sync_base`) | no | Default `true` |
| `Settings` | `AutoSyncMaxBehind` | `int` (`auto_sync_max_behind`) | no | Default `100`; `0` means no cap; negative is a config error |
| `Settings` | `BlockRunOnConflict` | `bool` (`block_run_on_conflict`) | no | Default `true` |

### Relationships
- `Repo.BaseBranch` → the remote-tracking ref `origin/<BaseBranch>` used as the comparison base; nothing else defines the base.
- `Session.Branch` → the task branch compared and merged into.
- `Run` → one optional sync outcome per run, written in the same transaction as the run's terminal status.
- `report.Run.Sync` → rendered inside the existing end-of-turn comment; it is not a separate comment.
- `internal/git.Clone` → gains read-only `Divergence` and `MergePreview`; `internal/git.Worktree` → gains the single new mutator `MergeBase`.

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Go | `(*git.Clone) Divergence(ctx, base, branch string) (Divergence, error)` | n/a | Wraps `git rev-list --left-right --count <base>...<branch>`; read-only, belongs on the S-6 allowlist |
| Go | `(*git.Clone) MergePreview(ctx, base, branch string) (MergePreview, error)` | n/a | Wraps `git merge-tree --write-tree --name-only <branch> <base>`; writes only loose objects, never a ref, so it stays a read-only-by-contract clone method |
| Go | `(*git.Worktree) MergeBase(ctx, base string, opts MergeOptions) (MergeResult, error)` | n/a | Third and only new mutator; runs `git merge --no-ff --no-edit` in the worktree; `MergeOptions` carries the commit trailers |
| Go | `internal/git` errors | n/a | `ErrMergeConflict` added; conflicts surface as a typed error, not a stderr string match |
| Go | `internal/turn.prepareClone` | n/a | Gains a `syncBase` step between `fetch` and `freezeSlug`; runs inside the single existing `CloneGate` acquisition |
| Config | `settings.auto_sync_base`, `settings.auto_sync_max_behind`, `settings.block_run_on_conflict` | operator (file perms) | Loaded and validated with the existing `Settings` validation path; unknown values are a startup error |
| Label | `lf:sync=off` \| `lf:sync=on` | issue write | Same fixed-charset validation and precedence pattern as `lf:tools=`; label wins over config for that issue; an unparsable value is reported and ignored |
| Comment | End-of-turn run report | n/a | Gains a sync section when `report.Run.Sync != nil` |
| Comment | Refusal reply | n/a | `ReplyRefusesSync` renders the conflict report: base, behind count, conflicting files, and the copy-paste command sequence to resolve locally |
| CLI | none | n/a | No new command, no new flag |

## Business Rules

- Drift is computed against `origin/<Repo.BaseBranch>` after the existing `fetch`, inside `prepareClone`, before `freezeSlug` and before the agent starts.
- The comparison base is the remote-tracking ref only. A local base branch in the registered clone is never read and never written.
- `Behind == 0` → outcome `up_to_date`; no merge, no sync section in the comment, no refusal.
- `Behind > 0` and auto-sync enabled and `Behind <= AutoSyncMaxBehind` (or cap is `0`) → run `MergePreview`.
- `MergePreview.Clean` → run `Worktree.MergeBase`, which produces a merge commit authored by litefactory's existing commit identity and carrying the existing `Litefactory-Task` / `Litefactory-Mode` / `Litefactory-Run` trailers; outcome `synced`; the agent then runs on the synced tree.
- The sync merge commit is pushed by the same end-of-run `Worktree.Push` that pushes the agent's work; there is no separate mid-run push.
- `MergePreview` reports conflicts → the branch is not touched at all; outcome `blocked`; when `BlockRunOnConflict` is true the agent is not started and the run terminates with `ReplyRefusesSync`.
- Merge, never rebase. Rebase would require a force-push that `Worktree.Push` does not offer and would rewrite the trailer history that ties commits to runs.
- `Behind > AutoSyncMaxBehind` → outcome `skipped`; no merge attempted; the run proceeds and the comment states the branch is N commits behind and was left alone because it exceeds the cap.
- Auto-sync disabled by config or by `lf:sync=off` → outcome `skipped`; drift is still measured and still disclosed in the comment; the run proceeds.
- Every automatic change to the branch is disclosed. A `synced` outcome always renders the sync section, naming the base branch, the behind count and the merge commit.
- litefactory never merges a task branch into the base branch, never pushes to the base branch, and never opens, approves or merges a pull request.
- All new git commands go through `internal/git/exec.go`. No other package execs git.
- The whole drift-and-sync step happens under the one `CloneGate` acquisition already taken by `prepareClone`; no second acquisition is introduced.
- Each new git command carries its own deadline in the existing style: `Divergence` 30s, `MergePreview` 60s, `MergeBase` 60s.
- A `blocked` or `failed` outcome leaves the branch at exactly the SHA it had when the run started; no partial merge state, no `MERGE_HEAD`, no index left behind.

## Edge Cases & Error Handling

- Base branch missing on the remote (`origin/<base>` does not resolve) → outcome `failed`; run refused via the existing `failuref` path; comment names the missing ref and the configured base branch.
- Worktree does not exist yet (first run of a task) → branch is cut from the freshly fetched base; `Behind == 0` by construction; outcome `up_to_date`.
- Branch has no commits of its own (`Ahead == 0`, `Behind > 0`) → still a merge, not a reset; outcome `synced`; the merge is a fast-forward and `MergeResult.Head` is the base tip.
- `Ahead == 0` and `Behind == 0` → `up_to_date`, silent.
- `merge-tree` unsupported by the installed git (older than 2.38) → outcome `failed` with a `*CommandError`; run refused; message names the required git version. No fallback to an in-worktree trial merge.
- `MergeBase` fails after a clean preview (race: base moved between preview and merge) → abort the merge (`git merge --abort`), outcome `failed`, run refused, branch unchanged. No retry inside the run.
- Merge succeeds but the later end-of-run push fails → existing push-failure reporting applies; the sync section still renders with `Pushed = false` and no permalink.
- Uncommitted changes present in the worktree before the merge (possible after an interrupted previous run) → do not merge; outcome `failed`; run refused; the comment names the dirty files. litefactory does not stash, reset or discard.
- Conflicting file list is very large → render the first 20 paths and "and N more".
- Repeated runs on a still-conflicting branch → each run reports the block once, in that run's own comment. No separate or duplicated comment stream, no escalating notification.
- Both `lf:sync=on` and `lf:sync=off` present on the issue → treat as unparsable: ignore the label, use the config default, and note the ambiguity in the comment.
- Label value outside the allowed charset → ignored with a note, exactly as an invalid `lf:tools=` value is handled.
- Auto-sync introduces a merge commit that a later squash-merge of the task branch collapses; that is expected and is the reason squash is the recommended base-branch history.
- Agent (claude) has already committed for itself during a prior run → irrelevant to this feature: drift is computed from refs, not from who authored the commits.

## Out of Scope

- litefactory merging, fast-forwarding or pushing anything into the base branch.
- litefactory creating, updating, approving or merging pull requests.
- Rebase, force-push, `git pull --rebase`, or any history rewrite of a task branch.
- Automatically starting an agent turn to resolve a conflict (Renovate-style branch regeneration).
- GitHub merge queue configuration or any repo-settings change made by litefactory.
- Time-based or idle-based staleness (drift is measured in commits only).
- A per-issue base-branch override (`lf:base=`); the base branch stays a repo-level property.
- A per-project base-branch hierarchy or multi-base topology.
- Any new CLI command or flag.
- Closing the claude `Bash` + additive `--allowedTools` loophole that lets an agent run git for itself; tracked separately.
- Conflict resolution UI, web editor guidance beyond the text of the refusal comment, or any change to GitHub's own conflict editor behaviour.
- Cross-task coordination, ordering or serialization of runs to avoid conflicts.

## Acceptance Criteria

- [ ] AC-1: With the task branch level with `origin/<base>`, a run produces no sync section in its comment and writes `SyncOutcome = up_to_date`.
- [ ] AC-2: With the base 3 commits ahead and no overlapping files, a run merges the base in, the agent runs on the merged tree, the comment states the branch was 3 commits behind and was synced, and `SyncOutcome = synced` with a non-empty `SyncCommitSHA`.
- [ ] AC-3: With the base and the branch both editing the same lines of the same file, the agent is never started, the run ends with `ReplyRefusesSync`, the comment lists the conflicting file paths, and the branch SHA is unchanged from before the run.
- [ ] AC-4: `git.Clone` still has no `Commit`, `Checkout`, `Switch`, `Stash`, `Reset`, `Merge` or `Pull` method; the only mutators in the module remain `Worktree.Commit`, `Worktree.Push` and the new `Worktree.MergeBase`.
- [ ] AC-5: `internal/git/exec.go` is still the only file in the module that execs git for these operations; a test asserting the single exec point passes unchanged.
- [ ] AC-6: `Divergence` returns `Ahead`/`Behind` matching `git rev-list --left-right --count base...branch` on a fixture repo covering all four combinations of ahead/behind zero and non-zero.
- [ ] AC-7: `MergePreview` reports `Clean = false` with the exact conflicting paths and creates no ref, no branch and no worktree change; the worktree status before and after is identical.
- [ ] AC-8: `settings.auto_sync_base: false` makes a behind branch report `skipped` and run normally, with the behind count still disclosed in the comment.
- [ ] AC-9: An issue labelled `lf:sync=off` reports `skipped` even when `auto_sync_base: true`; an issue labelled `lf:sync=on` syncs even when `auto_sync_base: false`.
- [ ] AC-10: With `auto_sync_max_behind: 2` and a branch 5 commits behind, no merge is attempted, the outcome is `skipped`, and the comment names both the count and the cap.
- [ ] AC-11: The drift check and any merge occur within the single `CloneGate` acquisition in `prepareClone`; a test asserts exactly one `Acquire` per prepare.
- [ ] AC-12: A worktree left dirty before the run causes outcome `failed`, a refusal, and no merge; the dirty files are named in the comment.
- [ ] AC-13: A forced `MergeBase` failure leaves no `MERGE_HEAD` in the worktree and the branch at its original SHA.
- [ ] AC-14: Migration `0005_run_sync.sql` applies to a database created at `0004` and the four new columns default correctly for pre-existing rows.
- [ ] AC-15: `ReplyRefusesSync` is a valid `ReplyKind` with a `String`, and `replyKind()` in `internal/turn/report.go` maps to it; the enum-exhaustiveness test passes.
- [ ] AC-16: No code path in the module pushes to, or creates a commit on, `Repo.BaseBranch`; a test asserts the push refspec always targets the session branch.

## Assumptions Recorded (resolved without the owner, headless run)

| # | Open question | Decision taken | Based on |
|---|---|---|---|
| A-1 | Build staleness/drift now or defer | Build now; it is the whole feature | Brief's recommended scope names it as the in-scope core |
| A-2 | Does litefactory open PRs | No; explicitly out of scope | Brief's hard boundary "never merge into the shared base"; PR creation is a distinct capability with its own auth surface |
| A-3 | Base-branch mis-resolution reporting | Missing `origin/<base>` is a run refusal via the existing `failuref` path, not a silent skip | `prepareClone` already turns world-state problems into reportable failures |
| A-4 | Documentation audience | Not a spec deliverable; the refusal comment itself carries the plain-language instructions | Issue #70 is about the owner's experience in the issue thread |
| A-5 | Retry semantics for a failed sync | No in-run retry; the next run re-measures from scratch | Every run already re-fetches; drift is cheap to recompute and a retry loop hides a moved base |
| A-6 | `lf:merge=` / `lf:base=` label lever | `lf:sync=on|off` only; no `lf:base=` | `lf:tools=` is the established single-purpose label pattern; base branch is repo-level state in `store.Repo` |
| A-7 | Concrete thresholds | `auto_sync_max_behind: 100`, no time-based threshold | Commit count is directly observable and actionable; an age threshold implies an action nobody defined |
| A-8 | Drift comment placement | Folded into the existing end-of-turn run report, not a separate comment at run start | One run, one comment is the established shape; a second comment doubles thread noise for the non-technical reader |
| A-9 | Codex sandbox git-network behaviour | Not relevant; all git runs in litefactory's own process, never in the agent sandbox | The centralization principle from the brief's third pass |
| A-10 | Closing the claude `Bash`/`--allowedTools` loophole | Out of scope, flagged for separate tracking | It is a sandbox-policy problem, not a merge problem; folding it in would double the blast radius |
| A-11 | Per-project base-branch hierarchy | Out of scope; single repo-level base | No current data model supports more than one base per repo |
| A-12 | Flag-a-human vs regenerate on conflict | Flag a human; refuse the run | Regeneration means discarding committed agent work, which contradicts "litefactory commits; nobody in here does" as a preservation guarantee |
| A-13 | Merge vs rebase for sync | Merge, `--no-ff` | Rebase needs force-push, which `Worktree.Push` deliberately lacks, and would rewrite run trailers |
| A-14 | Whether a conflict blocks the run | Yes by default, `block_run_on_conflict: true`, operator-overridable | An agent building on a tree that will be discarded wastes a run and produces a misleading diff |
| A-15 | Persistence of the sync outcome | Persist on `runs` via migration `0005` | Audit and log rendering need it; it is four columns, not a new table |
| A-16 | Conflict-file list length | Truncate at 20 with a remainder count | Matches how the existing files-touched list keeps comments readable |
