package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// JudgeRequest retains one qualified command and its immutable admission evidence.
type JudgeRequest struct {
	ID, SessionID, TriggerCommentID int64
	Task, TicketKey, CommentID      string
	Source                          JudgeSource
	Configuration, Evidence, Report []byte
}

const judgeRequestColumns = `id,session_id,trigger_comment_id,task,ticket_key,comment_id,source_json,configuration,evidence,report`

func scanJudgeRequest(row interface{ Scan(...any) error }) (JudgeRequest, error) {
	var r JudgeRequest
	var source []byte
	err := row.Scan(&r.ID, &r.SessionID, &r.TriggerCommentID, &r.Task, &r.TicketKey, &r.CommentID, &source, &r.Configuration, &r.Evidence, &r.Report)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(source, &r.Source)
	return r, err
}

// AdmitJudgeRequest pins once; redelivery returns the original source and settings.
func (db *DB) AdmitJudgeRequest(ctx context.Context, r JudgeRequest) (JudgeRequest, error) {
	if r.Task == "" || r.TicketKey == "" || r.CommentID == "" || r.SessionID < 1 || r.Source.Task != r.Task || r.Source.Reference == "" || len(r.Configuration) > MaxResultInputBytes || len(r.Evidence) > 128<<20 {
		return JudgeRequest{}, errors.New("invalid judge request")
	}
	source, err := json.Marshal(r.Source)
	if err != nil {
		return JudgeRequest{}, err
	}
	var out JudgeRequest
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		old, e := scanJudgeRequest(tx.QueryRowContext(ctx, `SELECT `+judgeRequestColumns+` FROM judge_requests WHERE ticket_key=? AND comment_id=?`, r.TicketKey, r.CommentID))
		if e == nil {
			if old.Task != r.Task || old.SessionID != r.SessionID {
				return ErrResultCorrelation
			}
			out = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		var task string
		if e = tx.QueryRowContext(ctx, `SELECT task_ref FROM sessions WHERE id=?`, r.SessionID).Scan(&task); e != nil {
			return e
		}
		if task != r.Task {
			return ErrResultAuthority
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO judge_requests(task,ticket_key,comment_id,session_id,trigger_comment_id,source_json,configuration,evidence,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, r.Task, r.TicketKey, r.CommentID, r.SessionID, r.TriggerCommentID, source, r.Configuration, r.Evidence, FormatTime(nowUTC()))
		if e != nil {
			return e
		}
		id, e := res.LastInsertId()
		if e != nil {
			return e
		}
		out, e = scanJudgeRequest(tx.QueryRowContext(ctx, `SELECT `+judgeRequestColumns+` FROM judge_requests WHERE id=?`, id))
		return e
	})
	return out, err
}

// GetJudgeRequest reads a pinned advisory command by its durable identity.
func (db *DB) GetJudgeRequest(ctx context.Context, id int64) (JudgeRequest, error) {
	return scanJudgeRequest(db.sql.QueryRowContext(ctx, `SELECT `+judgeRequestColumns+` FROM judge_requests WHERE id=?`, id))
}

// FindJudgeRequest reads a command by qualified ticket and immutable comment identity.
func (db *DB) FindJudgeRequest(ctx context.Context, ticketKey, commentID string) (JudgeRequest, error) {
	return scanJudgeRequest(db.sql.QueryRowContext(ctx, `SELECT `+judgeRequestColumns+` FROM judge_requests WHERE ticket_key=? AND comment_id=?`, ticketKey, commentID))
}

