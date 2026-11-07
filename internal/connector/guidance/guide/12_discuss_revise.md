topic: workflows
job: Discuss or revise a plan
chain: task_evidence_read revise_task action_confirm decision_get
needs: task_evidence_read revise_task action_confirm decision_get

## Workflow: discuss or revise a plan

Use this when the owner questions a plan, wants it changed, or asks for it to be approved.

1. Read the task with task_evidence_read, including the current plan and its approval evidence.
2. Talk it through. A question or an opinion alone is not a request to change anything.
3. When the owner asks for a change, call revise_task. It returns an exact proposal and changes nothing. Read back the task, the requested change, the effect on later work and the short decision summary.
4. Wait for the owner's yes in a separate message. Then call action_confirm.
5. Call decision_get to see whether the revision was admitted and how it is going. Admitted is not finished, and a revision is not an approval of the new plan.

Approving a stage is a separate request with its own yes. If the owner asks for changes and approval together, revise first and never approve work the owner has not seen.
