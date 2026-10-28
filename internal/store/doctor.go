package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
)

// ErrDoctorBusy means a live doctor attempt owns the conversation.
var ErrDoctorBusy = errors.New("doctor conversation is busy")

// ErrDoctorFenced means a stopped or superseded attempt no longer has authority.
var ErrDoctorFenced = errors.New("doctor attempt no longer has authority")

// DoctorConversation is durable history independent of workflow lifecycle.
type DoctorConversation struct {
	ID         int64  `json:"id"`
	TaskRef    string `json:"task_ref"`
	Generation int64  `json:"generation"`
	State      string `json:"state"`
	Revision   int64  `json:"revision"`
}

// DoctorAttempt records input before any provider launch, even a refused one.
type DoctorAttempt struct {
	ID              string `json:"id"`
	ConversationID  int64  `json:"conversation_id"`
	SourceID        string `json:"source_id"`
	OwnerText       string `json:"owner_text"`
	Provider        string `json:"provider"`
	ConfiguredModel string `json:"configured_model"`
	ObservedModel   string `json:"observed_model"`
	PromptVersion   string `json:"prompt_version"`
	GuideVersion    string `json:"guide_version"`
	Evidence        string `json:"evidence"`
	State           string `json:"state"`
	Failure         string `json:"failure"`
}

// CheckDoctorSchema never migrates or creates a migration table.
func (db *DB) CheckDoctorSchema(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	var version int
	if err := db.sql.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("doctor requires an initialized compatible database: %w", err)
	}
	if version != migrations[len(migrations)-1].version {
		return fmt.Errorf("doctor requires schema %d; found %d; use the installation migration lifecycle", migrations[len(migrations)-1].version, version)
	}
	return nil
}

// BeginDoctorAttempt is called while the process holds the issue's doctor lock.
// It recovers only abandoned doctor attempts, never workflow runs.
func (db *DB) BeginDoctorAttempt(ctx context.Context, ref string, a DoctorAttempt) (DoctorConversation, error) {
	if _, err := naming.ParseRef(ref); err != nil {
		return DoctorConversation{}, err
	}
	if a.ID == "" || len(a.ID) > 256 || a.SourceID == "" || len(a.SourceID) > 256 || a.OwnerText == "" || len(a.OwnerText) > 32768 || a.PromptVersion == "" || a.GuideVersion == "" {
		return DoctorConversation{}, errors.New("doctor attempt requires bounded input and identities")
	}
	var c DoctorConversation
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		now := FormatTime(nowUTC())
		if err := ownerActionEffectContextTx(ctx, tx); err != nil {
			return err
		}
		if err := validateDoctorSourceTx(ctx, tx, a.SourceID, ref); err != nil {
			return err
		}
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM doctor_attempts WHERE source_id=?`, a.SourceID).Scan(&existing)
		if err == nil {
			return fmt.Errorf("doctor source already admitted: %s", existing)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		c, err = scanDoctorConversation(tx.QueryRowContext(ctx, `SELECT id,task_ref,generation,state,revision FROM doctor_conversations WHERE task_ref=? AND state='active'`, ref))
		if errors.Is(err, sql.ErrNoRows) {
			result, e := tx.ExecContext(ctx, `INSERT INTO doctor_conversations(task_ref,generation,state,created_at,updated_at) SELECT ?,COALESCE(MAX(generation),0)+1,'active',?,? FROM doctor_conversations WHERE task_ref=?`, ref, now, now, ref)
			if e != nil {
				return e
			}
			id, e := result.LastInsertId()
			if e != nil {
				return e
			}
			c, err = scanDoctorConversation(tx.QueryRowContext(ctx, `SELECT id,task_ref,generation,state,revision FROM doctor_conversations WHERE id=?`, id))
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE meter_attempts SET finished_at=?,outcome='abandoned',collection_status='unknown',diagnostic_codes_json='["doctor_process_ended_before_collection"]' WHERE finished_at IS NULL AND doctor_attempt_id IN(SELECT id FROM doctor_attempts WHERE conversation_id=? AND state='running')`, now, c.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE doctor_attempts SET state='interrupted',failure='Previous doctor process ended before completion',finished_at=? WHERE conversation_id=? AND state='running'`, now, c.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO doctor_attempts(id,conversation_id,source_id,owner_text,prompt_version,guide_version,state,created_at) VALUES(?,?,?,?,?,?,'running',?)`, a.ID, c.ID, a.SourceID, a.OwnerText, a.PromptVersion, a.GuideVersion, now)
		return err
	})
	if err != nil {
		return DoctorConversation{}, fmt.Errorf("begin doctor attempt: %w", err)
	}
	return c, nil
}

