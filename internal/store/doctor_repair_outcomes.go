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
	rows, err := db.sql.QueryContext(ctx, `SELECT task_ref FROM doctor_repairs r WHERE state='restart_pending' OR (state='recovered' AND source_id LIKE 'github-doctor-apply:%' AND NOT EXISTS(SELECT 1 FROM report_deliveries d WHERE d.purpose='doctor-repair-outcome:'||r.id)) ORDER BY rowid LIMIT 16`)
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

// EnsureDoctorRepairOutcome retains redacted complete outcome parts before any
// remote effect, using the existing doctor delivery namespace and source identity.
func (db *DB) EnsureDoctorRepairOutcome(ctx context.Context, id string, parts []string, bot int64) error {
	if bot <= 0 || len(parts) == 0 || len(parts) > 20 {
		return errors.New("invalid doctor outcome presentation")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var command, comment int64
		var target string
		err := tx.QueryRowContext(ctx, `SELECT i.id,i.comment_id,i.task_ref FROM doctor_repairs r JOIN doctor_inbox i ON r.source_id='github-doctor-apply:'||i.id WHERE r.id=? AND r.state='recovered' AND i.task_ref=r.task_ref`, id).Scan(&command, &comment, &target)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
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
		return nil
	})
}
