# US-003 engineering report

Identity: #268 / issue ID 5440230911. Frozen revision
`779ceb3f23625af5e1b71f0f8a51d2efe97b8b854b1df037cbafa1610687c63e`.
US-001 and US-002 were supplied as engineering-complete dependencies. Their
implementation files and previously retained evidence were not modified.

T-001/T-002 deliver additive application-result binding/history/request/observation
persistence and an unwired direct turn service. T-003's explicitly authorized real
SQLite test phase follows production work in this same worker because the factory
cannot allocate another agent thread. Acceptance/review was not performed.

## Result

A real run/task/mode binding continues one context across fresh attempts. Exact
frozen key/revision sets participate in context identity; mutable Plan drafts do
not. New task authority revokes old writers, retaining historical reads. Atomic
compare-and-record appends full snapshot/retirement bytes, immutable request/hash
and received/input observations, with source run/attempt attribution. Exact retry
recovers its original applied revision after later work, revocation and reopen.
Changed-byte correlation rejects. Partial writes roll back on transaction failure.

Receipt dimensions remain independent and append-only. Application observations
have no workflow mutation or publication authority. The current v2 receipt/hash
helpers are reused without adopting v2 queues. Optional association references an
existing same-run/same-task v2 receipt and cannot modify it.

The direct service re-resolves trusted scope/evidence for each preflight and submit,
composes Create/ApplyUpdate/Project, stamps current factory metadata, and performs
no routing, proof capture, runner dispatch, readiness change, closure or publishing.
An explicit reader reconnect API preserves history access without new-write or
observation authority. Preflight allocates no request/proof/revision/observation.
Invalid input can retain a rejected receipt without an applied revision; stale
revision and bad authority retain no new request or candidate.

## Changed paths

- `internal/store/migrations/0040_application_results.sql`
- `internal/store/result_bindings.go`
- `internal/store/result_history.go`
- `internal/store/result_observations.go`
- `internal/store/result_history_test.go`
- `internal/turn/agent_results.go`
- `internal/turn/agent_results_test.go`
- `docs/agent-result-contract.md`

## Verification

`go test -race -cover ./internal/store -run 'TestResult|TestSubmission'` passed
(16.2% package coverage; targeted new plus existing v2 persistence regressions).
`go test -race -cover ./internal/turn -run 'TestAgentResult|TestSubmission'` passed
(19.8% package coverage; direct service plus existing v2 live-broker fixture paths).
Outputs: `US-003-store-tests.log`, `US-003-turn-tests.log`.

`US-003-final-tests.log` retains the final new-story race run after strengthening
byte-equality/read-only reconnect assertions. Pinned scoped v2.13.1 lint output is
`US-003-lint.log`. Formatting over changed Go files emitted no paths.

These real temporary SQLite and local broker fixtures prove offline API/storage
behavior, not production adapter adoption or actual model/provider provenance.
No external database/container/production state was needed. No tags, daemon build,
commit, submission, aggregate gate or acceptance/review was performed here.
