# Drift Report — Implement run 0020 (2026-09-28)

Scope: story-001 (#425), story-002 (#426), story-003 (#427), story-004 (#428),
the four frozen stories that make up feature 412 "lightweight bug workflow."

## Delivered as planned

- **story-001**: 33 `bug_*` tables replaced by six (`bug_tasks`, `bug_analyses`,
  `bug_repairs`, `bug_checks`, `bug_delivery`, `bug_events`) via migration
  `0079_bug_store_consolidation.sql`, one transaction, migrations 0066-0078
  untouched. All internal/store bug method files rewritten behind their prior
  exported names/signatures. Three-attempt repair cap by row count (no CHECK).
  Worktree guard moved onto `bug_tasks.guard_owner/guard_kind`. The three side
  logs (answers, review-change notes, test-resolution notes) folded into
  `bug_events`. The removed reference-update methods
  (`UpdateBugRepairReferences` etc.) and `ReadBugExecutionSections` are gone,
  replaced by `RecordBugFailure`/`BugFailureReasons`. Lifecycle report bodies
  are regenerated on retry rather than retained. Release runbook entry added
  (`docs/release-runbook.md`, newly created — no prior runbook file existed).
- **story-002**: `RecordBugFact`/`BugFacts` added (32-fact cap, 1KiB each, one
  overflow marker). Policy-classified validators in `internal/bugrepair` and
  `internal/buganalysis` now return `(notices []Notice, err error)` and
  continue on the success edge; guard/integrity sites are unchanged hard
  errors. `REFUSAL_REVIEW.md` documents the full guard/integrity/policy
  classification, including the five named pain points. "What the factory
  noticed" renders in stage order (or "None") in both the delivery review and
  the analysis-policy comment body; facts participate in the delivery-review
  digest so a later fact makes a published review stale.
- **story-003**: `bug_self_check_start`/`bug_self_check_status` MCP tools
  added, gated to the bug-fixer permission profile only. Store methods
  `StartBugSelfCheck`/`FinishBugSelfCheckPhase`/`BugSelfChecks`/
  `StopBugSelfChecks` write/read `bug_checks` rows with `source="fixer"`; every
  final-gate read now requires `source="factory"`. Repair handoff stops any
  running self-check and recovers crash-left pending runs on restart without
  rerunning them. A self-check evidence line appears in the delivery review
  and its digest. Both `bug-fixer.md` copies updated identically.
- **story-004**: bug.dot unchanged. Repair-budget exhaustion now records a
  sequential `exhaustion:<n>` event and posts a question (analysis-pass
  exhaustion still gets the old plain notice — verified by regression test).
  `RecordBugExhaustion`/`BugRepairHistory`/`DecideBugExhaustion` added;
  `bug_repairs.grant_reference` column added via migration
  `0080_bug_repair_grants.sql`; `ReserveBugRepair` allows ordinal >3 only with
  a unique grant reference; no cap on repeated grant/exhaust cycles (tested
  two full cycles). Owner-only `/grant-bug-attempt <n> [instructions]` and
  `/stop-bug <n>` handled in `continueExhausted`, refusing everything else
  with a message naming both commands. Granted attempts get an "Owner
  guidance for this attempt" section in the fixer prompt.

## Added scope (beyond the four stories' explicit text, judged necessary to
## reach a genuinely working engineering-complete state)

- Fixed four production regressions discovered while writing tests against
  the new schema (all four are consequences of the 33->6 table consolidation
  itself, not independent feature work): an analysis-review retirement replay
  conflict, an analysis-pass ordinal/revision conflation that broke the
  8-pass budget's tolerance for earlier failures, a merge guard that refused
  every session's first-ever merge attempt, and a hardcoded attempt id that
  silently dropped the exhaustion "stop" acknowledgement. See `notes.md` for
  full root-cause detail. These were fixed at the root rather than worked
  around, and are covered by tests written specifically to reproduce them
  before the fix and confirm them after.
- Built a full store-level PR -> review -> merge -> close delivery-journey
  test fixture (`bug_delivery_journey_test.go`) that story-001's summary asked
  for but the first test-writing pass judged too large to build in one
  dispatch; added in a follow-up pass specifically because it was the vehicle
  that surfaced the merge-guard regression above.
- Added `internal/turn/bug_exhaustion_reply_test.go` covering `continueOwner`/
  `continueExhausted`'s actual comment-parsing/authorization/replay wiring,
  which story-004's summary requires (AC-004/005) but the first turn-layer
  test-writing pass had judged too heavy a fixture to build in its own budget.

## Dropped or deferred, with reason

- **Self-check-inside-sandbox integration test (story-003)** is written
  (`internal/bugchecks/self_check_sandbox_integration_test.go`) but skips at
  runtime: it needs a real Codex CLI (`LF_TEST_CODEX`), which is unset in this
  environment — the same pre-existing skip condition already used by every
  other sandbox integration test in that package. Not a scope drop, an
  environment limitation; no substitute target was invented per the
  environment-validator rule.
- **Full, non-`Bug`-filtered `internal/turn` package test suite** could not be
  run as a single aggregate gate: it exceeds the ~8-minute per-call ceiling
  regardless of this feature's changes (confirmed independently by two
  different sub-agents during Phase 3, and by the orchestrator's own aggregate
  run in this pass). The `-run TestBug`/`-run Bug` targeted subset — the
  actually-changed surface — passes cleanly in both `internal/turn` (7s) and
  `internal/daemon` (0.6s); `internal/store`'s full suite (not just Bug-
  filtered) passes in under 9s. This is a pre-existing package-size property,
  not evidence of a regression, but it means the full whole-repo `go test
  ./...` was not exhaustively run as one gate — see Aggregate gates below for
  exactly what was run.
