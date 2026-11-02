# Execution extraction verification

Engineering evidence for story-001. Acceptance remains with Validate; no live-provider, deployment, approval or publication claim is made. Three subagents contributed in parallel: safety/Claude, graph/Codex, and example/usage. The lead reviewed their changes and owned the final package runs.

## Completed work

Neutral DOT declarations, action descriptions, structural checks, navigation, prepared execution, both native providers, process support and qualified usage collection live under `pkg/execution`. Factory adapters keep prompts, permission ceilings, captures, action bindings, result schemas, attempt attribution, receipts and durable ownership/recovery. The independent example owns its handlers and fake runner. No runtime state format, capture identity, migration or team release changed.

The graph compatibility fixture was generated from starting commit `6663b45a4421537ce921d5e99d1bfc07f70434d8` in an isolated `git archive` scratch directory before comparison. It contains all 31 shipped/source and retained legacy DOT profiles, checking unchanged factory JSON representation, byte identity, semantic digests and every declared navigation result. The scratch directory was deleted. Existing graph policy suites remain factory-owned.

## Full package runs

The commands below started together in background processes, each with a finite package timeout. No repository-wide test suite or `make check` was run. Complete outputs and command metadata are retained beside this document.

```sh
go test -race -count=1 -p=4 -timeout=30m github.com/cloudvane/litefactory/internal/agent github.com/cloudvane/litefactory/internal/agentresult github.com/cloudvane/litefactory/internal/codex github.com/cloudvane/litefactory/internal/doctor github.com/cloudvane/litefactory/internal/drift github.com/cloudvane/litefactory/internal/graphexec github.com/cloudvane/litefactory/internal/harness github.com/cloudvane/litefactory/internal/loganalysis github.com/cloudvane/litefactory/internal/logevidence github.com/cloudvane/litefactory/internal/messaging github.com/cloudvane/litefactory/internal/messaging/response github.com/cloudvane/litefactory/internal/meter github.com/cloudvane/litefactory/internal/meterview github.com/cloudvane/litefactory/internal/projectread github.com/cloudvane/litefactory/internal/providerprocess github.com/cloudvane/litefactory/internal/report github.com/cloudvane/litefactory/internal/rights github.com/cloudvane/litefactory/internal/submission github.com/cloudvane/litefactory/internal/taskdiscussion github.com/cloudvane/litefactory/internal/teamrelease github.com/cloudvane/litefactory/internal/teamsource github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest github.com/cloudvane/litefactory/pkg/execution/agent github.com/cloudvane/litefactory/pkg/execution/graph github.com/cloudvane/litefactory/pkg/execution/internal/process github.com/cloudvane/litefactory/pkg/execution/providers/claude github.com/cloudvane/litefactory/pkg/execution/providers/codex github.com/cloudvane/litefactory/examples/execution
```

Passed. The fixture helper package has no test files.

```sh
go test -count=1 -p=4 -timeout=35m github.com/cloudvane/litefactory/internal/cli github.com/cloudvane/litefactory/internal/daemon github.com/cloudvane/litefactory/internal/store github.com/cloudvane/litefactory/internal/turn
```

The CLI, daemon and store packages passed. The turn package completed in 811.054 seconds with the seven pre-existing failures below.

The public provider packages were run completely again after correcting native event-sink framing and preserving caller cancellation causes. The factory adapters use the unchanged lower execution seam; these final corrections affect only the new public Run API. The graph adapter package was run completely again after sorting compatibility fixture traversal. Both reruns passed:

```sh
go test -race -count=1 -timeout=5m ./pkg/execution/providers/claude ./pkg/execution/providers/codex
go test -race -count=1 -timeout=3m ./internal/graphexec
```

## Starting-commit failure comparison

Only the failing tests were rerun at starting commit `6663b45a4421537ce921d5e99d1bfc07f70434d8`, in a scratch directory outside this worktree. They failed at the same assertions with the same observable state/messages. This establishes that they are pre-existing; this extraction does not repair them under N-02. The scratch directory was deleted after inspection.

