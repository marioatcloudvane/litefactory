# Research brief

## Request

Widen scope for #377: make graph Plan deliver results through the shared MCP result route (preflight, submit, receipt), remove the NodeProtocol bypass, raise the three Plan role files and the Plan node-result contract to the Research standard, make rejected Plan results visible on the issue without silently consuming the owner's comment, and write and test the agent standard for every workflow.

## Decisions

- **d1** — The request is one issue with two layers: (1) route Plan node results through preflight, submit and receipt and remove the NodeProtocol bypass, (2) raise the Plan roles and contract to the Research standard, make failures visible, and write and test the standard for every workflow.
- **d2** — The Plan graph edge cases and the agent-facing standard apply to Research, Plan and the planned Bug workflow (#367); Implement, Validate and Spike graphs (#319, #321, #322) must use it from the start.


## Assumptions

- **a1** — The guard test for 'every registered workflow policy uses the shared result service' can be a structural check on WorkflowPolicy (a policy without a shared-result decoder does not register), not a runtime test per policy. Rests on the policy struct being the single registration point for Research and Plan today.
- **a2** — 'Nine sections' means the exact headings in Research: Role, Voice, Goal, How You Are Invoked, When You Are Done, Must Do, Must Not Do, Writing the Report, Output. The Plan technical role keeps its extra Skills section as a tenth.
- **a3** — A rejected Plan node result should post the failure through the existing failure-comment mechanism Research already uses, not a new channel. Whether the trigger comment stays unconsumed, or is consumed alongside a visible failure comment, is a design choice left to shaping; the issue only requires it not be consumed silently.
- **a4** — The preflight for the #367 case (create with changed_sections) should name the create rule in its diagnostic. Today's single error message does not, so the message in the request validator needs to be split per rule or the shared preflight must add its own.
