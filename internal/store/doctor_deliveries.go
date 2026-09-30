package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// EnsureDoctorDeliveries atomically retains every numbered part and its exact
// immutable proposal reference before the first remote post.
func (db *DB) EnsureDoctorDeliveries(ctx context.Context, c DoctorCommand, parts []string, botID int64) error {
	if len(parts) == 0 || len(parts) > 20 || botID <= 0 {
		return errors.New("invalid doctor presentation")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		current, err := scanDoctorCommand(tx.QueryRowContext(ctx, `SELECT `+doctorCommandColumns+` FROM doctor_inbox WHERE id=? AND state='completed'`, c.ID))
		if err != nil {
			return err
		}
		if current.TaskRef != c.TaskRef || current.ResultBody != c.ResultBody || current.ProposalID != c.ProposalID {
			return errors.New("doctor presentation identity drift")
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_presentations WHERE command_id=?`, c.ID).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		for i, body := range parts {
			if body == "" || len(body) > 60000 {
				return errors.New("doctor presentation part exceeds bounds")
			}
			sum := sha256.Sum256([]byte(fmt.Sprintf("doctor:%d:%d:%d", c.ID, c.ProposalID, i+1)))
			key := hex.EncodeToString(sum[:])
			marker := "<!-- lf:doctor delivery=" + key + " -->"
			body = fmt.Sprintf("Doctor response — part %d/%d\n\n%s\n\n%s", i+1, len(parts), body, marker)
			if _, err := tx.ExecContext(ctx, `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,run_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,doctor_command_id) VALUES(?,1,NULL,?,NULL,?,?,'doctor','doctor-response',?,?,?,?)`, key, c.CommentID, c.TaskRef, c.TaskRef, botID, body, marker, c.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO doctor_presentations(command_id,proposal_id,entry_point,part,total,delivery_key) VALUES(?,?,'github',?,?,?)`, c.ID, nullID(c.ProposalID), i+1, len(parts), key); err != nil {
				return err
			}
		}
		return nil
	})
}

// PendingDoctorDeliveries rotates bounded debt so uncertain posts cannot starve newer replies.
func (db *DB) PendingDoctorDeliveries(ctx context.Context, after ...string) ([]ReportDelivery, error) {
	cursor := ""
	if len(after) > 0 {
		cursor = after[0]
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE doctor_command_id IS NOT NULL AND completed=0 ORDER BY delivery_key<=?,delivery_key LIMIT 20`, cursor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReportDelivery
	for rows.Next() {
		d, err := scanReportDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CompleteDoctorDelivery never calls workflow source completion or watermarks.
func (db *DB) CompleteDoctorDelivery(ctx context.Context, key string) error {
	r, err := db.sql.ExecContext(ctx, `UPDATE report_deliveries SET completed=1 WHERE delivery_key=? AND doctor_command_id IS NOT NULL AND comment_id IS NOT NULL`, key)
	return doctorAffected(r, err)
}

// DoctorPresentationComplete requires every retained part's confirmed receipt.
func (db *DB) DoctorPresentationComplete(ctx context.Context, commandID int64) (bool, error) {
	var parts, total, pending int
	err := db.sql.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(p.total),0),COALESCE(sum(CASE WHEN d.completed=0 OR d.comment_id IS NULL THEN 1 ELSE 0 END),0) FROM doctor_presentations p JOIN report_deliveries d ON d.delivery_key=p.delivery_key WHERE p.command_id=?`, commandID).Scan(&parts, &total, &pending)
	return parts > 0 && parts == total && pending == 0, err
}

// DoctorDeliveryProbe advances only observation pagination, never delivery truth.
func (db *DB) DoctorDeliveryProbe(ctx context.Context, key string) (int, error) {
	var page int
	err := db.sql.QueryRowContext(ctx, `SELECT doctor_probe_page FROM report_deliveries WHERE delivery_key=? AND doctor_command_id IS NOT NULL`, key).Scan(&page)
	return page, err
}

// AdvanceDoctorDeliveryProbe retains observation position without asserting delivery.
func (db *DB) AdvanceDoctorDeliveryProbe(ctx context.Context, key string, next int) error {
	if next == 0 {
		next = 1
	}
	if next < 1 {
		return errors.New("invalid doctor report probe page")
	}
	r, err := db.sql.ExecContext(ctx, `UPDATE report_deliveries SET doctor_probe_page=? WHERE delivery_key=? AND doctor_command_id IS NOT NULL AND completed=0`, next, key)
	return doctorAffected(r, err)
}
