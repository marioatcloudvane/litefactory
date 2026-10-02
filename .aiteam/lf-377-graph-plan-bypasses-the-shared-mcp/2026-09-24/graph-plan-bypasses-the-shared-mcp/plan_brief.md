# Plan brief: graph Plan on the shared result route

- Feature: graph-plan-bypasses-the-shared-mcp
- Branch: lf/377-graph-plan-bypasses-the-shared-mcp
- Date: 2026-09-24
- Parent issue: #377
- Citation base commit: 8fb709f

## Shared Decisions

- A graph Plan done payload requires exactly one of a story request (create, update or retire one story) and a story set, without legacy planning fields. An ask uses shared questions with neither branch and authorizes no story effect. Legacy Plan retains its admitted payload contract. Defined in story-001 and reflected in story-002/story-003 examples.
- The factory fills stage, candidate, request identity, upstream references and shared-decision revision. Agents never supply them.
- Transport request identity is preserved with exact bytes on lost replies; recover the matching receipt before retries or effects. Corrected rejected content uses a new identity. Payload identity remains factory-owned.
- Retained accepted shared content alone supplies application data and effects in Research and graph Plan. Terminal envelopes may only check completion and consistency; they cannot replace a missing result. Guards cover runtime bypass even when a policy has a nominal decoder.
- Plan roles answer ask or done. Only the factory-bound owner path promotes anything.
- Graph topology (set stage, promotion wait, creation cycle) lives in the graph definition file, not in Go.
- The two copies of the Plan role, skill and contract files (source tree and embedded tree) change together and stay identical.
- Acceptance criterion IDs are unique and never reused. Run 10 retires AC-002, AC-004, AC-005, AC-007, AC-009, AC-014, AC-016, AC-021, AC-023, AC-026, AC-027, AC-028, AC-031 and AC-032. Their replacements and added obligations use AC-033 through AC-048. Unchanged criteria keep their IDs; task IDs are T-001 through T-027.

## Workflow

Ship in order: story-001, then story-002, then story-003. story-002 needs the final payload from story-001 so contract examples validate. story-003 needs both. Graph Plan stays behind the legacy route for new sessions throughout. All three may merge with that hold-back retained. The parent remains incomplete until the two rollout obligations below are satisfied; no fourth implementation story is introduced.

## Cross-Story Constraints

- Do not change legacy Plan, Research roles or graph topology, graph Implement, Validate or Spike, the Bug workflow, or shared result service delivery/receipt mechanics. Checking and enforcing the retained-result boundary in both Research and Plan is in scope.
- Preflight records no proof, event or authority reservation. Submit applies the same validation against current authority, revisions and bindings: unchanged state gives the same outcome; changed state can refuse unchanged bytes without retention or effects.
- Positive executable contract payload examples are separate from negative fixtures and delivery walkthroughs. Negative fixtures pass their checks by being refused.
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

Legacy Plan; Research roles and graph topology; graph Implement, Validate and Spike (#319, #321, #322); the Bug workflow (#367); changes to existing per-story approval rules beyond set review; re-splitting a promoted set; new provider hosts or tool servers; changes to shared result delivery/receipt mechanics; migrating old Plan outputs. The actual hold-back lift is outside these three stories but remains a required, separately approved parent completion obligation.

## Parent rollout obligations

These are unsatisfied parent completion obligations, not claimed evidence or additional implementation stories. AC-045 requires their tracking. Completing story criteria does not satisfy them or close #377.

| Obligation | Owner | Evidence and version bounds | Missing prerequisites / status |
| --- | --- | --- | --- |
| Real graph Plan tryout | Authenticated owner @marioatcloudvane | After all three stories merge, identify the merged commit, real multi-deliverable parent, run and shared-result receipt references, displayed story set and reviewed revision, owner promotion, and each created sub-issue with its stable parent-prefixed ordinal. Inspect whether independent deliverables were actually decomposed and all agreed scope was retained. | Pending: merged implementation, owner-operated graph tryout, and authentic evidence. Fake-agent tests prove orchestration, not real-agent decomposition or runtime delivery. |
| Hold-back lift and verification | Authenticated owner approves the separate change; factory/operator applies and records it | After successful tryout, retain explicit owner approval of the lift, the separate change and merged revision, then actual new-session admission evidence showing graph Plan selected at that revision. | Pending: successful tryout, separate owner approval, lift change and verification. No early lift or implicit approval follows from completing this Plan. |

## Provenance

- Citation base: commit 8fb709f on the task branch; research findings f1 to f18 in RESEARCH_TECHNICAL.md.
- Owner settled Q1 to Q6 all "a". The Plan run did not read the implementation-constraint documents; the written rule is added to them in story-002.
- Run 10 owner instruction, "I like your comments, add them to the plan!", authorizes the amendments from PLAN_CROSS_CHECK.md. The three existing story keys, shipping order and settled choices remain. Replaced criterion IDs are retired, not reused.
- AC-045 requires recording the two pending parent obligations above; it does not claim their execution. AC-046 establishes offline orchestration only, and the two rollout obligations remain unsatisfied until their authentic evidence exists.
- The nested-agent evidence decision document referenced by the Plan role does not exist in this worktree; no criterion depends on nested Codex observation.
