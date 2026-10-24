package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// BindDoctorResultAttempt uses the shared result ledger, without a workflow run.
// Conversation generation is part of the immutable context. Doctor and workflow
// writers never revoke each other.
func (db *DB) BindDoctorResultAttempt(ctx context.Context, attemptID string) (ResultBinding, error) {
	var b ResultBinding
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var c DoctorConversation
		err := tx.QueryRowContext(ctx, `SELECT c.id,c.task_ref,c.generation,c.state,c.revision FROM doctor_conversations c JOIN doctor_attempts a ON a.conversation_id=c.id WHERE a.id=? AND a.state='running' AND c.state='active'`, attemptID).Scan(&c.ID, &c.TaskRef, &c.Generation, &c.State, &c.Revision)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDoctorFenced
		}
		if err != nil {
			return err
		}
		old, err := readResultBinding(ctx, tx, 0, attemptID, false)
		if err == nil {
			if err := checkResultAccess(ctx, tx, old, true); err != nil {
				return err
			}
			b = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		work := fmt.Sprintf("doctor:%d:generation:%d", c.ID, c.Generation)
		hash, err := ResultContextHash(ResultAttempt{WorkContext: work, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_results(task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,workflow) VALUES(?,'',?,?,1,2,2,'doctor') ON CONFLICT(task,mode,context_hash) DO NOTHING`, c.TaskRef, work, hash); err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM application_results WHERE task=? AND mode='' AND context_hash=? AND workflow='doctor'`, c.TaskRef, hash).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE application_result_attempts SET active=0 WHERE result_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_result_attempts(run_id,attempt,result_id,active,doctor_attempt_id) VALUES(NULL,?,?,1,?)`, attemptID, id, attemptID); err != nil {
			return err
		}
		b, err = readResultBinding(ctx, tx, 0, attemptID, true)
		return err
	})
	return b, err
}

func checkDoctorResultWriter(ctx context.Context, tx *sql.Tx, b ResultBinding) error {
	if b.RunID != 0 || b.Mode != "" || b.Workflow != "doctor" || b.DoctorAttemptID != b.Attempt {
		return ErrResultAuthority
	}
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_attempts a JOIN doctor_conversations c ON c.id=a.conversation_id WHERE a.id=? AND a.state='running' AND c.state='active' AND c.task_ref=? AND ('doctor:'||c.id||':generation:'||c.generation)=?`, b.DoctorAttemptID, b.Task, b.WorkContext).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrResultAuthority
	}
	return nil
}

// AdoptDoctorResult settles only a durable accepted diagnosis of this invocation.
// Proposal bytes are an already validated redacted projection of that snapshot.
func (db *DB) AdoptDoctorResult(ctx context.Context, b ResultBinding, requestID string, proposal []byte) error {
	return db.adoptDoctorResult(ctx, b, requestID, proposal, false)
}

// AdoptRecoveredDoctorResult adopts an accepted result while truthfully recording
// that its process ended before finalization.
func (db *DB) AdoptRecoveredDoctorResult(ctx context.Context, b ResultBinding, requestID string, proposal []byte) error {
	return db.adoptDoctorResult(ctx, b, requestID, proposal, true)
}

