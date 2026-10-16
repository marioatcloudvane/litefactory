package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// MaxResultInputBytes matches the existing contribution request ceiling.
const MaxResultInputBytes = 4 << 20

// MaxResultSnapshotBytes includes the bounded authored result plus metadata.
const MaxResultSnapshotBytes = MaxResultInputBytes + 8192

// ResultRecord carries a fully validated candidate from the turn service. Store
// retains opaque bytes; application semantics remain outside this SQL package.
type ResultRecord struct {
	DoctorToolReceipt                       bool // retain a scoped doctor tool observation without adopting an application revision
	Binding                                 ResultBinding
	RequestID                               string
	Payload, Diagnostics, Snapshot, Retired []byte
	ExpectedRevision                        int64
	Accepted                                bool
	SubmissionReceiptID                     int64
	Aggregate                               *ResultAggregate
}

// ResultRevision is an immutable full snapshot with its source attribution.
type ResultRevision struct {
	ResultID, Revision, RunID, RequestRow int64
	Attempt, SnapshotHash                 string
	Snapshot, Retired                     []byte
}

// ResultReceipt preserves the immutable request and append-only observations.
type ResultReceipt struct {
	ID, ResultID, RunID, AppliedRevision, SubmissionReceiptID int64
	Attempt, RequestID, RequestHash, InputState               string
	Payload, Diagnostics                                      []byte
	Observations                                              []ResultObservation
	Latest                                                    ResultStates
}

// ResultObservation is an independent factory-observed state, not a workflow
// mutation. Run/attempt/sequence are stamped by store, never authored inputs.
type ResultObservation struct {
	Sequence, RunID           int64
	Attempt, Dimension, State string
}

// ResultStates projects independent dimensions without deleting their history.
type ResultStates struct{ Received, Input, Engineering, Acceptance, Publication string }

const resultReceiptColumns = `id,result_id,COALESCE(run_id,0),attempt,request_id,request_hash,payload,diagnostics,input_state,applied_revision,COALESCE(submission_receipt_id,0)`

