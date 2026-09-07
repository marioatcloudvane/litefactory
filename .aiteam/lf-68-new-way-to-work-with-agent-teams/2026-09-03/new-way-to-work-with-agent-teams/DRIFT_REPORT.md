# Drift Report — US-13 (`team manage` per-repo agent source override)
# Branch: lf/68-new-way-to-work-with-agent-teams | Date: 2026-09-07

## Delivered as planned

- T-35 — Overrides file schema `.litefactory/team.overrides.json` (`{agentKey:
  source}`) with strict two-phase decode (`decodeOverrides` in
  `internal/team/overrides.go`), reusing `internal/team/decode.go`'s
  generic-walk + `errors.Join` pattern exactly as scoped.
- T-36 — `team manage <agent-key> set/unset/show` commands
  (`internal/cli/cmd_team.go`: `newTeamManageCommand`, `runTeamManage`,
  `teamManageError`), replacing the pre-existing stub, advertised alongside
  the other five `team` subcommands.
- T-37 — Render-time override substitution in `internal/teamsource/render.go`
  (`readAgentSource`), sitting upstream of hashing so both `RenderTeam` and
  `RenderCurrent` pick it up, and so US-10's `team.Classify` needed zero
  modification — exactly the outcome the architecture notes predicted.
- T-38 — Tests proving: an override supersedes the index source
  (`internal/team/overrides_test.go`,
  `internal/teamsource/render_test.go`); `unset` reverts to the
  index-resolved source; and drift classification reacts to the override's
  file rather than the index's, with no special case in `Classify`
  (`TestTeamStatus_ActiveOverride_ClassifiesAgainstOverrideFile`).
- AC-1 — verified end to end through `team add` and `team update`, not just
  at the render-function level (`TestTeamAddSuccess_ActiveOverride_...`,
  `TestTeamUpdate_ActiveOverride_...`).
- AC-2 — verified at both the `internal/team` package level and the CLI level
  (`TestTeamManage_SetThenUnset`).
- AC-3 — verified end to end through `team status` (`TestTeamStatus_...`),
  including the negative case (override present but byte-identical to source
  still classifies in-sync).
- AC-4 — verified at both levels: nonexistent path and non-markdown path each
  fail with a precondition error and record no override
  (`TestSetOverride_PathFailures_RecordsNoOverride`,
  `TestTeamManage_SetNonexistentPath_FailsWithPrecondition`,
  `TestTeamManage_SetNonMarkdownPath_FailsWithPrecondition`).
- The recurring `errors.Join`/fail-fast finding — raised BLOCKING three times
  previously on this issue — was checked specifically by code review this
  run and confirmed correct in `decodeOverrides`. Zero BLOCKING findings this
  run, the first clean pass on that historical pattern for this issue.

## Added scope

- Three additional unit tests beyond what US-13's own task table named
  explicitly (`TestTeamAddSuccess_ActiveOverride_SubstitutesLocalFile`,
  `TestTeamUpdate_ActiveOverride_SubstitutesLocalFile`,
  `TestTeamStatus_ActiveOverride_ClassifiesAgainstOverrideFile`). These are
  test-only additions closing real end-to-end coverage gaps for AC-1 and AC-3
  that T-38's package-level tests alone did not reach (they proved the
  render/classify functions honor an override in isolation, not that the
  full `team add`/`team update`/`team status` CLI commands do). No
  production code or new capability was added — this is coverage depth, not
  scope growth.
- One non-behavioral fix: the `team manage` usage string was corrected by the
  implementation engineer mid-task to satisfy
  `internal/cli/router_test.go`'s `TestCommandsAreWellFormed` (from
  `"team manage <agent-key> set source=<path> | unset | show [--repo path]"`
  to `"team manage <agent-key> <action> [<source>] [--repo path]  (set
  source=<path> | unset | show)"`). Cosmetic, in-scope, no capability change.

## Dropped or deferred

- None. All four tasks (T-35–T-38) and all four acceptance criteria
  (AC-1–AC-4) were delivered and verified this run.
- US-14, US-15, US-16, US-17 were explicitly out of scope for this run per
  the task instruction and were not started, extended, or touched. `team
  manage` was implemented exactly to US-13's scope and not extended further.

## Architectural deviations

- None. The override mechanism was implemented exactly as Open Question 6's
  resolution specifies: a per-repo, per-agent-key `{source}` substitution
  file in `.litefactory/`, consumed at render time upstream of hashing, with
  `Dest` unaffected and `internal/team`'s three-tier index-level source
  resolution left untouched. The `cli -> teamsource -> (team, render)`
  dependency direction was preserved — `cmd_team.go`'s `team manage` wrapper
  contains no rendering/hashing/source-resolution logic of its own, confirmed
  by code review.

## Verdict

CLEAN — delivered matches plan within acceptable micro-decisions (added test
depth for AC-1/AC-3 end-to-end coverage; one cosmetic usage-string fix).
