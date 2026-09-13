# Retiring finished work

The daemon checks registered task resources daily. A pass processes one owner
per tick after normal polling and dispatch, with a 30-second batch timeout.
The next pass is due 24 elapsed hours after completion. Progress survives
restarts; startup recovery runs first. Request-budget pressure and GitHub
backoff defer cleanup without consuming normal execution/reporting capacity.

Run `litefactory task retire` to preview exact resources, proposed actions and
skip reasons. It fetches fresh remote references but deletes nothing and does
not advance the daily schedule. The preview is advisory; the daemon repeats
all checks before acting.

A successful individual GitHub issue response must identify the registered
repository and issue and explicitly say `closed`. Each resource deletion has
another uncached issue check immediately before it. Errors, missing issues and
unknown responses never imply closure. A reopen can still race after that final
read: GitHub does not offer an atomic issue-state-and-Git-deletion transaction.
Reopening later recreates the checkout on the next authorized run from a surviving
local branch, remote branch, or fresh remote base.

`worktree_retain_days` defaults to 14 and is active. Age starts at the last
completed run's finish, including failed runs. Tasks with no runs may use a
valid creation timestamp only when ownership is certain. Recent closure does
not reset this age. Active/prelaunch work and required pending operations defer
retirement. Shared children use the parent's resources and closure state.

Expired worktrees are removed only when tracked, staged, untracked and ignored
content is absent and all local commits are preserved in a freshly fetched
remote task branch or remote base. Ignored build outputs therefore cause skips:
inspect and remove them yourself if unwanted. Dirty or locally unpreserved tasks
defer all resource retirement. Missing upstream is not evidence of preservation.

Local and remote branches each need their exact tip to be an ancestor of the
fresh remote base. Squash, rebase and cherry-pick equivalence is insufficient.
Remote branch cleanup does not wait for worktree retention; local branches stay
while any worktree uses them. Configured base/default branches and uncertain or
manual resources are protected. No automatic archive tags are created.

External closure has the same rules as factory closure. Closed won't-do,
duplicate or abandoned work can lose its expired clean checkout if commits
remain remotely preserved; its unmerged branch stays. Validation success alone,
a merged PR on an open issue, or closure alone never authorizes branch deletion.
The existing `task clean` command and its explicit `--force` behavior remain
available independently of the conservative automatic policy.

Inspect `$LF_HOME/logs/daemon.log`, or run:

```sh
litefactory logs --all --grep retirement
```

Normal-visible structured records identify sweep, repository/task/issue,
observation time and phase, returned state/reason or lookup failure, exact
path/ref, retention evidence, relevant commit SHAs and actual outcome. Outcomes
separate `would_remove`, `removed`, `kept`, `already_absent`, `failed` and
`unknown`. Ordinary decisions use Info, unavailable verification Warn and
mutation/storage failures Error. Bodies, credentials and file contents are
excluded. Sweep summaries include elapsed duration, outcome counts and deferred
owners, including empty passes. Interrupted or storage-failed passes stay
incomplete. Durable per-resource intents allow retries to reconcile partial or
uncertain removal without treating an old intent as permission to delete a
recreated checkout or advanced branch.
