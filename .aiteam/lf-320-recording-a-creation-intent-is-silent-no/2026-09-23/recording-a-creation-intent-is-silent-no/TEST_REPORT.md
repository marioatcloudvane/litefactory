# Executed engineering evidence — run 599

Frozen revisions:
- creation-proposal-guidance (#375): 75b548b6628f8328cc9407c7405ef2640dd247e86c6091889291d5068e3a004a.
- creation-approval-acknowledgment (#376): 8396af1a0aa27ec1c4dca969d136e0d60a91154c7ab9e38cce02d858e540485d.

## Scoped checks

Guidance renderer/context cases, explicit and recovered preparation, and legacy/graph post-run presentations passed with race detection. Assertions preserve exact proposal/approval bytes and exclude execution/consent side effects.

Acknowledgment renderer cases cover immutable hash/relationship and original nanosecond UTC timestamps, historical wording, all publication contexts, conditional renewal, invalid input and unsafe output. SQLite/local HTTP cases exercise admission gaps, retention, dispatch, lost responses, receipt confirmation and transactional completion, strict trusted reconciliation, ambiguous evidence pending without resend, changed text/mode/configuration/proposal after expiry, renewal, replay and no backfill. Existing rejection and legacy/graph permission regressions passed.

Final combined scoped execution after lint fixes:

```sh
go test -race ./internal/report ./internal/store ./internal/daemon ./internal/turn -run 'TestReporter_Creation|TestMCPRuntime_CreationPublishingAvailable|TestCreationGuidanceIO|TestCreationSuccessIO|TestReportCreationPresentation|TestCreationAcceptance|TestReportDeliveryHTTPAndSQLiteFailureWindows|TestCreationAuthorizerDB_LiveGateGrantAndMode|TestCreationProposalCatalog_InstallationGrantsNothingAndKeepsGraphExclusions|TestCreationRejectionIO_' -count=1
```

```text
ok  	github.com/cloudvane/litefactory/internal/report	2.245s
ok  	github.com/cloudvane/litefactory/internal/store	19.169s
ok  	github.com/cloudvane/litefactory/internal/daemon	22.742s
ok  	github.com/cloudvane/litefactory/internal/turn	19.057s

```

## Final scoped captures

Implementation digest: d558600819b44cf1453a4a46f5ed1c78610db2623832773231835b2c249a4d55 (20 implementation/test files).

- Guidance snapshot `lf320-guidance-599-v3`, check `guidance-final-v3-599`, receipt `841b63a1be177654ac24c00ac86e9c89`.
- Acknowledgment snapshot `lf320-ack-599-v2`, check `ack-final-v2-599`, receipt `c4bcb55ea8e1a89acd1b1d440851cb86`.
- Both receipts: input valid; engineering recorded; acceptance not applicable; remote effects pending.

## Corrections and limitations

Initial acknowledgment regression expected obsolete direct consumption; it was updated to fault journal completion. An initial timestamp assertion was corrected to preserve RFC3339Nano. The first aggregate lint run found a missing constant-block comment and unchecked type assertion; both were fixed, and final scoped tests reran. Linter cache moved to writable temporary storage for the aggregate rerun.

Evidence is offline: real temporary migrated SQLite and local HTTP forge, not live daemon/GitHub. Acceptance/code/architecture/plan review was not performed. Aggregate gate is recorded below when complete.

## Aggregate outcome: INCOMPLETE

- Final-byte `go build ./...`: passed (exit 0).
- `gofmt -l .`: passed (empty output).
- `GOLANGCI_LINT_CACHE=/tmp/lf320-golangci-cache make check`: formatting, vet and pinned golangci-lint v2.13.1 passed (0 issues). Its repository-wide race stage reported failures in harness and teamsource, then exceeded the roughly ten-minute gate ceiling while remaining packages were still active; interrupted with exit 130. This is not an aggregate pass.
- Bounded rerun `go test -race ./internal/harness ./internal/teamsource -run 'TestRunStreamsEventsAsTheyArrive|TestPlanPackageStageContractsAndResearchPreservation|TestPlanPackageClosureAndDigest' -count=1 -timeout=2m`: harness passed; teamsource still failed.

```text
ok  	github.com/cloudvane/litefactory/internal/harness	3.488s
--- FAIL: TestPlanPackageStageContractsAndResearchPreservation (0.04s)
    plan_contract_test.go:314: plan-story.md missing contract "who needs what and why"
    plan_contract_test.go:314: plan-story.md missing contract "unprefixed name"
    plan_contract_test.go:314: plan-story.md missing contract "actor/want/benefit"
    plan_contract_test.go:314: plan-story.md missing contract "In-Scope/Not in Scope"
    plan_contract_test.go:314: plan-story.md missing contract "Never write architecture, tasks"
    plan_contract_test.go:314: plan-story.md missing contract "placeholders"
    plan_contract_test.go:314: plan-story.md missing contract "sub-issue"
--- FAIL: TestPlanPackageClosureAndDigest (0.04s)
    plan_contract_test.go:107: source/embedded mismatch: plan-story.md
FAIL
FAIL	github.com/cloudvane/litefactory/internal/teamsource	1.746s
FAIL

```

The implementation engineer's analysis confirmed unchanged baseline divergence: HEAD's embedded `internal/teamsource/plan/plan-story.md` was rewritten by commit `adfe773`, while `teams/source/plan/plan-story.md` retains the original contract. `git diff HEAD -- internal/harness internal/teamsource teams/source` is empty. Choosing/synchronizing Plan instructions is outside these frozen creation-feedback stories; no role content or test expectation was weakened. Harness's isolated pass is retained alongside its aggregate failure, without asserting a proven cause. No successful aggregate capture was submitted.

## Aggregate recovery — run 601 (2026-09-24): PASSED

Both factory-selected stories were already engineering-complete. This run performed aggregate engineering checks only and left their implementation and tests unchanged.

- `go build ./...`: passed, exit 0, no output.
- `GOLANGCI_LINT_CACHE=/tmp/lf320-golangci-cache make check TEST_TIMEOUT=8m`: passed, exit 0, 362.62 seconds. The command had a 480-second wall-clock bound; it completed naturally.
- The make check gate verified formatting (empty gofmt findings), vet, pinned golangci-lint v2.13.1 (0 issues), and the full repository race suite.
- The prior harness and Plan contract failures did not reproduce: harness and teamsource passed in this full execution. No root-cause or acceptance-review conclusion is inferred.

Actual aggregate output:

```text
Command: GOLANGCI_LINT_CACHE=/tmp/lf320-golangci-cache make check TEST_TIMEOUT=8m
go vet ./...
golangci-lint v2.13.1 (installed)
0 issues.
go test -race -timeout=8m ./...
ok  	github.com/cloudvane/litefactory/cmd/litefactory	1.977s
?   	github.com/cloudvane/litefactory/cmd/team-benchmark	[no test files]
?   	github.com/cloudvane/litefactory/cmd/team-publish	[no test files]
ok  	github.com/cloudvane/litefactory/internal/agent	1.758s
ok  	github.com/cloudvane/litefactory/internal/agentresult	2.793s
ok  	github.com/cloudvane/litefactory/internal/attach	2.644s
ok  	github.com/cloudvane/litefactory/internal/cli	79.668s
ok  	github.com/cloudvane/litefactory/internal/codex	65.958s
ok  	github.com/cloudvane/litefactory/internal/config	2.865s
ok  	github.com/cloudvane/litefactory/internal/confirm	1.962s
ok  	github.com/cloudvane/litefactory/internal/daemon	124.186s
ok  	github.com/cloudvane/litefactory/internal/git	44.466s
ok  	github.com/cloudvane/litefactory/internal/github	3.393s
ok  	github.com/cloudvane/litefactory/internal/gittest	3.462s
ok  	github.com/cloudvane/litefactory/internal/graphexec	3.006s
ok  	github.com/cloudvane/litefactory/internal/harness	71.131s
ok  	github.com/cloudvane/litefactory/internal/identity	23.820s
ok  	github.com/cloudvane/litefactory/internal/launchd	5.516s
ok  	github.com/cloudvane/litefactory/internal/logging	2.914s
ok  	github.com/cloudvane/litefactory/internal/mcpadapter	3.535s
ok  	github.com/cloudvane/litefactory/internal/meter	3.497s
ok  	github.com/cloudvane/litefactory/internal/naming	2.829s
ok  	github.com/cloudvane/litefactory/internal/planbrief	2.866s
ok  	github.com/cloudvane/litefactory/internal/redact	3.291s
ok  	github.com/cloudvane/litefactory/internal/render	3.105s
ok  	github.com/cloudvane/litefactory/internal/report	8.428s
ok  	github.com/cloudvane/litefactory/internal/repos	4.162s
ok  	github.com/cloudvane/litefactory/internal/schedule	2.719s
ok  	github.com/cloudvane/litefactory/internal/secrets	2.984s
ok  	github.com/cloudvane/litefactory/internal/store	61.057s
ok  	github.com/cloudvane/litefactory/internal/submission	3.659s
ok  	github.com/cloudvane/litefactory/internal/team	3.815s
ok  	github.com/cloudvane/litefactory/internal/teampublish	4.041s
ok  	github.com/cloudvane/litefactory/internal/teamremote	2.529s
ok  	github.com/cloudvane/litefactory/internal/teamsource	24.456s
ok  	github.com/cloudvane/litefactory/internal/turn	331.291s

Exit: 0; elapsed: 362.62s
```

These results establish offline engineering checks against the current worktree. They do not establish live daemon behavior, acceptance, save or publication. Aggregate capture and shared-result submission are handled separately by the parent dispatcher. No new story event was emitted.
