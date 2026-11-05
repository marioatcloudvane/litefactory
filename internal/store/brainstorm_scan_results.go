package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"
)

// BrainstormScanRun is the admitted, frozen identity of one scan as the worker reads it.
type BrainstormScanRun struct {
	ID, BrainstormID                    string
	ScanSeq, OwnerID, OwnerGeneration   int64
	RepositoryRef, RequestKey, Question string
	TeamVersion, TeamFiles, BasisCommit string
	GrantID, ConsentRevision            string
	JobID, ExecutionID, FailureReason   string
	Answer, Research, Questions, Usage  string
	Cost                                string
	ExchangeSeq                         int64
	CreatedAt, FinishedAt               time.Time
	// Purpose is "" (question) or "drafts".
	Purpose string
}

// BrainstormContextEntry is one kept entry as written into scan context files.
type BrainstormContextEntry struct {
	Seq                                              int64
	Kind, Text, DocumentName, IssueReference, ScanID string
	CreatedAt                                        time.Time
}

// BrainstormScanFinish is the worker's redacted projection of one scan outcome.
type BrainstormScanFinish struct {
	// State is the shared job state: succeeded, failed, cancelled or uncertain.
	State, Answer, Research, BasisCommit, Usage, Cost, FailureReason string
	Questions                                                        []string
	// Drafts is the validated draft set of a succeeded drafts scan; nil otherwise.
	Drafts *BrainstormDraftSetInput
}

// BrainstormScanResultBinding grants one scan execution a private result writer.
type BrainstormScanResultBinding struct {
	ExecutionID, ScanID string
	Fence               int64
}

const brainstormScanRunColumns = `id,brainstorm_id,scan_seq,owner_id,owner_generation,repository_ref,request_key,question,team_version,team_files,basis_commit,grant_id,consent_revision,job_id,execution_id,failure_reason,answer,research,questions,usage,cost,exchange_seq,created_at,finished_at,COALESCE(purpose,'')`

func scanBrainstormScanRun(row scanner) (BrainstormScanRun, error) {
	var s BrainstormScanRun
	var created, finished string
	err := row.Scan(&s.ID, &s.BrainstormID, &s.ScanSeq, &s.OwnerID, &s.OwnerGeneration, &s.RepositoryRef, &s.RequestKey, &s.Question, &s.TeamVersion, &s.TeamFiles, &s.BasisCommit, &s.GrantID, &s.ConsentRevision, &s.JobID, &s.ExecutionID, &s.FailureReason, &s.Answer, &s.Research, &s.Questions, &s.Usage, &s.Cost, &s.ExchangeSeq, &created, &finished, &s.Purpose)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	if s.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return s, err
	}
	if finished != "" {
		s.FinishedAt, err = time.Parse(time.RFC3339Nano, finished)
	}
	return s, err
}

// ReadBrainstormScanForAttempt resolves only the scan bound to this execution.
func (db *DB) ReadBrainstormScanForAttempt(ctx context.Context, a WorkflowAttempt) (BrainstormScanRun, error) {
	if a.AdmissionKind != "brainstorm_scan" {
		return BrainstormScanRun{}, ErrConnectorInvalid
	}
	return scanBrainstormScanRun(db.sql.QueryRowContext(ctx, `SELECT `+brainstormScanRunColumns+` FROM brainstorm_scans WHERE job_id=? AND execution_id=?`, a.JobID, a.ExecutionID))
}

// ReadBrainstormScanByExecution supports the runner and recovery without writer authority.
func (db *DB) ReadBrainstormScanByExecution(ctx context.Context, execution string) (BrainstormScanRun, error) {
	return scanBrainstormScanRun(db.sql.QueryRowContext(ctx, `SELECT `+brainstormScanRunColumns+` FROM brainstorm_scans WHERE execution_id=?`, execution))
}

