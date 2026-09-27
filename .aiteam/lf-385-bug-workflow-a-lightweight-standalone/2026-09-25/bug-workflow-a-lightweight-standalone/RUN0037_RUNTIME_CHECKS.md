# Run 0037 runtime wiring checks

These are scoped engineering observations, not acceptance, aggregate evidence,
live provider permission proof, or remote delivery. The final aggregate capture
must bind checks to the final files after all agents finish.

- `go test ./internal/config -run 'TestDefaultFile|TestBugRuntime' -count=1 -timeout=2m`: passed, config 0.390s. Existing default configuration and optional explicit Bug role/isolation configuration are covered.
- `go test ./internal/daemon -run 'TestOpenResolvesEverythingAndTakesTheLock|TestOpenSharedResultAuthorityIndependentOfNativeAutoStart|TestModesSelectCanonicalPermissionsForBothAgents|TestBugRuntime|TestWorkflowBoundaryBugCannotBorrowPermissions' -count=1 -timeout=3m`: passed, daemon 0.704s. Startup, independent shared-result authority and existing mode grants remain usable. Independent Bug grants reject widening and mutation of captured profiles.
- `go test ./internal/daemon -run 'TestBugRuntime|TestReportRecoverySQLiteTickPreservesOrderingAndOtherTaskCapacity' -count=1 -timeout=3m`: passed, daemon 0.641s. Generic pending-report recovery remains bounded for mode-managed work.
- `go test ./internal/turn -run '^TestBugRuntimeOwnerApprovalAndReplay$' -count=1 -timeout=2m`: final three-case run passed, turn 1.596s. Real local MCP result service, SQLite and git fixture with controlled runner: exact approval consumes no additional analysis pass; question answers persist unchanged; crash after review-change consumption recovers the inactive review; exact reply replay recovers one successor and changed reply text is refused.
- `go test -race ./internal/turn ./internal/daemon -run '^TestBugRuntime|^TestReportRecoverySQLiteTickPreservesOrderingAndOtherTaskCapacity$' -count=1 -timeout=3m`: passed, turn 3.617s and daemon 2.920s. This run preceded addition of the third review-change-crash test case.
- `go test -race ./internal/store -run '^TestBugParentRecovery' -count=1 -timeout=2m`: final run passed, store 2.140s. Factory recovery retains the same attempt and one dispatch; agent launch gaps and unknown nodes fail without relaunch or budget refund; restart report projection binding is idempotent and cannot be replaced.

The initial composed test fixture had an invalid zero comment identity, then its
question variant omitted the required displayed question batch. Both fixture
inputs were corrected before the passing results above. No invalid submission
was treated as accepted evidence.

Story-002 tests and unsupported host sandbox observations remain waived/skipped
per the run instruction. These parent-runtime tests do not claim actual hosted
provider restrictions or a Linux isolated check command on the macOS host.

A final extension of `TestBugParentRecovery` also exercises an agent attempt with
an actual retained run binding, not only the launch gap with no run ID. The same
race command passed again (store 2.376s); both cases remain failed with preserved
budget and cannot become resumable factory work.

PR clarification follow-up:
`go test -race ./internal/turn ./internal/store -run '^TestBugDeliveryPresentationOnlyExplicit|^TestBugDeliveryClarificationCreatesDistinct' -count=1 -timeout=2m`
passed (turn 1.676s, store 2.764s). Controlled forge and real SQLite demonstrate
that clarification creates a distinct review digest/publication key while keeping
the same child evidence, PR and repair usage. Exact requests for retained evidence
sections are referenced; arbitrary questions stay explicitly unanswered. Previous
presentation history remains retained. No remote publication or approval is claimed.
