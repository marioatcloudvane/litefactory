# Story005 engineering handoff

Frozen identity: story-005 / #461 / issue ID 5682594544 / revision
`5d8d371fd797599712d45fc9d2dc3c3cdf05ee1671257c5c081214260d9a62bb`.
Dependency story004 engineering was supplied complete. Engineering only: no
acceptance, code, architecture or plan assessment, live forge effect, commit,
push, checkpoint submission or application submission was performed.

## Implemented retirement

Only NewGraphBugRuntime constructs a supported Bug host. BugRuntime is now a
small simplified-only facade; the legacy constructor, lifecycle coordinators,
repair/check/test-child domains, state-specific replies/grants, delivery review,
merge/closure wrappers, native legacy preparation and report/save paths are
removed. Shared strict accepted-result retention, diagnosis rendering and the
existing narrow forge primitives remain. The production graph still owns routes.

The daemon no longer registers, dispatches, cancels or recovers the Bug check
child. Only /approve-bug remains a Bug-specific control; ordinary guidance and
normal task cancellation retain their existing routing. Unsupported pinned
packages are checked before report recovery, locks or scheduling, including
before graph dispatch reservation. Generic orphan run/schedule/lock repair
excludes retained legacy Bug sessions and pre-v2 pins; old work is not converted,
cancelled, resumed or reclassified. A controlled concrete-host scenario verifies
no provider call, attempt allocation, instance change or edit loss on refusal.

All eleven legacy Bug store source files and obsolete writer/accessor fixtures
are removed. The pure BugAnalysis/result contracts, shared Spike check isolation,
pinned-recipe reader, graph waits and attempts, local checkpoint recovery,
canonical source identity, validation generations and shared delivery journal
remain. Old clean-repair observation helpers and the unused delivery-review
renderer are removed. Retained pinned-recipe negative tests were separated from
obsolete clean-source checks rather than discarded. Shared result/broker tests
were adapted to the graph-only store boundary. Gate, surface, DOT inventory and
production-caller guards track the actual retired APIs and the concrete new host.

Both shipped bug_tests.dot copies and their capture/validation contracts are
removed. Legacy workflow documentation points to docs/workflow/bug-runtime.md,
which describes the supported flow and upgrade boundary without promising
factory validation or a second Bug delivery review.

## History and upgrade boundary

Forward migration 0096 creates 18 SQLite BEFORE INSERT/UPDATE/DELETE refusal
triggers on bug_tasks, bug_analyses, bug_repairs, bug_checks, bug_delivery and
bug_events. It alters no surviving row, column, key, foreign key or embedded
payload. Archive planning keeps every family containing these rows, even when
its old lifecycle is terminal. Ancestor deletion remains refused atomically.

The existing migration transaction checks each table named for destruction by
0079 before executing its historical SQL. Any populated dropped table refuses
with an explicit separately-reviewed-preservation-upgrade diagnostic; 0079 stays
unapplied and original records survive. Empty older/fresh installations retain
the normal chain. Existing installations where 0079 already applied preserve
all surviving history; this cannot recover records previously deleted by 0079.
The ordinary exclusive startup upgrade remains the only installation boundary.

Populated persistent temporary database tests compare all columns and logical
row bytes of all six actual tables, including opaque binary evidence, historical
failure/uncertain facts and repair ordinal four. They compare table definitions,
foreign keys and applied-version rows, refuse all three write types per table,
check ancestor deletion, reopen the database and verify unchanged history.
Further tests cover populated pre-0079 refusal, late migration failure rollback,
fresh/empty/current upgrades, idempotence, historical orphan preservation and
terminal-history cleanup refusal. No daemon state or real old work was touched.

## Exact inventory

STORY005_FILE_INVENTORY.json contains the exact paths: 3 new files, 31 modified
files and 74 removed files. Shared new files inherited from preceding engineers
are listed as modified where this retirement changed them. Session measurement
and this handoff are additional session artifacts.

## Reproducible measurement

Run from the repository root:

`python3 .aiteam/lf-453-simplify-the-bug-workflow-one-stop-rule/2026-10-02/simplify-the-bug-workflow-one-stop-rule/measure_bug_retirement.py`

The immutable baseline f6c814ec4f8df34ec2e7c3c262f43ef5383f04ef reproduces 57
selected files / 15,524 logical lines. The script uses splitlines, including
blank/comment lines, and a deterministic line diff without heuristic autojunk.
Only deleted lines in the saved selection earn credit. Every added non-test Go
line anywhere in internal/ and cmd/ is charged, including shared additions across
all five stories; unrelated deletions earn nothing. Relocation adds a charged
line as well as a deletion and earns no net credit. The per-file baseline and
change inventory is retained in BUG_RETIREMENT_MEASUREMENT.json.

At the final worker snapshot: 13,610 credited removed lines minus 5,169 charged
added lines = **8,441 net lines removed**, 679 above the 7,762 target; shortfall 0.
The current non-test-Go endpoint digest is
071b1ff3af3c79cbdefcfd007ae354cfd0911306e4ef40db768f4934b48eeae9.
This is a measurement identity, not the factory implementation snapshot digest.
An independent rerun produced byte-identical JSON, SHA256
5bf490fe9d7607e9c490480d39a153ead39a15400bebccc8d54dc6fc9419b794.
All 92 migration files in the saved baseline compare unchanged. There are 15
historical SQL files in versions 0066–0080, retained; the resulting complete
migration chain has 96 files. SQL-file counts are separate from the six legacy
Bug tables retired from active use (six retained read-only, zero active writers).

## Final actual executed checks

Passing final targeted command:

