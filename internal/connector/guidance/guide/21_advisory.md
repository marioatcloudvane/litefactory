topic: workflows
needs: ask_task_agent judge_task advisory_job_get advisory_job_cancel
job: Ask why a task was done this way (for an independent judgement use judge_task instead)
chain: ask_task_agent advisory_job_get advisory_job_cancel

## Workflow: ask why, or get an independent judgement

Use this only when the owner explicitly asks for it. For ordinary factual questions use the recorded task evidence instead; it costs nothing.

1. Pick the right call. To learn why the current work was done this way, use ask_task_agent. For a second opinion on quality, risks, gaps or alternatives, use judge_task. If it is unclear which the owner wants, ask in plain words; never start both.
2. Say what you are about to do and that it may cost money. Both calls start one private paid job on the owner's standing advisory consent and need no separate yes. Say the estimate, or that the cost is unknown.
3. Keep the returned job and conversation references yourself. After a lost reply, retry with the same request key and the same inputs.
4. Read the result with advisory_job_get. A partial page is not the answer: follow the cursors to the end. Report what the answer is based on, how fresh it is and what it left out.
5. To stop a queued or running job, use advisory_job_cancel. A finished answer may still win the race.

The answer is private and changes no stage, approval or wait. A judgement is not an approval.
