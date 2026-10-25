package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// DoctorRepairRecoveryTargets is bounded routing metadata for retained recovery
// work. It does not disclose any other issue evidence to a doctor invocation.
func (db *DB) DoctorRepairRecoveryTargets(ctx context.Context) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT task_ref FROM doctor_repairs r WHERE state='restart_pending' OR (state IN('recovered','completed','failed') AND source_id LIKE 'github-doctor-apply:%' AND NOT EXISTS(SELECT 1 FROM report_deliveries d WHERE (d.purpose='doctor-repair-outcome:'||r.id OR r.source_id='github-doctor-apply:'||d.doctor_command_id))) ORDER BY rowid LIMIT 16`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err = rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// DoctorRepairRecoveryOperations binds delivery debt to its original operation,
// even when a newer approved repair exists for the same issue.
func (db *DB) DoctorRepairRecoveryOperations(ctx context.Context) ([]DoctorRepair, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs r WHERE state='restart_pending' OR (state IN('recovered','completed','failed') AND source_id LIKE 'github-doctor-apply:%' AND NOT EXISTS(SELECT 1 FROM report_deliveries d WHERE (d.purpose='doctor-repair-outcome:'||r.id OR r.source_id='github-doctor-apply:'||d.doctor_command_id))) ORDER BY rowid LIMIT 16`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var operations []DoctorRepair
	for rows.Next() {
		op, e := readDoctorRepair(rows)
		if e != nil {
			return nil, e
		}
		operations = append(operations, op)
	}
	return operations, rows.Err()
}

// EnsureDoctorRepairOutcome retains redacted complete outcome parts before any
// remote effect, using the existing doctor delivery namespace and source identity.
func (db *DB) EnsureDoctorRepairOutcome(ctx context.Context, id string, parts []string, bot int64) error {
	if bot <= 0 || len(parts) == 0 || len(parts) > 20 {
		return errors.New("invalid doctor outcome presentation")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var command, comment int64
		var target string
		err := tx.QueryRowContext(ctx, `SELECT i.id,i.comment_id,i.task_ref FROM doctor_repairs r JOIN doctor_inbox i ON r.source_id='github-doctor-apply:'||i.id WHERE r.id=? AND r.state IN('recovered','completed','failed') AND i.task_ref=r.task_ref`, id).Scan(&command, &comment, &target)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// Existing generic delivery intent may already have been sent by an older
		// process. Preserve its bytes and key rather than creating another reply.
		var retained int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM report_deliveries WHERE doctor_command_id=?`, command).Scan(&retained); err != nil {
			return err
		}
		if retained == 0 {
			var publisher int64
			sourceErr := tx.QueryRowContext(ctx, `SELECT publisher_id FROM doctor_command_sources WHERE command_id=?`, command).Scan(&publisher)
			if sourceErr == nil {
				bot = publisher
			} else if !errors.Is(sourceErr, sql.ErrNoRows) {
				return sourceErr
			}
			for index, body := range parts {
				if body == "" || len(body) > 48000 {
					return errors.New("doctor outcome part exceeds bounds")
				}
				sum := sha256.Sum256([]byte(fmt.Sprintf("doctor-repair:%s:%d", id, index+1)))
				key := hex.EncodeToString(sum[:])
				marker := "<!-- lf:doctor delivery=" + key + " -->"
				rendered := fmt.Sprintf("Doctor repair outcome — part %d/%d\n\n%s\n\n%s", index+1, len(parts), body, marker)
				if _, err = tx.ExecContext(ctx, `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,run_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,doctor_command_id) VALUES(?,1,NULL,?,NULL,?,?,'doctor',?,?,?,?,?) ON CONFLICT(delivery_key) DO NOTHING`, key, comment, target, target, "doctor-repair-outcome:"+id, bot, rendered, marker, command); err != nil {
					return err
				}
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_inbox SET state='completed',result_body='' WHERE id=? AND state IN('queued','claimed','completed')`, command)
		return err
	})
}

// DoctorCommandRepair returns only the operation admitted for this command.
func (db *DB) DoctorCommandRepair(ctx context.Context, command int64) (DoctorRepair, error) {
	operation, err := readDoctorRepair(db.sql.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE source_id='github-doctor-apply:'||?`, command))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return operation, err
}
