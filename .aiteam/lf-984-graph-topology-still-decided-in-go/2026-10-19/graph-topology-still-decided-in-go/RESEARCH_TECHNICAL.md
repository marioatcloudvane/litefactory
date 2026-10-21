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

Remaining per-mode topology items were taken from the issue's survey and not individually re-verified.

_No source reference was recorded for this finding._

## f5

The hard-state evidence (#1006-#1012, #989, unconsumed story refinements after Plan to Implement) comes from the owner's 2026-10-21 comment and was not re-verified in source.

_No source reference was recorded for this finding._
