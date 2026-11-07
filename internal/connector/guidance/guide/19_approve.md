topic: workflows
job: Approve a stage
chain: approve_stage action_confirm decision_get
needs: approve_stage action_confirm decision_get

## Workflow: approve a stage

Use this only when the owner explicitly asks to approve the task's current stage.

1. Make sure you and the owner mean the same task and stage. If the owner also wants changes, revise first; never queue approval for work that does not exist yet.
2. Call approve_stage. It returns the exact candidate, what it depends on, the effect and the public decision summary, and changes nothing. Read it all back.
3. Wait for the owner's yes in a separate message. Then call action_confirm. A binding approval also needs the owner's check mark on the factory's Discord message; you cannot supply it.
4. Call decision_get. A missing reply is not permission to try again.
