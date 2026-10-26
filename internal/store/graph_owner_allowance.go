package store

import (
	"context"
	"database/sql"
)

// graphAutomaticDispatchesTx counts the node's dispatches since the instance's
// most recent owner input. A node limit guards against the factory going round
// in circles on its own; an owner reply is the conversation itself, so each
// reply that resumes the instance starts a fresh allowance, counted from the
// continuation it allocated. A claimed reply is about to be allocated to this
// reservation, so nothing counts yet. Without any owner input every dispatch of
// the node counts. resumed reports owner input.
func graphAutomaticDispatchesTx(ctx context.Context, tx *sql.Tx, instanceID int64, node string) (spent int, resumed bool, err error) {
	var claimed bool
	var boundary int64
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_owner_waits WHERE instance_id=? AND state='claimed'),COALESCE((SELECT MAX(continuation_attempt_id) FROM graph_owner_waits WHERE instance_id=? AND state='allocated'),0)`, instanceID, instanceID).Scan(&claimed, &boundary); err != nil {
		return 0, false, err
	}
	if claimed {
		return 0, true, nil
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_node_attempts WHERE instance_id=? AND node=? AND id>=?`, instanceID, node, boundary).Scan(&spent)
	return spent, boundary > 0, err
}
