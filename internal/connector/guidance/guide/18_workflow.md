topic: workflows
needs: workflows_list workflow_describe workflow_propose workflow_continue_propose action_confirm workflow_job_get

## Workflow: start a workflow

Use this to start an installed workflow (for example log analysis) for the owner.

1. Call workflows_list to find the workflow and its version. Only installed, available workflows can be started.
2. Call workflow_describe for its target rules, inputs, permissions and limits.
3. Collect the inputs from the owner. Resolve dates to exact times and ask for any explicit choice, such as a dry run.
4. Call workflow_propose with the exact workflow reference, version and inputs. It starts no work. Read back the target, inputs, impact and deadline.
5. Wait for the owner's yes in a separate message. Then call action_confirm. That admits the work; it does not finish it.
6. Call workflow_job_get for progress, questions and results. If the job waits for answers, propose the answers with workflow_continue_propose and wait for another yes.

Saved results are evidence, not approval. If a result proposes something, the owner still has to approve it separately.
