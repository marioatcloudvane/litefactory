# Plan brief: graph Plan on the shared result route

- Feature: graph-plan-bypasses-the-shared-mcp
- Branch: lf/377-graph-plan-bypasses-the-shared-mcp
- Date: 2026-09-24
- Parent issue: #377
- Citation base commit: 8fb709f

## Shared Decisions

- The shared Plan result payload has two mutually exclusive parts, a story request (create, update or retire one story) and a story set. Neither may appear with the other or with the old planning fields. Used by all three stories. Defined in story-001, extended with worked examples in story-002 and story-003.
- The factory fills stage, candidate, request identity, upstream references and shared-decision revision. Agents never supply them.
- Plan roles answer ask or done. Only the factory-bound owner path promotes anything.
- Graph topology (set stage, promotion wait, creation cycle) lives in the graph definition file, not in Go.
- The two copies of the Plan role, skill and contract files (source tree and embedded tree) change together and stay identical.
- Acceptance criteria ids are unique across all three stories (AC-001 to AC-032), tasks likewise (T-001 to T-024).

## Workflow

Ship in order: story-001, then story-002, then story-003. story-002 needs the final payload from story-001 so contract examples validate. story-003 needs both. Graph Plan stays behind the legacy route for new sessions throughout. After all three merge, the owner runs one tryout on a multi-deliverable parent; lifting the hold-back is a separate final change, not part of any story.

## Cross-Story Constraints

- Do not change the legacy Plan, the Research roles or graph, graph Implement, Validate or Spike, the Bug workflow, shared result service delivery or receipts.
- Preflight records no proof; a result accepted by preflight must be accepted unchanged by submit.
- Saved Plan state stays loadable; old private Plan outputs are untrusted prompt context only and are never migrated.
- Sessions already on graph Plan keep their route; a session that already has stories skips the set stage.
- More than 12 stories is refused with advice to split the parent.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| story-001 | Route every graph Plan result through the shared result service and make Plan failures visible | [#380](https://github.com/marioatcloudvane/litefactory/issues/380) |
| story-002 | Raise the Plan agents and result contract to the Research standard and write and test the standard for every workflow | [#381](https://github.com/marioatcloudvane/litefactory/issues/381) |
| story-003 | Split a large scope into a reviewed story set and create one sub-issue per story | [#382](https://github.com/marioatcloudvane/litefactory/issues/382) |

## Out of Scope

Legacy Plan; Research roles and graph; graph Implement, Validate and Spike (#319, #321, #322); the Bug workflow (#367); how owners approve stories, technical design or acceptance beyond set review; re-splitting a promoted set; new provider hosts or tool servers; changes to shared result service delivery and receipts; migrating old Plan outputs; lifting the legacy-route hold-back.

## Provenance

- Citation base: commit 8fb709f on the task branch; research findings f1 to f18 in RESEARCH_TECHNICAL.md.
- Owner settled Q1 to Q6 all "a". The Plan run did not read the implementation-constraint documents; the written rule is added to them in story-002.
- Unsatisfied obligation: the owner-run tryout on a real multi-deliverable parent. Only the owner can supply it, it is not covered by any acceptance criterion, and the offline integration test in story-003 does not replace it.
- The nested-agent evidence decision document referenced by the Plan role does not exist in this worktree; no criterion depends on nested Codex observation.
