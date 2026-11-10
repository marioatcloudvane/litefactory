topic: workflows
needs: action_cancel action_inspect action_history
job: Cancel or recover a proposal
chain: action_inspect action_cancel action_history

## Workflow: cancel or recover a proposal

Use this when the owner changes their mind, a reply was lost, or you are unsure what happened to an earlier proposal.

1. Read the recorded state first with action_inspect. It shows the proposal, the confirmation decisions and the outcome. Do not guess, and do not repeat a confirmation.
2. If the owner no longer wants a pending proposal, cancel it with action_cancel. Work that was already admitted cannot be undone: say so.
3. To find an older proposal after a reconnect, page action_history. It covers the last ninety days across your clients.
4. A stale, expired or cancelled proposal needs a fresh proposal and a fresh yes. An uncertain outcome is resolved by reading the recorded receipt, never by sending the effect again.
