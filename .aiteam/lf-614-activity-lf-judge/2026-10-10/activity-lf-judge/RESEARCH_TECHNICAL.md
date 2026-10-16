# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Command parsing recognizes work modes, plan/validate controls, schedule/confirm and separately doctor. /judge currently follows the unknown-command branch; ordinary work commands require nonempty text and map to a mode. A judge command is not automatically an existing workflow mode.

Evidence:

- `internal/turn/command.go:218`
- `internal/doctor/command.go:16`

## f2

Default provider is the first configured agent in YAML order. Coding Agent labels select registered providers. Tier resolution is centralized; current repository defaults map Claude flagship to opus and Codex flagship to gpt-6-astra. These are repository defaults, not a claim about live vendor catalogs.

Evidence:

- `internal/config/config.go:68`
- `internal/config/tier.go:126`
- `internal/config/tier.go:150`
- `internal/daemon/agent.go:79`

## f3

Graph action names are closed: generic lf_agent/lf_if/lf_loop/lf_comment/lf_ask or a workflow-prefixed capability. Plain lf_judge is not admitted by current naming rules. Runtime handlers and graph action specs are separately registered. Both admission and dispatch need coverage.

Evidence:

- `internal/graphexec/registry.go:47`
- `internal/turn/graph_activity.go:42`

## f4

Existing AgentActivity reuses captured role delivery, runner registry, result authority, budgets and metering, but checks that invocation provider equals recorded run provider and session equals the recorded segment. Switching provider inside that activity cannot be achieved by changing only a model setting.

Evidence:

- `internal/turn/research_agent.go:44`
- `internal/turn/research_agent.go:147`
- `internal/turn/research_agent.go:172`

## f5

Workflow delivery resolves each node's tier against the selected provider, renders native resources and incorporates provider/model into delivery identity. Both adapters use Delivery.MainModel when preparing the invocation. Literal model override must integrate with these captured delivery checks.

Evidence:

- `internal/turn/research_delivery.go:39`
- `internal/codex/runner.go:517`
- `internal/harness/runner.go:686`

## f6

Shared agent.Request has no thinking/effort setting. Current tier implementation explicitly notes that reasoning levels are not passed through. Inspected launch construction supplies model but does not provide a judge effort option. Usage reasoning-token fields are accounting, not controls.

Evidence:

- `internal/agent/request.go:53`
- `internal/config/tier.go:148`
- `internal/codex/runner.go:492`
- `internal/harness/runner.go:686`

## f7

Research currently has clarification and shaping stages with explicit source-bound control/publication nodes and owner approval for shaping. Retained structured results generate brief and technical documents. Source validation presently requires lf_source to refer to lf_agent, which a distinct judge output must account for if it participates in existing controls.

Evidence:

- `internal/teamsource/research/research.dot:18`
- `internal/graphexec/research_control.go:128`
- `internal/turn/research_briefs.go:23`

## f8

Graph execution reserves durable attempts before dispatch, enforces limits, persists outcome/transition, and separates private invocation from publication. These are relevant recovery and failure boundaries for an optional judge. Doctor supplies a separate slash-envelope precedent without introducing a workflow mode.

Evidence:

- `internal/turn/graph_node.go:42`
- `internal/turn/graph_node.go:129`
- `internal/turn/research_agent.go:99`
- `internal/doctor/provider.go:10`
