package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ReportDelivery is one immutable, final redacted report/reply intent. The
// possibly-sent boundary survives a lost response; only a trusted receipt can
// authorize completion. The caller owns identity derivation and redaction.
type ReportDelivery struct {
	Key                                          string
	Version                                      int
	SessionID, TriggerID, RunID                  int64
	Task, Destination, TriggerNamespace, Purpose string
	BotUserID                                    int64
	Body, Marker                                 string
	PossiblySent                                 bool
	CommentID                                    int64
	Completed, Legacy                            bool
}

const reportDeliveryColumns = `delivery_key,identity_version,COALESCE(session_id,0),trigger_id,COALESCE(run_id,0),task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,COALESCE(comment_id,0),completed,legacy`

func scanReportDelivery(row scanner) (ReportDelivery, error) {
	var d ReportDelivery
	err := row.Scan(&d.Key, &d.Version, &d.SessionID, &d.TriggerID, &d.RunID, &d.Task, &d.Destination, &d.TriggerNamespace, &d.Purpose, &d.BotUserID, &d.Body, &d.Marker, &d.PossiblySent, &d.CommentID, &d.Completed, &d.Legacy)
	if errors.Is(err, sql.ErrNoRows) {
		return ReportDelivery{}, ErrNotFound
	}
	return d, err
}

// GetReportDelivery returns the retained intent, including completed history.
func (db *DB) GetReportDelivery(ctx context.Context, key string) (ReportDelivery, error) {
	d, err := scanReportDelivery(db.sql.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, key))
	if err != nil {
		return ReportDelivery{}, fmt.Errorf("store: read report delivery: %w", err)
	}
	return d, nil
}

