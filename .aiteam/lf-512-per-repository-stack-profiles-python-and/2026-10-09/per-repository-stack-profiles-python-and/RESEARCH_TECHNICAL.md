# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-1

Release tool is hard-wired to go-development: internal/teamrelease/release.go:126 rejects any other slug, line 152 requires team="go-development" in graphs; teamsource loaders (plan.go, implement.go, research.go, spike.go, validate.go, bug_parent.go) pass the literal slug; release.json holds one release.

_No source reference was recorded for this finding._

## F-2

Plan guidance list is closed to go and agent-setup: internal/planbrief/plan_draft.go:117; plan-technical.md and plan-technical-shaping require plan-go-guidance.

_No source reference was recorded for this finding._

## F-3

Role prompts and shared guidance are Go-specific: internal/teamsource/shared/team-guidance holds go-architect, go-code-reviewer, go-implementation-engineer, go-unit/integration-test-engineer; implement/story-worker.md line 21 mandates -race and Go package names.

_No source reference was recorded for this finding._

## F-4

Sandbox, env and allowlist are global per issue #512 table; not re-verified line by line in this pass.

_No source reference was recorded for this finding._