- `TestImplementReplyResumesAParkedStoryAfterAnAnsweredResolverQuestion`
- `TestResearchShapingWorkerCannotCreateOwnerApproval`
- `TestResearchContinuationRechecksGeneratedDocuments`
- `TestResearchSilentContinuationAndExhaustion`
- `TestResearchOwnerRoundsDoNotUseUpNodeLimits`
- `TestValidateImplementReplyAfterAgentWithMixedHistory`
- `TestValidateDispatchResumesIntoTheRepairAgent`

```sh
go test -count=1 -timeout=5m ./internal/turn -run '^(TestImplementReplyResumesAParkedStoryAfterAnAnsweredResolverQuestion|TestResearchShapingWorkerCannotCreateOwnerApproval|TestResearchContinuationRechecksGeneratedDocuments|TestResearchSilentContinuationAndExhaustion|TestResearchOwnerRoundsDoNotUseUpNodeLimits|TestValidateImplementReplyAfterAgentWithMixedHistory|TestValidateDispatchResumesIntoTheRepairAgent)$'
```

The failures concern parked-story resolver continuation, research approval state, changed-document continuation, silent shaping continuation, owner-round limits, mixed-history validation handoff and repair dispatch. The last failure reports a missing local model for the captured flagship tier. Exact current and starting-commit output is retained in `final-heavy.log` and `starting-commit-turn-failures.log`; not every failure is attributed to that model message.

## Formatting, lint, build and structural checks

- `go build ./...`: passed after the final production changes, then passed again after review. No build wrote into `dist`.
- `gofmt -l .`: no output, including the final review pass.
- `git diff --check`: passed.
- Documentation references and the new README link resolve.
- `make lint PKGS=./internal/agent ./internal/meter ./internal/providerprocess ./internal/graphexec ./internal/codex ./internal/harness ./internal/turn ./pkg/execution/... ./examples/execution`: passed with pinned golangci-lint v2.13.1, zero issues. A writable temporary lint cache was supplied.
- `make lint PKGS=./pkg/execution/providers/claude ./pkg/execution/providers/codex`: passed after the public API corrections, zero issues.
- Final scoped lint of the graph adapter and factory process wrappers passed after test-order/documentation edits, zero issues (`documentation-final-lint.log`).
- The example's sole main-path `TestExecutionExample`, both credential-free provider constructor checks, external-consumer compilation, and transitive production/test dependency closure passed in the full example package run. No factory internal package enters that closure.
- Call-site review found no direct native `exec.Command`, `cmd.Start`, `cmd.Run` or `cmd.Wait` execution body remaining in factory provider/process production code. Remaining wrappers validate factory contracts, translate factory data or coordinate durable ownership. Provider-native argument encoding and usage/source parsing moved with recognizable existing bodies.

## Coverage limits and pending acceptance

The existing macOS process-list guard skipped three explicitly audited assertions: `TestDescendantsWalksMoreThanOneLevel`, `TestRunTimeoutKillsTheWholeSubtree`, and `TestRunCancelKillsDescendantsOutsideCodexProcessGroup`. The focused verbose audit is retained in `process-coverage-limits.log`; package exit success is not proof of these skipped assertions. Other pre-existing explicit fixture skips remain unchanged, including the disabled exit-7 probe subcases. Direct owned-PID barrier, acknowledgment failure, bounded-output and cancellation-cause checks executed successfully.

No credential-dependent provider calls, production tests, live availability checks or deployed recovery checks were performed. No `TEST_BASE_URL` or external database was needed: fixtures use temporary local processes, repositories and SQLite files. Fake-process evidence cannot establish exactly-once execution, complete usage or cross-version native sandbox capabilities.

Repository-wide `make check` and the final acceptance decision belong to Validate under the supplied worker rules. Owner check pending: AC-002, AC-003, AC-004, AC-005, AC-006. No approved outcome was narrowed and no unanticipated design departure is recorded.

## Existing guidance violation retained for operator disposition

The published agent-guidance standard requires behavioral guidance in agent/skill markdown. Existing Go-side guidance was encountered and deliberately not rewritten in this extraction: `internal/harness/prompt.go:450`, `:457`, `:513`–`:515`; `internal/harness/drift.go:64`; `internal/codex/judge_arguments.go:19`; and the existing role description at `internal/codex/runner.go:425`. This is existing operator work, not a newly introduced instruction source. No markdown/skill wording pins were added.