- **Analysis-review re-dispatch on a later fact (story-002, AC-007's analysis-
  review half)**: a fact recorded after an analysis review is confirmed does
  change that review's content digest (so a stored/compared digest would
  detect staleness), but there is no existing mechanism in this codebase that
  re-dispatches an *already-published* analysis-review comment purely because
  a fact appeared with no new analysis revision. The delivery-review half of
  this AC is fully wired (recompute-on-every-dispatch already existed and now
  includes facts/self-checks); the analysis-review half was explicitly
  flagged as a partial-wiring gap in `REFUSAL_REVIEW.md` by story-002's
  implementer rather than silently left uncovered. Judged out of this run's
  scope to invent a new re-dispatch mechanism not described by the story.

## Architectural deviations, with what was done instead

- **Cancel-blocks-grant (story-004)** is not an explicit check inside
  `continueExhausted` — it holds only because `CancelBugLifecycle`
  unconditionally overwrites disposition and the single current call site
  always re-fetches a fresh lifecycle snapshot immediately before dispatch. A
  test confirms this is correct *today*, but the guarantee is implicit/
  fragile rather than a named guard, flagged in `notes.md` for future
  hardening rather than fixed now (no test failure motivates a fix, and
  adding an explicit redundant guard was judged speculative hardening beyond
  the story's literal request).
- **story-001's cancel-blocks-dispatch triggers**: 7 of the original 11 SQL
  triggers were kept (the clearest, highest-value subset covering
  analysis/repair insert+dispatch and PR/merge/close dispatch on
  `bug_delivery`); the two test-child-specific triggers have no direct
  equivalent since test-call creation is now an `UPDATE` on the owning repair
  row rather than an `INSERT` — that guard is enforced in application code
  (`CreateBugTestCall`) instead of a DB trigger.
- **story-003's self-check recipe source**: reads from the repair's own
  pinned `SourceCommit` (no forge access, no network, matches the "no forge
  access" architecture note) rather than the factory check's live remote-HEAD
  target, since a self-check by definition runs against a fixer's
  in-progress, possibly-dirty worktree before any push exists.

## Aggregate gates run (this pass, orchestrator-level)

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go test ./internal/store/... -count=1` (full package, not just Bug-scoped)
  — ok, 8.8s.
- `go test ./internal/turn/... -run TestBug -count=1` — ok, 7.1s.
- `go test ./internal/daemon/... -run TestBug -count=1` — ok, 0.6s.
- `go test ./internal/report/... ./internal/agent/... ./internal/bugrepair/...
  ./internal/buganalysis/... ./internal/bugchecks/... ./internal/config/...
  ./internal/submission/... ./internal/teamsource/... ./internal/agentresult/...
  -count=1` — all ok.
- `go test ./internal/turn/... -count=1` (unscoped, full package) — timed out
  at the 8-minute ceiling; not a regression signal (see "Dropped or deferred"
  above), but the full unscoped suite for this one package was not completed
  as a single gate within this run's tooling limits.

## Verdict

**DRIFTED — three items, none blocking, all named above**: (1) the
analysis-review half of story-002's AC-007 staleness requirement is only
partially wired (digest changes, no re-dispatch trigger exists in this
codebase to act on it); (2) story-003's sandbox-self-check integration test
is written but skips for a missing `LF_TEST_CODEX` in this environment; (3)
story-004's cancel-blocks-grant guarantee is implicit/emergent rather than an
explicit guard. All three are called out by the engineers who found them
rather than silently passed over, and none represents a failing check or a
contradicted acceptance criterion — each is a real gap worth owner attention.
A short follow-up planning cycle is warranted specifically for item (1) if
the owner wants an explicit analysis-review re-dispatch mechanism; items (2)
and (3) are environment/hardening notes rather than planning gaps.
