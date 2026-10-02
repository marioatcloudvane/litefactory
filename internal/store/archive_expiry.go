package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ArchivedRun names one run's on-disk artefacts: the issue whose runs folder
// holds them (a child story's own issue for a child run) and the run ordinal.
type ArchivedRun struct {
	Issue   int
	Ordinal int
}

// ListArchivedRunArtefacts returns, read only, the (issue, ordinal) pair of
// every run in the task family that finished at or before archiveDate. The
// family is the owner session with the given task ref plus every session whose
// issue is one of its managed child stories. Sessions and runs are skeleton
// tables the archive never deletes, so the family can be matched after the
// archive. found is false when no session carries the task ref, in which case
// nothing can be matched to the archive. Runs that have not finished are never
// returned.
func (db *DB) ListArchivedRunArtefacts(ctx context.Context, taskRef string, archiveDate time.Time) (runs []ArchivedRun, found bool, err error) {
	var exists bool
	if err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE task_ref=:task)`, sql.Named("task", taskRef)).Scan(&exists); err != nil {
		return nil, false, fmt.Errorf("read archived family: %w", err)
	}
	if !exists {
		return nil, false, nil
	}
	rows, err := db.sql.QueryContext(ctx, `
WITH owner(repo_id) AS (SELECT repo_id FROM sessions WHERE task_ref=:task),
fs(id, issue_number) AS (
  SELECT id, issue_number FROM sessions
  WHERE task_ref=:task
     OR (repo_id IN (SELECT repo_id FROM owner)
         AND issue_number IN (SELECT issue_number FROM managed_stories WHERE parent_task=:task)))
SELECT fs.issue_number, r.ordinal, r.finished_at
FROM runs r JOIN fs ON fs.id=r.session_id
WHERE r.finished_at IS NOT NULL
ORDER BY fs.issue_number, r.ordinal`, sql.Named("task", taskRef))
	if err != nil {
		return nil, true, fmt.Errorf("read archived family runs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var run ArchivedRun
		var finished string
		if err := rows.Scan(&run.Issue, &run.Ordinal, &finished); err != nil {
			return nil, true, fmt.Errorf("read archived family run: %w", err)
		}
		at, err := ParseTime(finished)
		if err != nil {
			return nil, true, fmt.Errorf("run %d/%d finished_at: %w", run.Issue, run.Ordinal, err)
		}
		if !at.After(archiveDate) {
			runs = append(runs, run)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, true, fmt.Errorf("read archived family runs: %w", err)
	}
	return runs, true, nil
}
