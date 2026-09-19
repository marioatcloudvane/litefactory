package store

import (
	"context"
	"fmt"
)

// LatestGraphNodeAttempt returns the last attempt, including failed or running
// attempts. Omitting those would let a control default hide an invalid result.
// ErrNotFound means the node has never been attempted in this instance.
func (db *DB) LatestGraphNodeAttempt(ctx context.Context, instanceID int64, node string) (GraphNodeAttempt, error) {
	out, err := scanGraphAttempt(db.sql.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE instance_id=? AND node=? ORDER BY id DESC LIMIT 1`, instanceID, node))
	if err != nil {
		return GraphNodeAttempt{}, fmt.Errorf("store: latest graph node attempt: %w", err)
	}
	return out, nil
}
