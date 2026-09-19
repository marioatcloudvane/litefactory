package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrGraphOwnerWait prevents dispatch while an owner question has no admitted reply.
var ErrGraphOwnerWait = errors.New("store: graph awaits an authorized owner reply")

// GraphOwnerWait retains a reply until its continuation attempt is allocated.
// Publication is read from the original report journal, never inferred from state.
type GraphOwnerWait struct {
	DeliveryKey                 string
	InstanceID, SourceAttemptID int64
	State                       string
	ReplyID                     int64
	ReplyBody                   string
	ReplyAuthorID               int64
	ClaimedAt                   time.Time
	ContinuationAttemptID       int64
}

const graphWaitColumns = `delivery_key,instance_id,source_attempt_id,state,COALESCE(reply_id,0),COALESCE(reply_body,''),COALESCE(reply_author_id,0),claimed_at,COALESCE(continuation_attempt_id,0)`

func scanGraphOwnerWait(row scanner) (GraphOwnerWait, error) {
	var out GraphOwnerWait
	var at sql.NullString
	err := row.Scan(&out.DeliveryKey, &out.InstanceID, &out.SourceAttemptID, &out.State, &out.ReplyID, &out.ReplyBody, &out.ReplyAuthorID, &at, &out.ContinuationAttemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.ClaimedAt, err = scanTime(at)
	return out, err
}

// GetGraphOwnerWait reads the latest durable question and reply state for an instance.
func (db *DB) GetGraphOwnerWait(ctx context.Context, instanceID int64) (GraphOwnerWait, error) {
	return scanGraphOwnerWait(db.sql.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE instance_id=? ORDER BY source_attempt_id DESC LIMIT 1`, instanceID))
}

// GraphOwnerReply is constructed by authenticated daemon admission, not agent output.
// OwnerID is the configured numeric identity, SourceRef the observed issue source.
type GraphOwnerReply struct {
	InstanceID, OwnerID, AuthorID, CommentID, QuestionCommentID int64
	TaskRef, SourceRef, Mode, DeliveryKey, Body                 string
	At                                                          time.Time
}

// ClaimGraphOwnerReply authenticates and durably reserves an explicit reply once.
// It neither starts a process nor consumes the claim: allocation does that in the
// same transaction as StartGraphNodeAttempt. Replayed events are refused.
func (db *DB) ClaimGraphOwnerReply(ctx context.Context, in GraphOwnerReply) (out GraphOwnerWait, err error) {
	if in.OwnerID < 1 || in.AuthorID != in.OwnerID || in.CommentID < 1 || in.QuestionCommentID < 1 || in.Mode != "research" || in.TaskRef == "" || in.SourceRef != in.TaskRef || in.DeliveryKey == "" || strings.TrimSpace(in.Body) == "" || len(in.Body) > 65536 || in.At.IsZero() {
		return out, graphInvariant("invalid authenticated Research reply")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		wait, e := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=? AND instance_id=?`, in.DeliveryKey, in.InstanceID))
		if e != nil {
			return e
		}
		if wait.State != "waiting" {
			return ErrGraphOwnerWait
		}
		var task, mode, status, node string
		var question int64
		var completed bool
		e = tx.QueryRowContext(ctx, `SELECT s.task_ref,d.mode,i.status,a.node,r.comment_id,r.completed FROM graph_instances i JOIN sessions s ON s.id=i.session_id JOIN graph_definitions d ON d.id=i.definition_id JOIN graph_node_attempts a ON a.id=? JOIN report_deliveries r ON r.delivery_key=? WHERE i.id=?`, wait.SourceAttemptID, wait.DeliveryKey, in.InstanceID).Scan(&task, &mode, &status, &node, &question, &completed)
		if e != nil {
			return e
		}
		if task != in.TaskRef || mode != in.Mode || status != GraphRunning || !completed || question != in.QuestionCommentID || in.CommentID <= question {
			return graphInvariant("reply does not match published active Research question")
		}
		var running int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_node_attempts WHERE instance_id=? AND status='running'`, in.InstanceID).Scan(&running); e != nil {
			return e
		}
		if running != 0 {
			return ErrGraphAttemptRunning
		}
		_, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='claimed',reply_id=?,reply_body=?,reply_author_id=?,claimed_at=? WHERE delivery_key=?`, in.CommentID, in.Body, in.AuthorID, FormatTime(in.At), in.DeliveryKey)
		if e != nil {
			return e
		}
		if e = setGraphInstanceTx(ctx, tx, in.InstanceID, node, GraphRunning, in.At, time.Time{}); e != nil {
			return e
		}
		out, e = scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE delivery_key=?`, in.DeliveryKey))
		return e
	})
	if err != nil {
		return out, fmt.Errorf("store: claim Research reply: %w", err)
	}
	return out, nil
}

func graphReplyAllocationTx(ctx context.Context, tx *sql.Tx, in GraphAttemptStart, attemptID int64) error {
	wait, err := scanGraphOwnerWait(tx.QueryRowContext(ctx, `SELECT `+graphWaitColumns+` FROM graph_owner_waits WHERE instance_id=? AND state IN ('waiting','claimed')`, in.InstanceID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if wait.State != "claimed" {
		return ErrGraphOwnerWait
	}
	var origin string
	if err = tx.QueryRowContext(ctx, `SELECT node FROM graph_node_attempts WHERE id=?`, wait.SourceAttemptID).Scan(&origin); err != nil {
		return err
	}
	if in.Node != origin || in.RunID < 1 {
		return graphInvariant("reply allocation must resume originating agent with a real run")
	}
	_, err = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='allocated',continuation_attempt_id=? WHERE delivery_key=?`, attemptID, wait.DeliveryKey)
	return err
}
