# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

Agent selection is label-driven. resolveAgent reads a single 'Coding Agent: X' label (case-checked, ambiguity and unknown values are refused). No label means no decision.

Evidence:

- `internal/daemon/agent.go:96`

## F-02

When no label is present the tick falls back to d.defaultAgent. This is the line that sends every unlabelled Bug to Claude. The same fallback exists for continuations.

Evidence:

- `internal/daemon/tick.go:2831`
- `internal/daemon/continuation_dispatch.go:166`

## F-03

defaultAgent is the first agent stanza named in config.yaml (AgentOrder()[0]). The template says there is deliberately no separate default_agent setting, so there is no Bug-specific agent setting today.

Evidence:

- `internal/config/config.go:83`
- `internal/config/template.go:153`

## F-04

The tick passes the selected agent into the run request for every workflow including Bug (Agent: agent.Kind(selectedAgent)). Bug permission profile is resolved per role and agent at dispatch, with explicit Codex stanzas (analyst read-only, fixer workspace-write). So a Codex label on a Bug is carried to the runtime by existing code.

Evidence:

- `internal/daemon/tick.go:2947`
- `internal/daemon/bug_permissions.go:15`
- `internal/turn/bug_graph_runtime.go:462`

## F-05

Story children inherit the parent's Coding Agent label (written to the child issue, best-effort). The helper only runs for story issues, so it is the existing pattern to copy for Bugs, but does nothing for Bugs now.

Evidence:

- `internal/daemon/agent_inherit.go:20`

## F-06

The judge picks its provider from the last run segment's agent, falling back to defaultAgent. For a Bug run on Codex the judge follows Codex only if a segment exists. Delivery conflict advisor is chosen from run.Agent, also following the run.

Evidence:

- `internal/daemon/judge.go:66`
- `internal/turn/bug_graph_runtime.go:743`

## F-07

Log analysis files Bugs through the tracker's FileBug, which creates the issue and adds only the 'bug' label; no Coding Agent label is set, so these Bugs always fall to the first agent. Linear filing is not supported at all.

Evidence:

- `internal/loganalysis/trackers.go:89`
- `internal/loganalysis/trackers.go:135`

## F-08

Run reports carry an Agent field that falls back to 'claude' when empty. The report Run type has no model field in the code read, so showing the model needs a new field and the model value to be carried to it.

Evidence:

- `internal/report/run.go:19`

## F-09

Impact (source inspection retained): touched surfaces are daemon selection and continuation, parent resolution, optional config schema/template, shared run-report metadata and daemon/turn/report tests; dependents include story inheritance, judge provider and Bug delivery. Bug first-agent fallback is superseded only when a parent agent or Bug setting applies; stories and role grants remain unchanged. No data migration was found in the inspected selection/config/report surfaces; historical model data may be unavailable. The earlier blanket unchanged-install promise is superseded by K-01. No team-role or skill changes are required by this scope.

Evidence:

- `internal/daemon/tick.go:2819`
- `internal/daemon/agent_inherit.go:45`
- `internal/config/config.go:83`
- `internal/report/run.go:19`
