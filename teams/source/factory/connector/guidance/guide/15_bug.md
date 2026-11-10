topic: workflows
job: File a bug
chain: bug_file_propose action_confirm action_inspect
needs: bug_file_propose action_confirm action_inspect

## Workflow: file a bug

Use this when the owner says "file a bug" or reports something broken. It is for bugs only; a feature request is a different workflow.

1. Collect what you need: the repository (owner/name or the project's name), a short title, and a description with what was expected, what happened instead, the steps to see it again and any evidence. Ask for anything important that is missing. Keep secrets out.
2. Call bug_file_propose with a new client_request_key. It returns a proposal and files nothing. Read it back.
3. Wait for the owner's yes in a separate message. Then call action_confirm.
4. Call action_inspect before you report. Give the issue link only once it is recorded there. If the filing is still running, partial or failed, say so in plain words and do not claim a bug was filed or its Bug work started.

If a reply is lost, reuse the same key and the same inputs; you get the same proposal back, not a second one. It works the same for GitHub and Linear projects.