// EnsureReportDelivery records redacted intent before external effects. A
// repeated identity reuses original bytes and original numeric bot identity.
func (db *DB) EnsureReportDelivery(ctx context.Context, want ReportDelivery) (ReportDelivery, error) {
	if want.Version == 0 {
		want.Version = 1
	}
	if want.Key == "" || want.Version != 1 || want.SessionID <= 0 || want.TriggerID < 0 || want.RunID < 0 || want.Task == "" || want.Destination == "" || want.TriggerNamespace == "" || want.Purpose == "" || want.BotUserID <= 0 || want.Body == "" || want.Marker == "" {
		return ReportDelivery{}, errors.New("store: invalid report delivery identity or intent")
	}
	if want.CommentID != 0 || want.Completed {
		return ReportDelivery{}, errors.New("store: a new report intent cannot claim confirmation")
	}
	var out ReportDelivery
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, want.Key))
		if err == nil {
			if existing.Version != want.Version || existing.SessionID != want.SessionID || existing.TriggerID != want.TriggerID || existing.RunID != want.RunID || existing.Task != want.Task || existing.Destination != want.Destination || existing.TriggerNamespace != want.TriggerNamespace || existing.Purpose != want.Purpose {
				return errors.New("report delivery key conflicts with stored identity")
			}
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		var task string
		if err := tx.QueryRowContext(ctx, `SELECT task_ref FROM sessions WHERE id=?`, want.SessionID).Scan(&task); err != nil {
			return err
		}
		if task != want.Task {
			return errors.New("report delivery task does not match session")
		}
		if want.RunID != 0 {
			run, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id=?`, want.RunID))
			if err != nil {
				return err
			}
			if run.SessionID != want.SessionID || run.TriggerCommentID != want.TriggerID {
				return errors.New("report delivery run does not match source")
			}
			graph, err := graphReportDeliveryTx(ctx, tx, want)
			if err != nil {
				return err
			}
			if !run.Reportable() && !graph {
				return errors.New("report delivery run has not finished")
			}
		}
		if want.Legacy {
			want.PossiblySent = true
		}
		out, err = scanReportDelivery(tx.QueryRowContext(ctx, `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,run_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,legacy) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING `+reportDeliveryColumns, want.Key, want.Version, want.SessionID, want.TriggerID, nullID(want.RunID), want.Task, want.Destination, want.TriggerNamespace, want.Purpose, want.BotUserID, want.Body, want.Marker, want.PossiblySent, want.Legacy))
		return err
	})
	if err != nil {
		return ReportDelivery{}, fmt.Errorf("store: retain report delivery: %w", err)
	}
	return out, nil
}

// MarkReportDeliverySent atomically claims the only new POST opportunity. A
// false result means recovery must look up existing effects, never POST again.
func (db *DB) MarkReportDeliverySent(ctx context.Context, key string) (bool, error) {
	var claimed bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, key))
		if err != nil {
			return err
		}
		if d.PossiblySent || d.Completed {
			return nil
		}
		if d.TriggerNamespace == ResearchReminderNamespace {
			waitKey, e := researchReminderKeyTx(ctx, tx, d.Key)
			if e != nil {
				return e
			}
			eligible, e := researchReminderEligibleTx(ctx, tx, waitKey)
			if e != nil {
				return e
			}
			if !eligible {
				_, e = tx.ExecContext(ctx, `UPDATE research_wait_reminders SET suppressed=1 WHERE wait_delivery_key=?`, waitKey)
				return e
			}
		}
		res, err := tx.ExecContext(ctx, `UPDATE report_deliveries SET possibly_sent=1 WHERE delivery_key=? AND possibly_sent=0 AND completed=0`, key)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		claimed = n == 1
		return err
	})
	if err != nil {
		return false, fmt.Errorf("store: claim report dispatch: %w", err)
	}
	return claimed, nil
}

// ConfirmReportDelivery persists externally verified delivery. A saved receipt
// cannot be replaced by a different comment, even if the original was deleted.
func (db *DB) ConfirmReportDelivery(ctx context.Context, key string, commentID int64) (ReportDelivery, error) {
	if commentID <= 0 {
		return ReportDelivery{}, errors.New("store: report receipt requires positive comment identity")
	}
	var out ReportDelivery
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		current, err := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, key))
		if err != nil {
			return err
		}
		if current.CommentID != 0 && current.CommentID != commentID {
			return errors.New("report delivery already has a different receipt")
		}
		out, err = scanReportDelivery(tx.QueryRowContext(ctx, `UPDATE report_deliveries SET possibly_sent=1,comment_id=? WHERE delivery_key=? RETURNING `+reportDeliveryColumns, commentID, key))
		return err
	})
	if err != nil {
		return ReportDelivery{}, fmt.Errorf("store: confirm report delivery: %w", err)
	}
	return out, nil
}

// CompleteReportDelivery acknowledges the delivery, run and original source in
// one transaction. Uncertainty and local failures never advance source ordering.
func (db *DB) CompleteReportDelivery(ctx context.Context, key string) (ReportDelivery, error) {
	var out ReportDelivery
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanReportDelivery(tx.QueryRowContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE delivery_key=?`, key))
		if err != nil {
			return err
		}
		if d.Completed {
			out = d
			return nil
		}
		if d.CommentID <= 0 {
			return errors.New("report delivery has no confirmed receipt")
		}
		if d.TriggerNamespace == "doctor" {
			return errors.New("doctor delivery requires doctor completion")
		}
		graph, err := graphReportDeliveryTx(ctx, tx, d)
		if err != nil {
			return err
		}
		// Graph publication does not settle the graph or its subprocess.
		switch {
		case d.TriggerNamespace == "plan-observation" || d.TriggerNamespace == "plan-reply" || d.TriggerNamespace == PlanNoticeNamespace:
			if err = completePlanNoticeTx(ctx, tx, d); err != nil {
				return err
			}
		case d.TriggerNamespace == ResearchReminderNamespace:
			if _, err := researchReminderKeyTx(ctx, tx, d.Key); err != nil {
				return err
			}
		case !graph:
			if d.RunID != 0 {
				if _, err := completeRunReportTx(ctx, tx, d.RunID, d.CommentID); err != nil {
					return err
				}
			} else if err := consumeReportSource(ctx, tx, d.SessionID, d.TriggerID); err != nil {
				return err
			}
		}
		// A confirmed runless body refusal settles initialization in the same
		// transaction. A crash must not reopen this instruction after repair.
		if !graph && d.RunID == 0 && d.TriggerNamespace == "issue-body" {
			if _, err := tx.ExecContext(ctx, `UPDATE mode_selections SET cleanup_pending=1 WHERE session_id=? AND trigger_comment_id=0`, d.SessionID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE watermarks SET ticket_body_pending=0 WHERE session_id=?`, d.SessionID); err != nil {
				return err
			}
		}
		out, err = scanReportDelivery(tx.QueryRowContext(ctx, `UPDATE report_deliveries SET completed=1 WHERE delivery_key=? RETURNING `+reportDeliveryColumns, key))
		return err
	})
	if err != nil {
		return ReportDelivery{}, fmt.Errorf("store: complete report delivery: %w", err)
	}
	return out, nil
}

func consumeReportSource(ctx context.Context, tx *sql.Tx, sessionID, triggerID int64) error {
	refinement, err := consumeRefinement(ctx, tx, sessionID, triggerID)
	if err != nil {
		return fmt.Errorf("consume story refinement: %w", err)
	}
	if !refinement && !IsPrivateTrigger(triggerID) {
		if _, err := advanceWatermark(ctx, tx, sessionID, triggerID); err != nil {
			return fmt.Errorf("advance watermark: %w", err)
		}
	}
	return nil
}

// FindPendingReportDelivery permits source recovery before an executor can
// reinterpret the trigger against changed readiness or task state.
func (db *DB) FindPendingReportDelivery(ctx context.Context, sessionID, triggerID int64) (ReportDelivery, bool, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE session_id=? AND trigger_id=? AND completed=0 AND trigger_namespace NOT IN ('plan-observation','plan-reply','plan-notice','research-reminder','code-pr-proposal') ORDER BY delivery_key LIMIT 2`, sessionID, triggerID)
	if err != nil {
		return ReportDelivery{}, false, err
	}
	defer rows.Close()
	var out ReportDelivery
	count := 0
	for rows.Next() {
		d, err := scanReportDelivery(rows)
		if err != nil {
			return ReportDelivery{}, false, err
		}
		out = d
		count++
	}
	if err := rows.Err(); err != nil {
		return ReportDelivery{}, false, err
	}
	if count > 1 {
		return ReportDelivery{}, false, errors.New("store: source has multiple unresolved report purposes")
	}
	return out, count == 1, nil
}

// ListPendingReportDeliveries is durable recovery debt independent of watermarks.
func (db *DB) ListPendingReportDeliveries(ctx context.Context) ([]ReportDelivery, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE completed=0 AND doctor_command_id IS NULL AND trigger_namespace<>'code-pr-proposal' ORDER BY session_id,trigger_id,delivery_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportDelivery{}
	for rows.Next() {
		d, err := scanReportDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// IsLegacyReportRun distinguishes pre-journal allocations from safe new ones.
func (db *DB) IsLegacyReportRun(ctx context.Context, runID int64) (bool, error) {
	var legacy bool
	err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_delivery_legacy_runs WHERE run_id=?)`, runID).Scan(&legacy)
	return legacy, err
}