// BrainstormScanContext reads every kept entry of a brainstorm in seq order and
// every earlier finished scan, for the scan's bounded digest and context files.
func (db *DB) BrainstormScanContext(ctx context.Context, brainstormID, scanID string) ([]BrainstormContextEntry, []BrainstormScanRun, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT seq,kind,text,document_name,issue_reference,scan_id,created_at FROM brainstorm_entries WHERE brainstorm_id=? ORDER BY seq`, brainstormID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var entries []BrainstormContextEntry
	for rows.Next() {
		var e BrainstormContextEntry
		var created string
		if err = rows.Scan(&e.Seq, &e.Kind, &e.Text, &e.DocumentName, &e.IssueReference, &e.ScanID, &created); err != nil {
			return nil, nil, err
		}
		if e.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, nil, err
		}
		entries = append(entries, e)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	scans, err := db.sql.QueryContext(ctx, `SELECT `+brainstormScanRunColumns+` FROM brainstorm_scans WHERE brainstorm_id=? AND id<>? AND exchange_seq>0 ORDER BY scan_seq`, brainstormID, scanID)
	if err != nil {
		return nil, nil, err
	}
	defer scans.Close()
	var earlier []BrainstormScanRun
	for scans.Next() {
		s, e := scanBrainstormScanRun(scans)
		if e != nil {
			return nil, nil, e
		}
		earlier = append(earlier, s)
	}
	return entries, earlier, scans.Err()
}

// BrainstormState reads whether the brainstorm is still open.
func (db *DB) BrainstormState(ctx context.Context, brainstormID string) (string, error) {
	var state string
	err := db.sql.QueryRowContext(ctx, `SELECT state FROM brainstorms WHERE id=?`, brainstormID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return state, err
}

// FinishBrainstormScan fences the worker slot and settles the scan, its job and
// attempt, its exchange entry and open questions in one transaction. Recovery
// skips the deadline check, as for every receipt-only recovery.
func (db *DB) FinishBrainstormScan(ctx context.Context, a WorkflowAttempt, f BrainstormScanFinish, at time.Time, recovery bool) error {
	if a.AdmissionKind != "brainstorm_scan" || (f.State != "succeeded" && f.State != "failed" && f.State != "uncertain" && f.State != "cancelled") {
		return ErrConnectorInvalid
	}
	if len(f.Answer)+len(f.Research)+len(f.Usage) > 128*1024 || !utf8.ValidString(f.Answer+f.Research+f.FailureReason+f.Usage+f.Cost) || (f.Research != "" && !json.Valid([]byte(f.Research))) {
		return ErrConnectorInvalid
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var active, generation int64
		if err := tx.QueryRowContext(ctx, `SELECT attempt_id,generation FROM durable_workflow_worker WHERE singleton=1`).Scan(&active, &generation); err != nil {
			return err
		}
		if active != a.ID || generation != a.Fence {
			return ErrWorkflowFenced
		}
		current, err := scanWorkflowAttempt(tx.QueryRowContext(ctx, `SELECT `+workflowAttemptColumns+` FROM durable_workflow_attempts WHERE id=?`, a.ID))
		if err != nil {
			return err
		}
		if current.AdmissionKind != "brainstorm_scan" || (current.State != "running" && current.State != "stop_requested") || current.Fence != a.Fence || (!recovery && !at.Before(current.Deadline)) {
			return ErrWorkflowFenced
		}
		scan, err := scanBrainstormScanRun(tx.QueryRowContext(ctx, `SELECT `+brainstormScanRunColumns+` FROM brainstorm_scans WHERE job_id=? AND execution_id=?`, current.JobID, current.ExecutionID))
		if err != nil {
			return err
		}
		if err = settleBrainstormScanMeterTx(ctx, tx, scan.ID, f.State, at); err != nil {
			return err
		}
		stamp := ownerActionTime(at)
		if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_attempts SET state=?,finished_at=? WHERE id=?`, f.State, stamp, a.ID); err != nil {
			return err
		}
		summary := f.State
		if f.State != "succeeded" && f.FailureReason != "" {
			summary = f.FailureReason
		}
		if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_jobs SET state=?,revision=revision+1,summary=?,updated_at=? WHERE id=? AND admission_kind='brainstorm_scan'`, f.State, summary, stamp, current.JobID); err != nil {
			return err
		}
		questions := "[]"
		exchange := int64(0)
		if f.State == "succeeded" {
			raw, e := json.Marshal(append([]string{}, f.Questions...))
			if e != nil {
				return e
			}
			questions = string(raw)
			var state string
			if err = tx.QueryRowContext(ctx, `SELECT state FROM brainstorms WHERE id=?`, scan.BrainstormID).Scan(&state); err != nil {
				return err
			}
			if state == "open" {
				if exchange, err = appendBrainstormEntryTx(ctx, tx, scan.BrainstormID, "exchange", "Question: "+scan.Question+"\n\nAnswer: "+f.Answer, scan.ID, "scan:"+scan.ID, at); err != nil {
					return err
				}
				for i, q := range f.Questions {
					if _, err = appendBrainstormEntryTx(ctx, tx, scan.BrainstormID, "open_question", q, scan.ID, "scan:"+scan.ID+":question:"+strconv.Itoa(i+1), at); err != nil {
						return err
					}
				}
				if _, err = tx.ExecContext(ctx, `UPDATE brainstorms SET revision=revision+1,last_activity_at=? WHERE id=?`, stamp, scan.BrainstormID); err != nil {
					return err
				}
			}
			if scan.Purpose == BrainstormPurposeDrafts {
				if f.Drafts == nil {
					return ErrConnectorInvalid
				}
				if err = insertBrainstormDraftSetTx(ctx, tx, scan.BrainstormID, scan.ID, *f.Drafts, at); err != nil {
					return err
				}
			}
		}
		answer, research, reason := f.Answer, f.Research, f.FailureReason
		if f.State != "succeeded" {
			answer, research = "", ""
		} else {
			reason = ""
		}
		commit := scan.BasisCommit
		if f.BasisCommit != "" {
			commit = f.BasisCommit
		}
		if _, err = tx.ExecContext(ctx, `UPDATE brainstorm_scans SET answer=?,research=?,questions=?,usage=?,cost=?,failure_reason=?,basis_commit=?,exchange_seq=?,finished_at=? WHERE id=?`, answer, research, questions, f.Usage, f.Cost, reason, commit, exchange, stamp, scan.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE durable_workflow_worker SET attempt_id=0,expires_at='' WHERE singleton=1 AND attempt_id=? AND generation=?`, a.ID, a.Fence)
		return err
	})
}

