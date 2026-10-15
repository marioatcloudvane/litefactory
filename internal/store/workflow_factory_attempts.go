package store

import (
	"context"
	"fmt"
)

// ListWorkflowFactoryAttempts pages unsettled factory activities in the current
// Research or Plan node. It grants no dispatch authority: the caller must read
// the captured profile and decide whether the reserved activity is recoverable.
// Doctor-held sessions remain owned by their repair coordinator.
func (db *DB) ListWorkflowFactoryAttempts(ctx context.Context, afterID int64, limit int) ([]GraphNodeAttempt, error) {
	if afterID < 0 || limit < 1 || limit > 100 {
		return nil, graphInvariant("invalid workflow factory attempt page")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT a.id,a.instance_id,a.node,a.attempt,a.run_id,a.status,a.outcome,a.started_at,a.finished_at,a.launch_id FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE a.id>? AND a.run_id IS NULL AND a.status IN ('pending','running') AND i.status='running' AND i.current_node=a.node AND d.mode IN ('research','plan') AND NOT EXISTS(SELECT 1 FROM doctor_held_session_owners h WHERE h.session_id=i.session_id) ORDER BY a.id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list workflow factory attempts: %w", err)
	}
	defer rows.Close()
	out := make([]GraphNodeAttempt, 0)
	for rows.Next() {
		attempt, e := scanGraphAttempt(rows)
		if e != nil {
			return nil, fmt.Errorf("store: scan workflow factory attempt: %w", e)
		}
		out = append(out, attempt)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list workflow factory attempts: %w", err)
	}
	return out, nil
}
