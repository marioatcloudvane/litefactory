package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
)

// RetainDoctorInstructionCapture writes a validated capture exactly once while
// its attempt still owns an active conversation. It does not alter public history.
func (db *DB) RetainDoctorInstructionCapture(ctx context.Context, attemptID string, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := instructionpackage.Restore(raw); err != nil {
		return fmt.Errorf("retain doctor instruction capture: %w", err)
	}
	r, err := db.sql.ExecContext(ctx, `INSERT INTO doctor_instruction_captures(attempt_id,capture,created_at)
		SELECT a.id,?,? FROM doctor_attempts a JOIN doctor_conversations c ON c.id=a.conversation_id
		WHERE a.id=? AND a.state='running' AND c.state='active'
		ON CONFLICT(attempt_id) DO NOTHING`, raw, FormatTime(nowUTC()), attemptID)
	return doctorAffected(r, err)
}

// DoctorInstructionCapture restores and checks retained bytes without reading
// live instructions. Historical attempts without captures return ErrNotFound.
func (db *DB) DoctorInstructionCapture(ctx context.Context, attemptID string) ([]byte, error) {
	var raw []byte
	err := db.sql.QueryRowContext(ctx, `SELECT capture FROM doctor_instruction_captures WHERE attempt_id=?`, attemptID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := instructionpackage.Restore(raw); err != nil {
		return nil, fmt.Errorf("restore doctor instruction capture: %w", err)
	}
	return raw, nil
}
