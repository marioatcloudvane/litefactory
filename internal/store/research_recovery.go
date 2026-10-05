package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// RecoverResearchDispatch links an unmapped parked invocation to the shipped
// start. Pending comments and unanswered waits remain on their original identity.
func (db *DB) RecoverResearchDispatch(ctx context.Context, instanceID int64, in ResearchLaunch) (out ResearchAdmission, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		prior, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, instanceID))
		if e != nil {
			return e
		}
		var mode string
		if e = tx.QueryRowContext(ctx, `SELECT mode FROM graph_definitions WHERE id=?`, in.DefinitionID).Scan(&mode); e != nil {
			return e
		}
		if mode != "research" || prior.SessionID != in.SessionID || prior.Status != GraphRunning || in.Start == "" || !IsGraphTrigger(in.TriggerID) || len(in.Files) == 0 || len(in.Files) > 16<<20 {
			return graphInvariant("invalid Research graph recovery")
		}
		var pending int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_node_attempts WHERE instance_id=? AND status IN ('pending','running')`, prior.ID).Scan(&pending); e != nil {
			return e
		}
		if pending > 0 {
			return ErrGraphAttemptRunning
		}
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_owner_waits WHERE instance_id=? AND state IN ('waiting','claimed')`, prior.ID).Scan(&pending); e != nil {
			return e
		}
		if pending > 0 {
			return ErrGraphOwnerWait
		}
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_comment_intents c LEFT JOIN report_deliveries d ON d.delivery_key=c.delivery_key WHERE c.instance_id=? AND (d.delivery_key IS NULL OR d.completed=0 OR d.comment_id IS NULL)`, prior.ID).Scan(&pending); e != nil {
			return e
		}
		if pending > 0 {
			return ErrWorkflowEffectsPending
		}
		out.PredecessorID = prior.ID
		out.Instance, e = scanGraphInstance(tx.QueryRowContext(ctx, `INSERT INTO graph_instances(session_id,definition_id,status,current_node,started_at) VALUES(?,?,'running',?,?) RETURNING `+graphInstanceColumns, prior.SessionID, in.DefinitionID, in.Start, FormatTime(nowUTC())))
		if e != nil {
			return e
		}
		var reply string
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT reply_body FROM graph_owner_waits WHERE instance_id=? AND state='allocated' ORDER BY rowid DESC LIMIT 1),'')`, prior.ID).Scan(&reply); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO research_successors(instance_id,predecessor_id,trigger_id,reply_body) VALUES(?,?,?,?)`, out.Instance.ID, prior.ID, in.TriggerID, reply); e != nil {
			return e
		}
		if e = tx.QueryRowContext(ctx, `INSERT INTO research_launch_definitions(session_id,trigger_id,instance_id,content_digest,topology_digest,files) VALUES(?,?,?,?,?,?) RETURNING id`, in.SessionID, in.TriggerID, out.Instance.ID, in.ContentDigest, in.TopologyDigest, in.Files).Scan(&out.LaunchID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET instance_id=?,node=? WHERE id=? AND instance_id=? AND status IN ('reserved','running')`, out.Instance.ID, in.Start, in.TriggerID-graphTriggerBase, prior.ID); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE graph_instances SET status='cancelled',next_due_at=NULL,finished_at=? WHERE id=?`, FormatTime(nowUTC()), prior.ID)
		return e
	})
	if err != nil {
		return ResearchAdmission{}, fmt.Errorf("store: recover Research dispatch: %w", err)
	}
	return out, nil
}

// SettleResearchReview consumes only the authenticated current review reply and
// moves to its explicit graph approval edge, without allocating an agent attempt.
func (db *DB) SettleResearchReview(ctx context.Context, instanceID int64, key, target string, at time.Time) error {
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
		if mode != "research" || status != GraphRunning || kind != "review" || source != wait.SourceAttemptID || !completed || wait.State != "claimed" || wait.ReplyID <= comment || wait.ReplyAuthorID < 1 || strings.ToLower(strings.TrimSpace(wait.ReplyBody)) != "done" || target == "" || at.IsZero() {
			return graphInvariant("Research review is not authenticated current approval")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated' WHERE delivery_key=?`, key); e != nil {
			return e
		}
		return setGraphInstanceTx(ctx, tx, instanceID, target, GraphRunning, at, time.Time{})
	})
}
