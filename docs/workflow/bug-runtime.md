# Bug

[Documentation](../README.md) · [Workflow guide](../guides/workflow.md)

The Bug workflow fixes one bug end to end: diagnose, wait for your approval, repair, then deliver the fix. The flow is in `internal/teamsource/bug/bug.dot`.

## Starting a Bug task

Label the issue `bug` and comment as the owner. If the issue has no `Mode:` label and the comment is not a mode command, the factory treats it as a Bug task. This is fixed for the life of the task: a mode command (`/plan`, `/implement`, ...) on a Bug task is refused. Use another issue for other work. Commands are listed in [owner commands](../guides/owner-commands.md).

## The steps

1. **Conversation and analysis.** A read-only analyst (the flagship model of your default agent) studies the bug and posts a diagnosis. You can ask questions and give guidance in ordinary replies.
2. **Approval.** Nothing is changed until you send the exact command `/approve-bug analysis <revision>` for the current, confirmed diagnosis. Guidance does not approve a repair, and an old or changed diagnosis cannot be approved.
3. **Repair.** One fixer agent makes the smallest change that fixes the approved bug, and builds and tests it inside that same invocation, using the repository's own commands. The factory does not run a separate test phase for Bug.
4. **Delivery.** The factory saves the repair as a commit, pushes the branch without force, opens (or finds) one pull request into the target branch, merges it only at the expected head, checks that the merge really happened, and then closes the issue. There is no second owner review.

Each step observes the real state first and does only what is missing, so repeating it after a failure is safe. A merge that happened but could not be confirmed does not allow closing the issue, merging again or repairing again.

## When something stops

Every stop (a question, a blocked step, a failure, or an exhausted attempt budget) is parked with its reason and a question for you. Your next ordinary reply resumes that step once, with your words as guidance. After exhaustion that gives one more bounded fixer run. If the branch has gone stale, the work first parks at delivery; once you reply, it is revalidated before it continues. Normal task cancellation stays available. The older special commands for Bug grants and test retries no longer exist.

Before an agent starts, unfinished edits in the worktree are kept in a local checkpoint when needed, and the worktree is synced using your [sync settings](../guides/sync-and-conflicts.md).

## Bug history from older versions

The six tables that hold earlier Bug history (`bug_tasks`, `bug_analyses`, `bug_repairs`, `bug_checks`, `bug_delivery`, `bug_events`) are read-only now. Their rows are kept as they were; nothing can insert, change or delete them. They are evidence only and never grant approval or permission to deliver.

A database that still has Bug history from before the table consolidation (migration 0079) is refused at startup rather than losing it; see the [release runbook](../release-runbook.md). Bug tasks that were started with an older, unsupported Bug definition are not restarted, cancelled or converted. Their branches, discussions and pull requests stay yours to deal with, and you may open a new issue for the same bug.

The repository check recipe used by [Validate](validate.md) and [Spike](spike.md) is described in [Bug checks](../bug-checks.md).
