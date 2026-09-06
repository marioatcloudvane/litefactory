# Drift Report — new-way-to-work-with-agent-teams (Run 22: close out run 21's `team add` CLI wiring)
# Branch: lf/68-new-way-to-work-with-agent-teams | Date: 2026-09-06

Scope of this report: this run only — fixing CR-B-001 and CR-B-002 (both
BLOCKING findings left open against `team add`'s CLI wiring by run 21, which
ended at commit 76f3159 without completing its own review/test/bookkeeping
close-out), plus writing the missing unit tests and completing that
close-out. Not a full-feature drift report. Run 20's report (US-9 + US-18) is
superseded by this file for the same session per this orchestrator's
single-drift-report-per-Exit convention; its content is preserved instead in
`notes.md`'s Run 20 entry, which this run did not touch. Prior runs' stories
(US-1/2/3/5/6/7/8/19, and run 21's own `team list`/`team show` work) are out
of scope here.

## Delivered as planned

- **CR-B-001 fixed**: `internal/teamsource/render.go`'s new `RenderTeam`
  entry point unions each rendered agent's contributing-teams list with
  whatever `team.ExistingTeams(root, harness, agentKey)` already reports for
  that (harness, agent key) pair, instead of overwriting it with
  `[]string{teamName}`. Verified by a direct regression test
  (`TestRenderTeam_SharedAgentWithExistingContributingTeam_UnionsTeams`) that
  seeds a real prior `team.Add` and asserts the second team's render carries
  both teams — the exact invariant US-18 AC-2 requires and CR-B-001 was
  defeating.
- **CR-B-002 fixed**: rendering/hashing/team-collection logic
  (`parseAgentSource`, the render loop, `render.RenderedFile` →
  `team.RenderedFile` conversion) moved out of `internal/cli` into the new
  `internal/teamsource/render.go`, restoring the `cli -> teamsource ->
  (team, render)` package-boundary contract documented in
  `internal/team/doc.go`/`write_types.go`. `cmd_team.go`'s `runTeamAdd` now
  calls `teamsource.RenderTeam` and passes its result straight to
  `team.Add`.
- Re-review by `go-code-reviewer`, scoped to the seven touched files,
  independently confirmed both fixes and returned ADVISORY_ONLY (no third
  cycle needed) — treated as satisfying the run's "confirm CLEAN" instruction
  per this orchestrator's rule that ADVISORY_ONLY and CLEAN gate identically.
- Missing unit tests added, as explicitly requested: `internal/teamsource
  /render_test.go` (7 test functions covering `parseAgentSource` and
  `RenderTeam`, including the CR-B-001 regression case, multi-harness
  rendering, first-ever-add degrading to just the current team, malformed
  source, unknown harness) and `internal/cli/cmd_team_test.go` (new —
  `TestTeamAddRejections` covering all four `UsageError`/`PreconditionError`/
  plain-error exit-code paths, and `TestTeamAddSuccess` covering the full
  happy path end to end). `go test ./internal/teamsource/... ./internal/cli/...
  -race -cover`: all passing, `-race` clean, `go vet` clean.
- Bookkeeping this run was asked to complete: `notes.md` updated with a
  retroactive account of run 21's undocumented work plus this run's own
  narrative and advisories; this `DRIFT_REPORT.md`; `MANIFEST.md` History
  entry (below); `APP_CONTEXT.md`/`APP_CONSTRAINTS.md` update via
  `app-context-updater` in update mode.

## Added scope

- `internal/team/hash.go`'s unexported `hashBytes` was exported as
  `HashBytes` (taking CR-A-001's pre-existing advisory since this boundary
  was already being touched by the CR-B-002 relocation), with mechanical
  call-site renames in `marker.go`, `write.go`, and their `_test.go` files.
  This is a small, judgment-call addition beyond the letter of the two
  BLOCKING findings, taken because it was already in scope of the same edit
  and closes an existing advisory rather than leaving it to a future pass
  that would touch the same lines again.
- No other production behavior was added beyond what CR-B-001/CR-B-002
  required.

## Dropped or deferred

- **`team list`/`team show` test coverage**: the unit test engineer reported
  this run that these two `team` subcommands (built in run 21, in the same
  file as `team add`) currently have no test coverage in this worktree,
  contradicting an earlier assumption that they already did. Fixing that gap
  was not part of this run's scope (the sponsor's instruction named
  `cmd_team.go` and `internal/teamsource` test coverage for `team add`
  specifically) and was left untouched rather than silently expanded into.
  Flagged here and in `notes.md` as an open item for a future pass.
- **CR-A-002** (lockfile-exists short-circuit skips harness/index validation
  ordering, carried from run 21's review): still open, non-blocking,
  documentation-only. Not fixed this run — no cycle handling requires an
  ADVISORY finding to be fixed before Exit.
- **CR-A-003** (new this run): `RenderTeam` re-reads the lockfile once per
  (harness, roster entry) pair rather than loading it once per call — a
  performance nit, not a correctness issue at today's scale. Not fixed this
  run; recorded for a future pass.
- US-10, US-11, US-13, US-17, and US-1's `manage` subcommand beyond its
  existing stub remain untouched, exactly as instructed — not scope drops,
  scope exclusions stated up front in the task.

## Architectural deviations

- None. The one deviation that existed at the start of this run (rendering
  logic living in `internal/cli` instead of `internal/teamsource`,
  CR-B-002) is the deviation this run was dispatched to correct, and it has
  been corrected — `cmd_team.go` now only orchestrates the CLI-facing
  concerns (argument parsing, error mapping, output formatting) and calls
  into `internal/teamsource` for rendering, matching how US-9/US-18's engine
  is layered elsewhere.

## Verdict

CLEAN — this run's delivered work (two BLOCKING fixes, their regression
tests, and the close-out bookkeeping) matches the task's scope exactly. The
one open gap (`team list`/`team show` test coverage) predates this run,
was not part of its assignment, and is recorded rather than silently
expanded into or silently dropped.
