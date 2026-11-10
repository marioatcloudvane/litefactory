package store

import (
	"context"
)

// ListChatResearchClaims finds notes still owed, even after a claim was allocated.
func (db *DB) ListChatResearchClaims(ctx context.Context, limit int) ([]GraphOwnerWait, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE reply_source='chat' AND reply_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM report_deliveries r WHERE r.delivery_key='chat-owner:'||graph_owner_waits.delivery_key AND r.completed=1) ORDER BY rowid LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []GraphOwnerWait
	for rows.Next() {
		r, e := scanGraphOwnerWait(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
