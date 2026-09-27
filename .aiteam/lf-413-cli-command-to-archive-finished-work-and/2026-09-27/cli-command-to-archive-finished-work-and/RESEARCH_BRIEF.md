# Research brief

## Request

Automatically archive a task's finished work when its GitHub issue closes; automatically delete that archive after a configurable number of days (config.yaml, default 30); run one daily job that both moves closed work into the archive and deletes expired archives.

## Decisions

_No decisions have been settled yet._

## Assumptions

- **a1** — "The archive" in the request is assumed to mean the task's database evidence (the rows the original ticket identified as the actual space problem), not the git worktree/branch, which already has its own separate, already-shipped cleanup with its own default retention.
- **a2** — A new archive-retention setting needs its own config.yaml key, separate from the existing worktree-retention key, defaulting to 30 days as stated, since the existing key already ships at a 14-day default for an unrelated resource and must not change behind operators' backs.
- **a3** — Not resolved by the request text: whether the automatic daily job still performs the original ticket's safety steps (verify the archive before deleting source rows, keep a restore path) invisibly, or whether the simpler automatic description intentionally drops them. Left for the next planning step to raise with options, since it changes both risk and scope.
- **a4** — Not resolved by the request text: whether the original ticket's unrelated ask — rotating old deploy-script backups — is still in scope for this piece of work, since the follow-up comment does not mention it.
