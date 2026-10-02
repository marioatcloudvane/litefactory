package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TaskWorkflowAdmission is permanent task routing, independent of trigger journals.
type TaskWorkflowAdmission struct {
	SessionID        int64
	Version          int
	Family, Workflow string
	SourceTrigger    int64
}

func validAdmission(a TaskWorkflowAdmission) bool {
	if a.SessionID <= 0 || a.Version != 1 || a.SourceTrigger < 0 {
		return false
	}
	if a.Workflow == "bug" {
		return a.Family == "standalone"
	}
	switch a.Workflow {
	case "research", "plan", "implement", "validate", "spike":
		return a.Family == "mode"
	}
	return false
}

// GetTaskWorkflowAdmission reads the recorded route without consulting labels.
func (d *DB) GetTaskWorkflowAdmission(ctx context.Context, sessionID int64) (TaskWorkflowAdmission, error) {
	return readTaskWorkflowAdmission(ctx, d.sql, sessionID)
}

func readTaskWorkflowAdmission(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, sessionID int64) (TaskWorkflowAdmission, error) {
	var a TaskWorkflowAdmission
	err := q.QueryRowContext(ctx, `SELECT session_id,version,family,workflow,source_trigger FROM workflow_admissions WHERE session_id=?`, sessionID).Scan(&a.SessionID, &a.Version, &a.Family, &a.Workflow, &a.SourceTrigger)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("read workflow admission: %w", err)
	}
	if !validAdmission(a) {
		return a, errors.New("invalid retained workflow admission")
	}
	return a, nil
}

// AdmitWorkflow atomically retains the winner. Prior execution evidence prevents
// an old mode-managed task from being converted by a newly observed bug label.
func (d *DB) AdmitWorkflow(ctx context.Context, proposed TaskWorkflowAdmission) (TaskWorkflowAdmission, error) {
	if !validAdmission(proposed) {
		return TaskWorkflowAdmission{}, errors.New("invalid proposed workflow admission")
	}
	var result TaskWorkflowAdmission
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var applyErr error
		result, applyErr = admitWorkflowTx(ctx, tx, proposed)
		return applyErr
	})
	return result, err
}

func admitWorkflowTx(ctx context.Context, tx *sql.Tx, proposed TaskWorkflowAdmission) (TaskWorkflowAdmission, error) {
	existing, err := readTaskWorkflowAdmission(ctx, tx, proposed.SessionID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return TaskWorkflowAdmission{}, err
	}
	// A discovered session alone is not evidence of prior admission. Historical
	// records are examined inside the insertion transaction, before the winner is set.
	rows, err := tx.QueryContext(ctx, `SELECT mode FROM runs WHERE session_id=?
 UNION ALL SELECT mode FROM mode_selections WHERE session_id=?
 UNION ALL SELECT CASE WHEN d.workflow<>'' THEN d.workflow ELSE d.mode END FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=?
 UNION ALL SELECT CASE WHEN f.state='planning' THEN 'plan' ELSE 'implement' END FROM managed_features f JOIN sessions s ON s.task_ref=f.parent_task WHERE s.id=?`, proposed.SessionID, proposed.SessionID, proposed.SessionID, proposed.SessionID)
	if err != nil {
		return TaskWorkflowAdmission{}, fmt.Errorf("read prior admission evidence: %w", err)
	}
	prior := ""
	for rows.Next() {
		var mode string
		if err := rows.Scan(&mode); err != nil {
			return TaskWorkflowAdmission{}, errors.Join(err, rows.Close())
		}
		a := proposed
		a.Family = "mode"
		a.Workflow = mode
		if !validAdmission(a) {
			return TaskWorkflowAdmission{}, errors.Join(fmt.Errorf("ambiguous prior workflow evidence %q", mode), rows.Close())
		}
		if prior == "" {
			prior = mode
		}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return TaskWorkflowAdmission{}, err
	}
	if prior != "" && proposed.Family == "standalone" {
		proposed.Family = "mode"
		proposed.Workflow = prior
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workflow_admissions(session_id,version,family,workflow,source_trigger) VALUES(?,?,?,?,?)`, proposed.SessionID, proposed.Version, proposed.Family, proposed.Workflow, proposed.SourceTrigger)
	if err != nil {
		return TaskWorkflowAdmission{}, fmt.Errorf("retain workflow admission: %w", err)
	}
	return proposed, nil
}
