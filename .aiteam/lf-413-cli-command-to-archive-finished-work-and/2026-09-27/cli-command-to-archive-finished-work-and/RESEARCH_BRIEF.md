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


## Assumptions

- **a1** — The archive is database evidence for the task, not the git worktree.
- **a2** — The job is its own sweep with its own schedule state, separate from worktree retirement.
- **a3** — Issue #430 could not be read; its temp files and backups are assumed to be files on disk, handled per the open question.
- **a5** — The archive's compression format is chosen by the builder; gzip is acceptable.


## Open questions

### q6 — Which backup and temp files should be cleaned up, and by what rule?

_Asked of: Product Owner Questions_

- **a** — Delete deploy database backups and #430 temp files after the same `archive_retain_days`, but always keep the newest 5 backups. Same daily job.  *(recommended)*
- **b** — Keep only the newest N backups, no age rule; delete #430 temp files right after use.
- **c** — Tell me what #430 creates, and I will decide.

### q7 — How should global logs that belong to no issue be trimmed?

_Asked of: Product Owner Questions_

- **a** — Delete daemon and launchd log files older than a separate `log_retain_days` setting, default 30, by rotating them daily.  *(recommended)*
- **b** — Only delete per-issue run logs with the archive; leave global logs alone.

### q8 — Plan history tables refuse deletes by design; may the archive job remove them?

_Asked of: Technical Questions_

- **a** — Yes: only the archive job may delete them, after the archive is verified, through one narrow, tested exception to the delete block.  *(recommended)*
- **b** — No: leave those tables in the database and archive only the rest.
