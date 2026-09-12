# Engineering test report — issue #210 run 0005

Status: engineering and required aggregate checks passed. Workflow 2 engineering checks are distinct from acceptance, code/architecture/plan review, remote effects and factory save.

## Passed scoped checks

| Story | Retained output | Executed scope |
| --- | --- | --- |
| US-001 | checks/us001-unit.log | Pure daemon stopped-result ownership/capacity/repair, race and coverage |
| US-001 | checks/us001-daemon-db.log | Actual Tick with closed-store recovery, startup launched/prelaunch behavior, scheduled/continuation allocation reuse, race and coverage |
| US-001 | checks/us001-final-boundaries-race.log | Store/turn preparation, refusal, reportability, registration and SQLite lifecycle-write failures, race and coverage |
| US-001 | checks/us001-token-repair-race.log | Explicit token restoration after persistence failure, race and coverage |
| US-001 | checks/us001-refusal-report-race.log | Readiness refusal runless explanation, race and coverage |
| US-001 | checks/us001-store-turn-db.log | Full store/turn suite at the earlier US-001 compiled snapshot: store 166.923s, turn 843.570s; supplemental, not final aggregate proof |
| US-002 | checks/us002-unit.log | 28 identity, trusted-match and legacy cases, race and coverage |
| US-002 | checks/us002-store-db.log | Journal transitions, migration provenance, atomic acknowledgment and six SQLite/fake-HTTP fault windows, race and coverage |
| US-002 | checks/us002-turn-db.log | Report suite including changed-readiness refusal recovery, retained bytes, trust rejection, duplicate selection and redirected accounting, race and coverage |
| US-002 | checks/us002-receipt-diagnostic-race.log | Receipt failure retains delivery identity and uncertain state for direct POST and lookup recovery |
| US-003 | checks/us003-store-db.log | Recovery metadata migration/reopen/update/isolation/failure/resolution behavior, race and coverage |
| US-003 | checks/us003-unit.log | Exact fake-clock cadence, third-failed-recovery escalation, redaction, silent ineligible ticks, resolved state and outage memory, race and coverage |
| US-003 | checks/us003-daemon-db.log | SQLite restart/outage debt, actual Reporter/Tick recovery, blocked source ordering and unrelated task progress at one worker, race and coverage |
| Affected stories | checks/aggregate-cycle2-daemon-production.log | Cancellation receipt/ancillary acknowledgment, mode-journal cleanup and real CLI fixture continuation behavior, race and coverage |
| Affected stories | checks/aggregate-cycle2-daemon-tests.log | Trusted recovery fixtures, exact owner execution order, owner-label preservation and marker injection guard, race and coverage |
| Affected stories | checks/aggregate-cycle2-turn.log | Durable interruption replay blocks execution/repost until trusted receipt, race and coverage |

Each referenced log retains the executed command and its actual output. Earlier failed or interrupted attempts remain separate and are not passing evidence. Scoped package coverage percentages do not quantify acceptance-criterion coverage.

## Final aggregate and immutable evidence

- `GOLANGCI_LINT_CACHE=/private/tmp/lf210-golangci-cache make check` exited 0. Formatting, vet, pinned golangci-lint v2.13.1 (zero issues), and `go test -race -timeout=20m ./...` passed. Full output: checks/aggregate-make-check.log. The final daemon suite took 367.137s; turn took 738.623s. Other packages passed or used valid Go test cache results as shown in the log.
- `go build ./...` exited 0. Full output: checks/aggregate-build.log. No distribution binary was written.
- Final captures retain 62 changed implementation/test/documentation files with shared manifest digest `9cf7b4ffc3333e59165fe4f65d2ee10f1612994350179408a3bd05e18f321af3`. Each story references its own final aggregate-check and build IDs, bound to these final bytes. Earlier scoped checks remain attached to their original snapshots rather than being reattributed to changed files.

| Story | Final snapshot | Final request ID |
| --- | --- | --- |
| US-001 | us001-run0005-final | a16e82f861e49714491b4fcd3eb6233d |
| US-002 | us002-run0005-final | d1b56594e872595382ac6a39aae06a9f |
| US-003 | us003-run0005-final | dcc8c68d94868c41dd82f08d767f542c |

All final preflights were valid and complete with no diagnostics or unchecked paths. Subsequent receipt queries confirm InputState=valid, EngineeringState=recorded, AcceptanceState=not_applicable, EffectState=pending, PlanState=not_applicable for all three. Factory save/remote effects remain pending; no acceptance or issue closure is claimed.

The helper required explicit invalidation before replacing earlier unaccepted US-001/US-002 engineering. Refresh-event requests `0d6bbfb88bfc95a9fb3a8f0d75fb5c2a` and `d6bf198de2f153bb156b827f1c59bf0e` returned valid input and EngineeringState=rejected for the old engineering; the final replacement submissions above are recorded. Prior evidence and historical acceptance remain preserved.

## Evidence boundaries

Checks use temporary migrated SQLite, fake runners, fake HTTP and controlled clocks/git fixtures. They establish controlled application behavior, not live GitHub delivery guarantees, nested-agent behavior or historical incident reconstruction. No live daemon, runtime database or installed distribution was changed.


## Run 0006 — aggregate engineering recovery (2026-09-11)

Authoritative mode: Implement; workflow version 2. All three supplied frozen stories were already engineering-complete and remained read-only. No acceptance review or new story events were performed.

- `make check`: **FAILED**, exit 2. Formatting and vet passed; pinned golangci-lint v2.13.1 reported `0 issues.`. The race suite ran with `go test -race -timeout=20m ./...`.
- Failure: `internal/teamsource`, `TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly/research-orchestrator`, `aggregate_instruction_parity_test.go:127`: `.claude/agents/research-orchestrator.md: missing the exact handoff pointing at .claude/skills/shared/workflow.md`.
- All other listed packages passed or had no test files; some passing results used Go's cache. `internal/turn` completed in 1035.194s.
- `go build ./...`: **PASSED**, exit 0.

Retained raw output: [make check](run0006-make-check.log), [build](run0006-go-build.log). Per-command exit codes are retained in `run0006-make-check.exit` and `run0006-go-build.exit`. Lint used an isolated writable temporary cache.

Aggregate engineering gate remains failed. Per the supplied recovery instruction, no fixes were attempted. Acceptance is unassessed; save/push remains factory-owned and unconfirmed. This run does not replace or invalidate historical per-story engineering evidence.
