# Story004 engineering implementation

Frozen identity: story-004, issue #460, revision e71737b29d0d434cb847513987ad95b9d487c025ae43147eac44fc9130ce1b30, dependency story-003. Engineering only; no acceptance/code/architecture review or live delivery was performed. No commit or push was made.

Implemented one concrete v2 factory host, NewGraphBugRuntime, with analyst/fixer/finalization shared callbacks and exact pinned-profile admission. The daemon registers this host; legacy runtime remains unregistered pending story005 deletion. Preparation fetches and resolves slug/worktree before durable dirty checkpoint and shared synchronization. It does not call the dirty-refusing synchronization path before preservation, does not force synchronization off, and settles process execution before mechanical conversation or finalization. Warm/cold contexts retain diagnosis, approval, guidance and preparation facts.

GraphBugFinalization consumes current settled successful accepted repair eligibility, independently observes source/main/validation inputs and contained stable logs, saves only tested content with SaveSource, confirms nonforce push, correlates bot/numeric/marker PR identity, expected-head merges, independently proves integration and confirms numeric issue closure. The shared graph_effects journal retains immutable operation bindings, claims and append-only observations. Lost acknowledgments are observed before repetition; definitive typed provider 403/422 refusal establishes nonexecution and permits a fresh guarded retry. Transport failures, 5xx, open PRs and absent uncertain objects do not establish nonexecution. Main races and contradictory merge integration remain parked without closure, repeated merge or repair. Completion reporting retains normal factory publication debt and restart retries reporting without delivery mutations.

Stale first parks finalization. Only its allocated owner continuation, after effect reconciliation, exposes DOT revalidate. Mechanical owner continuation allowance propagates through one immediate retained declared transition into one substantive agent invocation, atomically retaining a separate allowance_attempt_id; no counters reset or invented grants. Pre-dispatch exhausted agent work remains runless/source-less. Recovery of supported v2 runs occurs after orphan run execution facts settle; unsupported legacy definitions are refused without mutation.

Configured validation input observation hashes bounded actual executable bytes, resolved directory and selected environment without running any recipe. Missing executable prevents certification. Relevant ignored/external inputs remain subject to story003's strict source contract. Remote API cannot atomically lock main; independently observed merge parents/main ancestry are the post-dispatch boundary and contradictory integration withholds closure.

## Actual executed checks

Latest combined targeted command:

`go test -race ./internal/bugchecks ./internal/turn ./internal/git ./internal/store -run 'TestConfiguredCommandInputs|TestGraphDeliveryObserved|TestGraphMechanical|TestGraphBugConcreteHost|TestGraphEffectPersistent|TestSavedSource|TestCriticalSectionsAreSingleAcquisitions'`

Passed: bugchecks 1.584s; turn 11.068s; git 3.002s; store 3.049s.

The selected ordinary tests use persistent temporary SQLite stores and controlled forge boundaries for competing claims, lost PR/merge/closure acknowledgments, definite merge refusal/retry, main races, stale evidence, binary/environment changes, consumed allowance replay and real temporary repository checkpoint/synchronization/SaveSource recovery. The concrete host test executes read-only analyst shared submissions through first entry and owner continuation, preserving dirty work while advancing main. Full aggregate checks remain orchestrator-owned.

`go vet ./internal/turn ./internal/store ./internal/git ./internal/bugchecks ./internal/daemon` passed with no output.

Scoped gofmt -l across all files modified by this worker passed with no output.

## Changed paths / stable APIs

New: internal/store/migrations/0095_graph_effects.sql; internal/store/graph_effects.go; internal/store/graph_effects_test.go; internal/turn/graph_delivery.go; internal/turn/graph_delivery_reports.go; internal/turn/graph_delivery_test.go; internal/turn/bug_graph_runtime.go; internal/turn/bug_graph_runtime_test.go; internal/git/source_save_test.go; internal/bugchecks/input_observation_test.go.

Modified shared seams: internal/git/source_checkpoint.go (SaveSource); internal/bugchecks/executor.go (Isolation.ObserveInputs); internal/turn/bug_runtime.go (Simplified dispatch); internal/turn/execute.go, prepare.go, research_workflow.go (factory-bound preparation/deferred save); internal/daemon/setup.go and daemon.go (v2 activation/recovery ordering); internal/turn/gate_test.go (new critical gate methods); internal/store/graph.go, graph_attempts.go, graph_owner_waits.go, graph_stops.go and internal/turn/graph_node.go (atomic substantive allowance); .aiteam/AC_AMENDMENTS.md (frozen narrow CONC11 exception).

Store APIs: RetainGraphEffect, GetGraphEffect, ClaimGraphEffect, ObserveGraphEffect, ListGraphEffects, ListPinnedGraphInstances. Host APIs: NewGraphBugRuntime, BugRuntime.Simplified. Shared reporting: ReportGraphDeliveryCompletion. Source save: Worktree.SaveSource. Tool observation: Isolation.ObserveInputs. Finalization callbacks: Resolve, Observe, Save, PublishSaved, LogRoot and DeliveryOperations. No story checkpoint or application submission was made by this worker.
