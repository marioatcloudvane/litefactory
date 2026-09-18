# Engineering test report — run 0012

Workflow version 2: engineering evidence only; acceptance is reserved for Validate. All results below are actual executions against scoped implementation bytes.

## Core

`go test ./internal/graphexec -short -race -cover -v` passed: 11 top-level tests, 50 subtests, 92.0% coverage. Attribute/default preservation, digest, invalid DOT, aggregated validation, bounded cycles, decisions, tiers/paths, cloning and purity checks passed. Dependency resolved to Gonum v0.16.0.

Snapshot: `lf315-run12-core-v1`; check: `lf315-core-scoped-v1`; implementation digest: `c45e1ba9ac56447de38ee5ef3a8c098c99b1990e31b6ed07cd8a1ac0ac9a7581`. Checkpoint request/receipt: `54edda25e351bdbe99c31261eda2b95d`. Input valid, engineering recorded, acceptance not applicable, remote effects pending.

## Persistence

`go test ./internal/store -run '^TestGraph' -count=1 -v` passed (1.224s). The same selection with `-race` passed (15.801s), nine top-level tests. Real SQLite tests covered migration/schema/indexes/FKs, pins/path refusal, attempt counts/limits, session isolation, atomic rollback, trace retention, due filtering and reopening. An initial duplicate-session fixture was corrected before the passing runs; no production defect was found.

Snapshot: `lf315-run12-store-v1`; check: `lf315-store-scoped-v1`; digest: `d0eeda33fe8321ade66280742e860adba232656fe683bb188cd1185af328036f`. Checkpoint request/receipt: `494728dacdc623908b9a02f53c672b5b`. Input valid, engineering recorded, acceptance not applicable, remote effects pending.

## Remaining gates

Activity, scheduler, composition and aggregate gates are pending. Build, go test ./... and make check will run after the last story lands.

- Run 0012 stopped after implement-orchestrator failed with HTTP 400 unsupported_parameter: access_programs.cyber is not enabled for this organization. Core and persistence engineering checkpoints are retained. Activity production code and daemon helpers are partial work; remaining scheduler test agent was interrupted. Activity scoped tests, scheduler scoped tests, composition and aggregate gates remain incomplete. No acceptance, full completion, commit or push is claimed.

## Run 0014 — executed scoped evidence

- Turn: `go test ./internal/turn -run 'TestGraph(ActivityRegistry|Node)' -count=1 -race -timeout=3m -v` passed (35.126s). Real migrated SQLite checks cover persisted dispatch, diagnostics/refusals, cloning, terminal handling, atomic failure and limits.
- Daemon selection: `go test ./internal/daemon -run '^TestGraphPollDueReadOnlySelection$' -count=1 -race -timeout=3m` passed (4.851s).
- Structural/unit checks: `go test ./internal/turn ./internal/daemon -run 'TestExecuteNodeActionHasNoProductionCaller|TestGraphActivitiesHaveNoClockOrRetryLoop|TestGraphActivityRegistry|TestGraphBackoff|TestGraphPollHasNoProductionCallerIncludingTick|TestGraphHelpersPureAndNonblocking' -short -race -cover -count=1` passed (turn 3.264s, daemon 2.500s).
- Composition: `go test ./internal/turn -run '^TestGraphComposition' -count=1 -v` passed (2.577s); matching race run passed (11.597s). Proves traversal, exact persisted records, bounded per-node loop history and between-step close/reopen continuation.
- `go build ./...` passed (5.800s). Full repository tests and make check still pending at this entry.

All activity execution is synthetic. These tests establish no authentic provider execution, uncertain-effect reconciliation or recovery during an in-flight dispatch. Acceptance remains for Validate.

## Run 0014 final aggregate results

AC-032 remains blocked: unchanged teamsource source-contract tests require docs/story-contract-instruction-manifest.md and docs/agent-auxiliary-contracts.md, deleted by baseline commit bd0b355f77fee415e2ba7a6c33701bfff74a9ed0. No in-scope authorization exists to restore those documents or alter unrelated tests. The full ordinary test gate reported these failures and reached its 480-second ceiling; make check passed formatting, vet and pinned lint (0 issues) but reached its 480-second ceiling in the full race suite. Aggregate completion is not proven.

No passing aggregate capture was submitted. Linter cache-write warnings were nonfatal; lint reported 0 issues.

