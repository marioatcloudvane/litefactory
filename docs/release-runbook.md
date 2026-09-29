# Release runbook

Pre-ship checks an operator runs before deploying a litefactory build that
carries a listed migration. Each entry names the migration, why it needs a
manual check instead of code, and the exact check to run.

## Migration 0079 — bug store consolidation

Migration 0079 replaces the 33 dedicated `bug_*` tables with six
(`bug_tasks`, `bug_analyses`, `bug_repairs`, `bug_checks`, `bug_delivery`,
`bug_events`), dropping the 33 retired tables outright in the same transaction
that creates the six new ones. This is a clean cutover: no data is copied
forward and no history is preserved for any bug task that is still running
when the migration applies.

**Before deploying a build that includes migration 0079**, confirm no bug
task is currently in flight: no `bug`-workflow graph instance is in a
non-terminal state (`running` or `pending`) on the database this build will
migrate.

```sql
SELECT count(*) FROM graph_instances i
JOIN graph_definitions d ON d.id = i.definition_id
WHERE d.workflow = 'bug' AND i.status NOT IN ('succeeded', 'failed');
```

The count must be `0`. If it is not, wait for the in-flight bug task(s) to
reach a terminal state (or cancel them through the normal `/cancel-bug`
control) before deploying this build. Bug runs are short and rare in
practice, so waiting is normally the right answer; there is no supported way
to recover a bug task's history once migration 0079 has dropped its tables.

This check has no code equivalent: the migration itself has no way to know
whether dropping a currently-running task's tables is acceptable, so the
decision stays with the operator deploying the build.
