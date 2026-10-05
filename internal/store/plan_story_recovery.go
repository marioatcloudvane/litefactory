package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// RetainPlanStoryAuthority preserves the authenticated original confirmation.
// It must follow immutable request retention and precede every remote mutation.
func (db *DB) RetainPlanStoryAuthority(ctx context.Context, parent, request, authority string) error {
	if authority == "" || len(authority) > 128*1024 {
		return errors.New("invalid Plan authority")
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO plan_story_authorities(parent,request_id,authority_json) VALUES(?,?,?) ON CONFLICT DO NOTHING`, parent, request, authority); err != nil {
		return err
	}
	prior, err := db.PlanStoryAuthority(ctx, parent, request)
	if err != nil {
		return err
	}
	if prior != authority {
		return errors.New("original Plan authority changed")
	}
	return nil
}

// PlanStoryAuthority reads the immutable authority retained for an original request.
func (db *DB) PlanStoryAuthority(ctx context.Context, parent, request string) (string, error) {
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT authority_json FROM plan_story_authorities WHERE parent=? AND request_id=?`, parent, request).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return raw, err
}

// PlanStoryRecoveryDebt includes historical applied effects missing workflow
// completion, inactive bindings and unbound creations. The durable cursor rotates
// the bounded batch so a permanently unknown result cannot starve later work.
func (db *DB) PlanStoryRecoveryDebt(ctx context.Context, parent string, limit int) ([]PlanStoryCandidate, error) {
	if limit < 1 || limit > 32 {
		return nil, errors.New("invalid Plan recovery budget")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT c.request_id,c.story_key,c.candidate,c.request_json FROM plan_story_candidates c WHERE c.parent=? AND NOT EXISTS(SELECT 1 FROM plan_story_completions x WHERE x.parent=c.parent AND x.request_id=c.request_id) ORDER BY (c.request_id<=COALESCE((SELECT request_id FROM plan_story_recovery_cursors WHERE parent=c.parent),'')), c.request_id LIMIT ?`, parent, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanStoryCandidate
	for rows.Next() {
		c := PlanStoryCandidate{Parent: parent}
		if err = rows.Scan(&c.RequestID, &c.Key, &c.Candidate, &c.RequestJSON); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AdvancePlanStoryRecovery persists the last attempted request for fair rotation.
func (db *DB) AdvancePlanStoryRecovery(ctx context.Context, parent, request string) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO plan_story_recovery_cursors(parent,request_id) VALUES(?,?) ON CONFLICT(parent) DO UPDATE SET request_id=excluded.request_id`, parent, request)
	return err
}

// CompletePlanStoryEffect is the guarded local commit after remote readback.
// Draft, effect, lifecycle, workflow history and completion debt settle together.
func (db *DB) CompletePlanStoryEffect(ctx context.Context, d PlanStoryDraft, expected string, e PlanStoryEffect, state planbrief.PlanState, revision int64, repair bool) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if state.SharedRevision == "" || len(raw) > 4<<20 {
		return errors.New("invalid completed Plan state")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE plan_story_drafts SET revision=?,candidate=?,stage=?,draft_json=?,retired=? WHERE parent=? AND story_key=? AND revision=? AND issue_id=? AND issue_number=?`, d.Revision, d.Candidate, d.Stage, d.DraftJSON, d.Retired, d.Parent, d.Key, expected, d.IssueID, d.IssueNumber)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("plan content changed during completion")
		}
		if revision == 0 {
			result, err = tx.ExecContext(ctx, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,1,?) ON CONFLICT DO NOTHING`, d.Parent, string(raw))
		} else {
			result, err = tx.ExecContext(ctx, `UPDATE plan_workflow_states SET revision=?,state_json=? WHERE parent=? AND revision=?`, revision+1, string(raw), d.Parent, revision)
		}
		if err != nil {
			return err
		}
		n, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrPlanStateConflict
		}
		if err = retainPlanNoticesTx(ctx, tx, d.Parent, state); err != nil {
			return err
		}
		if !repair {
			if _, err = tx.ExecContext(ctx, `INSERT INTO plan_story_effects(parent,request_id,phase,outcome,issue_id,issue_number,detail,revision) VALUES(?,?,?,?,?,?,?,?)`, e.Parent, e.RequestID, e.Phase, e.Outcome, e.IssueID, e.IssueNumber, e.Detail, e.Revision); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO plan_story_completions(parent,request_id) VALUES(?,?) ON CONFLICT DO NOTHING`, d.Parent, e.RequestID)
		return err
	})
}

// ListGraphPlanSessions keeps observation/recovery alive even without new parent
// comments. Existing daemon capacity, cursor and backoff still own admission.
// A parent observed closed is left out until sight records its reopening.
func (db *DB) ListGraphPlanSessions(ctx context.Context) ([]Session, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+prefixedSessionColumns+` FROM sessions JOIN plan_execution_paths p ON p.session_id=sessions.id WHERE p.path='graph' AND EXISTS(SELECT 1 FROM plan_story_drafts d WHERE d.parent=sessions.task_ref) AND sessions.task_ref NOT IN (SELECT parent_task FROM managed_parent_closures) ORDER BY sessions.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PlanStoryCompleted reports whether atomic local workflow completion is recorded.
func (db *DB) PlanStoryCompleted(ctx context.Context, parent, request string) (bool, error) {
	var complete bool
	err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM plan_story_completions WHERE parent=? AND request_id=?)`, parent, request).Scan(&complete)
	return complete, err
}

// SettlePlanStoryDebt marks a known non-mutating obsolete request, or a historical
// applied request already represented by newer content, without rewriting effects.
// Unknown mutation outcomes are never eligible for the obsolete-intent path.
func (db *DB) SettlePlanStoryDebt(ctx context.Context, parent, request string, state planbrief.PlanState, revision int64, applied bool) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var allowed bool
		if applied {
			err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT outcome FROM plan_story_effects WHERE parent=? AND request_id=? ORDER BY id DESC LIMIT 1),'pending')='applied'`, parent, request).Scan(&allowed)
		} else {
			err = tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM plan_story_effects WHERE parent=? AND request_id=? AND phase IN ('create','attach','update','retire') AND outcome='uncertain')`, parent, request).Scan(&allowed)
		}
		if err != nil {
			return err
		}
		if !allowed {
			return errors.New("unknown Plan mutation cannot be superseded")
		}
		result, err := tx.ExecContext(ctx, `UPDATE plan_workflow_states SET revision=?,state_json=? WHERE parent=? AND revision=?`, revision+1, string(raw), parent, revision)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("plan supersession revision changed")
		}
		if err = retainPlanNoticesTx(ctx, tx, parent, state); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO plan_story_completions(parent,request_id) VALUES(?,?) ON CONFLICT DO NOTHING`, parent, request)
		return err
	})
}