func scanDoctorConversation(s scanner) (DoctorConversation, error) {
	var c DoctorConversation
	err := s.Scan(&c.ID, &c.TaskRef, &c.Generation, &c.State, &c.Revision)
	return c, err
}

// ReadDoctorConversation returns the latest generation without creating history.
func (db *DB) ReadDoctorConversation(ctx context.Context, ref string) (DoctorConversation, error) {
	c, err := scanDoctorConversation(db.sql.QueryRowContext(ctx, `SELECT id,task_ref,generation,state,revision FROM doctor_conversations WHERE task_ref=? ORDER BY generation DESC LIMIT 1`, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// DoctorAttempts returns the latest bounded history in chronological order.
func (db *DB) DoctorAttempts(ctx context.Context, id int64) ([]DoctorAttempt, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id,conversation_id,source_id,owner_text,provider,configured_model,observed_model,prompt_version,guide_version,evidence,state,failure FROM (SELECT rowid AS sequence,* FROM doctor_attempts WHERE conversation_id=? ORDER BY rowid DESC LIMIT 30) ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DoctorAttempt, 0)
	for rows.Next() {
		var a DoctorAttempt
		if err := rows.Scan(&a.ID, &a.ConversationID, &a.SourceID, &a.OwnerText, &a.Provider, &a.ConfiguredModel, &a.ObservedModel, &a.PromptVersion, &a.GuideVersion, &a.Evidence, &a.State, &a.Failure); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PrepareDoctorAttempt retains the bounded redacted snapshot and model selection.
func (db *DB) PrepareDoctorAttempt(ctx context.Context, a DoctorAttempt) error {
	if len(a.Evidence) > 131072 {
		return errors.New("doctor evidence exceeds byte limit")
	}
	r, err := db.sql.ExecContext(ctx, `UPDATE doctor_attempts SET provider=?,configured_model=?,evidence=? WHERE id=? AND state='running' AND evidence='' AND EXISTS(SELECT 1 FROM doctor_conversations c WHERE c.id=doctor_attempts.conversation_id AND c.state='active')`, a.Provider, a.ConfiguredModel, a.Evidence, a.ID)
	return doctorAffected(r, err)
}

// FailDoctorAttempt preserves previous findings and marks only this attempt.
func (db *DB) FailDoctorAttempt(ctx context.Context, id, state, reason string) error {
	if state != "failed" && state != "interrupted" {
		return errors.New("invalid doctor failure state")
	}
	if len(reason) > 8192 {
		return errors.New("doctor failure exceeds byte limit")
	}
	r, err := db.sql.ExecContext(ctx, `UPDATE doctor_attempts SET state=?,failure=?,finished_at=? WHERE id=? AND state='running'`, state, reason, FormatTime(nowUTC()), id)
	return doctorAffected(r, err)
}
func doctorAffected(r sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrDoctorFenced
	}
	return nil
}

// StopDoctorConversation revokes pending authority without locking behind an agent.
func (db *DB) StopDoctorConversation(ctx context.Context, ref string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		now := FormatTime(nowUTC())
		if _, err := tx.ExecContext(ctx, `UPDATE doctor_attempts SET state='interrupted',failure='Stopped by owner',finished_at=? WHERE state='running' AND conversation_id IN(SELECT id FROM doctor_conversations WHERE task_ref=? AND state='active')`, now, ref); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE doctor_conversations SET state='stopped',revision=revision+1,updated_at=? WHERE task_ref=? AND state='active'`, now, ref)
		return err
	})
}

// RecordDoctorObservedModel records provider-observed identity separately from
// configured model; empty/unknown is never inferred from the requested model.
func (db *DB) RecordDoctorObservedModel(ctx context.Context, id, model string) error {
	if len(model) > 8192 {
		return errors.New("doctor observed model exceeds bounds")
	}
	r, err := db.sql.ExecContext(ctx, `UPDATE doctor_attempts SET observed_model=? WHERE id=? AND state='running'`, model, id)
	return doctorAffected(r, err)
}
