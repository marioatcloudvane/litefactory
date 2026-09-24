# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Plan graph nodes bypass the shared result service. PlanWorkflowPolicy sets a NodeProtocol; the agent activity only requires matching result authority when NodeProtocol is nil, and otherwise parses the Plan node straight from the agent's terminal text. Policy admission also accepts a policy with NodeProtocol and no Decode, so the bypass is structurally allowed.

Evidence:

- `internal/turn/plan_graph.go:25`
- `internal/turn/research_agent.go:112`
- `internal/turn/research_agent.go:170`
- `internal/turn/research_admission.go:32`

## f2

The Plan prompt tells the agent to return the private Plan node JSON with no shared application result wrapper, and never adds MCP guidance or a final-route binding. Research's invocation builds the MCP service, guidance and final route; Plan's prepare builds none of that and returns an invocation with no Results service.

Evidence:

- `internal/turn/plan_graph.go:307`
- `internal/turn/research_workflow.go:165`
- `internal/turn/research_workflow.go:252`

## f3

Root cause of the #367 failure: PlanStoryRequest.Validate rejects a create that carries changed_sections, expected_revision or a non-story stage, but with one message, 'create requires an initial story candidate', that names none of the three. The Plan node-result contract only describes changed_sections for updates.

Evidence:

- `internal/graphexec/plan_story.go:78`
- `internal/graphexec/plan_story.go:79`
- `internal/teamsource/plan/skills/plan-evidence/node-result.md:39`

## f4

Correction to the issue text: 'conversation' IS a valid changed_sections value (decisions, assumptions, questions). The only invalid part of the #367 request was changed_sections on a create. Valid values: intent, architecture, criteria, tasks, dependencies, design, guidance, conversation; a story-stage update may touch only intent, dependencies, conversation; a technical-stage update may not touch criteria or tasks.

Evidence:

- `internal/graphexec/plan_node.go:76`
- `internal/planbrief/plan_draft.go:261`
- `internal/teamsource/plan/skills/plan-evidence/node-result.md:39`

## f5

Silent failure is confirmed and has two causes. (a) When a graph node fails and the policy has a NodeProtocol, the workflow finishes without publishing a failure comment; Research publishes one, Plan skips it (and Plan defines no Failure candidate builder). (b) The daemon consumes the trigger comment for every graph run, on the basis that graph comment activities own publication, so with no comment posted the owner input is spent and nothing is visible.

Evidence:

- `internal/turn/research_workflow.go:144`
- `internal/turn/research_workflow.go:305`
- `internal/daemon/tick.go:1967`

## f6

The shared result schema already knows a 'plan' mode, but its payload is the legacy planning shape (summary, context, stories with criteria/tasks/architecture). It has no place for a graph Plan node's typed story request (operation, stage, candidate, expected_revision, upstream, shared_revision, draft, changed_sections, replacements). Edit targets for mode plan list only the legacy fields. Moving Plan onto the shared route needs a new or extended Plan payload.

Evidence:

- `internal/agentresult/types.go:168`
- `internal/agentresult/update.go:138`
- `internal/graphexec/plan_story.go:16`

## f7

Plan-specific checks that must survive inside the shared route: story/stage/candidate binding, exact upstream references, shared-revision match (Plan decode step) and the request shape and section rules in ParsePlanNode. ParsePlanNode is re-run when later nodes and the retirement path reload the retained output, so a second Plan-only format is read in several places.

Evidence:

- `internal/turn/plan_graph.go:320`
- `internal/turn/plan_graph.go:371`
- `internal/turn/plan_graph.go:546`
- `internal/graphexec/plan_node.go:23`

## f8

Role files: Research roles have all 9 sections (Role, Voice, Goal, How You Are Invoked, When You Are Done, Must Do, Must Not Do, Writing the Report, Output) at about 6.8 to 6.9 KB. The three Plan role files have only 'Role' (technical adds 'Skills') at 1.1 to 1.8 KB.

Evidence:

- `internal/teamsource/research/research.md:7`
- `internal/teamsource/research/shaping.md:7`
- `internal/teamsource/plan/plan-story.md:7`

## f9

Two copies of the Plan role and skill files exist: teams/source/plan and internal/teamsource/plan (embedded). They are byte-identical today; the embedded one is an explicit copy kept in step by package consistency tests. Both must change together. The Research role files exist only in the embedded tree.

Evidence:

- `internal/teamsource/plan.go:13`
- `internal/teamsource/plan.go:16`

## f10

Research's node-result contract has one worked envelope per response (ask, done, approved) plus artifact and correction rules and a 'Delivering the result' section that names preflight, submit and receipt. Plan's is about 3.2 KB of field prose with no JSON example and no mention of preflight, submit or receipt.

Evidence:

- `internal/teamsource/research/skills/research-evidence/node-result.md:1`
- `internal/teamsource/plan/skills/plan-evidence/node-result.md:1`

## f11

Existing tests that touch this: Plan package tests assert fixed phrases in node-result.md (e.g. 'ask or review_ready', 'changed_sections'); a Research test asserts the node-result carries a worked envelope for each response. Nothing checks role files for the 9 sections, nothing checks that a workflow policy uses the shared result service, and APP_CONSTRAINTS.md and APP_CONTEXT.md never mention the MCP result standard. It appears only in CLAUDE.md as final-output behavior for agents.

Evidence:

- `internal/teamsource/plan_contract_test.go:307`
- `internal/teamsource/research_response_contract_test.go:59`
- `CLAUDE.md:42`

## f12

Graph Plan is currently held back. AdmitPlanPath now admits new sessions to 'legacy' instead of 'graph'; sessions already admitted keep their route and graph tests admit graph explicitly. The commit message and store comment give the reason: graph Plan created one story for a multi-story scope, lacks the shared role/result standard, and bypasses MCP (#377). The hold-back is meant to last until graph Plan is reworked.

Evidence:

- `internal/store/plan_workflow.go:18`

## f13

Decomposition gap (second scope comment) is confirmed. The story role's task is 'Shape only the selected story intent' and the story-shaping skill says the same; neither mentions splitting or a story set. One node result carries a single request, so one agent run creates at most one story. The graph runs story_agent, story_draft, story_gate, story_review, then waits, and no node proposes further stories. Only the legacy Plan has a decomposing role (proxy-product-owner, 'Decompose the reviewed specification into independently executable managed stories').

Evidence:

- `internal/teamsource/plan/plan-story.md:5`
- `internal/teamsource/plan/skills/plan-story-shaping/SKILL.md:11`
- `internal/graphexec/plan_node.go:19`
- `internal/teamsource/plan/plan.dot:15`
- `internal/teamsource/plan/plan.dot:34`
- `teams/source/agents/shared/proxy-product-owner.md:3`

## f14

Graph topology lives in plan.dot: the story/technical/acceptance loops are three near-identical blocks, and all three use the single lf_plan_story effect for one story. A story-set loop or a decomposition node would be a change to this file plus the action set, which matches the owner's requirement that topology belongs in the graph file, not in Go.

Evidence:

- `internal/teamsource/plan/plan.dot:13`
- `internal/graphexec/plan_story.go:26`

## f15

A related fix landed on this branch: roles without their own tool list now inherit the session tools, so the attempt's MCP tools reach every Claude graph node (Research and Plan). Before it the MCP server was connected but its tools hidden. The shared route is therefore usable by Plan agents without further render work.

Evidence:

- `internal/render/native.go:25`
