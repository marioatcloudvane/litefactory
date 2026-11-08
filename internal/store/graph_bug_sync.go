package store

import (
	"context"
	"database/sql"
	"time"
)

// RedirectGraphBugFixerToSync routes a pending fixer through its factory-resolved
// prerequisite. It keeps the original authenticated claim and all attempt counts.
func (db *DB) RedirectGraphBugFixerToSync(ctx context.Context, instanceID int64, gate BugSyncPrerequisite, at time.Time) error {
	if instanceID < 1 || gate.SyncNode == "" || gate.FixerNode == "" || gate.SyncNode == gate.FixerNode || at.IsZero() {
		return graphInvariant("invalid Bug sync prerequisite")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.id=? AND i.status='running' AND i.current_node=? AND d.workflow='bug' AND d.profile_version=2 AND NOT EXISTS(SELECT 1 FROM graph_node_attempts a WHERE a.instance_id=i.id AND a.status='running') AND NOT EXISTS(SELECT 1 FROM graph_owner_waits w WHERE w.instance_id=i.id AND w.state='waiting'))`, instanceID, gate.FixerNode).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return graphInvariant("Bug fixer no longer owns prerequisite reroute")
		}
		if _, err := recordGraphTransitionTx(ctx, tx, GraphTransition{InstanceID: instanceID, FromNode: gate.FixerNode, ToNode: gate.SyncNode, OnOutcome: "sync_required", At: at}); err != nil {
			return err
		}
		return setGraphInstanceTx(ctx, tx, instanceID, gate.SyncNode, GraphRunning, at, time.Time{})
	})
}
