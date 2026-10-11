# Cleanup

Preview automatic retirement before deciding whether a task's local resources can be removed. This requires the initialized factory, registered clones and configured bot access to check issues and remote refs.

```sh
litefactory task retire
litefactory logs --all --grep retirement
```

`task retire` fetches fresh remote refs and reports exact resources, actions and skip reasons. It deletes nothing, retains ownership and does not advance the automatic sweep schedule. `would_remove` is advisory; the daemon repeats checks before removal.

## Automatic retirement

The daemon performs daily conservative sweeps. It requires an authoritative closed-issue response, certain resource ownership and no conflicting active or required pending work. Errors or missing issues are not proof of closure. Shared children use parent resources and parent closure state.

Worktree retention defaults to 14 days, measured from the last completed run (including failures), or a valid creation time for a certainly owned task with no runs. Recent issue closure does not reset that age. Configure the policy through the [configuration reference](../configuration/config-yaml.md).

Expired worktrees must be clean, including tracked, staged, untracked **and ignored** files, and all local commits must be preserved in a freshly fetched remote task branch or remote base. Missing upstream is not preservation. Dirty/unpreserved work defers all resource retirement.

Each local or remote branch tip must be an ancestor of the fresh remote base before deletion; squash/rebase/cherry-pick equivalence is insufficient. Remote branch cleanup need not wait for worktree retention. Local branches used by any worktree, configured base/default branches and uncertain/manual resources remain protected. There are no automatic archive tags.

## Explicit worktree removal

After preserving needed edits and ensuring no run is using the worktree:

```sh
litefactory task clean 'github:OWNER/REPO#123'
```

Replace the placeholder with the intended task. This explicitly removes its recorded worktree and clears that path; the branch stays untouched. It operates independently of automatic retirement policy. An already absent directory is reported and its recorded path cleared. Dirty work refuses removal by default. `--force` explicitly permits discarding uncommitted work; committed work remaining on the branch does not protect uncommitted files. Preserve those files elsewhere before deciding to use force.

If an issue number is ambiguous, use the full task ref. If cleanup refuses, inspect the named path and Git status, including ignored files, then retry only after resolving the cause. Never delete the registered clone to clean one task.

Logs distinguish `removed`, `kept`, `already_absent`, `failed` and `unknown`. An uncertain deletion is reconciled against current resources; an old intent does not authorize deleting a recreated checkout or advanced branch. See [retirement details](../retirement.md), [recovery](recovery.md) and [CLI reference](../cli/command-reference.md).

[Operations](README.md)
