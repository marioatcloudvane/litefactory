# #315 foundation integration — run 0014

Status: prerequisite integrated and scoped revalidation passed. No Research story completion or acceptance is claimed.

## Exact source and preconditions

Integrated the existing local commit `1fbd2c9716b6fea8d4c321a1ac713a6dbdbe4d5b` relative to common ancestor `7d5799834b5031c84286080708aaff6091ea63ee` by ordinary workspace file writes from local Git object bytes. HEAD remained `76ff748c83cbe8636c69982277093026621998bf`; the index remained clean. No merge, checkout, cherry-pick, commit, push, fetch, forge access, protected instruction-tree edits or daemon operations occurred.

Before writing, verified HEAD, common ancestor, no source divergence outside `.aiteam`, no local tracked source modifications, absence of all 27 added paths, and original HEAD bytes for the two modified manifests. Existing session notes and the prerequisite report were preserved. After writing and after checks, every one of the 29 files matched its pinned commit bytes exactly.

## Files integrated

- `go.mod`
- `go.sum`
- `internal/daemon/graph_backoff.go`
- `internal/daemon/graph_backoff_internal_test.go`
- `internal/daemon/graph_poll.go`
- `internal/daemon/graph_poll_internal_test.go`
- `internal/daemon/graph_structure_internal_test.go`
- `internal/graphexec/decide.go`
- `internal/graphexec/doc.go`
- `internal/graphexec/dot.go`
- `internal/graphexec/graphexec.go`
- `internal/graphexec/graphexec_test.go`
- `internal/graphexec/invoke_agent.go`
- `internal/graphexec/purity_test.go`
- `internal/graphexec/registry.go`
- `internal/graphexec/validate.go`
- `internal/store/graph.go`
- `internal/store/graph_attempts.go`
- `internal/store/graph_definitions.go`
- `internal/store/graph_instances.go`
- `internal/store/graph_outputs.go`
- `internal/store/graph_persistence_test.go`
- `internal/store/graph_transitions.go`
- `internal/store/migrations/0048_graph_execution_foundations.sql`
- `internal/turn/graph_activity.go`
- `internal/turn/graph_composition_test.go`
- `internal/turn/graph_node.go`
- `internal/turn/graph_node_test.go`
- `internal/turn/graph_structure_test.go`

## Executed checks

All listed commands exited 0 against the integrated worktree. These are scoped prerequisite checks, not the final repository aggregate gate.

1. `go test -race -count=1 ./internal/graphexec ./internal/store ./internal/turn ./internal/daemon -run 'Test(Graph|DOT|Registry|InvokeAgent|Decide|Validate|Pure)'`

```text
ok  github.com/cloudvane/litefactory/internal/graphexec 1.937s
ok  github.com/cloudvane/litefactory/internal/store 24.851s
ok  github.com/cloudvane/litefactory/internal/turn 41.519s
ok  github.com/cloudvane/litefactory/internal/daemon 4.082s
```

2. `go test -race -count=1 ./internal/graphexec -run 'Test(ParseProfile|BoundedCyclesAndDecide|OwnershipBoundaries)'`

```text
ok  github.com/cloudvane/litefactory/internal/graphexec 1.517s
```

3. `go test -race -count=1 ./internal/turn -run '^TestExecuteNodeActionHasNoProductionCaller$'`

```text
ok  github.com/cloudvane/litefactory/internal/turn 3.803s
```

4. `go build ./internal/graphexec ./internal/store ./internal/turn ./internal/daemon` — no output; exit 0.
5. `go vet ./internal/graphexec ./internal/store ./internal/turn ./internal/daemon` — no output; exit 0.
6. `gofmt -l internal/graphexec internal/store/graph*.go internal/turn/graph*.go internal/daemon/graph*.go` — no output; exit 0.

## Limits and handoff

This copies the existing foundation without redesigning or rebuilding it. Historical foundation artifacts were not imported. The original commit is not newly made a HEAD ancestor; factory persistence still owns the uncommitted worktree. Research production wiring remains for the frozen stories. No authentic host observation or acceptance review was performed. `make check` remains the orchestrator's end-of-run aggregate gate.

The referenced auxiliary `docs/story-contract-instruction-manifest.md` is absent, as already recorded in PREREQUISITE_REPORT.md; supplied contracts remain available.
