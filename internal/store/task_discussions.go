package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DiscussionScope binds private records to one installation owner and task.
type DiscussionScope struct {
	OwnerID, OwnerGeneration int64
	RepositoryRef, TaskRef   string
}

// DiscussionExchange is one completed private exchange. Active context is bounded.
type DiscussionExchange struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	JobID    string `json:"job_id"`
	Basis    string `json:"basis"`
}

// DiscussionConversation retains only the bounded completed context and revision.
type DiscussionConversation struct {
	DiscussionScope
	ID, ClientID, RequestKey, ActiveJob string
	Revision                            int64
	Exchanges                           []DiscussionExchange
	OmittedCount                        int64
	CreatedAt, UpdatedAt                time.Time
}

// AdvisoryAdmission freezes evidence, consent and input before any paid execution.
type AdvisoryAdmission struct {
	DiscussionScope
	ID, ConversationID, Operation, RequestKey, Digest  string
	Basis, Context, Question, GrantID, ConsentRevision string
	TargetGuard                                        string
	ConversationRevision                               int64
	JobID, ExecutionID                                 string
	CreatedAt                                          time.Time
	Job                                                WorkflowJob
}

// DiscussionConclusion freezes exact safe publication bytes independently of a job.
type DiscussionConclusion struct {
	DiscussionScope
	ID, ConversationID, ConclusionKey, Digest, Summary, Rendered   string
	SourceJob, GrantID, ConsentRevision, State, Reason, CommentURL string
	NextAttemptAt                                                  time.Time
	CreatedAt, UpdatedAt                                           time.Time
}

const discussionColumns = `id,owner_id,owner_generation,repository_ref,task_ref,client_id,request_key,revision,active_job,exchanges,omitted_count,created_at,updated_at`
const advisoryColumns = `id,conversation_id,owner_id,owner_generation,repository_ref,task_ref,operation,request_key,digest,basis,context,question,grant_id,consent_revision,conversation_revision,job_id,execution_id,created_at,target_guard`
const conclusionColumns = `id,conversation_id,owner_id,owner_generation,repository_ref,task_ref,conclusion_key,digest,summary,rendered,source_job,grant_id,consent_revision,state,reason,comment_url,created_at,updated_at,next_attempt_at`

