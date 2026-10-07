package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ReportRecovery retains daemon-owned retry eligibility and the latest bounded,
// redacted diagnostic. A stable source key can precede any durable delivery
// intent; DeliveryKey associates that intent once its identity is available.
// Attempts counts failed eligible recoveries, excluding the initial failure.
type ReportRecovery struct {
	Key                  string
	SessionID, TriggerID int64
	DeliveryKey, Task    string
	RunID                int64
	Attempts             int
	NextAt               time.Time
	State, Reason        string
	Escalated            bool
}

const reportRecoveryColumns = `recovery_key,session_id,trigger_id,delivery_key,task,COALESCE(run_id,0),attempts,next_at,state,reason,escalated`

func scanReportRecovery(row scanner) (ReportRecovery, error) {
	var r ReportRecovery
	var due string
	err := row.Scan(&r.Key, &r.SessionID, &r.TriggerID, &r.DeliveryKey, &r.Task, &r.RunID, &r.Attempts, &due, &r.State, &r.Reason, &r.Escalated)
	if errors.Is(err, sql.ErrNoRows) {
		return ReportRecovery{}, ErrNotFound
	}
	if err != nil {
		return ReportRecovery{}, err
	}
	r.NextAt, err = ParseTime(due)
	if err != nil {
		return ReportRecovery{}, fmt.Errorf("parse report recovery due time: %w", err)
	}
	return r, nil
}

// GetReportRecovery returns persisted eligibility without advancing its clock.
func (db *DB) GetReportRecovery(ctx context.Context, key string) (ReportRecovery, error) {
	r, err := scanReportRecovery(db.sql.QueryRowContext(ctx, `SELECT `+reportRecoveryColumns+` FROM report_recoveries WHERE recovery_key=?`, key))
	if err != nil {
		return ReportRecovery{}, fmt.Errorf("store: read report recovery: %w", err)
	}
	return r, nil
}

// PutReportRecovery saves policy computed by the daemon. No delivery intent is
// required: this also preserves intent-write failures and stopped-result debt.
// Existing keys may not move to a different original source.
func (db *DB) PutReportRecovery(ctx context.Context, r ReportRecovery) error {
	if r.Key == "" || r.SessionID <= 0 || r.TriggerID < 0 || r.RunID < 0 || r.Attempts < 0 || r.NextAt.IsZero() || r.State == "" {
		return errors.New("store: invalid report recovery")
	}
	result, err := db.sql.ExecContext(ctx, `INSERT INTO report_recoveries(recovery_key,session_id,trigger_id,delivery_key,task,run_id,attempts,next_at,state,reason,escalated) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(recovery_key) DO UPDATE SET delivery_key=excluded.delivery_key,task=excluded.task,run_id=excluded.run_id,attempts=excluded.attempts,next_at=excluded.next_at,state=excluded.state,reason=excluded.reason,escalated=excluded.escalated WHERE report_recoveries.session_id=excluded.session_id AND report_recoveries.trigger_id=excluded.trigger_id`, r.Key, r.SessionID, r.TriggerID, r.DeliveryKey, r.Task, nullID(r.RunID), r.Attempts, FormatTime(r.NextAt), r.State, r.Reason, r.Escalated)
	if err != nil {
		return fmt.Errorf("store: save report recovery: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: save report recovery count: %w", err)
	}
	if n != 1 {
		return errors.New("store: report recovery key identifies a different source")
	}
	return nil
}

// ListReportRecoveries restores persisted scheduling and diagnostic state. Due
// filtering and rate-limit holds belong to the daemon, not this accessor.
func (db *DB) ListReportRecoveries(ctx context.Context) ([]ReportRecovery, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+reportRecoveryColumns+` FROM report_recoveries ORDER BY recovery_key`)
	if err != nil {
		return nil, fmt.Errorf("store: list report recoveries: %w", err)
	}
	defer rows.Close()
	out := []ReportRecovery{}
	for rows.Next() {
		r, err := scanReportRecovery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
