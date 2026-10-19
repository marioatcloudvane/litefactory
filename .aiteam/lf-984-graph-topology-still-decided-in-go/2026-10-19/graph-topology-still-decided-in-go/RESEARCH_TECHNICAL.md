# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Validate ship order (push, PR, merge, close) is chosen by a Go switch over recorded effects in shipStep; the DOT only loops back to resolve.

Evidence:

- `internal/turn/validate_resolve.go:321`

## f2

ValidateBugParent requires ProfileVersion 2 and a digest equal to the shipped bug.dot (or historical profile), so any Bug graph change is refused.

Evidence:

- `internal/teamsource/bug_parent.go:38`

## f3

Owner reply resume uses COALESCE(w.origin_node, a.node) in SQL, not a DOT edge.

Evidence:

- `internal/store/graph_owner_waits.go:99`

## f4

Remaining per-mode items were taken from the issue's survey and not individually re-verified.

_No source reference was recorded for this finding._
