topic: workflows
job: Check where a task stands
chain: task_evidence_read
needs: task_evidence_read

## Workflow: check a task

Use this when the owner asks where a task stands, what it found, or what needs a decision.

1. Call task_evidence_read with the task reference (for example github:owner/repository#123) and a section. Start with the section that answers the question; read further sections only when asked.
2. Follow the continuation pages to the end for any section you quote.
3. Answer in a few plain sentences: where it stands, what the evidence shows, and what needs the owner. Say what is missing or out of date.

The read changes nothing and approves nothing.