func appendBrainstormEntryTx(ctx context.Context, tx *sql.Tx, brainstorm, kind, text, scanID, key string, at time.Time) (int64, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return 0, err
	}
	var seq int64
	if err := tx.QueryRowContext(ctx, `UPDATE brainstorms SET next_seq=next_seq+1 WHERE id=? RETURNING next_seq-1`, brainstorm).Scan(&seq); err != nil {
		return 0, err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO brainstorm_entries(id,brainstorm_id,seq,kind,text,scan_id,request_key,digest,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, hex.EncodeToString(random[:]), brainstorm, seq, kind, text, scanID, key, submissionDigest([]byte(kind+"\x00"+text)), ownerActionTime(at))
	return seq, err
}

// BindBrainstormScanResult validates the active worker and original consent.
func (db *DB) BindBrainstormScanResult(ctx context.Context, execution string) (BrainstormScanResultBinding, error) {
	var b BrainstormScanResultBinding
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		b.ExecutionID = execution
		if err := tx.QueryRowContext(ctx, `SELECT a.fence,s.id FROM durable_workflow_attempts a JOIN brainstorm_scans s ON s.execution_id=a.execution_id WHERE a.execution_id=? AND a.admission_kind='brainstorm_scan' AND a.state IN ('running','stop_requested')`, execution).Scan(&b.Fence, &b.ScanID); err != nil {
			return ErrResultAuthority
		}
		return checkBrainstormScanResultWriter(ctx, tx, b)
	})
	return b, err
}

