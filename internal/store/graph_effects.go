package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// GraphEffect is the shared projection of immutable effect intent and retained
// observations. Dispatched claims never expire into permission to repeat I/O.
type GraphEffect struct {
	Operation                                   string
	InstanceID, SourceAttemptID, ClaimAttemptID int64
	Kind                                        string
	Binding                                     json.RawMessage
	State                                       string
	Observation                                 json.RawMessage
	CreatedAt, UpdatedAt                        time.Time
}

// RetainGraphEffect saves immutable effect intent before external mutation.
func (db *DB) RetainGraphEffect(ctx context.Context, in GraphEffect) (out GraphEffect, err error) {
	if in.Operation == "" || len(in.Operation) > 256 || in.InstanceID < 1 || in.SourceAttemptID < 1 || in.Kind == "" || !json.Valid(in.Binding) || len(in.Binding) > 1<<20 || in.CreatedAt.IsZero() {
		return out, graphInvariant("invalid shared effect intent")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		a, e := scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, in.SourceAttemptID))
		if e != nil {
			return e
		}
		if a.InstanceID != in.InstanceID {
			return graphInvariant("effect source belongs to another instance")
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO graph_effects(operation,instance_id,source_attempt_id,kind,binding,state,created_at,updated_at) VALUES(?,?,?,?,?,'intent',?,?) ON CONFLICT(operation) DO NOTHING`, in.Operation, in.InstanceID, in.SourceAttemptID, in.Kind, []byte(in.Binding), FormatTime(in.CreatedAt), FormatTime(in.CreatedAt))
		if e != nil {
			return e
		}
		out, e = scanGraphEffect(tx.QueryRowContext(ctx, `SELECT `+graphEffectColumns+` FROM graph_effects WHERE operation=?`, in.Operation))
		if e != nil {
			return e
		}
		if out.InstanceID != in.InstanceID || out.SourceAttemptID != in.SourceAttemptID || out.Kind != in.Kind || !bytes.Equal(out.Binding, in.Binding) {
			return graphInvariant("immutable effect binding differs")
		}
		return nil
	})
	return out, err
}

const graphEffectColumns = `operation,instance_id,source_attempt_id,kind,binding,state,claim_attempt_id,observation,created_at,updated_at`

func scanGraphEffect(row scanner) (v GraphEffect, err error) {
	var claim sql.NullInt64
	var created, updated string
	err = row.Scan(&v.Operation, &v.InstanceID, &v.SourceAttemptID, &v.Kind, &v.Binding, &v.State, &claim, &v.Observation, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.ClaimAttemptID = claim.Int64
	v.CreatedAt, err = ParseTime(created)
	if err != nil {
		return v, err
	}
	v.UpdatedAt, err = ParseTime(updated)
	return v, err
}

// GetGraphEffect reads the retained binding, claim and observation state.
func (db *DB) GetGraphEffect(ctx context.Context, operation string) (GraphEffect, error) {
	return scanGraphEffect(db.sql.QueryRowContext(ctx, `SELECT `+graphEffectColumns+` FROM graph_effects WHERE operation=?`, operation))
}

// ClaimGraphEffect authorizes only a never dispatched intent. Recovery must
// observe uncertain effects; a new attempt cannot steal a prior claim.
func (db *DB) ClaimGraphEffect(ctx context.Context, operation string, attempt int64, at time.Time) (out GraphEffect, err error) {
	if operation == "" || attempt < 1 || at.IsZero() {
		return out, graphInvariant("invalid effect claim identity or time")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		a, e := scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, attempt))
		if e != nil {
			return e
		}
		if a.Status != GraphRunning {
			return graphInvariant("effect claim requires active allocation")
		}
		result, e := tx.ExecContext(ctx, `UPDATE graph_effects SET state='dispatched',claim_attempt_id=?,updated_at=? WHERE operation=? AND instance_id=? AND state='intent'`, attempt, FormatTime(at), operation, a.InstanceID)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return graphInvariant("effect already claimed or unresolved")
		}
		out, e = scanGraphEffect(tx.QueryRowContext(ctx, `SELECT `+graphEffectColumns+` FROM graph_effects WHERE operation=?`, operation))
		return e
	})
	return out, err
}

// ObserveGraphEffect atomically appends history and advances the projection.
// intent requires an explicit nonexecution observation from the coordinator.
func (db *DB) ObserveGraphEffect(ctx context.Context, operation string, attempt int64, state string, observation json.RawMessage, at time.Time) error {
	if !json.Valid(observation) || len(observation) > 1<<20 || at.IsZero() {
		return graphInvariant("invalid effect observation")
	}
	switch state {
	case "intent", "confirmed", "uncertain", "refused":
	default:
		return graphInvariant("invalid effect state")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		effect, e := scanGraphEffect(tx.QueryRowContext(ctx, `SELECT `+graphEffectColumns+` FROM graph_effects WHERE operation=?`, operation))
		if e != nil {
			return e
		}
		a, e := scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, attempt))
		if e != nil {
			return e
		}
		if a.InstanceID != effect.InstanceID || a.Status != GraphRunning {
			return graphInvariant("effect observation allocation mismatch")
		}
		if effect.State == "confirmed" && state != "confirmed" {
			return graphInvariant("confirmed effect is immutable")
		}
		raw, e := json.Marshal(struct {
			Record, Operation, State string
			Observation              json.RawMessage
		}{"effect_observation", operation, state, observation})
		if e != nil {
			return e
		}
		if _, e = recordGraphOutputTx(ctx, tx, GraphOutput{NodeAttemptID: attempt, Kind: GraphOutputJSON, Payload: raw, CreatedAt: at}); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE graph_effects SET state=?,observation=?,updated_at=? WHERE operation=?`, state, []byte(observation), FormatTime(at), operation)
		if e != nil {
			return fmt.Errorf("effect observation: %w", e)
		}
		return nil
	})
}

// ListPinnedGraphInstances restricts restart recovery to the supported exact
// profile; old executions remain untouched even when they owe work.
func (db *DB) ListPinnedGraphInstances(ctx context.Context, digest string) ([]GraphInstance, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE definition_id IN (SELECT id FROM graph_definitions WHERE workflow='bug' AND digest=?) AND status IN ('running','succeeded') ORDER BY id`, digest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GraphInstance
	for rows.Next() {
		v, e := scanGraphInstance(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListGraphEffects retains generations together so changed evidence cannot
// hide a previously dispatched merge or manufacture another delivery attempt.
func (db *DB) ListGraphEffects(ctx context.Context, instance int64) ([]GraphEffect, error) {
	rows, e := db.sql.QueryContext(ctx, `SELECT `+graphEffectColumns+` FROM graph_effects WHERE instance_id=? ORDER BY created_at,operation`, instance)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []GraphEffect
	for rows.Next() {
		v, e := scanGraphEffect(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
