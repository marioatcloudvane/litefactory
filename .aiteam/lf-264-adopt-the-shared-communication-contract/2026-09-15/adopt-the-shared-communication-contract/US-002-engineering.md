# US-002 implementation handoff

Frozen identity: #299 / issue_id 5463751769 / parent_id 5439211198 / revision `02ab327e5ff1f1b431f024ee7dba52f6b1a55e40879e55a2164b369ceb3a3d6f`; dependencies: US-001 (engineering satisfied). Source: https://github.com/marioatcloudvane/litefactory/issues/299. Workflow 2, Implement only.

Status: implementation ready for specialist verification. No engineering-complete submission, acceptance review, issue action, commit or push was performed. Tests remain owned by the specialists.

## Implemented

- Every shared launch gets an explicit empty baseline or complete authorized field projection with stable result/item IDs, observed revision and retired IDs. The full approved Implement result scope survives engineering checkpoint selection becoming smaller, including aggregate-only recovery when no writable stories remain.
- Launches also receive retained story snapshot/check references and a bounded recent result request inventory. Narrow readers receive only granted projections; receipts cannot bypass those grants by exposing raw payloads or full snapshots. Exact byte-identical receipt replay remains read-only after writer revocation.
- Result receipt lookup recovers a unique known request across earlier runs of the same result context; ambiguous reused IDs refuse. Factory composition installs bounded earlier family receipt scopes for the exact result before catalog freezing. These scopes grant no mutation authority.
- Invalid/stale updates return their authorized current projection for correction. Atomic store revision races produce the same actionable stale-revision diagnostics rather than an opaque storage error. Existing full-merge validation remains the owner of untouched fields and retired IDs.
- Fresh Implement context loads exact admitted-generation/frozen-revision checkpoints, validates event identity and digest, and resolves immutable submission snapshot/check records. Event-only historical records retain existing digest/test-name references. Nested context collections are copied. The existing aggregate resolver is installed before pure validation and completion is rechecked through the existing completion callback in preflight, submit and final enforcement. Missing resolver/evidence never disables RequireAggregate.
- Added the necessary production adapter for the already-existing aggregate capture and reference types. The adapter validates complete current worktree bytes and exact selected revisions, stores aggregate facts and their application receipt in one transaction, and creates no story event, result revision, acceptance decision or remote effect. Replays recover the existing receipt before validation; capture alone never satisfies final output.

## Explicit implementation assumption

The orchestrator authorized the minimal aggregate adapter as necessary AC-005 production wiring. It accepts only this routing shape through the existing shared result preflight/submit/receipt helper or permitted communication operations:

```json
{"engineering":{"snapshot_id":"aggregate-reference","check_ids":["aggregate-check"]},"capture":{"implementation_digest":"manifest SHA256","files":["existing complete ContributionCaptureFile objects"],"checks":[{"id":"aggregate-check","execution_id":"unique aggregate execution","scope":"aggregate","name":"actual check","executed":true,"passed":true,"evidence":"actual output","implementation_digest":"same SHA256"}]}}
```

The files entry above is illustrative; actual files must use the existing typed complete snapshot-file objects. No caller supplies mode, run, parent, generation, story or frozen revisions. `AggregateCapture` retains factual false/failed checks, but final completion rejects them. Scoped story check IDs/execution identities cannot be reused as aggregate proof. After capture, the ordinary application result references it in `payload.implement.aggregate_checks`.

## Production files changed for US-002

New:
- `internal/turn/agent_result_baseline.go`
- `internal/turn/agent_result_context.go`
- `internal/turn/agent_result_capture.go`

Modified:
- `internal/agentresult/update.go`
- `internal/store/result_aggregates.go`
- `internal/store/result_bindings.go`
- `internal/store/result_history.go`
- `internal/submission/tools.go`
- `internal/turn/agent_result_aggregates.go`
- `internal/turn/agent_results.go`
- `internal/turn/mcp_guidance.go`
- `internal/turn/mcp_runtime.go`
- `internal/turn/mcp_tools.go`
- `internal/turn/result_finalization.go` (created by US-001)
- `internal/turn/submissions.go`

US-001 changes in these shared owners were preserved. No test file was edited by this engineer.

## Executed scoped checks

Implementation is uncommitted on base `16634ed33075ea3acfc449bcb6316163bdc0b807`, following US-001 changes. Specialist/aggregate evidence must bind the final state rather than asserting this base alone contains the change.

- `go test ./internal/turn ./internal/store ./internal/submission -run 'TestResultAggregate|TestAgentResult|TestMCPApplicationGuidancePayloadContract|TestMCPRuntime|TestSubmissionLiveCorrectionAndFactoryEnvelope|TestSubmissionRecoveryFromFreshStoreConnection|TestToolProfile' -count=1`: PASS; turn 12.364s, store 2.384s, submission 1.295s.
- Earlier `go test ./internal/turn ./internal/store ./internal/agentresult -run 'TestAgentResult|TestResultBinding_AllModesAndContextContinuation|TestMCPRuntimeUnmanagedPreparation|TestMCPGuidance|TestUpdate' -count=1`: PASS; turn 3.088s, store 1.895s, agentresult 1.277s. Later changes require the new specialist cases below.
- `go vet ./internal/turn ./internal/store ./internal/submission ./internal/agentresult`: PASS, no output.
- `git diff --check` and scoped `gofmt -l` for all US-002 files: PASS, no output.

An initial existing read-only replay regression was found and corrected: historical readers may replay exact already-recorded bytes, while new submissions still fail writer authority. Passing scoped reruns include that fix.

No whole-package/repository suite, make check, dist build, live provider invocation, daemon mutation or forge action was performed.

## Required specialist verification

1. Production cold/resumed/fallback launch baselines include every authorized field and stable IDs/revision, including Plan nested content and Implement retained proof. Empty baseline is explicit. All-engineered recovery creates no writable story scope and still receives shared finalization.
2. Narrow historical readers cannot get sibling content through receipt payload, receipt diagnostics baseline, snapshot, read, retired IDs or replay. Historical grants cannot restore writer authority. Unique known receipt IDs recover across restart; ambiguous same IDs across runs refuse.
3. Sparse updates revalidate untouched stale evidence; stale revision and concurrent CAS rejection return authorized correction projections atomically, with no revision/retirement write.
4. Production aggregate capture succeeds through the real shared broker/service; preflight writes nothing; submit atomically retains capture+receipt without changing result revision, events or acceptance. Wrong mode/identity/unknown/null/duplicate fields, bad file hash, stale/missing/worktree bytes, incomplete check coverage and scoped-check relabeling refuse.
5. Lost-response/reopen replay returns exact durable capture receipt without duplicate evidence/effects. Capture-only finalization stays incomplete. Failed/unexecuted captures retain facts but cannot pass completion.
6. Retain live engineering checkpoints, then submit aggregate-only application completion through production preflight, submit and final enforcement. Verify fresh checkpoint/submission references, current-generation scope, old event-only proof, missing/invalidated/conflicted/misattributed proof and post-submit worktree changes.
7. Independent receipt dimensions remain unchanged by capture, read and advisory reporting. Validate capture remains refused and its restricted terminal path stays intact.

Authentic Claude/Codex host observations on #274/#296 remain external and unproven by these offline checks.
