package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// GraphResultReviewBinding resolves historical review access from a retained
// graph source, never from authored candidate data. Exact native node contexts
// may have several transport attempts; their latest attributed binding owns the
// completed source. Unsupported specialized contexts are never guessed.
func (db *DB) GraphResultReviewBinding(ctx context.Context, source GraphNodeAttempt) (ResultBinding, error) {
	var binding ResultBinding
	if source.RunID == 0 {
		return binding, ErrNotFound
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		retained, err := scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, source.ID))
		if err != nil {
			return err
		}
		if retained.InstanceID != source.InstanceID || retained.RunID != source.RunID || retained.Node != source.Node {
			return ErrResultAuthority
		}
		var task, mode, workflow, definitionMode, definitionWorkflow string
		err = tx.QueryRowContext(ctx, `SELECT s.task_ref,r.mode,r.workflow,d.mode,d.workflow FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id JOIN sessions s ON s.id=i.session_id JOIN runs r ON r.session_id=s.id WHERE i.id=? AND r.id=?`, source.InstanceID, source.RunID).Scan(&task, &mode, &workflow, &definitionMode, &definitionWorkflow)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrResultAuthority
		}
		if err != nil {
			return err
		}
		if workflow == "bug" {
			if definitionWorkflow != "bug" || mode != "" {
				return ErrResultAuthority
			}
		} else if workflow != "" || definitionWorkflow == "bug" || mode != definitionMode {
			return ErrResultAuthority
		}
		suffix := ""
		alternateSuffix := ""
		if workflow == "bug" {
			suffix = fmt.Sprintf("#bug:bug-analyst:analysis:%d:%d", source.InstanceID, source.ID)
			alternateSuffix = fmt.Sprintf("#bug:bug-fixer:repair:%d:%d", source.InstanceID, source.ID)
		} else {
			switch mode {
			case "plan", "implement":
				suffix = fmt.Sprintf("#graph-%d#attempt-%d", source.InstanceID, source.ID)
			case "research", "validate", "spike":
				var latest int64
				if err = tx.QueryRowContext(ctx, `SELECT MAX(id) FROM graph_node_attempts WHERE instance_id=? AND run_id=? AND node=?`, source.InstanceID, source.RunID, source.Node).Scan(&latest); err != nil {
					return err
				}
				if latest != source.ID {
					return ErrNotFound
				}
				suffix = fmt.Sprintf("#graph-%d#%s", source.InstanceID, source.Node)
			}
		}
		if suffix == "" {
			return ErrNotFound
		}
		rows, err := tx.QueryContext(ctx, `SELECT a.attempt,r.work_context FROM application_result_attempts a JOIN application_results r ON r.id=a.result_id WHERE COALESCE(a.run_id,0)=? AND r.task=? AND r.mode=? AND r.workflow=? ORDER BY a.rowid DESC`, source.RunID, task, mode, workflow)
		if err != nil {
			return err
		}
		selected := ""
		count := 0
		selectedContext := ""
		for rows.Next() {
			var attempt, workContext string
			if err = rows.Scan(&attempt, &workContext); err != nil {
				break
			}
			if !strings.HasSuffix(workContext, suffix) && (alternateSuffix == "" || !strings.HasSuffix(workContext, alternateSuffix)) {
				continue
			}
			count++
			if selected == "" {
				selected, selectedContext = attempt, workContext
			} else if workContext != selectedContext {
				err = ErrResultAuthority
				break
			}
		}
		rowErr := errors.Join(err, rows.Err(), rows.Close())
		if rowErr != nil {
			return rowErr
		}
		if count == 0 {
			return ErrNotFound
		}
		binding, err = readResultBinding(ctx, tx, source.RunID, selected, false)
		return err
	})
	return binding, err
}
