# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Plan graph nodes bypass the shared result service. PlanWorkflowPolicy sets a NodeProtocol; the agent activity only requires matching result authority when NodeProtocol is nil, and otherwise parses the Plan node straight from the agent's terminal text.

Evidence:

- `internal/turn/plan_graph.go:25`
- `internal/turn/research_agent.go:112`
- `internal/turn/research_agent.go:170`

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

Correction to the issue text: 'conversation' IS a valid changed_sections value. The contract lists it, the story-stage check accepts it, and the draft merge maps it to decisions, assumptions and questions. The only invalid part of the #367 request was changed_sections on a create. Valid values are intent, architecture, criteria, tasks, dependencies, design, guidance, conversation; a story-stage update may touch only intent, dependencies, conversation; a technical-stage update may not touch criteria or tasks.

Evidence:

- `internal/graphexec/plan_node.go:76`
- `internal/planbrief/plan_draft.go:261`
- `internal/teamsource/plan/skills/plan-evidence/node-result.md:39`

## f5

Silent failure is confirmed and has two causes. (a) When a graph node fails and the policy has a NodeProtocol, the workflow finishes without publishing a failure comment; Research publishes one, Plan skips it (and Plan defines no Failure candidate builder). (b) The daemon consumes the trigger comment for every graph run, on the stated basis that graph comment activities own publication, so with no comment posted the owner input is spent and nothing is visible.

Evidence:

- `internal/turn/research_workflow.go:144`
- `internal/turn/research_workflow.go:305`
- `internal/daemon/tick.go:1967`

## f6

The shared result schema already knows a 'plan' mode, but its payload is the legacy planning shape (summary, context, stories with criteria/tasks/architecture). It has no place for a graph Plan node's typed story request (operation, stage, candidate, expected_revision, upstream, shared_revision, draft, changed_sections, replacements). Edit targets for mode plan list only the legacy fields. Moving Plan onto the shared route therefore needs a new or extended Plan payload; the existing one does not fit as is.

Evidence:

- `internal/agentresult/types.go:168`
- `internal/agentresult/update.go:138`
- `internal/graphexec/plan_story.go:16`

## f7

Plan-specific checks that must survive inside the shared route: story/stage/candidate binding, exact upstream references, shared-revision match (all in the Plan decode step) and the request shape and section rules in ParsePlanNode. The same ParsePlanNode is re-run when later nodes and the retirement path reload the retained output, so a second Plan-only format is read in several places.

Evidence:

- `internal/turn/plan_graph.go:320`
- `internal/turn/plan_graph.go:371`
- `internal/turn/plan_graph.go:546`
- `internal/graphexec/plan_node.go:23`

## f8

Role files: Research roles have all 9 sections (Role, Voice, Goal, How You Are Invoked, When You Are Done, Must Do, Must Not Do, Writing the Report, Output) at about 6.8 to 6.9 KB. The three Plan role files have only 'Role' (technical adds 'Skills') at 1.1 to 1.8 KB. Confirmed by header listing.

Evidence:

- `internal/teamsource/research/research.md:7`
- `internal/teamsource/research/shaping.md:7`
- `internal/teamsource/plan/plan-story.md:6`

## f9

Two copies of the Plan role and skill files exist: teams/source/plan and internal/teamsource/plan (embedded). They are byte-identical today; the embedded one is described as an explicit copy kept in step by package consistency tests. Both must be changed together. The Research role files exist only in the embedded tree.

Evidence:

- `internal/teamsource/plan.go:13`
- `internal/teamsource/plan.go:16`

## f10

Research's node-result contract has one worked envelope per response (ask, done, approved) plus artifact and correction rules. Plan's is 3.2 KB of field prose with no JSON example and no mention of preflight, submit or receipt.

Evidence:

- `internal/teamsource/research/skills/research-evidence/node-result.md:1`
- `internal/teamsource/plan/skills/plan-evidence/node-result.md:1`

## f11

Existing tests that touch this: Plan package tests assert fixed phrases in node-result.md (e.g. 'ask or review_ready', 'changed_sections'); a Research test asserts the node-result carries a worked envelope for each response. Nothing checks role files for the 9 sections, nothing checks that a workflow policy uses the shared result service, and the constraint and context documents never mention the MCP result standard.

Evidence:

- `internal/teamsource/plan_contract_test.go:307`
- `internal/teamsource/research_response_contract_test.go:59`
