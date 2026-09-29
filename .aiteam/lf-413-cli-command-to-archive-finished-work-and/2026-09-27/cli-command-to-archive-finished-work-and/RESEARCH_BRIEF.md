# Research brief

## Request

Automatically archive a task's finished database evidence when its GitHub issue closes and the task has settled (no run in progress, nothing pending); automatically delete that archive after a configurable retention window (config.yaml, default 30 days); run one daily job that both archives newly-settled tasks and deletes expired archives.

## Decisions

- **d1** — One daily job archives closed, settled tasks and deletes expired archives.
- **d2** — A task means its whole session family, parent run plus child story runs.
- **d3** — Fully automatic: no manual, restore, dry-run or look-up command.
- **d4** — Only closed issues with no run, dispatch, owner wait, graph instance, delivery or pending effect are selected.
- **d5** — archive_retain_days defaults to 30 and counts from archive time.
- **d6** — Archives are one compressed file per task on disk; after verification the database rows are deleted and the database forgets the task.
- **d7** — Deletion date is read from the archive file name and header, with file modified time only as fallback.
- **d8** — A reopened issue starts fresh; nothing is restored.
- **d9** — Per-issue run logs are removed when their archive expires.
- **d10** — Scope covers all data tied to one issue, not only the three biggest tables.
- **d11** — Owner decision: keep the newest N deploy database backups, N configurable, default 5; delete older ones. Worktree file backups are ignored.
- **d12** — Owner decision: global daemon and launchd logs are deleted after a separate log_retain_days setting, default 30.
- **d13** — Owner decision: the archive job alone may delete plan history rows, after verification, through one narrow exception to the delete block.


## Assumptions

- **a1** — The archive is database evidence for the task, not the git worktree.
- **a2** — The job is its own sweep with its own schedule state, separate from worktree retirement.
- **a3** — Issue #430 could not be read; its database backups are taken to be the deploy script's .bak-pre-deploy- copies, which never get cleaned up today.
- **a5** — The archive's compression format is chosen by the builder; gzip is acceptable.
- **a6** — Old binary copies (.prev-) made by deploys are left alone, as replaceable files.
