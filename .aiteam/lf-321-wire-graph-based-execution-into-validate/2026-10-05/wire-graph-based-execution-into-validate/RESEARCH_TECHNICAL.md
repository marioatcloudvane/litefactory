# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Research ships investigation then shaping with private retained results, generated documents, bounded loops and durable owner waits. Plan ships intent/technical/acceptance shaping; graph completion does not select another mode. The closed policy registry contains Research, Plan, Bug and Spike, but no Implement or Validate policy. Some documentation is stale: research-graph.md still says no production Plan policy, contrary to the registry.

Evidence:

- `internal/teamsource/research/research.dot:16` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `docs/workflow/plan-graph.md:3` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/turn/workflow_registry.go:13` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `docs/workflow/research-graph.md:9` @ `680c4f2ba575a18b2f704a90f8b777079470724f`

## f2

Implement currently owns environment setup, tests, fixes and aggregate/save gates. Separated workflow keeps engineering completion independent from acceptance. Its orchestrator still requests repository format/vet/tests and bounded repair loops. Moving the full suite is prospective, not current behavior.

Evidence:

- `teams/source/skills/workflow/SKILL.md:90` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `teams/source/agents/shared/implement-orchestrator.md:22` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/turn/agent_result_aggregates.go:239` @ `680c4f2ba575a18b2f704a90f8b777079470724f`

## f3

Validate's reviewer reads retained evidence and surrounding code and returns criterion/code/architecture/plan assessments. It may not edit, run shell/build/test commands, or invoke implementation specialists. Permission policy enforces Claude's read-only tool set and Codex's read-only sandbox. A new factory check activity must remain outside reviewer authority.

Evidence:

- `teams/source/agents/go-team/go-code-reviewer.md:12` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/config/policy.go:426` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/config/policy.go:507` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `teams/source/skills/workflow/SKILL.md:95` @ `680c4f2ba575a18b2f704a90f8b777079470724f`

## f4

Bug checks offer a versioned repository recipe at .litefactory/bug-checks.yaml, with bounded argv, cwd, named environment inputs, timeouts and regression/focused/full phases. The local executor supplies isolation, bounded output and process lifecycle recovery. Commands are language neutral, but optional named failure reruns currently support only the go-test parser. These are potential reuse surfaces, not an admitted Validate recipe.

Evidence:

- `internal/config/bug_checks.go:12` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/config/bug_checks.go:19` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/config/bug_checks.go:69` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/bugchecks/executor.go:325` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/bugchecks/recovery.go:63` @ `680c4f2ba575a18b2f704a90f8b777079470724f`

## f5

Scheduling already supports relative delay, absolute local/UTC times, dependency delays and reviewed schedule proposals. Due schedules run through normal dispatch and share capacity with ordinary turns. The inspected grammar and pool do not express recurring daily windows, weighted mode slots or exclusive Validate admission. One worker slot does not bound child test-process parallelism.

Evidence:

- `internal/schedule/schedule.go:47` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/daemon/schedules.go:17` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/daemon/schedules.go:68` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/daemon/pool.go:45` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/daemon/pool.go:107` @ `680c4f2ba575a18b2f704a90f8b777079470724f`

## f6

Aggregate evidence already binds retained checks to immutable file digests, selected frozen revisions, context, run and attempt; final worktree mismatches are rejected. This is an engineering gate, not acceptance. Validate full-suite evidence must have its own supported attribution and retention policy rather than be asserted from an agent exit or generic receipt.

Evidence:

- `internal/turn/agent_result_aggregates.go:156` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/turn/agent_result_aggregates.go:199` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/turn/agent_result_aggregates.go:239` @ `680c4f2ba575a18b2f704a90f8b777079470724f`

## f7

Acceptance enumerates frozen criterion identities, retains snapshot-bound decisions, reconciles comments, preserves unrelated edits, confirms projected checkboxes and only then closes accepted stories. Feature delivery independently requires exact proposal/PR/head/main approval, verifies readiness and actual integration, then confirms parent closure.

Evidence:

- `internal/turn/acceptance.go:27` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/turn/acceptance.go:119` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/turn/acceptance.go:237` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/turn/feature_delivery.go:46` @ `680c4f2ba575a18b2f704a90f8b777079470724f`
- `internal/turn/feature_delivery.go:631` @ `680c4f2ba575a18b2f704a90f8b777079470724f`

## f8

Historical observation on 2026-10-05: 31 Validate metadata files were found in the available repository run-log tree; four recent Claude runs were inspected, not all runs. #473 run 18 (2026-10-04, daemon version 04f3889) submitted payload.validate.contributions; line 48 records input rejection for an unknown member, and the final agent response fell back to terminal JSON. Daemon lines 1301/1303 mark the turn failed despite provider success. Current Validate schema expects payload.validate.stories. This demonstrates a result-contract mismatch; it does not prove this was every historical failure. Local log sources (outside repository reference root): /Users/mariomh/.litefactory/logs/runs/github/marioatcloudvane/litefactory/473/0018.jsonl:48;/Users/mariomh/.litefactory/logs/daemon.log.20261005-144201:1301.

Evidence:

- `internal/agentresult/validate.go:232` @ `680c4f2ba575a18b2f704a90f8b777079470724f`

## f9

#406 runs 39/41 (2026-09-30, daemon version 49a84c0) exited successfully at provider level but were marked failed by the factory. Their final responses contain terminal contributions and unresolved authentic/adoption evidence. Run 41 explicitly says it did not open Go sources and that a later clean full-suite run was not part of the reviewed snapshot. #412 run 21 explicitly left code/architecture/plan reviews unverified because it performed no independent code review. These are observed reporting and evidence gaps; exact factory rejection causes for those runs were not established. Local log sources (outside repository reference root): /Users/mariomh/.litefactory/logs/runs/github/marioatcloudvane/litefactory/406/0041.jsonl:18;/Users/mariomh/.litefactory/logs/runs/github/marioatcloudvane/litefactory/412/0021.jsonl:22;/Users/mariomh/.litefactory/logs/daemon.log.20261001-064016:287471.

_No source reference was recorded for this finding._
