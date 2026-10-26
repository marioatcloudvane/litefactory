# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Go canonical source inherits base-team. The source guide says to edit shared content there, keep historical releases immutable, and leave version generation/linking to the owner. A source-only fix does not immediately alter installed teams or saved captures.

Evidence:

- `teams/source/go-development/inherit.json:1`
- `teams/source/go-development/README.md:3`
- `teams/go-development/v1.0.5/GENERATED.md:1`

## f2

Current reporting rules already require an outcome, known cause, impact, supported owner action and useful shortened reports. Plan also preserves review/approval boundaries. This overlaps with the newer merged #1067 work; new shared guidance should consolidate these protections rather than overwrite them with a generic recommendation.

Evidence:

- `teams/source/base-team/plan/plan-story.md:77`
- `teams/source/base-team/research/research.md:66`
- `teams/source/base-team/validate/validate-repair.md:57`

## f3

Skill delivery follows declared graph skills and transitive requires/resources, with skills resolved relative to the workflow capture. A role's one-line prose reference alone does not establish delivery of a new shared skill. A single cross-mode source location must be reconciled with existing mode-local packaging; no existing owner-report skill was located in the inspected source.

Evidence:

- `internal/teamsource/research.go:272`
- `teams/source/base-team/plan/plan.dot:31`
- `teams/source/base-team/plan/skills/plan-story-shaping/SKILL.md:4`
- `teams/source/go-development/README.md:3`

## f4

Plan has four stages, including story set, and separate question/review publication nodes. Revised comments can describe decisions and review changes without altering typed drafts or gates. The shared reporting rule must preserve supported actions and avoid claiming draft approval.

Evidence:

- `teams/source/base-team/plan/plan.dot:1`
- `teams/source/base-team/plan/plan-story.md:29`

## f5

Validate reviewers assess the same commit independently and cannot ask the owner, approve, ship or choose repair/return. The graph joins findings in a factory outcome activity; no agent-written combined final summary stage appears in this flow. Root-cause grouping across reviewers cannot simply be assumed available to each reviewer.

Evidence:

- `teams/source/base-team/validate/validate.dot:63`
- `teams/source/base-team/validate/validate-acceptance.md:29`
- `teams/source/base-team/validate/validate-acceptance.md:50`

## f6

The parent Validate report is factory-rendered from state, including lights, counts, story rows, commands and a Next line. Story reports also render findings and recovery instructions in Go. Thus the issue's broad desired final Validate experience exceeds what agent-comment instructions alone can guarantee.

Evidence:

- `internal/turn/validate_report.go:21`
- `internal/turn/validate_report.go:105`
- `internal/turn/validate_story_report.go:176`

## f7

Current finding text is bounded at 4096 bytes and marked '(text shortened)', not a bare ellipsis. Story rendering can halve text bounds to 512 bytes and omit later text/findings to fit the forge limit. More concise agent findings may help, but instructions cannot remove these factory limits.

Evidence:

- `internal/turn/validate_state.go:326`
- `internal/turn/validate_story_report.go:129`

## f8

Implement already requires naming deviations in the short completed comment; repair reports already distinguish remaining failures, repair limits and uncertain causes. These existing fields can support clearer prose without changing result construction.

Evidence:

- `teams/source/base-team/implement/skills/implement-result/node-result.md:18`
- `teams/source/base-team/validate/validate-repair.md:61`
