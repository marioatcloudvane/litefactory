package store

import (
	"context"
	"database/sql"
)

// graphStepsSinceOwnerTx counts the instance's node dispatches since its most
// recent accepted owner reply. The step budget guards against the factory going
// round in circles on its own; an owner reply is the conversation itself, so
// each reply that resumes the instance starts a fresh budget, counted from the
// continuation it allocated. A claimed reply is about to be allocated to this
// reservation, so nothing counts yet. Without any owner reply every dispatch of
// the instance counts.
func graphStepsSinceOwnerTx(ctx context.Context, tx *sql.Tx, instanceID int64) (spent int, err error) {
	var claimed bool
	var boundary int64
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_owner_waits WHERE instance_id=? AND state='claimed'),COALESCE((SELECT MAX(continuation_attempt_id) FROM graph_owner_waits WHERE instance_id=? AND state='allocated'),0)`, instanceID, instanceID).Scan(&claimed, &boundary); err != nil {
		return 0, err
	}
	if claimed {
		return 0, nil
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_node_attempts WHERE instance_id=? AND id>=?`, instanceID, boundary).Scan(&spent)
	return spent, err
}
