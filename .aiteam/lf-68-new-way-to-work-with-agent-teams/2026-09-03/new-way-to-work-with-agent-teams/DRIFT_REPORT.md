# Drift Report — Configurable Agent Teams (US-16 + US-17)
# Branch: lf/68-new-way-to-work-with-agent-teams | Date: 2026-09-07

This report is scoped to this run (US-16, US-17 — the last two stories on issue #68), per this
session's one-report-per-run convention. Prior runs' drift reports for US-1 through US-15 are
preserved in `notes.md`'s own per-run entries, not restated here.

## Delivered as planned

**US-16 — Gitignore the bookkeeping directory and protect it from accidental writes**
- T-44: `.aiteam/AC_AMENDMENTS.md`'s SEC-7 ledger entry corrected to the actual current Owned Paths
  (three harness dirs, `.litefactory/`, root-file marker spans, the single managed `.gitignore`
  line). AC-2 met.
- T-45: `.litefactory/` auto-appended to `.gitignore` on first bookkeeping-directory creation, per
  worktree, idempotently — hooked into `acquireRepoLock` so it fires on every `Add`/`SetOverride`/
  `UnsetOverride` call, including each per-worktree call inside US-15's `--all-worktrees` loop.
  AC-1 met.
- T-46: runtime write guard (`isOwnedWriteTarget`, wired into `writeFileAtomic`) refuses any write
  outside the corrected Owned Paths list, reported as an internal error via a new
  `ErrWriteOutsideOwnedPath` sentinel. AC-3 met.
- T-47: tests for both the `.gitignore` idempotency/multi-worktree behavior and a fault-injected
  out-of-scope write, independently re-verified by the unit test engineer with no gaps found.

**US-17 — Bootstrap a `go-team` index from litefactory's own agents**
- T-62: all surviving agents' markdown copied verbatim from `.claude/agents/` to their new
  `internal/team/embedded/teams/{go-team,shared}/` homes; embedded default index rebuilt with a
  real `go-team` entry (5 Go-specialist agents, inline) and a top-level `shared` map. AC-1, AC-2,
  AC-3 met.
- T-63: decode/validation tests against the real embedded snapshot, plus CLI-level `team list`/
  `team show` tests against it, independently re-verified with no gaps found.

Both stories implemented and tested against their own acceptance criteria independently, per this
run's explicit instruction not to conflate them — no fix cycle was needed for either.

## Added scope

None beyond what each story's own tasks called for. No new command, package, or capability was
added outside T-44 through T-47 and T-62/T-63.

## Dropped or deferred

- **US-17's shared-map membership: 7 agents, not 8.** `US-17.md`'s literal text names 8
  generic/orchestration agents including `app-context-updater`, but that agent was retired in a
  prior run (run 30) and its `.claude/agents/app-context-updater.md` source file no longer exists in
  this repo. The story's own architecture notes anticipated exactly this possibility and named the
  correct resolution (drop it rather than invent content); the shipped index correctly ships 7
  shared agents (12 total, not 13). This is not a scope drop in the sense of skipped work — it is
  the story's own documented contingency firing, confirmed independently by the implementing
  engineer, code review, and the unit test engineer, all three checking `.claude/agents/` directly
  rather than trusting each other's claim.
- **US-16's "reuses US-15's live worktree-discovery logic (T-41)" architecture note is satisfied by
  a different mechanism than literally described** (see Architectural Deviations below) — not a
  scope drop, but flagged here for completeness since it's the same category of "story doc doesn't
  quite match what shipped" as the point above.

## Architectural deviations

- **US-16's gitignore propagation mechanism.** The story's Architecture Notes state this story
  "reuses US-15's live worktree-discovery logic (T-41) rather than inventing a second way to
  enumerate worktrees." The shipped implementation does not call `discoverSiblingWorktrees` from
  `internal/team` at all. Instead, `ensureGitignoreEntry` is hooked into `acquireRepoLock`, which
  every `Add`/`SetOverride`/`UnsetOverride` call already passes through — including each
  per-worktree `Add` invoked from inside US-15's own `--all-worktrees` loop in
  `cmd_team_worktrees.go`. This achieves the same "uniformly across every worktree" requirement
  (AC-1) without a second, redundant worktree-enumeration call, and was verified correct by both the
  implementing engineer's tests and code review's independent trace. Recorded as a deviation from
  the story doc's literal wording, not from its actual intent (avoid inventing a second discovery
  mechanism) — the shipped approach arguably satisfies that intent more directly, since it doesn't
  even need a *first* dedicated discovery call for this purpose. Code review recommended (CR-A-002,
  non-blocking) a one-line comment in `lock.go` calling this out explicitly for future readers; not
  applied this run by judgment (advisory, not required).
- **US-17's `source` path convention omits the story doc's literal `"teams/"` prefix.** The worked
  example in `US-17.md` shows `source: "teams/go-team/go-architect.md"`. The shipped embedded index
  instead uses `source: "go-team/go-architect.md"` (no `teams/` prefix), because
  `internal/team/embedded.go`'s `fs.FS` is already rooted at `embedded/teams` — including the
  literal prefix would have doubled the path segment and broken every read. This was checked against
  `internal/teamsource/resolve.go`'s tier-2/tier-3 root convention and confirmed consistent across
  all three index-source tiers (embedded default, static path override, registered-repo override):
  none of them include a `teams/` prefix inside `source`, because in every tier the resolution root
  is already `<tier-root>/teams`. This is a correction of an inconsistency in the story doc's worked
  example, not a deviation from the accepted schema design (Shared Decision 1's `source`/`dest`
  semantics are otherwise followed exactly).

## Verdict

CLEAN — both stories delivered exactly against their own acceptance criteria. The two "deviations"
above are cases where the shipped code correctly resolved an internal inconsistency in the story
docs' own wording (a retired agent the story doc hadn't caught, a path-prefix convention the story
doc's worked example got backwards relative to the load mechanism), not divergences from the plan's
actual design intent — both were independently confirmed correct by code review and the unit test
engineer, not asserted unilaterally by the implementing engineers. No BLOCKING code review finding,
no bug-loop cycle, and `go test -race ./...` green across all 26 packages. **This is the last of the
18 planned stories (US-1 through US-11, US-13 through US-19, US-12 dropped in Plan-mode
reconciliation) on issue #68 — the feature is code-complete.**