`go test -race ./internal/store ./internal/turn ./internal/daemon ./internal/graphexec ./internal/teamsource ./internal/git ./internal/bugchecks -run 'TestBugArchive|TestBugResultBoundary|TestBugControlCommands|TestGraphBugConcreteHost|TestExecuteNodeActionHasOnlyApprovedProductionCallers|TestGraphShippedDOTScope|TestCriticalSections|TestSourceCheckpoint|TestSourceIdentity|TestGraphDeliveryObserved|TestGraphMechanical|TestGraphEffectPersistent|TestConfiguredCommandInputs|TestBugWorkflowBoundary|TestBugWorkflow|TestCaptureBug|TestBugChecksPinnedRecipe|TestBugChecksRecipeRejects'`

Actual final output:

```
ok github.com/cloudvane/litefactory/internal/store 34.469s
ok github.com/cloudvane/litefactory/internal/turn 17.897s
ok github.com/cloudvane/litefactory/internal/daemon 2.406s
ok github.com/cloudvane/litefactory/internal/graphexec 2.003s
ok github.com/cloudvane/litefactory/internal/teamsource 3.341s
ok github.com/cloudvane/litefactory/internal/git 6.994s
ok github.com/cloudvane/litefactory/internal/bugchecks 3.462s
```

`go vet ./internal/store ./internal/turn ./internal/daemon ./internal/teamsource ./internal/git ./internal/graphexec ./internal/bugchecks` passed with no output.

`gofmt -l internal/store internal/turn internal/daemon internal/teamsource internal/git internal/graphexec internal/bugchecks` passed with no output.

Initial failures from retired API fixture references, removed gate methods and
obsolete direct-dispatch counts were corrected through explicit inventory
updates and retained-coverage adaptation before the final passing command.
No unresolved engineering blocker remains. Full make check, the complete race
suite and final aggregate capture remain orchestrator-owned; no owner acceptance
or authentic provider/forge delivery is claimed from these controlled tests.

### Aggregate engineering fix cycle 1

Resolved the 45 aggregate lint findings without suppressions: exported API documentation, lowercase factory diagnostics, equivalent boolean simplifications, current caller context for source/log lookup, no unused terminal-result argument, and unused/shadowing fixture cleanup. The fixer freshness refusal helper preserves the separate process result and reports its actual diagnostic. Direct retained-result tests now exercise accepted/missing/current revision authority; the actual fixer execution test covers receipt, empty, prose, malformed and conflicting terminal text.

Executed `GOLANGCI_LINT_CACHE=/private/tmp/lf453-lint-worker golangci-lint run ./internal/bugrepair/... ./internal/git/... ./internal/store/... ./internal/graphexec/... ./internal/turn/...` with pinned v2.13.1: exit 0, `0 issues.`

Executed `go test -race ./internal/turn ./internal/store ./internal/git ./internal/bugrepair ./internal/graphexec -run 'TestAcceptRetained|TestRetainedRoute|TestGraphFixer|TestGraphBugConcreteHost|TestGraphDiagnosis|TestValidation|TestPlanGraphIntegration|TestWorkflowAdmission|TestWorkflowPolicy|TestBugArchive|TestSourceIdentity|TestSourceCheckpoint|TestGraphStopPersistent'`: exit 0; turn 40.586s, store 47.610s, git 4.120s, bugrepair 2.016s; graphexec had no matching tests. `gofmt -l` on these package directories returned no output.

Refreshed measurement: 13,610 credited removal minus 5,169 charged additions = 8,441 net, target exceeded by 679. Endpoint non-test Go digest: `071b1ff3af3c79cbdefcfd007ae354cfd0911306e4ef40db768f4934b48eeae9`. Aggregate repository checks remain orchestrator-owned. No acceptance conclusion is asserted.

### Aggregate engineering fix cycle 2

Fixed actual failures without skipping coverage: Worktree allowlist now documents the approved ObserveSource, CheckpointSource and SaveSource seams; the archive registry archives graph_effects before its referenced attempts/instances. The six legacy read-only archive guards remain unchanged. The fixer role keeps its original responsibilities within the required ordered nine sections, and its focused result skill includes shared preflight/submit/receipt and an illustrative blocked JSON example.

Three Plan resource source/embedded discrepancies existed before this correction (neither side had a working-tree diff). Canonical `teams/source/plan` content remains untouched; only the embedded plan-set, plan-technical and plan-acceptance copies were synchronized for required package closure. This changes no authored Plan policy.

Targeted race command with `-timeout=3m` across git/store/teamsource/harness for the seven explicit failed guards: git 1.597s, store 4.053s, harness 3.066s; corrected teamsource rerun 1.774s, all passing. `go test -race -timeout=3m ./internal/cli ./internal/daemon ./internal/store ./internal/turn -run 'TestCentralTeamCommandsOutsideGit|TestArchivePlanIntegrationForgetGatesThenSuccess|TestMCPCreationDB_ExpiryScopeAndNextLaunch|TestGraphFixerAcceptedSuccessRequiresFreshInputsAndSuccessfulProcess'` passed: cli 6.751s, daemon 28.489s, store 12.705s, turn 12.899s. The aggregate eight-minute alarms named tests active for only 2–12 seconds after cumulative suite progress; no isolated deadlock reproduced.

`go test -race -count=3 -timeout=1m ./internal/harness -run '^TestRunStreamsEventsAsTheyArrive$'` passed 2.723s with its original five-second streaming invariant unchanged. Scoped pinned v2.13.1 lint on store/git/teamsource: `0 issues.` Formatting is clean. Final accounting after the registry addition: 13,610 credited lines minus 5,170 charged replacement lines = 8,440 net, exceeding 7,762 by 678. Endpoint Go digest `44d0a29b2cf96607918e54b41b281b6bb403d1ec02b972f6c19c172c372276d9`. Aggregate full checks remain orchestrator-owned and must still complete.
