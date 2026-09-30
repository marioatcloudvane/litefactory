package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrSpikeLegacyRetired refuses Spike work whose recorded history belongs to
// the retired single-agent route, or whose provenance cannot be told apart from
// it. The history stays exactly as recorded; only its execution is refused.
var ErrSpikeLegacyRetired = errors.New("this session's Spike history belongs to the retired single-agent route or its provenance is ambiguous; it cannot continue here, so start a fresh graph Spike task with a new /spike request instead")

// AdmitSpikePath admits Spike work only on the graph. It reads the session's
// recorded provenance and never rewrites it:
//
//   - graph history, or no Spike history at all, is admitted (a fresh session
//     records the graph path once);
//   - a recorded legacy path, legacy-only Spike runs, or any record that
//     contradicts the history is refused with [ErrSpikeLegacyRetired].
//
// Existing rows and history tables are never modified except for the first
// graph record of a session that has no route yet.
func (db *DB) AdmitSpikePath(ctx context.Context, sessionID int64) error {
	if sessionID < 1 {
		return graphInvariant("invalid workflow admission")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var graph, legacyRuns bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=? AND d.mode='spike')`, sessionID).Scan(&graph); e != nil {
			return e
		}
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE session_id=? AND mode='spike')`, sessionID).Scan(&legacyRuns); e != nil {
			return e
		}
		var recorded string
		e := tx.QueryRowContext(ctx, `SELECT path FROM research_execution_paths WHERE session_id=?`, sessionID).Scan(&recorded)
		switch {
		case e == nil:
			if recorded != "graph" {
				return ErrSpikeLegacyRetired
			}
			if !graph && legacyRuns {
				return ErrSpikeLegacyRetired
			}
			return nil
		case e != sql.ErrNoRows:
			return e
		}
		if !graph && legacyRuns {
			return ErrSpikeLegacyRetired
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO research_execution_paths(session_id,path) VALUES(?,'graph') ON CONFLICT(session_id) DO NOTHING`, sessionID); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store: admit spike path: %w", err)
	}
	return nil
}
