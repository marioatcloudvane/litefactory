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

Log analysis builds its own Claude runner only when the default agent is Claude. Its filed Bugs' labels were not found in the code read; the creation path (and whether it can set labels) is unsettled.

Evidence:

- `internal/daemon/log_analysis_runtime.go:49`

## F-08

Run reports carry an Agent field that falls back to 'claude' when empty. Whether the Bug report renders agent and model was not verified in this pass.

Evidence:

- `internal/report/run.go:19`

## F-09

Impact: touched are daemon agent selection (tick, continuation dispatch, inheritance), config schema and template, and tests in daemon/turn. Depends on it: story agent inheritance, judge provider, Bug runtime, Linear (label writes limited to mapped mode labels). Existing installs: a new config key must be optional so current configs keep the first-listed-agent behaviour. No migrations found in the code read.

Evidence:

- `internal/daemon/tick.go:2819`
- `internal/daemon/agent_inherit.go:45`