// BrainstormScanResultBinding recovers historical attribution without writer authority.
func (db *DB) BrainstormScanResultBinding(ctx context.Context, execution string) (BrainstormScanResultBinding, error) {
	b := BrainstormScanResultBinding{ExecutionID: execution}
	err := db.sql.QueryRowContext(ctx, `SELECT a.fence,s.id FROM durable_workflow_attempts a JOIN brainstorm_scans s ON s.execution_id=a.execution_id WHERE a.execution_id=? AND a.admission_kind='brainstorm_scan'`, execution).Scan(&b.Fence, &b.ScanID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return b, err
}

func checkBrainstormScanResultWriter(ctx context.Context, tx *sql.Tx, b BrainstormScanResultBinding) error {
	s, err := scanBrainstormScanRun(tx.QueryRowContext(ctx, `SELECT `+brainstormScanRunColumns+` FROM brainstorm_scans WHERE id=? AND execution_id=?`, b.ScanID, b.ExecutionID))
	if err != nil {
		return ErrResultAuthority
	}
	now := ownerActionTime(nowUTC())
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM durable_workflow_attempts a JOIN durable_workflow_worker w ON w.attempt_id=a.id AND w.generation=a.fence WHERE a.execution_id=? AND a.fence=? AND a.admission_kind='brainstorm_scan' AND a.state IN ('running','stop_requested') AND w.expires_at>? AND a.deadline>?`, b.ExecutionID, b.Fence, now, now).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrResultAuthority
	}
	var client string
	if err = tx.QueryRowContext(ctx, `SELECT client_id FROM connector_grants WHERE id=?`, s.GrantID).Scan(&client); err != nil {
		return ErrResultAuthority
	}
	return brainstormConsentTx(ctx, tx, OwnerActionPrincipal{OwnerID: strconv.FormatInt(s.OwnerID, 10), OwnerGeneration: s.OwnerGeneration, Transport: "mcp", ClientID: client, GrantID: s.GrantID}, s.RepositoryRef, nowUTC())
}

// CheckBrainstormConsent reports whether the principal's live grant holds
// repository:brainstorm for the repository through its explicit repository list.
func (db *DB) CheckBrainstormConsent(ctx context.Context, p OwnerActionPrincipal, repositoryRef string, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error { return brainstormConsentTx(ctx, tx, p, repositoryRef, at) })
}

func brainstormConsentTx(ctx context.Context, tx *sql.Tx, p OwnerActionPrincipal, repositoryRef string, at time.Time) error {
	var owner string
	var generation int64
	err := tx.QueryRowContext(ctx, `SELECT CAST(o.owner_id AS TEXT),o.generation FROM connector_owner o JOIN identities i ON i.forge='github' AND i.role='owner' AND i.user_id=o.owner_id WHERE o.singleton=1`).Scan(&owner, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConnectorOwner
	}
	if err != nil {
		return err
	}
	if p.Transport != "mcp" || p.OwnerID != owner || p.OwnerGeneration != generation {
		return ErrConnectorOwner
	}
	var scope string
	err = tx.QueryRowContext(ctx, `SELECT scope FROM connector_grants WHERE id=? AND client_id=? AND owner_id=? AND generation=? AND revoked_at IS NULL AND expires_at>?`, p.GrantID, p.ClientID, owner, generation, FormatTime(at)).Scan(&scope)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConnectorInvalid
	}
	if err != nil {
		return err
	}
	if !ConnectorScopeHas(scope, "repository:brainstorm") {
		return ErrConnectorScope
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM connector_grant_repositories c JOIN repos r ON r.id=c.repo_id AND r.forge||':'||r.owner||'/'||r.name=c.repository_ref WHERE c.grant_id=? AND c.repository_ref=?`, p.GrantID, repositoryRef).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrConnectorScope
	}
	return nil
}

