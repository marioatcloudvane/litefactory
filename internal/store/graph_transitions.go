package store

import (
	"context"
	"database/sql"
	"fmt"
)

const graphTransitionColumns = `id,instance_id,from_node,to_node,on_outcome,at`

// RecordGraphTransition appends a trace without changing existing rows. Normal
// execution uses CompleteGraphNodeAttempt to couple trace and progress writes.
func (db *DB) RecordGraphTransition(ctx context.Context, in GraphTransition) (out GraphTransition, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error { var e error; out, e = recordGraphTransitionTx(ctx, tx, in); return e })
	if err != nil {
		return GraphTransition{}, fmt.Errorf("store: record graph transition: %w", err)
	}
	return out, nil
}

func recordGraphTransitionTx(ctx context.Context, tx *sql.Tx, in GraphTransition) (GraphTransition, error) {
	if in.InstanceID <= 0 || in.FromNode == "" || in.ToNode == "" || in.OnOutcome == "" || in.At.IsZero() {
		return GraphTransition{}, graphInvariant("invalid graph transition identity, nodes, outcome or time")
	}
	return scanGraphTransition(tx.QueryRowContext(ctx, `INSERT INTO graph_transitions(instance_id,from_node,to_node,on_outcome,at) VALUES(?,?,?,?,?) RETURNING `+graphTransitionColumns, in.InstanceID, in.FromNode, in.ToNode, in.OnOutcome, FormatTime(in.At)))
}

// ListGraphTransitions returns an instance's entire append-only trace in order.
func (db *DB) ListGraphTransitions(ctx context.Context, instanceID int64) ([]GraphTransition, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphTransitionColumns+` FROM graph_transitions WHERE instance_id=? ORDER BY id`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("store: list graph transitions: %w", err)
	}
	defer rows.Close()
	out := make([]GraphTransition, 0)
	for rows.Next() {
		v, e := scanGraphTransition(rows)
		if e != nil {
			return nil, fmt.Errorf("store: scan graph transition: %w", e)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list graph transitions: %w", err)
	}
	return out, nil
}

func scanGraphTransition(row scanner) (GraphTransition, error) {
	var v GraphTransition
	var at string
	if err := row.Scan(&v.ID, &v.InstanceID, &v.FromNode, &v.ToNode, &v.OnOutcome, &at); err != nil {
		return v, err
	}
	var err error
	v.At, err = ParseTime(at)
	return v, err
}
