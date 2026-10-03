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
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(d.workflow,''),d.mode) FROM graph_definitions d JOIN workflow_admissions a ON a.session_id=? WHERE d.id=? AND a.family='standalone'`, instance.SessionID, instance.DefinitionID).Scan(&workflow); e != nil {
			return e
		}
		if workflow != "bug" {
			return graphInvariant("owner parking requires admitted standalone Bug workflow")
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

// graphClaimAllowsAttemptTx releases exactly one bounded allocation from a
// consumed owner claim. It preserves accumulated attempt counters.
func graphClaimAllowsAttemptTx(ctx context.Context, tx *sql.Tx, in GraphAttemptStart) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_owner_waits WHERE instance_id=? AND state='claimed' AND origin_node=? AND (allow_runless=1 OR ?>0)`, in.InstanceID, in.Node, in.RunID).Scan(&count)
	if err != nil {
		return false, err
	}
	if count == 1 {
		return true, nil
	}
	if in.OwnerAllowanceSource > 0 {
		return graphMechanicalAllowanceTx(ctx, tx, in)
	}
	return false, nil
}

// graphMechanicalAllowanceTx accepts only the latest successful runless owner
// continuation and its retained immediate transition. It never chooses a route.
func graphMechanicalAllowanceTx(ctx context.Context, tx *sql.Tx, in GraphAttemptStart) (bool, error) {
	if in.OwnerAllowanceSource < 1 || in.RunID < 1 {
		return false, nil
	}
	var allowed bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_owner_waits w JOIN graph_node_attempts a ON a.id=w.continuation_attempt_id JOIN graph_instances i ON i.id=w.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE w.instance_id=? AND w.state='allocated' AND w.allow_runless=1 AND w.allowance_attempt_id IS NULL AND a.id=? AND a.run_id IS NULL AND a.status='succeeded' AND i.current_node=? AND d.workflow='bug' AND d.profile_version=2 AND a.id=(SELECT MAX(id) FROM graph_node_attempts WHERE instance_id=i.id) AND EXISTS(SELECT 1 FROM graph_transitions t WHERE t.instance_id=i.id AND t.from_node=a.node AND t.to_node=? AND t.on_outcome=a.outcome AND t.at=a.finished_at))`, in.InstanceID, in.OwnerAllowanceSource, in.Node, in.Node).Scan(&allowed)
	return allowed, e
}
