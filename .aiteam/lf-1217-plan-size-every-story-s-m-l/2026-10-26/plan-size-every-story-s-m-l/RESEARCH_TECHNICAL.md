# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

Research sizing is already consumed as guidance: plan-evidence says S-01 sets plan depth and shape. No per-story size or reason exists in the inspected authored set entry, progressive draft or final PlanStory contracts.

Evidence:

- `teams/source/base-team/plan/skills/plan-evidence/SKILL.md:35`
- `internal/agentresult/plan_graph.go:64`
- `internal/planbrief/plan_draft.go:16`
- `internal/agentresult/types.go:272`

## F-02

The proposed story set is rendered as ID, name, description and dependencies only. Authored entries are explicitly copied into saved entries by the turn layer. Size needs to survive this boundary and promotion into a story, rather than existing only in prose.

Evidence:

- `internal/planbrief/plan_set_proposal.go:92`
- `internal/turn/plan_graph_set.go:50`
- `internal/planbrief/plan_set.go:22`

## F-03

Depth guidance currently conflicts with variable acceptance depth: acceptance shaping says three to five criteria per story. Technical shaping already requires General and adds data/service/solution sections only where applicable. Acceptance validation links every criterion to tasks; smaller plans must retain those proof links.

Evidence:

- `teams/source/base-team/plan/skills/plan-acceptance-shaping/SKILL.md:7`
- `teams/source/base-team/plan/skills/plan-technical-shaping/SKILL.md:6`
- `internal/planbrief/plan_draft.go:180`

## F-04

Impact: canonical team guidance is shared in base-team and inherited by all four stacks. Plan set/story/technical/acceptance skills and their result examples, Go result validation, planbrief state, turn conversion and report rendering are affected. Existing set, draft, edit and workflow tests offer relevant regression seams.

Evidence:

- `teams/README.md:1`
- `teams/source/base-team/plan/plan.dot:31`
- `internal/planbrief/plan_set_proposal_test.go:106`
- `internal/report/plan_draft_test.go:20`
- `internal/planbrief/plan_edit_test.go:146`

## F-05

Compatibility: set content and drafts are hashed for exact review revisions. MergePlanDraft updates explicit sections; owner issue edits are parsed back into typed content. Adding size requires checking revision binding, selective merge, edit round trips and assembly into TypedStory, or size could disappear or approvals could refer to stale content. Saved pre-set state is explicitly supported.

Evidence:

- `internal/planbrief/plan_set_proposal.go:32`
- `internal/planbrief/plan_draft.go:218`
- `internal/planbrief/plan_edit.go:28`
- `internal/planbrief/plan_workflow.go:808`
- `internal/planbrief/plan_set.go:29`

## F-06

Downstream: the acceptance role assembles current active stories for final review; factory publication uses story issue identities and a link-only parent index. Sizes must remain available to this review and future fast-track work. No external provider API or credential change is indicated by the inspected contracts.

Evidence:

- `teams/source/base-team/plan/skills/plan-acceptance-shaping/SKILL.md:20`
- `internal/report/plan_draft.go:93`
- `internal/planbrief/plan_workflow.go:808`

## F-07

Supersession: #957 is closed and its owner says #1217 and #1218 start fresh on team 1.0.0. #1218 owns shorter paths, fewer stops and escalation to the normal path. Historical sizing-stage and architecture-count proposals are context, not current settled requirements. Recent #984 work introduced lf_gate while retaining shipped graph behavior; the current graph still has set/story/technical/acceptance.

Evidence:

- `teams/source/base-team/plan/plan.dot:1`
- `.aiteam/lf-984-graph-topology-still-decided-in-go/2026-10-19/graph-topology-still-decided-in-go/RESEARCH_BRIEF.md:7`

## F-08

Distribution: source teams are canonical; generated 1.0.0 releases are not edited directly. Owner-led release and new captures deliver future source edits; saved captures and historical reports remain immutable. No migration policy for already saved unsized plans is established by this request.

Evidence:

- `teams/README.md:1`
- `teams/README.md:88`