func discussionScopeValid(s DiscussionScope) bool {
	return s.OwnerID > 0 && s.OwnerGeneration > 0 && s.RepositoryRef != "" && s.TaskRef != "" && len(s.RepositoryRef) <= 1024 && len(s.TaskRef) <= 1024
}
func scanDiscussion(row scanner) (DiscussionConversation, error) {
	var c DiscussionConversation
	var exchanges, created, updated string
	err := row.Scan(&c.ID, &c.OwnerID, &c.OwnerGeneration, &c.RepositoryRef, &c.TaskRef, &c.ClientID, &c.RequestKey, &c.Revision, &c.ActiveJob, &exchanges, &c.OmittedCount, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal([]byte(exchanges), &c.Exchanges); err != nil {
		return c, err
	}
	c.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err == nil {
		c.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	}
	return c, err
}
func scanAdvisory(row scanner) (AdvisoryAdmission, error) {
	var a AdvisoryAdmission
	var created string
	err := row.Scan(&a.ID, &a.ConversationID, &a.OwnerID, &a.OwnerGeneration, &a.RepositoryRef, &a.TaskRef, &a.Operation, &a.RequestKey, &a.Digest, &a.Basis, &a.Context, &a.Question, &a.GrantID, &a.ConsentRevision, &a.ConversationRevision, &a.JobID, &a.ExecutionID, &created, &a.TargetGuard)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return a, err
}
func scanConclusion(row scanner) (DiscussionConclusion, error) {
	var c DiscussionConclusion
	var created, updated, next string
	err := row.Scan(&c.ID, &c.ConversationID, &c.OwnerID, &c.OwnerGeneration, &c.RepositoryRef, &c.TaskRef, &c.ConclusionKey, &c.Digest, &c.Summary, &c.Rendered, &c.SourceJob, &c.GrantID, &c.ConsentRevision, &c.State, &c.Reason, &c.CommentURL, &created, &updated, &next)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err == nil {
		c.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	}
	if err == nil && next != "" {
		c.NextAttemptAt, err = time.Parse(time.RFC3339Nano, next)
	}
	return c, err
}
func discussionScopeArgs(s DiscussionScope) []any {
	return []any{s.OwnerID, s.OwnerGeneration, s.RepositoryRef, s.TaskRef}
}

const discussionScopeWhere = `owner_id=? AND owner_generation=? AND repository_ref=? AND task_ref=?`

// OpenDiscussion registers an empty conversation; equal retry keys recover it.
func (db *DB) OpenDiscussion(ctx context.Context, c DiscussionConversation) (DiscussionConversation, error) {
	if !discussionScopeValid(c.DiscussionScope) || c.ID == "" || c.RequestKey == "" || len(c.RequestKey) > 256 || c.ClientID == "" || c.CreatedAt.IsZero() {
		return c, ErrConnectorInvalid
	}
	var out DiscussionConversation
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		args := append(discussionScopeArgs(c.DiscussionScope), c.RequestKey)
		old, err := scanDiscussion(tx.QueryRowContext(ctx, `SELECT `+discussionColumns+` FROM task_discussions WHERE `+discussionScopeWhere+` AND request_key=?`, args...))
		if err == nil {
			out = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO task_discussions(`+discussionColumns+`) VALUES(?,?,?,?,?,?,?,0,'','[]',0,?,?)`, c.ID, c.OwnerID, c.OwnerGeneration, c.RepositoryRef, c.TaskRef, c.ClientID, c.RequestKey, ownerActionTime(c.CreatedAt), ownerActionTime(c.CreatedAt))
		out = c
		out.Revision = 0
		out.ActiveJob = ""
		out.Exchanges = []DiscussionExchange{}
		out.UpdatedAt = c.CreatedAt
		return err
	})
	return out, err
}

// ReadDiscussion enforces exact owner generation, repository and task scope.
func (db *DB) ReadDiscussion(ctx context.Context, s DiscussionScope, id string) (DiscussionConversation, error) {
	args := append(discussionScopeArgs(s), id)
	return scanDiscussion(db.sql.QueryRowContext(ctx, `SELECT `+discussionColumns+` FROM task_discussions WHERE `+discussionScopeWhere+` AND id=?`, args...))
}

// AdvisoryByRequest returns a prior admission before current-target resolution.
func (db *DB) AdvisoryByRequest(ctx context.Context, s DiscussionScope, operation, key string) (AdvisoryAdmission, error) {
	args := append(discussionScopeArgs(s), operation, key)
	return scanAdvisory(db.sql.QueryRowContext(ctx, `SELECT `+advisoryColumns+` FROM advisory_admissions WHERE `+discussionScopeWhere+` AND operation=? AND request_key=?`, args...))
}

// ReadAdvisory reads a private scoped admission and its retained job projection.
func (db *DB) ReadAdvisory(ctx context.Context, s DiscussionScope, id string) (AdvisoryAdmission, error) {
	args := append(discussionScopeArgs(s), id, id)
	a, err := scanAdvisory(db.sql.QueryRowContext(ctx, `SELECT `+advisoryColumns+` FROM advisory_admissions WHERE `+discussionScopeWhere+` AND (id=? OR job_id=?)`, args...))
	if err == nil {
		a.Job, err = db.ReadWorkflowJob(ctx, a.JobID)
	}
	return a, err
}

// AdmitAdvisory reserves conversation order and admits one fixed advisory execution.
// The caller verifies current authority and target before this storage transaction.
func (db *DB) AdmitAdvisory(ctx context.Context, a AdvisoryAdmission) (AdvisoryAdmission, error) {
	return db.admitAdvisory(ctx, nil, a)
}

// AdmitAuthorizedAdvisory checks live owner consent in the admission transaction.
func (db *DB) AdmitAuthorizedAdvisory(ctx context.Context, actor OwnerActionPrincipal, a AdvisoryAdmission) (AdvisoryAdmission, error) {
	return db.admitAdvisory(ctx, &actor, a)
}
func (db *DB) admitAdvisory(ctx context.Context, actor *OwnerActionPrincipal, a AdvisoryAdmission) (AdvisoryAdmission, error) {
	if !discussionScopeValid(a.DiscussionScope) || a.ID == "" || a.ConversationID == "" || a.JobID == "" || a.ExecutionID == "" || a.RequestKey == "" || len(a.RequestKey) > 256 || a.Digest == "" || a.GrantID == "" || (a.Operation != "ask" && a.Operation != "judge") || !json.Valid([]byte(a.Basis)) || !json.Valid([]byte(a.Context)) || !utf8.ValidString(a.Question) || len(a.Question) > 32768 || len(a.Context) > 32768 || a.CreatedAt.IsZero() {
		return a, ErrConnectorInvalid
	}
	out := a
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if actor != nil {
			captured, err := captureDiscussionConsentTx(ctx, tx, *actor, a.DiscussionScope, a.GrantID, "task:advisory", a.ConsentRevision, a.CreatedAt)
			if err != nil {
				return err
			}
			a.ConsentRevision = captured
		}
		args := append(discussionScopeArgs(a.DiscussionScope), a.Operation, a.RequestKey)
		old, err := scanAdvisory(tx.QueryRowContext(ctx, `SELECT `+advisoryColumns+` FROM advisory_admissions WHERE `+discussionScopeWhere+` AND operation=? AND request_key=?`, args...))
		if err == nil {
			if old.Digest != a.Digest {
				return ErrWorkflowConflict
			}
			out = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		args = append(discussionScopeArgs(a.DiscussionScope), a.ConversationID)
		c, err := scanDiscussion(tx.QueryRowContext(ctx, `SELECT `+discussionColumns+` FROM task_discussions WHERE `+discussionScopeWhere+` AND id=?`, args...))
		if err != nil {
			return err
		}
		if a.TargetGuard != "" {
			guard, e := taskAdvisoryGuardTx(ctx, tx, a.TaskRef)
			if e != nil {
				return e
			}
			if guard != a.TargetGuard {
				return ErrWorkflowRevision
			}
		}
		if c.Revision != a.ConversationRevision || c.ActiveJob != "" {
			return ErrWorkflowRevision
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM durable_workflow_jobs WHERE owner_id=? AND owner_generation=? AND admission_kind='advisory' AND state IN ('queued','running','stop_requested')`, strconv.FormatInt(a.OwnerID, 10), a.OwnerGeneration).Scan(&count); err != nil {
			return err
		}
		if count >= 50 {
			return ErrConnectorCapacity
		}
		out = a
		_, err = tx.ExecContext(ctx, `INSERT INTO advisory_admissions(`+advisoryColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.ConversationID, a.OwnerID, a.OwnerGeneration, a.RepositoryRef, a.TaskRef, a.Operation, a.RequestKey, a.Digest, a.Basis, a.Context, a.Question, a.GrantID, a.ConsentRevision, a.ConversationRevision, a.JobID, a.ExecutionID, ownerActionTime(a.CreatedAt), a.TargetGuard)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO durable_workflow_jobs(`+workflowJobColumns+`,admission_kind) VALUES(?,?,?,?,'queued',0,'{}','[]','','',?,?,'advisory')`, a.JobID, strconv.FormatInt(a.OwnerID, 10), a.OwnerGeneration, a.Basis, ownerActionTime(a.CreatedAt), ownerActionTime(a.CreatedAt))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO durable_workflow_attempts(job_id,proposal_id,execution_id,state,queued_at,admission_kind,advisory_id) VALUES(?,NULL,?,'queued',?,'advisory',?)`, a.JobID, a.ExecutionID, ownerActionTime(a.CreatedAt), a.ID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE task_discussions SET active_job=?,revision=revision+1,updated_at=? WHERE id=?`, a.JobID, ownerActionTime(a.CreatedAt), a.ConversationID)
		return err
	})
	return out, err
}

// BoundedDiscussionContext keeps newest whole exchanges and reserves the question.
func BoundedDiscussionContext(exchanges []DiscussionExchange, question string) ([]DiscussionExchange, int, error) {
	if !utf8.ValidString(question) || len(question) > 32768 {
		return nil, 0, ErrConnectorInvalid
	}
	start := 0
	if len(exchanges) > 10 {
		start = len(exchanges) - 10
	}
	for start < len(exchanges) {
		raw, err := json.Marshal(exchanges[start:])
		if err != nil {
			return nil, 0, err
		}
		if len(raw)+len(question) <= 32768 {
			break
		}
		start++
	}
	out := append([]DiscussionExchange{}, exchanges[start:]...)
	return out, start, nil
}

// CancelAdvisory cancels queued work or records a best-effort running stop.
// The bool reports that a terminal completion already won the race.
func (db *DB) CancelAdvisory(ctx context.Context, s DiscussionScope, id string, at time.Time) (AdvisoryAdmission, bool, error) {
	var out AdvisoryAdmission
	var won bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		args := append(discussionScopeArgs(s), id, id)
		a, err := scanAdvisory(tx.QueryRowContext(ctx, `SELECT `+advisoryColumns+` FROM advisory_admissions WHERE `+discussionScopeWhere+` AND (id=? OR job_id=?)`, args...))
		if err != nil {
			return err
		}
		out = a
		j, err := scanWorkflowJob(tx.QueryRowContext(ctx, `SELECT `+workflowJobColumns+` FROM durable_workflow_jobs WHERE id=?`, a.JobID))
		if err != nil {
			return err
		}
		switch j.State {
		case "queued":
			if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_attempts SET state='cancelled',finished_at=? WHERE job_id=? AND state='queued'`, ownerActionTime(at), a.JobID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_jobs SET state='cancelled',revision=revision+1,updated_at=? WHERE id=?`, ownerActionTime(at), a.JobID); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE task_discussions SET active_job='',revision=revision+1,updated_at=? WHERE id=? AND active_job=?`, ownerActionTime(at), a.ConversationID, a.JobID)
			j.State = "cancelled"
		case "running":
			if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_attempts SET state='stop_requested' WHERE job_id=? AND state='running'`, a.JobID); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE durable_workflow_jobs SET state='stop_requested',updated_at=? WHERE id=?`, ownerActionTime(at), a.JobID)
			j.State = "stop_requested"
		case "succeeded", "failed":
			won = true
		case "stop_requested", "cancelled", "uncertain":
		}
		out.Job = j
		return err
	})
	return out, won, err
}

