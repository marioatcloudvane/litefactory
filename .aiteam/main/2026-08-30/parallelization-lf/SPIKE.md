# Spike — parallelization-lf: the two empirical gates

2026-08-30. `RESEARCH_BRIEF.md` rested on two assumptions it could not settle by
reading code, and flagged both as probes. This spike ran them. The go/no-go
passed; **both git conclusions in the brief are wrong in detail**, one of them
in the direction that matters.

Environment: macOS (Darwin 25.5.0), git 2.50.1, claude 2.1.251. Everything ran
in a scratch directory outside the repo; nothing in `litefactory` was modified.

## What was done

Three probes, mirroring litefactory's real argv rather than approximating it:

| Probe | Mirrors | Shape |
|---|---|---|
| P1 — concurrent `claude` | `internal/harness/runner.go:468` | 14 runs over 4 rounds (3, 3, 6, and 2 via `--resume`), shared `HOME`, distinct `--session-id` |
| P2a — `worktree add` | `internal/git/clone.go:138` | 4 concurrent × 100 iterations on one clone |
| P2b — `fetch --prune` | `internal/git/clone.go:98` | 4 × 100 with nothing to fetch; 4 × 60 with new refs each round |
| P2c — `push --set-upstream` | `internal/git/push.go:47` | 4 × 60, distinct branches |

`CLAUDE_CONFIG_DIR` was deliberately left unset in P1, because `env.go:35` says
litefactory must not set it — so every concurrent run shares one `~/.claude`.

## What was found

### P1 — two `claude` processes can run concurrently. GO.

14/14 exited 0 with `"subtype":"success"`. No lock, `EEXIST`, busy or corruption
errors on stderr in any round. `~/.claude` had the same 16 entries afterwards and
`settings.json` still parsed as JSON.

The `--resume` case is the one worth recording: two *different* sessions resumed
**concurrently** each recalled their own state correctly (one answered `0`, the
other `1`, neither cross-contaminated). That is the real shape — a second comment
on issue #7 and a second on issue #8 arriving at once.

**This closes the feature-level go/no-go.** The brief was right that nothing in
the repo answered it, and right to gate on it.

Limits of the evidence, stated plainly: trivial prompts, no tool use, no long
runs, N ≤ 6, one machine, and no account-level rate-limit pressure. It proves
concurrency is *possible*, not that it is safe at N = 20 under real load.

### P2a — `git worktree add` DOES contend. The brief's "good news" is false.

**3/400 failed** (0.75%) at parallelism 4:

```
Preparing worktree (new branch 'b-17-3')
fatal: failed to read .git/worktrees/w-17-0/commondir: Undefined error: 0
```

One `worktree add` scans the worktree registry while a sibling's entry is still
half-written, and dies. Different issues, different target directories, different
branches — the contention is on the shared registry in `.git/worktrees/`, not on
the index. Linked worktrees do get their own index, which is what the brief
reasoned from; that fact is true and does not save you.

0.75% sounds ignorable. It is not: the failure is fatal and unretried, so it ends
the turn and posts a failure comment on the issue. At a few hundred turns a week
it surfaces regularly, non-deterministically, and looks like a flake.

### P2b — `fetch --prune` contention depends entirely on whether refs move.

- Nothing to fetch: **0/400 failed.**
- New refs each round (the realistic daemon case): **180/240 failed — 75%.**

```
error: cannot lock ref 'refs/remotes/origin/main':
  is at f0d1e12... but expected c87a1d6...
```

The brief predicted this blocker and was right that it exists. It named the wrong
mechanism — `FETCH_HEAD.lock` / `packed-refs.lock` — and the real one is a losing
race to update the same remote-tracking ref. That matters for the fix: this is
not a stale lockfile that a retry clears, it is two fetches disagreeing about
what `refs/remotes/origin/main` should point at.

It also explains the 0/400: a no-op fetch updates no ref and so takes no ref
lock. Any probe that forgets to move the remote will conclude, wrongly, that
fetch is safe.

### P2c — `push --set-upstream` fails *silently*. Worse in kind than predicted.

**0/240 non-zero exits. All 240 branches reached the remote.** And yet:

- `error: could not lock config file .git/config: File exists` was printed in
  **89** of the 240 runs, and
- **60 of 240 branches ended with no `branch.<name>.remote` entry at all.**

git prints an error, does not write the upstream config, and **exits 0**.
`internal/git/push.go:47` checks only `err != nil`, so litefactory sees a clean
push. The brief predicted the opposite failure — a non-zero exit *after* the
objects landed, reporting failure for a branch that is actually on the remote.
The truth is the mirror image, and the mirror image is harder to notice: nothing
reports anything, and the branch quietly has no upstream.

## Decisions worth remembering

1. **A per-repo mutex is required, and it must cover more than `fetch`.** The
   brief scoped it to fetch. Measured, all three of `worktree add`, `fetch` and
   `push --set-upstream` contend on one clone. The mutex is per-clone, and the
   `claude` run stays outside it — that part of the brief's design survives and
   is the whole reason the feature still delivers parallelism.
2. **Concurrent `claude` is not the risk.** It was the loudest unknown and it
   came back clean. The risk is git.
3. **`push --set-upstream` needs an exit-code-independent check** if the mutex
   ever fails to cover it, because exit 0 is not evidence the upstream was set.
4. **Any future git-concurrency probe must move the remote**, or it measures
   nothing.

## Not done

- Nothing was implemented. No mutex, no daemon change, no test.
- P1 was not pushed past N=6, not run with tool use or long prompts, and not run
  against account rate limits. If Plan wants a concurrency ceiling justified by
  evidence rather than by cost, that is a separate, longer probe.
- The 89-errors-vs-60-missing-entries gap in P2c was not chased. Both numbers are
  bad; the exact accounting does not change the conclusion.
- No measurement of how long the per-repo mutex would actually hold, i.e. how
  much real parallelism survives it. That is a Plan-mode question and a good one.

## Recommendation

**Plan mode.** Both gates the brief raised are now cleared or converted into
design constraints:

- Gate 3 (can two `claude` processes coexist) — **answered, yes.**
- Gate 2 (does D-X still forbid a run-rate cap, and where does the cap live) —
  **still open, and still the owner's call.** No probe can answer it; it is a
  ruling about the project's own record.

Plan should treat the per-repo git mutex as a measured requirement covering
fetch, worktree add and push, not as a precaution. The brief's `RESEARCH_BRIEF.md`
sections *"The blockers"* and *"The good news"* now have corrections recorded
against them pointing here.
