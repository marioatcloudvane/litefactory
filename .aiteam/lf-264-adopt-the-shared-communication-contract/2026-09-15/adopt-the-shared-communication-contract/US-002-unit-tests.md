# US-002 unit execution evidence

Identity: #299, issue_id 5463751769, parent_id 5439211198, frozen revision `02ab327e5ff1f1b431f024ee7dba52f6b1a55e40879e55a2164b369ceb3a3d6f`, dependencies [US-001], workflow 2. Requirements consumed from supplied frozen inline body.

Implementation base is `16634ed33075ea3acfc449bcb6316163bdc0b807` plus current uncommitted US-001/US-002 changes. These results describe local execution on 2026-09-15, not tests of the unchanged base. Orchestrator must bind the final immutable capture before submission.

## Added files and outcomes

- `internal/agentresult/resume_evidence_test.go`: AC-003/005. Two correction cases (stale revision, malformed field) receive actionable diagnostics/current revision, use the explicitly authorized projection route with no sibling disclosure, and successfully correct against that revision while preserving original content. Seven sparse-update cases revalidate untouched story and aggregate evidence: valid, unretained snapshot, missing check, wrong revision, failed check, missing aggregate resolver, stale aggregate. Rejection returns no candidate and does not change retained history.
- `internal/turn/agent_result_context_unit_test.go`: AC-005 context isolation. Nested criteria, required-execution IDs, check maps, completion flags and Plan scope slices are independent copies, with RequireAggregate preserved. Six aggregate adapter structural cases cover typed references, attempted factory identity, duplicate fields, unknown fields, wrong types and trailing input.

Four test functions; 16 leaf cases passed, zero failed in final run. All dependencies are in memory. Aggregate resolver is a controlled fake; no filesystem, database, HTTP or provider operation runs inside these tests.

## Exact final command and output

```text
go test ./internal/agentresult ./internal/turn -run 'TestUpdate_CorrectionBaselineIsScopedAndReusable|TestUpdate_UntouchedCompletionEvidenceRevalidated|TestCloneResultContext_NestedScopeIndependence|TestAggregateResultInput_RejectsNonAuthoredShape' -short -race -cover -count=1 -timeout=8m
ok  github.com/cloudvane/litefactory/internal/agentresult 1.450s coverage: 51.4% of statements
ok  github.com/cloudvane/litefactory/internal/turn 1.830s coverage: 0.3% of statements
```

Scoped `gofmt -l` on both added files and `git diff --check --` both paths produced no output. No aggregate suite or make check executed by this specialist.

## Corrected test assumptions and remaining verification

Initial test construction assumed the pure ApplyUpdate function injected a baseline and the low-level JSON decoder rejected null aggregate objects. That run failed two correction cases and two null-decoding cases. The tests were corrected to their actual layer: ApplyUpdate returns the current revision plus an authorized Project read route; service baseline injection needs real-store verification. Null semantic rejection belongs to aggregate reference/capture validation and the real adapter path, not decoding alone. These were test-boundary corrections; no production source changed.

The real-I/O specialist must cover production baseline injection, CAS races, durable receipts/replay, scope isolation through the service, null aggregate semantics, current-worktree comparison, immutable checkpoint resolution and all three completion entrypoints. This unit evidence does not prove those I/O paths. Existing aggregate capture tests cover pure reference and file/hash claim validity; they were not rerun here.

No acceptance review, factory submission, forge action, commit, dist build, protected-tree edit or authentic host observation. #274/#296 observations remain external.
