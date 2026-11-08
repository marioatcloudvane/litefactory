package store

import (
	"context"
	"errors"
)

// ErrTaskNotStopped means a task still has work in flight: a running run, or a
// pending or running graph attempt outside the Doctor's own workflow.
var ErrTaskNotStopped = errors.New("store: the task still has work in flight")

// CheckTaskStopped is the Doctor's only admission rule: a repair starts only on
// a task that is stopped. The Doctor's own graph attempts do not count, so an
// owner reply to a waiting Doctor is never refused by its own instance.
func (db *DB) CheckTaskStopped(ctx context.Context, sessionID int64) error {
	var running int
	if err := db.sql.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM runs WHERE session_id=? AND status='running')
		+ (SELECT count(*) FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id
		   WHERE i.session_id=? AND a.status IN ('pending','running') AND d.mode<>'doctor')`, sessionID, sessionID).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return ErrTaskNotStopped
	}
	return nil
}

// WorkflowLaunched reports whether a workflow launch was admitted for this
// trigger on the session.
func (db *DB) WorkflowLaunched(ctx context.Context, sessionID, triggerID int64) (bool, error) {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM research_launch_definitions WHERE session_id=? AND trigger_id=?`, sessionID, triggerID).Scan(&n)
	return n > 0, err
}
