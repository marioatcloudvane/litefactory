package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// GraphStop is factory-owned stop intent, bound to the pinned graph origin.
// Candidate contains immutable reporting data, not agent-authored authority.
// A zero SourceAttemptID records refusal before dispatch without inventing a run.
type GraphStop struct {
	Key                         string
	InstanceID, SourceAttemptID int64
	Origin, Cause, Reason       string
	Candidate                   []byte
	At                          time.Time
	AllowRunless                bool
	Completion                  *GraphCompletion
}

// ParkGraphNode atomically records completion, retained history and a question
// at the existing origin. It clears automatic dispatch; publication is separate.
func (db *DB) ParkGraphNode(ctx context.Context, in GraphStop) error {
	if in.Key == "" || in.InstanceID <= 0 || in.SourceAttemptID < 0 || in.Origin == "" || in.Cause == "" || in.Reason == "" || len(in.Reason) > 65536 || len(in.Candidate) == 0 || len(in.Candidate) > 1<<20 || in.At.IsZero() || (in.SourceAttemptID == 0) != (in.Completion == nil) {
		return graphInvariant("invalid graph stop intent")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		instance, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, in.InstanceID))
		if e != nil {
			return e
		}
		if instance.Status != GraphRunning || instance.CurrentNode != in.Origin {
			return graphInvariant("stop no longer owns running origin")
		}
		var workflow string
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(d.workflow,''),d.mode) FROM graph_definitions d WHERE d.id=?`, instance.DefinitionID).Scan(&workflow); e != nil {
			return e
		}
		if workflow == "bug" {
			var admitted bool
			if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workflow_admissions WHERE session_id=? AND family='standalone' AND workflow='bug')`, instance.SessionID).Scan(&admitted); e != nil {
				return e
			}
			if !admitted {
				return graphInvariant("owner parking requires admitted standalone Bug workflow")
			}
		}
		if workflow == "" {
			return graphInvariant("owner parking requires admitted workflow")
		}
		var previousSource int64
		var previousOrigin, previousCause, previousReason string
		var previousBytes []byte
		e = tx.QueryRowContext(ctx, `SELECT COALESCE(source_attempt_id,0),stop_origin,stop_cause,stop_reason,candidate FROM graph_comment_intents WHERE delivery_key=? AND instance_id=?`, in.Key, in.InstanceID).Scan(&previousSource, &previousOrigin, &previousCause, &previousReason, &previousBytes)
		if e == nil {
			if previousSource != in.SourceAttemptID || previousOrigin != in.Origin || previousCause != in.Cause || previousReason != in.Reason || !bytes.Equal(previousBytes, in.Candidate) {
				return graphInvariant("stop correction requires new identity")
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if in.Completion != nil {
			c := *in.Completion
			if c.Transition != nil || c.InstanceStatus != GraphRunning || !c.FinishedAt.Equal(in.At) {
				return graphInvariant("parked completion cannot advance or terminate instance")
			}
			source, e := finishGraphAttemptTx(ctx, tx, in.SourceAttemptID, c.Status, c.Outcome, c.FinishedAt)
			if e != nil {
				return e
			}
			if source.InstanceID != in.InstanceID || source.Node != in.Origin || (source.RunID == 0 && !in.AllowRunless) {
				return graphInvariant("stop source mismatch")
			}
			for _, output := range c.Outputs {
				if output.NodeAttemptID != 0 && output.NodeAttemptID != source.ID {
					return graphInvariant("stop output source mismatch")
				}
				output.NodeAttemptID = source.ID
				if output.CreatedAt.IsZero() {
					output.CreatedAt = in.At
				}
				if _, e = recordGraphOutputTx(ctx, tx, output); e != nil {
					return e
				}
			}
		} else {
			var running int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_node_attempts WHERE instance_id=? AND status='running'`, in.InstanceID).Scan(&running); e != nil {
				return e
			}
			if running != 0 {
				return ErrGraphAttemptRunning
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO graph_comment_intents(delivery_key,instance_id,source_attempt_id,node,kind,candidate,stop_origin,stop_cause,stop_reason,created_at) VALUES(?,?,?,?,'question',?,?,?,?,?)`, in.Key, in.InstanceID, nullID(in.SourceAttemptID), in.Origin, in.Candidate, in.Origin, in.Cause, in.Reason, FormatTime(in.At)); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO graph_owner_waits(delivery_key,instance_id,source_attempt_id,state,origin_node,allow_runless) VALUES(?,?,?,'waiting',?,?)`, in.Key, in.InstanceID, nullID(in.SourceAttemptID), in.Origin, in.AllowRunless); e != nil {
			return e
		}
		return setGraphInstanceTx(ctx, tx, in.InstanceID, in.Origin, GraphRunning, time.Time{}, time.Time{})
	})
	if err != nil {
		return fmt.Errorf("store: park graph origin: %w", err)
	}
	return nil
}
