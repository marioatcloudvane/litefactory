# Research brief

## Request

Widen scope for #377: make graph Plan deliver results through the shared MCP result route (preflight, submit, receipt) and remove the NodeProtocol bypass; raise the three Plan role files and the Plan node-result contract to the Research standard; make a rejected Plan result visible on the issue without silently consuming the owner's comment; add a story decomposition step so a multi-deliverable scope yields a reviewed story set and one sub-issue per story; write and test the agent standard for every workflow.

## Decisions

- **d1** — The request is one issue with three layers: (1) route Plan node results through preflight, submit and receipt and remove the NodeProtocol bypass, (2) raise the Plan roles and contract to the Research standard, make failures visible, and write and test the standard for every workflow, (3) add story decomposition and a story-set review gate to graph Plan.
- **d2** — The agent-facing standard applies to Research, Plan and the planned Bug workflow (#367); Implement, Validate and Spike graphs (#319, #321, #322) must use it from the start.
- **d3** — Scope covers both owner scope comments: the agent-standard widening (roles, contract, visibility, guards) and the decomposition step. Graph topology for decomposition belongs in the graph definition file, not in Go, per the owner comment.


## Assumptions

- **a1** — The guard test for 'every registered workflow policy uses the shared result service' can be a structural check on WorkflowPolicy (a policy without a shared-result decoder does not register), not a runtime test per policy. Rests on the policy struct being the single registration point for Research and Plan today.
- **a2** — 'Nine sections' means the exact headings in Research: Role, Voice, Goal, How You Are Invoked, When You Are Done, Must Do, Must Not Do, Writing the Report, Output. The Plan technical role keeps its extra Skills section as a tenth.
- **a3** — A rejected Plan node result should post the failure through the existing failure-comment mechanism Research already uses, not a new channel. Whether the trigger comment stays unconsumed, or is consumed alongside a visible failure comment, is a design choice left to shaping; the issue only requires it not be consumed silently.
- **a4** — The preflight for the #367 case (create with changed_sections) should name the create rule in its diagnostic. Today's single error message does not, so the request validator message needs to be split per rule or the shared preflight must add its own.
- **a5** — The legacy-route hold-back for new Plan sessions (commit ade966e) is a temporary guard that this issue is meant to lift. Rests on the commit message and the store comment naming #377; not confirmed with the owner.
