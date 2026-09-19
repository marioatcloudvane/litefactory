# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Ordinary dispatch enters Daemon.run, admits the mode, calls resolveAgent with the issue-label snapshot, refuses invalid selections, and replaces an absent selection with d.defaultAgent. It passes the resulting kind as turn.Request.Agent to Execute. Session history does not choose the agent in this stage. All repository citations refer to revision 7d5799834b5031c84286080708aaff6091ea63ee.

Evidence:

- `internal/daemon/tick.go:1758`
- `internal/daemon/tick.go:1779`
- `internal/daemon/tick.go:1849`

## f2

resolveAgent detects the prefix Coding Agent: followed by a space case-insensitively but accepts its exact capitalization only. More than one detected selector is refused as ambiguous before validating individual labels. The value must match ^[A-Z][A-Za-z0-9_-]{0,39}$; it is then lowercased and looked up in the registered runner map. Malformed or unregistered selections are refused, without falling back to configuration. Unrelated labels are ignored. The resolver itself makes no API call.

Evidence:

- `internal/daemon/agent.go:21`
- `internal/daemon/agent.go:96`
- `internal/daemon/agent_internal_test.go:31`

## f3

Configuration preserves the YAML declaration order under agents; Config.DefaultAgent returns the first entry. Startup builds the runner set and obtains this default from configuration. Daemon.New rejects a missing default, a default with no registered runner, or a default without a mode classifier. A valid explicit label overrides the default. With no label, the default applies even if the session previously used another agent.

Evidence:

- `internal/config/config.go:62`
- `internal/daemon/setup.go:162`
- `internal/daemon/daemon.go:420`
- `internal/daemon/tick.go:1786`

## f4

For ordinary turns, prepareDeliverySegment reads the latest stored segment. If its agent differs from Request.Agent, reconcileSegmentAgent changes that same row in place, preserving ID and ordinal, generating a fresh session reference, clearing the resume token, and marking AgentSwitched. SetSegmentAgent also clears the delivery pin so a newly rendered bundle can be pinned. resumeDecision forces a cold start, including when switching back to an agent used earlier. Prior run records retain their recorded agents. Thus a valid Codex label with a Claude segment selects Codex and starts a fresh provider conversation; it does not refuse or resume Claude.

Evidence:

- `internal/turn/delivery.go:316`
- `internal/turn/delivery.go:446`
- `internal/store/segments.go:192`
- `internal/turn/prepare.go:1102`
- `internal/turn/execute_test.go:318`
- `internal/turn/execute_test.go:481`

## f5

Session state controls conversation reuse after agent selection. An agent switch takes precedence over simultaneous mode/bundle changes. For the same agent, a changed bundle identity or admitted mode rotates to a new segment; mode comparison uses the previous terminal, non-refused run. Otherwise the existing segment is reused. Normal resume requires a previous eligible run and a matching agent; Claude uses its session reference, while other agents additionally need a resume token. Preparation obtains the runner from the segment's reconciled agent and fails before run allocation if it is not registered.

Evidence:

- `internal/turn/delivery.go:351`
- `internal/turn/prepare.go:249`
- `internal/turn/prepare.go:1049`
- `internal/turn/prepare.go:1102`

## f6

Automatic recovery is the exception: observeContinuation fetches the current issue, resolves labels/default again, and cancels recovery with 'coding agent changed; resume manually' if selection is refused or differs from the saved continuation agent. Recovery loads its exact segment without ordinary reconciliation. A remaining agent mismatch in prepare is refused as defense in depth. Existing tests cover ordinary switching, switching back with a cold start, and the continuation-only refusal race.

Evidence:

- `internal/daemon/continuation_dispatch.go:91`
- `internal/turn/delivery.go:317`
- `internal/turn/prepare.go:233`
- `internal/turn/execute_test.go:562`
