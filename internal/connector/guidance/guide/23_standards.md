topic: workflows
needs: standards_read standards_role_counts
job: Look up the published standards
chain: standards_read standards_role_counts

## Workflow: look up the published standards

Use this when the owner asks which published standards apply to a task or role.

1. Call standards_read with the task. Filter by role and category if the owner asked for them; the filters narrow applicability and do not make you act as a role.
2. To compare how many standards apply to each role, call standards_role_counts. It labels the selected categories and the whole book separately; keep that difference when you speak.
3. Cite the standard identifier, the book revision and the observation time. When you relate a rule to code, say which part is a retrieved rule, which is a retrieved fact, and which is your interpretation.

The current book is not proof of which standards governed an earlier run. This reads only: you cannot create, edit or approve a standard here.
