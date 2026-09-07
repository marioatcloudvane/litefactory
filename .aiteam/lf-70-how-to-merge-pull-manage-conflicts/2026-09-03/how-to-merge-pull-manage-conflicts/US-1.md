# US-1 — Measure behind/ahead before every run

> As the owner of a task that has been open for a while, I want litefactory to
> check how far my task branch has drifted from the base branch before it
> starts working, so that I find out about drift immediately instead of
> discovering it later as a broken merge.

This story adds the measurement litefactory currently has no way to take: how
many commits a task branch is behind and ahead of its base, computed fresh on
every run.

## Why This Matters

Right now nothing in litefactory ever looks at how far a task branch has
fallen behind its base. A branch can sit open for weeks while the base branch
moves on, and the first anyone learns of it is when a human tries to merge and
hits a conflict GitHub's web editor cannot resolve. This story is the
foundation the rest of the plan stands on: without a trustworthy behind/ahead
count taken fresh before every run, nothing downstream — auto-merging when
it's safe, refusing when it isn't, telling the owner what happened — has
anything reliable to act on. It is deliberately the smallest piece of the
feature, but everything else depends on it landing correctly first.

## Architecture Notes

- Pattern: new read-only method on `*git.Worktree` (not `*git.Clone` — see
  `plan_brief.md`'s Shared Decisions on git-method placement), following the
  existing `Fetch`-style pattern of argument screening plus a named timeout
  plus exit-code classification.
- Package placement: new file `internal/git/merge.go`. This is the only story
  in the plan that touches `internal/git` with no dependency on any other
  story landing first — it is the true starting point of Phase 1.
- Key interface: `Divergence(ctx, base string) (Divergence, error)` returning
  `Divergence{Ahead int, Behind int, Base string, Branch string}`, backed by
  `git rev-list --left-right --count base...branch`.
- Integration point: comparison is always against `origin/<Repo.BaseBranch>`
  — the remote-tracking ref, after the existing fetch inside `prepareClone`.
  The local base branch is never read or written.
- Watch out for: `--left-right --count`'s output order is `<left> <right>` —
  which side is "ahead" and which is "behind" depends entirely on which side
  of `...` each ref sits. Pin this with a test that would fail if the two
  sides were swapped; this is the single easiest place to get this story
  backwards without any test catching it.
- Watch out for: `base` and `branch` are ref-name arguments and must pass
  `checkName` before reaching `runner.run`, exactly as `Fetch` screens
  `remote` today (S-1, `plan_brief.md`). The `base...branch` range string is
  assembled inside this method from the two already-screened names — never
  accept a pre-joined range from a caller.
- Risk: a missing base ref (deleted branch, renamed default branch, etc.)
  must produce a classified, reportable error via the existing `failuref`
  path — not a panic, not a raw exit-code surface.
- NOT in scope: deciding what to *do* with the divergence count (that's US-2),
  rendering it in the comment (US-3), and reading the git version floor probe
  belongs to Phase 0's prerequisite work, not this story's tasks below.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Probe: verify `merge-tree`/`rev-list` behavior and the git ≥2.38 floor | Spike | — | S |
| T-1.2 | Add `Divergence` type and `(*Worktree).Divergence` method | Code | T-1.1 | M |
| T-1.3 | Add `divergenceTimeout` const (30s) | Code | T-1.2 | XS |
| T-1.4 | Unit tests: 4 ahead/behind combinations, missing-ref error path, side-order regression test | Test | T-1.2, T-1.3 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | `Divergence` matches `git rev-list --left-right --count` output exactly across all four ahead/behind combinations (0/0, ahead-only, behind-only, both), verified by a test that pins which side of the `...` range is "ahead" and which is "behind." |
