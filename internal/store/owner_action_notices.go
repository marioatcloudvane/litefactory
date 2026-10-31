package store

import (
	"context"
	"database/sql"
)

// OwnerActionNoticesDue returns owner outcomes not yet queued for Discord.
func (db *DB) OwnerActionNoticesDue(ctx context.Context) ([]OwnerAction, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals p WHERE approval_channel<>'' AND state IN ('succeeded','failed','uncertain') AND NOT EXISTS(SELECT 1 FROM owner_action_notice_receipts n WHERE n.proposal_id=p.id AND n.state=p.state) AND NOT EXISTS(SELECT 1 FROM durable_workflow_requests w WHERE w.proposal_id=p.id) ORDER BY created_at,id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OwnerAction
	for rows.Next() {
		p, e := scanOwnerAction(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// QueueOwnerActionOutcome atomically saves the notice and a durable deduplication
// receipt that outlives ordinary chat cleanup. A reconciled outcome gets a new notice.
func (db *DB) QueueOwnerActionOutcome(ctx context.Context, id, state string, notices []ChatNotice) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var current string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM owner_action_proposals WHERE id=?`, id).Scan(&current); err != nil {
			return err
		}
		if current != state {
			return ErrNotFound
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO owner_action_notice_receipts(proposal_id,state) VALUES(?,?) ON CONFLICT DO NOTHING`, id, state)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count == 0 {
			return err
		}
		return insertChatNotices(ctx, tx, "discord", notices)
	})
}
