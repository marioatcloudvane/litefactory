package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// AdvisoryResultBinding grants one execution a private result writer fenced by its worker lease.
type AdvisoryResultBinding struct {
	ExecutionID string
	Fence       int64
}

// BindAdvisoryResultAttempt validates the active worker and original consent.
func (db *DB) BindAdvisoryResultAttempt(ctx context.Context, execution string) (AdvisoryResultBinding, error) {
	var b AdvisoryResultBinding
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		b.ExecutionID = execution
		if err := tx.QueryRowContext(ctx, `SELECT fence FROM durable_workflow_attempts WHERE execution_id=? AND admission_kind='advisory' AND state IN ('running','stop_requested')`, execution).Scan(&b.Fence); err != nil {
			return ErrResultAuthority
		}
		return checkAdvisoryResultWriter(ctx, tx, b)
	})
	return b, err
}

// AdvisoryResultBinding recovers historical execution attribution without writer authority.
func (db *DB) AdvisoryResultBinding(ctx context.Context, execution string) (AdvisoryResultBinding, error) {
	var b AdvisoryResultBinding
	b.ExecutionID = execution
	err := db.sql.QueryRowContext(ctx, `SELECT fence FROM durable_workflow_attempts WHERE execution_id=? AND admission_kind='advisory'`, execution).Scan(&b.Fence)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return b, err
}
func checkAdvisoryResultWriter(ctx context.Context, tx *sql.Tx, b AdvisoryResultBinding) error {
	var a AdvisoryAdmission
	var client string
	var repositoryID int64
	a, err := scanAdvisory(tx.QueryRowContext(ctx, `SELECT `+advisoryColumns+` FROM advisory_admissions WHERE execution_id=?`, b.ExecutionID))
	if err != nil {
		return err
	}
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM durable_workflow_attempts a JOIN durable_workflow_worker w ON w.attempt_id=a.id AND w.generation=a.fence WHERE a.execution_id=? AND a.fence=? AND a.admission_kind='advisory' AND a.state IN ('running','stop_requested') AND w.expires_at>? AND a.deadline>?`, b.ExecutionID, b.Fence, ownerActionTime(nowUTC()), ownerActionTime(nowUTC())).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrResultAuthority
	}
	if err = tx.QueryRowContext(ctx, `SELECT client_id FROM connector_grants WHERE id=?`, a.GrantID).Scan(&client); err != nil {
		return ErrResultAuthority
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM repos WHERE forge||':'||owner||'/'||name=?`, a.RepositoryRef).Scan(&repositoryID); err != nil {
		return ErrResultAuthority
	}
	principal := OwnerActionPrincipal{OwnerID: strconv.FormatInt(a.OwnerID, 10), OwnerGeneration: a.OwnerGeneration, Transport: "mcp", ClientID: client, GrantID: a.GrantID}
	return ownerActionAuthority(ctx, tx, principal, OwnerAction{Principal: principal, RepositoryID: repositoryID, RepositoryRef: a.RepositoryRef, Scope: "task:advisory"}, nowUTC())
}

// CheckAdvisoryResultWriter rechecks worker fence, lease and original consent.
func (db *DB) CheckAdvisoryResultWriter(ctx context.Context, b AdvisoryResultBinding) error {
	return db.withTx(ctx, func(tx *sql.Tx) error { return checkAdvisoryResultWriter(ctx, tx, b) })
}

