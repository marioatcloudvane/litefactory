package store

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
)

const graphOutputColumns = `id,node_attempt_id,kind,payload,owner_comment_id,created_at`

// RecordGraphOutput appends opaque bytes. CompleteGraphNodeAttempt is preferred
// when this output belongs to the same atomic operation as node completion.
func (db *DB) RecordGraphOutput(ctx context.Context, in GraphOutput) (out GraphOutput, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error { var e error; out, e = recordGraphOutputTx(ctx, tx, in); return e })
	if err != nil {
		return GraphOutput{}, fmt.Errorf("store: record graph output: %w", err)
	}
	return out, nil
}

func recordGraphOutputTx(ctx context.Context, tx *sql.Tx, in GraphOutput) (GraphOutput, error) {
	if in.NodeAttemptID <= 0 || in.OwnerCommentID < 0 || in.CreatedAt.IsZero() || (in.Kind != GraphOutputText && in.Kind != GraphOutputJSON && in.Kind != GraphOutputDiagnostic) {
		return GraphOutput{}, graphInvariant("invalid graph output kind, identity or creation time")
	}
	payload := bytes.Clone(in.Payload)
	if payload == nil {
		payload = []byte{}
	}
	return scanGraphOutput(tx.QueryRowContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,owner_comment_id,created_at) VALUES(?,?,?,?,?) RETURNING `+graphOutputColumns, in.NodeAttemptID, in.Kind, payload, nullID(in.OwnerCommentID), FormatTime(in.CreatedAt)))
}

// ListGraphOutputs returns an attempt's outputs in insertion order.
func (db *DB) ListGraphOutputs(ctx context.Context, attemptID int64) ([]GraphOutput, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphOutputColumns+` FROM graph_outputs WHERE node_attempt_id=? ORDER BY id`, attemptID)
	if err != nil {
		return nil, fmt.Errorf("store: list graph outputs: %w", err)
	}
	defer rows.Close()
	out := make([]GraphOutput, 0)
	for rows.Next() {
		v, e := scanGraphOutput(rows)
		if e != nil {
			return nil, fmt.Errorf("store: scan graph output: %w", e)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list graph outputs: %w", err)
	}
	return out, nil
}

func scanGraphOutput(row scanner) (GraphOutput, error) {
	var v GraphOutput
	var owner sql.NullInt64
	var created string
	if err := row.Scan(&v.ID, &v.NodeAttemptID, &v.Kind, &v.Payload, &owner, &created); err != nil {
		return v, err
	}
	v.OwnerCommentID = scanID(owner)
	v.Payload = bytes.Clone(v.Payload)
	var err error
	v.CreatedAt, err = ParseTime(created)
	return v, err
}
