topic: workflows
needs: repo_read repo_grep repo_log repo_diff
job: Read or review a task's code
chain: repo_grep repo_read repo_diff repo_log

## Workflow: read or review a task's code

Use this when the owner asks what the code of a task does, what changed, or whether something is in it. It reads committed code only; it changes nothing.

1. Name the task (its reference) on every call, and choose the task branch or its base branch.
2. To find something, search with repo_grep. Then read the matching files with repo_read before you explain how they behave.
3. For what changed, use repo_diff (base to task, or one commit) and repo_log for the history behind it.
4. Cite the full commit, the branch, the file and the lines. Keep related reads at the same observed commit by passing the returned view token on.
5. Say plainly when a search found nothing and when coverage was incomplete: "no match" and "not fully searched" are different answers. Label your own interpretation apart from what you read.

Uncommitted files and other branches are out of reach. Reading code grants no permission to run or edit it.
