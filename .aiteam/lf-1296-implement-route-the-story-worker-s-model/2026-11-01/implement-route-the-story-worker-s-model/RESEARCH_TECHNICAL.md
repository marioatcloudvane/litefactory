# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The story-worker role is already tier flagship in source and in all four 1.0.0 teams, after commit 79bafcc9 (owner decision 2026-11-01, PR #1293 merged 09:30Z). Issue #1296 was opened at 09:41Z but still states balanced. As written, the request is a no-op.

Evidence:

- `teams/source/base-team/implement/story-worker.md:4`
- `teams/go-development/v1.0.0/implement/story-worker.md:4`

## f2

Validate roles (acceptance, architecture, code, repair) are all flagship already, so 'flagship reviewers only for L' would also mean routing down, not up.

Evidence:

- `teams/source/base-team/validate/validate-code.md:4`

## f3

A node-level lf_model_tier overrides the role tier when the node is captured, and delivery resolves the model from that captured tier. Allowed values are flagship, balanced and fast. Bug's analyst node already uses this override.

Evidence:

- `internal/teamsource/research.go:241`
- `internal/turn/research_delivery.go:42`
- `internal/graphexec/invoke_agent.go:34`
- `teams/source/base-team/bug/bug.dot:7`

## f4

Fact comparisons (story.size, story.sizing_valid, story.fallback) are only accepted when the mode is Plan. In every other mode, lf_field must be response.

Evidence:

- `internal/graphexec/research_control.go:52`
- `internal/graphexec/research_control.go:87`
- `internal/graphexec/plan_parallel.go:55`

## f5

The Implement control vocabulary is only completed/incomplete/blocked, and the Implement graph check rejects any action outside its known list (lf_if is not on it).

Evidence:

- `internal/graphexec/implement.go:65`
- `internal/graphexec/implement.go:190`

## f6

The Implement graph check requires exactly one non-resolver lf_agent, and requires the worker sync to send synced and current to the same worker agent.

Evidence:

- `internal/graphexec/implement.go:247`
- `internal/graphexec/implement.go:467`

## f7

Story size is computed only from Plan state (PlanStorySetState entry), and only inside the Plan runtime. Implement's records (ImplementStory, ManagedStory, ApprovedStoryRevision) carry no size, and implementWorkerHandoff does not read Plan state. Plan state stays readable by parent ref (GetPlanWorkflowState is also used by the daemon and ask), so Implement could read it. Inference: no code does this today.

Evidence:

- `internal/turn/plan_parallel.go:33`
- `internal/store/implement_stories.go:38`
- `internal/store/managed_stories.go:29`
- `internal/turn/implement_steps.go:628`
- `internal/daemon/story_refinements.go:136`

## f8

'Growth' is a Plan-only event: RecordPlanGrowth moves a story from the fast track back to the normal Plan path and does not change its size. Nothing in Implement re-sizes a story, so an M story cannot become L mid-Implement.

Evidence:

- `internal/planbrief/plan_parallel.go:318`

## f9

Impact: touched would be implement.dot (source and four 1.0.0 releases), graphexec Implement checks and control policy, and the Implement runtime's fact source. Depends on it: every team release's Implement graph and its validation tests. Superseded: none found. Data: no migration found; the size is already persisted in Plan state. Not verified: whether slot budget and lf_source resolution assume one worker node.

Evidence:

- `teams/source/base-team/implement/implement.dot:59`
- `teams/teams.json:1`
