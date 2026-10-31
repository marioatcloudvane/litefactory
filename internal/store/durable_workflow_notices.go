package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// WorkflowNotice retains a deduplicated event and its independent delivery outcome.
type WorkflowNotice struct {
	ID                                                 int64
	JobID                                              string
	Revision                                           int64
	Event, Destination, State, Body, MessageID, Reason string
	RetryAt, CreatedAt                                 time.Time
}

const workflowNoticeColumns = `id,job_id,revision,event,destination,state,body,message_id,reason,retry_at,created_at`

func scanWorkflowNotice(row scanner) (WorkflowNotice, error) {
	var n WorkflowNotice
	var retry, created string
	err := row.Scan(&n.ID, &n.JobID, &n.Revision, &n.Event, &n.Destination, &n.State, &n.Body, &n.MessageID, &n.Reason, &retry, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	if err != nil {
		return n, err
	}
	n.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err == nil && retry != "" {
		n.RetryAt, err = time.Parse(time.RFC3339Nano, retry)
	}
	return n, err
}
func insertWorkflowNoticeTx(ctx context.Context, tx *sql.Tx, n WorkflowNotice) error {
	if n.Destination == "" {
		n.State = "unavailable"
	} else if n.State == "" {
		n.State = "pending"
	}
	if n.State != "pending" && n.State != "unavailable" {
		return errors.New("invalid initial workflow notice")
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO durable_workflow_notices(job_id,revision,event,destination,state,body,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(job_id,revision,event,destination) DO NOTHING`, n.JobID, n.Revision, n.Event, n.Destination, n.State, n.Body, ownerActionTime(n.CreatedAt))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 || n.State == "unavailable" {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	return insertChatNotices(ctx, tx, "discord", []ChatNotice{{ChatReply: ChatReply{Transport: "discord", MessageID: "workflow-notice:" + strconv.FormatInt(id, 10), ChannelRef: n.Destination, Command: "workflow", ReplyText: n.Body, CreatedAt: n.CreatedAt}, TaskRef: n.JobID}})
}

// WorkflowNotices returns a job's retained notice events in creation order.
func (db *DB) WorkflowNotices(ctx context.Context, job string) ([]WorkflowNotice, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+workflowNoticeColumns+` FROM durable_workflow_notices WHERE job_id=? ORDER BY id`, job)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkflowNotice{}
	for rows.Next() {
		n, e := scanWorkflowNotice(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ClaimWorkflowNotice persists sending before the external call; sending is never reclaimed.
func (db *DB) ClaimWorkflowNotice(ctx context.Context, at time.Time) (WorkflowNotice, error) {
	var n WorkflowNotice
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		n, err = scanWorkflowNotice(tx.QueryRowContext(ctx, `SELECT `+workflowNoticeColumns+` FROM durable_workflow_notices WHERE state='pending' AND (retry_at='' OR retry_at<=?) ORDER BY id LIMIT 1`, ownerActionTime(at)))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE durable_workflow_notices SET state='sending' WHERE id=? AND state='pending'`, n.ID)
		n.State = "sending"
		return err
	})
	return n, err
}

// FinishWorkflowNotice accepts positive receipts, confirmed refusals, or an ambiguous failure.
func (db *DB) FinishWorkflowNotice(ctx context.Context, id int64, state, message, reason string, retryAt time.Time) error {
	if state != "delivered" && state != "failed" && state != "pending" {
		return errors.New("invalid workflow notice outcome")
	}
	if state == "delivered" && message == "" {
		return errors.New("missing workflow delivery receipt")
	}
	if state == "pending" && retryAt.IsZero() {
		return errors.New("missing workflow retry deadline")
	}
	r, err := db.sql.ExecContext(ctx, `UPDATE durable_workflow_notices SET state=?,message_id=?,reason=?,retry_at=? WHERE id=? AND state='sending'`, state, message, reason, ownerActionTime(retryAt), id)
	return chatAffected(r, err)
}

// ReconcileWorkflowNotices makes lost replies visible without a blind resend.
func (db *DB) ReconcileWorkflowNotices(ctx context.Context) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE durable_workflow_notices SET state='failed',reason='delivery_unconfirmed' WHERE state='sending'`)
	return err
}

// ReconcileWorkflowNoticeReceipt records a read-only provider receipt for an ambiguous send.
func (db *DB) ReconcileWorkflowNoticeReceipt(ctx context.Context, id int64, message string) error {
	if message == "" {
		return errors.New("missing workflow delivery receipt")
	}
	r, err := db.sql.ExecContext(ctx, `UPDATE durable_workflow_notices SET state='delivered',message_id=?,reason='' WHERE id=? AND state IN ('sending','failed')`, message, id)
	return chatAffected(r, err)
}

// FinishWorkflowChatNotice settles the shared outbox claim and retained workflow receipt together.
func (db *DB) FinishWorkflowChatNotice(ctx context.Context, r ChatReply, state, message, reason string, next, at time.Time) error {
	if state != "delivered" && state != "failed" && state != "pending" {
		return errors.New("invalid workflow notice outcome")
	}
	if state == "delivered" && message == "" {
		return errors.New("missing workflow notice provider receipt")
	}
	if state == "pending" && !next.After(at) {
		return errors.New("missing workflow notice retry deadline")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		chatState := "failed"
		if state == "delivered" {
			chatState = "confirmed"
		}
		if state == "pending" {
			chatState = "pending"
		}
		result, err := tx.ExecContext(ctx, `UPDATE chat_notices SET state=?,next_attempt_at=?,finished_at=?,reply_text=CASE WHEN ?='pending' THEN reply_text ELSE '' END WHERE transport=? AND message_id=? AND state='sent' AND attempts=?`, chatState, FormatTime(next), FormatTime(at), state, r.Transport, r.MessageID, r.Attempts)
		if err = chatAffected(result, err); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `UPDATE durable_workflow_notices SET state=?,message_id=?,reason=?,retry_at=? WHERE 'workflow-notice:'||id=? AND state='sending'`, state, message, reason, ownerActionTime(next), r.MessageID)
		return chatAffected(result, err)
	})
}

// ReadWorkflowNotice reads retained delivery facts for an authorization recheck.
func (db *DB) ReadWorkflowNotice(ctx context.Context, id int64) (WorkflowNotice, error) {
	return scanWorkflowNotice(db.sql.QueryRowContext(ctx, `SELECT `+workflowNoticeColumns+` FROM durable_workflow_notices WHERE id=?`, id))
}

// WorkflowRecentNotices reads at most limit recent notices in chronological order.
func (db *DB) WorkflowRecentNotices(ctx context.Context, job string, limit int) ([]WorkflowNotice, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("invalid workflow notice history limit")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+workflowNoticeColumns+` FROM (SELECT * FROM durable_workflow_notices WHERE job_id=? ORDER BY id DESC LIMIT ?) ORDER BY id`, job, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkflowNotice{}
	for rows.Next() {
		n, e := scanWorkflowNotice(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