func (db *DB) adoptDoctorResult(ctx context.Context, b ResultBinding, requestID string, proposal []byte, recovered bool) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, true); err != nil {
			return err
		}
		receipt, err := resultReceiptTx(ctx, tx, b.ResultID, 0, requestID)
		if err != nil {
			return err
		}
		if b.DoctorAttemptID == "" || receipt.Attempt != b.Attempt || receipt.InputState != "accepted" || receipt.AppliedRevision < 1 {
			return ErrResultAuthority
		}

		var snapshot []byte
		if err := tx.QueryRowContext(ctx, `SELECT r.snapshot FROM application_result_revisions r JOIN application_results a ON a.id=r.result_id AND a.revision=r.revision WHERE r.result_id=? AND r.revision=?`, b.ResultID, receipt.AppliedRevision).Scan(&snapshot); err != nil {
			return ErrResultAuthority
		}
		var projection struct {
			Proposal         json.RawMessage `json:"proposal"`
			WithdrawProposal bool            `json:"withdraw_proposal"`
		}
		if err := json.Unmarshal(snapshot, &projection); err != nil {
			return err
		}
		if len(proposal) > 0 && !bytes.Equal(projection.Proposal, proposal) {
			return errors.New("doctor proposal differs from accepted snapshot")
		}
		if len(proposal) == 0 && len(projection.Proposal) > 0 && string(projection.Proposal) != "null" && string(projection.Proposal) != "[]" {
			return errors.New("doctor proposal projection omitted")
		}
		if projection.WithdrawProposal && len(proposal) > 0 {
			return errors.New("cannot withdraw and replace a proposal together")
		}
		var cid int64
		if err := tx.QueryRowContext(ctx, `SELECT conversation_id FROM doctor_attempts WHERE id=?`, b.DoctorAttemptID).Scan(&cid); err != nil {
			return err
		}
		var proposalID any
		if len(proposal) > 0 {
			if !json.Valid(proposal) || len(proposal) > 65536 {
				return errors.New("invalid doctor proposal projection")
			}
			result, err := tx.ExecContext(ctx, `INSERT INTO doctor_proposals(conversation_id,attempt_id,result_id,result_revision,payload) VALUES(?,?,?,?,?)`, cid, b.DoctorAttemptID, b.ResultID, receipt.AppliedRevision, proposal)
			if err != nil {
				return err
			}
			proposalID, err = result.LastInsertId()
			if err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE doctor_conversations SET current_proposal_id=CASE WHEN ? THEN NULL ELSE COALESCE(?,current_proposal_id) END,current_result_id=?,current_result_revision=?,revision=revision+1,updated_at=? WHERE id=? AND state='active'`, projection.WithdrawProposal, proposalID, b.ResultID, receipt.AppliedRevision, FormatTime(nowUTC()), cid); err != nil {
			return err
		}
		state, reason := "completed", ""
		if recovered {
			state, reason = "interrupted", "Accepted diagnosis recovered after doctor process ended"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE doctor_attempts SET state=?,failure=?,finished_at=? WHERE id=? AND state='running'`, state, reason, FormatTime(nowUTC()), b.DoctorAttemptID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE application_result_attempts SET active=0 WHERE doctor_attempt_id=?`, b.DoctorAttemptID)
		return err
	})
}

// DoctorCurrentResult returns retained full diagnosis bytes without exposing
// another conversation's result or requiring an active invocation.
func (db *DB) DoctorCurrentResult(ctx context.Context, cid int64) ([]byte, error) {
	var data []byte
	err := db.sql.QueryRowContext(ctx, `SELECT r.snapshot FROM doctor_conversations c JOIN application_result_revisions r ON r.result_id=c.current_result_id AND r.revision=c.current_result_revision WHERE c.id=?`, cid).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}

// DoctorCurrentProposal preserves the most recent proposal across findings-only
// turns and failed attempts; presentation never authorizes repair.
func (db *DB) DoctorCurrentProposal(ctx context.Context, cid int64) ([]byte, error) {
	var data []byte
	err := db.sql.QueryRowContext(ctx, `SELECT p.payload FROM doctor_conversations c JOIN doctor_proposals p ON p.id=c.current_proposal_id WHERE c.id=?`, cid).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}

// PendingDoctorResult identifies an accepted result left by a dead doctor writer.
// Callers must own the process-held issue lock before attempting recovery.
func (db *DB) PendingDoctorResult(ctx context.Context, ref string) (DoctorAttempt, string, error) {
	var a DoctorAttempt
	var request string
	err := db.sql.QueryRowContext(ctx, `SELECT a.id,a.conversation_id,a.evidence,q.request_id FROM doctor_conversations c JOIN doctor_attempts a ON a.conversation_id=c.id JOIN application_result_requests q ON q.doctor_attempt_id=a.id WHERE c.task_ref=? AND c.state='active' AND a.state='running' AND q.input_state='accepted' AND q.applied_revision>0 ORDER BY q.id DESC LIMIT 1`, ref).Scan(&a.ID, &a.ConversationID, &a.Evidence, &request)
	if errors.Is(err, sql.ErrNoRows) {
		return a, "", ErrNotFound
	}
	return a, request, err
}

// InterruptDoctorMeters closes only orphaned doctor usage attempts, retaining
// unknown collection and truthful abandoned process outcome.
func (db *DB) InterruptDoctorMeters(ctx context.Context, doctorID string) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE meter_attempts SET finished_at=?,outcome='abandoned',collection_status='unknown',diagnostic_codes_json='["doctor_process_ended_before_collection"]' WHERE doctor_attempt_id=? AND finished_at IS NULL`, FormatTime(nowUTC()), doctorID)
	return err
}
