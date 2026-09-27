# Research brief

## Request

Automatically archive a task's finished database evidence when its GitHub issue closes and the task has settled (no run in progress, nothing pending); automatically delete that archive after a configurable retention window (config.yaml, default 30 days); run one daily job that both archives newly-settled tasks and deletes expired archives.

## Decisions

- **d1** — The archive job runs once a day, using the same closed-issue detection the existing worktree-retirement sweep already relies on.
- **d2** — "A task" being archived means its whole session family — the parent run plus any child story runs under the same issue — archived together, matching how the existing worktree cleanup already groups ownership.
- **d3** — Archiving is fully automatic: no manual trigger command, no --dry-run flag, no --older-than flag, matching the owner's simplified description.
- **d4** — A task's rows are only ever selected once its issue is closed and it has no active run and no pending effect of any kind, so in-flight work is never archived or deleted.
- **d5** — A new archive_retain_days config.yaml key controls the deletion timer, separate from the existing worktree_retain_days key, defaulting to 30 days as the owner specified, counted from the day a task's rows are archived.
- **d6** — Deletion actually removes archived rows (not just flags them), and the daily job compacts the database afterward so the file itself gets smaller, matching the ticket's original "keep the store slim" goal.


## Assumptions

- **a1** — "The archive" is the task's database evidence rows (the ones the original ticket identified as the actual space problem), not the git worktree/branch, which already has its own separate, already-shipped cleanup with its own unrelated default.
- **a2** — Retention counts from the day a task's rows are archived (effectively the day its issue is found closed, per d1), not from the last completed run like the unrelated worktree timer.
- **a3** — The daily archive/delete job is its own independent sweep, with its own scheduling state, separate from the existing worktree-retirement sweep, since it operates on an entirely different resource and must not share a completion cursor with it.


## Open questions

### q1 — Which tables count as "finished work" to archive in this first version?

_Asked of: Product Owner Questions_

- **a** — Only the three tables that are both the biggest space users and safe to move today: story progress events, submission receipts, submission file snapshots. Together they are the large majority of the measured growth. The permanently-kept Plan history table, the more tangled result tables, and usage-metering history are left out of this pass.  *(recommended)*
- **b** — All six areas the original ticket named, including the table that the database itself is built to never delete and the tables with heavy cross-references to each other — accepting the extra design and risk needed to touch those safely.

### q3 — Should there be any way to look at a task's archived evidence before it is deleted?

_Asked of: Product Owner Questions_

- **a** — No — archived rows are simply held until deletion; nothing else is added.
- **b** — Yes — one small read-only command that shows a task's archived evidence, for support/debugging only. No restore into the live tables, no dry-run flag.  *(recommended)*

### q4 — What happens if a closed, already-archived task's issue gets reopened?

_Asked of: Product Owner Questions_

- **a** — Nothing special: the archived rows stay archived (or get deleted on schedule if their time is up); any new work on the reopened issue simply creates fresh rows and does not depend on the old ones.  *(recommended)*
- **b** — Reopening automatically restores the task's archived rows back into the live tables before new work can start.

### q5 — Is the deploy-script backup rotation from the original ticket still wanted as part of this same piece of work?

_Asked of: Product Owner Questions_

- **a** — No — it is a separate, disk-level backup problem, not a database one; raise it as its own issue if still wanted.  *(recommended)*
- **b** — Yes — bundle "keep the last N deploy backups" into this same delivery.

### q2 — Where does archived data physically live?

_Asked of: Technical Questions_

- **a** — Inside the same database, in separate archive tables. The busy/live tables shrink the moment a task is archived; the file itself shrinks later, once retention expires, rows are deleted, and the database is compacted. No new file format, no separate restore tooling.  *(recommended)*
- **b** — A compressed file per task on disk, as the original ticket described, which needs its own verify-before-delete and restore machinery to be safe.
