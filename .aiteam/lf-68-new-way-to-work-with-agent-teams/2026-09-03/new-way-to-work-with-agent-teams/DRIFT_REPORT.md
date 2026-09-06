# Drift Report — new-way-to-work-with-agent-teams (Run 25)
# Branch: lf/68-new-way-to-work-with-agent-teams | Date: 2026-09-06

## Scope of this run
US-10 (`team status` drift detection) plus the pre-existing `team list`/`team show`
test-coverage gap flagged by runs 22 and 24. US-11, US-13 through US-17 and any
extension to `team manage` beyond its existing stub were explicitly out of scope.

## Delivered as planned

- T-21 (byte-comparison classification logic, US-10's five-state table) —
  `internal/team/status.go`: `Status` enum, `String()`, `LoadLockfile`/`DecodeTeams`
  wrappers, `Classify(root, rec, freshSourceHash, freshTeams, freshBody)` covering
  In sync / Locally modified / Upstream updated / Conflict / Missing, for both
  standalone files and root-file marker-span targets. Stdlib-only imports; no
  `internal/render`/`internal/teamsource` import — the `cli -> teamsource ->
  (team, render)` boundary contract is honored.
- T-28 (`team status` command) — `internal/cli/cmd_team.go`: `newTeamStatusCommand`/
  `runTeamStatus`, `--repo` flag, `--json` and table output, reuses
  `resolveTeamRepoPath`/`openTeamRuntime`/`resolveTeamIndex` exactly as `team
  list`/`team show` do. Exit code 0 only when every tracked record is In sync;
  any drift or per-record failure is a plain non-zero exit, never
  `UsageError`/`PreconditionError` — matches Shared Decision 2's exit-code
  contract. `team.ErrNoLockfile` treated as vacuous success.
- New narrow seam — `internal/teamsource/render.go`: `RenderCurrent(fsys,
  harnessName, sourcePath)`, a team-membership-independent re-render of one
  agent's current source, added specifically for `team status`'s "fresh
  render" half of the comparison. `RenderTeam` itself untouched.
- T-29 (tests) — AC-1/AC-2/AC-3 covered at two layers:
  - `internal/team/status_test.go`: one reproduction per classification state
    (including missing-file and both-sides-changed conflict) plus root-file
    target variants.
  - `internal/cli/cmd_team_status_test.go` (added in Phase 3, closing a gap the
    implementation engineer's own layer didn't reach): CLI-level coverage —
    flag parsing, `--repo` resolution, JSON vs. table output, exit-code mapping
    across all five states plus the render/classify-error path, sort order,
    no-lockfile case, bad-repo case.
- Test-coverage gap (runs 22/24) closed — `internal/cli/cmd_team_list_show_test.go`:
  `team list` happy path, empty-index case, `--json` mode, bad-repo error path;
  `team show` happy path, unknown-team error path (asserts `ExitUsage`),
  `--json` mode. Reused the existing `cmd_team_test.go` fixture/harness
  conventions (temp-dir repos via `gittest`, no mocking framework introduced).
- AC-2's hand-edit-risk note is emitted exactly for Locally modified and
  Conflict records, verified by both test files.
- AC-3's Missing-record path is reported (`Status: "missing"`), never skipped,
  verified in both test files.

## Added scope

- CR-B-001 fix: `runTeamStatus`'s per-record loop now aggregates render/classify
  failures via `errors.Join` instead of failing fast on the first error,
  reporting each failing record as its own `Status: "error"` row. This was not
  a plan item — it surfaced during Phase 1.5 code review as a violation of
  AC-1/AC-3 (a record whose render/classify failed was being dropped from the
  report entirely, which is a stronger failure than "skipped" — the whole
  command aborted). Fixed within the review cycle; re-review confirmed no
  remaining BLOCKING findings, verdict moved to ADVISORY_ONLY.

## Dropped or deferred

None. All three US-10 tasks (T-21, T-28, T-29) and both required test-coverage
items (`team status`, `team list`/`team show`) were completed within this run.

## Architectural deviations

None. The five-state byte-comparison scheme was implemented exactly as
specified in US-10 (no merged-config diff, no new hash type beyond
`sourceHash`/`renderedHash`), the `cli -> teamsource -> (team, render)` import
boundary was preserved, and the CLI-advertising decision (see below) followed
Shared Decision 2's static six-command architecture without modification.

## Decision recorded (not itself drift, but tracked per orchestrator instructions)

With `team status` now real, `team update` and `team manage` remain
unimplemented stubs. Decision: keep advertising all six `team` subcommands
(list/show/add/status/update/manage) rather than hiding the two remaining
stubs. Rationale (recorded in full in `notes.md`, Run 25 entry):
1. `plan_brief.md`'s Shared Decision 2 specifies a static six-command
   architecture — one `Command` constructor each, one line each in
   `Commands()`, no switch/mutable registry — which structurally
   pre-commits to always advertising all six.
2. No story in this plan authorizes dynamic or conditional command
   registration based on implementation status.
3. The existing stub pattern (honest "not implemented yet, only validates
   flags" help text) was already established by prior runs for exactly this
   situation and remains unchanged for `team update`/`team manage`.

## Verdict

CLEAN — delivered matches plan within acceptable micro-decisions (the CR-B-001
error-aggregation fix and the CLI-advertising decision, both recorded above and
in `notes.md`).
