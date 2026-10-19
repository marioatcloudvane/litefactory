# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Validate ship order (push, then PR, then merge, then close) is chosen by a Go switch over recorded effects; the DOT only loops back to resolve.

Evidence:

- `internal/turn/validate_resolve.go:321`

## f2

ValidateBugParent requires the profile digest to equal the shipped bug.dot (or historical profile) and ProfileVersion 2, so any Bug graph change is refused.

Evidence:

- `internal/teamsource/bug_parent.go:38`

## f3

Owner reply resume uses COALESCE(origin_node, source attempt node) in SQL, not a DOT edge, for all graph modes.

Evidence:

- `internal/store/graph_owner_waits.go:99`

## f4

The remaining per-mode items (Plan, Implement, Validate, Bug, Research, Spike, across-modes) were taken from the issue's survey and not individually re-verified.

_No source reference was recorded for this finding._