// PendingJudgeRequests includes saved reports until their publication is reconciled by the caller.
func (db *DB) PendingJudgeRequests(ctx context.Context) ([]JudgeRequest, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+judgeRequestColumns+` FROM judge_requests ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JudgeRequest
	for rows.Next() {
		r, e := scanJudgeRequest(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveJudgeReport is append-only and accepts identical recovery writes.
func (db *DB) SaveJudgeReport(ctx context.Context, id int64, report []byte) error {
	if len(report) == 0 || len(report) > MaxResultSnapshotBytes {
		return errors.New("invalid judge report size")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var old []byte
		if err := tx.QueryRowContext(ctx, `SELECT report FROM judge_requests WHERE id=?`, id).Scan(&old); err != nil {
			return err
		}
		if old != nil {
			if bytes.Equal(old, report) {
				return nil
			}
			return ErrResultCorrelation
		}
		_, err := tx.ExecContext(ctx, `UPDATE judge_requests SET report=? WHERE id=?`, report, id)
		return err
	})
}

// JudgeAttempt is a separately attributed provider launch; at most two belong to a request.
type JudgeAttempt struct {
	ID                                        string
	RequestID                                 int64
	Ordinal                                   int
	Provider, Model, Thinking, State, Failure string
	ObservedModel, ProviderVersion            string
}

const judgeAttemptColumns = `id,request_id,ordinal,provider,model,thinking,state,failure,observed_model,provider_version`

func scanJudgeAttempt(row interface{ Scan(...any) error }) (JudgeAttempt, error) {
	var a JudgeAttempt
	err := row.Scan(&a.ID, &a.RequestID, &a.Ordinal, &a.Provider, &a.Model, &a.Thinking, &a.State, &a.Failure, &a.ObservedModel, &a.ProviderVersion)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return a, err
}

// StartJudgeAttempt allocates one primary or fallback advisory launch.
func (db *DB) StartJudgeAttempt(ctx context.Context, requestID int64, provider, model, thinking string) (JudgeAttempt, error) {
	if provider != "claude" && provider != "codex" {
		return JudgeAttempt{}, errors.New("invalid judge provider")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return JudgeAttempt{}, err
	}
	id := "judge-" + hex.EncodeToString(random[:])
	var a JudgeAttempt
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM judge_requests WHERE id=? AND report IS NULL`, requestID).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return ErrResultAuthority
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM judge_attempts WHERE request_id=? AND state='running'`, requestID).Scan(&active); err != nil {
			return err
		}
		if active != 0 {
			return ErrResultAuthority
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO judge_attempts(id,request_id,ordinal,provider,model,thinking,state,started_at) VALUES(?,?,(SELECT COALESCE(MAX(ordinal),0)+1 FROM judge_attempts WHERE request_id=?),?,?,?,'running',?)`, id, requestID, requestID, provider, model, thinking, FormatTime(nowUTC()))
		if err != nil {
			return err
		}
		a, err = scanJudgeAttempt(tx.QueryRowContext(ctx, `SELECT `+judgeAttemptColumns+` FROM judge_attempts WHERE id=?`, id))
		return err
	})
	return a, err
}

// GetJudgeAttempt reads one separately attributed provider launch.
func (db *DB) GetJudgeAttempt(ctx context.Context, id string) (JudgeAttempt, error) {
	return scanJudgeAttempt(db.sql.QueryRowContext(ctx, `SELECT `+judgeAttemptColumns+` FROM judge_attempts WHERE id=?`, id))
}

