# Run 0010 verification

The worktree started clean with the factory merge completed. Three subagents reviewed storage, source/provider boundaries and CLI integration; one implemented the storage fix. No additional merge conflicts or concrete boundary/CLI defects were found.

Changed only the aggregate prevention record cap and its regression test. The old 1 MiB cap could reject individually valid bounded components after multiple corrections. The cap is now 2 MiB; source, evidence, draft, result and analyst output limits are unchanged. The regression retains an exact 512 KiB manifest, four 64 KiB drafts with 64 KiB diagnostics each, a 256 KiB result, then closes/reopens storage and compares the complete saved history and final result.

Complete checks covered 39 affected packages and direct importers on the current merged tree. Race detection was used except for store, CLI, daemon and turn. After the storage change, final checks used GOMAXPROCS=2 and -p2. Unchanged successful results reused the Go test cache; changed tests and initial failures ran again. The complete CLI package includes the retained bug-analysis main-path test.

The initial race pass failed two timing-sensitive tests: ordinary provider exits exceeded a two-second probe deadline, and a 50 ms broker handler deadline test failed its wait assertion under concurrent load. Both complete packages passed in the final lower-concurrency check. These failures were not hidden by test edits. Both initial and final outputs follow.

The final store package passed including the new bounded-component reopen regression; the final turn package passed in 530.586s. `go build ./...`, `gofmt -l .` (empty output), scoped pinned lint and `git diff --check` passed. No whole test suite, commit, stage, push, approval or publication was performed by this worker. Acceptance remains with Validate.

## Final race package selection

```text
./cmd/litefactory ./internal/agent ./internal/agentresult ./internal/codex ./internal/doctor ./internal/drift ./internal/github ./internal/graphexec ./internal/harness ./internal/instructionpackage ./internal/loganalysis ./internal/logevidence ./internal/logreader ./internal/messaging ./internal/messaging/ask ./internal/messaging/discord ./internal/messaging/feature ./internal/messaging/prompts ./internal/messaging/response ./internal/meter ./internal/meterview ./internal/prevention ./internal/projectconnection ./internal/providerprocess ./internal/report ./internal/repos ./internal/resultservice ./internal/rights ./internal/standards ./internal/standardsexceptions ./internal/store/storetest ./internal/submission ./internal/teamrelease ./internal/teamsource ./internal/webhookreg
```

## 992-r10-race.log

```text
ok  	github.com/cloudvane/litefactory/cmd/litefactory	7.939s
ok  	github.com/cloudvane/litefactory/internal/agent	8.909s
ok  	github.com/cloudvane/litefactory/internal/agentresult	11.812s
ok  	github.com/cloudvane/litefactory/internal/codex	292.206s
ok  	github.com/cloudvane/litefactory/internal/doctor	134.608s
ok  	github.com/cloudvane/litefactory/internal/drift	54.317s
ok  	github.com/cloudvane/litefactory/internal/github	10.809s
ok  	github.com/cloudvane/litefactory/internal/graphexec	7.843s
ok  	github.com/cloudvane/litefactory/internal/harness	266.768s
ok  	github.com/cloudvane/litefactory/internal/instructionpackage	11.434s
ok  	github.com/cloudvane/litefactory/internal/loganalysis	44.843s
ok  	github.com/cloudvane/litefactory/internal/logevidence	42.747s
ok  	github.com/cloudvane/litefactory/internal/logreader	7.475s
ok  	github.com/cloudvane/litefactory/internal/messaging	300.000s
ok  	github.com/cloudvane/litefactory/internal/messaging/ask	7.737s
ok  	github.com/cloudvane/litefactory/internal/messaging/discord	17.699s
ok  	github.com/cloudvane/litefactory/internal/messaging/feature	4.651s
ok  	github.com/cloudvane/litefactory/internal/messaging/prompts	3.825s
ok  	github.com/cloudvane/litefactory/internal/messaging/response	3.910s
ok  	github.com/cloudvane/litefactory/internal/meter	3.581s
ok  	github.com/cloudvane/litefactory/internal/meterview	11.474s
ok  	github.com/cloudvane/litefactory/internal/prevention	23.686s
ok  	github.com/cloudvane/litefactory/internal/projectconnection	20.916s
--- FAIL: TestProbeTransientRecoveryAndBounds (17.88s)
    --- FAIL: TestProbeTransientRecoveryAndBounds/exit_70 (3.81s)
        conversation_capability_test.go:35: ordinary exit retried: [1s] conversation capability probe failed: exit status 7
    --- FAIL: TestProbeTransientRecoveryAndBounds/exit_71 (2.77s)
        conversation_capability_test.go:35: ordinary exit retried: [1s] <nil>
FAIL
FAIL	github.com/cloudvane/litefactory/internal/providerprocess	32.887s
ok  	github.com/cloudvane/litefactory/internal/report	13.625s
ok  	github.com/cloudvane/litefactory/internal/repos	6.262s
?   	github.com/cloudvane/litefactory/internal/resultservice	[no test files]
ok  	github.com/cloudvane/litefactory/internal/rights	11.801s
ok  	github.com/cloudvane/litefactory/internal/standards	60.179s
ok  	github.com/cloudvane/litefactory/internal/standardsexceptions	17.651s
?   	github.com/cloudvane/litefactory/internal/store/storetest	[no test files]
--- FAIL: TestBrokerDeadlineCancelsHandler (30.29s)
    broker_test.go:209: request deadline did not cancel handler
FAIL
FAIL	github.com/cloudvane/litefactory/internal/submission	33.184s
ok  	github.com/cloudvane/litefactory/internal/teamrelease	175.273s
ok  	github.com/cloudvane/litefactory/internal/teamsource	3.762s
ok  	github.com/cloudvane/litefactory/internal/webhookreg	16.364s
FAIL
```

