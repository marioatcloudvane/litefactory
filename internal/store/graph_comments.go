package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GraphCommentIntent retains authored content independently of rendering and
// delivery. Its source attempt and operation key survive report-only recovery.
type GraphCommentIntent struct {
	Key                         string
	InstanceID, SourceAttemptID int64
	Node, Kind                  string
	Candidate                   []byte
}

// RetainGraphComment saves exact authored bytes and the original publication identity.
func (db *DB) RetainGraphComment(ctx context.Context, in GraphCommentIntent) error {
	if in.Key == "" || in.InstanceID <= 0 || in.SourceAttemptID <= 0 || in.Node == "" || len(in.Candidate) == 0 || len(in.Candidate) > 1<<20 || !GraphCommentKind(in.Kind) {
		return errors.New("store: invalid graph comment intent")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var instance int64
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT instance_id,status FROM graph_node_attempts WHERE id=?`, in.SourceAttemptID).Scan(&instance, &status); err != nil {
			return err
		}
		if instance != in.InstanceID || ((in.Kind == "failure" && status != GraphFailed) || (in.Kind != "failure" && status != GraphSucceeded)) {
			return errors.New("store: graph comment source mismatch")
		}
		var old GraphCommentIntent
		err := tx.QueryRowContext(ctx, `SELECT delivery_key,instance_id,source_attempt_id,node,kind,candidate FROM graph_comment_intents WHERE delivery_key=?`, in.Key).Scan(&old.Key, &old.InstanceID, &old.SourceAttemptID, &old.Node, &old.Kind, &old.Candidate)
		if err == nil {
			if old.InstanceID != in.InstanceID || old.SourceAttemptID != in.SourceAttemptID || old.Node != in.Node || old.Kind != in.Kind || !bytes.Equal(old.Candidate, in.Candidate) {
				return errors.New("store: graph comment correction requires a new identity")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO graph_comment_intents(delivery_key,instance_id,source_attempt_id,node,kind,candidate) VALUES(?,?,?,?,?,?)`, in.Key, in.InstanceID, in.SourceAttemptID, in.Node, in.Kind, in.Candidate)
		if err != nil {
			return err
		}
		// A review waits like a question does: the agent has shown finished
		// work and the stage does not advance until the owner answers.
		if GraphCommentWaits(in.Kind) {
			if _, err = tx.ExecContext(ctx, `INSERT INTO graph_owner_waits(delivery_key,instance_id,source_attempt_id,state) VALUES(?,?,?,'waiting')`, in.Key, in.InstanceID, in.SourceAttemptID); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=NULL WHERE id=?`, in.InstanceID)
		}
		return err
	})
}

// GetGraphComment reads the retained candidate for one durable publication identity.
func (db *DB) GetGraphComment(ctx context.Context, key string) (GraphCommentIntent, error) {
	var out GraphCommentIntent
	err := db.sql.QueryRowContext(ctx, `SELECT delivery_key,instance_id,source_attempt_id,node,kind,candidate FROM graph_comment_intents WHERE delivery_key=?`, key).Scan(&out.Key, &out.InstanceID, &out.SourceAttemptID, &out.Node, &out.Kind, &out.Candidate)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, fmt.Errorf("store: read graph comment: %w", err)
	}
	return out, nil
}

// graphReportDeliveryTx prevents graph publication from finalizing a whole run
// or consuming owner input. Only a retained, exactly bound graph intent qualifies.
func graphReportDeliveryTx(ctx context.Context, tx *sql.Tx, d ReportDelivery) (bool, error) {
	var session int64
	var run sql.NullInt64
	var workflow, status, kind string
	err := tx.QueryRowContext(ctx, `SELECT i.session_id,a.run_id,COALESCE(g.workflow,''),a.status,c.kind FROM graph_comment_intents c JOIN graph_instances i ON i.id=c.instance_id JOIN graph_definitions g ON g.id=i.definition_id JOIN graph_node_attempts a ON a.id=c.source_attempt_id AND a.instance_id=i.id WHERE c.delivery_key=?`, d.Key).Scan(&session, &run, &workflow, &status, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Bug factory activities have no subprocess run. Their actual succeeded
	// or failed source attempt remains the publication authority; never invent
	// a run or extend this exception to existing agent-backed workflows.
	if !run.Valid && (workflow != "bug" || d.RunID != 0 || (kind == "failure" && status != GraphFailed) || (kind != "failure" && status != GraphSucceeded)) {
		return false, errors.New("store: runless graph report requires a terminal Bug factory source")
	}
	if session != d.SessionID || run.Int64 != d.RunID {
		return false, errors.New("store: graph report run/session mismatch")
	}
	return true, nil
}

// ListGraphComments pages operation metadata without allocating candidate bodies.
// Empty afterKey starts an instance's history. Delivery receipts are read by Key;
// a missing report delivery means the retained candidate still needs rendering.
func (db *DB) ListGraphComments(ctx context.Context, instanceID int64, afterKey string, limit int) ([]GraphCommentIntent, error) {
	if instanceID <= 0 || limit < 1 || limit > 100 {
		return nil, errors.New("store: invalid graph comment page")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT delivery_key,instance_id,source_attempt_id,node,kind FROM graph_comment_intents WHERE instance_id=? AND delivery_key>? ORDER BY delivery_key LIMIT ?`, instanceID, afterKey, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list graph comments: %w", err)
	}
	defer rows.Close()
	var out []GraphCommentIntent
	for rows.Next() {
		var item GraphCommentIntent
		if err := rows.Scan(&item.Key, &item.InstanceID, &item.SourceAttemptID, &item.Node, &item.Kind); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// GraphCommentKind reports whether a value names a publication this package
// retains. "question" asks the owner something; "review" shows them finished
// work and asks whether it stands; "final" reports a settled outcome and
// "failure" an attempt that produced none.
func GraphCommentKind(kind string) bool {
	switch kind {
	case "question", "review", "final", "failure":
		return true
	}
	return false
}

// GraphCommentWaits reports whether publishing this kind leaves the instance
// waiting on the owner rather than carrying on.
func GraphCommentWaits(kind string) bool { return kind == "question" || kind == "review" }
