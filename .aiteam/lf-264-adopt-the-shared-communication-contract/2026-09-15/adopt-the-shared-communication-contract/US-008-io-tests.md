# US-008 real I/O verification

Frozen story #305, issue ID 5463755356, parent ID 5439211198, revision `53dc9a6dc1096297d1f5196dceac1376b5b901e5600820e81f4fa6ab3b323902`, dependencies none, workflow 2.

Executed against HEAD `16634ed33075ea3acfc449bcb6316163bdc0b807` plus current run uncommitted implementation and test changes. Environment: `go version go1.27.0 darwin/arm64`. The orchestrator retains the complete immutable implementation capture. Added test file SHA256: `fc10e0600cf2355f379a846fc292fe90e48e1aefa26082221e305ed718fc4aa8`.

## Result

13 top-level functions passed, 0 failed in both final plain and race runs. Initial plain execution exposed missing temporary directory creation in the new test fixture; fixed that setup and reran. Initial failure output is retained, not attributed to production.

Only new code owned here: `internal/turn/discovery_retirement_test.go`. It composes the same concrete GitHub client and report renderer as daemon setup, exercises real temporary git/SQLite save, observes a controlled localhost HTTP boundary and proves Markdown artifacts trigger no publication, remain saved intact, and cause no second index commit. It does not call the full daemon constructor. Existing shared fixtures are reused; no new dependency, provider, tag or special integration test naming.

Preserved coverage: managed PublishPlan recovery across SQLite close/reopen after lost create/failed attach; complete story details and stale-base refusal; actual link-only index in discussion/completion; rooted index/session symlink and replacement checks; predecessor leftovers saved in a single succeeding commit; clone gate inventory/exclusion/concurrent-clone behavior; real rendered complete-story bytes accepted through HTTP and numeric child ID/retry behavior.

## Exact commands

```sh
gofmt -w internal/turn/discovery_retirement_test.go
go test -count=1 -timeout=8m ./internal/turn ./internal/github -run 'Test(WorkingRunSavesMarkdownWithoutDiscoveringStories|CommitAndPushIncludesPredecessorLeftoversOnSucceedingRun|CriticalSectionsAreSingleAcquisitions|TurnImposesNoAmbientDeadline|ClaudeRunsOutsideTheCloneGate|ConcurrentTurnsAgainstTwoClones|ManagedPublicationRecoversDurableCreateAndAttach|PlanIndexRefusesSymlinksAndConcurrentReplacement|PlanSessionLookupRefusesSymlinkComponents|PlanDiscussionThenCompletion|ARenderedSubIssueIsAcceptedAsASpec|TheSubIssueRequestCarriesTheChildsDatabaseID|CreateSubIssuesResumesAnUnattachedIssue)$' -v
go test -race -count=1 -timeout=8m ./internal/turn ./internal/github -run 'Test(WorkingRunSavesMarkdownWithoutDiscoveringStories|CommitAndPushIncludesPredecessorLeftoversOnSucceedingRun|CriticalSectionsAreSingleAcquisitions|TurnImposesNoAmbientDeadline|ClaudeRunsOutsideTheCloneGate|ConcurrentTurnsAgainstTwoClones|ManagedPublicationRecoversDurableCreateAndAttach|PlanIndexRefusesSymlinksAndConcurrentReplacement|PlanSessionLookupRefusesSymlinkComponents|PlanDiscussionThenCompletion|ARenderedSubIssueIsAcceptedAsASpec|TheSubIssueRequestCarriesTheChildsDatabaseID|CreateSubIssuesResumesAnUnattachedIssue)$' -v
```

Passing output: `US-008-io-plain.log`, `US-008-io-race.log`. Initial fixture failure: `US-008-io-initial-failure.log`.

Race output summary:

```text
--- PASS: TestPlanIndexRefusesSymlinksAndConcurrentReplacement (0.02s)
--- PASS: TestPlanSessionLookupRefusesSymlinkComponents (0.01s)
--- PASS: TestPlanDiscussionThenCompletion (7.41s)
--- PASS: TestManagedPublicationRecoversDurableCreateAndAttach (5.61s)
--- PASS: TestTurnImposesNoAmbientDeadline (0.61s)
--- PASS: TestCriticalSectionsAreSingleAcquisitions (0.81s)
--- PASS: TestWorkingRunSavesMarkdownWithoutDiscoveringStories (6.37s)
--- PASS: TestClaudeRunsOutsideTheCloneGate (6.70s)
--- PASS: TestCommitAndPushIncludesPredecessorLeftoversOnSucceedingRun (7.07s)
--- PASS: TestConcurrentTurnsAgainstTwoClones (11.94s)
ok  	github.com/cloudvane/litefactory/internal/turn	27.315s
--- PASS: TestTheSubIssueRequestCarriesTheChildsDatabaseID (0.01s)
--- PASS: TestCreateSubIssuesResumesAnUnattachedIssue (0.01s)
--- PASS: TestARenderedSubIssueIsAcceptedAsASpec (0.01s)
ok  	github.com/cloudvane/litefactory/internal/github	1.812s
```

## Bounds

Supports AC-003 preserved live helper regression and AC-002 behavior after dead discovery removal. AC-001 source ownership proof belongs to the engineering inventory. No live cohort/drain, legacy reader retirement, authentic host capability, acceptance decision or full aggregate make check is claimed. Local HTTP substitutes only the external forge boundary. US-009 compatibility gates remain separate. Tests use no production service or runtime state.