Actual ordinary gate output:
```text
$ go test -timeout=8m ./...
ok  	github.com/cloudvane/litefactory/cmd/litefactory	0.687s
?   	github.com/cloudvane/litefactory/cmd/team-benchmark	[no test files]
?   	github.com/cloudvane/litefactory/cmd/team-publish	[no test files]
ok  	github.com/cloudvane/litefactory/internal/agent	1.085s
ok  	github.com/cloudvane/litefactory/internal/agentresult	1.919s
ok  	github.com/cloudvane/litefactory/internal/attach	1.418s
ok  	github.com/cloudvane/litefactory/internal/cli	138.970s
ok  	github.com/cloudvane/litefactory/internal/codex	62.286s
ok  	github.com/cloudvane/litefactory/internal/config	1.084s
ok  	github.com/cloudvane/litefactory/internal/confirm	1.728s
ok  	github.com/cloudvane/litefactory/internal/daemon	257.952s
ok  	github.com/cloudvane/litefactory/internal/git	138.124s
ok  	github.com/cloudvane/litefactory/internal/github	2.615s
ok  	github.com/cloudvane/litefactory/internal/gittest	6.006s
ok  	github.com/cloudvane/litefactory/internal/graphexec	2.492s
ok  	github.com/cloudvane/litefactory/internal/harness	68.655s
ok  	github.com/cloudvane/litefactory/internal/identity	24.169s
ok  	github.com/cloudvane/litefactory/internal/launchd	5.483s
ok  	github.com/cloudvane/litefactory/internal/logging	2.235s
ok  	github.com/cloudvane/litefactory/internal/mcpadapter	2.752s
ok  	github.com/cloudvane/litefactory/internal/meter	2.707s
ok  	github.com/cloudvane/litefactory/internal/naming	2.251s
ok  	github.com/cloudvane/litefactory/internal/planbrief	2.603s
ok  	github.com/cloudvane/litefactory/internal/redact	2.540s
ok  	github.com/cloudvane/litefactory/internal/render	2.471s
ok  	github.com/cloudvane/litefactory/internal/report	8.207s
ok  	github.com/cloudvane/litefactory/internal/repos	11.842s
ok  	github.com/cloudvane/litefactory/internal/schedule	2.917s
ok  	github.com/cloudvane/litefactory/internal/secrets	3.336s
ok  	github.com/cloudvane/litefactory/internal/store	59.587s
ok  	github.com/cloudvane/litefactory/internal/submission	1.959s
ok  	github.com/cloudvane/litefactory/internal/team	3.141s
ok  	github.com/cloudvane/litefactory/internal/teampublish	5.954s
ok  	github.com/cloudvane/litefactory/internal/teamremote	1.224s
--- FAIL: TestAdoptedInstructionManifestExactRoleTargets (0.00s)
    adopted_contract_test.go:62: open ../../docs/story-contract-instruction-manifest.md: no such file or directory
--- FAIL: TestAdoptedCommunicationSourceContract (0.00s)
    --- FAIL: TestAdoptedCommunicationSourceContract/docs/story-contract-instruction-manifest.md (0.00s)
        adopted_contract_test.go:46: open ../../docs/story-contract-instruction-manifest.md: no such file or directory
    --- FAIL: TestAdoptedCommunicationSourceContract/docs/agent-auxiliary-contracts.md (0.00s)
        adopted_contract_test.go:46: open ../../docs/agent-auxiliary-contracts.md: no such file or directory
FAIL
FAIL	github.com/cloudvane/litefactory/internal/teamsource	20.605s

exit_code=124 elapsed_seconds=480.107
```

Actual make check output (repeated cache-permission warning lines omitted):
```text
$ make check
go vet ./...
golangci-lint v2.13.1 (installed)
0 issues.
go test -race -timeout=0 ./...
ok  	github.com/cloudvane/litefactory/cmd/litefactory	2.509s
?   	github.com/cloudvane/litefactory/cmd/team-benchmark	[no test files]
?   	github.com/cloudvane/litefactory/cmd/team-publish	[no test files]
ok  	github.com/cloudvane/litefactory/internal/agent	2.344s
ok  	github.com/cloudvane/litefactory/internal/agentresult	4.575s
ok  	github.com/cloudvane/litefactory/internal/attach	3.323s

exit_code=124 elapsed_seconds=480.145
make: *** [test] Terminated: 15
```

Actual scoped composition output:
```text
=== RUN   TestGraphCompositionTraversal
=== RUN   TestGraphCompositionTraversal/reopen=false
=== RUN   TestGraphCompositionTraversal/reopen=true
--- PASS: TestGraphCompositionTraversal (1.44s)
    --- PASS: TestGraphCompositionTraversal/reopen=false (0.89s)
    --- PASS: TestGraphCompositionTraversal/reopen=true (0.55s)
=== RUN   TestGraphCompositionBoundedLoop
--- PASS: TestGraphCompositionBoundedLoop (0.53s)
PASS
ok  	github.com/cloudvane/litefactory/internal/turn	2.577s
=== RUN   TestGraphCompositionTraversal
=== RUN   TestGraphCompositionTraversal/reopen=false
=== RUN   TestGraphCompositionTraversal/reopen=true
--- PASS: TestGraphCompositionTraversal (5.45s)
    --- PASS: TestGraphCompositionTraversal/reopen=false (3.40s)
    --- PASS: TestGraphCompositionTraversal/reopen=true (2.04s)
=== RUN   TestGraphCompositionBoundedLoop
--- PASS: TestGraphCompositionBoundedLoop (3.86s)
PASS
ok  	github.com/cloudvane/litefactory/internal/turn	11.597s
```
