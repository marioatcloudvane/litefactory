package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

func mergeMeterDiagnostics(previous string, codes []string) (string, error) {
	var old []string
	if previous != "" {
		if err := json.Unmarshal([]byte(previous), &old); err != nil {
			return "", err
		}
	}
	old = append(old, codes...)
	merged := old
	slices.Sort(merged)
	merged = slices.Compact(merged)
	return meterDiagnostics(merged)
}
func meterAttemptCoverage(ctx context.Context, tx *sql.Tx, id, status string, codes []string) (string, string, error) {
	var oldStatus, oldCodes string
	if err := tx.QueryRowContext(ctx, `SELECT collection_status,diagnostic_codes_json FROM meter_attempts WHERE id=?`, id).Scan(&oldStatus, &oldCodes); err != nil {
		return "", "", err
	}
	merged, err := mergeMeterDiagnostics(oldCodes, codes)
	if err != nil {
		return "", "", err
	}
	status = meterStatus(status)
	if oldStatus == "partial" || merged != "[]" && merged != "null" {
		status = "partial"
	}
	return status, merged, nil
}
func meterAttemptDiagnostic(ctx context.Context, tx *sql.Tx, id, code string) error {
	status, diagnostics, err := meterAttemptCoverage(ctx, tx, id, "partial", []string{code})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE meter_attempts SET collection_status=?,diagnostic_codes_json=? WHERE id=?`, status, diagnostics, id)
	return err
}

// FinishMeterAttempt records coverage independently of the subprocess outcome.
// Persistence owns the bounded deadline; callers can detach cancellation after
// their subprocess has stopped without extending the subprocess lifetime.
func (db *DB) FinishMeterAttempt(ctx context.Context, id, outcome, status, version, format string, codes []string) error {
	return db.FinishMeterAttemptWithRefusal(ctx, id, outcome, status, version, format, codes, false)
}

// FinishMeterAttemptWithRefusal atomically retains a provider-affirmed refusal
// alongside finalization. The fact never upgrades collection or token pricing.
func (db *DB) FinishMeterAttemptWithRefusal(ctx context.Context, id, outcome, status, version, format string, codes []string, zeroCostRefusal bool) error {
	if zeroCostRefusal && outcome != "failure" {
		return errors.New("store: refusal requires failed attempt")
	}
	if outcome != "success" && outcome != "failure" && outcome != "cancelled" && outcome != "abandoned" {
		return errors.New("store: invalid attempt outcome")
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.withTx(bounded, func(tx *sql.Tx) error {
		mergedStatus, diagnostics, err := meterAttemptCoverage(bounded, tx, id, status, codes)
		if err != nil {
			return err
		}
		r, err := tx.ExecContext(bounded, `UPDATE meter_attempts SET finished_at=?,outcome=?,collection_status=?,agent_version=?,source_format=?,diagnostic_codes_json=?,provider_reported_zero_cost_refusal=? WHERE id=? AND finished_at IS NULL`, FormatTime(nowUTC()), outcome, mergedStatus, nullString(version), nullString(format), diagnostics, zeroCostRefusal, id)
		if err != nil {
			return err
		}
		return requireAffected(r, "finish meter attempt")
	})
}

// UpdateMeterCoverage checkpoints known coverage without finalizing execution.
func (db *DB) UpdateMeterCoverage(ctx context.Context, id, status, version, format string, codes []string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		mergedStatus, diagnostics, err := meterAttemptCoverage(ctx, tx, id, status, codes)
		if err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx, `UPDATE meter_attempts SET collection_status=?,agent_version=?,source_format=?,diagnostic_codes_json=? WHERE id=? AND finished_at IS NULL`, mergedStatus, nullString(version), nullString(format), diagnostics, id)
		if err != nil {
			return err
		}
		return requireAffected(r, "update meter coverage")
	})
}

// Model provenance uses a canonical JSON set in aggregates. Source observations
// retain their original provenance verbatim, allowing mixed verified sources to
// be combined without a delimiter collision or losing earlier attribution.
func mergeMeterProvenance(previous, current string) (string, error) {
	var sources []string
	if previous != "" {
		if err := json.Unmarshal([]byte(previous), &sources); err != nil {
			return "", err
		}
	}
	if current != "" {
		sources = append(sources, current)
	}
	slices.Sort(sources)
	sources = slices.Compact(sources)
	if len(sources) > 64 {
		return "", errors.New("store: excessive model provenance sources")
	}
	data, err := json.Marshal(sources)
	return string(data), err
}

func abandonMeterAttempts(ctx context.Context, tx *sql.Tx, runID int64, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM meter_attempts WHERE run_id=? AND finished_at IS NULL`, runID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return errors.Join(err, rows.Close())
		}
		ids = append(ids, id)
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	for _, id := range ids {
		if err := meterAttemptDiagnostic(ctx, tx, id, "abandoned_attempt"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE meter_attempts SET outcome='abandoned',finished_at=? WHERE id=?`, FormatTime(now), id); err != nil {
			return err
		}
	}
	return nil
}
