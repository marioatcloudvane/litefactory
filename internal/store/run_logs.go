package store

import (
	"context"
	"errors"
	"strings"
	"time"
)

// PriorRunScope is launcher-owned authority. Empty repositories deny all access.
type PriorRunScope struct {
	RepositoryIDs []int64
	TaskRefs      []string
	RunIDs        []int64
}

// PriorRunQuery freezes discovery and narrows the launcher's authority.
type PriorRunQuery struct {
	Scope         PriorRunScope
	Cutoff        time.Time
	HighWater     int64
	BeforeID      int64
	TaskRef       string
	StartedAfter  time.Time
	StartedBefore time.Time
	Status        RunStatus
	Limit         int
}

// PriorRun includes only the owning task identity in addition to the stored run.
type PriorRun struct {
	Run
	RepoID  int64
	TaskRef string
}

func priorRunWhere(q PriorRunQuery) (string, []any) {
	where := "r.status IN ('succeeded','failed','reported') AND r.finished_at IS NOT NULL AND r.finished_at <= ? AND r.id <= ?"
	args := []any{FormatTime(q.Cutoff), q.HighWater}
	addIDs := func(column string, ids []int64) {
		if len(ids) == 0 {
			where += " AND 0"
			return
		}
		where += " AND " + column + " IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	addIDs("s.repo_id", q.Scope.RepositoryIDs)
	if len(q.Scope.RunIDs) > 0 {
		addIDs("r.id", q.Scope.RunIDs)
	}
	if len(q.Scope.TaskRefs) > 0 {
		where += " AND s.task_ref IN (" + strings.TrimSuffix(strings.Repeat("?,", len(q.Scope.TaskRefs)), ",") + ")"
		for _, ref := range q.Scope.TaskRefs {
			args = append(args, ref)
		}
	}
	if q.BeforeID > 0 {
		where += " AND r.id < ?"
		args = append(args, q.BeforeID)
	}
	if q.TaskRef != "" {
		where += " AND s.task_ref = ?"
		args = append(args, q.TaskRef)
	}
	if !q.StartedAfter.IsZero() {
		where += " AND r.started_at >= ?"
		args = append(args, FormatTime(q.StartedAfter))
	}
	if !q.StartedBefore.IsZero() {
		where += " AND r.started_at <= ?"
		args = append(args, FormatTime(q.StartedBefore))
	}
	if q.Status != "" {
		where += " AND r.status = ?"
		args = append(args, string(q.Status))
	}
	return where, args
}

// PriorRunCutoff captures a bounded discovery watermark without returning rows.
func (db *DB) PriorRunCutoff(ctx context.Context, scope PriorRunScope, before time.Time) (int64, error) {
	q := PriorRunQuery{Scope: scope, Cutoff: before, HighWater: 1<<63 - 1}
	where, args := priorRunWhere(q)
	var high int64
	err := db.sql.QueryRowContext(ctx, "SELECT COALESCE(MAX(r.id),0) FROM runs r JOIN sessions s ON s.id=r.session_id WHERE "+where, args...).Scan(&high)
	return high, err
}

// ListPriorRuns applies authority before LIMIT and returns newest IDs first.
func (db *DB) ListPriorRuns(ctx context.Context, q PriorRunQuery) ([]PriorRun, error) {
	if q.Limit < 1 || q.Limit > 26 || q.Cutoff.IsZero() {
		return nil, errors.New("store: invalid prior run query")
	}
	where, args := priorRunWhere(q)
	args = append(args, q.Limit)
	// Select the full run with the established scanner, then resolve the tiny
	// owner projection after closing rows: the store has one connection.
	columns := strings.Split(strings.ReplaceAll(runColumns, "\n", ""), ",")
	for i := range columns {
		columns[i] = "r." + strings.TrimSpace(columns[i])
	}
	// #nosec G202 -- columns and predicates contain only fixed SQL fragments; all selectors are bound parameters.
	rows, err := db.sql.QueryContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM runs r JOIN sessions s ON s.id=r.session_id WHERE "+where+" ORDER BY r.id DESC LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	out := make([]PriorRun, 0, q.Limit)
	for rows.Next() {
		run, e := scanRun(rows)
		if e != nil {
			_ = rows.Close()
			return nil, e
		}
		out = append(out, PriorRun{Run: run})
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	for i := range out {
		if err := db.sql.QueryRowContext(ctx, "SELECT repo_id,task_ref FROM sessions WHERE id=?", out[i].SessionID).Scan(&out[i].RepoID, &out[i].TaskRef); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// GetPriorRun uses the same frozen authorization predicates as discovery.
func (db *DB) GetPriorRun(ctx context.Context, q PriorRunQuery, id int64) (PriorRun, error) {
	q.Limit = 1
	where, args := priorRunWhere(q)
	args = append(args, id)
	columns := strings.Split(strings.ReplaceAll(runColumns, "\n", ""), ",")
	for i := range columns {
		columns[i] = "r." + strings.TrimSpace(columns[i])
	}
	run, err := scanRun(db.sql.QueryRowContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM runs r JOIN sessions s ON s.id=r.session_id WHERE "+where+" AND r.id=?", args...))
	if err != nil {
		return PriorRun{}, err
	}
	out := PriorRun{Run: run}
	err = db.sql.QueryRowContext(ctx, "SELECT repo_id,task_ref FROM sessions WHERE id=?", run.SessionID).Scan(&out.RepoID, &out.TaskRef)
	return out, err
}
