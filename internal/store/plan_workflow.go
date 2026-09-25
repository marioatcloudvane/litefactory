package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// AdmitPlanPath freezes the Plan route for a session on its first admission.
// New sessions get graph Plan, which decomposes the work into a story set and
// returns every node result through the shared MCP result service (#377).
// Sessions already admitted keep their route, including legacy sessions, and an
// admitted route never changes on reply.
func (db *DB) AdmitPlanPath(ctx context.Context, sessionID int64) (string, error) {
	return db.AdmitPlanWorkflowPath(ctx, sessionID, "graph")
}

// GraphPlanStoryParent returns the parent task of a live graph Plan story
// issue, identified by its forge issue id. That parent observes the issue's
// comments as Plan input, so the issue is never a task of its own.
func (db *DB) GraphPlanStoryParent(ctx context.Context, issueID int64) (string, bool, error) {
	if issueID < 1 {
		return "", false, nil
	}
	var parent string
	err := db.sql.QueryRowContext(ctx, `SELECT d.parent FROM plan_story_drafts d JOIN sessions s ON s.task_ref=d.parent JOIN plan_execution_paths p ON p.session_id=s.id WHERE d.issue_id=? AND d.retired=0 AND p.path='graph' LIMIT 1`, issueID).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: graph plan story parent: %w", err)
	}
	return parent, true, nil
}

// AdmitPlanWorkflowPath freezes a factory-selected route on first admission.
// This compatibility seam is not agent input or an upgrade/downgrade operation:
// an existing persisted route always wins, including against a changed default.
func (db *DB) AdmitPlanWorkflowPath(ctx context.Context, sessionID int64, selected string) (string, error) {
	if sessionID < 1 || (selected != "legacy" && selected != "graph") {
		return "", errors.New("invalid plan workflow admission")
	}
	var out string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO plan_execution_paths(session_id,path) VALUES(?,?) ON CONFLICT(session_id) DO NOTHING`, sessionID, selected); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT path FROM plan_execution_paths WHERE session_id=?`, sessionID).Scan(&out)
	})
	return out, err
}

// PlanExecutionPath reads persisted admission without selecting or upgrading it.
func (db *DB) PlanExecutionPath(ctx context.Context, sessionID int64) (string, error) {
	var selected string
	err := db.sql.QueryRowContext(ctx, `SELECT path FROM plan_execution_paths WHERE session_id=?`, sessionID).Scan(&selected)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return selected, err
}

// GetPlanWorkflowState returns the current state and optimistic local revision.
// Missing state is an empty value with revision zero, suitable for first save.
func (db *DB) GetPlanWorkflowState(ctx context.Context, parent string) (planbrief.PlanState, int64, error) {
	var state planbrief.PlanState
	var revision int64
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT revision,state_json FROM plan_workflow_states WHERE parent=?`, parent).Scan(&revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return state, 0, nil
	}
	if err != nil {
		return state, 0, err
	}
	if err = json.Unmarshal([]byte(raw), &state); err != nil {
		return state, 0, fmt.Errorf("decode retained Plan state: %w", err)
	}
	return state, revision, nil
}

// SavePlanWorkflowState journals an immutable snapshot and changes current state
// in one local transaction. No transaction spans remote publication or git save.
func (db *DB) SavePlanWorkflowState(ctx context.Context, parent string, expected int64, state planbrief.PlanState) (int64, error) {
	if parent == "" || expected < 0 || state.SharedRevision == "" {
		return 0, errors.New("plan parent, shared revision and expected revision required")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return 0, err
	}
	if len(raw) > 4<<20 {
		return 0, errors.New("plan state exceeds 4 MiB")
	}
	revision := expected + 1
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var result sql.Result
		var e error
		if expected == 0 {
			result, e = tx.ExecContext(ctx, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,?,?) ON CONFLICT(parent) DO NOTHING`, parent, revision, string(raw))
		} else {
			result, e = tx.ExecContext(ctx, `UPDATE plan_workflow_states SET revision=?,state_json=? WHERE parent=? AND revision=?`, revision, string(raw), parent, expected)
		}
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return errors.New("plan state revision conflict")
		}
		if e = retainPlanNoticesTx(ctx, tx, parent, state); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO plan_workflow_history(parent,revision,state_json) VALUES(?,?,?)`, parent, revision, string(raw))
		return e
	})
	if err != nil {
		return 0, err
	}
	return revision, nil
}

// PlanWorkflowHistory returns an exact immutable restart/reconciliation snapshot.
func (db *DB) PlanWorkflowHistory(ctx context.Context, parent string, revision int64) (planbrief.PlanState, error) {
	var state planbrief.PlanState
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT state_json FROM plan_workflow_history WHERE parent=? AND revision=?`, parent, revision).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return state, ErrNotFound
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal([]byte(raw), &state)
	return state, err
}