## 992-r10-plain.log

```text
ok  	github.com/cloudvane/litefactory/internal/store	181.806s
ok  	github.com/cloudvane/litefactory/internal/cli	62.599s
ok  	github.com/cloudvane/litefactory/internal/daemon	221.569s
ok  	github.com/cloudvane/litefactory/internal/turn	657.489s
```

## 992-r10-final-race.log

```text
ok  	github.com/cloudvane/litefactory/cmd/litefactory	(cached)
ok  	github.com/cloudvane/litefactory/internal/agent	(cached)
ok  	github.com/cloudvane/litefactory/internal/agentresult	(cached)
ok  	github.com/cloudvane/litefactory/internal/codex	(cached)
ok  	github.com/cloudvane/litefactory/internal/doctor	(cached)
ok  	github.com/cloudvane/litefactory/internal/drift	(cached)
ok  	github.com/cloudvane/litefactory/internal/github	(cached)
ok  	github.com/cloudvane/litefactory/internal/graphexec	2.201s
ok  	github.com/cloudvane/litefactory/internal/harness	(cached)
ok  	github.com/cloudvane/litefactory/internal/instructionpackage	(cached)
ok  	github.com/cloudvane/litefactory/internal/loganalysis	(cached)
ok  	github.com/cloudvane/litefactory/internal/logevidence	(cached)
ok  	github.com/cloudvane/litefactory/internal/logreader	(cached)
ok  	github.com/cloudvane/litefactory/internal/messaging	(cached)
ok  	github.com/cloudvane/litefactory/internal/messaging/ask	(cached)
ok  	github.com/cloudvane/litefactory/internal/messaging/discord	(cached)
ok  	github.com/cloudvane/litefactory/internal/messaging/feature	(cached)
ok  	github.com/cloudvane/litefactory/internal/messaging/prompts	(cached)
ok  	github.com/cloudvane/litefactory/internal/messaging/response	(cached)
ok  	github.com/cloudvane/litefactory/internal/meter	(cached)
ok  	github.com/cloudvane/litefactory/internal/meterview	(cached)
ok  	github.com/cloudvane/litefactory/internal/prevention	7.220s
ok  	github.com/cloudvane/litefactory/internal/projectconnection	(cached)
ok  	github.com/cloudvane/litefactory/internal/providerprocess	18.844s
ok  	github.com/cloudvane/litefactory/internal/report	(cached)
ok  	github.com/cloudvane/litefactory/internal/repos	(cached)
?   	github.com/cloudvane/litefactory/internal/resultservice	[no test files]
ok  	github.com/cloudvane/litefactory/internal/rights	(cached)
ok  	github.com/cloudvane/litefactory/internal/standards	(cached)
ok  	github.com/cloudvane/litefactory/internal/standardsexceptions	(cached)
?   	github.com/cloudvane/litefactory/internal/store/storetest	[no test files]
ok  	github.com/cloudvane/litefactory/internal/submission	1.922s
ok  	github.com/cloudvane/litefactory/internal/teamrelease	(cached)
ok  	github.com/cloudvane/litefactory/internal/teamsource	(cached)
ok  	github.com/cloudvane/litefactory/internal/webhookreg	(cached)
```

## 992-r10-final-plain.log

```text
ok  	github.com/cloudvane/litefactory/internal/store	71.563s
ok  	github.com/cloudvane/litefactory/internal/cli	42.834s
ok  	github.com/cloudvane/litefactory/internal/daemon	(cached)
```

## 992-r10-final-turn.log

```text
ok  	github.com/cloudvane/litefactory/internal/turn	530.586s
```

## 992-r10-lint.log

```text
0 issues.
```

## 992-r10-store-lint.log

```text
0 issues.
```

Preflight was valid and complete. Submission input was accepted; matching receipt read for request `23bbf1fdc798b55a6b0921b4d2d8d70b`. Engineering, acceptance and publication remained pending in that receipt.
