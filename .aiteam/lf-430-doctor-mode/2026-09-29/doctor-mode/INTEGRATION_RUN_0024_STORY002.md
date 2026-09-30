# Story-002 additional local boundary tests — run 0024

Frozen selection: story-002 / #433 / revision 3dd12b86539e838f358dc8e240afa920510f6cf3f3951c2c900c1112126dd916 / dependency story-001. Assigned T009 ordinary Go tests. This records engineering execution, not acceptance or review.

Only new test files were added; no production implementation was edited:

- `internal/github/doctor_page_io_test.go`: real loopback HTTP server and actual SDK client verify exact repository-comment endpoint, UTC since value, ascending updated order, explicit page/limit, and NextPage retention without automatically following the link. Oversized server pages fail. A 429 remains a typed quota error with one request and no client retry. Invalid page/limit values never reach HTTP.
- `internal/store/doctor_discovery_io_test.go`: real temporary SQLite file is closed and reopened after one command admission but before page advancement. The immutable command survives, edited replay preserves original instructions and identity, the incomplete scan retains its original fixed window, additional input is admitted, a stale page completion is fenced, and both queued commands execute once without replay duplication. No workflow session or live installation is used.

Executed command:

```
go test -race ./internal/github ./internal/store ./internal/daemon ./internal/doctor -run Doctor -count=1
```

Actual passing output:

```
ok github.com/cloudvane/litefactory/internal/github 1.456s
ok github.com/cloudvane/litefactory/internal/store 8.305s
ok github.com/cloudvane/litefactory/internal/daemon 4.094s
ok github.com/cloudvane/litefactory/internal/doctor 6.011s
```

Pinned golangci-lint v2.13.1 scoped to github/store: `0 issues.` Both new files were gofmt formatted. The command included the existing Doctor tests; it was not the full repository suite.

Fixture limits: HTTP responses are controlled local fixtures, not authentic GitHub observations. SQLite reopen simulates loss between durable operations; no live daemon was restarted. No paid/native provider, forge mutation or live runtime database was accessed. These tests do not establish authentic provider isolation, real stuck-case capture, external restart behavior or whole-story completion. Existing missing observations remain unverified. Aggregate checks and submission remain orchestrator-owned.
