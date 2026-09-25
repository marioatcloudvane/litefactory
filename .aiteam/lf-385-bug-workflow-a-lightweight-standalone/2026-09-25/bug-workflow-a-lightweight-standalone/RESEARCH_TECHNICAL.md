# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The active Research and shaping markdowns and all four Plan role markdowns already carry the nine requested dedicated sections. Plan has source and embedded copies with parity tests and section-specific obligations. Its Output forbids approved, whereas Research's response vocabulary permits approved only with owner evidence; Bug must share structure and delivery rules while retaining its own role-specific responses, not copy Plan's response set.

Evidence:

- `internal/teamsource/research/research.md:7` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/teamsource/research/shaping.md:7` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `teams/source/plan/plan-story.md:7` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `teams/source/plan/plan-technical.md:7` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/teamsource/plan_role_guidance_test.go:102` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/teamsource/plan_role_guidance_test.go:153` @ `fb060b9c06ce923c47aa231b85975490200e2712`

## f2

Mode is a closed five-value vocabulary. Current issue admission resolves Mode labels, stored selections, explicit commands, managed-feature state and classification; there is no Bug branch in that admission switch. Existing mode persistence is not the requested durable standalone Bug workflow identity. Profile, action specification, control policy and WorkflowPolicy are all mode-typed, and activity registration requires a valid mode prefix. Bug therefore touches routing, policy admission, graph definitions and namespace validation, not just a new DOT file.

Evidence:

- `internal/agent/mode.go:8` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/daemon/mode.go:148` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/graphexec/graphexec.go:11` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/graphexec/registry.go:25` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/graphexec/registry.go:41` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/turn/workflow_policy.go:16` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/turn/workflow_registry.go:13` @ `fb060b9c06ce923c47aa231b85975490200e2712`

## f3

The shared result route is now a runtime boundary: only a retained accepted result supplies application data. Terminal JSON checks completion and consistency and cannot replace a missing submission. The closed shipped policy registry contains Research and Plan. Bug must register its own policy and extend the shared contract from introduction; existing registry tests reject terminal-only completion even with a nominal decoder. The current application payload union also enumerates modes rather than a Bug payload.

Evidence:

- `internal/turn/research_agent.go:216` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/turn/workflow_registry.go:13` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/agentresult/types.go:130` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/turn/workflow_admission_gap_test.go:20` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `.aiteam/APP_CONSTRAINTS.md:224` @ `fb060b9c06ce923c47aa231b85975490200e2712`

## f4

Graph attempt reservation already counts all dispatches of a node within an instance, refuses concurrent running attempts and enforces a durable limit. Completion atomically retains outputs and transition state. This supports a shared budget foundation, but the Bug repair reservation must distinguish a logical repair from retries/recovery and preserve its identity. Existing graph instances contain no parent-child call relationship; the inspected graph schema and lifecycle expose instances, attempts, outputs, transitions and waits, not the requested durable Bug test-call contract.

Evidence:

- `internal/store/graph_attempts.go:13` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/store/graph_attempts.go:42` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/store/graph_attempts.go:122` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/store/graph.go:55` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/store/migrations/0048_graph_execution_foundations.sql:13` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/store/graph_recovery.go:32` @ `fb060b9c06ce923c47aa231b85975490200e2712`

## f5

Durable owner waits already authenticate numeric owner/author identity and bind replies to task, question comment and delivery key. Existing tests reject non-owner and unrelated replies. Bug can reuse this mechanism, but exact analysis-revision authorization and tested PR head/target approval are additional Bug domain bindings; a generic valid reply alone does not prove either.

Evidence:

- `internal/store/graph_owner_waits.go:52` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/store/graph_owner_waits.go:60` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/store/graph_owner_reply_validation_test.go:10` @ `fb060b9c06ce923c47aa231b85975490200e2712`

## f6

The inspected configuration exposes settings, providers, teams, tiers, agents and permission profiles, but no declared regression/focused/full recipe contract. Focused searches of config and repository registration did not locate one. Treat recipe schema, execution environment, timeout/cancellation, evidence capture and source/recipe binding as new work pending shaping, rather than claiming the existing configuration already supplies Bug checks.

Evidence:

- `internal/config/config.go:48` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/config/config.go:85` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/config/config.go:385` @ `fb060b9c06ce923c47aa231b85975490200e2712`

## f7

Existing GitHub delivery methods can read/create PRs, merge with GitHub's atomic expected-head guard and close a numerically identified issue. Merge response is explicitly not integration proof: callers must read the PR again. Close may return after an update without a confirming reread. These calls are reusable, but Bug must own its approval checks, recovery bookkeeping, confirmed integration and confirmed closure.

Evidence:

- `internal/github/delivery.go:77` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/github/delivery.go:98` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/github/delivery.go:128` @ `fb060b9c06ce923c47aa231b85975490200e2712`
- `internal/github/delivery.go:152` @ `fb060b9c06ce923c47aa231b85975490200e2712`

## f8

Retrieved #367 BUG_SCOPE.md revision 5 through GitHub, blob 519b886b1e2d0aa45521511887cc17366422d1c2, on 2026-09-25. It supplies the prior agent duties, bug/execution record fields and illustrative DOT graphs. Those graphs omit explicit regression phase and some failure edges; PR feedback routes through analysis rather than expressing #385's two repair entry edges directly. Older prose also allows an explicit continuation to reset budgets, unlike #385's no-reset requirement. Use this reference as historical evidence, not an executable profile or final schema. Shaping must reconcile these differences with #385 and preserve visible failure handling. Reference: https://github.com/marioatcloudvane/litefactory/blob/lf/367-bug-workflow/.aiteam/lf-367-bug-workflow/2026-09-22/bug-workflow/BUG_SCOPE.md

_No source reference was recorded for this finding._
