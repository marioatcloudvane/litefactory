package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ValidationAssessment retains the exact input and validated advisory output.
// It is not itself an acceptance or remote effect; those require decision replay.
type ValidationAssessment struct {
	RunID       int64
	Parent      string
	Generation  int
	ContextJSON string
	ResultJSON  string
}

// RetainValidationAssessment persists the attributed frozen input and complete result.
func (db *DB) RetainValidationAssessment(ctx context.Context, a ValidationAssessment) error {
	if a.RunID <= 0 || a.Parent == "" || a.Generation <= 0 || a.ContextJSON == "" || a.ResultJSON == "" {
		return fmt.Errorf("store: incomplete validation assessment")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var old ValidationAssessment
		err := tx.QueryRowContext(ctx, `SELECT parent_task,generation,context_json,result_json FROM validation_assessments WHERE run_id=?`, a.RunID).Scan(&old.Parent, &old.Generation, &old.ContextJSON, &old.ResultJSON)
		if err == nil {
			if old.Parent == a.Parent && old.Generation == a.Generation && old.ContextJSON == a.ContextJSON && old.ResultJSON == a.ResultJSON {
				return nil
			}
			return fmt.Errorf("store: conflicting validation assessment replay")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO validation_assessments(run_id,parent_task,generation,context_json,result_json) VALUES(?,?,?,?,?)`, a.RunID, a.Parent, a.Generation, a.ContextJSON, a.ResultJSON)
		return err
	})
}

// GetValidationAssessment loads the immutable context and result of a validation run.
func (db *DB) GetValidationAssessment(ctx context.Context, runID int64) (ValidationAssessment, error) {
	a := ValidationAssessment{RunID: runID}
	err := db.sql.QueryRowContext(ctx, `SELECT parent_task,generation,context_json,result_json FROM validation_assessments WHERE run_id=?`, runID).Scan(&a.Parent, &a.Generation, &a.ContextJSON, &a.ResultJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}
