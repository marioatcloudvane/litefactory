package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ResearchLaunchFiles reads bounded historical execution context, not authority
// to select future graph edges.
func (db *DB) ResearchLaunchFiles(ctx context.Context, instanceID int64) ([]byte, error) {
	var raw []byte
	err := db.sql.QueryRowContext(ctx, `SELECT files FROM research_launch_definitions WHERE instance_id=? AND length(files)<=16777216 ORDER BY id DESC LIMIT 1`, instanceID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return raw, err
}

// IsApprovalReply reports whether an owner reply approves a review whose
// approval token is token. It lowercases the reply, collapses whitespace and
// strips trailing '.', '!' and '?'. The only accepted spelling beyond the token
// itself is "ship it" for the "ship" token; no other text is interpreted.
func IsApprovalReply(body, token string) bool {
	if token == "" {
		return false
	}
	reply := strings.TrimRight(strings.Join(strings.Fields(strings.ToLower(body)), " "), ".!?")
	return reply == token || (token == "ship" && reply == "ship it")
}

// SettleGraphReview consumes a claimed, published review for an admitted exact reply.
// The caller resolves the token and target from the retained graph, never agent text.
func (db *DB) SettleGraphReview(ctx context.Context, instanceID int64, key, target, workflow, reply string, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		wait, e := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE instance_id=? AND delivery_key=?`, instanceID, key))
		if e != nil {
			return e
		}
		var mode, status, kind string
		var source, comment int64
		var completed bool
		e = tx.QueryRowContext(ctx, `SELECT d.mode,i.status,c.kind,c.source_attempt_id,r.comment_id,r.completed FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id JOIN graph_comment_intents c ON c.instance_id=i.id AND c.delivery_key=? JOIN report_deliveries r ON r.delivery_key=c.delivery_key WHERE i.id=?`, key, instanceID).Scan(&mode, &status, &kind, &source, &comment, &completed)
		if e != nil {
			return e
		}
		if mode != workflow || reply == "" || status != GraphRunning || kind != "review" || source != wait.SourceAttemptID || !completed || wait.State != "claimed" || wait.ReplyID <= comment || wait.ReplyAuthorID < 1 || !IsApprovalReply(wait.ReplyBody, reply) || target == "" || at.IsZero() {
			return graphInvariant("graph review is not authenticated current approval")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated' WHERE delivery_key=?`, key); e != nil {
			return e
		}
		return setGraphInstanceTx(ctx, tx, instanceID, target, GraphRunning, at, time.Time{})
	})
}

// ApprovedGraphReview recovers a consumed review independently of later retry waits.
func (db *DB) ApprovedGraphReview(ctx context.Context, instance, source int64) (GraphOwnerWait, error) {
	return scanGraphOwnerWait(db.sql.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE instance_id=? AND source_attempt_id=? AND state='allocated' AND delivery_key IN (SELECT delivery_key FROM graph_comment_intents WHERE instance_id=? AND kind='review') ORDER BY rowid DESC LIMIT 1`, instance, source, instance))
}
