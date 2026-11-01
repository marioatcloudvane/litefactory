# Claude cutover evidence

The factory adapter now delegates native ordinary argv construction, event stream decoding and pipe lifecycle to `pkg/execution/providers/claude`. Its remaining wrapper attaches factory usage attribution, quota decisions, result classification, receipt/log identity and resume decisions. Native stream tests moved alongside the decoder; factory fixture helpers remain for integration tests.

Classification, scheduling and conflict advice delegate stateless native argv and buffered lifecycle. Chat and conversation delegate native argv; scoped doctor/judge/repair and drift/log advisers use extracted native argument builders and bounded transport. Conversation retains factory lock acquisition, PID journal persistence and cleanup, delegating its barrier/process mechanics through the public coordination seam. Thinking-display capability probing also moved to the provider. No direct native command start/wait/run body remains in the harness.

Public construction takes an explicit executable, environment and native typed model/tool options. Omitted environment becomes an explicit empty environment. Tool grant presence distinguishes nil (omitted flag) from non-nil empty (explicit empty flag). The factory adapter continues supplying an explicit tool flag and its resolved ceilings. Public prepared runs use the request output policy and return exact native session acknowledgment, raw events/usage, started/terminal facts and wrapped causes. Failed session acknowledgment withholds terminal text. Authorization failure is never treated as a recoverable binary update, and an eligible launch retry checks authorization again.

## Focused checks executed

- `go test -race -timeout=2m ./internal/harness -run 'TestEventStream|TestTailWriter'`: initial compilation found the extracted result event's Errors field was accidentally unexported; corrected before successful subsequent checks.
- `go test -race -timeout=3m ./internal/harness -run 'TestEventStream|TestTailWriter|TestRunner|TestClaudeDelivery|TestClaudeVerified|TestWorkflowMainRole|TestBugAnalyst|TestBugFixer|TestRecordedClaude|TestPropose|TestClassif'`: passed (33.488s).
- `go test -race -timeout=2m ./pkg/execution/providers/claude -run 'TestEventStream|TestTailWriter|TestScanner'`: passed (1.527s), after preserving the existing truncated-tail diagnostic marker.
- `go test -race -timeout=3m ./internal/harness -run 'TestChat|TestRecordedClaude|TestDrift|TestDoctor|TestJudge|TestConversationArguments|TestThinking'`: passed (41.868s).
- `go test -race -timeout=3m ./internal/harness -run 'TestPropose|TestClassif|TestConflictAdvisor'`: passed (13.993s).
- `go test -race -timeout=2m ./internal/harness ./pkg/execution/providers/claude -run 'TestPrepared|TestArguments|TestResume|TestEventStream'`: passed.
- `go test -race -timeout=3m ./pkg/execution/providers/claude -run 'TestPrepared|TestArguments|TestExecute'`: passed (2.864s). New cases exercise native evidence/session acknowledgment, refused launch/session, permission presence, output limits, empty environment and retry authorization.
- `go test -race -timeout=3m ./internal/harness -run 'TestRunner|TestClaudeDelivery|TestClaudeVerified|TestWorkflowMainRole|TestRecordedClaude|TestChat|TestDrift|TestDoctor|TestJudge|TestThinking|TestPropose|TestClassif|TestConflictAdvisor'`: passed (51.029s).
- `GOLANGCI_LINT_CACHE=/tmp/lf1295-claude-lint golangci-lint run ./internal/harness/... ./pkg/execution/providers/claude/...`: passed with zero issues before the final minor drain-error-priority/default-logger/explicit-empty-tool changes; root final lint owns the final tree.
- Changed files formatted with `gofmt`.

Root owns complete package/importer runs and final lint. These focused results do not establish live provider availability or acceptance. Native usage collector relocation is owned by another subagent and is not represented as completed by this record.

## Review

Read-only comparison of the extracted private process support against the starting implementation found unchanged missing-executable retry/backoff, capability probe bounds, environment allowlist, descendant cancellation and owned-process barrier semantics. The factory still persists and syncs the exact PID before releasing the barrier and preserves unverified cleanup fencing. The generic output policy defaults retain the original scoped budgets; factory judge calls retain their existing oversized-line discard behavior.

## Final follow-up

- Removed the pure harness subtree cancellation wrapper and its duplicate impossible-PID test; private process coverage retains that case. Factory fixture process-list preflight remains.
- Migrated harness environment, probe sentinel, failure/oversize observer and provider-exit references directly to public execution contracts. Its only remaining production dependency on internal/providerprocess is durable conversation directory/launch coordination.
- Wired the relocated native usage collector into public Run, including selected transcript source, acknowledged session before observation and result coverage. Raw usage evidence retains complete native events, including assistant message counters and session/model source fields.
- `go test -race -timeout=3m ./internal/harness ./pkg/execution/providers/claude -run 'TestPrepared|TestArguments|TestExecute|TestRecordedClaudeProbe|TestThinking'`: passed (19.872s and 3.702s).
- `go test -race -timeout=3m ./pkg/execution/providers/claude -run 'TestPrepared|TestArguments|TestExecute'`: passed (3.191s), including qualified usage wiring with measured zero versus unknown counters.
- Preserved deterministic stdout-before-stderr error selection after both drain goroutines join. The factory still prioritizes its stream persistence error.
