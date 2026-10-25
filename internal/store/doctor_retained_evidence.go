package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// DoctorRetainedSource identifies an existing owning record, never a filesystem
// path or permission. Journal revisions are checked by the handoff digest.
type DoctorRetainedSource struct{ Kind, ID, Revision, AttemptID string }

// DoctorRetainedSources accounts for records in the reader's exact conversation.
// The bounded page has an explicit continuation; it never implies full history.
func (db *DB) DoctorRetainedSources(ctx context.Context, task, reader string, offset, limit int) ([]DoctorRetainedSource, bool, error) {
	if offset < 0 || limit < 1 || limit > 128 {
		return nil, false, errors.New("invalid retained evidence page")
	}
	var cid int64
	if err := db.sql.QueryRowContext(ctx, `SELECT c.id FROM doctor_conversations c JOIN doctor_attempts a ON a.conversation_id=c.id WHERE c.task_ref=? AND a.id=? AND a.state='running' AND c.state='active'`, task, reader).Scan(&cid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrDoctorFenced
		}
		return nil, false, err
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT kind,identity,revision,attempt FROM (
 SELECT 'doctor_input' kind,a.id identity,'1' revision,a.id attempt FROM doctor_attempts a WHERE conversation_id=?
 UNION ALL SELECT 'doctor_evidence',a.id,'1',a.id FROM doctor_attempts a WHERE conversation_id=?
 UNION ALL SELECT 'doctor_guidance',a.id,'1',a.id FROM doctor_attempts a WHERE conversation_id=?
 UNION ALL SELECT 'doctor_findings',CAST(r.result_id AS TEXT),CAST(r.revision AS TEXT),a.id FROM application_result_revisions r JOIN doctor_attempts a ON a.id=r.doctor_attempt_id WHERE a.conversation_id=?
 UNION ALL SELECT 'doctor_command_receipt',CAST(q.id AS TEXT),'1',a.id FROM application_result_requests q JOIN doctor_attempts a ON a.id=q.doctor_attempt_id WHERE a.conversation_id=? AND q.applied_revision=0 AND q.input_state='accepted'
 UNION ALL SELECT 'repair_operation',p.id,CAST(p.revision AS TEXT),q.attempt_id FROM doctor_repairs p JOIN doctor_proposals q ON q.id=p.proposal_id WHERE p.conversation_id=?
 UNION ALL SELECT 'repair_step',p.id||':'||s.number,CAST(p.revision AS TEXT),q.attempt_id FROM doctor_repair_steps s JOIN doctor_repairs p ON p.id=s.repair_id JOIN doctor_proposals q ON q.id=p.proposal_id WHERE p.conversation_id=?
 ) ORDER BY kind,identity,revision LIMIT ? OFFSET ?`, cid, cid, cid, cid, cid, cid, cid, limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []DoctorRetainedSource
	for rows.Next() {
		var s DoctorRetainedSource
		if err := rows.Scan(&s.Kind, &s.ID, &s.Revision, &s.AttemptID); err != nil {
			return nil, false, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, nil
}

// ReadDoctorRetainedSource rechecks the current reader and exact source ownership
// on each read. It cannot execute a command or mutate repair state.
func (db *DB) ReadDoctorRetainedSource(ctx context.Context, task, reader string, source DoctorRetainedSource) ([]byte, error) {
	var cid int64
	err := db.sql.QueryRowContext(ctx, `SELECT c.id FROM doctor_conversations c JOIN doctor_attempts a ON a.conversation_id=c.id JOIN doctor_attempts source ON source.conversation_id=c.id WHERE c.task_ref=? AND a.id=? AND a.state='running' AND c.state='active' AND source.id=?`, task, reader, source.AttemptID).Scan(&cid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDoctorFenced
		}
		return nil, err
	}
	var raw []byte
	switch source.Kind {
	case "doctor_input":
		err = db.sql.QueryRowContext(ctx, `SELECT owner_text FROM doctor_attempts WHERE conversation_id=? AND id=? AND id=?`, cid, source.ID, source.AttemptID).Scan(&raw)
	case "doctor_evidence":
		err = db.sql.QueryRowContext(ctx, `SELECT evidence FROM doctor_attempts WHERE conversation_id=? AND id=? AND id=?`, cid, source.ID, source.AttemptID).Scan(&raw)
	case "doctor_guidance":
		if source.ID != source.AttemptID {
			return nil, ErrDoctorFenced
		}
		return db.DoctorInstructionCapture(ctx, source.ID)
	case "doctor_command_receipt":
		err = db.sql.QueryRowContext(ctx, `SELECT diagnostics FROM application_result_requests WHERE id=? AND doctor_attempt_id=? AND input_state='accepted' AND applied_revision=0`, source.ID, source.AttemptID).Scan(&raw)
	case "doctor_findings":
		err = db.sql.QueryRowContext(ctx, `SELECT snapshot FROM application_result_revisions WHERE result_id=? AND revision=? AND doctor_attempt_id=?`, source.ID, source.Revision, source.AttemptID).Scan(&raw)
	case "repair_operation", "repair_step":
		id := source.ID
		number := 0
		if source.Kind == "repair_step" {
			split := strings.LastIndex(id, ":")
			if split < 0 {
				return nil, ErrNotFound
			}
			number, err = strconv.Atoi(id[split+1:])
			if err != nil {
				return nil, err
			}
			id = id[:split]
		}
		var r DoctorRepair
		r, err = readDoctorRepair(db.sql.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE id=? AND conversation_id=? AND revision=? AND proposal_id IN(SELECT id FROM doctor_proposals WHERE attempt_id=?)`, id, cid, source.Revision, source.AttemptID))
		if err != nil {
			break
		}
		if source.Kind == "repair_operation" {
			return json.Marshal(r)
		}
		var s DoctorRepairStep
		err = db.sql.QueryRowContext(ctx, `SELECT number,kind,target,before_value,after_value,state,backup_reference,backup_digest,observed_after,source_identity FROM doctor_repair_steps WHERE repair_id=? AND number=?`, id, number).Scan(&s.Number, &s.Kind, &s.Target, &s.Before, &s.After, &s.State, &s.BackupReference, &s.BackupDigest, &s.ObservedAfter, &s.SourceIdentity)
		if err == nil {
			return json.Marshal(s)
		}
	default:
		return nil, fmt.Errorf("unsupported retained source kind %q", source.Kind)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return raw, err
}
