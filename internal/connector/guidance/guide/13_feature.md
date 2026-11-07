topic: workflows
job: File a feature request
chain: brainstorm_open brainstorm_draft brainstorm_drafts_get action_propose action_confirm brainstorm_tasks_get
needs: brainstorm_open brainstorm_add brainstorm_ask brainstorm_scan_get brainstorm_draft brainstorm_drafts_get brainstorm_tasks_get action_propose action_confirm

## Workflow: file a feature request

This guide tool files nothing. A feature request is filed through a brainstorm and its drafts.

1. Work the idea out in conversation: goal and who it helps, scope and non-goals, open questions one at a time.
2. Call brainstorm_open for the repository (it resumes an open one). Keep ideas, decisions and open questions with brainstorm_add. On resume, read every kept entry first.
3. Send each factual question about the repository to brainstorm_ask yourself; each one is a paid read-only run. Read the answer with brainstorm_scan_get.
4. When the owner says it is ready, call brainstorm_draft. It makes one to five issue drafts. Check progress with brainstorm_scan_get, then read the drafts with brainstorm_drafts_get. Read each draft aloud: title, size, impact and any possible duplicate (a warning, never proof).
5. The owner keeps or drops drafts. Ask the open questions one at a time and keep each answer with brainstorm_add.
6. Propose creating the kept drafts with action_propose, using the action brainstorm_create_tasks with the arguments brainstorm, draft_set and drafts (the keys of the kept drafts, in the order read). Read the proposal back. Wait for a separate yes, then call action_confirm.
7. Read the outcome per draft with brainstorm_tasks_get and say it plainly, with the issue number when known. If creation is uncertain, do not propose it again while checks run.

The yes approves only creating those issues and starting Plan. It approves no Plan stage. This is for features and ideas; a bug has its own workflow.