// CheckBrainstormScanResultWriter rechecks worker fence, lease and original consent.
func (db *DB) CheckBrainstormScanResultWriter(ctx context.Context, b BrainstormScanResultBinding) error {
	return db.withTx(ctx, func(tx *sql.Tx) error { return checkBrainstormScanResultWriter(ctx, tx, b) })
}

// SetBrainstormScanCommit records the commit a running scan reads before launch.
func (db *DB) SetBrainstormScanCommit(ctx context.Context, b BrainstormScanResultBinding, commit string) error {
	if len(commit) > 64 {
		return ErrConnectorInvalid
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkBrainstormScanResultWriter(ctx, tx, b); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE brainstorm_scans SET basis_commit=? WHERE id=? AND execution_id=?`, commit, b.ScanID, b.ExecutionID)
		return err
	})
}

// SubmitBrainstormScanResult is append-only; an equal retry recovers the same receipt.
func (db *DB) SubmitBrainstormScanResult(ctx context.Context, b BrainstormScanResultBinding, request string, payload []byte) (bool, error) {
	if request == "" || len(request) > 256 || len(payload) == 0 || len(payload) > 128*1024 {
		return false, ErrConnectorInvalid
	}
	replay := false
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var digest, existing string
		err := tx.QueryRowContext(ctx, `SELECT request_id,digest FROM brainstorm_scan_receipts WHERE scan_id=?`, b.ScanID).Scan(&existing, &digest)
		if err == nil {
			if existing != request || digest != submissionDigest(payload) {
				return ErrResultCorrelation
			}
			replay = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err = checkBrainstormScanResultWriter(ctx, tx, b); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO brainstorm_scan_receipts(scan_id,execution_id,request_id,digest,payload,created_at) VALUES(?,?,?,?,?,?)`, b.ScanID, b.ExecutionID, request, submissionDigest(payload), string(payload), FormatTime(nowUTC()))
		return err
	})
	return replay, err
}

// ReadBrainstormScanResult reads the execution's immutable receipt only.
func (db *DB) ReadBrainstormScanResult(ctx context.Context, b BrainstormScanResultBinding, request string) (string, []byte, error) {
	var id, payload string
	query := `SELECT request_id,payload FROM brainstorm_scan_receipts WHERE scan_id=? AND execution_id=?`
	args := []any{b.ScanID, b.ExecutionID}
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

// StartBrainstormScanMeterAttempt attributes one real scan execution to the shared ledger.
func (db *DB) StartBrainstormScanMeterAttempt(ctx context.Context, b BrainstormScanResultBinding, provider string) (MeterAttempt, error) {
	if meterIdentity(b.ScanID, provider) != nil {
		return MeterAttempt{}, ErrConnectorInvalid
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return MeterAttempt{}, err
	}
	a := MeterAttempt{ID: hex.EncodeToString(random[:]), CodingAgent: provider}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkBrainstormScanResultWriter(ctx, tx, b); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `INSERT INTO meter_attempts(id,brainstorm_scan_id,ordinal,coding_agent,started_at,outcome,collection_status,diagnostic_codes_json,price_revision_id) VALUES(?,?,1,?,?,'running','unknown','[]',(SELECT MAX(id) FROM meter_price_revisions)) RETURNING ordinal,price_revision_id`, a.ID, b.ScanID, provider, FormatTime(nowUTC())).Scan(&a.Ordinal, &a.PriceRevisionID)
	})
	return a, err
}

// settleBrainstormScanMeterTx closes crash or stop leftovers without inventing completion.
func settleBrainstormScanMeterTx(ctx context.Context, tx *sql.Tx, scanID, state string, at time.Time) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM meter_attempts WHERE brainstorm_scan_id=? AND finished_at IS NULL`, scanID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	outcome := "failure"
	if state == "cancelled" {
		outcome = "cancelled"
	}
	if err = meterAttemptDiagnostic(ctx, tx, id, "brainstorm_scan_completion_unverified"); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE meter_attempts SET finished_at=?,outcome=? WHERE id=? AND finished_at IS NULL`, FormatTime(at), outcome, id)
	return err
}
