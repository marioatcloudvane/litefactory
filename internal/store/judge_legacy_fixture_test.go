package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
)

// retainPreJudgeDoctorProposal reproduces an accepted pre-judge ledger snapshot
// for upgrade tests. It does not ask current-schema accessors to write old tables.
func retainPreJudgeDoctorProposal(t *testing.T, db *DB, c DoctorConversation, proposal []byte) int64 {
	t.Helper()
	ctx := t.Context()
	var proposalID int64
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		work := fmt.Sprintf("doctor:%d:generation:%d", c.ID, c.Generation)
		hash, err := ResultContextHash(ResultAttempt{WorkContext: work, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO application_results(task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,revision,workflow) VALUES(?,'',?,?,1,2,2,1,'doctor')`, c.TaskRef, work, hash)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO application_result_attempts(run_id,attempt,result_id,active,doctor_attempt_id) VALUES(NULL,'repair-proposer',?,0,'repair-proposer')`, id); err != nil {
			return err
		}
		payload := []byte(`{"comment":"retained historical proposal"}`)
		diagnostics := []byte(`{"valid":true}`)
		request, err := tx.ExecContext(ctx, `INSERT INTO application_result_requests(result_id,run_id,attempt,request_id,request_hash,payload,diagnostics,input_state,applied_revision,doctor_attempt_id) VALUES(?,NULL,'repair-proposer','proposal',?,?,?,'accepted',1,'repair-proposer')`, id, submissionDigest(payload), payload, diagnostics)
		if err != nil {
			return err
		}
		requestID, err := request.LastInsertId()
		if err != nil {
			return err
		}
		snapshot, err := json.Marshal(struct {
			Proposal json.RawMessage `json:"proposal"`
		}{Proposal: proposal})
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO application_result_revisions(result_id,revision,run_id,attempt,request_row,snapshot,snapshot_hash,retired,doctor_attempt_id) VALUES(?,1,NULL,'repair-proposer',?,?,?,'[]','repair-proposer')`, id, requestID, snapshot, submissionDigest(snapshot)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO application_result_observations(request_row,sequence,run_id,attempt,dimension,state,doctor_attempt_id) VALUES(?,1,NULL,'repair-proposer','received','received','repair-proposer'),(?,2,NULL,'repair-proposer','input','accepted','repair-proposer')`, requestID, requestID); err != nil {
			return err
		}
		saved, err := tx.ExecContext(ctx, `INSERT INTO doctor_proposals(conversation_id,attempt_id,result_id,result_revision,payload) VALUES(?,'repair-proposer',?,1,?)`, c.ID, id, proposal)
		if err != nil {
			return err
		}
		proposalID, err = saved.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE doctor_conversations SET current_proposal_id=?,current_result_id=?,current_result_revision=1,revision=revision+1,updated_at=? WHERE id=?`, proposalID, id, FormatTime(nowUTC()), c.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE doctor_attempts SET state='completed',finished_at=? WHERE id='repair-proposer'`, FormatTime(nowUTC()))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return proposalID
}
