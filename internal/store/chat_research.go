package store

import (
	"context"
	"database/sql"
	"time"
)

// Chat replies occupy their own band below graph dispatch identities. Their
// ordinal is the wait row, so repeated deliveries cannot mint another answer.
const chatTriggerBase int64 = 1 << 59

// IsChatTrigger distinguishes owner answers received through chat from comments.
func IsChatTrigger(id int64) bool { return id > chatTriggerBase && id < graphTriggerBase }

// ClaimChatResearchReply uses the same publication, ownership and running-attempt
// checks as forge replies, with an additional Research-question restriction.
// The caller must hold the task lock, freshly observe an open issue and redact
// the bounded body. Claim and continuation intent commit together.
func (db *DB) ClaimChatResearchReply(ctx context.Context, in GraphOwnerReply) (GraphOwnerWait, error) {
	in.CommentID = chatTriggerBase + 1
	in.Workflow = "research"
	return db.claimGraphOwnerReply(ctx, in, true)
}

func validateChatResearchWaitTx(ctx context.Context, tx *sql.Tx, wait GraphOwnerWait) error {
	var latest bool
	var kind string
	var questions, asks int
	err := tx.QueryRowContext(ctx, `SELECT
 i.id=(SELECT MAX(g.id) FROM graph_instances g WHERE g.session_id=i.session_id),
 c.kind,
 CASE WHEN json_valid(CAST(c.candidate AS TEXT)) THEN COALESCE(json_array_length(CAST(c.candidate AS TEXT),'$.questions'),0) ELSE 0 END,
 (SELECT COUNT(*) FROM graph_outputs o WHERE o.node_attempt_id=w.source_attempt_id AND o.kind='json'
  AND CASE WHEN json_valid(CAST(o.payload AS TEXT)) THEN json_extract(CAST(o.payload AS TEXT),'$.response')='ask' ELSE 0 END)
 FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id
 JOIN graph_comment_intents c ON c.delivery_key=w.delivery_key
 WHERE w.delivery_key=? AND w.instance_id=?`, wait.DeliveryKey, wait.InstanceID).Scan(&latest, &kind, &questions, &asks)
	if err != nil {
		return err
	}
	if !latest || kind != "question" || questions < 1 || asks != 1 {
		return graphInvariant("chat reply requires the current Research ask")
	}
	return nil
}

func parkChatResearchReplyTx(ctx context.Context, tx *sql.Tx, instanceID int64, node string, at time.Time) error {
	// Capture exactly the saved launch that ordinary graph parking would use.
	// No watermark is touched: the private reply is never a forge comment.
	_, err := tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at,launch_id)
 SELECT i.id,i.session_id,?,?, 'pending',?,
 (SELECT id FROM research_launch_definitions WHERE instance_id=i.id ORDER BY id DESC LIMIT 1)
 FROM graph_instances i WHERE i.id=?
 AND NOT EXISTS(SELECT 1 FROM graph_dispatches d WHERE d.instance_id=i.id AND d.status IN ('pending','reserved','running'))`, node, FormatTime(at), FormatTime(at), instanceID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=? WHERE id=?`, FormatTime(at), instanceID)
	return err
}
