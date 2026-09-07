# US-2 — Auto-merge the base branch when the merge is clean

> As the owner of a task, I want litefactory to merge the latest base branch
> into my task branch automatically whenever that merge is clean, so that my
> agent always works on current code without me having to do anything.

This story is the core of the feature: when a task branch is behind and the
base merges in cleanly, litefactory performs that merge itself, before the
coding agent ever starts.

## Why This Matters

This is the piece that actually solves the problem in the issue title. Today,
if a task branch drifts behind its base, nobody notices until a human tries to
merge it — and by then the fix is a manual trip through GitHub's web conflict
editor, which cannot even represent every kind of conflict. This story removes
that entirely for the common case: most drift is not actually a conflict, it's
just staleness, and a clean merge can be done safely by machine. The hard part
is not the merge command itself — it's proving the merge is safe *before*
touching anything, doing it inside the same critical section that already
guards this clone against other races, and doing it in a way that never
surprises the agent or the owner about what changed underneath them.

## Architecture Notes

- Pattern: `MergePreview` is a dry-run check via `git merge-tree --write-tree
  --name-only` (git ≥2.38); `MergeBase` is the real merge via `git merge
  --no-ff --no-edit`, carrying the same identity prelude as `Commit`
  (`-c user.name= -c user.email= -c commit.gpgsign=false`) since the daemon's
  git child inherits no identity configuration (S-4).
- Package placement: `internal/git/merge.go` (same file as US-1), plus
  `ErrMergeConflict` in `internal/git/errors.go`.
- Key interfaces: `MergePreview(ctx, base string) (MergePreview, error)`
  returning `MergePreview{Clean bool, Conflicts []string, TreeSHA string}`;
  `MergeResult{Head string, Merged bool}` and `(*Worktree).MergeBase(ctx, base
  string, opts MergeOptions) (MergeResult, error)`. `MergeResult.Merged`
  follows the `CommitResult.Committed` precedent exactly — there is no
  separate "nothing to merge" sentinel error, the caller reads the bool.
- Integration point (architect DEV-1 — overrides the spec's original
  ordering): `syncBase` is wired into `internal/turn/prepare.go` as the
  **fourth and last** step inside `prepareClone`, running *after*
  `ensureWorktree`, not between `fetch` and `freezeSlug`. `MergeBase` is a
  `*Worktree` method and the worktree does not exist, nor is the branch name
  known, until `ensureWorktree` has run. The whole sync step stays inside the
  one existing `CloneGate` acquisition — it takes no acquisition of its own.
- `syncBase`'s decision tree: `Behind==0` → `up_to_date`. `Behind>0` and
  auto-sync enabled (config `AutoSyncBase` and `lf:sync=` resolved — see US-7,
  US-8) and within cap → run `MergePreview`. Clean preview → `MergeBase` →
  `synced`. Unclean preview → branch untouched, `blocked` (see US-4). Disabled
  by config/label or over cap → `skipped`, drift still measured and disclosed
  (see US-3, US-8).
- Critical ordering detail: `preparation.shaBefore` must be read **after**
  `syncBase` completes, not before it runs — reading it earlier reports a
  phantom diff on every synced run, since the branch moves without the agent
  doing anything.
- Never rebase, never force-push: rebase would rewrite the
  `Litefactory-Task`/`Mode`/`Run` trailers already committed on the branch.
  The sync merge commit is pushed by the existing end-of-run `Worktree.Push`
  — there is no separate mid-run push.
- Depends on US-1 (needs `Divergence` to decide whether to run at all), US-7
  (the `lf:sync=` label decision must be threaded into `syncBase` before this
  story's integration task can be considered done — see the hard coupling in
  `plan_brief.md`'s Workflow section), and US-8 (the three config keys
  `syncBase` reads).
- Watch out for: `internal/turn/gate_test.go`'s `gated` map must gain
  `"syncBase": {"prepareClone"}`; its `sections` map must **not** gain a
  fourth entry — record the reason in the commit (TEST-10).
- Watch out for: rewrite `Worktree`'s doc comment — it currently states the
  mutating half is `Commit` and `Push` only; `MergeBase` is now a third
  mutator and the comment must say so.
- NOT in scope: what happens when the preview is unclean beyond marking the
  outcome (that refusal path, including the comment and `ReplyRefusesSync`,
  is US-4); rendering the sync outcome in the comment (US-3); the rollback
  guarantee on failure (US-10, though it lives in the same file and is
  exercised by this story's merge path).

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Add `MergePreview` type and `(*Worktree).MergePreview` method | Code | T-1.1 | M |
| T-2.2 | Add `MergeResult` type and `(*Worktree).MergeBase` method | Code | T-2.1 | L |
| T-2.3 | Add `ErrMergeConflict` sentinel in `internal/git/errors.go` | Code | T-2.2 | S |
| T-2.4 | Rewrite `Worktree`'s doc comment for the third mutator | Code | T-2.2 | XS |
| T-2.5 | Wire `syncBase` into `internal/turn/prepare.go` as the last step of `prepareClone` | Code | T-2.2, US-7 T-7.2, US-8 T-8.1 | L |
| T-2.6 | Edit `internal/turn/gate_test.go`'s `gated` map (record reason; `sections` unchanged) | Test | T-2.5 | S |
| T-2.7 | Unit tests: clean merge, unclean preview leaves branch untouched, `Behind==0` short-circuit | Test | T-2.1, T-2.2 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | Given a branch 3 commits behind with no overlapping changes, the branch is merged, the agent runs against the merged tree, the comment states 3-behind-and-synced, and `SyncCommitSHA` is non-empty. |
| AC-2.2 | Given an unclean `MergePreview`, the exact conflicting paths are returned and neither the ref, the branch, nor the worktree is changed by the preview itself. |
| AC-2.3 | No code path introduced by this story ever pushes to or commits on `Repo.BaseBranch` — the push refspec always targets the session branch, in every outcome. |
