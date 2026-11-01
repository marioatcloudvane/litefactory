package store

import (
	"context"
	"time"
)

// DelayDiscussionReconciliation paces read-only receipt lookup without making a
// possibly sent conclusion eligible for another POST.
func (db *DB) DelayDiscussionReconciliation(ctx context.Context, s DiscussionScope, id, state string, due, at time.Time) error {
	if (state != "sending" && state != "uncertain") || !due.After(at) {
		return ErrConnectorInvalid
	}
	args := []any{ownerActionTime(due), ownerActionTime(at)}
	args = append(args, discussionScopeArgs(s)...)
	args = append(args, id, state)
	r, err := db.sql.ExecContext(ctx, `UPDATE discussion_conclusions SET next_attempt_at=?,updated_at=? WHERE `+discussionScopeWhere+` AND id=? AND state=?`, args...)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrWorkflowRevision
	}
	return err
}
