package store

import (
	"context"
	"errors"
	"fmt"
)

// ListGraphNodeAttemptsByRun returns every dispatch one run made, in insertion
// order. A run may span the attempts of a single instance only, so the result
// is what a failure report needs to name the step the run stopped on.
func (db *DB) ListGraphNodeAttemptsByRun(ctx context.Context, runID int64) ([]GraphNodeAttempt, error) {
	if runID < 1 {
		return nil, errors.New("store: a run id is required to list its graph attempts")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE run_id=? ORDER BY id`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: list graph node attempts by run: %w", err)
	}
	defer rows.Close()
	out := make([]GraphNodeAttempt, 0)
	for rows.Next() {
		v, e := scanGraphAttempt(rows)
		if e != nil {
			return nil, fmt.Errorf("store: scan graph node attempt: %w", e)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list graph node attempts by run: %w", err)
	}
	return out, nil
}

// GraphFailureReported reports whether the run's failure comment was published:
// a retained graph comment at the named failure node whose source attempt
// belongs to the run and whose delivery completed. An owner comment may be
// marked handled after a failed run only when this is true, so the owner is
// never left with neither an answer nor an unhandled comment to retry from.
func (db *DB) GraphFailureReported(ctx context.Context, runID int64, node string) (bool, error) {
	if runID < 1 || node == "" {
		return false, errors.New("store: a run and failure node are required")
	}
	var found bool
	err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_comment_intents c JOIN graph_node_attempts a ON a.id=c.source_attempt_id JOIN report_deliveries d ON d.delivery_key=c.delivery_key WHERE a.run_id=? AND c.node=? AND d.completed=1)`, runID, node).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("store: read graph failure report: %w", err)
	}
	return found, nil
}
