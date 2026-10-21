# Run 0008 verification

This attempt reviewed the existing implementation and repaired unavailable remote collection, per-bug membership of shared fix evidence, retention of all four invalid drafts, archive table classifications, and two structure-test inventories. Three subagents reviewed the story; two also implemented fixes.

Complete checks covered 36 affected packages, including direct importers. Race detection was used except for store, CLI, daemon and turn. The CLI package includes TestPreventionAnalysisMainPath with temporary storage and HTTP/provider fixtures. No production endpoint was used.

Initial complete checks failed on archive classifications, SDK transport delegation inventory, and embedded role inventory. Those failures were corrected and the failing packages passed complete reruns. The first lint invocation could not write its cache; the configured writable-cache run reports zero issues.

`go build ./...`, `gofmt -l .` (empty output), and `git diff --check` passed. No whole-repository test/check command, commit, push, approval or publication was performed by this worker. Acceptance is reserved for Validate.

The inherited metadata amplification guards were reviewed: at most 128 coverage entries/64 KiB and 128 link candidates per issue, with omissions retained explicitly.

## Executed check output

### 992-current-race.log

```text
ok  	github.com/cloudvane/litefactory/cmd/litefactory	4.074s
ok  	github.com/cloudvane/litefactory/internal/agent	(cached)
ok  	github.com/cloudvane/litefactory/internal/agentresult	(cached)
ok  	github.com/cloudvane/litefactory/internal/codex	163.425s
ok  	github.com/cloudvane/litefactory/internal/doctor	53.432s
--- FAIL: TestEveryEndpointGoesThroughTheSDK (0.01s)
    sdk_test.go:397: analysis_bounds.go:70 builds or sends an HTTP request by hand (.RoundTrip(): resp, err := next.RoundTrip(req)
        
        One HTTP stack means one credential path, which is the whole reason D1 was a full migration rather than a hybrid.
    sdk_test.go:397: polling_cache.go:69 builds or sends an HTTP request by hand (.RoundTrip(): resp, err = t.next.RoundTrip(request)
        
        One HTTP stack means one credential path, which is the whole reason D1 was a full migration rather than a hybrid.
FAIL
FAIL	github.com/cloudvane/litefactory/internal/github	4.697s
ok  	github.com/cloudvane/litefactory/internal/graphexec	(cached)
ok  	github.com/cloudvane/litefactory/internal/harness	(cached)
ok  	github.com/cloudvane/litefactory/internal/logevidence	9.411s
ok  	github.com/cloudvane/litefactory/internal/logreader	(cached)
ok  	github.com/cloudvane/litefactory/internal/messaging	160.083s
ok  	github.com/cloudvane/litefactory/internal/messaging/ask	5.305s
ok  	github.com/cloudvane/litefactory/internal/messaging/discord	7.704s
ok  	github.com/cloudvane/litefactory/internal/messaging/feature	4.888s
?   	github.com/cloudvane/litefactory/internal/messaging/response	[no test files]
ok  	github.com/cloudvane/litefactory/internal/meter	5.555s
ok  	github.com/cloudvane/litefactory/internal/meterview	3.879s
ok  	github.com/cloudvane/litefactory/internal/projectconnection	7.553s
ok  	github.com/cloudvane/litefactory/internal/providerprocess	25.291s
ok  	github.com/cloudvane/litefactory/internal/report	9.309s
ok  	github.com/cloudvane/litefactory/internal/repos	4.685s
?   	github.com/cloudvane/litefactory/internal/resultservice	[no test files]
ok  	github.com/cloudvane/litefactory/internal/rights	5.637s
ok  	github.com/cloudvane/litefactory/internal/standards	12.277s
?   	github.com/cloudvane/litefactory/internal/store/storetest	[no test files]
ok  	github.com/cloudvane/litefactory/internal/submission	3.420s
ok  	github.com/cloudvane/litefactory/internal/teamrelease	42.103s
ok  	github.com/cloudvane/litefactory/internal/teamsource	3.157s
ok  	github.com/cloudvane/litefactory/internal/webhookreg	14.638s
FAIL
```

### 992-current-heavy.log

```text
--- FAIL: TestArchiveIntegrationSuiteRowsRetained (0.04s)
    archive_integration_test.go:977: registry problems [prevention_analyses is not classified prevention_analysis_proposals is not classified] <nil>
    archive_integration_test.go:74: archive logs:
FAIL
FAIL	github.com/cloudvane/litefactory/internal/daemon	121.626s
ok  	github.com/cloudvane/litefactory/internal/turn	489.440s
FAIL
```

### 992-current-prevention.log

```text
ok  	github.com/cloudvane/litefactory/internal/prevention	10.104s
```

### 992-current-cli2.log

```text
ok  	github.com/cloudvane/litefactory/internal/cli	27.927s
```

### 992-current-store2.log

```text
ok  	github.com/cloudvane/litefactory/internal/store	67.760s
```

### 992-current-instructions2.log

```text
ok  	github.com/cloudvane/litefactory/internal/instructionpackage	6.053s
```

### 992-current-daemon2.log

```text
ok  	github.com/cloudvane/litefactory/internal/daemon	103.126s
```

### 992-current-github2.log

```text
ok  	github.com/cloudvane/litefactory/internal/github	3.700s
```

### 992-current-prompts.log

```text
ok  	github.com/cloudvane/litefactory/internal/messaging/prompts	1.461s
```

### 992-current-lint5.log

```text
0 issues.
```

Result preflight was valid and complete. Submission input was accepted; matching receipt read for request `0beeac1ceeeea3894905abab9e79637d`. Engineering, acceptance and publication remained pending in the receipt.
