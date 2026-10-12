# Release runbook

[Documentation](README.md)

Checks an operator runs before deploying a litefactory build that carries a listed migration. Each entry names the migration, why it needs a manual check instead of code, and the exact check to run.

## Migrations 0079 and 0096: Bug store consolidation

Migration 0079 replaces the 33 old `bug_*` tables with six (`bug_tasks`, `bug_analyses`, `bug_repairs`, `bug_checks`, `bug_delivery`, `bug_events`) and drops the old ones outright in the same transaction. No data is copied forward. Migration 0096 then makes the six new tables read-only, so their rows are kept for reference.

This only matters for a database that has not applied 0079 yet. If your factory's migration table already shows 0079 or later, skip this entry:

```sql
SELECT max(version) FROM schema_migrations;
```

**Before deploying a build onto a database below version 79:**

1. Confirm no Bug task is still running. Pre-0079 Bug tasks are lost when their tables are dropped.

   ```sql
   SELECT count(*) FROM graph_instances i
   JOIN graph_definitions d ON d.id = i.definition_id
   WHERE d.workflow = 'bug' AND i.status = 'running';
   ```

   The count must be `0`. If it is not, wait for the running Bug task(s) to finish, or cancel them with the normal task stop, before you deploy. There is no supported way to recover a Bug task's history once 0079 has dropped its tables.

2. Know what the migration will do with old history. The migration refuses to run, and the new build fails to start, if any of the old `bug_*` tables still holds a row. The error says `preservation refusal: populated <table> would be destroyed by migration 0079; a separately reviewed preservation upgrade is required`. A database with empty Bug tables migrates normally. If you see this refusal, do not remove rows to get past it; the history needs a separately reviewed preservation upgrade first.

Bug tasks started with an older, unsupported Bug definition are not restarted or converted by the upgrade. See [Bug](workflow/bug-runtime.md#bug-history-from-older-versions).

This check has no code equivalent for the in-flight tasks: the migration cannot know whether dropping a running task's tables is acceptable, so the decision stays with the operator deploying the build.