// ReserveDiscussionConclusion binds a unique conclusion identity to frozen content.
func (db *DB) ReserveDiscussionConclusion(ctx context.Context, c DiscussionConclusion) (DiscussionConclusion, error) {
	if !discussionScopeValid(c.DiscussionScope) || c.ID == "" || c.ConversationID == "" || c.ConclusionKey == "" || len(c.ConclusionKey) > 256 || c.Digest == "" || !json.Valid([]byte(c.Summary)) || c.Rendered == "" || !utf8.ValidString(c.Rendered) || len(c.Rendered) > 65536 || c.GrantID == "" || c.CreatedAt.IsZero() {
		return c, ErrConnectorInvalid
	}
	out := c
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var actor OwnerActionPrincipal
		if json.Unmarshal([]byte(c.ConsentRevision), &actor) == nil && actor.Transport != "" {
			if actor.Transport != "mcp" {
				return ErrConnectorInvalid
			}
			captured, err := captureDiscussionConsentTx(ctx, tx, actor, c.DiscussionScope, c.GrantID, "task:discussion-publish", c.ConsentRevision, c.CreatedAt)
			if err != nil {
				return err
			}
			c.ConsentRevision = captured
		}

		args := append(discussionScopeArgs(c.DiscussionScope), c.ConversationID, c.ConclusionKey)
		old, err := scanConclusion(tx.QueryRowContext(ctx, `SELECT `+conclusionColumns+` FROM discussion_conclusions WHERE `+discussionScopeWhere+` AND conversation_id=? AND conclusion_key=?`, args...))
		if err == nil {
			if old.Digest != c.Digest {
				return ErrWorkflowConflict
			}
			out = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		args = append(discussionScopeArgs(c.DiscussionScope), c.ConversationID)
		if _, err = scanDiscussion(tx.QueryRowContext(ctx, `SELECT `+discussionColumns+` FROM task_discussions WHERE `+discussionScopeWhere+` AND id=?`, args...)); err != nil {
			return err
		}
		if c.SourceJob != "" {
			args = append(discussionScopeArgs(c.DiscussionScope), c.SourceJob, c.ConversationID)
			if _, err = scanAdvisory(tx.QueryRowContext(ctx, `SELECT `+advisoryColumns+` FROM advisory_admissions WHERE `+discussionScopeWhere+` AND job_id=? AND conversation_id=?`, args...)); err != nil {
				return err
			}
		}
		out = c
		_, err = tx.ExecContext(ctx, `INSERT INTO discussion_conclusions(`+conclusionColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'pending','','',?,?,'')`, c.ID, c.ConversationID, c.OwnerID, c.OwnerGeneration, c.RepositoryRef, c.TaskRef, c.ConclusionKey, c.Digest, c.Summary, c.Rendered, c.SourceJob, c.GrantID, c.ConsentRevision, ownerActionTime(c.CreatedAt), ownerActionTime(c.CreatedAt))
		out.State = "pending"
		out.UpdatedAt = c.CreatedAt
		return err
	})
	return out, err
}

// ReadDiscussionConclusion reads only the exact scoped publication record.
func (db *DB) ReadDiscussionConclusion(ctx context.Context, s DiscussionScope, id string) (DiscussionConclusion, error) {
	args := append(discussionScopeArgs(s), id)
	return scanConclusion(db.sql.QueryRowContext(ctx, `SELECT `+conclusionColumns+` FROM discussion_conclusions WHERE `+discussionScopeWhere+` AND id=?`, args...))
}

// UpdateDiscussionConclusion uses state fencing; uncertain sends cannot be requeued.
func (db *DB) UpdateDiscussionConclusion(ctx context.Context, s DiscussionScope, id, expected, state, reason, commentURL string, at time.Time) error {
	valid := expected == "pending" && (state == "sending" || state == "failed") || expected == "sending" && (state == "delivered" || state == "failed" || state == "uncertain") || expected == "uncertain" && state == "delivered" || expected == "failed" && state == "pending"
	if !valid || (state == "delivered" && !strings.HasPrefix(commentURL, "https://")) {
		return ErrConnectorInvalid
	}
	args := []any{state, reason, commentURL, ownerActionTime(at)}
	args = append(args, discussionScopeArgs(s)...)
	args = append(args, id, expected)
	query := `UPDATE discussion_conclusions SET state=?,reason=?,comment_url=?,updated_at=? WHERE ` + discussionScopeWhere + ` AND id=? AND state=?`
	if expected == "pending" && state == "sending" {
		query += ` AND (next_attempt_at='' OR next_attempt_at<=?)`
		args = append(args, ownerActionTime(at))
	}
	r, err := db.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrWorkflowRevision
	}
	return err
}

// FinishAdvisory fences a private result through the same durable worker slot.
func (db *DB) FinishAdvisory(ctx context.Context, a WorkflowAttempt, f WorkflowFinish, at time.Time) error {
	if a.AdmissionKind != "advisory" {
		return ErrConnectorInvalid
	}
	return db.FinishWorkflowAttempt(ctx, a, f, at)
}
func finishAdvisoryTx(ctx context.Context, tx *sql.Tx, a WorkflowAttempt, f WorkflowFinish, at time.Time) error {
	if f.State != "succeeded" && f.State != "failed" && f.State != "uncertain" && f.State != "cancelled" {
		return ErrConnectorInvalid
	}
	if len(f.Result) > 128*1024 || !utf8.ValidString(f.Result) {
		return ErrConnectorInvalid
	}
	admission, err := scanAdvisory(tx.QueryRowContext(ctx, `SELECT `+advisoryColumns+` FROM advisory_admissions WHERE id=? AND job_id=? AND execution_id=?`, a.AdvisoryID, a.JobID, a.ExecutionID))
	if err != nil {
		return err
	}
	if err = settleAdvisoryMeterTx(ctx, tx, a.ExecutionID, f.State, at); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_attempts SET state=?,finished_at=? WHERE id=?`, f.State, ownerActionTime(at), a.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE durable_workflow_jobs SET state=?,revision=revision+1,result=?,summary=?,updated_at=? WHERE id=? AND admission_kind='advisory'`, f.State, f.Result, f.Summary, ownerActionTime(at), a.JobID); err != nil {
		return err
	}
	c, err := scanDiscussion(tx.QueryRowContext(ctx, `SELECT `+discussionColumns+` FROM task_discussions WHERE id=? AND active_job=?`, admission.ConversationID, a.JobID))
	if err != nil {
		return err
	}
	if f.State == "succeeded" {
		var answer struct {
			Answer string `json:"answer"`
		}
		if err = json.Unmarshal([]byte(f.Result), &answer); err != nil {
			return err
		}
		// Result schemas are enforced by the runner; retention still bounds whole exchanges.
		c.Exchanges = append(c.Exchanges, DiscussionExchange{Question: admission.Question, Answer: answer.Answer, JobID: a.JobID, Basis: compactAdvisoryBasis(admission.Basis)})
		bounded, omitted, err := BoundedDiscussionContext(c.Exchanges, "")
		if err != nil {
			return err
		}
		c.Exchanges = bounded
		c.OmittedCount += int64(omitted)
	}
	raw, err := json.Marshal(c.Exchanges)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE task_discussions SET active_job='',revision=revision+1,exchanges=?,omitted_count=?,updated_at=? WHERE id=? AND active_job=?`, string(raw), c.OmittedCount, ownerActionTime(at), c.ID, a.JobID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE durable_workflow_worker SET attempt_id=0,expires_at='' WHERE singleton=1 AND attempt_id=? AND generation=?`, a.ID, a.Fence)
	return err
}

// ReadAdvisoryByExecution supports worker recovery without granting workflow authority.
func (db *DB) ReadAdvisoryByExecution(ctx context.Context, execution string) (AdvisoryAdmission, error) {
	return scanAdvisory(db.sql.QueryRowContext(ctx, `SELECT `+advisoryColumns+` FROM advisory_admissions WHERE execution_id=?`, execution))
}

// ReadAdvisoryForAttempt resolves only the admission bound to this execution.
func (db *DB) ReadAdvisoryForAttempt(ctx context.Context, a WorkflowAttempt) (AdvisoryAdmission, error) {
	if a.AdmissionKind != "advisory" {
		return AdvisoryAdmission{}, ErrConnectorInvalid
	}
	return scanAdvisory(db.sql.QueryRowContext(ctx, `SELECT `+advisoryColumns+` FROM advisory_admissions WHERE id=? AND job_id=? AND execution_id=?`, a.AdvisoryID, a.JobID, a.ExecutionID))
}

// AdvisoryTarget resolves a handle only within the authenticated owner generation.
func (db *DB) AdvisoryTarget(ctx context.Context, owner, generation int64, id string) (DiscussionScope, error) {
	var s DiscussionScope
	err := db.sql.QueryRowContext(ctx, `SELECT owner_id,owner_generation,repository_ref,task_ref FROM advisory_admissions WHERE owner_id=? AND owner_generation=? AND (id=? OR job_id=?)`, owner, generation, id, id).Scan(&s.OwnerID, &s.OwnerGeneration, &s.RepositoryRef, &s.TaskRef)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return s, err
}

// ConclusionTarget resolves a publication handle within its authenticated scope.
func (db *DB) ConclusionTarget(ctx context.Context, owner, generation int64, id string) (DiscussionScope, error) {
	var s DiscussionScope
	err := db.sql.QueryRowContext(ctx, `SELECT owner_id,owner_generation,repository_ref,task_ref FROM discussion_conclusions WHERE owner_id=? AND owner_generation=? AND id=?`, owner, generation, id).Scan(&s.OwnerID, &s.OwnerGeneration, &s.RepositoryRef, &s.TaskRef)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return s, err
}

// compactAdvisoryBasis preserves provenance without replaying captured resources.
func compactAdvisoryBasis(raw string) string {
	var basis struct {
		TaskRef  string `json:"task_ref,omitempty"`
		Stage    string `json:"stage,omitempty"`
		Role     string `json:"role,omitempty"`
		Revision string `json:"revision,omitempty"`
		Commit   string `json:"commit,omitempty"`
	}
	if json.Unmarshal([]byte(raw), &basis) != nil {
		return submissionDigest([]byte(raw))
	}
	if basis.Revision == "" {
		basis.Revision = submissionDigest([]byte(raw))
	}
	encoded, err := json.Marshal(basis)
	if err != nil {
		return submissionDigest([]byte(raw))
	}
	return string(encoded)
}

// UpdateDiscussionConclusionRetry defers only a definitively unsent dispatch.
func (db *DB) UpdateDiscussionConclusionRetry(ctx context.Context, s DiscussionScope, id, expected, reason string, due, at time.Time) error {
	if expected != "sending" || !due.After(at) {
		return ErrConnectorInvalid
	}
	args := []any{reason, ownerActionTime(due), ownerActionTime(at)}
	args = append(args, discussionScopeArgs(s)...)
	args = append(args, id, expected)
	r, err := db.sql.ExecContext(ctx, `UPDATE discussion_conclusions SET state='pending',reason=?,next_attempt_at=?,updated_at=? WHERE `+discussionScopeWhere+` AND id=? AND state=?`, args...)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrWorkflowRevision
	}
	return err
}

// ClaimDiscussionPublication checks original live consent and fences the first
// possible send together. No caller may post unless this transaction succeeds.
func (db *DB) ClaimDiscussionPublication(ctx context.Context, c DiscussionConclusion, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		args := append(discussionScopeArgs(c.DiscussionScope), c.ID)
		stored, err := scanConclusion(tx.QueryRowContext(ctx, `SELECT `+conclusionColumns+` FROM discussion_conclusions WHERE `+discussionScopeWhere+` AND id=?`, args...))
		if err != nil {
			return err
		}
		if stored.Digest != c.Digest || stored.Rendered != c.Rendered {
			return ErrWorkflowConflict
		}
		if stored.State != "pending" || !stored.NextAttemptAt.IsZero() && at.Before(stored.NextAttemptAt) {
			return ErrWorkflowRevision
		}
		target, err := resolveConnectorDiscussionTarget(ctx, tx, stored.TaskRef)
		if err != nil {
			return err
		}
		if target.RepositoryRef != stored.RepositoryRef {
			return ErrConnectorScope
		}
		var client string
		if err = tx.QueryRowContext(ctx, `SELECT client_id FROM connector_grants WHERE id=?`, stored.GrantID).Scan(&client); errors.Is(err, sql.ErrNoRows) {
			return ErrConnectorInvalid
		} else if err != nil {
			return err
		}
		actor := OwnerActionPrincipal{OwnerID: strconv.FormatInt(stored.OwnerID, 10), OwnerGeneration: stored.OwnerGeneration, Transport: "mcp", ClientID: client, GrantID: stored.GrantID}
		if err = ownerActionAuthority(ctx, tx, actor, OwnerAction{Principal: actor, RepositoryID: target.RepoID, RepositoryRef: target.RepositoryRef, Scope: "task:discussion-publish"}, at); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE discussion_conclusions SET state='sending',updated_at=? WHERE id=? AND state='pending'`, ownerActionTime(at), stored.ID)
		return err
	})
}