// SubmitAdvisoryResult is append-only; equal retries recover the same execution receipt.
func (db *DB) SubmitAdvisoryResult(ctx context.Context, b AdvisoryResultBinding, request string, payload []byte) (bool, error) {
	if request == "" || len(request) > 256 || len(payload) == 0 || len(payload) > 128*1024 {
		return false, ErrConnectorInvalid
	}
	replay := false
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var digest string
		err := tx.QueryRowContext(ctx, `SELECT digest FROM task_advisory_result_receipts WHERE execution_id=? AND request_id=?`, b.ExecutionID, request).Scan(&digest)
		if err == nil {
			if digest != submissionDigest(payload) {
				return ErrResultCorrelation
			}
			replay = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err = checkAdvisoryResultWriter(ctx, tx, b); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO task_advisory_result_receipts(execution_id,request_id,digest,payload,created_at) VALUES(?,?,?,?,?)`, b.ExecutionID, request, submissionDigest(payload), string(payload), FormatTime(time.Now().UTC()))
		return err
	})
	return replay, err
}

// ReadAdvisoryResult only reads the admitted execution's immutable private receipt.
func (db *DB) ReadAdvisoryResult(ctx context.Context, b AdvisoryResultBinding, request string) (string, []byte, error) {
	var id, payload string
	query := `SELECT request_id,payload FROM task_advisory_result_receipts WHERE execution_id=?`
	args := []any{b.ExecutionID}
	if request != "" {
		query += ` AND request_id=?`
		args = append(args, request)
	}
	err := db.sql.QueryRowContext(ctx, query, args...).Scan(&id, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return id, []byte(payload), err
}

// StartAdvisoryInvocation pins the current pricing catalog and sole paid launch
// before invoking a provider. Raw observations do not imply a priced total.
func (db *DB) StartAdvisoryInvocation(ctx context.Context, b AdvisoryResultBinding, provider, model, thinking string, deadline time.Duration) error {
	if provider == "" || model == "" || deadline <= 0 {
		return ErrConnectorInvalid
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkAdvisoryResultWriter(ctx, tx, b); err != nil {
			return err
		}
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM durable_workflow_attempts WHERE execution_id=? AND fence=? AND admission_kind='advisory'`, b.ExecutionID, b.Fence).Scan(&state); err != nil {
			return err
		}
		if state != "running" {
			return ErrResultAuthority
		}
		var priceRevision int64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(id) FROM meter_price_revisions`).Scan(&priceRevision); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO task_advisory_invocations(execution_id,fence,price_revision_id,provider,model,thinking,deadline,started_at) VALUES(?,?,?,?,?,?,?,?)`, b.ExecutionID, b.Fence, priceRevision, provider, model, thinking, int64(deadline), FormatTime(nowUTC()))
		return err
	})
}

// RecordAdvisoryUsage retains bounded provider observations without workflow metering identity.
func (db *DB) RecordAdvisoryUsage(ctx context.Context, b AdvisoryResultBinding, event []byte) error {
	if len(event) == 0 || len(event) > 128*1024 {
		return ErrConnectorInvalid
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM task_advisory_invocations WHERE execution_id=? AND fence=? AND outcome='running'`, b.ExecutionID, b.Fence).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrResultAuthority
		}
		var retainedBytes int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(SUM(length(CAST(event AS BLOB))),0) FROM task_advisory_usage_events WHERE execution_id=?`, b.ExecutionID).Scan(&count, &retainedBytes); err != nil {
			return err
		}
		if count >= 4096 || retainedBytes+int64(len(event)) > 10<<20 {
			return ErrConnectorCapacity
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO task_advisory_usage_events(execution_id,sequence,event) VALUES(?,?,?)`, b.ExecutionID, count, string(event))
		return err
	})
}

// FinishAdvisoryInvocation retains provider observations without changing task state.
func (db *DB) FinishAdvisoryInvocation(ctx context.Context, b AdvisoryResultBinding, observedModel, version, outcome string, usage []byte) error {
	if len(observedModel) > 4096 || len(version) > 4096 || len(usage) > 128*1024 {
		return ErrConnectorInvalid
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE task_advisory_invocations SET observed_model=?,provider_version=?,outcome=?,usage=?,finished_at=? WHERE execution_id=? AND fence=? AND outcome='running'`, observedModel, version, outcome, string(usage), FormatTime(nowUTC()), b.ExecutionID, b.Fence)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrResultAuthority
	}
	return nil
}