// ListJudgeAttempts returns a command's launches in allocation order.
func (db *DB) ListJudgeAttempts(ctx context.Context, id int64) ([]JudgeAttempt, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+judgeAttemptColumns+` FROM judge_attempts WHERE request_id=? ORDER BY ordinal`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JudgeAttempt
	for rows.Next() {
		a, e := scanJudgeAttempt(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FinishJudgeAttempt records execution settlement and fences its result writer.
func (db *DB) FinishJudgeAttempt(ctx context.Context, id, state, failure string) error {
	if state != "completed" && state != "failed" && state != "interrupted" {
		return errors.New("invalid judge attempt state")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		finishedAt := FormatTime(nowUTC())
		_, err := tx.ExecContext(ctx, `UPDATE judge_attempts SET state=?,failure=?,finished_at=? WHERE id=? AND state='running'`, state, failure, finishedAt, id)
		if err != nil {
			return err
		}

		if state == "interrupted" {
			// Recovered launches retain every recorded observation and valuation;
			// interruption settles execution without inventing complete usage.
			if _, err = tx.ExecContext(ctx, `UPDATE meter_attempts SET outcome='cancelled',finished_at=?,collection_status='unknown',diagnostic_codes_json=json_insert(diagnostic_codes_json,'$[#]','judge_execution_interrupted') WHERE judge_attempt_id=? AND finished_at IS NULL AND EXISTS(SELECT 1 FROM judge_attempts a WHERE a.id=meter_attempts.judge_attempt_id AND a.state IN ('failed','interrupted'))`, finishedAt, id); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE application_result_attempts SET active=0 WHERE judge_attempt_id=?`, id)
		return err
	})
}

// BindJudgeResultAttempt creates a report-only writer without touching source writers.
func (db *DB) BindJudgeResultAttempt(ctx context.Context, attemptID string) (ResultBinding, error) {
	var b ResultBinding
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var id int64
		var task string
		if err := tx.QueryRowContext(ctx, `SELECT q.id,q.task FROM judge_requests q JOIN judge_attempts a ON a.request_id=q.id WHERE a.id=? AND a.state='running' AND q.report IS NULL`, attemptID).Scan(&id, &task); err != nil {
			return ErrResultAuthority
		}
		old, e := readResultBinding(ctx, tx, 0, attemptID, false)
		if e == nil {
			if e = checkResultAccess(ctx, tx, old, true); e != nil {
				return e
			}
			b = old
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		work := fmt.Sprintf("judge:%d", id)
		hash, e := ResultContextHash(ResultAttempt{WorkContext: work, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO application_results(task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,workflow) VALUES(?,'judge',?,?,1,2,2,'judge') ON CONFLICT(task,mode,context_hash) DO NOTHING`, task, work, hash); e != nil {
			return e
		}
		var resultID int64
		if e = tx.QueryRowContext(ctx, `SELECT id FROM application_results WHERE task=? AND mode='judge' AND context_hash=? AND workflow='judge'`, task, hash).Scan(&resultID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE application_result_attempts SET active=0 WHERE result_id=?`, resultID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO application_result_attempts(run_id,attempt,result_id,active,judge_attempt_id) VALUES(NULL,?,?,1,?)`, attemptID, resultID, attemptID); e != nil {
			return e
		}
		b, e = readResultBinding(ctx, tx, 0, attemptID, true)
		return e
	})
	return b, err
}
func checkJudgeResultWriter(ctx context.Context, tx *sql.Tx, b ResultBinding) error {
	if b.RunID != 0 || b.Mode != "judge" || b.Workflow != "judge" || b.JudgeAttemptID != b.Attempt {
		return ErrResultAuthority
	}
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM judge_attempts a JOIN judge_requests q ON q.id=a.request_id WHERE a.id=? AND a.state='running' AND q.task=? AND ('judge:'||q.id)=? AND q.report IS NULL`, b.JudgeAttemptID, b.Task, b.WorkContext).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrResultAuthority
	}
	return nil
}

// JudgeResultBinding returns attributed historical access without restoring a writer.
func (db *DB) JudgeResultBinding(ctx context.Context, attemptID string) (ResultBinding, error) {
	var b ResultBinding
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		b, e = readResultBinding(ctx, tx, 0, attemptID, false)
		if e != nil {
			return e
		}
		if b.JudgeAttemptID != attemptID {
			return ErrResultAuthority
		}
		return nil
	})
	return b, err
}

// RecordJudgeInvocation retains native invocation observations without rewriting requested intent.
func (db *DB) RecordJudgeInvocation(ctx context.Context, id, observedModel, providerVersion string) error {
	if len(observedModel) > 4096 || len(providerVersion) > 4096 {
		return errors.New("judge invocation provenance exceeds limit")
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE judge_attempts SET observed_model=?,provider_version=? WHERE id=? AND state='running' AND observed_model='' AND provider_version=''`, observedModel, providerVersion, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrResultAuthority
	}
	return nil
}

// LockJudgeRequest serializes one advisory command across processes without
// taking source workflow ownership or holding a database transaction over I/O.
func (db *DB) LockJudgeRequest(ctx context.Context, ticketKey, commentID, lockDir string) (func() error, error) {
	if ticketKey == "" || commentID == "" {
		return nil, errors.New("judge command identity is required")
	}
	return db.lockParentStatus(ctx, ticketKey+"\x00"+commentID, lockDir, "judge-request", true)
}
