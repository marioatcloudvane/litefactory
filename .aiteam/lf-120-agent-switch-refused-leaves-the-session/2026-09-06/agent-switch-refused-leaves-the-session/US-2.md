# US-2 — Close the sibling bug class across all eight error paths in `prepare`

> As an operator, I want the same guarantee for every other error `prepare` can
> return after the point where the session used to be marked `running`, so the
> whole bug class is closed rather than just the one reported instance.

Confirms that the single statement move made in US-1 also fixes every other place
in `prepare` that could previously leave a session stuck showing `running`, not
just the one the issue reporter happened to hit.

## Why This Matters

The reported bug — a refused turn leaving a task stuck at `running` — was one
symptom of a wider design mistake: `prepare` used to mark a task `running` early,
long before all the things that could still go wrong on the way to actually
starting work. There are eight such error exits in total, each one previously
capable of producing the exact same false "still running" record the issue
reported, just triggered by a different kind of failure (a bad session reference,
a missing coding-agent registration, a database hiccup while allocating the run,
and so on). Fixing only the one reported case and leaving the other seven would
have cost nearly the same effort while deliberately shipping known-broken
behaviour that the next bug report would simply rediscover one site at a time.
This story is the verification that the one moved statement genuinely closes all
eight, not a second code change.

## Architecture Notes

- The eight sites are: `prepare.go:129` (`naming.SessionRef`), `:148`
  (`GetOrCreateSegment`), `:157` (the agent-switch refusal — US-1's site), `:168`
  (`opts.Runners.runner(kind)` lookup), `:181` (`previousRun`), `:194`
  (`allocateRun`), `:198` (`harness.MetaPath`), `:203` (`commentHistory`), `:250`
  (`harness.BuildPrompt`). The original research brief counted seven and missed
  `:168` — this story's scope statement is corrected to eight; it is a count
  correction only, not additional code.
- `:168`'s `opts.Runners.runner(kind)` lookup returns a `*failure`, not an ordinary
  error, so it routes through `prepareFailed` to `SessionFailed` exactly as it does
  today — after the move it transitions `idle → failed` instead of `running →
  failed`, which is correct, unchanged behaviour. **Do not "fix" this site to leave
  state untouched** — a missing coding-agent registration is a genuine failure the
  issue is owed an explanation for. `TestExecuteFetchFailureAbortsWithNoWorktree`
  already proves the `idle → failed` transition works today via the earlier fetch
  path; cite it rather than writing a new test.
- This is a read-and-confirm task, not a code change: after T-1.1 lands, walk
  `prepare.go` from the moved write to `return &preparation{…}` and confirm no
  `return nil, err` remains in that span. There is no code path left to fix.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Verify by reading that no `return nil, err` remains between the moved write and `prepare`'s return, and that all eight sibling paths (the seven originally identified plus `prepare.go:168`) now either leave state untouched or route through the unchanged `prepareFailed`; record the corrected count of eight in the story notes rather than treating it as new work | logic | US-1 / T-1.1 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | All eight identified error-return sites in `prepare` (listed in Architecture Notes) sit after the relocated `SetSessionState` write, with no `return nil, err` between the write and `prepare`'s final return. |
| AC-2.2 | `prepare.go:168`'s runner-lookup failure still ends the session at `state='failed'` via `prepareFailed` (unchanged behaviour) — not "fixed" to leave state untouched. |
| AC-2.3 | The story notes/PR description states the corrected count of eight sibling sites, not seven, and names the one the original count missed. |