func scanResultReceipt(row interface{ Scan(...interface{}) error }) (ResultReceipt, error) {
	var r ResultReceipt
	err := row.Scan(&r.ID, &r.ResultID, &r.RunID, &r.Attempt, &r.RequestID, &r.RequestHash, &r.Payload, &r.Diagnostics, &r.InputState, &r.AppliedRevision, &r.SubmissionReceiptID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}
func resultReceiptTx(ctx context.Context, tx *sql.Tx, resultID, runID int64, requestID string) (ResultReceipt, error) {
	r, err := scanResultReceipt(tx.QueryRowContext(ctx, `SELECT `+resultReceiptColumns+` FROM application_result_requests WHERE result_id=? AND COALESCE(run_id,0)=? AND request_id=?`, resultID, runID, requestID))
	if err != nil {
		return r, err
	}
	r.Latest = ResultStates{Received: "pending", Input: "pending", Engineering: "pending", Acceptance: "pending", Publication: "pending"}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,COALESCE(run_id,0),attempt,dimension,state FROM application_result_observations WHERE request_row=? ORDER BY sequence`, r.ID)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var o ResultObservation
		if err := rows.Scan(&o.Sequence, &o.RunID, &o.Attempt, &o.Dimension, &o.State); err != nil {
			return r, err
		}
		r.Observations = append(r.Observations, o)
		switch o.Dimension {
		case "received":
			r.Latest.Received = o.State
		case "input":
			r.Latest.Input = o.State
		case "engineering":
			r.Latest.Engineering = o.State
		case "acceptance":
			r.Latest.Acceptance = o.State
		case "publication":
			r.Latest.Publication = o.State
		}
	}
	return r, rows.Err()
}

// GetResultReceipt permits an attributed historical reader of the same result.
func (db *DB) GetResultReceipt(ctx context.Context, b ResultBinding, runID int64, requestID string) (ResultReceipt, error) {
	var r ResultReceipt
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		var err error
		r, err = resultReceiptTx(ctx, tx, b.ResultID, runID, requestID)
		return err
	})
	return r, err
}

// ReplayResultRequest recovers a lost response even after writer revocation.
// Original correlation must retain byte-identical input and original attribution.
func (db *DB) ReplayResultRequest(ctx context.Context, b ResultBinding, requestID string, payload []byte) (ResultReceipt, error) {
	r, err := db.GetResultReceipt(ctx, b, b.RunID, requestID)
	if err != nil {
		return r, err
	}
	if r.Attempt != b.Attempt || r.RequestHash != submissionDigest(payload) || !bytes.Equal(r.Payload, payload) {
		return ResultReceipt{}, ErrResultCorrelation
	}
	return r, nil
}

// RecordResult rechecks authority and revision in the same transaction that
// appends request/observations and (when accepted) one immutable full revision.
func (db *DB) RecordResult(ctx context.Context, q ResultRecord) (ResultReceipt, error) {
	var r ResultReceipt
	if q.RequestID == "" || len(q.RequestID) > 256 || len(q.Payload) > MaxResultInputBytes || len(q.Diagnostics) > MaxResultInputBytes || !json.Valid(q.Diagnostics) {
		return r, fmt.Errorf("store: invalid result request")
	}
	if q.DoctorToolReceipt && (q.Binding.DoctorAttemptID == "" || !q.Accepted || q.Aggregate != nil || len(q.Snapshot) != 0 || len(q.Retired) != 0) {
		return r, fmt.Errorf("store: invalid doctor tool receipt")
	}

	if q.Aggregate != nil {
		normalized, err := normalizeResultAggregate(q.Binding, *q.Aggregate)
		if err != nil {
			return r, err
		}
		q.Aggregate = &normalized
		if !q.Accepted || len(q.Snapshot) != 0 || len(q.Retired) != 0 {
			return r, fmt.Errorf("store: aggregate input cannot include a result revision")
		}
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, q.Binding, false); err != nil {
			return err
		}
		old, err := resultReceiptTx(ctx, tx, q.Binding.ResultID, q.Binding.RunID, q.RequestID)
		if err == nil {
			if old.Attempt != q.Binding.Attempt || old.RequestHash != submissionDigest(q.Payload) || !bytes.Equal(old.Payload, q.Payload) {
				return ErrResultCorrelation
			}
			r = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		// Correlation is unique within a source run, including other result contexts.
		var collisions int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM application_result_requests WHERE run_id=? AND request_id=?`, q.Binding.RunID, q.RequestID).Scan(&collisions); err != nil {
			return err
		}
		if collisions != 0 {
			return ErrResultCorrelation
		}
		if err := checkResultAccess(ctx, tx, q.Binding, true); err != nil {
			return err
		}
		var current int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM application_results WHERE id=? AND context_hash=?`, q.Binding.ResultID, q.Binding.ContextHash).Scan(&current); err != nil {
			return err
		}
		if q.Aggregate == nil && !q.DoctorToolReceipt && current != q.ExpectedRevision {
			return &ResultRevisionConflict{Expected: q.ExpectedRevision, Current: current}
		}
		revision := int64(0)
		state := "rejected"
		switch {
		case q.DoctorToolReceipt:
			state = "accepted"
		case q.Aggregate != nil:
			if err := retainResultAggregateTx(ctx, tx, q.Binding, *q.Aggregate); err != nil {
				return err
			}
			state = "accepted"
		case q.Accepted:
			if current == math.MaxInt64 || len(q.Snapshot) > MaxResultSnapshotBytes || len(q.Retired) > MaxResultInputBytes || !json.Valid(q.Snapshot) || !json.Valid(q.Retired) {
				return fmt.Errorf("store: invalid result candidate")
			}
			revision = current + 1
			state = "accepted"
		case len(q.Snapshot) != 0 || len(q.Retired) != 0:
			return fmt.Errorf("store: rejected input cannot retain a candidate")
		}
		var association interface{}
		if q.SubmissionReceiptID != 0 {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM submission_receipts r JOIN submission_attempts a ON a.run_id=r.run_id AND a.attempt=r.attempt WHERE r.id=? AND r.run_id=? AND a.parent_task=?`, q.SubmissionReceiptID, q.Binding.RunID, q.Binding.Task).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return ErrResultAuthority
			}
			association = q.SubmissionReceiptID
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO application_result_requests(result_id,run_id,attempt,request_id,request_hash,payload,diagnostics,input_state,applied_revision,submission_receipt_id,doctor_attempt_id,judge_attempt_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, q.Binding.ResultID, nullID(q.Binding.RunID), q.Binding.Attempt, q.RequestID, submissionDigest(q.Payload), q.Payload, q.Diagnostics, state, revision, association, nullString(q.Binding.DoctorAttemptID), nullString(q.Binding.JudgeAttemptID))
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if q.Accepted && q.Aggregate == nil && !q.DoctorToolReceipt {
			if _, err := tx.ExecContext(ctx, `INSERT INTO application_result_revisions(result_id,revision,run_id,attempt,request_row,snapshot,snapshot_hash,retired,doctor_attempt_id,judge_attempt_id) VALUES(?,?,?,?,?,?,?,?,?,?)`, q.Binding.ResultID, revision, nullID(q.Binding.RunID), q.Binding.Attempt, id, q.Snapshot, submissionDigest(q.Snapshot), q.Retired, nullString(q.Binding.DoctorAttemptID), nullString(q.Binding.JudgeAttemptID)); err != nil {
				return err
			}
			res, err := tx.ExecContext(ctx, `UPDATE application_results SET revision=? WHERE id=? AND revision=? AND context_hash=?`, revision, q.Binding.ResultID, current, q.Binding.ContextHash)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return &ResultRevisionConflict{Expected: q.ExpectedRevision, Current: current}
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_result_observations(request_row,sequence,run_id,attempt,dimension,state,doctor_attempt_id,judge_attempt_id) VALUES(?,1,?,?,'received','received',?,?),(?,2,?,?,'input',?,?,?)`, id, nullID(q.Binding.RunID), q.Binding.Attempt, nullString(q.Binding.DoctorAttemptID), nullString(q.Binding.JudgeAttemptID), id, nullID(q.Binding.RunID), q.Binding.Attempt, state, nullString(q.Binding.DoctorAttemptID), nullString(q.Binding.JudgeAttemptID)); err != nil {
			return err
		}
		r, err = resultReceiptTx(ctx, tx, q.Binding.ResultID, q.Binding.RunID, q.RequestID)
		return err
	})
	return r, err
}

// GetResultRevision reads exact retained bytes; revision zero selects the latest.
func (db *DB) GetResultRevision(ctx context.Context, b ResultBinding, revision int64) (ResultRevision, error) {
	var r ResultRevision
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		if revision == 0 {
			if err := tx.QueryRowContext(ctx, `SELECT revision FROM application_results WHERE id=?`, b.ResultID).Scan(&revision); err != nil {
				return err
			}
		}
		err := tx.QueryRowContext(ctx, `SELECT result_id,revision,COALESCE(run_id,0),attempt,request_row,snapshot,snapshot_hash,retired FROM application_result_revisions WHERE result_id=? AND revision=?`, b.ResultID, revision).Scan(&r.ResultID, &r.Revision, &r.RunID, &r.Attempt, &r.RequestRow, &r.Snapshot, &r.SnapshotHash, &r.Retired)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return r, err
}

// ResultRevisionNumbers lists history identities without loading all snapshots.
func (db *DB) ResultRevisionNumbers(ctx context.Context, b ResultBinding) ([]int64, error) {
	revisions := []int64{}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT revision FROM application_result_revisions WHERE result_id=? ORDER BY revision`, b.ResultID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n int64
			if err := rows.Scan(&n); err != nil {
				return err
			}
			revisions = append(revisions, n)
		}
		return rows.Err()
	})
	return revisions, err
}

// ResultRequestReference identifies retained input within one authorized result.
// It contains no authored payload, capability or evidence bytes.
type ResultRequestReference struct {
	RunID           int64  `json:"run_id"`
	RequestID       string `json:"request_id"`
	InputState      string `json:"input_state"`
	AppliedRevision int64  `json:"applied_revision"`
}

// ResultRequestReferences returns a bounded recent correlation inventory for
// launch/recovery. Exact receipt lookup remains scoped to the same result.
func (db *DB) ResultRequestReferences(ctx context.Context, b ResultBinding) ([]ResultRequestReference, error) {
	refs := []ResultRequestReference{}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT COALESCE(run_id,0),request_id,input_state,applied_revision FROM application_result_requests WHERE result_id=? ORDER BY id DESC LIMIT 32`, b.ResultID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ref ResultRequestReference
			if err := rows.Scan(&ref.RunID, &ref.RequestID, &ref.InputState, &ref.AppliedRevision); err != nil {
				return err
			}
			refs = append(refs, ref)
		}
		return rows.Err()
	})
	return refs, err
}

// FindResultReceipt recovers a known correlation in this exact result after a
// new run binding. Ambiguous reused IDs refuse rather than selecting a source.
func (db *DB) FindResultReceipt(ctx context.Context, b ResultBinding, requestID string) (ResultReceipt, error) {
	var receipt ResultReceipt
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		var count int
		var runID int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(MAX(run_id),0) FROM application_result_requests WHERE result_id=? AND request_id=?`, b.ResultID, requestID).Scan(&count, &runID); err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
		if count != 1 {
			return ErrResultCorrelation
		}
		var err error
		receipt, err = resultReceiptTx(ctx, tx, b.ResultID, runID, requestID)
		return err
	})
	return receipt, err
}
